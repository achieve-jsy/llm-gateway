package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// TraceBodyZone 是 dt 口径使用的时区：固定 UTC+8。
//
// 【为什么不用 time.LoadLocation("Asia/Shanghai")】网关容器里没有 tzdata，
// LoadLocation 会返回错误；而中国自 1991 年起不再使用夏令时，
// 固定 +08:00 与真实时区在任何我们关心的日期上都等价。
//
// 【为什么不用 time.Local】生产 compose 未设 TZ（容器是 UTC），
// 依赖 time.Local 会让 dt 随宿主机/容器环境漂移 —— 而 dt 是**目录名**，
// 漂移会让同一 request_id 的正文落在另一个 dt 下，按 dt 查不到。
var TraceBodyZone = time.FixedZone("CST", 8*60*60)

// traceBodyMaxRollInterval 是滚动间隔的硬上界（规格 §15.1 R8：≤ 1h）。
const traceBodyMaxRollInterval = time.Hour

// traceBodyMaxFlushInterval 是 flush 周期的硬上界（规格 §7.1 / 计划全局约束：≤ 1m）。
//
// 【为什么必须**强制**、而不只是写进文档】周期 flush 是 sealer 的前置条件：
// sealer 只看得见已 write(2) 的字节，flush 周期一旦被放宽（例如 5m），
// 「块已停止追加、但字节还在用户态缓冲」的窗口就被拉长 —— 声明的不变量变成空的，
// 而配置看上去完全合法。因此超额必须**启动失败**，不得静默接受。
const traceBodyMaxFlushInterval = time.Minute

// traceBodyMinAge 是 sealer 侧 minAge 的镜像（规格 §7.2：3h）。
// 滚动间隔必须**严格小于**它：sealer 只靠 mtime + minAge 判断「已停止追加」，
// 网关若在 sealer 已封块并删除 spool 之后仍持有同一 part 的 fd，
// 追加会写进已被 unlink 的 inode —— 落盘成功但永久不可达（D7）。
//
// ⚠️ 在本文件的校验次序下，这一条**不可达**：`RollInterval > 1h` 的更早守卫
// 已经拦下所有 `>= 3h` 的取值（见 tools/trace-mutate/sets/p2-task-2.py 的 m03）。
// 保留它是防御性的：若上界常量将来被放宽，它仍是最后一道；但它无法被任何用例
// 单独证伪，因此不得声称「有用例证明它承重」。
const traceBodyMinAge = 3 * time.Hour

// TraceBodyConfig 是正文直写 /datalog 的网关侧配置（规格 §11.1）。
type TraceBodyConfig struct {
	// Enabled 由 Root != "" 推导，没有独立开关：一个「开着但没有根目录」的
	// 状态没有意义，多一个布尔量就多一种自相矛盾的组合。
	Enabled bool

	Root                 string
	Inst                 string
	BlockMB              int64
	FlushInterval        time.Duration
	RollInterval         time.Duration
	DiskWatermarkPercent int
}

