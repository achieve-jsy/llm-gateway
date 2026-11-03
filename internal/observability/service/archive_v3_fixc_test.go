// archive_v3_fixc_test.go —— FIX-C：对抗式校验发现的 6 条「数据安全缺陷」的回归测试。
//
// 覆盖（每条测试都经验证：把对应修复回退后必然失败，证据见各测试头注释）：
//
//	T-F1  (a)(b)(c) 盖戳按本轮 rid 列表 / assertPointerCoverage 失败 / 门闸⑧拦下
//	T-F2  (1)(2)(3) 巡检 LoadIndex 失败→suspect；index last_rid 改坏→suspect；retentionGate 拒绝删除
//	T-F2b           盘上有 index.jsonl 而冻结 bodyCount==0 → 不删 _SUCCESS、不标 empty，改标 suspect + 报错
//	T-F3            门闸④逐块 sha256 与 manifest 不符时失败（即使抽样恰全命中）
//	T-F4            verified 天有 leak 行→重新归档；cleaned 天有 leak 行→报错且不重归档
//	T-F5            nullifyDay 游标带 COLLATE "C"
//	T-F6            抽样候选查询含 body 非空谓词；抽到无正文行不误拦
//
// 全部沿用既有测试基建：tests/archive/fakedb 假 DB 驱动 + 真实 LocalStore（t.TempDir）。
// 不引入真实 PostgreSQL 依赖，也不修改 FIX-A/FIX-B 独占的实现文件。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"gorm.io/datatypes"

	"github.com/example/llm-gateway/internal/archive"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/tests/archive/fakedb"
)

// ─── 假 DB 脚本（按 SQL 形状分流；本文件专用，避免与既有 scriptDB 的匹配顺序耦合）──

// fixcScript 描述一次假 DB 会话的全部脚本化返回。
//
// 计数类查询各自独立取值（不像既有 scriptDB 共用 total）——F1 的核心正是
// 「冻结全量行数 != 已盖戳且有正文的行数」，两者必须是可分别注入的两个值。
type fixcScript struct {
	total      int64  // 基准 1：该天全量行数
	bodyCount  int64  // 基准 2：有正文行数
	bodySHA    string // 基准 2：rid 摘要
	noBodyRows int64  // 无正文行数（校验②的加数）

	leakRows     int64 // archive_status = 0 且有正文（countLeakRows）
	stampedRows  int64 // archive_status = 1 且有正文（assertPointerCoverage / 门闸⑧）
	pendingRows  int64 // archive_status IN (0,1) 且有正文（markCleanedDays）
	liveBodyRows int64 // 有正文（不限 status；countLiveBodies）

	exportRows [][]any // 导出批次

	manifest     *cpmodel.TraceArchiveManifest   // loadManifest 返回的行（nil → 不存在）
	manifestList []*cpmodel.TraceArchiveManifest // state=? 的候选列表查询

	sampleRIDs   []string // 有正文的抽样候选
	bodylessRIDs []string // 无正文的候选（仅当 SQL 缺 body 谓词时才会被「选中」，模拟旧口径）

	nullifyIDs []string // 置空 keyset 游标第一批

	insertErr error // 注入 manifest UPSERT 失败
}

func fixcDB(sc *fixcScript) *fakedb.DB {
	nullifyCalls := 0
	return fakedb.New(func(raw string, _ []any) (fakedb.Result, error) {
		q := strings.ReplaceAll(raw, `"`, "")
		ql := strings.ToLower(q)
		switch {
		// —— manifest 读写 ——
		case strings.Contains(ql, "string_agg"):
			var sha any
			if sc.bodySHA != "" {
				sha = sc.bodySHA
			}
			return fakedb.Rows([]string{"cnt", "sha"}, sc.bodyCount, sha), nil
		case strings.Contains(ql, "insert into trace_archive_manifests"):
			if sc.insertErr != nil {
				return fakedb.Err(sc.insertErr), nil
			}
			return fakedb.Result{RowsAffected: 1}, nil
		case strings.Contains(ql, "update trace_archive_manifests"):
			return fakedb.Result{RowsAffected: 1}, nil
		case strings.Contains(ql, "trace_archive_manifests"):
			if strings.Contains(ql, "state = ") {
				return fixcManifestListResult(sc.manifestList), nil
			}
			return fixcManifestResult(sc.manifest), nil

		// —— 导出 ——
		case strings.Contains(ql, "select ct.request_id"):
			return fixcExportResult(sc.exportRows), nil

		// —— 抽样（random 与首尾 UNION 两条）——
		case strings.Contains(ql, "order by random()"):
			return fixcSampleResult(ql, sc), nil
		case strings.Contains(ql, "union"):
			return fixcSampleResult(ql, sc), nil

		// —— 置空 keyset 游标 ——
		case strings.Contains(ql, "request_id >"):
			if nullifyCalls == 0 {
				nullifyCalls++
				return fixcStringRows(sc.nullifyIDs), nil
			}
			return fixcStringRows(nil), nil

		// —— 「按天聚合」的天查询（补归档 / 未过闸扫描）：返回空，隔离本文件关注点 ——
		case strings.Contains(ql, "select distinct (ts at time zone"):
			return fakedb.MultiRows([]string{"d"}), nil

		// —— 计数类（谓词分流，顺序敏感）——
		case strings.Contains(ql, "count(*)") && strings.Contains(ql, "archive_status = 0"):
			return fakedb.Rows([]string{"count"}, sc.leakRows), nil
		case strings.Contains(ql, "count(*)") && strings.Contains(ql, "request_body is null and response_body is null"):
			return fakedb.Rows([]string{"count"}, sc.noBodyRows), nil
		case strings.Contains(ql, "count(*)") && strings.Contains(ql, "archive_status in (0, 1)"):
			return fakedb.Rows([]string{"count"}, sc.pendingRows), nil
		case strings.Contains(ql, "count(*)") && strings.Contains(ql, "archive_status = 1") &&
			strings.Contains(ql, "request_body is not null"):
			return fakedb.Rows([]string{"count"}, sc.stampedRows), nil
		case strings.Contains(ql, "count(*)") && strings.Contains(ql, "at time zone"):
			return fakedb.Rows([]string{"count"}, sc.liveBodyRows), nil
		case strings.Contains(ql, "count(*)"):
			return fakedb.Rows([]string{"count"}, sc.total), nil

		// —— 写语句 ——
		case strings.Contains(ql, "archive_ref = "):
			return fakedb.Result{RowsAffected: int64(len(sc.exportRows))}, nil
		case strings.Contains(ql, "set request_body = null"):
			return fakedb.Result{RowsAffected: int64(len(sc.nullifyIDs))}, nil

		default:
			return fakedb.Result{}, fmt.Errorf("fixc fakedb: 未脚本化的语句: %s",
				strings.Join(strings.Fields(q), " "))
		}
	})
}

