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
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chaunsin/netease-cloud-music/api"
	"github.com/chaunsin/netease-cloud-music/config"
	"github.com/chaunsin/netease-cloud-music/pkg/database"
	"github.com/chaunsin/netease-cloud-music/pkg/log"
)

const (
	// deviceFingerprintDBKeyPrefix is the DB key prefix; the full key includes uid
	// so each account has its own persistent device fingerprint.
	deviceFingerprintDBKeyPrefix = "playids:device:fingerprint:"

	// appVersion should match the APK version we analyzed
	appVersion     = "9.5.0"
	appVersionCode = "9050000"
	appChannel     = "netease"
)

// DeviceFingerprint represents a simulated Android device identity.
// Once generated, it is persisted to the database so the same device identity
// is reused across sessions — exactly as a real device would behave.
type DeviceFingerprint struct {
	DeviceId    string `json:"deviceId"`
	Os          string `json:"os"`
	OsVer       string `json:"osVer"`
	AppVer      string `json:"appVer"`
	VersionCode string `json:"versionCode"`
	Channel     string `json:"channel"`
	MobileName  string `json:"mobileName"`
	Resolution  string `json:"resolution"`
	BuildVer    string `json:"buildVer"`
}

// loadOrCreateFingerprint loads the persisted device fingerprint from the database,
// keyed by uid so each account has its own identity. If no fingerprint exists (first run),
// it generates one and persists it. If a DeviceConfig is provided, its non-empty fields
// override the stored/generated values.
func loadOrCreateFingerprint(ctx context.Context, db database.Database, uid string, rng *rand.Rand, cfgDev *config.DeviceConfig) (*DeviceFingerprint, error) {
	dbKey := deviceFingerprintDBKeyPrefix + uid

	var fp *DeviceFingerprint

	data, err := db.Get(ctx, dbKey)
	if err == nil && data != "" {
		var stored DeviceFingerprint
		if jsonErr := json.Unmarshal([]byte(data), &stored); jsonErr == nil {
			fp = &stored
			log.Debug("[device] loaded existing fingerprint for uid=%s: deviceId=%s mobileName=%s", uid, fp.DeviceId, fp.MobileName)
		} else {
			log.Warn("[device] failed to parse stored fingerprint for uid=%s, regenerating", uid)
		}
	}

	if fp == nil {
		fp = generateFingerprint(rng)
		log.Debug("[device] generated new fingerprint for uid=%s: deviceId=%s mobileName=%s", uid, fp.DeviceId, fp.MobileName)
	}

	// Apply config overrides: non-empty values from config take priority
	if cfgDev != nil {
		applyDeviceConfigOverrides(fp, cfgDev)
	}

	// Persist (update) the fingerprint
	raw, err := json.Marshal(fp)
	if err != nil {
		return nil, fmt.Errorf("marshal fingerprint: %w", err)
	}
	if err := db.Set(ctx, dbKey, string(raw)); err != nil {
		return nil, fmt.Errorf("persist fingerprint: %w", err)
	}

	return fp, nil
}

// applyDeviceConfigOverrides applies user-specified config values to the fingerprint.
// Only non-empty config fields override the auto-generated values.
func applyDeviceConfigOverrides(fp *DeviceFingerprint, cfg *config.DeviceConfig) {
	if cfg.DeviceId != "" {
		fp.DeviceId = cfg.DeviceId
	}
	if cfg.Os != "" {
		fp.Os = cfg.Os
	}
	if cfg.OsVer != "" {
		fp.OsVer = cfg.OsVer
	}
	if cfg.AppVer != "" {
		fp.AppVer = cfg.AppVer
	}
	if cfg.Channel != "" {
		fp.Channel = cfg.Channel
	}
	if cfg.MobileName != "" {
		fp.MobileName = cfg.MobileName
	}
	if cfg.Resolution != "" {
		fp.Resolution = cfg.Resolution
	}
}

// generateFingerprint creates a new random device fingerprint.
// It picks one complete real-world device profile to avoid impossible combinations
// (e.g. a budget phone with 2K resolution, or a 2024 phone with Android 12).
func generateFingerprint(rng *rand.Rand) *DeviceFingerprint {
	profile := deviceProfiles[rng.Intn(len(deviceProfiles))]
	return &DeviceFingerprint{
		DeviceId:    generateDeviceId(rng),
		Os:          "android",
		OsVer:       profile.osVer,
		AppVer:      appVersion,
		VersionCode: appVersionCode,
		Channel:     appChannel,
		MobileName:  profile.modelName,
		Resolution:  profile.resolution,
		BuildVer:    fmt.Sprintf("%d", time.Now().Unix()),
	}
}

