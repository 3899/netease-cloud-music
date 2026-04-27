// MIT License
//
// Copyright (c) 2024 chaunsin
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//

package ncmctl

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chaunsin/netease-cloud-music/api"
	"github.com/chaunsin/netease-cloud-music/api/eapi"
	"github.com/chaunsin/netease-cloud-music/api/types"
	"github.com/chaunsin/netease-cloud-music/api/weapi"
	"github.com/chaunsin/netease-cloud-music/pkg/database"
	"github.com/chaunsin/netease-cloud-music/pkg/log"
	"github.com/chaunsin/netease-cloud-music/pkg/utils"

	"github.com/cheggaaa/pb/v3"
	"github.com/spf13/cobra"
)

const (
	playIDsDefaultDailyMin int64 = 50
	playIDsDefaultDailyMax int64 = 100
	playIDsDefaultGapMin   int64 = 5
	playIDsDefaultGapMax   int64 = 20
)

type PlayIDsOpts struct {
	IDs      string
	IDsFile  string
	Num      int64 // max songs per run (0 = use daily limit)
	GapMin   int64
	GapMax   int64
	DailyMin int64 // daily limit range min
	DailyMax int64 // daily limit range max
}

type PlayIDs struct {
	root *Root
	cmd  *cobra.Command
	opts PlayIDsOpts
	l    *log.Logger
	rng  *rand.Rand
	cache *audioCache // CDN download cache: same songId only downloads once
}

type playSongMetadata struct {
	ID       int64
	Name     string
	AlbumID  int64
	Duration int64
}

type playSongQueue struct {
	ids      []int64
	round    []int64
	index    int
	roundNum int
	rng      *rand.Rand
}

func NewPlayIDs(root *Root, l *log.Logger) *PlayIDs {
	c := &PlayIDs{
		root:  root,
		l:     l,
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
		cache: newAudioCache(),
		cmd: &cobra.Command{
			Use:     "playids",
			Short:   "[need login] Fully play specified songs by song IDs",
			Example: "  ncmctl playids --ids 2600804126,1984580503\n  ncmctl playids --ids-file ./song_ids.txt --num 50",
		},
	}
	c.addFlags()
	c.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return c.execute(cmd.Context())
	}
	return c
}

func (c *PlayIDs) addFlags() {
	c.cmd.PersistentFlags().StringVar(&c.opts.IDs, "ids", "", "comma-separated song ids")
	c.cmd.PersistentFlags().StringVar(&c.opts.IDsFile, "ids-file", "", "path to a file containing song ids")
	c.cmd.PersistentFlags().Int64VarP(&c.opts.Num, "num", "n", 0, "max number of songs per run (0 = use daily limit)")
	c.cmd.PersistentFlags().Int64Var(&c.opts.GapMin, "gap-min", playIDsDefaultGapMin, "minimum random gap between songs in seconds")
	c.cmd.PersistentFlags().Int64Var(&c.opts.GapMax, "gap-max", playIDsDefaultGapMax, "maximum random gap between songs in seconds")
	c.cmd.PersistentFlags().Int64Var(&c.opts.DailyMin, "daily-min", playIDsDefaultDailyMin, "daily play limit range minimum")
	c.cmd.PersistentFlags().Int64Var(&c.opts.DailyMax, "daily-max", playIDsDefaultDailyMax, "daily play limit range maximum")
}

func (c *PlayIDs) validate() error {
	if c.opts.IDs == "" && c.opts.IDsFile == "" {
		return fmt.Errorf("ids or ids-file is required")
	}
	if c.opts.Num < 0 {
		return fmt.Errorf("num must be >= 0")
	}
	if c.opts.GapMin < 0 || c.opts.GapMax < 0 {
		return fmt.Errorf("gap-min and gap-max must be >= 0")
	}
	if c.opts.GapMin > c.opts.GapMax {
		return fmt.Errorf("gap-min > gap-max")
	}
	if c.opts.DailyMin <= 0 {
		return fmt.Errorf("daily-min must be > 0")
	}
	if c.opts.DailyMax < c.opts.DailyMin {
		return fmt.Errorf("daily-max must be >= daily-min")
	}
	if _, err := parsePlaySongIDs(c.opts.IDs, c.opts.IDsFile); err != nil {
		return err
	}
	return nil
}

