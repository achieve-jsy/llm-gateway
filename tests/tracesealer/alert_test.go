package tracesealer_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestSummarizeCleanRoundIsSilent 全绿轮次**不得**产生任何告警
// （否则每天 288 轮的 INFO 会把真正的故障埋掉）。
func TestSummarizeCleanRoundIsSilent(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 3, Sealed: 3})
	require.Equal(t, tracesealer.SeverityNone, a.Severity)
}

// TestSummarizePartExistsAloneIsSilent ErrPartExists 是预期状态 ⇒ 不得告警。
func TestSummarizePartExistsAloneIsSilent(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, PartExists: 1})
	require.Equal(t, tracesealer.SeverityNone, a.Severity)
}

// TestSummarizeRetryableFailureIsWarn 可重试失败 = WARN（等下一轮），不是 ERROR。
func TestSummarizeRetryableFailureIsWarn(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, SealFailed: 1})
	require.Equal(t, tracesealer.SeverityWarn, a.Severity)
}

// TestSummarizeTooManyRowsIsErrorAndNamed 永久失败必须是 ERROR，且 reason 是稳定串
// （alert 规则按它匹配，不能靠错误文本）。
func TestSummarizeTooManyRowsIsErrorAndNamed(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, TooManyRows: 1})
	require.Equal(t, tracesealer.SeverityError, a.Severity)
	require.Equal(t, "too_many_rows", a.Reason)
}

// TestSummarizeUnknownFailureIsError 未分类失败按 ERROR 处理（宁可吵，不可静默）。
func TestSummarizeUnknownFailureIsError(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, UnknownFailed: 1})
	require.Equal(t, tracesealer.SeverityError, a.Severity)
	require.Equal(t, "unknown_failure", a.Reason)
}

// TestSummarizeFieldsAreStable 字段集必须固定（看板/告警规则依赖它），
// 且零值也要出现（缺失字段会被误读成「没问题」）。
func TestSummarizeFieldsAreStable(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{})
	for _, k := range []string{"eligible", "sealed", "part_exists", "row_mismatch",
		"seal_failed", "too_many_rows", "unknown_failed", "max_rows", "min_bytes_per_row", "min_ratio"} {
		require.Contains(t, a.Fields, k, "字段 %s 必须恒在", k)
	}
}
