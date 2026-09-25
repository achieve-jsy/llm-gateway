package tracesealer

import (
	"errors"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// FailureKind 是封块失败的分类。
//
// 【为什么必须有分类，而不是「失败就重试」】冻结包 errors.go 已经把三类失败
// 的语义写死：ErrPartExists 幂等无害、ErrRowCountMismatch/ErrSealFailed 保留 spool
// 并可重试、ErrTooManyRows 重试**永远不会**成功（需人工介入）。若 sealer 把三者
// 揉成一个「失败 ⇒ 告警 ⇒ 下轮重试」，运维看到的将是一堆永远不消失的告警，
// 而真正需要人拆块的那一块被淹没 —— 这是「把可诊断故障变成噪音」的典型形态。
type FailureKind string

const (
	KindPartExists  FailureKind = "part_exists"
	KindRowMismatch FailureKind = "row_count_mismatch"
	KindSealFailed  FailureKind = "seal_failed"
	KindTooManyRows FailureKind = "too_many_rows"
	KindUnknown     FailureKind = "unknown"
)

// Classify 用 errors.Is 判定分类（**不得用 ==**：冻结包一律用 %w 包装，
// 见 errors.go 的注释「三者都必须用 %w 包装，否则调用方的 errors.Is 分类失效」）。
//
// 【判定次序】先判「永久」再判「幂等」再判「可重试」：三类哨兵互不重叠，
// 次序只在 KindUnknown 的兜底上与实现耦合 —— 兜底必须是最后一个 else，
// 绝不能让某个未知错误被某个具体分支抢先吞掉。
func Classify(err error) FailureKind {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ta.ErrTooManyRows):
		return KindTooManyRows
	case errors.Is(err, ta.ErrPartExists):
		return KindPartExists
	case errors.Is(err, ta.ErrRowCountMismatch):
		return KindRowMismatch
	case errors.Is(err, ta.ErrSealFailed):
		return KindSealFailed
	default:
		return KindUnknown
	}
}

// Retryable 表示「保留 spool、下轮重试」。
// KindPartExists **不算**重试：它是「已经做好了，什么都不用做」。
func (k FailureKind) Retryable() bool {
	return k == KindRowMismatch || k == KindSealFailed || k == KindUnknown
}

// Permanent 表示「重试永远不会成功，需人工介入」。
func (k FailureKind) Permanent() bool { return k == KindTooManyRows }