func (c *PlayIDs) Add(command ...*cobra.Command) {
	c.cmd.AddCommand(command...)
}

func (c *PlayIDs) Command() *cobra.Command {
	return c.cmd
}

func (c *PlayIDs) execute(ctx context.Context) error {
	// Merge config file values into opts (config file < command-line flags)
	if cfg := c.root.Cfg.PlayIDs; cfg != nil {
		if !c.cmd.Flags().Changed("daily-min") && cfg.DailyMin > 0 {
			c.opts.DailyMin = cfg.DailyMin
		}
		if !c.cmd.Flags().Changed("daily-max") && cfg.DailyMax > 0 {
			c.opts.DailyMax = cfg.DailyMax
		}
	}
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	songIDs, err := parsePlaySongIDs(c.opts.IDs, c.opts.IDsFile)
	if err != nil {
		return fmt.Errorf("parsePlaySongIDs: %w", err)
	}

	cli, err := api.NewClient(c.root.Cfg.Network, c.l)
	if err != nil {
		return fmt.Errorf("NewClient: %w", err)
	}
	defer cli.Close(ctx)
	request := weapi.New(cli)
	eapiRequest := eapi.New(cli)

	if request.NeedLogin(ctx) {
		return fmt.Errorf("need login")
	}

	user, err := request.GetUserInfo(ctx, &weapi.GetUserInfoReq{})
	if err != nil {
		return fmt.Errorf("GetUserInfo: %w", err)
	}
	if user.Code != 200 || user.Account == nil {
		return fmt.Errorf("GetUserInfo: %+v", user)
	}
	uid := fmt.Sprintf("%v", user.Account.Id)

	defer func() {
		refresh, err := request.TokenRefresh(ctx, &weapi.TokenRefreshReq{})
		if err != nil || refresh.Code != 200 {
			log.Warn("TokenRefresh resp:%+v err: %s", refresh, err)
		}
	}()

	db, err := database.New(c.root.Cfg.Database)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close(ctx)

	// Load or generate device fingerprint for Android client simulation (per-uid)
	fp, err := loadOrCreateFingerprint(ctx, db, uid, c.rng, c.root.Cfg.Device)
	if err != nil {
		return fmt.Errorf("loadOrCreateFingerprint: %w", err)
	}
	injectDeviceCookies(cli, fp)

	expire, err := utils.TimeUntilMidnight("Local")
	if err != nil {
		return fmt.Errorf("TimeUntilMidnight: %w", err)
	}

	// --- Independent playids daily limit (separate from scrobble) ---
	dailyLimit, err := c.resolveDailyLimit(ctx, db, uid, expire)
	if err != nil {
		return fmt.Errorf("resolveDailyLimit: %w", err)
	}

	record, err := db.Get(ctx, playidsTodayNumKey(uid))
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			record = "0"
		} else {
			return fmt.Errorf("get playids today num: %w", err)
		}
	}
	finish, err := strconv.ParseInt(record, 10, 64)
	if err != nil {
		return fmt.Errorf("ParseInt(%v): %w", record, err)
	}
	if finish >= dailyLimit {
		c.cmd.Printf("[playids] 今日已达上限: %d/%d\n", finish, dailyLimit)
		return nil
	}

	left := dailyLimit - finish
	target := left
	if c.opts.Num > 0 && c.opts.Num < target {
		target = c.opts.Num
	}
	metadata, err := c.fetchSongMetadata(ctx, request, songIDs)
	if err != nil {
		return fmt.Errorf("fetchSongMetadata: %w", err)
	}
	if len(metadata) == 0 {
		return fmt.Errorf("input resource is empty or unavailable")
	}

	var validSongIDs = make([]int64, 0, len(songIDs))
	for _, id := range songIDs {
		if _, ok := metadata[id]; ok {
			validSongIDs = append(validSongIDs, id)
		}
	}
	if len(validSongIDs) == 0 {
		return fmt.Errorf("input resource is empty or unavailable")
	}

	nickname := ""
	if user.Profile.Nickname != "" {
		nickname = user.Profile.Nickname
	}
	c.cmd.Printf("[playids] 当前账号: uid=%s 昵称=%q\n", uid, nickname)
	c.cmd.Printf("[playids] 设备指纹: %s\n", formatDeviceInfo(fp))
	c.cmd.Printf("[playids] 任务开始: 歌曲池=%d首, 目标播放=%d首, 今日上限=%d首, 今日已完成=%d首, 今日剩余=%d首, 间隔=%ds-%ds\n",
		len(validSongIDs), target, dailyLimit, finish, left, c.opts.GapMin, c.opts.GapMax)
	printPlaySongList(c.cmd, metadata, validSongIDs)

	bar := pb.Full.Start64(target)
	defer bar.Finish()

	queue := newPlaySongQueue(validSongIDs, c.rng)
	var success int64
	var failed int64
	for success < target {
		var roundSuccess int64
		for i := 0; i < len(validSongIDs) && success < target; i++ {
			songID, round, index, newRound := queue.Next()
			info := metadata[songID]
			if newRound && round > 1 {
				c.cmd.Printf("[playids] 开始第%d轮随机打乱\n", round)
				printPlaySongList(c.cmd, metadata, queue.CurrentRound())
			}
			attempt := success + failed + 1
			c.cmd.Printf("[playids] 正在播放: 第%d/%d首, 第%d轮第%d首, songId=%d, 歌名=%q, 时长=%s\n",
				attempt, target, round, index, songID, info.Name, formatPlayDuration(time.Duration(info.Duration)*time.Millisecond))
			if err := c.playSong(ctx, cli, request, eapiRequest, info); err != nil {
				failed++
				c.cmd.Printf("[playids] 本首结果: 第%d/%d首, 失败, songId=%d, 歌名=%q, 原因=%q\n",
					attempt, target, songID, info.Name, err.Error())
				log.Warn("[playids] play %d (%s) err: %v", songID, info.Name, err)
				continue
			}

			if _, err := db.Increment(ctx, playidsTodayNumKey(uid), 1, expire); err != nil {
				log.Warn("[playids] set %v record err: %v", songID, err)
			}

			success++
			roundSuccess++
			c.cmd.Printf("[playids] 本首结果: 第%d/%d首, 成功, songId=%d, 歌名=%q\n",
				attempt, target, songID, info.Name)
			bar.Increment()

			if success >= target {
				break
			}
			if gap := randomGap(c.rng, c.opts.GapMin, c.opts.GapMax); gap > 0 {
				c.cmd.Printf("[playids] 播放间隔: 下一首等待 %s\n", formatPlayDuration(gap))
				if err := sleepWithContext(ctx, gap); err != nil {
					return err
				}
			}
		}
		if roundSuccess == 0 {
			if success == 0 {
				return fmt.Errorf("all songs failed to play")
			}
			log.Warn("[playids] all songs in current round failed, stop early")
			break
		}
	}

	c.cmd.Printf("[playids] 执行完成: 目标=%d首, 成功=%d首, 失败=%d首\n", target, success, failed)
	c.cmd.Printf("[playids] 今日统计: 执行前=%d首, 执行后=%d首, 今日上限=%d首\n", finish, finish+success, dailyLimit)
	c.cmd.Printf("report total: %d success: %d failed: %d\n", target, success, failed)
	return nil
}

