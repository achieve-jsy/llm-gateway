package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func newTraceViper() *viper.Viper {
	v := viper.New()
	v.SetDefault("trace_body.block_mb", int64(1536))
	v.SetDefault("trace_body.flush_interval", "1m")
	v.SetDefault("trace_body.roll_interval", "1h")
	v.SetDefault("trace_body.disk_watermark_percent", 90)
	return v
}

// traceViperEnabled 返回一份「合法且已开启」的输入，供各条**隔离探针**只改动
// 自己关心的那一个字段 —— 探针的判据是「只含该条校验的输入」，否则表驱动/共享
// 断言环里更早的条目会遮蔽（全局约束的证据制度第 3 条）。
func traceViperEnabled() *viper.Viper {
	v := newTraceViper()
	v.Set("trace_body.root", "/datalog/trace-archive-v4")
	v.Set("trace_body.inst", "blue")
	return v
}

// TestTraceBodyConfigDisabledByDefault 是**安全前提**的测试：
// 未设 ROOT ⇒ 功能关闭 ⇒ 网关走既有路径（正文仍写 PG）。
// 若把默认值改成开启，挂载到位前的部署会静默丢掉所有正文。
func TestTraceBodyConfigDisabledByDefault(t *testing.T) {
	cfg, err := config.LoadTraceBodyConfig(newTraceViper())
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.Empty(t, cfg.Root)
	require.Equal(t, int64(1536), cfg.BlockMB)
	require.Equal(t, time.Minute, cfg.FlushInterval)
	require.Equal(t, time.Hour, cfg.RollInterval)
	require.Equal(t, 90, cfg.DiskWatermarkPercent)
}

// TestTraceBodyConfigRollIntervalMustBeUnderOneHour 固定 R8 的上界与
// 「严格小于 sealer 的 minAge」这一条：1h 是默认值，配置允许更小但不允许更大。
//
// ⚠️ 第二条断言（3h）在 1h 上界在位时**不可能失败**：`3h >= 3h` 蕴含 `3h > 1h`，
// 更早的上界守卫先报错。它因此不是上界守卫的证据，只为「上界若被放宽，3h 仍须被拒」
// 留下意图记录；minAge 守卫的可达性分析见 tools/trace-mutate/sets/p2-task-2.py 的 m03。
func TestTraceBodyConfigRollIntervalMustBeUnderOneHour(t *testing.T) {
	v := newTraceViper()
	v.Set("trace_body.root", "/datalog/trace-archive-v4")
	v.Set("trace_body.inst", "blue")
	v.Set("trace_body.roll_interval", "2h")
	_, err := config.LoadTraceBodyConfig(v)
	require.Error(t, err, "滚动间隔 > 1h 必须被拒绝（规格 §15.1 R8）")

	v.Set("trace_body.roll_interval", "3h")
	_, err = config.LoadTraceBodyConfig(v)
	require.Error(t, err, "滚动间隔 >= minAge(3h) 必须被拒绝")
}

// TestTraceBodyConfigInstIsWhitelisted 固定 §5.1 的目录布局：inst ∈ {blue,green}。
// 空串（开启而未设）也必须被拒绝 —— inst 是路径分量，空值会让写入落到无法隔离的目录。
func TestTraceBodyConfigInstIsWhitelisted(t *testing.T) {
	v := newTraceViper()
	v.Set("trace_body.root", "/datalog/trace-archive-v4")
	for _, bad := range []string{"", "red", "Blue", "blue/../green"} {
		v.Set("trace_body.inst", bad)
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "inst=%q 必须被拒绝", bad)
	}
	v.Set("trace_body.inst", "green")
	cfg, err := config.LoadTraceBodyConfig(v)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, "green", cfg.Inst)
}

// TestTraceBodyConfigRootMustBeAbsolute 固定 root 的形态校验：
// 相对路径的含义取决于进程工作目录（容器里是 /app 还是 / 由镜像决定），
// 而 /datalog 是要被 bind mount 的绝对路径 ⇒ 静默接受会让正文落到无法预测的位置。
func TestTraceBodyConfigRootMustBeAbsolute(t *testing.T) {
	for _, bad := range []string{"datalog/trace-archive-v4", "./datalog", "../datalog", "datalog/"} {
		v := newTraceViper()
		v.Set("trace_body.root", bad)
		v.Set("trace_body.inst", "blue")
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "相对路径 root=%q 必须被拒绝", bad)
	}
}

