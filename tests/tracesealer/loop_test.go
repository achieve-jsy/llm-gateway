package tracesealer_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 常驻循环（runner.go 的 Loop）的在册行为证据
//
// 【为什么轮次数必须由身份探针来数，而不是由封块器来数】根目录不可用的那一轮，
// Loop **根本不会**调用 ListPendingSeals（护栏在发现之前就拦下了），因此以
// 「封块器被问了几次」计轮，在正是需要它工作的时候恒为 0 —— 那条断言会变成
// 「怎么都过不去」或「怎么都过得去」的假断言。`Check()` 每轮恰好调用一次身份探针，
// 所以「探针次数 - 1 次启动基线」就是**轮次数**，护栏坏没坏都成立。
// ---------------------------------------------------------------------------

// countingSealer 是 loop 用例的替身封块器：它让「同时有几块在飞」可观察
// （轮次一旦并行，maxInFlight 必然 > 1），并提供 onSeal 钩子（用来在封第一块时取消
// ctx —— 那正是关机的形态）。零值可用：缺省一轮一块，块本身不做任何事。
type countingSealer struct {
	tasks     []ta.SealTask // nil ⇒ 默认 [mkTask(0)]
	sealDelay time.Duration
	onSeal    func()

	mu        sync.Mutex
	seals     int
	inFlight  int
	maxFlight int
}

func (c *countingSealer) ListPendingSeals() ([]ta.SealTask, error) {
	if c.tasks != nil {
		return c.tasks, nil
	}
	return []ta.SealTask{mkTask(0)}, nil
}

func (c *countingSealer) Seal(_ context.Context, _ ta.SealTask) error {
	c.mu.Lock()
	c.seals++
	c.inFlight++
	if c.inFlight > c.maxFlight {
		c.maxFlight = c.inFlight
	}
	c.mu.Unlock()
	// 【为什么用 sleep 而不是「什么都不做」】封块是慢操作，轮次并行的证据只有在
	// 「一块还在飞的时候下一轮已经开跑」时才可观察。
	if c.sealDelay > 0 {
		time.Sleep(c.sealDelay)
	}
	if c.onSeal != nil {
		c.onSeal()
	}
	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	return nil
}

func (c *countingSealer) sealCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seals
}

func (c *countingSealer) maxInFlight() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxFlight
}

// loopHarness 把常驻循环与它的两个可观测面拢在一起：**轮次**（身份探针计数）与
// **封块**（countingSealer）。身份探针是本包在册的可注入点（Options.Identity），
// 不是给 Runner 加的测试专用方法。
type loopHarness struct {
	r *tracesealer.Runner

	mu     sync.Mutex
	probes int
	broken bool
}

// identity 是注入的根目录身份探针：每次调用记一笔（含构造期那一次启动基线），
// breakMount 之后返回一个与基线不同的串 —— 这就是「运行中挂载被撤掉」。
func (h *loopHarness) identity(string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.probes++
	if h.broken {
		return "broken-mount|0:0|none", nil
	}
	return "baseline-mount|8:1|ext4", nil
}

// breakMount 把身份探针换成「挂载已经不是启动时那一个」。
func (h *loopHarness) breakMount() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broken = true
}

// rounds = 循环跑过的轮次数 = 探针次数 - 1（构造期建立启动基线的那一次）。
func (h *loopHarness) rounds() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.probes - 1
}

func (h *loopHarness) Loop(ctx context.Context) error { return h.r.Loop(ctx) }

// Round 直接暴露一轮（不入循环）：让「护栏失败的那一轮汇总长什么样」可断言。
func (h *loopHarness) Round(ctx context.Context) (tracesealer.RoundResult, error) {
	return h.r.Round(ctx)
}

// newLoopRunner 造一个常驻循环的 Runner：短轮询周期、注入身份探针与替身封块器，
// 日志落到 io.Discard（本文件的断言面是轮次与封块，不是日志）。
func newLoopRunner(t *testing.T, f *countingSealer, poll time.Duration) *loopHarness {
	t.Helper()
	h := &loopHarness{}
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root:         t.TempDir(),
		PollInterval: poll,
		Identity:     h.identity,
		Slog:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		NewSealer: func(ta.SealerOptions) (tracesealer.Sealer, error) {
			return f, nil
		},
	})
	require.NoError(t, err)
	h.r = r
	return h
}