// fixcSampleResult 模拟「有正文」谓词的过滤语义：
// SQL 带 body 谓词 → 只返回有正文的候选；缺谓词 → 无正文行也会被选出（旧口径）。
// 这样「修复被回退」时测试会以行为方式失败，而不只是 SQL 文本比对失败。
func fixcSampleResult(ql string, sc *fixcScript) fakedb.Result {
	if strings.Contains(ql, "request_body is not null") {
		return fixcStringRows(sc.sampleRIDs)
	}
	out := append(append([]string(nil), sc.bodylessRIDs...), sc.sampleRIDs...)
	return fixcStringRows(out)
}

func fixcStringRows(ids []string) fakedb.Result {
	rows := make([][]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, []any{id})
	}
	return fakedb.MultiRows([]string{"request_id"}, rows...)
}

func fixcExportResult(rows [][]any) fakedb.Result {
	return fakedb.MultiRows([]string{
		"request_id", "ts", "request_headers", "request_body", "response_body",
		"error_code", "error_message",
	}, rows...)
}

var fixcManifestColumns = []string{
	"dt", "state", "state_rank", "expected_total", "expected_body_count",
	"expected_body_rids_sha256", "files", "row_count", "verify_state",
	"created_at", "verified_at", "cleaned_at", "last_inspected_at",
}

func fixcManifestResult(m *cpmodel.TraceArchiveManifest) fakedb.Result {
	if m == nil {
		return fakedb.Empty(fixcManifestColumns...)
	}
	return fakedb.MultiRows(fixcManifestColumns, fixcManifestRow(m))
}

func fixcManifestListResult(ms []*cpmodel.TraceArchiveManifest) fakedb.Result {
	rows := make([][]any, 0, len(ms))
	for _, m := range ms {
		rows = append(rows, fixcManifestRow(m))
	}
	return fakedb.MultiRows(fixcManifestColumns, rows...)
}

func fixcManifestRow(m *cpmodel.TraceArchiveManifest) []any {
	var sha, files, verified, cleaned, inspected any
	if m.ExpectedBodyRIDsSHA256 != nil {
		sha = *m.ExpectedBodyRIDsSHA256
	}
	if len(m.Files) > 0 {
		files = []byte(m.Files)
	}
	if m.VerifiedAt != nil {
		verified = *m.VerifiedAt
	}
	if m.CleanedAt != nil {
		cleaned = *m.CleanedAt
	}
	if m.LastInspectedAt != nil {
		inspected = *m.LastInspectedAt
	}
	return []any{
		m.DT, m.State, int64(m.StateRank), m.ExpectedTotal, m.ExpectedBodyCount, sha,
		files, m.RowCount, m.VerifyState, m.CreatedAt, verified, cleaned, inspected,
	}
}

// ─── 夹具 ────────────────────────────────────────────────────────────────────

// fixcTraceLine 构造一行归档 JSONL（withBody=false 时正文为 null，模拟「无正文行」）。
func fixcTraceLine(rid string, withBody bool) []byte {
	rb, resp := "null", "null"
	if withBody {
		rb = fmt.Sprintf(`{"q":%q}`, rid)
		resp = fmt.Sprintf(`{"a":%q}`, rid)
	}
	return []byte(fmt.Sprintf(
		`{"request_id":%q,"ts":"2026-09-19T03:00:00Z","request_headers":{},"request_body":%s,"response_body":%s,"error_code":"","error_message":""}`,
		rid, rb, resp))
}

// fixcWriteDay 用生产 PartWriter 写出一天（part + index.jsonl + manifest.json + _SUCCESS）。
// rids 内部按字节序排序后写入（与导出 SQL `ORDER BY request_id COLLATE "C"` 对齐）。
func fixcWriteDay(t *testing.T, root string, day time.Time, rids []string, chunkBytes int64) ([]archive.IndexEntry, []string) {
	t.Helper()
	ctx := context.Background()
	store := archive.NewLocalStore(root, "")
	store.SetZstdWindowBytes(8 << 20)
	dir := "dt=" + day.Format("2006-01-02")

	ordered := append([]string(nil), rids...)
	sort.Strings(ordered)

	w, err := archive.NewPartWriter(ctx, store, dir, archive.PartWriterOptions{
		ChunkBytes:      chunkBytes,
		ZstdWindowBytes: 8 << 20,
	})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	for _, rid := range ordered {
		if err := w.WriteLine(fixcTraceLine(rid, true)); err != nil {
			t.Fatalf("WriteLine(%s): %v", rid, err)
		}
	}
	res, err := w.Close(ctx)
	if err != nil {
		t.Fatalf("PartWriter.Close: %v", err)
	}
	if _, err := store.Put(ctx, dir+"/"+archive.SuccessMarkerName, strings.NewReader(""), 0,
		archive.PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put _SUCCESS: %v", err)
	}
	return res.Parts, ordered
}

