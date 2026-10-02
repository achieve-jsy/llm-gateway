package tracesealer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// A7 自动化部分的在册行为证据（真实冻结包 Writer + 替身 sealer）
//
// 【为什么用替身 sealer 而不是真实 Seal】真封块要本机有 zstd，缺失时用例只能
// SKIP —— 而本仓库的变异工具要求全层级 SKIP == 0，一个 requireZstd 式的跳过会让
// 整批证据判为无效。次序（D7 的机制）在这里被钉住；真实 zstd 路径由计划 1 的
// tests/tracearchive/ 覆盖，进程级由任务 8 的剧本覆盖。
// ---------------------------------------------------------------------------

// handoffSealer 模拟冻结包 Seal 成功后的两个副作用（写产物、删 spool），顺序与
// 冻结包一致（先建产物再删 spool：反过来会让中途崩溃时两边都没有）。
type handoffSealer struct {
	root string
	// handed: "dt/inst/part" -> 交给它时的行数（SealTask.Rows）
	handed map[string]int
}

// artifactPathFromSpool 由 spool 路径推出产物路径：去掉 "spool/" 这一层、把扩展名
// 换成 .jsonl.zst（<root>/spool/dt=D/inst=I/part-N.jsonl → <root>/dt=D/inst=I/part-N.jsonl.zst）。
// 用 task.SpoolPath 推导（而不是另算一份）是因为冻结包要求两者同址同号。
func artifactPathFromSpool(root, spoolPath string) string {
	rel := strings.TrimPrefix(spoolPath, root+string(os.PathSeparator))
	rel = strings.TrimPrefix(rel, "spool"+string(os.PathSeparator))
	return filepath.Join(root, strings.TrimSuffix(rel, ".jsonl")+".jsonl.zst")
}

// handoff 的三个动作与冻结包 Seal 的成功路径同序：建产物目录（替身；真实的
// MkdirAll 在冻结包 sealer.go 里）→ 写一个占位产物 → 删 spool。
func (h *handoffSealer) handoff(task ta.SealTask) error {
	art := artifactPathFromSpool(h.root, task.SpoolPath)
	if err := os.MkdirAll(filepath.Dir(art), 0o755); err != nil {
		return fmt.Errorf("建产物目录失败: %w", err)
	}
	if err := os.WriteFile(art, []byte("placeholder\n"), 0o644); err != nil {
		return fmt.Errorf("写占位产物失败: %w", err)
	}
	if err := os.Remove(task.SpoolPath); err != nil {
		return fmt.Errorf("删 spool 失败: %w", err)
	}
	if h.handed == nil {
		h.handed = map[string]int{}
	}
	h.handed[fmt.Sprintf("%s/%s/part-%06d", task.DT, task.Inst, task.Part)] = task.Rows
	return nil
}

// assertRefsLand 逐个 Ref 判定：产物文件存在 ⇒ OK（已交接）；否则 spool 文件存在
// 且行数 > row ⇒ OK（未交接，正文还在 spool 里）；两者皆无 ⇒ 「Ref 丢了」。
func (h *handoffSealer) assertRefsLand(refs []ta.Ref) error {
	p := ta.NewPaths(h.root)
	for _, ref := range refs {
		if _, err := os.Stat(p.PartFile(ref)); err == nil {
			continue
		}
		data, err := os.ReadFile(p.SpoolFile(ref))
		if err != nil {
			return fmt.Errorf("Ref %s 丢了：产物与 spool 都不在（%v）", ref, err)
		}
		lines := strings.Count(string(data), "\n")
		if lines <= ref.Row {
			return fmt.Errorf("Ref %s 丢了：spool 只有 %d 行（需要 > %d 行）", ref, lines, ref.Row)
		}
	}
	return nil
}

// sealOnce 走一遍「PendingSeals → MarkSealed → handoff」。次序本身是 N1/R8 的
// 不变量：先 MarkSealed 滚掉旧 part（关 fd），sealer 才可能遇到它。反过来就是
// D7 —— sealer 删掉 spool 后网关仍持有 fd，追加写进已 unlink 的 inode
// （落盘成功、永久不可达）。
func sealOnce(t *testing.T, w *ta.Writer, hs *handoffSealer) ta.SealTask {
	t.Helper()
	// ① PendingSeals 是「把块交给 sealer」的入口，它**无条件 flush** 当前块
	//    （冻结包 writer.go:353-380）：漏 flush 会让 sealer 少读行，因此
	//    flush 失败必须上抛 ≠ nil 返回，这里 require.NoError 就是在钉这一点。
	tasks, err := w.PendingSeals()
	require.NoError(t, err)
	require.Len(t, tasks, 1, "BlockBytes=1 ⇒ 每轮必有一块待封")
	task := tasks[0]
	// ② 【冻结契约】MarkSealed(dt string, part int) **无返回值**（writer.go:420）⇒
	//    不得写成 require.NoError(t, ...)。
	w.MarkSealed(task.DT, task.Part)
	// ③ 最后才交接。
	require.NoError(t, hs.handoff(task))
	return task
}