// playidsTodayNumKey returns the DB key for tracking how many songs
// have been played today via playids (independent from scrobble).
func playidsTodayNumKey(uid string) string {
	return fmt.Sprintf("playids:today:%v", uid)
}

// playidsTodayTargetKey returns the DB key for the randomized daily target.
// The target is determined once per day so repeated runs see the same limit.
func playidsTodayTargetKey(uid string) string {
	return fmt.Sprintf("playids:today:target:%v", uid)
}

// resolveDailyLimit determines today's play limit. On the first run of the day,
// a random value within [DailyMin, DailyMax] is chosen and persisted.
// Subsequent runs within the same day reuse that value.
func (c *PlayIDs) resolveDailyLimit(ctx context.Context, db database.Database, uid string, expire time.Duration) (int64, error) {
	dailyMin := c.opts.DailyMin
	dailyMax := c.opts.DailyMax
	if dailyMax < dailyMin {
		dailyMax = dailyMin
	}

	// Try to load today's persisted target
	key := playidsTodayTargetKey(uid)
	if data, err := db.Get(ctx, key); err == nil && data != "" {
		if target, err := strconv.ParseInt(data, 10, 64); err == nil && target > 0 {
			log.Debug("[playids] reusing today's daily target: %d", target)
			return target, nil
		}
	}

	// First run today: randomize within range
	var target int64
	if dailyMin == dailyMax {
		target = dailyMin
	} else {
		target = dailyMin + c.rng.Int63n(dailyMax-dailyMin+1)
	}

	// Persist until midnight
	if err := db.Set(ctx, key, strconv.FormatInt(target, 10), expire); err != nil {
		log.Warn("[playids] failed to persist daily target: %v", err)
		// Non-fatal: continue with the computed target
	}

	log.Debug("[playids] set today's daily target: %d (range %d~%d)", target, dailyMin, dailyMax)
	return target, nil
}