// fixcManifest 构造一个「字段齐全、可通过门闸①②③⑤」的 manifest（对应真实盘上归档）。
func fixcManifest(t *testing.T, day time.Time, state string, parts []archive.IndexEntry, rids []string, bodyCount int64) *cpmodel.TraceArchiveManifest {
	t.Helper()
	files, err := json.Marshal(parts)
	if err != nil {
		t.Fatalf("marshal parts: %v", err)
	}
	sha := frozenSHA(rids...)
	now := time.Now()
	verified := now.Add(-2 * time.Hour)
	inspected := now.Add(-30 * time.Minute)
	return &cpmodel.TraceArchiveManifest{
		DT:                     day,
		State:                  state,
		ExpectedTotal:          bodyCount,
		ExpectedBodyCount:      bodyCount,
		ExpectedBodyRIDsSHA256: &sha,
		Files:                  datatypes.JSON(files),
		RowCount:               bodyCount,
		VerifyState:            cpmodel.ArchiveVerifyOK,
		CreatedAt:              now.Add(-3 * time.Hour),
		VerifiedAt:             &verified,
		LastInspectedAt:        &inspected,
	}
}

func fixcJanitor(t *testing.T, db *fakedb.DB, cfg cpconfig.ArchiveConfig) *ArchiveJanitor {
	t.Helper()
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	archiver := NewTraceArchiveService(gdb, nil, cfg)
	jan := NewTraceArchiveJanitor(gdb, nil, archiver, cfg)
	if jan.store == nil {
		t.Fatalf("janitor store 未构造（root=%q）", cfg.Root)
	}
	return jan
}

