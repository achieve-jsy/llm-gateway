package tracesealer_test

import (
	"errors"
	"fmt"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestClassifyPartExistsIsIdempotent PartExists 是幂等无害的：
// 下轮扫描仍会看到同一个 spool，运维动作是「什么都不做」，因此**不得告警**。
func TestClassifyPartExistsIsIdempotent(t *testing.T) {
	err := fmt.Errorf("%w: /datalog/x/part-000001.jsonl.zst", ta.ErrPartExists)
	require.Equal(t, tracesealer.KindPartExists, tracesealer.Classify(err))
	require.False(t, tracesealer.Classify(err).Retryable(), "幂等无害 ⇒ 不算「重试」")
	require.False(t, tracesealer.Classify(err).Permanent())
}

// TestClassifyRowMismatchIsRetryable 行数不符 ⇒ 保留 spool、下轮重试、必须告警。
func TestClassifyRowMismatchIsRetryable(t *testing.T) {
	err := fmt.Errorf("%w: 扫描=10 调用方=9", ta.ErrRowCountMismatch)
	require.Equal(t, tracesealer.KindRowMismatch, tracesealer.Classify(err))
	require.True(t, tracesealer.Classify(err).Retryable())
}

// TestClassifySealFailedIsRetryable 子进程非零退出 / 版本不符 / 残行都归这一类。
func TestClassifySealFailedIsRetryable(t *testing.T) {
	err := fmt.Errorf("%w: zstd 版本不符：期望 1.4.8，实际 1.5.7", ta.ErrSealFailed)
	require.Equal(t, tracesealer.KindSealFailed, tracesealer.Classify(err))
	require.True(t, tracesealer.Classify(err).Retryable())
}

// TestClassifyTooManyRowsIsPermanent 超限块**重试永远不会成功**（同一个 spool 重扫多少次
// 行数都一样）⇒ 必须与「可重试」分开，且标成需要人工介入。
func TestClassifyTooManyRowsIsPermanent(t *testing.T) {
	err := fmt.Errorf("%w: spool 行数=8388609 超过上界 8388608", ta.ErrTooManyRows)
	require.Equal(t, tracesealer.KindTooManyRows, tracesealer.Classify(err))
	require.True(t, tracesealer.Classify(err).Permanent())
	require.False(t, tracesealer.Classify(err).Retryable())
}

// TestClassifyUnknownIsLoudAndRetryable 未分类错误不得被当成「幂等无害」吞掉
// （那等于静默丢数据），也不得被当成永久（那会让一次瞬时故障永久停摆）。
func TestClassifyUnknownIsLoudAndRetryable(t *testing.T) {
	require.Equal(t, tracesealer.KindUnknown, tracesealer.Classify(errors.New("磁盘抖动")))
	require.True(t, tracesealer.Classify(errors.New("磁盘抖动")).Retryable())
}

// TestClassifyUsesErrorsIsNotEquality 冻结包用 %w 包装三类哨兵；用 == 比较会全部
// 落进 unknown（这条用例是「必须用 errors.Is」的杀手）。
func TestClassifyUsesErrorsIsNotEquality(t *testing.T) {
	wrapped := fmt.Errorf("外层上下文: %w", fmt.Errorf("中间层: %w", ta.ErrSealFailed))
	require.Equal(t, tracesealer.KindSealFailed, tracesealer.Classify(wrapped))
}
