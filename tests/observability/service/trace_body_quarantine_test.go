package service_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 本文件是计划 3/4 任务 4 的 B 层用例：**服务 + 真实冻结包 Reader + 真实文件系统**。
//
// 【为什么必须有这一层】任务 2 的用例全部用替身 reader（快、可控），因此**完全没有**
// 覆盖真实 ta.Reader 的两条路径。本文件补的正是「服务 ↔ 冻结包」这道接缝，也是隔离件
// 命名/发现规则的唯一可验证形式（规格 §7.1）。
//
// 【本任务只写测试】不改任何生产代码 ⇒ 变异打在任务 2 的服务文件上
// （`tools/trace-mutate/sets/p3-task-4.py`）。
//
// 范围（严格四项，多一件都不做）：① 隔离件形态；② spool 兜底；③ 行号越界 vs 真实缺失；
// ④ 跨 dt / 跨 inst / 跨 part 边界。

// ---------------------------------------------------------------------------
// 本文件的共享 helper（与 trace_body_service_test.go 同包；不复用其替身 reader）
// ---------------------------------------------------------------------------

// requireZstdCLI 返回 (zstd 可执行文件路径, 本机版本)。找不到就 t.Skip —— 本机没有
// C 版 zstd CLI 时「真实封块产物」那条用例无法产出证据（集合的 SKIP_WHITELIST 已登记）。
//
// 【为什么必须把本机版本显式传给 Sealer】冻结包的版本守卫要求 ExpectedZstdVersion
// 与本机 `--version` 实际值相等，否则每一次封块都被拒绝（默认值 defaultZstdVersion
// 是生产标定版本，与本机开发版本不一定相同）。
func requireZstdCLI(t *testing.T) (string, string) {
	t.Helper()
	bin, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("未找到 zstd 二进制：真实封块产物的用例需要 C 版 zstd CLI（本用例在集合的 SKIP_WHITELIST 内）")
	}
	out, err := exec.Command(bin, "--version").Output()
	require.NoError(t, err, "探测 zstd 版本失败（%s --version）", bin)
	s := string(out)
	i := strings.Index(s, " v")
	require.GreaterOrEqual(t, i, 0, "无法从 zstd 版本输出解析版本号: %q", s)
	rest := s[i+2:]
	if j := strings.IndexAny(rest, ", \n"); j >= 0 {
		rest = rest[:j]
	}
	ver := strings.TrimSpace(rest)
	require.NotEmpty(t, ver, "无法从 zstd 版本输出解析版本号: %q", s)
	return bin, ver
}

// quarantineRefRow 构造一行**带完整定位键**的 trace：dt / inst / part / row 显式给出，
// body_missing=false，jsonb 三列全 NULL（正文只在归档/spool 里）。
//
// 【dt 一律用 time.UTC 构造】不得写成 time.Local 形状：本机 /etc/localtime 是
// Asia/Shanghai，用 Local 会让 dt= 目录名随宿主机状态漂移。读侧的 dt 只由这一列的
// Format("2006-01-02") 决定（读侧零日期计算，全局约束 + E2）。
func quarantineRefRow(requestID, dt, inst string, part, row int) obsmodel.CallTrace {
	day, err := time.ParseInLocation("2006-01-02", dt, time.UTC)
	if err != nil {
		panic("测试向量 dt 必须是 YYYY-MM-DD: " + err.Error())
	}
	p, r := part, row
	return obsmodel.CallTrace{
		RequestID:   requestID,
		TS:          time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		BodyRefDT:   &day,
		BodyRefInst: &inst,
		BodyRefPart: &p,
		BodyRefRow:  &r,
		BodyMissing: false,
	}
}

// createRefRow 落一行定位键行（gorm 的 Create 需要可取地址的值 ⇒ 不能直接对函数结果取址）。
func createRefRow(t *testing.T, db *gorm.DB, requestID, dt, inst string, part, row int) {
	t.Helper()
	traceRow := quarantineRefRow(requestID, dt, inst, part, row)
	require.NoError(t, db.Create(&traceRow).Error)
}

