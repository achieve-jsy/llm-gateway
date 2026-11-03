// archive_v3_neg_test.go —— WS-F：v3 §11 / 契约 §8 的负向测试（服务与清理路径）。
//
// 覆盖 N4（manifest 落库失败 → 本轮终止且指针未写入）、N5（陈旧 _SUCCESS → 校验③失败）、
// N6（expected_body_count==0 → empty，不写指针不置空）、N7（抽样回读失败 → janitor 跳过该 dt 并告警）。
//
// 全部使用假 DB（tests/archive/fakedb：拒绝真实 PostgreSQL）+ 真实 LocalStore（t.TempDir）。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/archive"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/tests/archive/fakedb"
)

// ─── 夹具 ────────────────────────────────────────────────────────────────────

var archiveTestDay = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)

// archiveNegHandler 是负向测试共用的假 DB 脚本：按 SQL 形状返回脚本化结果。
//
// 匹配顺序很重要：基准 2 的 SQL 同时含 count(*) 与 string_agg，必须先匹配更具体的形状。
type archiveNegScenario struct {
	total      int64            // 基准 1：该天全量行数（不含 body 谓词）
	bodyCount  int64            // 基准 2：有正文行数
	bodySHA    string           // 基准 2：rid 摘要（bodyCount>0 时必填）
	noBodyRows int64            // 无正文行数（expected_total - expected_body_count）
	exportRows [][]any          // 导出批次（fetchBatch）返回的行
	insertErr  error            // 注入：manifest UPSERT 落库失败
	pointerErr error            // 注入：写指针失败
	manifest   *manifestFixture // 非 nil 时返回该 manifest（janitor 路径）
	sampleIDs  []string         // janitor 抽样 rid
	nullifyIDs []string         // janitor 置空候选 rid
}

// manifestFixture 假 DB 返回的 manifest 行（只给业务真正读取的列）。
type manifestFixture struct {
	dt            time.Time
	state         string
	verifyState   string
	ridsSHA       string
	createdAt     time.Time
	verifiedAt    time.Time
	lastInspected time.Time
}

// scriptDB 依据 scenario 构造假 DB。
func scriptDB(s archiveNegScenario) *fakedb.DB {
	return fakedb.New(func(raw string, _ []any) (fakedb.Result, error) {
		// GORM 会给标识符加双引号（UPDATE "trace_archive_manifests" ...），先归一化再匹配。
		q := strings.ReplaceAll(raw, `"`, "")
		switch {
		case strings.Contains(q, "string_agg"):
			var sha any
			if s.bodySHA != "" {
				sha = s.bodySHA
			}
			return fakedb.Rows([]string{"cnt", "sha"}, s.bodyCount, sha), nil
		case strings.Contains(q, "count(*)") && strings.Contains(q, "call_traces"):
			return fakedb.Rows([]string{"count"}, s.total), nil
		case strings.Contains(q, "request_body IS NULL AND response_body IS NULL"):
			return fakedb.Rows([]string{"count"}, s.noBodyRows), nil
		case strings.Contains(q, "SELECT ct.request_id"):
			return fakedb.MultiRows([]string{
				"request_id", "ts", "request_headers", "request_body", "response_body",
				"error_code", "error_message",
			}, s.exportRows...), nil
		case strings.Contains(q, "ORDER BY random()"):
			return stringRows(s.sampleIDs), nil
		case strings.Contains(q, "UNION") && strings.Contains(q, "archive_status IN (1, 2)"):
			return stringRows(s.sampleIDs), nil
		case strings.Contains(q, "archive_status = 1") && strings.Contains(q, "request_id >"):
			return stringRows(s.nullifyIDs), nil
		case strings.Contains(q, "archive_status IN (0, 1)"):
			return fakedb.Rows([]string{"count"}, int64(0)), nil
		case strings.Contains(q, "SELECT * FROM") && strings.Contains(q, "trace_archive_manifests"):
			return manifestRows(s.manifest), nil
		case strings.Contains(q, "INSERT INTO trace_archive_manifests"):
			if s.insertErr != nil {
				return fakedb.Err(s.insertErr), nil
			}
			return fakedb.Result{RowsAffected: 1}, nil
		case strings.Contains(q, "UPDATE trace_archive_manifests"):
			return fakedb.Result{RowsAffected: 1}, nil
		case strings.Contains(q, "archive_ref = "):
			if s.pointerErr != nil {
				return fakedb.Err(s.pointerErr), nil
			}
			return fakedb.Result{RowsAffected: int64(len(s.exportRows))}, nil
		case strings.Contains(q, "SET request_body = NULL"):
			return fakedb.Result{RowsAffected: int64(len(s.nullifyIDs))}, nil
		default:
			return fakedb.Result{}, fmt.Errorf(
				"fakedb: 未脚本化的语句（若实现改写了 SQL 口径，请同步更新 WS-F 的脚本）: %s",
				strings.Join(strings.Fields(q), " "))
		}
	})
}