func (c *PlayIDs) fetchSongMetadata(ctx context.Context, request *weapi.Api, ids []int64) (map[int64]playSongMetadata, error) {
	pages, err := utils.SplitSlice(ids, 500)
	if err != nil {
		return nil, fmt.Errorf("SplitSlice: %w", err)
	}

	metadata := make(map[int64]playSongMetadata, len(ids))
	for _, page := range pages {
		req := make([]weapi.SongDetailReqList, 0, len(page))
		for _, id := range page {
			req = append(req, weapi.SongDetailReqList{Id: fmt.Sprintf("%d", id), V: 0})
		}

		resp, err := request.SongDetail(ctx, &weapi.SongDetailReq{C: req})
		if err != nil {
			return nil, fmt.Errorf("SongDetail: %w", err)
		}
		if resp.Code != 200 {
			return nil, fmt.Errorf("SongDetail err: %+v", resp)
		}
		for _, song := range resp.Songs {
			metadata[song.Id] = playSongMetadata{
				ID:       song.Id,
				Name:     song.Name,
				AlbumID:  song.Al.Id,
				Duration: song.Dt,
			}
		}
	}
	return metadata, nil
}

func (c *PlayIDs) playSong(ctx context.Context, cli *api.Client, request *weapi.Api, eapiReq *eapi.Api, song playSongMetadata) error {
	// Use EAPI (Android client path) to get the song player URL
	playResp, err := eapiReq.SongPlayerV1(ctx, &eapi.SongPlayerV1Req{
		Ids:   types.IntsString{song.ID},
		Level: types.LevelStandard,
	})
	if err != nil {
		return fmt.Errorf("SongPlayerV1(%d): %w", song.ID, err)
	}
	if playResp.Code != 200 {
		return fmt.Errorf("SongPlayerV1(%d) err: %+v", song.ID, playResp)
	}
	if len(playResp.Data) <= 0 {
		return fmt.Errorf("SongPlayerV1(%d) data is empty", song.ID)
	}

	data := playResp.Data[0]
	if data.Code != 200 || data.Url == "" {
		return fmt.Errorf("SongPlayerV1(%d) unavailable: code=%d url=%q", song.ID, data.Code, data.Url)
	}

	duration := data.Time
	if duration <= 0 {
		duration = song.Duration
	}
	if duration <= 0 {
		return fmt.Errorf("song %d duration is invalid", song.ID)
	}

	// Determine end type and effective play time before starting
	endType := randomEndType(c.rng)
	effectiveDuration := duration
	if endType == "interrupt" {
		// Simulate skipping: play 30%-80% of the song
		ratio := 0.3 + c.rng.Float64()*0.5
		effectiveDuration = int64(float64(duration) * ratio)
	}
	// Add small jitter to make timing less mechanical (±0~3s)
	effectiveDuration = randomPlayDurationJitter(c.rng, effectiveDuration)

	expected := time.Duration(effectiveDuration) * time.Millisecond
	sourceInfo := playSourceForSong(song)

	// === Phase 1: startplay event (before streaming) ===
	if resp, err := request.WebLog(ctx, buildStartPlayWebLogRequest(song, sourceInfo)); err != nil {
		log.Warn("[playids] startplay WebLog(%d) err: %v", song.ID, err)
		c.cmd.Printf("[playids] Phase1 startplay: songId=%d, 结果=失败, 原因=%v\n", song.ID, err)
	} else if resp.Code != 200 {
		log.Warn("[playids] startplay WebLog(%d) code: %d", song.ID, resp.Code)
		c.cmd.Printf("[playids] Phase1 startplay: songId=%d, 结果=异常, code=%d\n", song.ID, resp.Code)
	} else {
		c.cmd.Printf("[playids] Phase1 startplay: songId=%d, 结果=成功\n", song.ID)
	}

	// Small delay simulating audio buffer initialization (100~500ms)
	bufferDelay := time.Duration(100+c.rng.Intn(400)) * time.Millisecond
	if err := sleepWithContext(ctx, bufferDelay); err != nil {
		return err
	}

	// === Phase 2: play begin event (after buffering, before actual play) ===
	c.cmd.Printf("[playids] Phase2 play-begin: songId=%d, 缓冲耗时=%s\n", song.ID, formatPlayDuration(bufferDelay))
	if resp, err := request.WebLog(ctx, buildPlayBeginWebLogRequest(song, sourceInfo)); err != nil {
		log.Warn("[playids] play-begin WebLog(%d) err: %v", song.ID, err)
		c.cmd.Printf("[playids] Phase2 play-begin: songId=%d, 结果=失败, 原因=%v\n", song.ID, err)
	} else if resp.Code != 200 {
		log.Warn("[playids] play-begin WebLog(%d) code: %d", song.ID, resp.Code)
		c.cmd.Printf("[playids] Phase2 play-begin: songId=%d, 结果=异常, code=%d\n", song.ID, resp.Code)
	} else {
		c.cmd.Printf("[playids] Phase2 play-begin: songId=%d, 结果=成功\n", song.ID)
	}

	// Stream audio or use cache (same songId only downloads from CDN once)
	var elapsed time.Duration
	var fromCache bool
	if entry, ok := c.cache.get(song.ID); ok {
		// Cache hit: simulate download time with slight variation, skip actual CDN request
		fromCache = true
		simulated := entry.elapsed + time.Duration(c.rng.Intn(200)-100)*time.Millisecond
		if simulated < 50*time.Millisecond {
			simulated = 50 * time.Millisecond
		}
		if err := sleepWithContext(ctx, simulated); err != nil {
			return err
		}
		elapsed = simulated
	} else {
		// Cache miss: download from CDN, record metadata
		var err error
		elapsed, err = streamSongToDiscard(ctx, cli, data.Url)
		if err != nil {
			return fmt.Errorf("streamSongToDiscard(%d): %w", song.ID, err)
		}
		c.cache.put(song.ID, elapsed)
	}

	downloadFlag := 0
	if fromCache {
		downloadFlag = 1 // indicates local cache playback in WebLog
	}

	wait := expected - elapsed - bufferDelay
	if wait < 0 {
		wait = 0
	}
	cacheLabel := "CDN"
	if fromCache {
		cacheLabel = "缓存"
	}
	c.cmd.Printf("[playids] 拉流完成: songId=%d, 来源=%s, 已耗时=%s, 补等待=%s, end=%s\n",
		song.ID, cacheLabel, formatPlayDuration(elapsed+bufferDelay), formatPlayDuration(wait), endType)
	if wait > 0 {
		if err := sleepWithContext(ctx, wait); err != nil {
			return err
		}
	}

	// === Phase 3: play complete event (with time and end type) ===
	reportSeconds := millisecondsToSeconds(effectiveDuration)
	resp, err := request.WebLog(ctx, buildPlayCompleteWebLogRequest(song, sourceInfo, reportSeconds, endType, downloadFlag))
	if err != nil {
		return fmt.Errorf("WebLog(%d): %w", song.ID, err)
	}
	if resp.Code != 200 {
		return fmt.Errorf("WebLog(%d) err: %+v", song.ID, resp)
	}
	c.cmd.Printf("[playids] Phase3 play-complete: songId=%d, 结果=成功, 上报时长=%ds, end=%s, download=%d\n", song.ID, reportSeconds, endType, downloadFlag)
	return nil
}