// newRealTraceBodyService 装配「读侧已启用 + **真实** ta.Reader」的点读服务。
// Reader 的 Root 就是 root，窗口与并发取生产默认量级（512 MiB / 2）。
func newRealTraceBodyService(t *testing.T, db *gorm.DB, root string) *obssvc.TraceBodyService {
	t.Helper()
	r, err := ta.NewReader(ta.ReaderOptions{
		Root:            root,
		ZstdWindowBytes: 512 << 20,
		MaxConcurrent:   2,
	})
	require.NoError(t, err, "root 非空时必须能构造真实 Reader")
	svc, err := obssvc.NewTraceBodyService(db, obssvc.TraceBodyServiceOptions{
		Config: enabledReadConfig(root),
		Reader: r,
	})
	require.NoError(t, err)
	return svc
}

// writeSpoolLines 把若干行按 spool 的物理布局写入（每行后跟一个 '\n'），返回文件路径。
// 路径由冻结包的 Paths.SpoolFile 生成 —— 不手写目录名，避免与生产布局漂移。
func writeSpoolLines(t *testing.T, root, dt, inst string, part int, lines [][]byte) string {
	t.Helper()
	path := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: part})
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	var buf bytes.Buffer
	for _, l := range lines {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}

// ---------------------------------------------------------------------------
// ① 隔离件形态（规格 §7.1；全局约束的命名/发现规则，登记 D60）
// ---------------------------------------------------------------------------

// TestTraceBodyServiceQuarantineYieldsNotArchived ← m01 的杀手
//
// 布置：在【spool 目录】写 <root>/spool/dt=2026-10-08/inst=blue/part-000000.jsonl.zst，
// 内容是**明文 JSONL**（模拟 writer.go 的 quarantinePath 产物：写入失败被隔离的块）。
//
// 断言：GetBody ⇒ ErrNotArchived，**且不是** ErrFooterCorrupt（把它当产物打开会得到
// 「产物损坏」这个**错误诊断**——内容是明文、根本不是 zstd 帧），**且不是**
// ErrBodyMissing（它不是「正文从未落盘」），**且**响应为 nil（绝不能把明文当正文返回）。
//
// 理由：隔离件是「响亮、需人工介入」的一类；其中的正文不能通过正常路径读到，
// 读侧也**不得**为它新增分支或回退明文（规格 §7.1）。
func TestTraceBodyServiceQuarantineYieldsNotArchived(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"

	db := newTraceBodyTestDB(t)
	createRefRow(t, db, "req-quarantine", dt, inst, 0, 0)

	root := t.TempDir()
	paths := ta.NewPaths(root)
	// 隔离件：位置在 spool 目录、名字是 part-NNNNNN.jsonl.zst（quarantineSuffix）。
	quarantinePath := filepath.Join(paths.SpoolDir(dt, inst), "part-000000.jsonl.zst")
	require.NoError(t, os.MkdirAll(filepath.Dir(quarantinePath), 0o755))
	plaintext := []byte(`{"request_body":{"i":1}}` + "\n")
	require.NoError(t, os.WriteFile(quarantinePath, plaintext, 0o644))

	svc := newRealTraceBodyService(t, db, root)
	res, err := svc.GetBody(context.Background(), "req-quarantine")

	require.Nil(t, res, "隔离件里的明文绝不能当正文返回")
	require.Error(t, err)
	require.ErrorIs(t, err, ta.ErrNotArchived, "隔离件两条精确路径都不命中 ⇒ ErrNotArchived")
	require.NotErrorIs(t, err, ta.ErrFooterCorrupt,
		"隔离件不能被当产物打开（内容是明文 JSONL，不是 zstd 帧 ⇒ 报损坏是错误诊断）")
	require.NotErrorIs(t, err, ta.ErrBodyMissing, "隔离件不是「正文从未落盘」")
	require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
}

