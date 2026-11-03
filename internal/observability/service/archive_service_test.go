package service

import (
	"encoding/json"
	"strings"
	"testing"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
)

// TestRawOrNull 校验 JSONB 字节 → json.RawMessage 的转换：空/NULL 输出合法 JSON null，非空保留原始 JSON。
func TestRawOrNull(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{name: "nil", in: nil, want: "null"},
		{name: "empty", in: []byte{}, want: "null"},
		{name: "object", in: []byte(`{"a":1}`), want: `{"a":1}`},
		{name: "string", in: []byte(`"x"`), want: `"x"`},
		{name: "array", in: []byte(`[1,2]`), want: `[1,2]`},
	}
	for _, c := range cases {
		got := rawOrNull(c.in)
		if string(got) != c.want {
			t.Errorf("%s: rawOrNull(%q)=%q, want %q", c.name, string(c.in), string(got), c.want)
		}
		var v interface{}
		if err := json.Unmarshal(got, &v); err != nil {
			t.Errorf("%s: rawOrNull result is not valid JSON: %v", c.name, err)
		}
	}
}

// TestScrubArchiveNUL 校验 \u0000 → \ufffd 转义替换，与 TraceWriter 写入侧一致。
func TestScrubArchiveNUL(t *testing.T) {
	in := []byte(`{"body":"a\u0000b"}`)
	out := scrubArchiveNUL(in)
	if got := string(out); got != `{"body":"a\ufffdb"}` {
		t.Errorf("scrubArchiveNUL(%q)=%q, want %q", string(in), got, `{"body":"a\ufffdb"}`)
	}
	// 无 NUL 时应原样返回（同一 slice）。
	plain := []byte(`{"body":"ok"}`)
	if got := string(scrubArchiveNUL(plain)); got != string(plain) {
		t.Errorf("scrubArchiveNUL should be no-op for plain input: got %q", got)
	}
}

// TestArchiveSettings_Defaults 校验「注入 Config.Archive（§2 默认值）→ 运行时 archiveSettings」的映射。
// v3 关键点：开关拆分为 write/read 两态，读路径不依赖写开关（N10）。
func TestArchiveSettings_Defaults(t *testing.T) {
	cfg := cpconfig.ArchiveConfig{
		WriteEnabled:          true,
		ReadEnabled:           true,
		Root:                  "/var/lib/llm-gateway/archive",
		TZ:                    "Asia/Shanghai",
		HotWindowDays:         30,
		BatchRows:             3000,
		MaxRetry:              5,
		ClosureLagHours:       24,
		CronSchedule:          "0 2 * * *",
		StagingRetentionHours: 336,
		RequireOffhostCopy:    true,
		ChunkMB:               256,
		ZstdLevel:             "best",
		ZstdWindowMB:          128,
		EncoderConcurrency:    1,
		MaxConcurrentReads:    4,
		VerifySampleRows:      20,
		InspectFreshnessHours: 168,
		InspectPerCycle:       3,
		RetentionDays:         180,
	}

	got := loadArchiveSettings(cfg)
	if !got.writeEnabled {
		t.Error("writeEnabled should be true")
	}
	if !got.readEnabled {
		t.Error("readEnabled 必须独立于 writeEnabled 生效（N10）")
	}
	if got.root != cfg.Root {
		t.Errorf("root=%q, want %q", got.root, cfg.Root)
	}
	if got.tz != "Asia/Shanghai" {
		t.Errorf("tz=%q, want Asia/Shanghai", got.tz)
	}
	if got.hotWindowDays != 30 {
		t.Errorf("hotWindowDays=%d, want 30", got.hotWindowDays)
	}
	if got.batchRows != 3000 {
		t.Errorf("batchRows=%d, want 3000", got.batchRows)
	}
	if got.maxRetry != 5 {
		t.Errorf("maxRetry=%d, want 5", got.maxRetry)
	}
	if got.closureLagHours != 24 {
		t.Errorf("closureLagHours=%d, want 24", got.closureLagHours)
	}
	if got.schedule != "0 2 * * *" {
		t.Errorf("schedule=%q, want 0 2 * * *", got.schedule)
	}
	if got.stagingRetentionH != 336 {
		t.Errorf("stagingRetentionH=%d, want 336", got.stagingRetentionH)
	}
	if !got.requireOffhostCopy {
		t.Error("requireOffhostCopy should be true (hard gate)")
	}
	if got.chunkMB != 256 {
		t.Errorf("chunkMB=%d, want 256", got.chunkMB)
	}
	if got.zstdLevel != "best" {
		t.Errorf("zstdLevel=%q, want best", got.zstdLevel)
	}
	if got.zstdWindowMB != 128 {
		t.Errorf("zstdWindowMB=%d, want 128（不可省，见 N9）", got.zstdWindowMB)
	}
	if got.encoderConcurrency != 1 {
		t.Errorf("encoderConcurrency=%d, want 1", got.encoderConcurrency)
	}
	if got.maxConcurrentReads != 4 {
		t.Errorf("maxConcurrentReads=%d, want 4", got.maxConcurrentReads)
	}
	if got.verifySampleRows != 20 {
		t.Errorf("verifySampleRows=%d, want 20", got.verifySampleRows)
	}
	if got.inspectFreshnessHours != 168 {
		t.Errorf("inspectFreshnessHours=%d, want 168", got.inspectFreshnessHours)
	}
	if got.inspectPerCycle != 3 {
		t.Errorf("inspectPerCycle=%d, want 3", got.inspectPerCycle)
	}
	if got.retentionDays != 180 {
		t.Errorf("retentionDays=%d, want 180", got.retentionDays)
	}
}

