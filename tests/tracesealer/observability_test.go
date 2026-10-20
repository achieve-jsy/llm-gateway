package tracesealer_test

// 本文件是 B 层测试（tests/tracesealer，package tracesealer_test）：驱动
// `Runner.Round` 的**观测面**（A8 行数分布、A6 压缩比）。
//
// 【为什么断言按结构化字段做，而不是 grep 日志文本】日志是运维的采集面，字段名与
// 级别是契约；文本 grep 只能证明「某处出现过某个字」，把 `level=WARN` 换成
// `level=INFO`、把字段名 `rows` 换成 `row_count` 都会漏网。因此这里用一个**记录型
// slog handler** 把 (级别, 消息, 字段) 收进内存，直接断言三者。
//
// 【为什么读数都用手工拼的产物】A8 要观测的是「封出来的块里有多少行」，而
// RowCount 只读末尾 32 字节 —— 因此「行数 = 上界的一半 + 1」这种**接近上界**的形态
// 可以用 32 字节的 footer 确定复现，不需要真的造一个 800 万行的块。这正是本文件能
// 在本机（无真实流量、无 A3 环境）把预警线钉死的原因。

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// logRecord 是一条被捕获的日志。
type logRecord struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

// recordHandler 把每条 slog 记录原样存进内存（不落盘、不格式化）。
type recordHandler struct{ recs *[]logRecord }

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.Any(); return true })
	*h.recs = append(*h.recs, logRecord{Level: r.Level, Msg: r.Message, Attrs: attrs})
	return nil
}

func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

func (h recordHandler) byMsg(msg string) []logRecord {
	var out []logRecord
	for _, r := range *h.recs {
		if r.Msg == msg {
			out = append(out, r)
		}
	}
	return out
}

func (h recordHandler) byLevel(lv slog.Level) []logRecord {
	var out []logRecord
	for _, r := range *h.recs {
		if r.Level == lv {
			out = append(out, r)
		}
	}
	return out
}

func (h recordHandler) reasonsAt(lv slog.Level) []any {
	var out []any
	for _, r := range h.byLevel(lv) {
		out = append(out, r.Attrs["reason"])
	}
	return out
}

// observedRunner 建一个「root 在场、日志被捕获、封块器由替身驱动」的 Runner。
// 返回 (root, runner, 日志)。替身指针先建后填（`driveSealedParts` 就地改它），
// 因此 NewSealer 闭包抓到的就是同一个替身。
func observedRunner(t *testing.T, f *fakeSealer) (string, *tracesealer.Runner, *recordHandler) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "spool"), 0o755))
	logs := &recordHandler{recs: &[]logRecord{}}
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root: root,
		Slog: slog.New(logs),
		NewSealer: func(ta.SealerOptions) (tracesealer.Sealer, error) {
			return f, nil
		},
	})
	require.NoError(t, err)
	return root, r, logs
}

// sealPlan 描述一个「成功封出」的块：Part 序号、调用方声明的未压缩字节、以及产物
// 文件里 footer 写进去的行数与文件总长。
type sealPlan struct {
	part    int
	unzip   int64
	rows    int32
	artSize int
}

// driveSealedParts 按 sealPlan 就地填好替身：替身在 Seal 被调用时**真的把产物写到**
// `PartDir(dt,inst)` 下面 —— 这一步是刻意的，`observeSealedPart` 从磁盘读 footer，
// 不写文件就观测不到任何东西。
func driveSealedParts(t *testing.T, root, dt, inst string, plans []sealPlan, f *fakeSealer) {
	t.Helper()
	for _, p := range plans {
		f.tasks = append(f.tasks, ta.SealTask{
			DT: dt, Inst: inst, Part: p.part,
			SpoolPath:         ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: p.part}),
			UncompressedBytes: p.unzip,
		})
	}
	f.onSeal = func(part int) {
		for _, p := range plans {
			if p.part != part {
				continue
			}
			writeArtifactWithFooter(t,
				ta.NewPaths(root).PartFile(ta.Ref{DT: dt, Inst: inst, Part: part}),
				p.artSize, p.rows)
		}
	}
}

const logMsgSealedPart = "trace-sealer sealed part"
const logMsgRound = "trace-sealer round"