// TestTraceBodyServiceQuarantineNameIsNotDoubleSuffixed ← **无变异**（该守卫在冻结包的
// 命名/路径精确性里，不在服务文件 ⇒ 由集合的 CHECKS「读侧无隔离件分支」与冻结包
// spool 兜底的 CHECKS 行兜住，见集合头）
//
// 断言：隔离件的名字**恰好**是 part-000000.jsonl.zst（一次后缀），不是
// part-000000.jsonl.jsonl.zst —— 路径只能由冻结包的 partName(part, ".jsonl.zst") 生成。
// 再布置一个双后缀名字的文件：spool 兜底只认 .jsonl（精确路径），因此它同样不命中
// ⇒ 仍是 ErrNotArchived（而不是被当成 spool 读回明文）。
func TestTraceBodyServiceQuarantineNameIsNotDoubleSuffixed(t *testing.T) {
	const dt, inst = "2026-10-08", "blue"
	ref := ta.Ref{DT: dt, Inst: inst, Part: 0, Row: 0}

	root := t.TempDir()
	paths := ta.NewPaths(root)

	// 冻结包生成的两个精确名字（不手写目录名，避免与生产布局漂移）。
	require.Equal(t, "part-000000.jsonl.zst", filepath.Base(paths.PartFile(ref)),
		"产物名必须是 part-%06d.jsonl.zst（单后缀）；双后缀说明路径拼接错了")
	require.Equal(t, "part-000000.jsonl", filepath.Base(paths.SpoolFile(ref)),
		"spool 兜底只认 .jsonl（精确路径）⇒ 隔离件的 .jsonl.zst 天然不命中")

	t.Run("正确位置与单后缀的隔离件", func(t *testing.T) {
		db := newTraceBodyTestDB(t)
		createRefRow(t, db, "req-q-single", dt, inst, 0, 0)
		p := filepath.Join(paths.SpoolDir(dt, inst), "part-000000.jsonl.zst")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(`{"request_body":{"i":1}}`+"\n"), 0o644))

		svc := newRealTraceBodyService(t, db, root)
		res, err := svc.GetBody(context.Background(), "req-q-single")
		require.Nil(t, res)
		require.ErrorIs(t, err, ta.ErrNotArchived)
	})

	t.Run("双后缀名字不被当spool读", func(t *testing.T) {
		db := newTraceBodyTestDB(t)
		createRefRow(t, db, "req-q-double", dt, inst, 0, 0)
		p := filepath.Join(paths.SpoolDir(dt, inst), "part-000000.jsonl.jsonl.zst")
		require.NoError(t, os.WriteFile(p, []byte(`{"request_body":{"i":1}}`+"\n"), 0o644))

		svc := newRealTraceBodyService(t, db, root)
		res, err := svc.GetBody(context.Background(), "req-q-double")
		require.Nil(t, res, "双后缀不在任何一条精确路径上 ⇒ 不得返回其明文")
		require.ErrorIs(t, err, ta.ErrNotArchived)
		require.NotErrorIs(t, err, ta.ErrFooterCorrupt)
	})
}

// ---------------------------------------------------------------------------
// ② spool 兜底（规格 §8.2：未封块的近期正文直接读明文，不等封块）
// ---------------------------------------------------------------------------

