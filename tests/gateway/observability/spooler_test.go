package observability_test

// 本文件是 B 层（仅公开 API）的 TraceSpooler 用例。
//
// 【相对简报 task-5-brief.md 的如实改动（每条都有实测依据，见 task-5-report.md）】
//
//  1. 简报用例 2（TestSpoolerRollsOnBlockBytes）在 FlushAndRoll 之后直接断言
//     `part-000001.jsonl` 存在 —— 这与冻结包 MarkSealed 的契约不符：MarkSealed 只
//     关掉当前句柄（closeCurLocked 后 cur = nil），**新块是下一次 Append 才建的**
//     （brief 用例 3 / 用例 5 自己就是这么写的：先 AppendLine 再断言 part-000001）。
//     实测：照抄简报原文时该用例恒红（Rolls 计为 1、但 part-000001 不存在）。
//     本文件按同一契约补一次 AppendLine 再断言（与用例 3 一致）。
//
//  2. 新增用例 12–18（简报只有 11 条）。依据是增补条目 A5-1/A5-4 与控制者的
//     「每条断言都必须能红」要求：
//     - A5-1 要求「运行中挂载消失」由**根目录身份基线**识别（冻结包的 MkdirAll 会
//       在容器可写层把 root 重建出来 ⇒ 存在性检查与写探针都会成功），
//       这条能力必须有能红的断言 ⇒ 12/13/14/15（身份变化、身份取不到、
//       解锁探针先验身份、构造期基线取不到即启动失败）；
//     - 简报的 TestSpoolerRefusesMissingRoot 只覆盖「启动期未挂载」，且它**杀不死**
//       「删掉启动期存在性检查」这条变异（身份基线在缺失路径上同样失败，两条检查
//       互相遮蔽）⇒ 16 用「身份可取得但目录不存在」把它隔离出来；
//     - 简报的 m07（写失败不闩锁）在简报 11 条用例上**零覆盖**：
//       TestSpoolerChecksRootAfterAppendFailure 会先被 checkRoot 闩锁（root_unavailable）
//       并提前 return，永远到不了 append_failed 那一支 ⇒ 17 让根目录保持健康、只让
//       建块失败，使 append_failed 有独立观察者；
//     - A5-4 要求 gate.Tick（含探针 fsync）在**写协程**上执行 ⇒ 18 观测
//       「Run 的 goroutine 上探针一次都没跑」。
//
//  3. 简报 m06/m07 的杀手用例名 `TestSpoolerLatchedAppendDoesNotTouchDisk` 在 11 条
//     用例里**不存在**。本文件用真实的顶层用例名取代它：
//     闩锁后必须拒绝写入 ⇒ TestSpoolerLatchesWhenRootDisappears；
//     写失败必须闩锁 ⇒ TestSpoolerLatchesOnAppendFailureWhileRootHealthy。

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gwconfig "github.com/example/llm-gateway/internal/gateway/config"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/stretchr/testify/require"
)

func newSpooler(t *testing.T, blocks int64, roll, flush time.Duration) (*gwobs.TraceSpooler, string) {
	t.Helper()
	root := t.TempDir()
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue",
		BlockBytes: blocks, RollInterval: roll, FlushInterval: flush,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, root
}

// spoolPath 拼出未封块正文的路径（与 tracearchive.Paths.SpoolFile 的布局一致）。
func spoolPath(root, dt string) string {
	return filepath.Join(root, "spool", "dt="+dt, "inst=blue", "part-000000.jsonl")
}

// identityStub 是可注入的根目录身份（A5-1）。
//
// 【为什么可用注入表达「挂载消失」】与 SpoolGate 用可注入水位表达「盘满」是同一
// 手法：等价的是**状态**（根目录背后换了一个挂载点），不是成因。测试不依赖真实
// umount，报告里也不得宣称验证过真实 umount。
//
// failing = true 时**刻意返回与基线相同的值 + error**：这样「取不到身份」必须靠
// err 分支闩锁，而不能靠「值恰好也变了」蒙混过关（隔离「忽略 err」的实现）。
type identityStub struct {
	id      atomic.Value // string
	failing atomic.Bool
}

func (s *identityStub) fn(string) (string, error) {
	v := s.id.Load().(string)
	if s.failing.Load() {
		return v, errors.New("mountinfo/statfs 不可读")
	}
	return v, nil
}

func newIdentityStub(v string) *identityStub {
	s := &identityStub{}
	s.id.Store(v)
	return s
}

// atomicProbe 是并发安全的探针计数器（用例 18 的 Run 跑在另一个 goroutine 上，
// 非原子的 countingProbe 会触发数据竞争）。
type atomicProbe struct {
	calls atomic.Int32
	err   error
}

func (p *atomicProbe) probe(string) error {
	p.calls.Add(1)
	return p.err
}