// TestTraceBodyConfigRootRejectsFilesystemRoot 固定「不得是 /」：
// "/" 能通过「是个目录」的检查，于是 spool 会建在宿主根文件系统上（/spool/dt=…）。
// 四条输入在 filepath.Clean 之后都等于 "/"（或 "//" 归一后仍是根）。
func TestTraceBodyConfigRootRejectsFilesystemRoot(t *testing.T) {
	for _, bad := range []string{"/", "//", "/./", "/tmp/.."} {
		v := newTraceViper()
		v.Set("trace_body.root", bad)
		v.Set("trace_body.inst", "blue")
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "root=%q（Clean 后为 /）必须被拒绝", bad)
	}
}

// TestTraceBodyConfigWhitespaceRootIsDisabled 固定「trim 后为空 = 关闭」：
// TRACE_BODY_ROOT=" " 不能被当成开启（否则会走一条 root 为空白字符的路径，
// 失败方式难以诊断），且 Root 必须是 trim 之后的值。
func TestTraceBodyConfigWhitespaceRootIsDisabled(t *testing.T) {
	v := newTraceViper()
	v.Set("trace_body.root", "   ")
	cfg, err := config.LoadTraceBodyConfig(v)
	require.NoError(t, err)
	require.False(t, cfg.Enabled, "trim 后为空必须等同于未设置")
	require.Empty(t, cfg.Root, "Root 必须是 TrimSpace 之后的值")
}

// TestTraceBodyConfigFlushMustBeFarBelowRoll 是「flush 必须远小于滚动间隔」的**隔离探针**：
// 输入只违反这一条，其余字段全合法。
//
// ⚠️ flush 取 **1m**（而不是 1h）是刻意的：`flush ≤ 1m` 的上界现在也是承重守卫，
// 若探针写 flush=1h，那条上界会**先**报错 ⇒ 删掉 `flush >= roll` 守卫后探针照样红，
// 于是 m04 的变异会被遮蔽成假绿（全局约束的证据制度第 3 条的同型问题：
// 遮蔽方是另一条更早的守卫）。取 1m 正好落在上界内，只有 `flush >= roll` 能拦它。
func TestTraceBodyConfigFlushMustBeFarBelowRoll(t *testing.T) {
	v := traceViperEnabled()
	v.Set("trace_body.flush_interval", "1m")
	v.Set("trace_body.roll_interval", "1m")
	_, err := config.LoadTraceBodyConfig(v)
	require.Error(t, err, "flush 周期等于滚动间隔必须被拒绝（规格 §7.1：sealer 只能看到已 write(2) 的字节）")
}

// TestTraceBodyConfigFlushIntervalMustBeAtMostOneMinute 固定 flush 周期的**上界 ≤ 1m**
// （规格 §7.1 与计划全局约束：周期 flush 不设前置条件、必须无条件执行）。
// 只写进文档而不强制时，`TRACE_BODY_FLUSH_INTERVAL=5m` 是完全合法的配置，
// 而「块已停止追加但字节仍在缓冲」的窗口被拉长到 5 分钟 ⇒ 声明的不变量是空的。
func TestTraceBodyConfigFlushIntervalMustBeAtMostOneMinute(t *testing.T) {
	for _, bad := range []string{"61s", "5m"} {
		v := traceViperEnabled()
		v.Set("trace_body.flush_interval", bad)
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "flush_interval=%s 超过 1m 上界必须被拒绝（roll=1h 本身合法）", bad)
	}
	// 边界值本身必须被接受，否则上界会被"收紧"成 < 1m 而无人察觉
	v := traceViperEnabled()
	v.Set("trace_body.flush_interval", "1m")
	v.Set("trace_body.roll_interval", "1h")
	_, err := config.LoadTraceBodyConfig(v)
	require.NoError(t, err, "flush_interval=1m 必须被接受（规格 §7.1 的上界是 ≤ 1m）")
}

// TestTraceBodyConfigWatermarkRange 是水位范围的**隔离探针**：0/100 的后果极端
// （0 会让每个请求都降级，100 等于不设水位），且极值也能被拦住。
func TestTraceBodyConfigWatermarkRange(t *testing.T) {
	for _, bad := range []int{-1, 0, 100, 101} {
		v := traceViperEnabled()
		v.Set("trace_body.disk_watermark_percent", bad)
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "水位 %d 必须被拒绝（合法区间 1..99）", bad)
	}
	for _, good := range []int{1, 99} {
		v := traceViperEnabled()
		v.Set("trace_body.disk_watermark_percent", good)
		_, err := config.LoadTraceBodyConfig(v)
		require.NoError(t, err, "水位 %d 必须被接受", good)
	}
}