// TestArchiveSettings_ZeroValueDefaults 校验零值配置下的兜底默认（chunk/window/level/并发/批量）。
// 这些默认值直接决定压缩率与内存占用，缺省退化必须显式可见。
func TestArchiveSettings_ZeroValueDefaults(t *testing.T) {
	got := loadArchiveSettings(cpconfig.ArchiveConfig{})
	if got.chunkMB != archiveDefaultChunkMB {
		t.Errorf("chunkMB=%d want %d", got.chunkMB, archiveDefaultChunkMB)
	}
	if got.zstdWindowMB != archiveDefaultZstdWindowMB {
		t.Errorf("zstdWindowMB=%d want %d（漏配会让压缩率静默劣化）", got.zstdWindowMB, archiveDefaultZstdWindowMB)
	}
	if got.encoderConcurrency != archiveDefaultEncoderConcurrency {
		t.Errorf("encoderConcurrency=%d want %d", got.encoderConcurrency, archiveDefaultEncoderConcurrency)
	}
	if got.batchRows != archiveDefaultBatchRows {
		t.Errorf("batchRows=%d want %d", got.batchRows, archiveDefaultBatchRows)
	}
	if got.zstdLevel != "best" {
		t.Errorf("zstdLevel=%q want best", got.zstdLevel)
	}
	if got.writeEnabled || got.readEnabled {
		t.Error("零值配置下 write/read 都必须关闭")
	}
}