// lowUsageGate 构造一个「水位恒为 10%」的 Gate（评审 M5）。
//
// 【为什么必须注入】凡是要让闸门**解锁/恢复**的用例，若用内部默认 Gate，解锁条件就
// 包含「宿主真实盘用量 < 90%」—— 开发机或 CI 的盘用量超过 90% 时这两条用例**假红**。
// 它们要验证的是「解锁探针先验身份」与「根目录不可用时不做结构性写入」，与真实水位无关。
func lowUsageGate(root string) *gwobs.SpoolGate {
	return gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 root,
		DiskWatermarkPercent: 90,
		UsagePercent:         func(string) (float64, error) { return 10, nil },
	})
}

// logLines 返回日志缓冲里的非空行（用于「跳变时恰好一条 ERROR」这类计数断言）。
func logLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestSpoolerFlushMakesBufferedLinesVisible：Append 只是进 bufio 缓冲，
// 周期 flush 之后 sealer 才能看到完整字节（规格 §7.1）。
func TestSpoolerFlushMakesBufferedLinesVisible(t *testing.T) {
	s, root := newSpooler(t, 1<<30, time.Hour, time.Minute)
	for _, l := range []string{`{"a":1}`, `{"a":2}`} {
		_, err := s.AppendLine("2026-09-24", []byte(l))
		require.NoError(t, err)
	}
	spool := spoolPath(root, "2026-09-24")
	before, err := os.ReadFile(spool)
	require.NoError(t, err)
	require.Empty(t, before, "未 flush 前缓冲不得落盘（这正是「不 flush 会丢正文」的机理）")

	require.NoError(t, s.FlushAndRoll())
	after, err := os.ReadFile(spool)
	require.NoError(t, err)
	require.Equal(t, "{\"a\":1}\n{\"a\":2}\n", string(after))

	// §9.3 降级可观测性：周期 flush 必须计数；这一轮没有达阈值的块 ⇒ 不得记滚动。
	require.Equal(t, int64(1), s.Stats().Flushes)
	require.Equal(t, int64(0), s.Stats().Rolls)
}

// TestSpoolerRollsOnBlockBytes：达到块大小阈值 ⇒ 滚动到 part-000001。
func TestSpoolerRollsOnBlockBytes(t *testing.T) {
	const line = `{"pad":"0123456789012345678901234567890123456789"}`
	s, root := newSpooler(t, 1<<10 /* 1 KiB */, time.Hour, time.Minute)
	for i := 0; i < 64; i++ {
		_, err := s.AppendLine("2026-09-24", []byte(line))
		require.NoError(t, err)
	}
	require.NoError(t, s.FlushAndRoll())
	require.Equal(t, int64(1), s.Stats().Rolls, "达阈值的滚动必须被计数")
	dir := filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue")
	require.FileExists(t, filepath.Join(dir, "part-000000.jsonl"))

	// 【相对简报的修正】MarkSealed 只关句柄，新块由**下一次** Append 建立
	//（writer.go 的 MarkSealed 契约：closeCurLocked 后 cur = nil）。因此这里必须
	// 再写一行，part-000001 才会存在。
	_, err := s.AppendLine("2026-09-24", []byte(line))
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "part-000001.jsonl"),
		"达到 1 KiB 后必须滚动，新块必须是 part-000001（不是复用 000000）")
}

// TestSpoolerRollsOnRollInterval：到滚动间隔（≤1h，默认 1h）⇒ 滚动。
func TestSpoolerRollsOnRollInterval(t *testing.T) {
	s, root := newSpooler(t, 1<<30, 30*time.Millisecond, time.Minute)
	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, s.FlushAndRoll())
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000001.jsonl"))
}

// TestSpoolerDoesNotRollEmptyBlock 是**承重**用例：空块上到点不滚动。
// 若在空块上 MarkSealed，会留下 0 字节 spool；sealer 按文件名照样能发现它，
// 可能封出 0 行产物，使该 part 的所有点读变成「行号越界」（而不是响亮缺失）。
func TestSpoolerDoesNotRollEmptyBlock(t *testing.T) {
	s, root := newSpooler(t, 1<<30, 10*time.Millisecond, time.Minute)
	time.Sleep(40 * time.Millisecond)
	require.NoError(t, s.FlushAndRoll())
	require.NoFileExists(t, spoolPath(root, "2026-09-24"))

	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	require.NoError(t, s.FlushAndRoll())
	require.FileExists(t, spoolPath(root, "2026-09-24"),
		"空块上不得滚动 ⇒ 首次写入必须落在 part-000000")
}

