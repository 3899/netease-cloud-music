package ncmctl

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestParsePlaySongIDs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "song_ids.txt")
	if err := os.WriteFile(file, []byte("3\n4,2\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ids, err := parsePlaySongIDs("1,2; 3", file)
	if err != nil {
		t.Fatalf("parsePlaySongIDs: %v", err)
	}

	want := []int64{1, 2, 3, 4}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids mismatch: got=%v want=%v", ids, want)
	}
}

func TestParsePlaySongIDsInvalid(t *testing.T) {
	t.Parallel()

	if _, err := parsePlaySongIDs("1,abc", ""); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestPlaySongQueueRepeatsAcrossRounds(t *testing.T) {
	t.Parallel()

	queue := newPlaySongQueue([]int64{1, 2}, rand.New(rand.NewSource(1)))

	got := make([]int64, 0, 5)
	rounds := make([]int, 0, 5)
	for range 5 {
		id, round, _, _ := queue.Next()
		got = append(got, id)
		rounds = append(rounds, round)
	}
	if len(got) != 5 {
		t.Fatalf("unexpected queue length: %d", len(got))
	}

	firstRound := map[int64]struct{}{got[0]: {}, got[1]: {}}
	secondRound := map[int64]struct{}{got[2]: {}, got[3]: {}}
	if len(firstRound) != 2 || len(secondRound) != 2 {
		t.Fatalf("queue should contain both ids in each round: %v", got)
	}
	if got[4] != 1 && got[4] != 2 {
		t.Fatalf("unexpected repeated id: %d", got[4])
	}
	if rounds[0] != 1 || rounds[2] != 2 || rounds[4] != 3 {
		t.Fatalf("unexpected round sequence: %v", rounds)
	}
}

func TestBuildPlayCompleteWebLogRequest(t *testing.T) {
	t.Parallel()

	song := playSongMetadata{ID: 12, AlbumID: 34}
	src := playSourceForSong(song)
	withAlbum := buildPlayCompleteWebLogRequest(song, src, 123, "playend", 0)
	payload := withAlbum.Logs[0]["json"].(map[string]interface{})
	if payload["source"] != "album" {
		t.Fatalf("unexpected source: %v", payload["source"])
	}
	if payload["sourceId"] != "34" {
		t.Fatalf("unexpected sourceId: %v", payload["sourceId"])
	}
	if payload["content"] != "id=34" {
		t.Fatalf("unexpected content: %v", payload["content"])
	}
	if payload["end"] != "playend" {
		t.Fatalf("unexpected end: %v", payload["end"])
	}

	songNoAlbum := playSongMetadata{ID: 12}
	srcNoAlbum := playSourceForSong(songNoAlbum)
	withoutAlbum := buildPlayCompleteWebLogRequest(songNoAlbum, srcNoAlbum, 123, "ui", 1)
	payload = withoutAlbum.Logs[0]["json"].(map[string]interface{})
	if payload["source"] != "list" {
		t.Fatalf("unexpected source for no album: %v", payload["source"])
	}
	if payload["end"] != "ui" {
		t.Fatalf("unexpected end: %v", payload["end"])
	}
}

func TestRandomGapRange(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 10; i++ {
		gap := randomGap(rng, 5, 20)
		if gap < 5*time.Second || gap > 20*time.Second {
			t.Fatalf("gap out of range: %v", gap)
		}
	}
}

func TestFormatPlayDuration(t *testing.T) {
	t.Parallel()

	if got := formatPlayDuration(0); got != "0s" {
		t.Fatalf("unexpected zero duration: %s", got)
	}
	if got := formatPlayDuration(1500 * time.Millisecond); got != "2s" {
		t.Fatalf("unexpected rounded seconds: %s", got)
	}
	if got := formatPlayDuration(250 * time.Millisecond); got != "250ms" {
		t.Fatalf("unexpected sub-second duration: %s", got)
	}
}
