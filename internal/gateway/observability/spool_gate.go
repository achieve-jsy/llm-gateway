package observability

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// 【本文件的硬约束：不得出现任何建目录调用】网关侧唯一的建目录动作在冻结包里
//（`internal/tracearchive/writer.go:489`），网关自己**一个目录都不建**。
// 一旦这里出现 MkdirAll（例如早期的探针实现），它会把消失的挂载点在容器可写层
// 重新建回来，从而掩盖 D49 要检测的「运行中挂载消失」。见 defaultProbeWrite。
//
// 【静态检查的效力边界（如实标注，不要据此宣称结构不变量）】集合里的
// CHECKS 行用正则 `\bMkdir[A-Za-z]*\s*\(` 做启发式匹配：它覆盖**直接调用**形态 ——
// 无论写成 os.MkdirAll、别名导入后的 o.MkdirAll，还是 syscall.Mkdir，
// 只要标识符后面紧跟一个 ASCII 左括号就命中；但它**不是**结构不变量 ——
// 把 os.MkdirAll 取出赋值再调用、或走 exec 调 mkdir(1)，都能绕过；
// 反向它也会命中注释里那种「标识符紧跟 ASCII 左括号」的提及
//（本文件的注释因此一律不写该形态）。**承重证据是行为用例**：
// TestGateDefaultProbeDoesNotCreateRoot 用真实的 defaultProbeWrite 对不存在的 <root>
// 断言「探针失败 + 保持闩锁 + 根目录仍不存在」（对应变异 m16：往探针里注入一次
// MkdirAll 调用即转红）。静态行只是辅助信号。

// ErrSpoolLatched 表示正文落盘已被闩锁关闭：调用方**不得**再尝试写 spool，
// 而应把该行标为 body_missing=true（规格 §15.1 R9 的降级约定）。
var ErrSpoolLatched = errors.New("observability: 正文落盘已闩锁")

// GateStats 是闩锁状态的可观测快照（规格 §9.3：降级必须有明确可观测信号）。
type GateStats struct {
	Latched        bool
	Reason         string
	Trips          int64   // 累计跳变次数（不是行数）
	ProbeFailures  int64   // 探针未通过恢复判据的次数（探针写失败，或水位尚未回落）
	LatchedSeconds int64   // 本次闩锁已持续的秒数（解锁后归零；每次 Tick 重算）
	UsagePercent   float64 // 最近一次观测到的水位
}

// SpoolGateOptions 描述水位与闩锁行为。三个函数型字段都可注入，
// 这是「测试不得依赖真实盘满」的实现方式（注入水位/探针，而非 mock 文件系统）。
type SpoolGateOptions struct {
	Root                 string
	DiskWatermarkPercent int
	ProbeInterval        time.Duration
	Now                  func() time.Time
	UsagePercent         func(root string) (float64, error)
	ProbeWrite           func(root string) error
}

// SpoolGate 是「要不要写 spool」的唯一判定者（规格 §15.1 R9）。
//
// 【为什么必须有闩锁，而不是每次写入失败就跳过这一行】写失败（典型 ENOSPC）后，
// tracearchive.Writer 会把当前块毒化隔离并**在下次 Append 时**换新 part，
// 而算新序号要 os.ReadDir(spool 目录) —— 每请求一次失败就付出一次 O(n) 目录扫描，
// 同时 spool 目录里的 part 序号与 dentry 都会无界增长。闩锁把「每请求重试」
// 变成「每 ProbeInterval 探一次」。
type SpoolGate struct {
	opts SpoolGateOptions

	mu             sync.Mutex
	latched        bool
	reason         string
	latchedAt      time.Time
	lastProbe      time.Time
	trips          int64
	probeFailures  int64
	latchedSeconds int64
	usagePercent   float64
}

func NewSpoolGate(opts SpoolGateOptions) *SpoolGate {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ProbeInterval <= 0 {
		opts.ProbeInterval = time.Minute
	}
	if opts.UsagePercent == nil {
		opts.UsagePercent = defaultUsagePercent
	}
	if opts.ProbeWrite == nil {
		opts.ProbeWrite = defaultProbeWrite
	}
	return &SpoolGate{opts: opts}
}

// Allow 是热路径唯一会调用的方法：只读一个布尔量，**不做任何 IO**。
func (g *SpoolGate) Allow() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.latched
}

// Trip 进入闩锁。返回值表示「本次是否发生了跳变」——调用方据此恰好打印一条
// ERROR 日志；闩锁期间的每一行都打日志会淹没日志（规格 §9.3 要的是信号，不是噪音）。
func (g *SpoolGate) Trip(reason string, cause error) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.latched {
		return false
	}
	g.latched = true
	g.reason = reason
	if cause != nil {
		g.reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	g.latchedAt = g.opts.Now()
	g.lastProbe = g.latchedAt
	g.trips++
	return true
}

