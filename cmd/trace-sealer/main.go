// trace-sealer 是调用链正文的封块器（方案 C：网关内不压缩，压缩由本进程调
// C 版 zstd CLI 完成）。它**必须在宿主机上运行**（不进容器），用宿主机的
// /usr/bin/zstd；详见 deploy/systemd/trace-sealer.service 与
// docs/superpowers/plans/2026-10-08-trace-sealer-deployment.md。
//
// 退出码契约（unit 与 CI 都依赖它）：见 internal/tracesealer/exit.go 的 ExitOK /
// ExitFatal / ExitFailed。**本文件不得自己决定退出码**，一律 os.Exit(tracesealer.ExitCode(...))。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/llm-gateway/internal/tracesealer"
)

func main() {
	var (
		root        = flag.String("root", "", "归档根目录（必填，必须绝对路径且不是 /）")
		zstdBinary  = flag.String("zstd-binary", "", "zstd 可执行文件（生产必须显式给 /usr/bin/zstd）")
		zstdLevel   = flag.Int("zstd-level", 0, "zstd 级别（0 ⇒ 19，规格 §5.3.3）")
		zstdWinMB   = flag.Int("zstd-window-mb", 0, "zstd 窗口 MiB（0 ⇒ 512 ⇒ --long=29）")
		minAge      = flag.Duration("min-age", 0, "「已停止追加」判定窗口（0 ⇒ 3h；必须 > 1h）")
		expectZstd  = flag.String("expected-zstd-version", "", "期望的 zstd 版本（0 ⇒ 1.4.8）")
		pollEvery   = flag.Duration("poll-interval", 0, "常驻循环的轮询周期（0 ⇒ 5m，未提供的 0 值即默认）")
		sealTimeout = flag.Duration("seal-timeout", 0, "单个 part 的封块上限（0 ⇒ 30m）")
		lockFile    = flag.String("lock-file", "", "单实例锁文件（默认 <root>/.sealer.lock）")
		once        = flag.Bool("once", false, "只跑一轮然后退出（离线标定与排障用）")
		version     = flag.Bool("version", false, "打印版本并退出")
	)
	flag.Parse()
	if *version {
		fmt.Println("trace-sealer (plan 4/4)")
		return
	}
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root: *root, ZstdBinary: *zstdBinary, ZstdLevel: *zstdLevel,
		ZstdWindowMB: *zstdWinMB, MinAge: *minAge, ExpectedZstdVersion: *expectZstd,
		PollInterval: *pollEvery, SealTimeout: *sealTimeout, LockFile: *lockFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "trace-sealer: %v\n", err)
		os.Exit(tracesealer.ExitFatal)
	}
	// 单实例锁与 Close 在计划 4/4 的任务 4 落地；本任务还没有 Close。

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if !*once {
		fmt.Fprintln(os.Stderr, "trace-sealer: 常驻循环在计划 4/4 任务 3 落地；当前只支持 --once")
		os.Exit(tracesealer.ExitFatal)
	}
	res, err := r.Round(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trace-sealer: %v\n", err)
		os.Exit(tracesealer.ExitCode(res, err))
	}
	os.Exit(tracesealer.ExitCode(res, nil))
}