// TestLoopRunsFirstRoundImmediately 启动即跑第一轮（不是先等一个 poll 周期）：
// 否则「配置写错了」这件事要等到下一个周期才会被说出来，而积压也白等一轮。
func TestLoopRunsFirstRoundImmediately(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, time.Hour) // poll 1h：只有「立即跑」才能观察到第一轮
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.GreaterOrEqual(t, r.rounds(), 1)
}

// TestLoopRepeatsRoundsOnPollInterval 周期到点必须继续跑。
func TestLoopRepeatsRoundsOnPollInterval(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(120 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.GreaterOrEqual(t, r.rounds(), 3, "5ms 周期跑 120ms 至少应有 3 轮")
}

// TestLoopStopsOnContextCancel 取消后必须干净返回，且**不再**有新轮次。
func TestLoopStopsOnContextCancel(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	n := r.rounds()
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, n, r.rounds(), "取消之后不得再跑轮次")
}

// TestLoopKeepsRunningWhenRootIsUnavailable 根目录不可用是**环境级**问题：
// 进程必须继续活着（退出会被 systemd 按 RestartSec 反复拉起，重试更密，
// 而重启不会把挂载变回来），靠「每轮一条汇总」的心跳让运维看见。
func TestLoopKeepsRunningWhenRootIsUnavailable(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, 5*time.Millisecond)
	r.breakMount() // 用注入的 Identity 让每轮复检都失败
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.GreaterOrEqual(t, r.rounds(), 3, "根目录不可用时循环仍必须继续（且不 Seal）")
	require.Zero(t, f.sealCalls(), "根目录不可用时**不得**调用 Seal")
}

// TestLoopStopsSealingAfterCancel 取消之后**不得**再开新块：关机时「少封一块」无害
// （下一轮会重新发现），「封到一半被杀」才会留下 .tmp。
//
// 【为什么这条在 Loop 侧也必须存在】Task 1 的 TestRoundCancellationStopsBeforeNextSeal
// 钉住的是 Round 自己；而运维真正的关机路径是 SIGTERM → ctx 取消 → **Loop 里的某一轮**。
// 本用例让第一块的封块过程触发取消，因此「取消后是否还会开第二块」在 Loop 侧可观察。
func TestLoopStopsSealingAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &countingSealer{tasks: []ta.SealTask{mkTask(0), mkTask(1)}}
	f.onSeal = func() { cancel() }
	r := newLoopRunner(t, f, 5*time.Millisecond)
	require.NoError(t, r.Loop(ctx))
	require.Equal(t, 1, f.sealCalls(), "取消之后不得再开始封下一块")
}

// TestLoopReportsRootUnavailable 护栏失败的那一轮必须把 `RootUnavailable` 标出来，
// 并且**错误是身份不符**（不是「本轮没活干」）。
//
// 【为什么这条在册】「盘掉了」与「今天没有可封块」在汇总里必须是两件事：若共用零值，
// 看板会把一次环境级故障读成正常空轮次（而它要人去看挂载）。日志与将来的看板都按这个
// 字段分流，所以它是一条**行为**要求，而不是「字段碰巧被赋值」。
func TestLoopReportsRootUnavailable(t *testing.T) {
	f := &countingSealer{}
	r := newLoopRunner(t, f, time.Hour)
	r.breakMount()
	res, err := r.Round(context.Background())
	require.ErrorIs(t, err, tracesealer.ErrRootIdentityMismatch)
	require.True(t, res.RootUnavailable, "根目录不可用的那一轮必须在汇总里标出来")
	require.Zero(t, f.sealCalls(), "护栏失败的一轮不得调用 Seal")
}

// TestLoopNeverOverlapsRounds 轮次必须串行：并发的两个 Round 会对同一 (dt,inst,part)
// 同时 Seal，而冻结包明确要求同一个三元组串行（共用同一个 .tmp）。
func TestLoopNeverOverlapsRounds(t *testing.T) {
	f := &countingSealer{sealDelay: 20 * time.Millisecond}
	r := newLoopRunner(t, f, 1*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	require.NoError(t, r.Loop(ctx))
	require.Equal(t, 1, f.maxInFlight(), "同时只能有一轮在跑")
}
