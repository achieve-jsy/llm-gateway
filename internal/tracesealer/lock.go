package tracesealer

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrAnotherSealerRunning 已有另一个 sealer 持有这个 root 的锁。
var ErrAnotherSealerRunning = errors.New("tracesealer: 已有另一个 sealer 持有锁")

// Lock 是**跨进程**单实例锁（flock）。
//
// 【为什么必须有它，而不是「systemd 保证只起一个」】systemd 只保证**它自己**起一个。
// 运维手工跑一次 `--once`（排障、离线标定）与常驻循环撞在一起时，两个进程会对同一个
// (dt, inst, part) 并发调用 Seal —— 冻结包在 writer.go 的 Seal 路径上明确写着
// 「同一个 (dt, inst, part) 必须串行：它们共用同一个 <partPath>.tmp，并发跑会互相
// 交错/覆盖同一个临时文件」。而发现路径的 os.Stat(partPath) 检查也不是原子的：
// 两个进程可以同时通过，然后同时往同一个 .tmp 里写。
// 因此互斥必须是**内核级**的、覆盖整个发现+封块过程，而不是靠「启动时看一眼」。
type Lock struct{ f *os.File }

// AcquireLock 以**非阻塞**排他方式取得 path 上的 flock。第二个持有者立即拿到
// ErrAnotherSealerRunning（而不是排队等 —— 等会把一次手工排障变成一次神秘挂起）。
func AcquireLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开锁文件失败: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w（锁文件 %s）", ErrAnotherSealerRunning, path)
		}
		return nil, fmt.Errorf("加锁失败: %w", err)
	}
	return &Lock{f: f}, nil
}

// Release 解锁并关闭；幂等（defer + 显式调用两条路径都会走到）。
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	// 先显式解锁再关闭：即使 Close 失败，锁也已经放开，不会把下一个 sealer 挡在外面。
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}