// TestTraceBodyServiceReadsUnsealedSpool ← m02 的杀手
//
// 布置：<root>/spool/dt=D/inst=blue/part-000007.jsonl，含 3 行合法 JSON，每行以 '\n' 结尾。
// 断言：row=0/1/2 逐字节取回自己的那一行（不含换行）、Source==SourceSpool、
// 且导出的 Ref 四分量与请求行一致（dt 来自行内列）。
//
// 理由：「未封块的近期 trace 直接读明文，无需等待封块」是读侧可用性的关键；
// 来源诊断（Source）失真会让排障与压缩比核算指向错误的对象。
func TestTraceBodyServiceReadsUnsealedSpool(t *testing.T) {
	const dt, inst, part = "2026-10-08", "blue", 7

	lines := [][]byte{
		[]byte(`{"request_headers":{"h":"0"},"request_body":{"line":0}}`),
		[]byte(`{"request_body":{"line":1},"response_body":{"o":1}}`),
		[]byte(`{"response_body":{"line":2}}`),
	}

	db := newTraceBodyTestDB(t)
	root := t.TempDir()
	writeSpoolLines(t, root, dt, inst, part, lines)

	svc := newRealTraceBodyService(t, db, root)

	for row, want := range lines {
		requestID := fmt.Sprintf("req-spool-%d", row)
		createRefRow(t, db, requestID, dt, inst, part, row)

		res, err := svc.GetBody(context.Background(), requestID)
		require.NoError(t, err, "row=%d 必须能从未封块的 spool 读回", row)
		require.NotNil(t, res)
		require.Equal(t, want, res.Body, "row=%d 必须逐字节取回自己的那一行（不含换行）", row)
		require.Equal(t, obssvc.SourceSpool, res.Source, "来源必须是 spool（不是 archive）")
		require.Equal(t, ta.Ref{DT: dt, Inst: inst, Part: part, Row: row}, res.Ref)
	}
}

// TestTraceBodyServiceSpoolFallbackWhenProductAbsent ← 同 m02（不强制产物存在）
//
// 只在 spool 放行、**不放产物** ⇒ 必须读到：读取顺序是「产物优先」，但产物不存在
// 不是错误，而是回落到 spool（否则所有近期 trace 都要等封块才能点读）。
func TestTraceBodyServiceSpoolFallbackWhenProductAbsent(t *testing.T) {
	const dt, inst, part = "2026-10-08", "green", 3
	line := []byte(`{"request_body":{"only":"spool"}}`)

	db := newTraceBodyTestDB(t)
	root := t.TempDir()
	writeSpoolLines(t, root, dt, inst, part, [][]byte{line})

	paths := ta.NewPaths(root)
	_, err := os.Stat(paths.PartFile(ta.Ref{DT: dt, Inst: inst, Part: part}))
	require.True(t, os.IsNotExist(err), "本用例的前置：产物确实不存在")

	createRefRow(t, db, "req-spool-only", dt, inst, part, 0)
	svc := newRealTraceBodyService(t, db, root)

	res, err := svc.GetBody(context.Background(), "req-spool-only")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, line, res.Body)
	require.Equal(t, obssvc.SourceSpool, res.Source)
}

// ---------------------------------------------------------------------------
// ③ 行号稠密性与越界（越界 vs 真实缺失的**不同**错误语义）
// ---------------------------------------------------------------------------

// TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing ← m01/m03 的杀手
// （**需要真实 zstd**；本用例整条登记在集合的 SKIP_WHITELIST 内）
//
// 布置：用**真实 Sealer** 封 3 行产物（footer.row_count=3）。
// 断言：row=3 与 row=999 ⇒ ErrNotArchived（**不是** ErrBodyMissing、**不是** 404 类语义、
// **不是** ErrTraceBodyInconsistent）。
//
// 判据：行号越界是「元数据与存储不一致」（500 类，需人工介入），不是「正文从未落盘」。
// 【与生产产物的关系】本机 zstd 版本与生产标定版本可能不同 ⇒ 本用例证明的是
// 「服务对越界产物的分类正确」，**不得**把它当成生产产物形态的验证。
func TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing(t *testing.T) {
	const dt, inst, part = "2026-10-08", "blue", 0
	bin, ver := requireZstdCLI(t)

	lines := [][]byte{
		[]byte(`{"request_body":{"line":0}}`),
		[]byte(`{"request_body":{"line":1}}`),
		[]byte(`{"request_body":{"line":2}}`),
	}

	db := newTraceBodyTestDB(t)
	root := t.TempDir()
	spool := writeSpoolLines(t, root, dt, inst, part, lines)

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	require.NoError(t, err)
	require.NoError(t, s.Seal(context.Background(), ta.SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: len(lines),
	}), "封块失败（本机 zstd=%s）", ver)

	paths := ta.NewPaths(root)
	_, statErr := os.Stat(paths.PartFile(ta.Ref{DT: dt, Inst: inst, Part: part}))
	require.NoError(t, statErr, "封块后产物必须存在（本用例考的就是产物路径的越界判定）")

	svc := newRealTraceBodyService(t, db, root)
	for _, row := range []int{len(lines), 999} {
		requestID := fmt.Sprintf("req-oor-%d", row)
		createRefRow(t, db, requestID, dt, inst, part, row)

		res, err := svc.GetBody(context.Background(), requestID)
		require.Nil(t, res, "row=%d 越界不得返回正文", row)
		require.ErrorIs(t, err, ta.ErrNotArchived, "row=%d 越界 ⇒ 元数据与存储不一致", row)
		require.NotErrorIs(t, err, ta.ErrBodyMissing, "row=%d 越界不是「正文从未落盘」", row)
		require.NotErrorIs(t, err, ta.ErrFooterCorrupt, "row=%d 越界不是产物损坏", row)
		require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
	}
}

// TestTraceBodyServiceMissingProductAndSpoolIsNotArchived ← m01/m03 的杀手
// （**不需要 zstd** ⇒ 两条变异在无 zstd 的机器上也必须红在它身上）
//
// 布置：只写 PG 行（带完整定位键），文件系统上**什么都没有**（root 是空目录）。
// 断言：ErrNotArchived —— 不是 ErrBodyMissing（正文不是「从未落盘」，而是元数据指向了
// 不存在的副本）、不是 gorm.ErrRecordNotFound（记录明明在）、不是服务层不一致。
func TestTraceBodyServiceMissingProductAndSpoolIsNotArchived(t *testing.T) {
	db := newTraceBodyTestDB(t)
	createRefRow(t, db, "req-missing-both", "2026-10-08", "blue", 0, 0)

	root := t.TempDir() // 完全空：既无产物目录，也无 spool 目录
	svc := newRealTraceBodyService(t, db, root)

	res, err := svc.GetBody(context.Background(), "req-missing-both")
	require.Nil(t, res)
	require.ErrorIs(t, err, ta.ErrNotArchived)
	require.NotErrorIs(t, err, ta.ErrBodyMissing)
	require.NotErrorIs(t, err, gorm.ErrRecordNotFound, "记录存在 ⇒ 不得报成「没有这条 trace」")
	require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
	require.NotErrorIs(t, err, obssvc.ErrTraceBodyDisabled)
}

// TestTraceBodyServiceMarkerLineIsBodyMissingNotText ← m05 的杀手
//
// 布置：spool 里第 0 行是字面量 null（AppendFailed 的标记行形态），第 1 行是正常正文。
// 断言：row=0 ⇒ ErrBodyMissing（404 类），响应为 nil —— **绝不**返回字符串 "null"；
// 同一份 spool 的 row=1 必须正常取回（证明不是「整个文件一律拒绝」，也证明标记行识别
// 只作用于被请求的那一行）。
//
// 理由：读取侧逐字返回、不做 JSON 校验（reader.go 的注释），少了识别就会把
// 「从未落盘」伪装成「正文就是 null」。
func TestTraceBodyServiceMarkerLineIsBodyMissingNotText(t *testing.T) {
	const dt, inst, part = "2026-10-08", "blue", 0
	marker := []byte("null")
	second := []byte(`{"request_body":{"i":2}}`)

	db := newTraceBodyTestDB(t)
	root := t.TempDir()
	writeSpoolLines(t, root, dt, inst, part, [][]byte{marker, second})
	svc := newRealTraceBodyService(t, db, root)

	createRefRow(t, db, "req-marker-0", dt, inst, part, 0)
	res, err := svc.GetBody(context.Background(), "req-marker-0")
	require.Nil(t, res, "标记行绝不能把字面量 null 当正文返回")
	require.ErrorIs(t, err, ta.ErrBodyMissing)
	require.NotErrorIs(t, err, ta.ErrNotArchived)
	require.NotErrorIs(t, err, obssvc.ErrTraceBodyInconsistent)
	require.NotErrorIs(t, err, obssvc.ErrTraceBodyDisabled)

	// 对照格：同一份 spool 的下一条物理行是正常正文 ⇒ 必须正常取回。
	createRefRow(t, db, "req-marker-1", dt, inst, part, 1)
	res2, err2 := svc.GetBody(context.Background(), "req-marker-1")
	require.NoError(t, err2)
	require.NotNil(t, res2)
	require.Equal(t, second, res2.Body, "标记行只占它自己那一行的判定，不影响后续行")
}

