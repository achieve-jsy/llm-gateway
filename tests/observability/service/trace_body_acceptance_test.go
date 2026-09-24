package service_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// 本文件是计划 3/4 **任务 8** 的 A2 生产形态验收（规格 §14.2 A2 / 计划 2 的 A 表 A2）。
//
// 【与计划 1 的关系（不得含糊）】A2 的**库层**命题（「封块产物可点读、索引正确」）已由计划 1
// 覆盖，本文件**不重验**：
//   - `tests/tracearchive/roundtrip_test.go` 的 `TestEndToEndRoundTripByteExact`
//     （写 N 行 → 真实封块 → 随机抽 K 行读回 → 逐字节；**直接构造 `ta.Ref` 调 `Reader.GetBody`**）；
//   - `TestFooterGoldenBytes` / `TestReaderReturnsExactLine`；
//   - R11 v3 的 `task-7.py`（32 条）与 `task-8-reader.py`（6 条）变异证据（冻结提交 `ea962a70`）。
//
// 本文件补的是**生产形态缺口**（计划「既有能力基线」C 节的 5 条，别多补也别少补）：
//  1. `request_id` → PG 行 → 定位键 → `GetBody` 的**完整链路**逐字节；
//  2. 四种状态**互不混淆**；
//  3. 跨 part / 跨 `dt` / 行号越界的**对外表现**；
//  4. 隔离件形态（库层无此用例）—— 由任务 4 的
//     `tests/observability/service/trace_body_quarantine_test.go` 覆盖，本文件**不重复**；
//  5. HTTP 表现与库层哨兵的对应 —— 由任务 3/5 的 handler 用例覆盖，本文件只到服务层。
//
// 【效力边界（必须随结论一起读）】本文件用**真实** `ta.Sealer` 封块（C 版 zstd CLI）与
// **真实** `ta.Reader`，但：
//   - 本机 zstd 是 **v1.5.7**，**不是**生产标定版本 `defaultZstdVersion = "1.4.8"`
//     （冻结包 `sealer.go` 已明文登记这一差异）⇒ 本文件证明的是「服务 ↔ 冻结包」这条
//     生产链路对**合法产物**的正确性，**不得**当作「生产产物形态」或「压缩比档位」的验收；
//   - `CallTrace` 落在**内存 sqlite**（不是真实 PG）⇒ 本文件证明的是「定位键列 ↔ 归档产物」
//     的契约，**不得**当作真实 PG 行的验收（真实 PG + 二进制级路径属任务 8 步骤 6 的 CLI 组）；
//   - 规模 n = 2000 行（不是规格 A1 的 1,000,000）⇒ 它覆盖「跨 part 索引正确」，不覆盖压缩比。
//
// 【本文件只写测试】不改任何生产代码 ⇒ 变异打在任务 2 的服务文件上
// （`tools/trace-mutate/sets/p3-task-8.py`）。

// a2SealPart 把一份 spool 明文用**真实 Sealer** 封成产物，并返回产物路径。
//
// 复用同包的 `writeSpoolLines`（路径由冻结包 `Paths.SpoolFile` 生成，不手写目录名）。
func a2SealPart(t *testing.T, bin, ver, root, dt, inst string, part int, lines [][]byte) {
	t.Helper()
	spool := writeSpoolLines(t, root, dt, inst, part, lines)
	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	require.NoError(t, err)
	require.NoError(t, s.Seal(context.Background(), ta.SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: len(lines),
	}), "封块失败（本机 zstd=%s）", ver)
}