// TestObserveTracksMinimumBytesPerRow 两个块的行数不同 ⇒ `MinBytesPerRow` 必须取
// **较小**的那个（「每行最少多少字节」是 A6/A8 的下界读数，取成大值会让容量账偏乐观）。
//
// 【它是 m02/m03 的在册杀手】
//   - m02 把更新判据的 `<` 改成 `>` ⇒ 该量变成最大值 ⇒ 红；
//   - m03 把 bytesPerRow 的分母改成 `rows+1` ⇒ 两个块的读数都变小 ⇒ 红。
func TestObserveTracksMinimumBytesPerRow(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"
	// 块 0：1 MiB / 32 行 = 32768 B/行；块 1：1 MiB / 512 行 = 2048 B/行。
	plans := []sealPlan{
		{part: 0, unzip: 1 << 20, rows: 32, artSize: 1 << 17},
		{part: 1, unzip: 1 << 20, rows: 512, artSize: 1 << 17},
	}
	f := &fakeSealer{byPart: map[int]error{}}
	root, r, logs := observedRunner(t, f)
	driveSealedParts(t, root, dt, inst, plans, f)

	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, res.Sealed)
	require.Equal(t, 512, res.MaxRows)
	require.Equal(t, int64(2048), res.MinBytesPerRow)
	require.InDelta(t, 8.0, res.MinRatio, 1e-9) // 1 MiB / 128 KiB，两块相同
	require.Len(t, logs.byMsg(logMsgSealedPart), 2, "每个成功观测到的块恰好一条 INFO")
}

// TestObserveTracksMaxRowsAndMinRatio 钉住另外两个量的更新方向与量纲：
// `MaxRows` 取最大行数、`MinRatio` 取**最小**压缩比（压缩比最小 = 最难压的那一块，
// 容量账必须按它算）。
func TestObserveTracksMaxRowsAndMinRatio(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"
	// 块 0：1 MiB / 10 行 / 256 KiB 产物 ⇒ 4.0；块 1：1 MiB / 1000 行 / 128 KiB ⇒ 8.0。
	plans := []sealPlan{
		{part: 0, unzip: 1 << 20, rows: 10, artSize: 1 << 18},
		{part: 1, unzip: 1 << 20, rows: 1000, artSize: 1 << 17},
	}
	f := &fakeSealer{byPart: map[int]error{}}
	root, r, logs := observedRunner(t, f)
	driveSealedParts(t, root, dt, inst, plans, f)

	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1000, res.MaxRows)
	require.Equal(t, int64(1048), res.MinBytesPerRow) // 1 MiB / 1000 行（块 1 更小）
	require.InDelta(t, 4.0, res.MinRatio, 1e-9, "最小压缩比取的是难压的那一块")
	require.Len(t, logs.byMsg(logMsgSealedPart), 2)
}

// TestObserveWarnsAtHalfOfRowCap A8 的预警线：行数**严格超过**上界的一半 ⇒ 必须 WARN
// （且必须带上行数、块身份与占上界的百分比）；刚好等于一半 ⇒ 不预警（边界是 `>`）。
//
// 【它是 m01 的在册杀手】m01 把 50% 阈值改成 100%（等于取消了预警）⇒ 该 WARN 消失 ⇒ 红。
func TestObserveWarnsAtHalfOfRowCap(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"
	cap := tracesealer.MaxTrailerEntries
	require.Equal(t, 8_388_608, cap, "上界是冻结包 maxTrailerEntries 的镜像值")
	half := int32(cap / 2)

	plans := []sealPlan{
		{part: 0, unzip: 1 << 20, rows: half, artSize: 1 << 16},
		{part: 1, unzip: 1 << 20, rows: half + 1, artSize: 1 << 16},
	}
	f := &fakeSealer{byPart: map[int]error{}}
	root, r, logs := observedRunner(t, f)
	driveSealedParts(t, root, dt, inst, plans, f)

	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, res.Sealed)

	warns := logs.byLevel(slog.LevelWarn)
	require.Len(t, warns, 1, "只有越线的那一块预警（等于一半不预警）")
	require.EqualValues(t, cap/2+1, warns[0].Attrs["rows"])
	require.Equal(t, "2026-10-08/blue/part-000001", warns[0].Attrs["ref"])
	// 4194305 / 8388608 = 50.000012%：百分比必须是「占上界多少」，不是随便一个数。
	require.InDelta(t, 50.0, warns[0].Attrs["row_cap_pct"], 0.001)
}