// newA7Writer 造真实冻结包 Writer：BlockBytes=1 ⇒ PendingSeals 的「按大小滚动」
// 条件恒真（Append **不会**自己滚动：滚动只由调用方的 PendingSeals + MarkSealed
// 触发）；SealInterval=1h 只是让它不因时间阈值滚动而干扰用例的确定性。
func newA7Writer(t *testing.T, root string) *ta.Writer {
	t.Helper()
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1,
		SealInterval: time.Hour})
	require.NoError(t, err)
	return w
}

const a7dt = "2026-10-08"

func a7line(i int) []byte {
	return []byte(fmt.Sprintf(`{"i":%d,"pad":"%s"}`, i, strings.Repeat("x", 64)))
}

// TestA7NoRefPointsToNothing 每一个已返回的 Ref 都必须有落点：产物在场（已交接）
// 或 spool 在场且该行存在（未交接）。
//
// 【零变异杀手，如实登记】本集合的 TARGET 是 lock.go，而本用例的被测对象是**冻结包
// Writer 的次序**（本计划不许变异冻结包）⇒ 它没有变异杀手，按 E64② 登记进集合的
// COVERAGE_GAPS，其可失败性由人工探针承载（见集合文档串）。
func TestA7NoRefPointsToNothing(t *testing.T) {
	root := t.TempDir()
	w := newA7Writer(t, root)
	hs := &handoffSealer{root: root}
	refs := make([]ta.Ref, 0, 200)
	for i := 0; i < 200; i++ {
		ref, err := w.Append(a7dt, a7line(i))
		require.NoError(t, err)
		refs = append(refs, ref)
		// 最后一轮**不交接**：让「未交接的 Ref 靠 spool 行数落点」那条分支也被走到
		// （每条分支都只有一个承重形态时，另一条会变成恒真的空断言）。
		if i%40 == 39 && i < 160 {
			_ = sealOnce(t, w, hs)
		}
	}
	require.NoError(t, w.Close())
	require.Len(t, hs.handed, 4, "200 次 Append / 每 40 次交接一次 ⇒ 恰好 4 次交接")
	require.NoError(t, hs.assertRefsLand(refs))
}

// TestA7WriterNeverAppendsToAHandedOffPart 一旦某个 part 被交给 sealer（= 网关已
// MarkSealed 关掉 fd），后续 Append **绝不允许**再落到这个 part 上。
// 这正是 N1/R8 的次序不变量：先 MarkSealed、后交接。反过来写就是 D7
// （数据写进已被 unlink 的 inode：落盘成功、永久不可达）。
//
// **注意 30s 批次预算**：总循环 200 次 Append 是刻意的上界，不要再加。
func TestA7WriterNeverAppendsToAHandedOffPart(t *testing.T) {
	root := t.TempDir()
	w := newA7Writer(t, root)
	hs := &handoffSealer{root: root}
	sealed := map[int]bool{}
	for i := 0; i < 200; i++ {
		ref, err := w.Append(a7dt, a7line(i))
		require.NoError(t, err)
		require.False(t, sealed[ref.Part], "part %d 已交接给 sealer，不得再被追加（D7）", ref.Part)
		if i%40 == 39 && i < 160 {
			task := sealOnce(t, w, hs)
			sealed[task.Part] = true
		}
	}
	require.NoError(t, w.Close())
	require.Len(t, sealed, 4, "4 次交接必须真的发生过，否则上面的断言是恒真的空断言")
}