// TestAcceptanceA2ProductionRoundTrip 是规格 §14.2 A2 的【生产形态】部分。
//
// 与计划 1 的 `TestEndToEndRoundTripByteExact` 的关系（必须在注释里写明）：
//
//	计划 1 直接构造 `Ref` 调 `Reader.GetBody`（库层，已覆盖且**不重验**）；
//	本用例经过【完整生产链路】：真实 `ta.Sealer` 封块 → PG 行（含 `body_ref_*` 四列）
//	→ `TraceBodyService.GetBody` → 逐字节。
//	因此它补的是「定位键**写入与读取是否对得上**」这一【跨模块契约】——
//	这正是计划 2 的 A1 写入半部在写入侧无法自证的那一半。
//
// 规模：不必百万（那是 A1 的规格值，且封块需要真实 zstd）。取 n = 2000 行、
// 切成 7 个 part（每 part 286 / 284 行）以强制多产物，这样【索引的跨 part 正确性】
// 在一次用例里被覆盖。
func TestAcceptanceA2ProductionRoundTrip(t *testing.T) {
	bin, ver := requireZstdCLI(t)

	const (
		n        = 2000
		parts    = 7
		sampleN  = 200
		dt, inst = "2026-10-08", "blue"
	)

	db := newTraceBodyTestDB(t)
	root := t.TempDir()

	// ① 逐 part 写 spool 并真实封块；同时把每行的**原字节**存成参照，
	//    并在 PG 侧落一条带完整定位键的行（jsonb 三列全 NULL，body_missing=false）。
	linesPerPart := (n + parts - 1) / parts
	partLines := make([][][]byte, parts)
	partRows := make([][]obsmodel.CallTrace, parts)
	want := make([][]byte, n)
	locPart := make([]int, n)
	locRow := make([]int, n)
	reqIDs := make([]string, n)

	for i := 0; i < n; i++ {
		part := i / linesPerPart
		row := len(partLines[part])
		line := []byte(fmt.Sprintf(
			`{"request_headers":{"i":%d},"request_body":{"line":%d},"response_body":{"ok":true}}`, i, i))
		reqID := fmt.Sprintf("req-a2-%05d", i)

		partLines[part] = append(partLines[part], line)
		partRows[part] = append(partRows[part], quarantineRefRow(reqID, dt, inst, part, row))
		want[i], locPart[i], locRow[i], reqIDs[i] = line, part, row, reqID
	}

	// 每个 part 都必须非空：空的 part 会让「跨 part」这一命题在不同机器上悄悄退化。
	for p := 0; p < parts; p++ {
		require.NotEmpty(t, partLines[p], "part-%06d 不能是空 part（否则跨 part 命题未被测到）", p)
		a2SealPart(t, bin, ver, root, dt, inst, p, partLines[p])
		require.NoError(t, db.CreateInBatches(&partRows[p], 200).Error)
	}
	require.Len(t, partLines, parts)

	svc := newRealTraceBodyService(t, db, root)

	// ③ 随机抽 K = 200 行（固定 seed 保证可复现）。
	rng := rand.New(rand.NewSource(20261008))
	sampled := rng.Perm(n)[:sampleN]

	partsSeen := map[int]bool{}
	for _, i := range sampled {
		res, err := svc.GetBody(context.Background(), reqIDs[i])
		require.NoError(t, err, "index=%d（part=%d row=%d）必须能从真实产物读回", i, locPart[i], locRow[i])
		require.NotNil(t, res)

		// 逐字节：这是 A2 的核心命题。
		require.Equal(t, want[i], res.Body,
			"index=%d（part=%d row=%d）读到的不是它自己那一行（定位键与产物内容对不上）",
			i, locPart[i], locRow[i])

		// 来源必须是封块产物（不是 spool / jsonb）。
		require.Equal(t, obssvc.SourceArchive, res.Source,
			"index=%d 的来源不是 archive（说明定位键没走到产物路径）", i)

		// 服务导出的定位键必须逐字等于请求行的四列（dt 只做布局格式化，读侧零日期计算）。
		require.Equal(t, ta.Ref{DT: dt, Inst: inst, Part: locPart[i], Row: locRow[i]}, res.Ref)

		partsSeen[locPart[i]] = true
	}

	// ④ 【覆盖守卫 —— 如实标注：它不是**独立杀手**】
	//
	//   计划把这一条写成「索引正确的承重断言」。本轮实测（隔离探针 R7：把服务导出的
	//   `Ref.Part` 单独写死为 0、正文路径不动）得到的是**上面那条 `require.Equal(…, res.Ref)`
	//   转红**，本行**没有被触到** —— 因为 `partsSeen` 正是从那个已被逐字钉住的
	//   `res.Ref.Part` 取的，逻辑上被上一条蕴含（典型的遮蔽效应）。
	//   ⇒ 本行的真实身份是**守卫测试夹具本身**（防止后人把 `parts` 改成 1、让「跨 part」
	//      这一命题悄悄退化）。**不得**把它记成「索引正确」的独立证据：
	//      索引正确的独立证据是上面的**逐字节**断言（R1 已实测可红）。
	require.GreaterOrEqual(t, len(partsSeen), 2,
		"抽样覆盖的 part 数 = %d（必须 >= 2，否则跨 part 索引正确性未被测到）", len(partsSeen))
	t.Logf("A2 生产形态往返：n=%d / parts=%d / 抽样=%d 行 / 覆盖 %d 个 part（本机 zstd=%s，生产标定=%s）",
		n, parts, sampleN, len(partsSeen), ver, "1.4.8")
}