// TestTraceBodyConfigBlockMBMustBePositive 是块大小的**隔离探针**：
// block_mb=0 会让每个块一建即达阈值（或按实现直接退化），必须是配置错误。
func TestTraceBodyConfigBlockMBMustBePositive(t *testing.T) {
	v := traceViperEnabled()
	v.Set("trace_body.block_mb", 0)
	_, err := config.LoadTraceBodyConfig(v)
	require.Error(t, err, "block_mb=0 必须被拒绝")
}

// TestTraceBodyConfigFlushIntervalMustBePositive 是 flush 周期的**隔离探针**：
// 0/负值会让周期 flush 永不发生（sealer 只能看到已 write(2) 的字节）。
func TestTraceBodyConfigFlushIntervalMustBePositive(t *testing.T) {
	for _, bad := range []string{"0s", "-1m"} {
		v := traceViperEnabled()
		v.Set("trace_body.flush_interval", bad)
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "flush_interval=%s 必须被拒绝", bad)
	}
}

// TestTraceBodyConfigRollIntervalMustBePositive 是滚动间隔的**隔离探针**：
// 非正值必须被拒绝。注意它是**行为**断言而非「哪条守卫报错」的断言 ——
// 在 flush>0 的前提下，`roll <= 0` 必然同时命中 flush>=roll，所以本用例
// 既不能证明也不能否证 roll<=0 那条守卫承重（分析见集合里的 m16）。
func TestTraceBodyConfigRollIntervalMustBePositive(t *testing.T) {
	for _, bad := range []string{"0s", "-1h"} {
		v := traceViperEnabled()
		v.Set("trace_body.roll_interval", bad)
		_, err := config.LoadTraceBodyConfig(v)
		require.Error(t, err, "roll_interval=%s 必须被拒绝", bad)
	}
}

// TestDayStartIsUTCMidnightOfTheCSTDay 固定「唯一日期入口」的两个性质：
// ① DayOf(ts) 必须等于 DayStart(ts) 的日期（同源，不得各算各的）；
// ② DayStart 必须是**该日历日的 UTC 零点**，不是 +08:00 的零点
//
//	—— 后者写进 PG date 列会在 UTC 会话下差一天。
func TestDayStartIsUTCMidnightOfTheCSTDay(t *testing.T) {
	cfg := config.TraceBodyConfig{Root: "/x", Inst: "blue"}
	// 北京 2026-09-25 00:30 = UTC 2026-09-24 16:30（跨日边界，最易暴露口径不一致）
	ts := time.Date(2026, 9, 24, 16, 30, 0, 0, time.UTC)

	start := cfg.DayStart(ts)
	require.Equal(t, "2026-09-25", start.Format("2006-01-02"), "DayStart 必须是北京 9-25 那一日")
	require.Equal(t, time.UTC, start.Location(), "必须是 UTC 零点（+08:00 零点会在 UTC 会话下差一天）")
	require.Equal(t, 0, start.Hour())
	require.Equal(t, 0, start.Minute())
	require.Equal(t, "2026-09-25", cfg.DayOf(ts), "DayOf 必须与 DayStart 同源")
}

// TestDayOfUsesUTCPlus8 pin 死 dt 口径。生产库 TimeZone=UTC、容器无 TZ，
// 因此时区**不能**依赖 time.Local；固定 +08:00 且跨日边界必须正确。
func TestDayOfUsesUTCPlus8(t *testing.T) {
	cfg := config.TraceBodyConfig{Root: "/x", Inst: "blue"}
	cases := []struct {
		name string
		ts   time.Time
		want string
	}{
		{"北京 23:59:59 仍属当天", time.Date(2026, 9, 24, 23, 59, 59, 0, config.TraceBodyZone), "2026-09-24"},
		{"北京 00:00:00 进入次日", time.Date(2026, 9, 25, 0, 0, 0, 0, config.TraceBodyZone), "2026-09-25"},
		{"UTC 16:00 已是北京次日", time.Date(2026, 9, 24, 16, 0, 0, 0, time.UTC), "2026-09-25"},
		{"UTC 15:59:59 仍是北京当天", time.Date(2026, 9, 24, 15, 59, 59, 0, time.UTC), "2026-09-24"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, cfg.DayOf(c.ts))
		})
	}
}

// mustLoadTraceBody 调 config.Load() 并把「启动路径不得 panic」变成**断言级**失败：
// 若某个变异把 Enabled 强制为 true，Load 会在空 root 上 panic；不用 NotPanics 包住
// 就会让 panic 直接终止测试二进制，整轮只剩「崩溃判红」而丢掉逐条终判。
func mustLoadTraceBody(t *testing.T) config.TraceBodyConfig {
	t.Helper()
	var tb config.TraceBodyConfig
	require.NotPanics(t, func() { tb = config.Load().TraceBody })
	return tb
}