// generateDeviceId generates a device ID in the format used by Android NetEase client.
// Format mirrors the real client: two UUIDs joined by "|" and URL-encoded.
func generateDeviceId(rng *rand.Rand) string {
	uuid1 := randomUUID(rng)
	uuid2 := randomUUID(rng)
	return url.QueryEscape(uuid1 + "|" + uuid2)
}

// randomUUID generates a v4-style UUID string.
func randomUUID(rng *rand.Rand) string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// injectDeviceCookies sets the device fingerprint cookies on the API client,
// matching the cookies that the real Android client sends on every request.
func injectDeviceCookies(cli *api.Client, fp *DeviceFingerprint) {
	musicURL, _ := url.Parse("https://music.163.com")
	cookies := []*http.Cookie{
		{Name: "deviceId", Value: fp.DeviceId, Domain: ".music.163.com", Path: "/"},
		{Name: "os", Value: fp.Os, Domain: ".music.163.com", Path: "/"},
		{Name: "osver", Value: fp.OsVer, Domain: ".music.163.com", Path: "/"},
		{Name: "appver", Value: fp.AppVer, Domain: ".music.163.com", Path: "/"},
		{Name: "versioncode", Value: fp.VersionCode, Domain: ".music.163.com", Path: "/"},
		{Name: "channel", Value: fp.Channel, Domain: ".music.163.com", Path: "/"},
		{Name: "mobilename", Value: fp.MobileName, Domain: ".music.163.com", Path: "/"},
		{Name: "resolution", Value: fp.Resolution, Domain: ".music.163.com", Path: "/"},
		{Name: "buildver", Value: fp.BuildVer, Domain: ".music.163.com", Path: "/"},
	}
	cli.SetCookies(musicURL, cookies)
}

// deviceProfile 表示一个真实存在的设备档案。
// 型号、系统版本、分辨率三者绑定，不允许随机拼凑，避免出现不可能的组合。
type deviceProfile struct {
	modelName  string // Android Build.MODEL 值
	osVer      string // Android 系统版本号 (如 "14", "15")
	resolution string // 屏幕分辨率 宽x高 (竖屏方向)
}