func parsePlaySongIDs(idsText, idsFile string) ([]int64, error) {
	var input []string
	if idsText != "" {
		input = append(input, idsText)
	}
	if idsFile != "" {
		data, err := os.ReadFile(idsFile)
		if err != nil {
			return nil, fmt.Errorf("ReadFile(%s): %w", idsFile, err)
		}
		input = append(input, string(data))
	}

	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for _, item := range input {
		tokens := strings.FieldsFunc(item, func(r rune) bool {
			switch r {
			case ',', ';', '\n', '\r', '\t', ' ':
				return true
			default:
				return false
			}
		})
		for _, token := range tokens {
			id, err := strconv.ParseInt(strings.TrimSpace(token), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid song id %q: %w", token, err)
			}
			if id <= 0 {
				return nil, fmt.Errorf("invalid song id %q", token)
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("song ids is empty")
	}
	return ids, nil
}

func newPlaySongQueue(ids []int64, rng *rand.Rand) *playSongQueue {
	return &playSongQueue{
		ids: ids,
		rng: rng,
	}
}

func (q *playSongQueue) Next() (id int64, round int, index int, newRound bool) {
	if len(q.ids) == 0 {
		return 0, 0, 0, false
	}
	if q.index >= len(q.round) {
		q.round = shuffleSongIDs(q.ids, q.rng)
		q.index = 0
		q.roundNum++
		newRound = true
	}
	id = q.round[q.index]
	round = q.roundNum
	index = q.index + 1
	q.index++
	return id, round, index, newRound
}

func (q *playSongQueue) CurrentRound() []int64 {
	return append([]int64(nil), q.round...)
}

// audioCache stores CDN download metadata per songId so the same song
// is only downloaded once from CDN. Subsequent plays simulate local cache.
type audioCache struct {
	store map[int64]audioCacheEntry
}

type audioCacheEntry struct {
	elapsed time.Duration // time the first CDN download took
}

func newAudioCache() *audioCache {
	return &audioCache{store: make(map[int64]audioCacheEntry)}
}

func (c *audioCache) get(songId int64) (audioCacheEntry, bool) {
	e, ok := c.store[songId]
	return e, ok
}

func (c *audioCache) put(songId int64, elapsed time.Duration) {
	c.store[songId] = audioCacheEntry{elapsed: elapsed}
}

func shuffleSongIDs(ids []int64, rng *rand.Rand) []int64 {
	shuffled := append([]int64(nil), ids...)
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled
}

// playSourceInfo holds the source context for WebLog events,
// matching the fields the Android client sends (source, sourceId, content).
type playSourceInfo struct {
	Source   string // e.g. "album", "toplist", "playlist"
	SourceID string // the source resource id
	Content  string // format "id=<sourceId>"
}

// playSourceForSong determines the play source context from song metadata.
func playSourceForSong(song playSongMetadata) playSourceInfo {
	if song.AlbumID > 0 {
		sid := fmt.Sprintf("%d", song.AlbumID)
		return playSourceInfo{Source: "album", SourceID: sid, Content: fmt.Sprintf("id=%s", sid)}
	}
	return playSourceInfo{Source: "list", SourceID: "", Content: ""}
}

// buildStartPlayWebLogRequest builds the Phase 1 "startplay" event.
// Sent before audio streaming begins, signals intent to play.
func buildStartPlayWebLogRequest(song playSongMetadata, src playSourceInfo) *weapi.WebLogReq {
	payload := map[string]interface{}{
		"id":       song.ID,
		"type":     "song",
		"mainsite": "1",
	}
	if src.Content != "" {
		payload["content"] = src.Content
	}

	return &weapi.WebLogReq{
		Logs: []map[string]interface{}{
			{
				"action": "startplay",
				"json":   payload,
			},
		},
	}
}

// buildPlayBeginWebLogRequest builds the Phase 2 "play" begin event (no time/end).
// Sent after audio buffering starts, confirms playback has begun.
func buildPlayBeginWebLogRequest(song playSongMetadata, src playSourceInfo) *weapi.WebLogReq {
	payload := map[string]interface{}{
		"id":       fmt.Sprintf("%d", song.ID),
		"type":     "song",
		"mainsite": "1",
	}
	if src.Source != "" {
		payload["source"] = src.Source
		payload["sourceid"] = src.SourceID
	}
	if src.Content != "" {
		payload["content"] = src.Content
	}

	return &weapi.WebLogReq{
		Logs: []map[string]interface{}{
			{
				"action": "play",
				"json":   payload,
			},
		},
	}
}

// buildPlayCompleteWebLogRequest builds the Phase 3 "play" complete event (with time/end).
// Sent after the song finishes playing (or is interrupted).
func buildPlayCompleteWebLogRequest(song playSongMetadata, src playSourceInfo, durationSeconds int64, endType string, downloadFlag int) *weapi.WebLogReq {
	payload := map[string]interface{}{
		"type":     "song",
		"wifi":     0,
		"download": downloadFlag,
		"id":       song.ID,
		"time":     durationSeconds,
		"end":      endType,
		"mainsite": "1",
	}
	if src.Source != "" {
		payload["source"] = src.Source
		payload["sourceId"] = src.SourceID
	}
	if src.Content != "" {
		payload["content"] = src.Content
	}

	return &weapi.WebLogReq{
		Logs: []map[string]interface{}{
			{
				"action": "play",
				"json":   payload,
			},
		},
	}
}

// randomEndType returns a play end type matching real client distribution:
// ~85% playend (natural finish), ~10% ui (UI-triggered end), ~5% interrupt (skip).
func randomEndType(rng *rand.Rand) string {
	r := rng.Float32()
	switch {
	case r < 0.85:
		return "playend"
	case r < 0.95:
		return "ui"
	default:
		return "interrupt"
	}
}

// randomPlayDurationJitter adds ±0~3 seconds of jitter to the play duration (in ms)
// to avoid perfectly mechanical timing. Never reduces below 1 second.
func randomPlayDurationJitter(rng *rand.Rand, durationMs int64) int64 {
	jitterMs := int64(rng.Intn(3001)) // 0~3000ms
	if rng.Intn(2) == 0 {
		jitterMs = -jitterMs
	}
	result := durationMs + jitterMs
	if result < 1000 {
		result = 1000
	}
	return result
}

func streamSongToDiscard(ctx context.Context, cli *api.Client, targetURL string) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return 0, fmt.Errorf("NewRequestWithContext: %w", err)
	}
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", "https://music.163.com")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept-Language", "zh-CN,zh-Hans;q=0.9")
	req.Header.Set("User-Agent", fmt.Sprintf("NeteaseMusic/%s(9050000) Android/14", appVersion))

	start := time.Now()
	resp, err := cli.GetClient().Do(req)
	if err != nil {
		return time.Since(start), err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return time.Since(start), fmt.Errorf("http status code: %d", resp.StatusCode)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return time.Since(start), err
	}
	return time.Since(start), nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// randomGap returns a human-like inter-song gap using a log-normal-inspired distribution.
// Most gaps cluster around the lower end (5-10s) with occasional longer pauses,
// mimicking real listening behavior more naturally than uniform random.
func randomGap(rng *rand.Rand, minSeconds, maxSeconds int64) time.Duration {
	if maxSeconds <= 0 || maxSeconds < minSeconds {
		return 0
	}
	if minSeconds == maxSeconds {
		return time.Duration(minSeconds) * time.Second
	}
	// Log-normal-like: use exponential of normal to bias toward lower values
	span := float64(maxSeconds - minSeconds)
	u := rng.Float64()
	// Transform uniform [0,1) with power curve to bias toward smaller values
	biased := math.Pow(u, 1.5)
	value := float64(minSeconds) + biased*span
	return time.Duration(value) * time.Second
}

func millisecondsToSeconds(ms int64) int64 {
	if ms <= 0 {
		return 0
	}
	return (ms + 999) / 1000
}

func formatPlayDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

func printPlaySongList(cmd *cobra.Command, metadata map[int64]playSongMetadata, ids []int64) {
	for i, id := range ids {
		info, ok := metadata[id]
		if !ok {
			continue
		}
		cmd.Printf("[playids] 歌曲池[%d]: songId=%d 歌名=%q 时长=%s\n",
			i+1, info.ID, info.Name, formatPlayDuration(time.Duration(info.Duration)*time.Millisecond))
	}
}
