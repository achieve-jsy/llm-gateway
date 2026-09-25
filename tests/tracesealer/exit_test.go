package tracesealer_test

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestExitCodeZeroOnCleanRound 全绿轮次退 0。
func TestExitCodeZeroOnCleanRound(t *testing.T) {
	require.Equal(t, tracesealer.ExitOK, tracesealer.ExitCode(tracesealer.RoundResult{Sealed: 2}, nil))
}

// TestExitCodeOneOnFatalError 环境级/启动级错误退 1（与「某一轮有失败」区分开：
// 前者的运维动作是修环境，后者是盯那块 spool）。
func TestExitCodeOneOnFatalError(t *testing.T) {
	require.Equal(t, tracesealer.ExitFatal,
		tracesealer.ExitCode(tracesealer.RoundResult{}, errors.New("根目录不可用")))
}

// TestExitCodeTwoWhenOnceHasFailures 有失败（含幂等之外的任一计数）退 2。
func TestExitCodeTwoWhenOnceHasFailures(t *testing.T) {
	require.Equal(t, tracesealer.ExitFailed, tracesealer.ExitCode(tracesealer.RoundResult{TooManyRows: 1}, nil))
	require.Equal(t, tracesealer.ExitFailed, tracesealer.ExitCode(tracesealer.RoundResult{SealFailed: 1}, nil))
	// PartExists 是幂等无害 ⇒ 不得退 2（否则每天都会有一次「失败」告警）。
	require.Equal(t, tracesealer.ExitOK, tracesealer.ExitCode(tracesealer.RoundResult{PartExists: 3}, nil))
}