// TestSpoolerRollLeavesCompleteSpoolAndNeverAppendsAgain 固定「先滚动、后封块」的次序
//（规格 §15.1 R8、计划 1 `MarkSealed` 的契约）：滚动之后，sealer 可能随时封块并删除
// spool；若网关仍持有该 part 的 fd 继续追加，写会落进已被 unlink 的 inode ——
// write(2) 返回成功、正文永久不可达（D7）。本用例断言两件事：
//  ① 被滚动的文件在滚动那一刻是**完整**的（最后一行有换行）；
//  ② 之后再 Append，老文件逐字节不变（新内容进 part-000001）。
func TestSpoolerRollLeavesCompleteSpoolAndNeverAppendsAgain(t *testing.T) {
	s, root := newSpooler(t, 1<<10, time.Hour, time.Minute)
	p0 := spoolPath(root, "2026-09-24")
	for i := 0; i < 64; i++ {
		_, err := s.AppendLine("2026-09-24", []byte(`{"pad":"0123456789012345678901234567890123456789"}`))
		require.NoError(t, err)
	}
	require.NoError(t, s.FlushAndRoll())

	rolled, err := os.ReadFile(p0)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(string(rolled), "\n"), "滚动后的 spool 必须是完整行结尾")
	require.NotContains(t, string(rolled), "\x00")

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":"after-roll"}`))
	require.NoError(t, err)
	require.NoError(t, s.FlushAndRoll())

	again, err := os.ReadFile(p0)
	require.NoError(t, err)
	require.Equal(t, rolled, again, "滚动之后老 part 必须逐字节不再变化")
	require.FileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000001.jsonl"))
}

// TestSpoolerRollsOnDTChange 固定跨日行为：dt 变 ⇒ 换到新 dt 目录的 part-000000，
// 老 dt 的 spool 被正常关闭（不隔离、不删除 —— 它是合法正文，由 sealer 按 mtime 封块）。
func TestSpoolerRollsOnDTChange(t *testing.T) {
	s, root := newSpooler(t, 1<<30, time.Hour, time.Minute)
	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	_, err = s.AppendLine("2026-09-25", []byte(`{"a":2}`))
	require.NoError(t, err)
	require.NoError(t, s.FlushAndRoll())

	require.Equal(t, "{\"a\":1}\n", mustRead(t, spoolPath(root, "2026-09-24")))
	require.Equal(t, "{\"a\":2}\n", mustRead(t, spoolPath(root, "2026-09-25")))
}

// TestSpoolerRefusesMissingRoot 是**安全**用例：Root 不存在时构造失败，
// 构造期不做任何隐式创建（require.NoDirExists 盯的就是这一点）。
//
// 【本用例**不能**证明什么 —— 必须说清（登记 D49）】它只覆盖「**启动时**未挂载」。
// 网关依赖的冻结包在**每次建块**时都会自己建目录（`writer.go:489`），
// 因此「**运行中**挂载消失」由 TestSpoolerLatchesWhenRootDisappears 与
// TestSpoolerLatchesWhenRootIdentityChanges 覆盖；几条用例合起来才是完整的边界。
func TestSpoolerRefusesMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	_, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
	})
	require.Error(t, err)
	require.NoDirExists(t, root, "构造失败不得留下自建的目录")
}

// TestSpoolerRefusesFilesystemRoot 固定第二道防线：root 不得是文件系统根
//（第一道在 config.LoadTraceBodyConfig）。"/" 能通过「是个目录」的检查，
// 于是 spool 会建在宿主根文件系统上（/spool/dt=…）。
func TestSpoolerRefusesFilesystemRoot(t *testing.T) {
	for _, bad := range []string{"/", "//", "/./"} {
		_, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
			Root: bad, Inst: "blue", BlockBytes: 1 << 20,
			RollInterval: time.Hour, FlushInterval: time.Minute,
		})
		require.Error(t, err, "root=%q（Clean 后为 /）必须被拒绝", bad)
	}
}

