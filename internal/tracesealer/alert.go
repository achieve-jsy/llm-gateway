package tracesealer

// Severity 是本轮汇总的**告警分级**。它与日志级别不是同一件事：日志级别由运维的
// 采集配置决定，而这个量回答的是「这一轮该不该触发告警、以什么级别触发」。告警规则
// 按它匹配 ⇒ 三个取值的语义一经发布就是对外契约，不得为了「少吵一点」而挪用。
type Severity int

const (
	// SeverityNone 无需告警：全绿轮次，**以及** ErrPartExists —— 产物已存在是幂等
	// 无害的预期状态（下轮扫描仍会看到同一 spool，不做任何补救动作，见全局约束
	// 第 3 条与 N5）。把它算成告警会让每天有一个恒定的告警，最终让真告警失效。
	SeverityNone Severity = iota
	// SeverityWarn 可重试失败：行数不符 / 封块失败 ⇒ 告警 + 保留 spool + 下轮重试。
	SeverityWarn
	// SeverityError 永久失败（TooManyRows：重试**永远不会**成功，需人工介入）或
	// 未分类失败（「我们还不知道这是什么」不等于「没问题」⇒ 宁可吵，不可静默）。
	SeverityError
)

// Alert 是一轮汇总的对外可观测面：该不该告警（Severity）、告警规则按什么匹配
// （Reason）、以及看板/规则要用的稳定字段集（Fields）。
type Alert struct {
	Severity Severity
	// Reason 是**稳定串**：too_many_rows / unknown_failure / round_failed；
	// SeverityNone 时为空串。见 Summarize 的说明（不得用错误文本）。
	Reason string
	// Fields 是稳定字段集：十个键**恒在**（零值也写）。
	Fields map[string]any
}

// Summarize 把一轮的计数映射成告警。判定次序**从严到宽**短路：
//
//	TooManyRows   > 0          ⇒ SeverityError / "too_many_rows"
//	UnknownFailed > 0          ⇒ SeverityError / "unknown_failure"
//	RowMismatch + SealFailed>0 ⇒ SeverityWarn  / "round_failed"
//	否则                        ⇒ SeverityNone  / ""
//
// 【为什么 PartExists 不在任何条件里】它是幂等无害的预期状态（冻结包 errors.go），
// 计进失败面就等于把一个正常状态报成故障（见全局约束第 3 条）。
//
// 【为什么 Reason 是手写稳定串、不是错误文本】告警规则、看板、运行手册都按它匹配。
// 用 err.Error() 拼出来的串会随上游一次文案改动就**静默失配** —— 规则还在，但永远
// 不响。三个取值就是契约，因此本文件也不得出现任何格式化拼串（静态检查钉住）。
//
// 【为什么 Fields 是显式字面量、不是反射】反射会让「漏掉一个字段」在编译期完全
// 不可见。显式字面量把字段集写死在这一个函数里，增删必须经过 review。十个键在
// **零值轮次**也照样出现：缺失字段会被看板读成「没有问题」，而它的真实含义是
// 「本轮没有样本」（例如任务 10 之前 MaxRows / MinBytesPerRow / MinRatio 恒为零值，
// 其零值语义见 RoundResult 的注释）。
func Summarize(res RoundResult) Alert {
	fields := map[string]any{
		"eligible":          res.Eligible,
		"sealed":            res.Sealed,
		"part_exists":       res.PartExists,
		"row_mismatch":      res.RowMismatch,
		"seal_failed":       res.SealFailed,
		"too_many_rows":     res.TooManyRows,
		"unknown_failed":    res.UnknownFailed,
		"max_rows":          res.MaxRows,
		"min_bytes_per_row": res.MinBytesPerRow,
		"min_ratio":         res.MinRatio,
	}
	switch {
	case res.TooManyRows > 0:
		return Alert{Severity: SeverityError, Reason: "too_many_rows", Fields: fields}
	case res.UnknownFailed > 0:
		return Alert{Severity: SeverityError, Reason: "unknown_failure", Fields: fields}
	case res.RowMismatch+res.SealFailed > 0:
		return Alert{Severity: SeverityWarn, Reason: "round_failed", Fields: fields}
	}
	return Alert{Severity: SeverityNone, Fields: fields}
}