// TestArchiveSettings_FromConfig 校验自定义 Config.Archive 值正确映射到运行时设置。
// 特别覆盖「write=false, read=true」（N10：只读点读必须可选可用）。
func TestArchiveSettings_FromConfig(t *testing.T) {
	cfg := cpconfig.ArchiveConfig{
		WriteEnabled:         false,
		ReadEnabled:          true,
		Root:                 "/data/archive",
		TZ:                   "UTC",
		HotWindowDays:        7,
		MaxRetry:             3,
		RequireOffhostCopy:   false,
		OffhostCopyMarker:    "/backup/ok.marker",
		DiskWatermarkPercent: 75,
		MaxDiskGB:            8,
		MaxConcurrentReads:   2,
		ZstdWindowMB:         64,
	}
	got := loadArchiveSettings(cfg)
	if got.writeEnabled {
		t.Error("writeEnabled should be false")
	}
	if !got.readEnabled {
		t.Error("readEnabled should be true（write=false, read=true 仍必须能点读）")
	}
	if got.root != "/data/archive" {
		t.Errorf("root=%q, want /data/archive", got.root)
	}
	if got.tz != "UTC" {
		t.Errorf("tz=%q, want UTC", got.tz)
	}
	if got.hotWindowDays != 7 {
		t.Errorf("hotWindowDays=%d, want 7", got.hotWindowDays)
	}
	if got.maxRetry != 3 {
		t.Errorf("maxRetry=%d, want 3", got.maxRetry)
	}
	if got.requireOffhostCopy {
		t.Error("requireOffhostCopy should be false")
	}
	if got.offhostMarker != "/backup/ok.marker" {
		t.Errorf("offhostMarker=%q, want /backup/ok.marker", got.offhostMarker)
	}
	if got.diskWatermarkPct != 75 {
		t.Errorf("diskWatermarkPct=%d, want 75", got.diskWatermarkPct)
	}
	if got.maxDiskGB != 8 {
		t.Errorf("maxDiskGB=%d, want 8", got.maxDiskGB)
	}
	if got.maxConcurrentReads != 2 {
		t.Errorf("maxConcurrentReads=%d, want 2", got.maxConcurrentReads)
	}
	if got.zstdWindowMB != 64 {
		t.Errorf("zstdWindowMB=%d, want 64", got.zstdWindowMB)
	}
}

// TestDiskGuardDecision 校验磁盘守卫纯决策函数：
//   - 水位（百分比）只告警不阻断（maxGB=0 时永不 block）。
//   - maxGB>0 时：超 archive 大小上限或剩余 < 日均体积 2 倍 → block。
//   - totalB=0（statfs 异常/0 块设备）→ 放行（usedPercent 0、blocked false）。
func TestDiskGuardDecision(t *testing.T) {
	const (
		gib = int64(1) << 30
	)
	cases := []struct {
		name       string
		total, av  uint64
		archiveB   int64
		days       int
		maxGB      int
		wantPct    float64
		checkPct   bool
		wantBlock  bool
		wantReason string // 子串；空表示不断言
	}{
		{name: "used 80% 不阻断 (maxGB=0)", total: 1000, av: 200, archiveB: 0, days: 0, maxGB: 0, wantPct: 80, checkPct: true, wantBlock: false},
		{name: "used 90% 只告警不阻断 (maxGB=0)", total: 1000, av: 100, archiveB: 0, days: 0, maxGB: 0, wantPct: 90, checkPct: true, wantBlock: false},
		{name: "maxGB=0 不限永不 block", total: 1000, av: 1, archiveB: 100 << 30, days: 10, maxGB: 0, wantPct: 99.9, checkPct: true, wantBlock: false},
		{name: "archive 大小达上限 → block", total: 1000 << 30, av: 500 << 30, archiveB: 5 * gib, days: 5, maxGB: 5, wantBlock: true, wantReason: "max_disk_gb"},
		{name: "剩余 < 日均体积 2 倍 → block", total: 1000 << 30, av: uint64(3 * gib), archiveB: 4 * gib, days: 2, maxGB: 5, wantBlock: true, wantReason: "2x daily"},
		{name: "剩余充足 → 不 block", total: 1000 << 30, av: uint64(5 * gib), archiveB: 4 * gib, days: 2, maxGB: 5, wantBlock: false},
		{name: "total=0 → 放行", total: 0, av: 0, archiveB: 0, days: 0, maxGB: 0, wantPct: 0, checkPct: true, wantBlock: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			used, blocked, reason := diskGuardDecision(c.total, c.av, c.archiveB, c.days, c.maxGB)
			if c.checkPct && (used < c.wantPct-0.01 || used > c.wantPct+0.01) {
				t.Errorf("usedPercent=%v, want %v", used, c.wantPct)
			}
			if blocked != c.wantBlock {
				t.Errorf("blocked=%v, want %v (reason=%q)", blocked, c.wantBlock, reason)
			}
			if c.wantReason != "" && !strings.Contains(reason, c.wantReason) {
				t.Errorf("reason=%q, want contains %q", reason, c.wantReason)
			}
		})
	}
}