// TestSpoolerLatchesWhenRootDisappears 覆盖**运行中挂载消失**（登记 D49）：
// 复检（周期 tick / 滚动前 / Append 失败后）必须让网关进入闩锁并停止写盘，
// 之后所有行由上层标成 body_missing=true。
//
// 【为什么不依赖真实挂载】用可注入的 RootCheck 表达「根目录消失」，
// 与 SpoolGate 用可注入的水位表达「盘满」是同一手法：等价的是**状态**、
// 不是成因 —— 报告里必须这样写，不得宣称已验证真实 umount。
func TestSpoolerLatchesWhenRootDisappears(t *testing.T) {
	root := t.TempDir()
	gone := false
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootCheck: func(string) error {
			if gone {
				return os.ErrNotExist // 等价于挂载点消失后 stat 失败
			}
			return nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	require.True(t, s.Gate().Allow())

	gone = true
	// 周期 tick（即 FlushAndRoll）必须复检并闩锁
	require.NoError(t, s.FlushAndRoll())
	require.False(t, s.Gate().Allow(), "根目录消失后必须闩锁")
	require.Contains(t, s.Gate().Reason(), "root_unavailable")

	// 闩锁之后不得再写盘：第二行被拒绝，文件仍是 flush 后的内容
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.ErrorIs(t, err, gwobs.ErrSpoolLatched)
	require.Equal(t, "{\"a\":1}\n", mustRead(t, spoolPath(root, "2026-09-24")))
}

// TestSpoolerChecksRootAfterAppendFailure 固定「**任何 Append 失败**都要复检根目录」：
// 这一条把「盘满」与「挂载消失」在闩锁原因上分开 —— 两者都要闩锁，
// 但运维动作完全不同（清空间 vs 重新挂载）。
//
// 【怎么确定性地让 Append 失败】在 `<root>/spool/` 下放一个**普通文件**占住 dt 目录名：
// 冻结包建块路径里的建目录调用会以 ENOTDIR 失败（`writer.go:489`），
// 于是 Append 返回错误、且**不会**毒化任何已有块。
// 这不依赖真实盘满，也不依赖真实 umount（等价的是状态，不是成因）。
func TestSpoolerChecksRootAfterAppendFailure(t *testing.T) {
	root := t.TempDir()
	gone := false
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootCheck: func(string) error {
			if gone {
				return os.ErrNotExist
			}
			return nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)

	// 用普通文件占住下一个 dt 的目录名 ⇒ 下一次 Append 必然失败
	require.NoError(t, os.MkdirAll(filepath.Join(root, "spool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "spool", "dt=2026-09-25"), []byte("x"), 0o644))

	gone = true
	_, err = s.AppendLine("2026-09-25", []byte(`{"a":2}`))
	require.Error(t, err)
	require.False(t, s.Gate().Allow(), "Append 失败后必须闩锁")
	require.Contains(t, s.Gate().Reason(), "root_unavailable",
		"根目录复检失败时必须报 root_unavailable，而不是笼统的 append_failed")
}

// TestSpoolerDoesNotRollWhileRootUnavailable 固定「根目录不可用时不做结构性写入」：
// 滚动本身只关句柄，但紧接着的 Append 会建块 ⇒ 触发冻结包的建块路径，
// 在容器可写层重建 root。所以根目录不可用的那一轮**不滚动**。
//
// 【可观测量是 part 序号】根目录与闸门恢复后，如果那一轮滚过，新行会落在
// part-000001；没滚则仍落在 part-000000。这是该分支唯一的可观测差异，故用它判定
//（否则这条守卫在测试里不可见，会变成一条无法证明承重的分支）。
func TestSpoolerDoesNotRollWhileRootUnavailable(t *testing.T) {
	root := t.TempDir()
	gone := false
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 30,
		RollInterval:  time.Nanosecond, // 任何时刻都已达滚动阈值
		FlushInterval: time.Minute,
		RootCheck: func(string) error {
			if gone {
				return os.ErrNotExist
			}
			return nil
		},
		Gate: lowUsageGate(root), // M5：不依赖宿主真实盘用量
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)

	gone = true
	require.NoError(t, s.FlushAndRoll()) // 已达阈值，但根目录不可用 ⇒ 不滚动
	require.False(t, s.Gate().Allow())

	// 根目录与闸门都恢复：此时若那一轮滚过，新行会进 part-000001
	gone = false
	s.Gate().Tick(time.Now().Add(2 * time.Minute)) // 越过 ProbeInterval，探针会成功
	require.True(t, s.Gate().Allow(), "前置条件：闸门必须已恢复")

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.NoError(t, err)

	require.FileExists(t, spoolPath(root, "2026-09-24"))
	require.NoFileExists(t, filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000001.jsonl"),
		"根目录不可用的那一轮不得滚动")
}

// TestSpoolerRefusesMissingRootEvenWhenIdentityIsObtainable 是**隔离探针**（A5-1）：
// 简报的 TestSpoolerRefusesMissingRoot 杀不死「删掉启动期存在性检查」这条变异 ——
// 路径不存在时身份基线同样取不到，两条检查互相遮蔽（本文件新增这条就是为了拆开）。
// 这里让身份取数恒成功，于是启动失败**只能**由存在性检查提供。
func TestSpoolerRefusesMissingRootEvenWhenIdentityIsObtainable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	_, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootIdentity: func(string) (string, error) { return "dev=1", nil },
	})
	require.Error(t, err, "目录不存在必须启动失败（身份取得到也不行）")
	require.NoDirExists(t, root, "构造失败不得留下自建的目录")
}

// TestSpoolerRefusesRootWithoutIdentityBaseline 固定 A5-1 第 1 点：启动期取不到身份
// 基线 ⇒ **响亮失败**（不能悄悄退化成「只验存在/可写」，那正是 A5-1 要堵的静默错位）。
func TestSpoolerRefusesRootWithoutIdentityBaseline(t *testing.T) {
	root := t.TempDir()
	_, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootIdentity: func(string) (string, error) { return "", errors.New("mountinfo/statfs 不可读") },
	})
	require.Error(t, err, "取不到身份基线必须启动失败")
	require.NoDirExists(t, filepath.Join(root, "spool"), "构造失败不得留下自建的目录")
}