// deviceProfiles 中国大陆市场主流安卓手机真实设备档案库（50款）
// 覆盖 2023~2026 年间的主流机型，每条记录均为该机型的真实参数
// 覆盖品牌：小米、Redmi、华为、荣耀、vivo、iQOO、OPPO、一加、realme、三星
var deviceProfiles = []deviceProfile{
	// ========================
	// 小米 (Xiaomi) — 2024~2026
	// ========================
	{"Xiaomi 17 Pro Max", "15", "1200x2608"},  // 2026 骁龙8至尊版 6.9"
	{"Xiaomi 17 Pro", "15", "1440x3200"},      // 2026 旗舰Pro 6.73"
	{"Xiaomi 17", "15", "1200x2670"},          // 2026 旗舰 6.36"
	{"Xiaomi 16 Ultra", "15", "1440x3200"},    // 2025 影像旗舰 6.73"
	{"Xiaomi 15 Ultra", "15", "1440x3200"},    // 2025 影像旗舰 6.73"
	{"Xiaomi 15 Pro", "15", "1440x3200"},      // 2024 旗舰Pro 6.73"
	{"Xiaomi 15", "15", "1200x2670"},          // 2024 旗舰 6.36"
	{"Xiaomi 14 Pro", "14", "1440x3200"},      // 2023 旗舰Pro 6.73"
	{"Xiaomi 14", "14", "1200x2670"},          // 2023 旗舰 6.36"
	{"Xiaomi MIX Fold 4", "14", "1440x3200"},  // 2024 折叠屏 内屏

	// ========================
	// Redmi — 2023~2025
	// ========================
	{"Redmi K80 Pro", "15", "1440x3200"},      // 2024 性能旗舰 6.67" 2K
	{"Redmi K80", "15", "1220x2712"},          // 2024 性能旗舰 6.67" 1.5K
	{"Redmi K70 Pro", "14", "1440x3200"},      // 2023 性能旗舰 6.67" 2K
	{"Redmi K70", "14", "1220x2712"},          // 2023 性能旗舰 6.67" 1.5K
	{"Redmi Note 14 Pro+", "14", "1220x2712"}, // 2024 中端 6.67" 1.5K
	{"Redmi Note 13 Pro+", "14", "1080x2400"}, // 2023 中端 6.67" FHD+
	{"Redmi Turbo 4", "15", "1220x2712"},      // 2025 性能中端 6.67" 1.5K

	// ========================
	// 华为 (HUAWEI) — 2023~2025
	// ========================
	{"HUAWEI Pura 70 Pro+", "14", "1260x2844"}, // 2024 旗舰 6.8"
	{"HUAWEI Pura 70 Pro", "14", "1260x2844"},   // 2024 旗舰 6.8"
	{"HUAWEI Pura 70", "14", "1256x2760"},       // 2024 旗舰 6.6"
	{"HUAWEI Mate 60 Pro", "14", "1260x2720"},   // 2023 旗舰 6.82"
	{"HUAWEI Mate 60", "14", "1080x2400"},       // 2023 旗舰 6.69"
	{"HUAWEI nova 12 Pro", "14", "1200x2670"},   // 2024 中端 6.7"

	// ========================
	// 荣耀 (HONOR) — 2023~2025
	// ========================
	{"HONOR Magic7 Pro", "15", "1280x2800"},   // 2024 旗舰 6.8"
	{"HONOR Magic6 Pro", "14", "1280x2800"},   // 2023 旗舰 6.78"
	{"HONOR 200 Pro", "14", "1200x2664"},      // 2024 中高端 6.78"
	{"HONOR X60", "14", "1080x2412"},          // 2024 中端 6.7"

	// ========================
	// vivo — 2023~2025
	// ========================
	{"vivo X200 Pro", "15", "1440x3200"},      // 2024 旗舰 6.78" 2K
	{"vivo X200", "15", "1260x2800"},          // 2024 旗舰 6.67" 1.5K
	{"vivo X100 Pro", "14", "1440x3200"},      // 2023 旗舰 6.78" 2K
	{"vivo X100", "14", "1260x2800"},          // 2023 旗舰 6.67" 1.5K
	{"vivo S19 Pro", "14", "1260x2800"},       // 2024 中端 6.78" 1.5K
	{"vivo Y200 GT", "14", "1080x2400"},       // 2024 入门 6.78" FHD+

	// ========================
	// iQOO — 2024~2025
	// ========================
	{"iQOO 13", "15", "1440x3200"},            // 2024 性能旗舰 6.82" 2K
	{"iQOO Neo9 Pro", "14", "1260x2800"},      // 2024 中高端 6.78" 1.5K
	{"iQOO Z9 Turbo", "14", "1260x2800"},      // 2024 中端 6.78" 1.5K

	// ========================
	// OPPO — 2023~2025
	// ========================
	{"OPPO Find X8 Pro", "15", "1264x2780"},   // 2024 旗舰 6.78"
	{"OPPO Find X8", "15", "1264x2780"},       // 2024 旗舰 6.59"
	{"OPPO Find X7 Ultra", "14", "1440x3168"}, // 2024 影像旗舰 6.82" 2K
	{"OPPO Reno12 Pro", "14", "1264x2780"},    // 2024 中高端 6.7"
	{"OPPO A3 Pro", "14", "1080x2412"},        // 2024 中端 6.7"

	// ========================
	// 一加 (OnePlus) — 2023~2025
	// ========================
	{"OnePlus 13", "15", "1440x3168"},         // 2024 旗舰 6.82" 2K
	{"OnePlus 12", "14", "1440x3168"},         // 2023 旗舰 6.82" 2K
	{"OnePlus Ace 5 Pro", "15", "1264x2780"},  // 2025 性能中端 6.78"

	// ========================
	// realme — 2024~2025
	// ========================
	{"realme GT7 Pro", "15", "1264x2780"},     // 2024 旗舰 6.78"
	{"realme GT Neo6", "14", "1264x2780"},     // 2024 中高端 6.78"

	// ========================
	// 三星 (Samsung) 国行 — 2024~2025
	// ========================
	{"SM-S9380", "15", "1440x3120"},           // Galaxy S25 Ultra 国行 6.9"
	{"SM-S9280", "15", "1440x3120"},           // Galaxy S24 Ultra 国行 6.8"
	{"SM-S9210", "14", "1080x2340"},           // Galaxy S24 国行 6.2"
}

// formatDeviceInfo returns a human-readable summary of the device fingerprint for logging.
func formatDeviceInfo(fp *DeviceFingerprint) string {
	parts := []string{
		fmt.Sprintf("设备=%s", fp.MobileName),
		fmt.Sprintf("系统=Android %s", fp.OsVer),
		fmt.Sprintf("版本=%s", fp.AppVer),
		fmt.Sprintf("渠道=%s", fp.Channel),
	}
	// show only first 16 chars of deviceId for brevity
	did := fp.DeviceId
	if len(did) > 16 {
		did = did[:16] + "..."
	}
	parts = append(parts, fmt.Sprintf("deviceId=%s", did))
	return strings.Join(parts, ", ")
}