func stringRows(ids []string) fakedb.Result {
	rows := make([][]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, []any{id})
	}
	return fakedb.MultiRows([]string{"request_id"}, rows...)
}

func manifestRows(m *manifestFixture) fakedb.Result {
	if m == nil {
		return fakedb.Empty("dt", "state", "state_rank", "expected_body_rids_sha256",
			"verify_state", "created_at", "verified_at", "last_inspected_at")
	}
	return fakedb.MultiRows([]string{
		"dt", "state", "state_rank", "expected_body_rids_sha256",
		"verify_state", "created_at", "verified_at", "last_inspected_at",
	}, []any{
		m.dt, m.state, int64(1), m.ridsSHA, m.verifyState,
		m.createdAt, m.verifiedAt, m.lastInspected,
	})
}

// frozenSHA 计算「SQL 侧冻结 rid 摘要」的等价物：
// sha256(rid 按字节序升序、以 "\n" 串联（无尾随换行）) 的十六进制。
// 与 archive.ReadBack / PartWriter 的摘要口径一致（契约 §5.1 基准 2）。
func frozenSHA(rids ...string) string {
	ordered := append([]string(nil), rids...)
	sort.Strings(ordered) // Go 字符串比较即字节序 == PostgreSQL COLLATE "C"
	h := sha256.New()
	for i, rid := range ordered {
		if i > 0 {
			_, _ = h.Write([]byte("\n"))
		}
		_, _ = h.Write([]byte(rid))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// exportRow 构造一条导出批次行（与 archiveExportRow 列顺序一致）。
func exportRow(rid string) []any {
	ts := archiveTestDay.Add(3 * time.Hour)
	return []any{
		rid, ts,
		[]byte(`{"x-rid":"` + rid + `"}`),
		[]byte(`{"model":"gpt-4o-mini","rid":"` + rid + `"}`),
		[]byte(`{"content":"回答 ` + rid + `"}`),
		"", "",
	}
}

// archiveTestConfig 返回一份「写路径打开」的配置（root 为临时目录）。
func archiveTestConfig(root string) cpconfig.ArchiveConfig {
	return cpconfig.ArchiveConfig{
		WriteEnabled:          true,
		ReadEnabled:           false,
		Root:                  root,
		TZ:                    "UTC",
		HotWindowDays:         3,
		DaysPerNight:          2,
		ClosureLagHours:       24,
		CronSchedule:          "0 3 * * *",
		ChunkMB:               16,
		ZstdLevel:             "best",
		ZstdWindowMB:          8,
		EncoderConcurrency:    1,
		MaxConcurrentReads:    4,
		BatchRows:             1000,
		MaxRetry:              5,
		VerifySampleRows:      2,
		InspectFreshnessHours: 168,
		InspectPerCycle:       3,
		RequireOffhostCopy:    false,
		DiskWatermarkPercent:  100,
	}
}

func newArchiveTestService(t *testing.T, db *fakedb.DB, root string) *ArchiveService {
	t.Helper()
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	return NewTraceArchiveService(gdb, nil, archiveTestConfig(root))
}

// dayFiles 列出天目录下的文件（不含 .spool 目录内部）。
func dayFiles(t *testing.T, root string, day time.Time) []string {
	t.Helper()
	dir := filepath.Join(root, "dt="+day.Format("2006-01-02"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read day dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// ─── N4：manifest 落库失败 → 本轮终止，且指针未被写入（防 BLK-01 回归）────────

func TestN4_ManifestWriteFailureAbortsBeforePointerWrite(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	boom := errors.New("injected manifest insert failure")
	exp := archiveNegScenario{
		total:      2,
		bodyCount:  2,
		bodySHA:    frozenSHA("req_a", "req_b"),
		noBodyRows: 0,
		exportRows: [][]any{exportRow("req_a"), exportRow("req_b")},
		insertErr:  boom,
	}
	db := scriptDB(exp)
	svc := newArchiveTestService(t, db, root)

	settled, err := svc.ArchiveDay(ctx, archiveTestDay, true)
	if err == nil {
		t.Fatal("manifest 落库失败时 ArchiveDay 必须返回 error（本轮终止）")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("错误必须保留底层原因，got %v", err)
	}
	if settled {
		t.Fatal("落库失败不得报告该天已终态")
	}
	// 核心断言：冻结集落库失败后，绝不能写指针（否则 janitor 会在错误前提下置空正文）。
	if n := db.CountContains("archive_ref = "); n != 0 {
		t.Fatalf("指针被写入 %d 次（BLK-01 回归）: %+v", n, db.FindContains("archive_ref = "))
	}
	// 且不得产生任何归档文件（契约 §6：manifest 落库失败 → 不写任何归档文件）。
	for _, name := range dayFiles(t, root, archiveTestDay) {
		if name == archive.IndexFileName || name == archive.SuccessMarkerName || strings.HasSuffix(name, ".zst") {
			t.Fatalf("落库失败却写出了归档产物: %s", name)
		}
	}
}

// ─── N6：expected_body_count==0 的天 → empty，不写指针、不置空（防 R14）────────

func TestN6_NoBodyDayMarkedEmptyWithoutPointerOrNullify(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	// 该天共 7 行，但没有任何行带正文：expected_total=7 / expected_body_count=0。
	exp := archiveNegScenario{total: 7, bodyCount: 0, noBodyRows: 7}
	db := scriptDB(exp)
	svc := newArchiveTestService(t, db, root)

	settled, err := svc.ArchiveDay(ctx, archiveTestDay, true)
	if err != nil {
		t.Fatalf("空天不得报错（应标记 empty 并正常收敛）: %v", err)
	}
	if !settled {
		t.Fatal("空天必须收敛为终态（empty）")
	}

	inserts := db.FindContains("INSERT INTO trace_archive_manifests")
	if len(inserts) == 0 {
		t.Fatal("空天必须落库 state=empty（供运维可观测）")
	}
	var sawEmpty bool
	for _, st := range inserts {
		for _, a := range st.Args {
			if s, ok := a.(string); ok && s == cpmodel.ArchiveStateEmpty {
				sawEmpty = true
			}
		}
	}
	if !sawEmpty {
		t.Fatalf("未落库 state=empty，args=%+v", inserts[0].Args)
	}
	// 期望集必须用【独立基准 1】的全量行数（7），而不是导出谓词口径（0）——防 BLK-02 分母同源。
	var sawTotal bool
	for _, a := range inserts[0].Args {
		if n, ok := a.(int64); ok && n == exp.total {
			sawTotal = true
		}
	}
	if !sawTotal {
		t.Fatalf("expected_total 未写入独立基准值 %d，args=%+v", exp.total, inserts[0].Args)
	}
	// 不写指针、不置空。
	if n := db.CountContains("archive_ref = "); n != 0 {
		t.Fatalf("空天却写了指针 %d 次（R14 回归）", n)
	}
	if n := db.CountContains("SET request_body = NULL"); n != 0 {
		t.Fatalf("空天却置空了正文 %d 次（R14 回归）", n)
	}
	// 不产生归档文件。
	for _, name := range dayFiles(t, root, archiveTestDay) {
		if name == archive.IndexFileName || name == archive.SuccessMarkerName || strings.HasSuffix(name, ".zst") {
			t.Fatalf("空天却产生了归档产物: %s", name)
		}
	}
}

// ─── N5：陈旧 _SUCCESS（mtime 早于导出开始）→ 校验③必须失败（防 B2 回归）───────

// staleSuccessStore 包装 LocalStore：模拟「导出前删除旧标记失败 / 标记被外部重新造出」，
// 使得陈旧 _SUCCESS 在本次导出期间依然存在（正是校验③要兜住的场景）。
type staleSuccessStore struct {
	*archive.LocalStore
}

func (s *staleSuccessStore) Delete(ctx context.Context, key string) error {
	if strings.Contains(key, archive.SuccessMarkerName) {
		return nil // 删除失败（静默）：旧标记仍在
	}
	return s.LocalStore.Delete(ctx, key)
}

func (s *staleSuccessStore) Put(ctx context.Context, key string, r io.Reader, size int64, opts archive.PutOptions) (archive.PutResult, error) {
	if strings.Contains(key, archive.SuccessMarkerName) {
		return archive.PutResult{}, nil // 写入失败（静默）：旧标记的 mtime 保持不变
	}
	return s.LocalStore.Put(ctx, key, r, size, opts)
}

// TestN5_StaleSuccessMarkerFailsVerifyStep3 断言：
//  1. 陈旧 _SUCCESS（mtime 早于导出开始）必须让四步校验③失败，本轮终止、不写指针、不置 verified；
//  2. 正向对照：同一脚本下健康 store 能走到 ⑨/⑩（证明失败确实来自③，而不是更早的门闸）。
func TestN5_StaleSuccessMarkerFailsVerifyStep3(t *testing.T) {
	ctx := context.Background()
	exp := archiveNegScenario{
		total:      2,
		bodyCount:  2,
		bodySHA:    frozenSHA("req_a", "req_b"),
		noBodyRows: 0,
		exportRows: [][]any{exportRow("req_a"), exportRow("req_b")},
	}

	// —— 正向对照：健康 store 应当成功（验证脚本、期望集与四步校验都能过）——
	{
		root := t.TempDir()
		db := scriptDB(exp)
		svc := newArchiveTestService(t, db, root)
		settled, err := svc.ArchiveDay(ctx, archiveTestDay, true)
		if err != nil || !settled {
			t.Fatalf("正向对照失败（脚本或实现有问题）: settled=%v err=%v", settled, err)
		}
		if db.CountContains("archive_ref = ") == 0 {
			t.Fatal("正向对照未写指针：说明用例根本没走到 ⑩")
		}
	}

	// —— 负向：预置陈旧 _SUCCESS，且本次导出无法删除/刷新它 ——
	root := t.TempDir()
	dayDir := filepath.Join(root, "dt="+archiveTestDay.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o750); err != nil {
		t.Fatalf("mkdir day dir: %v", err)
	}
	stalePath := filepath.Join(dayDir, archive.SuccessMarkerName)
	if err := os.WriteFile(stalePath, nil, 0o640); err != nil {
		t.Fatalf("write stale _SUCCESS: %v", err)
	}
	staleAt := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stalePath, staleAt, staleAt); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	db := scriptDB(exp)
	ls := archive.NewLocalStore(root, "")
	ls.SetZstdWindowBytes(8 << 20)
	svc := newArchiveTestService(t, db, root)
	svc.SetArchiveStore(&staleSuccessStore{LocalStore: ls})

	settled, err := svc.ArchiveDay(ctx, archiveTestDay, true)
	if err == nil {
		t.Fatal("陈旧 _SUCCESS 必须让校验③失败并终止本轮（B2 回归）")
	}
	if settled {
		t.Fatal("校验③失败的天不得报告终态")
	}
	if !strings.Contains(err.Error(), "verify") && !strings.Contains(err.Error(), "_SUCCESS") {
		t.Fatalf("错误应指向校验③/_SUCCESS，got %v", err)
	}
	// 关键数据安全断言：③ 失败 → 绝不写指针、绝不置 verified。
	if n := db.CountContains("archive_ref = "); n != 0 {
		t.Fatalf("校验③失败却写了指针 %d 次", n)
	}
	if n := db.CountContains("verified_at = NOW()"); n != 0 {
		t.Fatalf("校验③失败却执行了 markManifestVerified %d 次", n)
	}
}

// TestN5_VerifySuccessMarkerUnit 直接以 mtime 判定校验③（陈旧 → 报错；新鲜 → 通过）。
func TestN5_VerifySuccessMarkerUnit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := scriptDB(archiveNegScenario{})
	svc := newArchiveTestService(t, db, root)
	dir := "dt=2026-09-19"
	if _, err := svc.store.Put(ctx, dir+"/"+archive.SuccessMarkerName,
		strings.NewReader(""), 0, archive.PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put marker: %v", err)
	}
	startedAt := time.Now()
	if err := svc.verifySuccessMarker(ctx, dir, startedAt); err != nil {
		t.Fatalf("刚写入的 _SUCCESS 必须通过校验③: %v", err)
	}
	stale := time.Now().Add(-time.Hour)
	// 手工把标记做旧（直接改盘上文件 mtime）。
	path := filepath.Join(root, filepath.FromSlash(dir), archive.SuccessMarkerName)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := svc.verifySuccessMarker(ctx, dir, startedAt); err == nil {
		t.Fatal("mtime 早于导出开始的 _SUCCESS 必须让校验③失败")
	}
	// 标记缺失同样必须失败（不允许「没有证据也算过」）。
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	if err := svc.verifySuccessMarker(ctx, dir, startedAt); err == nil {
		t.Fatal("_SUCCESS 缺失必须让校验③失败")
	}
}

// ─── N7：抽样回读失败 → janitor 跳过该 dt 并告警（防 R4 不可逆丢失）────────────

// writeJanitorTestDay 在 root 下写出一个「已验证」的天（分块 + index + _SUCCESS）。
// corrupt=true 时把 part 覆盖为坏字节，模拟归档文件损坏。
func writeJanitorTestDay(t *testing.T, root string, day time.Time, corrupt bool) []string {
	t.Helper()
	ctx := context.Background()
	var rids []string
	store := archive.NewLocalStore(root, "")
	store.SetZstdWindowBytes(8 << 20)

	dir := "dt=" + day.Format("2006-01-02")
	w, err := archive.NewPartWriter(ctx, store, dir, archive.PartWriterOptions{ChunkBytes: 1 << 20})
	if err != nil {
		t.Fatalf("NewPartWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		rid := fmt.Sprintf("req_%03d", i)
		rids = append(rids, rid)
		line := fmt.Sprintf(
			`{"request_id":%q,"ts":"2026-09-19T03:00:00Z","request_headers":{},"request_body":{"q":%q},"response_body":{"a":%q},"error_code":"","error_message":""}`,
			rid, rid, rid)
		if err := w.WriteLine([]byte(line)); err != nil {
			t.Fatalf("WriteLine: %v", err)
		}
	}
	if _, err := w.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.Put(ctx, dir+"/"+archive.SuccessMarkerName, strings.NewReader(""), 0,
		archive.PutOptions{Overwrite: true}); err != nil {
		t.Fatalf("put _SUCCESS: %v", err)
	}
	if corrupt {
		part := filepath.Join(root, filepath.FromSlash(dir), "part-00000.jsonl.zst")
		if err := os.WriteFile(part, []byte("corrupted-not-a-zstd-frame"), 0o640); err != nil {
			t.Fatalf("corrupt part: %v", err)
		}
	}
	return rids
}

// janitorTestFixture 准备 janitor 负向测试：真实 LocalStore + 假 DB 门闸数据。
type janitorTestFixture struct {
	janitor *ArchiveJanitor
	db      *fakedb.DB
	root    string
	rids    []string
}

func newJanitorTestFixture(t *testing.T, corrupt bool) janitorTestFixture {
	t.Helper()
	root := t.TempDir()
	rids := writeJanitorTestDay(t, root, archiveTestDay, corrupt)

	now := time.Now()
	cfg := archiveTestConfig(root)
	cfg.WriteEnabled = true
	cfg.HotWindowDays = 3
	cfg.RetentionDays = 0 // 关闭保留期删除，隔离本用例关注点
	m := &manifestFixture{
		dt:            archiveTestDay,
		state:         cpmodel.ArchiveStateVerified,
		verifyState:   cpmodel.ArchiveVerifyOK,
		ridsSHA:       strings.Repeat("c", 64),
		createdAt:     now.Add(-3 * time.Hour),
		verifiedAt:    now.Add(-2 * time.Hour),
		lastInspected: now.Add(-30 * time.Minute),
	}
	db := scriptDB(archiveNegScenario{
		manifest:   m,
		sampleIDs:  []string{rids[0], rids[4]},
		nullifyIDs: []string{rids[0]},
	})
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	archiver := NewTraceArchiveService(gdb, nil, cfg)
	jan := NewTraceArchiveJanitor(gdb, nil, archiver, cfg)
	if jan.store == nil {
		t.Fatalf("janitor store 未构造（root=%s）", root)
	}
	return janitorTestFixture{janitor: jan, db: db, root: root, rids: rids}
}

// TestN7_SampleReadBackFailureSkipsDay 断言：归档损坏导致抽样回读失败时，
// janitor 必须跳过该 dt（不置空正文），并留下告警审计。
//
// 正向对照（同一门闸数据 + 健康归档）证明 7 项门闸中只有④会失败。
func TestN7_SampleReadBackFailureSkipsDay(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	cutoff := now.Add(-72 * time.Hour).Format("2006-01-02")

	// —— 正向对照：健康归档 → 门闸全过 → 真的置空 ——
	{
		f := newJanitorTestFixture(t, false)
		cleaned, err := f.janitor.nullifyVerifiedDays(ctx, cutoff, now)
		if err != nil {
			t.Fatalf("正向对照 nullifyVerifiedDays: %v", err)
		}
		if n := f.db.CountContains("SET request_body = NULL"); n == 0 {
			t.Fatalf("正向对照未置空：门闸数据或实现有问题（cleaned=%v）", cleaned)
		}
	}

	// —— 负向：归档损坏 → ④ 必须拦下 ——
	f := newJanitorTestFixture(t, true)
	cleaned, err := f.janitor.nullifyVerifiedDays(ctx, cutoff, now)
	if err != nil {
		t.Fatalf("nullifyVerifiedDays 不应把门闸跳过变成致命错误: %v", err)
	}
	if n := f.db.CountContains("SET request_body = NULL"); n != 0 {
		t.Fatalf("抽样回读失败却置空了正文 %d 次（R4 回归：不可逆丢失）", n)
	}
	if cleaned["2026-09-19"] {
		t.Fatal("抽样回读失败的天不得计入 cleaned")
	}
	// 且必须「并告警」：落审计留痕（不得静默跳过）。
	auditPath := filepath.Join(f.root, janitorAuditDirName, janitorAuditSkipFile)
	b, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("抽样回读失败必须留审计告警 %s: %v", auditPath, err)
	}
	if !strings.Contains(string(b), "2026-09-19") || !strings.Contains(string(b), "④") {
		t.Fatalf("审计内容应含 dt 与门闸编号④: %s", b)
	}
}

// TestN7_CheckNullifyGatesGateFour 直接把门闸判定收敛到 ④：
// 门闸①-③⑤⑥⑦均可满足，唯独归档损坏使④失败。
func TestN7_CheckNullifyGatesGateFour(t *testing.T) {
	ctx := context.Background()
	f := newJanitorTestFixture(t, true)
	m := &cpmodel.TraceArchiveManifest{DT: archiveTestDay, State: cpmodel.ArchiveStateVerified}
	now := time.Now()
	created := now.Add(-3 * time.Hour)
	verified := now.Add(-2 * time.Hour)
	inspected := now.Add(-30 * time.Minute)
	sha := strings.Repeat("c", 64)
	m.CreatedAt = created
	m.VerifiedAt = &verified
	m.LastInspectedAt = &inspected
	m.ExpectedBodyRIDsSHA256 = &sha
	m.VerifyState = cpmodel.ArchiveVerifyOK

	g := f.janitor.checkNullifyGates(ctx, m, now)
	if g.OK {
		t.Fatal("归档损坏时门闸不得通过")
	}
	if g.Gate != "④" {
		t.Fatalf("应由门闸④（抽样回读）拦下，实际 gate=%s reason=%s", g.Gate, g.Reason)
	}
	if g.Reason == "" {
		t.Fatal("门闸失败必须给出原因（告警/审计用）")
	}
}

// TestN7_GatesPassOnHealthyArchive 门闸在健康归档下必须全过（防止 N7 的正向对照被门闸误拦而失效）。
func TestN7_GatesPassOnHealthyArchive(t *testing.T) {
	ctx := context.Background()
	f := newJanitorTestFixture(t, false)
	m := &cpmodel.TraceArchiveManifest{DT: archiveTestDay, State: cpmodel.ArchiveStateVerified}
	now := time.Now()
	created := now.Add(-3 * time.Hour)
	verified := now.Add(-2 * time.Hour)
	inspected := now.Add(-30 * time.Minute)
	sha := strings.Repeat("c", 64)
	m.CreatedAt = created
	m.VerifiedAt = &verified
	m.LastInspectedAt = &inspected
	m.ExpectedBodyRIDsSHA256 = &sha
	m.VerifyState = cpmodel.ArchiveVerifyOK

	if g := f.janitor.checkNullifyGates(ctx, m, now); !g.OK {
		t.Fatalf("健康归档的门闸应全部通过，gate=%s reason=%s", g.Gate, g.Reason)
	}
}

// 便于断言 nullify 语句的参数确实指向当天（而非别的天）。
func TestN7_NullifyStatementTargetsDay(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	f := newJanitorTestFixture(t, false)
	if _, err := f.janitor.nullifyVerifiedDays(ctx, now.Add(-72*time.Hour).Format("2006-01-02"), now); err != nil {
		t.Fatalf("nullifyVerifiedDays: %v", err)
	}
	stmts := f.db.FindContains("SET request_body = NULL")
	if len(stmts) == 0 {
		t.Fatal("未置空")
	}
	// 参数中必须出现当天抽样的 request_id（幂等 keyset 置空按 rid 精确点名）。
	var found bool
	for _, a := range stmts[0].Args {
		if s, ok := a.(string); ok && strings.HasPrefix(s, "req_") {
			found = true
		}
	}
	if !found {
		t.Fatalf("置空语句参数异常: %+v", stmts[0].Args)
	}
}