// TestSpoolerLatchesWhenRootIdentityChanges 覆盖**运行中挂载消失**在 A5-1 下的真正判据：
// 挂载消失后 root 仍在（冻结包下一次建块时会在容器可写层把它重建出来）⇒
// stat、IsDir、写探针**全部成功**，只有「身份与启动基线不一致」能识破。
func TestSpoolerLatchesWhenRootIdentityChanges(t *testing.T) {
	root := t.TempDir()
	id := newIdentityStub("mount=/datalog|dev=1:1|fstype=ext4")
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootIdentity: id.fn,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	require.True(t, s.Gate().Allow())

	// 等价于「挂载点消失、路径落到容器可写层」：身份变了，存在性与可写性都还在。
	id.id.Store("mount=/|dev=0:99|fstype=overlay")

	require.NoError(t, s.FlushAndRoll())
	require.False(t, s.Gate().Allow(), "身份变化后必须闩锁")
	require.Contains(t, s.Gate().Reason(), "root-identity-mismatch")

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.ErrorIs(t, err, gwobs.ErrSpoolLatched)
	require.Equal(t, "{\"a\":1}\n", mustRead(t, spoolPath(root, "2026-09-24")),
		"闩锁后不得再把正文写进（可能已错位的）根目录")
}

// TestSpoolerLatchesWhenRootIdentityUnobtainable 固定 A5-1 第 2 点的另一半：
// 身份**取不到**同样必须闩锁（不能因为「读不到」就放行）。
//
// 【隔离设计】注入的身份函数返回**与基线相同的值** + error，因此「忽略 err」的实现
// 会看到 cur == baseline 而不闩锁 ⇒ 这条断言只可能由 err 分支满足。
func TestSpoolerLatchesWhenRootIdentityUnobtainable(t *testing.T) {
	root := t.TempDir()
	id := newIdentityStub("dev=1")
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootIdentity: id.fn,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)

	id.failing.Store(true) // 身份值不变，但取不到了

	require.NoError(t, s.FlushAndRoll())
	require.False(t, s.Gate().Allow(), "取不到身份必须闩锁")
	require.Contains(t, s.Gate().Reason(), "root-identity-mismatch")
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.ErrorIs(t, err, gwobs.ErrSpoolLatched)
}

// TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched 固定 A5-1 第 3 点：
// **解锁探针必须先过同一道身份检查**，否则「挂载仍缺失但容器层可写」时探针会报成功
// ⇒ 网关解锁 ⇒ 正文继续写进容器可写层（静默错位，重新部署后消失）。
//
// 【为什么必须用它而不是只测闩锁】这条分支只在「闩锁 + 探针成功 + 水位正常」时
// 才可见：本条用例把水位与探针都做成「看起来好了」（探针是 SpoolGate 的默认实现，
// 在 t.TempDir() 上会成功），唯一能维持闩锁的就是探针里的身份检查。
//
// 【为什么注入 Gate 而不是用内部默认 Gate（评审 M5）】默认 Gate 的水位来自真实 statfs，
// 解锁那一步会依赖「宿主盘用量 < 90%」⇒ 盘用量高的机器上假红；这里注入水位恒为 10% 的
// Gate。**注意**：注入 Gate 不改变被验证的行为 —— 身份包装对注入的 Gate 同样生效
// （这正是 TestSpoolerInjectedGateProbeStillRequiresIdentity 单独盯住的那一点）。
func TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched(t *testing.T) {
	root := t.TempDir()
	id := newIdentityStub("mount=/datalog|dev=1:1|fstype=ext4")
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootIdentity: id.fn,
		Gate:         lowUsageGate(root), // M5：不依赖宿主真实盘用量
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)

	id.id.Store("mount=/|dev=0:99|fstype=overlay")
	require.NoError(t, s.FlushAndRoll())
	require.False(t, s.Gate().Allow())

	// 探针（默认实现）在 root 上会成功、注入的水位是 10%（远低于 90%）：
	// 若探针不先验身份，网关就会在这里解锁。
	s.Gate().Tick(time.Now().Add(2 * time.Minute))
	require.False(t, s.Gate().Allow(), "身份不一致时解锁探针必须失败（A5-1 第 3 点）")

	// 身份恢复（例如挂载被重新挂上）⇒ 探针成功 + 水位回落 ⇒ 才允许解锁。
	id.id.Store("mount=/datalog|dev=1:1|fstype=ext4")
	s.Gate().Tick(time.Now().Add(4 * time.Minute))
	require.True(t, s.Gate().Allow(), "身份恢复且探针成功 ⇒ 必须能解锁")
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.NoError(t, err)
}