// Tick 是周期评估入口（由 spooler 的 flush 循环调用，**不在请求路径上**）。
//
//   - 未闩锁：查一次水位，超限则闩锁；
//   - 已闩锁：到点做一次探针；**水位回落**与**探针成功**必须同时成立才解锁。
//
// 【为什么解锁要两个条件】水位回落只说明盘上有空间了，不说明本进程能写
// （权限、只读挂载、目录被删都可能）。反之探针成功也可能只是写进了别的文件系统。
// 两个条件同时成立才恢复，是 R9「恢复条件必须显式」的落地。
//
// 【一处**故意的 fail-open 取舍**（必须写明，不能靠读代码猜）】已闩锁分支里，
// 若 statfs（UsagePercent）本身失败，`usageErr == nil && …` 这一半为假 ⇒
// 「双条件解锁」退化为**只看探针**：探针可写就解锁。取舍理由：statfs 的 err
// 可能只是权限/短暂故障，把它当成「水位未回落」会把网关永久钉死在闩锁上；
// 而「根目录是否还是原来那个挂载点」这件事**探针判不了** —— 冻结包的
// writer.go:489 会在容器可写层把根目录重建出来，容器层里写探针是会成功的。
// 因此运行中挂载消失的判据（根目录身份基线 + 每 tick/滚动比对 + 解锁前先验身份）
// **不在本文件**，属 Task 5 的周期 flush 与滚动路径；本文件只保证
// 「读不到水位」不伪装成「盘满」（变异 m17 / TestGateUsageErrorDoesNotLatch）。
func (g *SpoolGate) Tick(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.latched {
		if pct, err := g.opts.UsagePercent(g.opts.Root); err == nil {
			g.usagePercent = pct
			if pct >= float64(g.opts.DiskWatermarkPercent) {
				g.latched = true
				g.reason = fmt.Sprintf("disk_watermark: %s 已用 %.1f%% ≥ %d%%",
					g.opts.Root, pct, g.opts.DiskWatermarkPercent)
				g.latchedAt = now
				g.lastProbe = now
				g.trips++
			}
		}
		// statfs 失败（例如根目录不存在）本身不闩锁：真正的判据是「能不能写」，
		// 由第一次 Append 失败或探针来定。这里保持沉默是**故意**的——
		// 把「statfs 失败」也当闩锁会让一个权限问题伪装成磁盘问题。
		return
	}

	g.latchedSeconds = int64(now.Sub(g.latchedAt).Seconds())
	if now.Sub(g.lastProbe) < g.opts.ProbeInterval {
		return
	}
	g.lastProbe = now

	probeErr := g.opts.ProbeWrite(g.opts.Root)
	pct, usageErr := g.opts.UsagePercent(g.opts.Root)
	if usageErr == nil {
		g.usagePercent = pct
	}
	if probeErr != nil {
		g.probeFailures++
		return
	}
	if usageErr == nil && pct >= float64(g.opts.DiskWatermarkPercent) {
		g.probeFailures++
		return
	}
	g.latched = false
	g.reason = ""
	g.latchedSeconds = 0
}

func (g *SpoolGate) Latched() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.latched }

func (g *SpoolGate) Reason() string { g.mu.Lock(); defer g.mu.Unlock(); return g.reason }

func (g *SpoolGate) Stats() GateStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	return GateStats{
		Latched: g.latched, Reason: g.reason, Trips: g.trips,
		ProbeFailures: g.probeFailures, LatchedSeconds: g.latchedSeconds,
		UsagePercent: g.usagePercent,
	}
}

// defaultUsagePercent 用 statfs 算「已用百分比」。
//
// 取 Bavail（普通用户可用块）而不是 Bfree：容器里进程常以 root 跑，
// 但用 Bavail 是保守的一侧 —— 水位线宁可早触发也不该晚触发。
func defaultUsagePercent(root string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", root, err)
	}
	total := uint64(st.Blocks) * uint64(st.Bsize)
	if total == 0 {
		return 0, fmt.Errorf("statfs %s: 总块数为 0", root)
	}
	avail := uint64(st.Bavail) * uint64(st.Bsize)
	return float64(total-avail) / float64(total) * 100, nil
}

// defaultProbeWrite 是恢复探针：在正文根目录下建一个临时文件、写一个字节、删掉。
//
// 【探针绝不得创建根目录（登记 D49 的一部分）】这里**必须**用
// `os.CreateTemp(root, …)` 这种「父目录必须已存在」的写法，**不得**先建出
// `<root>/spool` 再建文件：MkdirAll 会把缺失的**父目录链**一路建出来，
// 也就是把消失的挂载点在**容器可写层**重新建回来 —— 探针会因此「成功」，
// 把 D49 要检测的「运行中挂载消失」掩盖掉（更糟：它自己制造了那个容器层目录）。
// 用 CreateTemp 时根目录不存在 ⇒ 直接 ENOENT ⇒ 探针失败 ⇒ 保持闩锁，这才是要的语义。
//
// 【为什么探针就放在 <root> 而不是 spool 子目录】根目录可能是个只读挂载点而
// spool 子目录可写，反之亦然；但 spool 与 dt/inst 子目录都不可能各自是独立挂载点
// （`/datalog` 才是挂载点，其余都是它上面的普通目录），因此在同一文件系统上探测即可，
// 而放在 <root> 恰好满足「不创建目录」这一条硬约束。
//
// 【为什么探针只写 1 字节】恢复判定要的是「写权限 + 空间」这两件事，
// 不需要复现真实写入量；写多了会在接近满盘时把水位推得更高，形成抖动。
func defaultProbeWrite(root string) error {
	f, err := os.CreateTemp(root, ".probe-*")
	if err != nil {
		return fmt.Errorf("建探针文件失败: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write([]byte{'\n'}); err != nil {
		f.Close()
		return fmt.Errorf("写探针失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync 探针失败: %w", err)
	}
	return f.Close()
}
