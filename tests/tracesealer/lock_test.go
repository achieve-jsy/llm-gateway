package tracesealer_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestLockIsExclusive 第二个持有者必须失败。**同一个进程里两次独立 open 也够**：
// flock 锁属于 open file description，两次 open 得到两个不同的 description，
// 因此非阻塞加锁会拿到 EWOULDBLOCK —— 不需要起第二个进程就能测。
func TestLockIsExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".sealer.lock")
	l1, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	defer l1.Release()
	_, err = tracesealer.AcquireLock(p)
	require.ErrorIs(t, err, tracesealer.ErrAnotherSealerRunning)
}

// TestLockReleasedAllowsReacquire 释放之后必须能被重新获取（否则一次排障之后
// sealer 就永久起不来了 —— 而这需要重启宿主机才能修）。
func TestLockReleasedAllowsReacquire(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".sealer.lock")
	l1, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	require.NoError(t, l1.Release())
	l2, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	require.NoError(t, l2.Release())
}

// TestLockReleaseIsIdempotent 退出路径（defer + 显式）会调两次，必须无害。
func TestLockReleaseIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".sealer.lock")
	l, err := tracesealer.AcquireLock(p)
	require.NoError(t, err)
	require.NoError(t, l.Release())
	require.NoError(t, l.Release())
}

// TestNewRunnerRefusesWhenLockHeld 装配期就必须挡住第二个实例（而不是等它
// 真的开始封块才发现）—— 这是「运维手工 --once 撞上常驻循环」的防线。
func TestNewRunnerRefusesWhenLockHeld(t *testing.T) {
	root := t.TempDir()
	r1, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	defer r1.Close()
	_, err = tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.ErrorIs(t, err, tracesealer.ErrAnotherSealerRunning)
}

// TestNewRunnerReleasesLockOnClose Close 之后同一 root 必须能再起一个。
func TestNewRunnerReleasesLockOnClose(t *testing.T) {
	root := t.TempDir()
	r1, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	require.NoError(t, r1.Close())
	r2, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	require.NoError(t, r2.Close())
}

// TestNewRunnerCloseIsIdempotent 退出路径（defer + 显式）会调两次 Close，必须无害。
//
// 【零变异杀手，如实登记】`Close` 的幂等由两层保证（`r.lock = nil` + `Lock.Release`
// 自己的幂等），而 runner.go 侧任何单点变异都杀不掉它（去掉 `r.lock = nil` 之后第二次
// Close 仍然走到 `Release`，而 `Release` 幂等）⇒ 它按 E64② 登记进集合
// p4-task-4-runner 的 COVERAGE_GAPS；下层的 `Lock.Release` 幂等由
// TestLockReleaseIsIdempotent 承载（被集合 A 的 m05 断言红杀死）。
func TestNewRunnerCloseIsIdempotent(t *testing.T) {
	root := t.TempDir()
	r, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
}

// TestNewRunnerReleasesLockWhenSealerFails 装配失败（封块器构造失败）必须把**已经取得
// 的锁**释放干净：否则一次配错会把锁一直握到进程退出，下一次启动会报「已有另一个
// sealer 持有锁」—— 而真正的原因只是配置错了一个字段，运维会被指向错误的方向。
func TestNewRunnerReleasesLockWhenSealerFails(t *testing.T) {
	root := t.TempDir()
	_, err := tracesealer.NewRunner(tracesealer.Options{
		Root: root,
		NewSealer: func(ta.SealerOptions) (tracesealer.Sealer, error) {
			return nil, errors.New("替身：封块器构造失败")
		},
	})
	require.Error(t, err)
	r, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err, "装配失败路径必须释放已取得的锁")
	require.NoError(t, r.Close())
}

// TestNewRunnerNeverCreatesRoot 装配失败路径也不得创建根目录（与 Task 3 同一条铁律，
// 这里守的是「先建目录再取锁」的实现顺序）。
func TestNewRunnerNeverCreatesRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	_, err := tracesealer.NewRunner(tracesealer.Options{Root: missing, NewSealer: stubSealer})
	require.Error(t, err)
	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr))
}