// TestSpoolerLatchesOnAppendFailureWhileRootHealthy 固定「写失败必须闩锁」这一支
// （简报 m07 的杀手）：与 TestSpoolerChecksRootAfterAppendFailure 的分工是 ——
// 那条用例让根目录复检**先失败**（于是提前 return，永远到不了写失败分支 ⇒ 零覆盖），
// 本条让根目录保持健康，使 append_failed 有独立观察者。
func TestSpoolerLatchesOnAppendFailureWhileRootHealthy(t *testing.T) {
	s, root := newSpooler(t, 1<<20, time.Hour, time.Minute)

	// 让建块失败但根目录健康：把 `<root>/spool` 占成一个普通文件 ⇒
	// 冻结包建块路径里的建目录调用以 ENOTDIR 失败（`writer.go:489`），
	// Append 报错，且不会毒化任何已有块（此时还没有块）。
	require.NoError(t, os.WriteFile(filepath.Join(root, "spool"), []byte("x"), 0o644))

	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.Error(t, err)
	require.False(t, s.Gate().Allow(), "写失败必须闩锁")
	require.Contains(t, s.Gate().Reason(), "append_failed",
		"根目录健康时的写失败必须以 append_failed 闩锁（与 root_unavailable 分开：运维动作不同）")
}

// TestSpoolerRunDefersProbeToWriteGoroutine 固定 A5-4：SpoolGate.Tick 持同一把锁执行
// 探针（CreateTemp + write + fsync + unlink），而热路径 Allow() 取同一把锁 ⇒
// Run **不得**在自己的 goroutine 上调用 Tick，否则每分钟一次的 fsync 会堵住
// 网关写路径（p99）。本用例把周期压到 5ms 跑一会儿：Run 侧探针必须一次都没跑，
// 直到写协程入口 AppendLine 消费「到期」标志。
//
// 【有意延迟解锁】写协程空闲时闩锁保持、标志保留；恢复流量后由首个 AppendLine
// 完成探针与解锁（空闲时没有待写的行，解锁早晚不影响正确性）。
func TestSpoolerRunDefersProbeToWriteGoroutine(t *testing.T) {
	root := t.TempDir()
	p := &atomicProbe{err: errors.New("probe down")}
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: 5 * time.Millisecond,
		Gate: gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
			Root:                 root,
			DiskWatermarkPercent: 90,
			ProbeInterval:        5 * time.Millisecond,
			UsagePercent:         func(string) (float64, error) { return 95, nil }, // 恒超水位 ⇒ 必闩锁
			ProbeWrite:           p.probe,
		}),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()

	// Run 已跑过很多轮 ticker：若 Tick 在 Run 的 goroutine 上执行，探针会持锁 fsync。
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int32(0), p.calls.Load(), "Run 不得在自己的 goroutine 上执行探针（A5-4）")

	// 写协程入口消费到期标志：未闩锁分支只评估水位（95 ≥ 90）⇒ 闩锁，不跑探针。
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.ErrorIs(t, err, gwobs.ErrSpoolLatched)
	require.False(t, s.Gate().Allow())

	// 闩锁期间 Run 又跑了若干轮：到期探针同样只能由写协程消费（有意延迟解锁）。
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int32(0), p.calls.Load(), "闩锁中的到期探针同样只能由写协程消费")

	// 第二个 AppendLine 消费一次到期标志：距闩锁已远超 ProbeInterval ⇒ 探针恰跑一次。
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.ErrorIs(t, err, gwobs.ErrSpoolLatched)
	require.Equal(t, int32(1), p.calls.Load(), "到期探针必须在写协程入口执行，且恰好一次")

	cancel()
	<-done
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}

// TestSpoolerInjectedGateProbeStillRequiresIdentity 固定 A5-1 第 3 点在**注入 Gate**
// 时同样成立（评审 I2）。
//
// 【为什么单独一条】身份包装若只做在「内部构造 Gate」的分支里，Task 6 为了尊重配置
// 水位而自带 Gate（并自带一个会成功的探针）时，探针里就没有身份检查；而 identityErr
// 是构造期闭包 ⇒ Task 6 自建不出等价探针。后果：「挂载缺失但容器可写」时探针成功 +
// 水位正常 ⇒ 解锁 ⇒ 正文写进容器可写层，并按 ProbeInterval **反复**发生 ——
// 正是 A5-1 要堵的静默错位。
//
// 【本用例与 TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched 的分工】那条
// 覆盖「身份检查本身存在」，本条覆盖「**包装无条件生效**」（去掉无条件包装后，
// 只有本条会红：那条也注入了 Gate，但它的断言在身份检查存在时仍成立）。
func TestSpoolerInjectedGateProbeStillRequiresIdentity(t *testing.T) {
	root := t.TempDir()
	id := newIdentityStub("mount=/datalog|dev=1:1|fstype=ext4")
	probes := 0
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootIdentity: id.fn,
		// Task 6 的典型注入：配置水位 + 自带探针（这里让它恒成功，模拟
		// 「容器可写层里写探针会成功」）。
		Gate: gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
			Root:                 root,
			DiskWatermarkPercent: 90,
			ProbeInterval:        time.Minute,
			UsagePercent:         func(string) (float64, error) { return 10, nil },
			ProbeWrite: func(string) error {
				probes++
				return nil
			},
		}),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)

	// 等价于「挂载点消失、路径落到容器可写层」：身份变了，存在性与可写性都还在。
	id.id.Store("mount=/|dev=0:99|fstype=overlay")
	require.NoError(t, s.FlushAndRoll())
	require.False(t, s.Gate().Allow())

	s.Gate().Tick(time.Now().Add(2 * time.Minute))
	require.False(t, s.Gate().Allow(),
		"注入 Gate 的探针也必须先验身份：否则「挂载缺失 + 容器层可写」会解锁（A5-1 静默错位）")

	// 身份恢复（例如挂载被重新挂上）⇒ 注入的探针成功 + 水位正常 ⇒ 才允许解锁。
	id.id.Store("mount=/datalog|dev=1:1|fstype=ext4")
	s.Gate().Tick(time.Now().Add(4 * time.Minute))
	require.True(t, s.Gate().Allow(), "身份恢复后注入的探针成功 + 水位正常 ⇒ 必须能解锁")
	require.Greater(t, probes, 0, "解锁那一次必须真的调用过注入的探针")
}