// fixcHasArg 判断语句组里是否存在等于 want 的字符串参数（用于断言落库的状态值）。
func fixcHasArg(stmts []fakedb.Stmt, want string) bool {
	for _, st := range stmts {
		for _, a := range st.Args {
			if s, ok := a.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// fixcStringArgs 把 fakedb 记录的参数摊平为字符串（GORM 可能把 []string 展开或整片传入）。
func fixcStringArgs(args []any) []string {
	var out []string
	for _, a := range args {
		switch v := a.(type) {
		case string:
			out = append(out, v)
		case []string:
			out = append(out, v...)
		case []byte:
			out = append(out, string(v))
		}
	}
	return out
}

func fixcSameRIDs(a, b []string) bool {
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func fixcDayPath(root string, day time.Time) string {
	return filepath.Join(root, "dt="+day.Format("2006-01-02"))
}

// ─── T-F1：盖戳集合必须 == 本轮归档集合（rid 列表），且被覆盖断言与门闸⑧双重兜底 ─────

// TestFixC_TF1_PointerStampingIsRIDScopedAndCoverageAsserted 覆盖 F1(a)+(b)。
//
// 场景：该天冻结 expected_total=3 / expected_body_count=2（1 行无正文），本轮导出 2 行；
// 但 PG 中「已盖戳且有正文」的行数是 3——多出来的 1 行正是 F1 描述的「被旧 ts 区间谓词
// 误盖 status=1、却不在归档里」的幽灵行。
//
// 断言：(a) 盖戳语句只点名本轮 rid（不得回退为 ts 区间全量）；
//
//	(b) assertPointerCoverage 必须因此失败并终止本轮（不写/不确认 verified 语义）。
//
// 回退验证：把 writeArchivePointers 的谓词改回 ts 区间 → (a) 失败；
// 去掉 exportDay 里的 assertPointerCoverage 调用 → (b) 失败（会静默成功）。
func TestFixC_TF1_PointerStampingIsRIDScopedAndCoverageAsserted(t *testing.T) {
	ctx := context.Background()
	rids := []string{"req_a", "req_b"}

	// —— 幽灵行存在：已盖戳且有正文的行数 = 归档 2 + 幽灵 1 = 3 ——
	root := t.TempDir()
	sc := &fixcScript{
		total:       3,
		bodyCount:   2,
		bodySHA:     frozenSHA(rids...),
		noBodyRows:  1,
		stampedRows: 3,
		exportRows:  [][]any{exportRow("req_a"), exportRow("req_b")},
	}
	db := fixcDB(sc)
	svc := newArchiveTestService(t, db, root)

	settled, err := svc.ArchiveDay(ctx, archiveTestDay, true)
	if err == nil {
		t.Fatal("(b) 存在「已盖戳却不在归档里」的行时 assertPointerCoverage 必须失败")
	}
	if settled {
		t.Fatal("(b) 覆盖断言失败的天不得报告终态")
	}
	if !strings.Contains(err.Error(), "pointer coverage") {
		t.Fatalf("(b) 错误应来自覆盖断言，got %v", err)
	}

	// —— (a) 盖戳语句必须按本轮 rid 列表，而不是 ts 区间全量 ——
	stamps := db.FindContains("archive_ref = ")
	if len(stamps) == 0 {
		t.Fatal("(a) 未观察到任何盖戳语句")
	}
	for i, st := range stamps {
		if !strings.Contains(st.SQL, "request_id IN") {
			t.Fatalf("(a) 盖戳谓词必须按 request_id 列表，实际: %s", st.SQL)
		}
		if strings.Contains(strings.ToLower(st.SQL), "ts >=") {
			t.Fatalf("(a) 盖戳谓词不得回退为 ts 区间全量（F1 回归）: %s", st.SQL)
		}
		if i == 0 {
			var got []string
			for _, a := range fixcStringArgs(st.Args) {
				if strings.HasPrefix(a, "req_") {
					got = append(got, a)
				}
			}
			if !fixcSameRIDs(got, rids) {
				t.Fatalf("(a) 盖戳参数必须恰好是本轮导出的 rid 列表，got=%v want=%v", got, rids)
			}
		}
	}

	// —— 正向对照：无幽灵行（stamped == expected_body_count）时同一场景必须成功 ——
	root2 := t.TempDir()
	sc2 := &fixcScript{
		total:       3,
		bodyCount:   2,
		bodySHA:     frozenSHA(rids...),
		noBodyRows:  1,
		stampedRows: 2,
		exportRows:  [][]any{exportRow("req_a"), exportRow("req_b")},
	}
	if settled, err := newArchiveTestService(t, fixcDB(sc2), root2).ArchiveDay(ctx, archiveTestDay, true); err != nil || !settled {
		t.Fatalf("正向对照应成功（证明 (b) 的失败确实来自幽灵行）: settled=%v err=%v", settled, err)
	}
}

// TestFixC_TF1_Gate8BlocksStampedButUnarchivedDay 覆盖 F1(c)。
//
// 场景同 T-F1：已盖戳且有正文的行数（4）> expected_body_count（3）。
// 断言：janitor 门闸⑧拦下该天 → checkNullifyGates 返回 ⑧；
//
//	nullifyVerifiedDays 不执行任何 SET body=NULL，也不把该天计入 cleaned，并留审计。
//
// 回退验证：去掉 checkNullifyGates 里的 verifyStampedBodyCount 调用 → 门闸全过并置空 → 失败。
func TestFixC_TF1_Gate8BlocksStampedButUnarchivedDay(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rids := []string{"req_000", "req_001", "req_002"}
	parts, ordered := fixcWriteDay(t, root, archiveTestDay, rids, 1<<20)
	// 该天 PG 中已盖戳且有正文的行数比冻结基准多 1（幽灵行）。
	const expectedBody = 3
	m := fixcManifest(t, archiveTestDay, cpmodel.ArchiveStateVerified, parts, ordered, expectedBody)

	cfg := archiveTestConfig(root)
	cfg.RetentionDays = 0

	// —— 直接门闸判定：必须停在 ⑧（④逐块 sha256 + 抽样回读都健康，故失败只可能来自⑧）——
	db := fixcDB(&fixcScript{stampedRows: expectedBody + 1, sampleRIDs: ordered})
	jan := fixcJanitor(t, db, cfg)
	g := jan.checkNullifyGates(ctx, m, time.Now())
	if g.OK {
		t.Fatal("(c) 盖戳集合 != 归档集合时门闸必须拦下（F1 回归）")
	}
	if g.Gate != "⑧" {
		t.Fatalf("(c) 应由门闸⑧拦下，实际 gate=%s reason=%s", g.Gate, g.Reason)
	}

	// —— 全流程：不得置空、不得计入 cleaned、必须留审计 ——
	db2 := fixcDB(&fixcScript{
		manifestList: []*cpmodel.TraceArchiveManifest{m},
		stampedRows:  expectedBody + 1,
		sampleRIDs:   ordered,
		nullifyIDs:   ordered,
	})
	jan2 := fixcJanitor(t, db2, cfg)
	cleaned, err := jan2.nullifyVerifiedDays(ctx, "2026-09-20", time.Now())
	if err != nil {
		t.Fatalf("门闸跳过不应变成致命错误: %v", err)
	}
	if n := db2.CountContains("SET request_body = NULL"); n != 0 {
		t.Fatalf("(c) 门闸⑧失败却置空了正文 %d 次（不可逆丢失）", n)
	}
	if cleaned["2026-09-19"] {
		t.Fatal("(c) 门闸⑧失败的 dt 不得计入 cleaned")
	}
	b, rerr := os.ReadFile(filepath.Join(root, janitorAuditDirName, janitorAuditSkipFile))
	if rerr != nil {
		t.Fatalf("(c) 门闸失败必须留审计: %v", rerr)
	}
	if !strings.Contains(string(b), "⑧") {
		t.Fatalf("(c) 审计应记录门闸编号⑧: %s", b)
	}
}

// ─── T-F4：verified 天有 leak → 重新归档；cleaned 天有 leak → 报错且不重归档 ─────────

// TestFixC_TF4_VerifiedLeakReArchives_CleanedLeakErrors 覆盖 F4。
//
// 回退验证：
//   - 把 verified 分支改回 `return false, false, error` → 第一段失败（重新归档未发生）；
//   - 把 cleaned 分支改回重新归档/静默收敛 → 第二段失败（错误与「不得重归档」断言破裂）。
func TestFixC_TF4_VerifiedLeakReArchives_CleanedLeakErrors(t *testing.T) {
	ctx := context.Background()
	rids := []string{"req_a", "req_b"}

	// —— verified 天 + leak 行：必须【重新归档】而不是直接报错 ——
	{
		root := t.TempDir()
		m := &cpmodel.TraceArchiveManifest{
			DT:        archiveTestDay,
			State:     cpmodel.ArchiveStateVerified,
			CreatedAt: time.Now().Add(-3 * time.Hour),
		}
		db := fixcDB(&fixcScript{
			total:       2,
			bodyCount:   2,
			bodySHA:     frozenSHA(rids...),
			noBodyRows:  0,
			leakRows:    1, // 晚到/漏网行
			stampedRows: 2,
			exportRows:  [][]any{exportRow("req_a"), exportRow("req_b")},
			manifest:    m,
		})
		svc := newArchiveTestService(t, db, root)

		// force=false：只有这样才能走进「已终态天」的分支。
		settled, err := svc.ArchiveDay(ctx, archiveTestDay, false)
		if err != nil {
			t.Fatalf("verified 天存在 leak 行时必须重新归档而不是直接报错: %v", err)
		}
		if !settled {
			t.Fatal("重新归档成功后该天应处于终态")
		}
		if db.CountContains("INSERT INTO trace_archive_manifests") == 0 {
			t.Fatal("未观察到 archiving UPSERT：说明根本没有重新归档")
		}
		if db.CountContains("archive_ref = ") == 0 {
			t.Fatal("重新归档后必须重写指针")
		}
		files := dayFiles(t, root, archiveTestDay)
		var hasIndex bool
		for _, f := range files {
			if f == archive.IndexFileName {
				hasIndex = true
			}
		}
		if !hasIndex {
			t.Fatalf("重新归档未产出 %s: %v", archive.IndexFileName, files)
		}
	}

	// —— cleaned 天 + leak 行：必须报错且不得重归档 ——
	{
		root := t.TempDir()
		m := &cpmodel.TraceArchiveManifest{
			DT:        archiveTestDay,
			State:     cpmodel.ArchiveStateCleaned,
			CreatedAt: time.Now().Add(-3 * time.Hour),
		}
		db := fixcDB(&fixcScript{manifest: m, leakRows: 2})
		svc := newArchiveTestService(t, db, root)

		settled, err := svc.ArchiveDay(ctx, archiveTestDay, false)
		if err == nil {
			t.Fatal("cleaned 天仍存在 unarchived 正文行必须报错（交人工复核）")
		}
		if settled {
			t.Fatal("cleaned 天异常时不得报告终态")
		}
		if n := db.CountContains("INSERT INTO trace_archive_manifests"); n != 0 {
			t.Fatalf("cleaned 天绝不重新归档，却写了 %d 次 manifest（与已置空语义冲突）", n)
		}
		if n := db.CountContains("archive_ref = "); n != 0 {
			t.Fatalf("cleaned 天不得写指针，却写了 %d 次", n)
		}
	}

	// —— 正向对照：cleaned 天无 leak → 正常收敛，不报错 ——
	{
		root := t.TempDir()
		m := &cpmodel.TraceArchiveManifest{
			DT:        archiveTestDay,
			State:     cpmodel.ArchiveStateCleaned,
			CreatedAt: time.Now().Add(-3 * time.Hour),
		}
		db := fixcDB(&fixcScript{manifest: m, leakRows: 0})
		svc := newArchiveTestService(t, db, root)
		settled, err := svc.ArchiveDay(ctx, archiveTestDay, false)
		if err != nil || !settled {
			t.Fatalf("无 leak 的 cleaned 天应正常收敛: settled=%v err=%v", settled, err)
		}
	}
}

// ─── T-F2b：盘上已有 index.jsonl 而冻结 bodyCount==0 ────────────────────────────

// TestFixC_TF2b_ExistingIndexWithZeroBodyCountKeepsSuccessAndMarksSuspect 覆盖 F2b。
//
// 场景：该天此前归档成功（盘上有 index.jsonl + _SUCCESS），但本轮冻结基准判定 bodyCount==0
// （manifest 状态被外部改动/丢失）。旧实现会删掉 _SUCCESS 并标 empty —— empty 天永不被巡检，
// 也没有任何路径重写 _SUCCESS，该天归档即永久不可读。
//
// 断言：报错；_SUCCESS 保留；不标 empty；改标 suspect；index.jsonl 原样保留。
//
// 回退验证：删掉 exportDay 里的 ①' 守卫块 → 会走「删 _SUCCESS + 标 empty」→ 失败。
func TestFixC_TF2b_ExistingIndexWithZeroBodyCountKeepsSuccessAndMarksSuspect(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rids := []string{"req_000", "req_001"}
	if _, ordered := fixcWriteDay(t, root, archiveTestDay, rids, 1<<20); len(ordered) != len(rids) {
		t.Fatalf("夹具写入行数异常: %v", ordered)
	}
	dirPath := fixcDayPath(root, archiveTestDay)
	markerPath := filepath.Join(dirPath, archive.SuccessMarkerName)
	indexPath := filepath.Join(dirPath, archive.IndexFileName)

	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.jsonl: %v", err)
	}

	// 冻结基准：该天全量 5 行，但「有正文」为 0（正文早已离场/状态被外部改动）。
	db := fixcDB(&fixcScript{total: 5, bodyCount: 0, bodySHA: "", noBodyRows: 5})
	svc := newArchiveTestService(t, db, root)

	settled, err := svc.ArchiveDay(ctx, archiveTestDay, true)
	if err == nil {
		t.Fatal("盘上有 index.jsonl 而冻结 expected_body_count==0 必须报错（交人工复核）")
	}
	if settled {
		t.Fatal("suspect 天不是终态，不得报告 settled")
	}
	if _, serr := os.Stat(markerPath); serr != nil {
		t.Fatalf("_SUCCESS 不得被删除（删除即该天归档永久不可读）: %v", serr)
	}
	inserts := db.FindContains("INSERT INTO trace_archive_manifests")
	if fixcHasArg(inserts, cpmodel.ArchiveStateEmpty) {
		t.Fatal("不得标记 empty（empty 天不会进入巡检候选集）")
	}
	if !fixcHasArg(inserts, cpmodel.ArchiveStateSuspect) {
		t.Fatalf("必须标记 suspect，inserts=%+v", inserts)
	}
	indexAfter, rerr := os.ReadFile(indexPath)
	if rerr != nil {
		t.Fatalf("index.jsonl 不得被删除: %v", rerr)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("index.jsonl 内容被改动（守卫必须在任何破坏性动作之前 fail-closed）")
	}
	if n := db.CountContains("archive_ref = "); n != 0 {
		t.Fatalf("suspect 天不得写指针，却写了 %d 次", n)
	}
}

// ─── T-F2：巡检/保留期删除对 index 不可读与 last_rid 被改坏的判定 ─────────────────

// TestFixC_TF2_InspectSuspectWhenIndexUnreadable 覆盖 F2 缺陷1。
//
// 场景：PG 的 manifest.files 完好，但盘上 index.jsonl 被写坏。
// 点读流程强依赖盘上 index（_SUCCESS → LoadIndex → FindPart），读不出会让 ReadBody 返回
// CodeNotFound，对用户表现为「数据不存在」，且 verified 天在非 force 路径下永不重导出。
//
// 回退验证：把 LoadIndex 失败改回 slog.Warn + 继续 → verifyDayArchive 返回 nil → 失败。
func TestFixC_TF2_InspectSuspectWhenIndexUnreadable(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rids := []string{"req_000", "req_001", "req_002"}
	parts, ordered := fixcWriteDay(t, root, archiveTestDay, rids, 1<<20)
	m := fixcManifest(t, archiveTestDay, cpmodel.ArchiveStateVerified, parts, ordered, int64(len(ordered)))

	db := fixcDB(&fixcScript{})
	jan := fixcJanitor(t, db, archiveTestConfig(root))

	// 基线：健康归档巡检必须通过（防止用例因别的原因假绿）。
	if err := jan.verifyDayArchive(ctx, m); err != nil {
		t.Fatalf("基线巡检应通过: %v", err)
	}

	indexPath := filepath.Join(fixcDayPath(root, archiveTestDay), archive.IndexFileName)
	if err := os.WriteFile(indexPath, []byte("{not-a-json-index-line"), 0o640); err != nil {
		t.Fatalf("corrupt index.jsonl: %v", err)
	}

	err := jan.verifyDayArchive(ctx, m)
	if err == nil {
		t.Fatal("index.jsonl 读不出时巡检必须失败（不得只 Warn 后继续）")
	}
	if !strings.Contains(err.Error(), "index.jsonl") {
		t.Fatalf("错误应指向 index.jsonl，got %v", err)
	}

	// 巡检结论必须落 suspect（冻结该天的置空与保留期删除）。
	jan.inspectDay(ctx, m, time.Now())
	if !fixcHasArg(db.FindContains("verify_state"), cpmodel.ArchiveVerifySuspect) {
		t.Fatal("index 不可读必须落 verify_state=suspect")
	}
}

// TestFixC_TF2_InspectSuspectWhenIndexLastRIDCorrupted 覆盖 F2 缺陷2。
//
// 场景：index.jsonl 的块名/块数/rows 全部保持原样，只把某块的 last_rid 改坏。
// 点读用盘上 index 的 LastRID 做二分，改坏即「块名一致但点读全 miss」，只比块名/块数会漏检。
//
// 回退验证：删掉 verifyDayArchive 里的逐条 first_rid/last_rid/rows 比对 → 巡检返回 nil → 失败。
func TestFixC_TF2_InspectSuspectWhenIndexLastRIDCorrupted(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rids := []string{"req_000", "req_001", "req_002"}
	parts, ordered := fixcWriteDay(t, root, archiveTestDay, rids, 256) // 小块 → 多块
	if len(parts) < 2 {
		t.Fatalf("夹具未产生多块（blocks=%d），无法定位「某一块的 last_rid」", len(parts))
	}
	m := fixcManifest(t, archiveTestDay, cpmodel.ArchiveStateVerified, parts, ordered, int64(len(ordered)))
	dir := "dt=" + archiveTestDay.Format("2006-01-02")

	jan := fixcJanitor(t, fixcDB(&fixcScript{}), archiveTestConfig(root))
	if err := jan.verifyDayArchive(ctx, m); err != nil {
		t.Fatalf("基线巡检应通过: %v", err)
	}

	// 只改坏盘上 index 的 last_rid（manifest.files 保持原值）。
	tampered := append([]archive.IndexEntry(nil), parts...)
	tampered[0].LastRID = "zzz_" + tampered[0].LastRID
	store := archive.NewLocalStore(root, "")
	store.SetZstdWindowBytes(8 << 20)
	if err := archive.WriteIndex(ctx, store, dir, tampered); err != nil {
		t.Fatalf("改写 index.jsonl: %v", err)
	}

	err := jan.verifyDayArchive(ctx, m)
	if err == nil {
		t.Fatal("index 的 last_rid 被改坏时巡检必须判失败（点读按 LastRID 二分，会全 miss）")
	}
	if !strings.Contains(err.Error(), "last_rid") {
		t.Fatalf("错误应指向 last_rid，got %v", err)
	}
}

// TestFixC_TF2_RetentionGateRefusesUnreadableIndex 覆盖 F2 缺陷3。
//
// 场景：某 cleaned 天满足保留期删除的全部既有门闸（巡检 ok 且新鲜、不在保护窗、PG 已无正文、
// _SUCCESS 存在），但盘上 index.jsonl 不可读。删除会移除唯一可恢复副本 —— 必须拒绝。
//
// 回退验证：删掉 retentionGate 的 R5 段（_SUCCESS + LoadIndex）→ 返回 OK 并删除目录 → 失败。
func TestFixC_TF2_RetentionGateRefusesUnreadableIndex(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	// 用较早的 dt，避免保护窗（hot_window_days）干扰；并把 hot_window 压到 1 天。
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	rids := []string{"req_000", "req_001"}
	parts, ordered := fixcWriteDay(t, root, day, rids, 1<<20)
	m := fixcManifest(t, day, cpmodel.ArchiveStateCleaned, parts, ordered, int64(len(ordered)))
	cleaned := time.Now().Add(-2 * time.Hour)
	m.CleanedAt = &cleaned

	cfg := archiveTestConfig(root)
	cfg.HotWindowDays = 1
	cfg.RetentionDays = 30

	dayPath := fixcDayPath(root, day)
	indexPath := filepath.Join(dayPath, archive.IndexFileName)
	if err := os.WriteFile(indexPath, []byte("{not-a-json-index-line"), 0o640); err != nil {
		t.Fatalf("corrupt index.jsonl: %v", err)
	}

	db := fixcDB(&fixcScript{manifestList: []*cpmodel.TraceArchiveManifest{m}, liveBodyRows: 0})
	jan := fixcJanitor(t, db, cfg)

	g := jan.retentionGate(ctx, m, time.Now())
	if g.OK {
		t.Fatal("index.jsonl 不可读时必须拒绝留存期删除（删除后无法点读）")
	}
	if g.Gate != "R5" {
		t.Fatalf("应由 R5 拦下，实际 gate=%s reason=%s", g.Gate, g.Reason)
	}

	// 全流程：RetentionSweep 不得删除该天归档目录。
	jan.RetentionSweep(ctx)
	if _, err := os.Stat(dayPath); err != nil {
		t.Fatalf("索引不可读的归档目录不得被删除: %v", err)
	}
}

// ─── T-F3：门闸④的逐块 sha256 确定性校验 ─────────────────────────────────────

// TestFixC_TF3_Gate4FailsOnPerPartSHA256MismatchDespiteSamplesHitting 覆盖 F3。
//
// 场景：多块归档中第 2 块被替换为「同内容重新编码」的帧 —— 可正常解压、rid 全在、抽样
// （本例 verify_sample_rows=20，覆盖全部候选）全部命中且正文非空，因此 4b 抽样回读
// 必然通过；唯一能拦住它的是 4a「逐块 sha256 与 manifest.files 比对」。
//
// 断言：sampleReadBack 通过；verifyAllPartsSHA256 失败；checkNullifyGates 停在 ④ 且 Suspect；
//
//	nullifyVerifiedDays 不置空 + 落 suspect。
//
// 回退验证：删掉 checkNullifyGates 里的 verifyAllPartsSHA256 调用（只剩抽样）→ 门闸通过并置空 → 失败。
func TestFixC_TF3_Gate4FailsOnPerPartSHA256MismatchDespiteSamplesHitting(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rids := []string{"req_000", "req_001", "req_002", "req_003", "req_004"}
	parts, ordered := fixcWriteDay(t, root, archiveTestDay, rids, 256)
	if len(parts) < 2 {
		t.Fatalf("夹具未产生多块（blocks=%d）", len(parts))
	}
	dir := "dt=" + archiveTestDay.Format("2006-01-02")
	m := fixcManifest(t, archiveTestDay, cpmodel.ArchiveStateVerified, parts, ordered, int64(len(ordered)))

	cfg := archiveTestConfig(root)
	cfg.VerifySampleRows = 20 // 抽样覆盖全部候选：让「抽样命中」不再是变量
	cfg.RetentionDays = 0

	// —— 基线：门闸全过 ——
	janOK := fixcJanitor(t, fixcDB(&fixcScript{
		stampedRows: int64(len(ordered)),
		sampleRIDs:  ordered,
	}), cfg)
	if g := janOK.checkNullifyGates(ctx, m, time.Now()); !g.OK {
		t.Fatalf("基线门闸应全过: gate=%s reason=%s", g.Gate, g.Reason)
	}

	// —— 篡改第 2 块：同内容重新编码（rid 不变、可解压），压缩字节不同 → sha256 不符 ——
	target := parts[1]
	lines := decodePartForTest(t, root, dir, target)
	origBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), target.Part))
	if err != nil {
		t.Fatalf("read part: %v", err)
	}
	newBytes := fixcEncodePartDifferent(t, lines)
	if bytes.Equal(origBytes, newBytes) {
		t.Fatal("夹具问题：重新编码未产生字节差异，无法构造 sha256 不符场景")
	}
	putPartForTest(t, root, dir, target, newBytes)

	jan := fixcJanitor(t, fixcDB(&fixcScript{
		stampedRows: int64(len(ordered)),
		sampleRIDs:  ordered,
	}), cfg)

	// 4b 抽样回读必须仍然通过（证明失败只可能来自 4a，而不是抽样没命中）。
	if err := jan.sampleReadBack(ctx, m); err != nil {
		t.Fatalf("抽样回读应全部命中（该场景专门隔离 4a）: %v", err)
	}
	if err := jan.verifyAllPartsSHA256(ctx, m, dir); err == nil {
		t.Fatal("逐块 sha256 必须发现「抽样未覆盖的块」被替换")
	}
	g := jan.checkNullifyGates(ctx, m, time.Now())
	if g.OK {
		t.Fatal("F3 回归：某块 sha256 与 manifest 不符时门闸④必须失败（即使抽样恰好全命中）")
	}
	if g.Gate != "④" {
		t.Fatalf("应由门闸④拦下，实际 gate=%s reason=%s", g.Gate, g.Reason)
	}
	if !g.Suspect {
		t.Fatal("归档数据完整性缺陷必须判 Suspect（落 verify_state=suspect，冻结置空与删除）")
	}

	// —— janitor 全流程：不置空 + 落 suspect ——
	dbRun := fixcDB(&fixcScript{
		manifestList: []*cpmodel.TraceArchiveManifest{m},
		stampedRows:  int64(len(ordered)),
		sampleRIDs:   ordered,
		nullifyIDs:   ordered,
	})
	janRun := fixcJanitor(t, dbRun, cfg)
	cleaned, err := janRun.nullifyVerifiedDays(ctx, "2026-09-20", time.Now())
	if err != nil {
		t.Fatalf("门闸跳过不应变成致命错误: %v", err)
	}
	if n := dbRun.CountContains("SET request_body = NULL"); n != 0 {
		t.Fatalf("逐块 sha256 不符却置空了正文 %d 次", n)
	}
	if cleaned["2026-09-19"] {
		t.Fatal("归档完整性失败的天不得计入 cleaned")
	}
	if !fixcHasArg(dbRun.FindContains("verify_state"), cpmodel.ArchiveVerifySuspect) {
		t.Fatal("必须落 verify_state=suspect")
	}
}