// ---------------------------------------------------------------------------
// ④ 跨日 / 跨 inst / 跨 part 边界
// ---------------------------------------------------------------------------

// TestTraceBodyServiceCrossDayAndCrossPart ← m04 的杀手（dt 分量）
//
// 两个 dt（2026-10-08 / 2026-10-09）× 两个 inst（blue/green）× 两个 part（0/1）
// 共 8 个 spool；每行内容里编码自己的唯一标识。断言每个 (dt,inst,part,row) 取回的
// 是【它自己】的那一行（逐字节），且导出的 Ref 四分量逐字等于请求行。
//
// 理由：定位键的 dt/inst/part 三分量任一被拼错/调换，都会读到另一个文件的行
// （或落进 ErrNotArchived）；dt 的唯一事实源必须是行内 body_ref_dt 列（读侧零日期计算）。
func TestTraceBodyServiceCrossDayAndCrossPart(t *testing.T) {
	dts := []string{"2026-10-08", "2026-10-09"}
	insts := []string{"blue", "green"}
	parts := []int{0, 1}

	type crossCell struct {
		dt, inst string
		part     int
		row      int
		line     []byte
		reqID    string
	}

	db := newTraceBodyTestDB(t)
	root := t.TempDir()

	var cells []crossCell
	for _, dt := range dts {
		for _, inst := range insts {
			for _, part := range parts {
				const row = 0
				line := []byte(fmt.Sprintf(
					`{"request_body":{"where":"dt=%s/inst=%s/part=%d/row=%d"}}`, dt, inst, part, row))
				reqID := fmt.Sprintf("req-cross-%s-%s-%d", dt, inst, part)
				// 每个 (dt,inst,part) 一个 spool，只放这一行 ⇒ 内容就是该格的唯一指纹。
				writeSpoolLines(t, root, dt, inst, part, [][]byte{line})
				cells = append(cells, crossCell{dt: dt, inst: inst, part: part, row: row, line: line, reqID: reqID})
			}
		}
	}
	require.Len(t, cells, len(dts)*len(insts)*len(parts), "必须驱动全部 8 格（少一格即证据不全）")

	svc := newRealTraceBodyService(t, db, root)
	for _, c := range cells {
		createRefRow(t, db, c.reqID, c.dt, c.inst, c.part, c.row)
	}

	for _, c := range cells {
		res, err := svc.GetBody(context.Background(), c.reqID)
		require.NoError(t, err, "cell dt=%s inst=%s part=%d", c.dt, c.inst, c.part)
		require.NotNil(t, res)
		require.Equal(t, c.line, res.Body,
			"cell dt=%s inst=%s part=%d 读到了别的文件的行（分量被拼错/调换）", c.dt, c.inst, c.part)
		require.Equal(t, obssvc.SourceSpool, res.Source)
		require.Equal(t, ta.Ref{DT: c.dt, Inst: c.inst, Part: c.part, Row: c.row}, res.Ref)
	}
}