// TestSpoolerAppendAfterCloseDoesNotRecreatePart 固定「Close 之后不得再写」（评审 M3）。
//
// 【为什么需要本任务自己挡】冻结包的 Writer.Close 只是关掉当前句柄并把 cur 置 nil，
// **没有** closed 标志；此后 AppendLine 会被当成「正常的新块」重新建 part ⇒
// 一个已经封口的 spool 被静默续写，而调用方以为它不再变化。
func TestSpoolerAppendAfterCloseDoesNotRecreatePart(t *testing.T) {
	s, root := newSpooler(t, 1<<30, time.Hour, time.Minute)
	_, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	require.NoError(t, s.FlushAndRoll())
	require.NoError(t, s.Close())
	require.NoError(t, s.Close(), "Close 必须幂等（关停编排与 t.Cleanup 各调一次）")

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.ErrorIs(t, err, gwobs.ErrSpoolLatched, "Close 之后不得再写")

	dir := filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue")
	require.NoFileExists(t, filepath.Join(dir, "part-000001.jsonl"), "Close 之后不得再建 part")
	require.Equal(t, "{\"a\":1}\n", mustRead(t, spoolPath(root, "2026-09-24")),
		"Close 之后的写入不得落到盘上")
}

// TestSpoolerLatchLogsExactlyOncePerTransition 固定规格 §9.3 的降级可观测性：
// 闩锁**跳变那一刻恰好一条 ERROR**，闩锁期间**不再重复**打日志。
//
// 【为什么这一条必须存在】SpoolGate.Trip 的契约把「打日志」交给调用方（它返回
// 「本次是否跳变」），而 checkRoot 每个 tick、每次 Append 失败都会跑 ⇒ 无条件打日志会
// 把日志淹没（§9.3 要的是信号，不是噪音）。反过来，一条都不打则跳变时刻完全没有信号
// （Run 只在 FlushAndRoll 返错时打日志，而根目录不可用那一轮返回 nil）。
func TestSpoolerLatchLogsExactlyOncePerTransition(t *testing.T) {
	root := t.TempDir()
	gone := false
	s, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		RootCheck: func(string) error {
			if gone {
				return os.ErrNotExist
			}
			return nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	require.Empty(t, logLines(buf.String()), "未闩锁时不得有闩锁 ERROR")

	gone = true
	require.NoError(t, s.FlushAndRoll())
	lines := logLines(buf.String())
	require.Len(t, lines, 1, "跳变时必须恰好一条 ERROR（规格 §9.3：要信号不要噪音）")
	require.Contains(t, lines[0], "ERROR")
	require.Contains(t, lines[0], "root_unavailable")

	// 闩锁期间：每个 tick 都复检、每行都被拒绝 —— 都不得再打日志
	require.NoError(t, s.FlushAndRoll())
	require.NoError(t, s.FlushAndRoll())
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.ErrorIs(t, err, gwobs.ErrSpoolLatched)
	require.Len(t, logLines(buf.String()), 1, "闩锁期间不得重复打日志（噪音）")
}

// ---------------------------------------------------------------------------
// 关停竞态（评审必修 1）：spool 的生命周期不归 Run，归关停编排
// ---------------------------------------------------------------------------

// TestSpoolerRunDoesNotCloseOnContextCancel 固定「Run 退出**不**关闭 spool」这条契约。
//
// 【为什么必须有它】关停时 `main` 的 `cancel()` 会同时唤醒 spooler 的 Run 与写入侧的
// Run；写入侧还要用 WithoutCancel 补写最后一批（≤flushN 条 / ≤2s）。Run 若在 ctx.Done
// 里 Close，这一批就会撞上 AppendLine 的 closed 检查 ⇒ 健康盘上的最后一批全部
// body_missing=true，并且经 noteSpoolFailure 打一条「正文落盘已闩锁」的 ERROR
//（每次优雅重启都可能发生的假磁盘降级告警）。次序必须由调用方编排：
// cancel → 等 (*TraceWriter).Done() → Close。
//
// 【确定性】本用例先等 Run 的包装 goroutine 真正返回（`<-done`），再调 AppendLine ——
// 不存在「谁先跑」的调度概率：旧实现（Run 里 Close）在这里必然已置 closed。
func TestSpoolerRunDoesNotCloseOnContextCancel(t *testing.T) {
	// 用 newTestSpooler（注入闸门、固定水位 1%）：避免把「能解锁」押在宿主磁盘用量上。
	s := newTestSpooler(t, t.TempDir(), "blue")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runReturned := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runReturned)
	}()

	cancel()
	select {
	case <-runReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("Run 未在预算内退出")
	}

	ref, err := s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err,
		"ctx 取消后 spool 必须仍可写：Run 不得自行 Close（生命周期归关停编排，排空后才关）")
	require.Equal(t, "blue", ref.Inst)
}