// fixcEncodePartDifferent 用与生产写入侧不同的 zstd 参数（不同窗口 + SpeedFastest）
// 重新编码同一批行：内容等价（ReadBack/抽样仍命中）但压缩字节不同（sha256 必不符）。
func fixcEncodePartDifferent(t *testing.T, lines [][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithWindowSize(1<<20),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	for _, line := range lines {
		if _, err := zw.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			t.Fatalf("zstd write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	return buf.Bytes()
}

// ─── T-F5：nullifyDay 游标必须显式 COLLATE "C" ─────────────────────────────────

// TestFixC_TF5_NullifyCursorUsesCollateC 覆盖 F5。
//
// request_id 是客户端可控任意文本；若游标比较漏掉 COLLATE "C"，在默认 collation 非 C 的库上
// 会与 ORDER BY 的字节序不一致 → 批次满 N 条时跳行 → 被跳过的行永不置空 → 该天到不了 cleaned。
//
// 回退验证：删掉 `request_id > ? COLLATE "C"` 里的 COLLATE → 正则断言失败。
func TestFixC_TF5_NullifyCursorUsesCollateC(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cfg := archiveTestConfig(root)
	cfg.BatchRows = 3 // 本例候选 2 条 < 3 → 单批结束，避免无意义的循环
	cfg.RetentionDays = 0

	ids := []string{"req_000", "req_001"}
	db := fixcDB(&fixcScript{nullifyIDs: ids})
	jan := fixcJanitor(t, db, cfg)

	if _, err := jan.nullifyDay(ctx, "2026-09-19"); err != nil {
		t.Fatalf("nullifyDay: %v", err)
	}

	var batch *fakedb.Stmt
	for _, st := range db.FindContains("FROM call_traces") {
		if strings.Contains(st.SQL, "request_id >") {
			s := st
			batch = &s
			break
		}
	}
	if batch == nil {
		t.Fatal("未观察到 keyset 游标查询")
	}
	// GORM 会把 ? 占位符改写成 $N，故两者都要接受。
	cursorCollate := regexp.MustCompile(`request_id\s*>\s*(?:\$\d+|\?)\s*COLLATE\s+"?C"?`)
	if !cursorCollate.MatchString(batch.SQL) {
		t.Fatalf("游标比较必须显式 COLLATE \"C\"（F5 回归，会导致跳行/永不 cleaned）: %s", batch.SQL)
	}
	orderCollate := regexp.MustCompile(`ORDER BY\s+request_id\s+COLLATE\s+"?C"?`)
	if !orderCollate.MatchString(batch.SQL) {
		t.Fatalf("ORDER BY 必须显式 COLLATE \"C\": %s", batch.SQL)
	}
	if db.CountContains("SET request_body = NULL") == 0 {
		t.Fatal("夹具未走到置空（游标查询本身就没被执行）")
	}
}

// ─── T-F6：抽样候选必须排除无正文行 ───────────────────────────────────────────

// TestFixC_TF6_SampleCandidatesRequireNonEmptyBody 覆盖 F6。
//
// 历史天（F1 修复前）可能留下「archive_status=1 但正文为空」的行。若抽样候选不加 body 谓词，
// 这类行会被抽出来做 ReadBody → 必然 NotFound → 门闸④失败 → 整天被跳过；当该天 C 序首/尾
// rid 恰是这类行时【每轮必然】命中，该天永久无法置空。
//
// 断言：(1) 随机与首尾两条候选查询都带 body 非空谓词；
//
//	(2) 正常天（F1 修复后，已无此类行被选中）门闸必须通过，且抽样结果里不出现无正文行。
//
// 假 DB 会按「SQL 是否真的带 body 谓词」决定返回哪些候选（缺谓词则把无正文行也选出），
// 因此回退该修复会以行为方式失败（门闸被误拦），而不只是文本断言失败。
func TestFixC_TF6_SampleCandidatesRequireNonEmptyBody(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rids := []string{"req_000", "req_001", "req_002"}
	parts, ordered := fixcWriteDay(t, root, archiveTestDay, rids, 1<<20)
	m := fixcManifest(t, archiveTestDay, cpmodel.ArchiveStateVerified, parts, ordered, int64(len(ordered)))

	cfg := archiveTestConfig(root)
	cfg.VerifySampleRows = 20
	cfg.RetentionDays = 0

	db := fixcDB(&fixcScript{
		stampedRows:  int64(len(ordered)),
		sampleRIDs:   ordered,
		bodylessRIDs: []string{"req_zzz_nobody"},
	})
	jan := fixcJanitor(t, db, cfg)

	got, err := jan.sampleRIDs(ctx, "2026-09-19", 20)
	if err != nil {
		t.Fatalf("sampleRIDs: %v", err)
	}
	// (1) 行为断言（优先，保证回退时以「被误拦」而不是纯文本比对失败）：
	//     抽到的样本里不得出现无正文行，且正常天门闸必须通过。
	for _, rid := range got {
		if rid == "req_zzz_nobody" {
			t.Fatalf("抽样选中了无正文行（F6 回归）: %v", got)
		}
	}
	if g := jan.checkNullifyGates(ctx, m, time.Now()); !g.OK {
		t.Fatalf("F1 修复后的正常天必须通过门闸（抽到无正文行不得误拦）: gate=%s reason=%s", g.Gate, g.Reason)
	}
	// (2) SQL 断言：候选查询必须显式带 body 非空谓词
	//     （随机 + 首尾，共 3 条子查询含 archive_status IN (1, 2)）。
	cands := db.FindContains("archive_status IN (1, 2)")
	if len(cands) < 2 {
		t.Fatalf("应捕获随机与首尾候选查询，实际 %d 条", len(cands))
	}
	for _, st := range cands {
		if !strings.Contains(strings.ToLower(st.SQL), "request_body is not null") {
			t.Fatalf("抽样候选查询缺 body 非空谓词（F6 回归，无正文行会被选出并误拦整天）: %s", st.SQL)
		}
	}
}