// TestObserveLogsOneInfoLinePerSealedPart 观测面的「持续可见」要求：
// **每个**成功读到行数的块一行 INFO（可下钻到具体哪一块），**每轮**一行汇总
// （`trace-sealer round`）带上 max_rows / min_bytes_per_row / min_ratio 三个量。
func TestObserveLogsOneInfoLinePerSealedPart(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"
	plans := []sealPlan{
		{part: 0, unzip: 1 << 20, rows: 10, artSize: 1 << 17},
		{part: 1, unzip: 1 << 20, rows: 20, artSize: 1 << 17},
		{part: 2, unzip: 1 << 20, rows: 30, artSize: 1 << 17},
	}
	f := &fakeSealer{byPart: map[int]error{}}
	root, r, logs := observedRunner(t, f)
	driveSealedParts(t, root, dt, inst, plans, f)

	res, err := r.Round(context.Background())
	require.NoError(t, err)

	parts := logs.byMsg(logMsgSealedPart)
	require.Len(t, parts, 3)
	for _, rec := range parts {
		require.Equal(t, slog.LevelInfo, rec.Level)
		require.Contains(t, rec.Attrs, "rows")
		require.Contains(t, rec.Attrs, "bytes_per_row")
		require.Contains(t, rec.Attrs, "ratio")
		require.Contains(t, rec.Attrs, "row_cap_pct")
	}
	require.EqualValues(t, 10, parts[0].Attrs["rows"])
	require.EqualValues(t, 30, parts[2].Attrs["rows"])

	rounds := logs.byMsg(logMsgRound)
	require.Len(t, rounds, 1, "每轮恰好一条汇总（心跳）")
	require.EqualValues(t, res.MaxRows, rounds[0].Attrs["max_rows"])
	require.EqualValues(t, 30, rounds[0].Attrs["max_rows"])
	require.EqualValues(t, res.MinBytesPerRow, rounds[0].Attrs["min_bytes_per_row"])
	require.InDelta(t, res.MinRatio, rounds[0].Attrs["min_ratio"], 1e-9)
}

// TestObserveSkipsUnreadableArtifact 产物已封出、但**行数读不出来**（文件连 32 字节
// 都不到）⇒ 本轮**无样本**：三个量保持零值，另留一条带稳定 reason 的 ERROR。
//
// 【为什么这个语义是承重的】把「读不到」折成某个读数（0 行、或猜 1 行）会让 A8 的
// 分布与 A6 的压缩比**从此不可信**，而且没有任何东西报警 —— 这正是本计划反复登记的
// 「观测面自己失真」那一类失效。Sealed 计数不受影响：产物确确实实封出来了。
//
// 【它是 m04 的在册杀手】m04 把「读不出行数」分支改成 `rows = 1`（把一次读取失败
// 伪装成一个读数）⇒ MaxRows 变成 1、且不再有 ERROR ⇒ 红。
func TestObserveSkipsUnreadableArtifact(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"
	f := &fakeSealer{byPart: map[int]error{}}
	root, r, logs := observedRunner(t, f)
	f.tasks = []ta.SealTask{{
		DT: dt, Inst: inst, Part: 0,
		SpoolPath:         ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: 0}),
		UncompressedBytes: 1 << 20,
	}}
	f.onSeal = func(part int) {
		p := ta.NewPaths(root).PartFile(ta.Ref{DT: dt, Inst: inst, Part: part})
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("short!!\n"), 0o644)) // 8 字节 < 32
	}

	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Sealed, "产物封出的事实不因观测失败而改变")
	require.Equal(t, 0, res.MaxRows, "读不出行数 ⇒ 本轮无样本（不是 0 行、更不是猜 1 行）")
	require.Equal(t, int64(0), res.MinBytesPerRow)
	require.Zero(t, res.MinRatio)
	require.Contains(t, logs.reasonsAt(slog.LevelError), "footer_unreadable",
		"对照度：行数读不出来必须留一条带**稳定 reason** 的 ERROR（否则「无样本」与「没跑」无法区分）")
}

// TestObserveSkipsDegenerateDenominator 分母非法的两个形态 —— 块内 0 行、调用方声明
// 的未压缩字节 ≤ 0 —— 都必须是「跳过本块」（WARN + 稳定 reason），**绝不**发明一个
// 读数（0 会污染 Min* 的下界语义、1 会凭空造出一个压缩比）。
//
// 【它是 m05 的在册杀手】m05 从守卫里摘掉 `rows <= 0 ||` ⇒ 0 行的那一块被当成样本 ⇒
// 预警条数从 2 变 1、per-part INFO 多出一条、且 MinRatio 被写成一个真实读数 ⇒ 红。
func TestObserveSkipsDegenerateDenominator(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"
	plans := []sealPlan{
		{part: 0, unzip: 1 << 20, rows: 0, artSize: 1 << 16}, // 0 行
		{part: 1, unzip: 0, rows: 10, artSize: 1 << 16},      // 未压缩字节为 0
	}
	f := &fakeSealer{byPart: map[int]error{}}
	root, r, logs := observedRunner(t, f)
	driveSealedParts(t, root, dt, inst, plans, f)

	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, res.Sealed)
	require.Equal(t, 0, res.MaxRows)
	require.Equal(t, int64(0), res.MinBytesPerRow)
	require.Zero(t, res.MinRatio)
	require.Empty(t, logs.byMsg(logMsgSealedPart), "没有合法的样本就不该有 per-part 的 INFO 读数")

	var skipped int
	for _, reason := range logs.reasonsAt(slog.LevelWarn) {
		if reason == "observation_skipped" {
			skipped++
		}
	}
	require.Equal(t, 2, skipped, "两个非法分母各留一条 WARN（不许静默跳过）")
}