// LoadTraceBodyConfig 从 viper 读取 trace_body.* 配置并做完整校验。
//
// 环境变量名与规格 §11.1 逐字一致（viper 的 "." → "_" 替换规则给出
// trace_body.root → TRACE_BODY_ROOT）：不要把这些键放进 observability.*，
// 那会得到 OBSERVABILITY_TRACE_BODY_ROOT，与部署文档不符。
//
// 校验策略分三类（这个区分是刻意的）：
//   - 「字段本身非法」（inst 不在白名单、间隔非正或超上界、位数为 0、水位越界）⇒ **一律报错**，
//     无论功能是否开启。因为这类值一定是配置错误，静默接受只会掩盖问题。
//   - 「功能开启所必需但可以缺省」（inst 为空）⇒ 关闭时允许、开启时必须报错。
//   - 「root 的形态」⇒ 非空时必须绝对路径且不得是文件系统根（见下）。
//     root 为空或 trim 后为空是**关闭**的表达方式，不是错误。
func LoadTraceBodyConfig(v *viper.Viper) (TraceBodyConfig, error) {
	cfg := TraceBodyConfig{
		// TrimSpace 之后再判空：TRACE_BODY_ROOT=" " 这类「看起来设了、其实没设」
		// 的值若被当成开启，会走一条 root 为空白字符的路径，失败方式难以诊断。
		Root:                 strings.TrimSpace(v.GetString("trace_body.root")),
		Inst:                 strings.TrimSpace(v.GetString("trace_body.inst")),
		BlockMB:              v.GetInt64("trace_body.block_mb"),
		FlushInterval:        v.GetDuration("trace_body.flush_interval"),
		RollInterval:         v.GetDuration("trace_body.roll_interval"),
		DiskWatermarkPercent: v.GetInt("trace_body.disk_watermark_percent"),
	}
	cfg.Enabled = cfg.Root != ""

	if cfg.Enabled && !filepath.IsAbs(cfg.Root) {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.root 必须是绝对路径，实际 %q：相对路径的含义取决于进程工作目录，"+
				"而 /datalog 是要被 bind mount 的绝对路径 —— 相对路径会让正文落到无法预测的位置", cfg.Root)
	}
	if cfg.Enabled && filepath.Clean(cfg.Root) == "/" {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.root 不得是文件系统根 %q：spool 会被建在宿主根文件系统上（/spool/dt=…），"+
				"这正是 2026-09-24 `/` 被写满的同一类事故", cfg.Root)
	}
	if cfg.Inst != "" && cfg.Inst != "blue" && cfg.Inst != "green" {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.inst 只能是 blue 或 green，实际 %q（规格 §5.1 的目录布局要求实例隔离）", cfg.Inst)
	}
	if cfg.Enabled && cfg.Inst == "" {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.root 已设置（功能开启）但 trace_body.inst 为空：没有实例标识就无法隔离写入")
	}
	if cfg.BlockMB <= 0 {
		return TraceBodyConfig{}, fmt.Errorf("trace_body.block_mb 必须为正，实际 %d", cfg.BlockMB)
	}
	if cfg.FlushInterval <= 0 {
		return TraceBodyConfig{}, fmt.Errorf("trace_body.flush_interval 必须为正，实际 %s", cfg.FlushInterval)
	}
	if cfg.FlushInterval > traceBodyMaxFlushInterval {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.flush_interval=%s 超过 1m 上界（规格 §7.1 / 全局约束：周期 flush ≤ 1m 且必须无条件执行）；"+
				"flush 过慢会让「块已停止追加但字节仍在缓冲」的窗口拉长，而 sealer 只看得到已 write(2) 的字节",
			cfg.FlushInterval)
	}
	// 【已知的冗余守卫，不要当成证据】在 flush > 0 的前提下，`RollInterval <= 0`
	// 必然同时命中紧随其后的 `FlushInterval >= RollInterval`（flush < roll <= 0
	// 与 flush > 0 矛盾）⇒ 这一条的报错路径**不可达**，删掉它行为不变
	//（见集合 p2-task-2.py 的 m16）。保留它是为了给出准确的错误信息。
	if cfg.RollInterval <= 0 {
		return TraceBodyConfig{}, fmt.Errorf("trace_body.roll_interval 必须为正，实际 %s", cfg.RollInterval)
	}
	if cfg.RollInterval > traceBodyMaxRollInterval {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.roll_interval=%s 超过 1h 上界（规格 §15.1 R8：滚动间隔 ≤ 1h）；"+
				"滚动过慢会让 sealer 在网关仍持有 fd 时封块并删除 spool", cfg.RollInterval)
	}
	if cfg.RollInterval >= traceBodyMinAge {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.roll_interval=%s 必须严格小于 sealer 的 minAge=%s（规格 §7.2）", cfg.RollInterval, traceBodyMinAge)
	}
	if cfg.FlushInterval >= cfg.RollInterval {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.flush_interval=%s 必须远小于滚动间隔=%s（规格 §7.1：sealer 只能看到已 write(2) 的字节）",
			cfg.FlushInterval, cfg.RollInterval)
	}
	if cfg.DiskWatermarkPercent < 1 || cfg.DiskWatermarkPercent > 99 {
		return TraceBodyConfig{}, fmt.Errorf(
			"trace_body.disk_watermark_percent 必须在 1..99，实际 %d（0 会让每个请求都降级，100 等于不设水位）",
			cfg.DiskWatermarkPercent)
	}
	return cfg, nil
}

// DayStart 是**唯一**的日期计算入口：返回该事件归属的那一天（UTC+8 的日历日）
// 在 **UTC 零点** 的 time.Time，供 `body_ref_dt`（PG `date` 列）使用。
//
// 口径固定为「TS 在 UTC+8 下的日期」，理由见 TraceBodyZone 与规格 §5.1：
// 归档行里的 ts 惯例是 +08:00，dt 必须与它指向同一天。
//
// 【为什么归一到 UTC 零点，而不是 +08:00 的零点】`body_ref_dt` 是 PG `date` 列，
// 驱动按**会话时区**（生产 = UTC）把 time.Time 折算成日期：若传
// `2026-09-24T00:00:00+08:00`，UTC 会话下会落成 `2026-09-23`（差一天）。
// 用「该日历日的 UTC 零点」表达日期，与 DayOf 的字符串形式严格一致。
func (c TraceBodyConfig) DayStart(ts time.Time) time.Time {
	t := ts.In(TraceBodyZone)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// DayOf 返回事件归属的 dt（目录名分量）。
//
// **必须由 DayStart 派生**（单一事实源，见全局约束）：写入 body_ref_dt 用 DayStart、
// 拼接 `dt=` 目录名用 DayOf，二者同源 ⇒ 列与目录不可能指向不同的日子。
//
// 【为什么这条不变量由静态检查而不是用例锁住】任何「自己算一遍」的正确实现都会
// 得到同一个字符串，行为上无法区分 ⇒ 用例与变异都测不出来（见集合的
// CHECKS「DayOf 由 DayStart 派生」）。
func (c TraceBodyConfig) DayOf(ts time.Time) string {
	return c.DayStart(ts).Format("2006-01-02")
}