// TestAcceptanceA2IndexBoundaries 覆盖索引的三个边界（库层已有分类，这里测【对外表现】）：
//
//	① 跨日：两个 dt 目录各一个 part，抽样断言各自取回自己的行（不串目录）
//	② 跨 part：同一 dt 的 part-000000 与 part-000001，抽样不串文件
//	③ 行号越界：row == row_count（恰好在界外一个）与 row == math.MaxInt32
//	   断言 errors.Is(err, ta.ErrNotArchived) 且【不是】ErrBodyMissing
//	   —— 后者是承重的：越界若被报成「正文从未落盘」，就把存储不一致伪装成了业务事实。
func TestAcceptanceA2IndexBoundaries(t *testing.T) {
	bin, ver := requireZstdCLI(t)

	const inst = "blue"
	const dtA, dtB = "2026-10-08", "2026-10-09"

	db := newTraceBodyTestDB(t)
	root := t.TempDir()

	// 三个产物：跨日两格 + 跨 part 两格（part 0 / part 1，同一 dt）。
	linesA0 := [][]byte{
		[]byte(`{"request_body":{"cell":"dtA-part0-row0"}}`),
		[]byte(`{"request_body":{"cell":"dtA-part0-row1"}}`),
		[]byte(`{"request_body":{"cell":"dtA-part0-row2"}}`),
	}
	linesB0 := [][]byte{
		[]byte(`{"request_body":{"cell":"dtB-part0-row0"}}`),
		[]byte(`{"request_body":{"cell":"dtB-part0-row1"}}`),
	}
	linesA1 := [][]byte{
		[]byte(`{"request_body":{"cell":"dtA-part1-row0"}}`),
		[]byte(`{"request_body":{"cell":"dtA-part1-row1"}}`),
	}

	a2SealPart(t, bin, ver, root, dtA, inst, 0, linesA0)
	a2SealPart(t, bin, ver, root, dtB, inst, 0, linesB0)
	a2SealPart(t, bin, ver, root, dtA, inst, 1, linesA1)

	cases := []struct {
		reqID    string
		dt       string
		part     int
		row      int
		wantLine []byte
		label    string
	}{
		{"req-bd-a0-0", dtA, 0, 0, linesA0[0], "跨日/甲日 part0 row0"},
		{"req-bd-a0-2", dtA, 0, 2, linesA0[2], "跨日/甲日 part0 row2"},
		{"req-bd-b0-0", dtB, 0, 0, linesB0[0], "跨日/乙日 part0 row0（不得串到甲日）"},
		{"req-bd-b0-1", dtB, 0, 1, linesB0[1], "跨日/乙日 part0 row1"},
		{"req-bd-a1-0", dtA, 1, 0, linesA1[0], "跨 part/part1 row0（不得串到 part0）"},
		{"req-bd-a1-1", dtA, 1, 1, linesA1[1], "跨 part/part1 row1"},
	}
	for _, c := range cases {
		createRefRow(t, db, c.reqID, c.dt, inst, c.part, c.row)
	}

	svc := newRealTraceBodyService(t, db, root)

	for _, c := range cases {
		res, err := svc.GetBody(context.Background(), c.reqID)
		require.NoError(t, err, "%s：必须能读回", c.label)
		require.NotNil(t, res)
		require.Equal(t, c.wantLine, res.Body, "%s：读到了别的文件/别的日的行（分量被拼错或调换）", c.label)
		require.Equal(t, obssvc.SourceArchive, res.Source, "%s：来源必须是 archive", c.label)
		require.Equal(t, ta.Ref{DT: c.dt, Inst: inst, Part: c.part, Row: c.row}, res.Ref)
	}

	// ③ 行号越界（footer.row_count = 3 的产物）：恰好在界外一个 + 极大值。
	//    product 路径先在读 footer 后立即判越界（冻结包 reader.go:182-184），因此这里
	//    命中产物文件却仍必须报 ErrNotArchived。
	createRefRow(t, db, "req-bd-oor-eq", dtA, inst, 0, len(linesA0))
	createRefRow(t, db, "req-bd-oor-max", dtA, inst, 0, math.MaxInt32)

	for _, reqID := range []string{"req-bd-oor-eq", "req-bd-oor-max"} {
		res, err := svc.GetBody(context.Background(), reqID)
		require.Nil(t, res, "%s：越界不得返回正文", reqID)
		require.Error(t, err)
		require.ErrorIs(t, err, ta.ErrNotArchived,
			"%s：行号越界 ⇒ 元数据与存储不一致（不是「正文从未落盘」）", reqID)
		require.NotErrorIs(t, err, ta.ErrBodyMissing,
			"%s：越界若被报成 body_missing，就把存储不一致伪装成了业务事实", reqID)
		require.NotErrorIs(t, err, ta.ErrFooterCorrupt, "%s：越界不是产物损坏", reqID)
		require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent, "%s：越界不是元数据自身矛盾", reqID)
		require.NotErrorIs(t, err, gorm.ErrRecordNotFound, "%s：记录存在", reqID)
	}
}