// clearTraceBodyEnv 把 6 个 TRACE_BODY_* 环境变量**真正移除**（不是设为空串），
// 并在用例结束后还原。理由：
//   - 本用例要断言的是**产品侧默认值**，只有「环境里没有这个变量」才会走 SetDefault；
//   - `t.Setenv(k, "")` 对字符串键与缺省同义，但对 GetInt64/GetDuration 这类键，
//     空串会被 viper 的 AutomaticEnv 取到并 cast 成 0 ⇒ 变成「配置错误」而非默认值，
//     所以数值键必须用 Unsetenv；
//   - 开发机可能为本地试跑 export 过这些变量，不清干净就会误红/误绿。
func clearTraceBodyEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"TRACE_BODY_ROOT", "TRACE_BODY_INST", "TRACE_BODY_BLOCK_MB",
		"TRACE_BODY_FLUSH_INTERVAL", "TRACE_BODY_ROLL_INTERVAL",
		"TRACE_BODY_DISK_WATERMARK_PERCENT",
	}
	for _, k := range keys {
		if old, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { _ = os.Setenv(k, old) })
			require.NoError(t, os.Unsetenv(k))
		}
	}
}

// TestLoadWiresTraceBodyFromEnvVerbatim 钉住「环境变量名与规格 §11.1 逐字一致」这条
// **无法由 trace_body.go 的变异覆盖**的要求（Viper 键名住在 config.go 里）：
// 键必须是顶层 `trace_body.*`（放进 observability.* 会得到 OBSERVABILITY_TRACE_BODY_ROOT），
// 且 config.Load() 必须把解析结果装配进 Config.TraceBody；非法 root 必须在
// 启动路径上响亮失败（Load 的签名不返回 error，故以 panic 表达）。
//
// 它同时是**唯一**走真实 Load() 的用例，因此也是唯一能约束 `config.go` 里
// `SetDefault` 的地方：newTraceViper() 里那套默认值是测试自己的 helper，
// 对「产品默认值漂移」没有约束力（例如把 block_mb 默认改成 1024、水位改成 80，
// 套件仍会全绿）⇒ 下面 4 条 require.Equal 必须打在 Load() 的结果上。
func TestLoadWiresTraceBodyFromEnvVerbatim(t *testing.T) {
	t.Setenv("APP_ENV", "nonexistent-trace-body-config-test")
	clearTraceBodyEnv(t)

	// 阶段一：产品默认值（环境里没有 TRACE_BODY_* ⇒ 只能来自 config.go 的 SetDefault）
	tbDefault := mustLoadTraceBody(t)
	require.False(t, tbDefault.Enabled, "未设 TRACE_BODY_ROOT ⇒ 默认关闭")
	require.Empty(t, tbDefault.Root)
	require.Empty(t, tbDefault.Inst)
	require.Equal(t, int64(1536), tbDefault.BlockMB, "block_mb 的产品默认值必须是 1536")
	require.Equal(t, time.Minute, tbDefault.FlushInterval, "flush_interval 的产品默认值必须是 1m")
	require.Equal(t, time.Hour, tbDefault.RollInterval, "roll_interval 的产品默认值必须是 1h")
	require.Equal(t, 90, tbDefault.DiskWatermarkPercent, "disk_watermark_percent 的产品默认值必须是 90")

	// 阶段二：环境变量逐字生效
	t.Setenv("TRACE_BODY_ROOT", "/datalog/trace-archive-v4")
	t.Setenv("TRACE_BODY_INST", "blue")
	t.Setenv("TRACE_BODY_BLOCK_MB", "64")
	t.Setenv("TRACE_BODY_FLUSH_INTERVAL", "30s")
	t.Setenv("TRACE_BODY_ROLL_INTERVAL", "30m")
	t.Setenv("TRACE_BODY_DISK_WATERMARK_PERCENT", "85")
	tb := mustLoadTraceBody(t)
	require.True(t, tb.Enabled)
	require.Equal(t, "/datalog/trace-archive-v4", tb.Root)
	require.Equal(t, "blue", tb.Inst)
	require.Equal(t, int64(64), tb.BlockMB)
	require.Equal(t, 30*time.Second, tb.FlushInterval)
	require.Equal(t, 30*time.Minute, tb.RollInterval)
	require.Equal(t, 85, tb.DiskWatermarkPercent)

	// 阶段三：非法 root ⇒ 启动失败（响亮，不是静默退化）
	t.Setenv("TRACE_BODY_ROOT", "datalog/trace-archive-v4")
	require.Panics(t, func() { config.Load() }, "非法 TRACE_BODY_ROOT 必须在启动路径上失败")
}
