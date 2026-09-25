package tracesealer_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// fakeSealer 让「同一批任务里既有成功又有四类失败」这种组合可确定复现 ——
// 真实冻结包无法为同一 Ref 报不同错误（它的失败是环境的函数）。
type fakeSealer struct {
	tasks   []ta.SealTask
	byPart  map[int]error
	calls   []int
	listErr error
	onSeal  func(part int)
}

func (f *fakeSealer) ListPendingSeals() ([]ta.SealTask, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tasks, nil
}

func (f *fakeSealer) Seal(_ context.Context, task ta.SealTask) error {
	f.calls = append(f.calls, task.Part)
	if f.onSeal != nil {
		f.onSeal(task.Part)
	}
	return f.byPart[task.Part]
}

func mkTask(part int) ta.SealTask {
	return ta.SealTask{
		DT: "2026-10-08", Inst: "blue", Part: part,
		SpoolPath:         fmt.Sprintf("/datalog/trace-archive-v4/spool/dt=2026-10-08/inst=blue/part-%06d.jsonl", part),
		UncompressedBytes: 1 << 20,
	}
}

func newTestRunner(t *testing.T, f *fakeSealer) *tracesealer.Runner {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "spool"), 0o755))
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root: root,
		NewSealer: func(ta.SealerOptions) (tracesealer.Sealer, error) {
			return f, nil
		},
	})
	require.NoError(t, err)
	return r
}

// TestRoundSealsEveryEligiblePart 一轮必须走完所有可封块（一个失败不能中断整轮）。
func TestRoundSealsEveryEligiblePart(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0), mkTask(1), mkTask(2), mkTask(3)},
		byPart: map[int]error{1: fmt.Errorf("%w: 子进程退出 37", ta.ErrSealFailed)},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int{0, 1, 2, 3}, f.calls, "part 1 失败后必须继续封 2 与 3")
	require.Equal(t, 4, res.Eligible)
	require.Equal(t, 3, res.Sealed)
	require.Equal(t, 1, res.SealFailed)
}

// TestRoundTreatsPartExistsAsIdempotentNoop 产物已存在 ⇒ 不计入失败、不告警。
func TestRoundTreatsPartExistsAsIdempotentNoop(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0), mkTask(1)},
		byPart: map[int]error{0: fmt.Errorf("%w: 已存在", ta.ErrPartExists)},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.PartExists)
	require.Equal(t, 0, res.Failed(), "PartExists 不是失败")
	require.Equal(t, 1, res.Sealed)
}

// TestRoundSeparatesPermanentFromRetryable 超限块必须与可重试失败分开计数
// （否则运维无法从汇总里区分「等一等就好」与「必须有人动手」）。
func TestRoundSeparatesPermanentFromRetryable(t *testing.T) {
	f := &fakeSealer{
		tasks: []ta.SealTask{mkTask(0), mkTask(1)},
		byPart: map[int]error{
			0: fmt.Errorf("%w: 行数=8388609", ta.ErrTooManyRows),
			1: fmt.Errorf("%w: 行数不符", ta.ErrRowCountMismatch),
		},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.TooManyRows)
	require.Equal(t, 1, res.RowMismatch)
	require.Equal(t, 2, res.Failed())
}

// TestRoundCountsUnknownFailuresAsFailures 未分类失败也必须计入 Failed()：
// 它是「我们还不知道这是什么」，不是「没问题」；漏计会让退出码与告警一起沉默。
func TestRoundCountsUnknownFailuresAsFailures(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0)},
		byPart: map[int]error{0: errors.New("EIO：底层 IO 抖动")},
	}
	r := newTestRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.UnknownFailed)
	require.Equal(t, 1, res.Failed())
}

// stubSealer 是 options_test.go / exit_test.go 共用的替身构造器
// （它们只关心启动期校验与退出码，不需要真实的发现结果）。
func stubSealer(ta.SealerOptions) (tracesealer.Sealer, error) { return &fakeSealer{}, nil }

// TestRoundCancellationStopsBeforeNextSeal ctx 取消后**不得**再开始新的一块：
// 关机时「少封一块」是无害的（下轮会重新发现），「封到一半被杀」才会留下 .tmp。
func TestRoundCancellationStopsBeforeNextSeal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeSealer{tasks: []ta.SealTask{mkTask(0), mkTask(1), mkTask(2)}, byPart: map[int]error{}}
	f.onSeal = func(int) { cancel() }
	r := newTestRunner(t, f)
	_, err := r.Round(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []int{0}, f.calls)
}

// TestRoundDoesNotSwallowListError 发现阶段失败必须上抛（它是「根目录读不了」这类
// 环境级问题，与「某一块封不了」不同 —— 前者会让本轮所有块都被跳过）。
func TestRoundDoesNotSwallowListError(t *testing.T) {
	f := &fakeSealer{listErr: errors.New("读 spool 根目录失败: permission denied")}
	r := newTestRunner(t, f)
	_, err := r.Round(context.Background())
	require.ErrorContains(t, err, "列举待封块失败")
}