// TestAcceptanceA2StateMatrixMutuallyExclusive ← 本任务的「互不混淆」验收
//
// 同一批数据里构造四种状态各若干行，一次性断言四者的 (err, source) 组合【两两不同】：
//
//	jsonb        ⇒ (nil, SourceJSONB)
//	archive      ⇒ (nil, SourceArchive)
//	body_missing ⇒ (ErrBodyMissing, —)
//	未归档       ⇒ (ErrNotArchived, —)
//
// 【为什么要这条】规格 §8.1 的全部意图就是「不能让一种状态伪装成另一种」；
// 四条独立用例各自绿【不能】证明它们互不混淆（一个把所有错误都返回同一哨兵的实现
// 可以让四条都绿）⇒ 必须在同一个用例里做【互异性】断言。
//
// 【禁用态在本用例里必须是「启用」】上面第一格「jsonb ⇒ 200」在生产上成立的前提是
// 定位键全 NULL（D55/E6）：本用例显式用**读侧已启用**的配置（root 非空），
// 因此 jsonb 那一格考的是「功能关闭/构造失败留下的历史行」，而不是「读侧禁用」。
// 读侧禁用是另一条判据（503），由任务 5/8 的装配用例覆盖。
func TestAcceptanceA2StateMatrixMutuallyExclusive(t *testing.T) {
	bin, ver := requireZstdCLI(t)

	const dt, inst = "2026-10-08", "blue"

	db := newTraceBodyTestDB(t)
	root := t.TempDir()

	// archive 格：真实封块产物（1 行）。
	archiveLine := []byte(`{"request_body":{"state":"archive"}}`)
	a2SealPart(t, bin, ver, root, dt, inst, 0, [][]byte{archiveLine})
	createRefRow(t, db, "req-state-archive", dt, inst, 0, 0)

	// jsonb 格：四个定位键全 NULL + body_missing=false（正文在 jsonb 列）。
	require.NoError(t, db.Create(&obsmodel.CallTrace{
		RequestID:      "req-state-jsonb",
		TS:             time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		RequestHeaders: datatypes.JSON([]byte(`{"h":"jsonb"}`)),
		RequestBody:    datatypes.JSON([]byte(`{"state":"jsonb"}`)),
		BodyMissing:    false,
	}).Error)

	// body_missing 格：body_missing=true 且四键全 NULL（正文在写入时即失败，从未落盘）。
	require.NoError(t, db.Create(&obsmodel.CallTrace{
		RequestID:   "req-state-missing",
		TS:          time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		BodyMissing: true,
	}).Error)

	// 未归档格：四键全非 NULL，但该 part 既无产物也无 spool。
	createRefRow(t, db, "req-state-notarchived", dt, inst, 5, 0)

	svc := newRealTraceBodyService(t, db, root)

	type cell struct {
		name    string
		reqID   string
		wantErr bool // true ⇒ 必须失败
		sig     string
	}
	// classify 把 (err, source) 归约成可比较的签名；四种状态必须给出四个不同签名。
	classify := func(err error, src obssvc.TraceBodySource) string {
		switch {
		case err == nil:
			return "nil|" + string(src)
		case errors.Is(err, ta.ErrBodyMissing):
			return "body_missing|"
		case errors.Is(err, ta.ErrNotArchived):
			return "not_archived|"
		default:
			return "other:" + err.Error() + "|"
		}
	}

	cells := []cell{
		{name: "archive", reqID: "req-state-archive", wantErr: false},
		{name: "jsonb", reqID: "req-state-jsonb", wantErr: false},
		{name: "body_missing", reqID: "req-state-missing", wantErr: true},
		{name: "not_archived", reqID: "req-state-notarchived", wantErr: true},
	}

	seen := map[string]string{}
	for i := range cells {
		c := &cells[i]
		res, err := svc.GetBody(context.Background(), c.reqID)

		if !c.wantErr {
			require.NoError(t, err, "状态 %s 必须成功", c.name)
			require.NotNil(t, res, "状态 %s 必须返回结果", c.name)
			c.sig = classify(nil, res.Source)
			continue
		}
		require.Error(t, err, "状态 %s 必须失败（不得伪装成另一种状态）", c.name)
		require.Nil(t, res, "状态 %s 失败时不得返回正文", c.name)
		c.sig = classify(err, "")
	}

	// 逐格钉住期望（四格各自的语义）。
	require.Equal(t, "nil|"+string(obssvc.SourceArchive), cells[0].sig, "archive 格")
	require.Equal(t, "nil|"+string(obssvc.SourceJSONB), cells[1].sig, "jsonb 格")
	require.Equal(t, "body_missing|", cells[2].sig, "body_missing 格")
	require.Equal(t, "not_archived|", cells[3].sig, "not_archived 格")

	// archive 格还必须逐字节等于产物里的那一行（来源正确 + 内容正确）。
	resArchive, err := svc.GetBody(context.Background(), "req-state-archive")
	require.NoError(t, err)
	require.Equal(t, archiveLine, resArchive.Body)

	// 【互异性】四格两两不同；任何「把所有错误都归到同一哨兵」或
	// 「把 archive 与 jsonb 都报成同一来源」的实现都会在这里转红。
	for _, c := range cells {
		if prev, dup := seen[c.sig]; dup {
			t.Fatalf("状态 %s 与 %s 的 (err, source) 组合相同（%s）⇒ 两种状态互相伪装", c.name, prev, c.sig)
		}
		seen[c.sig] = c.name
	}
	require.Len(t, seen, 4, "四种状态的 (err, source) 组合必须两两不同，实际不同组合数 = %d", len(seen))
}