// TestA7RestartAfterSealStartsAtNextPart 进程重启（新 Writer，同一 root）在
// **产物已存在、spool 已删**之后必须从 part+1 继续（冻结包的目录扫描发现逻辑）。
//
// 【零变异杀手】同 TestA7NoRefPointsToNothing：被测对象是冻结包的目录扫描。
func TestA7RestartAfterSealStartsAtNextPart(t *testing.T) {
	root := t.TempDir()
	w1 := newA7Writer(t, root)
	hs := &handoffSealer{root: root}
	for i := 0; i < 40; i++ {
		ref, err := w1.Append(a7dt, a7line(i))
		require.NoError(t, err)
		require.Equal(t, 0, ref.Part)
	}
	task := sealOnce(t, w1, hs)
	require.Equal(t, 0, task.Part)
	require.NoError(t, w1.Close())

	p := ta.NewPaths(root)
	zero := ta.Ref{DT: a7dt, Inst: "blue", Part: 0}
	require.FileExists(t, p.PartFile(zero), "产物必须已由 handoff 落盘")
	_, err := os.Stat(p.SpoolFile(zero))
	require.True(t, os.IsNotExist(err), "交接后 spool 必须已删，实际 err=%v", err)

	w2 := newA7Writer(t, root)
	ref, err := w2.Append(a7dt, a7line(0))
	require.NoError(t, err)
	require.Equal(t, 1, ref.Part, "重启后必须从 part+1 继续（产物已存在 ⇒ 序号 0 已被占住）")
	require.NoError(t, w2.Close())
}

// TestA7RoundIsSerializedWithLock 常驻循环持锁期间，第二个 Runner（模拟手工 --once）
// 必须被拒。
//
// 【杀手】集合 p4-task-4-runner 的 m06（NewRunner 不再取锁）与集合 p4-task-4-lock 的
// m02/m03（排他变共享 / 不再返回哨兵错误）。
func TestA7RoundIsSerializedWithLock(t *testing.T) {
	root := t.TempDir()
	r1, err := tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.NoError(t, err)
	defer r1.Close()
	_, err = tracesealer.NewRunner(tracesealer.Options{Root: root, NewSealer: stubSealer})
	require.ErrorIs(t, err, tracesealer.ErrAnotherSealerRunning,
		"常驻循环持锁期间，运维手工 --once 必须被装配期挡住")
}

// a7RecordingSealer 记录「每个 part 被封了几次」与「同一时刻最多有几块在飞」。
type a7RecordingSealer struct {
	mu          sync.Mutex
	perPart     map[int]int
	inFlight    int
	maxInFlight int
}

func (s *a7RecordingSealer) ListPendingSeals() ([]ta.SealTask, error) {
	return []ta.SealTask{mkTask(0), mkTask(1), mkTask(2)}, nil
}

func (s *a7RecordingSealer) Seal(_ context.Context, task ta.SealTask) error {
	s.mu.Lock()
	if s.perPart == nil {
		s.perPart = map[int]int{}
	}
	s.perPart[task.Part]++
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.mu.Unlock()
	// 封块是慢操作：两个 Round 真的并行时，这里才观察得到 inFlight > 1。
	time.Sleep(2 * time.Millisecond)
	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return nil
}

// TestA7NoPartIsSealedTwiceConcurrently 一个轮次内对同一 part 只能有一次 Seal 调用
// （冻结包要求同一个 (dt,inst,part) 串行：它们共用同一个 <partPath>.tmp）。
//
// 【零变异杀手，如实登记】本集合两个 TARGET（lock.go / runner.go）的变异都不改变
// Round 的串行性 ⇒ 本用例没有变异杀手；可失败性由人工探针承载（在 runner.go 的
// Round 里把 Seal 改成 `go func` 启动 ⇒ 本条在 maxInFlight 断言处转红，见集合
// p4-task-4-runner 的 COVERAGE_GAPS）。
func TestA7NoPartIsSealedTwiceConcurrently(t *testing.T) {
	root := t.TempDir()
	f := &a7RecordingSealer{}
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root: root,
		NewSealer: func(ta.SealerOptions) (tracesealer.Sealer, error) {
			return f, nil
		},
	})
	require.NoError(t, err)
	defer r.Close()
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, res.Sealed)
	require.Len(t, f.perPart, 3)
	// 【断言次序】先证「同一时刻只有一块在封」再证「每个 part 只封一次」：两者由同一条
	// 用例承载，但它们**各自**可被不同的失效形态打红（前者 = 轮次/封块并行，后者 =
	// 对同一 part 重复调用 Seal）⇒ 两半都必须能被单独观察到（见集合 p4-task-4-runner
	// 的 COVERAGE_GAPS 里两条探针）。
	require.Equal(t, 1, f.maxInFlight, "同一个三元组必须串行：不得有两块同时在封")
	for part, n := range f.perPart {
		require.Equal(t, 1, n, "part %d 在一个轮次内被封了 %d 次（同一三元组必须只封一次）", part, n)
	}
}