// shutdownGateSink 是把「spooler 先退出、写入侧的最后一批随后到达」这个交错变成
// **确定性事实**的探针：它在第一次追加之前先等 spooler 的 Run 真正返回。
//
// 于是旧实现（Run 在 ctx.Done 里 Close）下，这一批必然撞上 closed 检查 ——
// 不是「写入侧先跑就绿」的调度概率。
type shutdownGateSink struct {
	gwobs.TraceBodySink // DayStart / DayOf 委托给真实组装物（生产日期入口）

	spoolerRunDone <-chan struct{}

	mu       sync.Mutex
	appends  int
	nonNilEr []error
}

func (s *shutdownGateSink) AppendLine(dt string, line []byte) (ta.Ref, error) {
	select {
	case <-s.spoolerRunDone:
	case <-time.After(5 * time.Second):
		// 预算内 spooler 的 Run 没退出 ⇒ 关停编排失效。返回错误让用例以断言红暴露，
		// 而不是把整包拖成 30s 超时红。
		return ta.Ref{}, errors.New("spooler.Run 未在预算内退出（关停编排失效）")
	}
	ref, err := s.TraceBodySink.AppendLine(dt, line)
	s.mu.Lock()
	s.appends++
	if err != nil {
		s.nonNilEr = append(s.nonNilEr, err)
	}
	s.mu.Unlock()
	return ref, err
}

func (s *shutdownGateSink) appendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appends
}

func (s *shutdownGateSink) appendErrors() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.nonNilEr...)
}

// TestShutdownFlushIsNotLatchedBySpoolerRun 是关停竞态的**端到端**确定性用例
//（评审必修 1 要求的「cancel 同一 ctx 后，最后一批追加不是 ErrSpoolLatched、
// 且这些 PG 行没有 body_missing=true」）。
//
// 【确定性从哪来（两层，都不靠调度概率）】
//  ① 写入器用**无缓冲** channel（bufSize=0）：`Emit` 的发送与写协程的接收同步，
//     Emit 返回即代表该事件已被取进 batch；`flushN=50` / `interval=1m` ⇒ 它只能由
//     `ctx.Done` 分支的关停 flush 写出（该分支用 WithoutCancel，故 PG 插入不会因
//     取消而失败）。
//  ② 追加被 shutdownGateSink 押在「spooler 的 Run 已返回」之后 ⇒ 旧实现里 spooler
//     必然已 Close，最后一批必然被拒绝。
func TestShutdownFlushIsNotLatchedBySpoolerRun(t *testing.T) {
	root := t.TempDir()
	spooler := newTestSpooler(t, root, "blue")

	spoolerRunDone := make(chan struct{})
	sink := &shutdownGateSink{
		TraceBodySink:  &sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}},
		spoolerRunDone: spoolerRunDone,
	}

	db, _ := newSpoolTestDB(t, &orderRecorder{})
	w := gwobs.NewTraceWriterWithSink(db, 0, 50, time.Minute, gwobs.TraceWriterLimits{}, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		spooler.Run(ctx)
		close(spoolerRunDone)
	}()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{
		RequestID: "req-shutdown", TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`),
	})
	cancel()

	select {
	case <-w.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("写入侧未在预算内排空（Done 未关闭）：关停编排失效")
	}

	require.Equal(t, 1, sink.appendCount(), "最后一批必须交给 sink（否则下面的断言是空的）")
	require.Empty(t, sink.appendErrors(),
		"健康盘上关停的最后一批不得被 spooler 拒绝（ErrSpoolLatched）")
	require.NoError(t, spooler.Close())

	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "req-shutdown").Error)
	require.False(t, row.BodyMissing,
		"健康盘上关停的最后一批不得降级为 body_missing=true（旧实现：Run 先 Close ⇒ 断言在这里红）")
	require.NotNil(t, row.BodyRefPart)
	require.NotNil(t, row.BodyRefRow)
	require.Equal(t, "blue", *row.BodyRefInst)
}
