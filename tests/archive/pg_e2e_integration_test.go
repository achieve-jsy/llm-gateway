//go:build integration

// pg_e2e_integration_test.go —— 真实 PostgreSQL + 真实 Redis 的归档端到端验收。
//
// 为什么需要这个文件：本特性此前的全部测试都用脚本化假 DB 驱动，覆盖不到
// 「Go 代码 ↔ 真实 PG」这一层——GORM 参数绑定、`AT TIME ZONE ?` 传参、
// `COLLATE "C"` 游标、keyset 分页循环、事务与 jsonb 往返。这些恰恰是历史缺陷
// （BLK-01/02、F1）最容易藏身的接缝。
//
// 本文件的核心断言是【唯一安全不变量】的端到端编码：
//
//	凡 archive_status = 2（正文已从 PG 离场）的行，其正文必须能从归档回读出来。
//
// 这一条同时覆盖 F1（盖戳集合 ⊋ 归档集合）与 F2（盘上索引不可读却被判 ok）：
// 任何"置空了但读不回来"的组合都会在此失败。
//
// 前置：
//
//	ARCHIVE_TEST_DSN            必填，指向一个【专用 scratch 库】（测试会 TRUNCATE 表）
//	ARCHIVE_TEST_REDIS_ADDR     必填，janitor 的 leader 锁需要真实 Redis
//	ARCHIVE_TEST_REDIS_PASSWORD 可选
//	ARCHIVE_TEST_REDIS_DB       可选，默认 15（与共享测试环境隔离；测试会 FlushDB）
//	ARCHIVE_TEST_ROOT           可选，归档落盘根目录；默认 t.TempDir()。
//	                            注意：janitor 门闸⑦ 检查磁盘水位，指向写满的分区会导致置空被跳过。
//
// 建表：本测试【不】建表，要求 call_traces 与 trace_archive_manifests 已存在
// （即迁移已应用）。缺失时以明确信息失败——这本身就是对迁移的验收。
//
// 运行：go test ./tests/archive/... -tags=integration -count=1 -run RealPostgres -v
package archive_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	archivepkg "github.com/example/llm-gateway/internal/archive"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/observability/service"
)

const e2eTZ = "Asia/Shanghai"

// e2eEnv 收集并校验集成测试所需的环境。
type e2eEnv struct {
	db     *gorm.DB
	rdb    *redis.Client
	root   string
	loc    *time.Location
	day    time.Time // 被测天（Asia/Shanghai 的 00:00:00）
	cfg    cpconfig.ArchiveConfig
	arch   *service.ArchiveService
	jan    *service.ArchiveJanitor
	reader *archivepkg.LocalStore
	ctx    context.Context
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()
	dsn := os.Getenv("ARCHIVE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 ARCHIVE_TEST_DSN，跳过真实 PG 端到端")
	}
	rdbAddr := os.Getenv("ARCHIVE_TEST_REDIS_ADDR")
	if rdbAddr == "" {
		t.Skip("未设置 ARCHIVE_TEST_REDIS_ADDR（janitor leader 锁需要真实 Redis）")
	}

	loc, err := time.LoadLocation(e2eTZ)
	if err != nil {
		t.Fatalf("加载时区 %s: %v", e2eTZ, err)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 PostgreSQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取 sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	// 建表前置：缺失即迁移未应用，直接失败（这本身就是对迁移的验收）。
	for _, tbl := range []string{"call_traces", "trace_archive_manifests"} {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM information_schema.tables
			WHERE table_schema='public' AND table_name = ?`, tbl).Scan(&n).Error; err != nil {
			t.Fatalf("探测表 %s: %v", tbl, err)
		}
		if n == 0 {
			t.Fatalf("表 %s 不存在——请先应用迁移 atlas-migrations/*_add_trace_archive.sql", tbl)
		}
	}
	var cols int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_name='call_traces' AND column_name IN
		('task_id','documents_count','archive_ref','archive_status','archived_at')`).Scan(&cols).Error; err != nil {
		t.Fatalf("探测归档列: %v", err)
	}
	if cols != 5 {
		t.Fatalf("call_traces 归档列只找到 %d/5——迁移未应用完整", cols)
	}

	// Redis：用独立 DB，避免污染共享测试环境的 DB 0。
	rdbDB, _ := strconv.Atoi(envOr("ARCHIVE_TEST_REDIS_DB", "15"))
	rdb := redis.NewClient(&redis.Options{
		Addr:     rdbAddr,
		Password: os.Getenv("ARCHIVE_TEST_REDIS_PASSWORD"),
		DB:       rdbDB,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis 不可用（%s db=%d）: %v", rdbAddr, rdbDB, err)
	}
	// 清掉可能残留的 leader 锁，保证本轮能真正进入 janitor cycle。
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("清空测试 Redis DB %d: %v", rdbDB, err)
	}

	root := os.Getenv("ARCHIVE_TEST_ROOT")
	if root == "" {
		root = t.TempDir()
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建归档根目录 %s: %v", root, err)
	}

	// 被测天：3 天前（保证整日早于 janitor 的 cutoff），避免硬编码日期随时间失效。
	now := time.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -3)

	cfg := cpconfig.ArchiveConfig{
		WriteEnabled:          true,
		ReadEnabled:           true,
		Root:                  root,
		TZ:                    e2eTZ,
		HotWindowDays:         2,
		DaysPerNight:          5,
		ClosureLagHours:       0,
		ChunkMB:               1,  // 故意调小：让几百行正文切出多个块，逼出分块/索引/二分路径
		ZstdLevel:             "best",
		ZstdWindowMB:          8,  // 调小以加快测试；写入与读取两侧必须一致
		EncoderConcurrency:    1,
		MaxConcurrentReads:    4,
		BatchRows:             100, // 调小：逼出 keyset 分页与盖戳分批路径
		MaxRetry:              3,
		VerifySampleRows:      20,
		InspectFreshnessHours: 168,
		InspectPerCycle:       5,
		RequireOffhostCopy:    false, // 门闸⑥ 是文件系统语义，由单测覆盖；此处聚焦 SQL 门闸
		DiskWatermarkPercent:  99,    // 避免宿主分区占用偏高时门闸⑦ 误拦
		MaxDiskGB:             0,
		RetentionDays:         365, // 测试期内不允许保留期删除，保证置空后仍能点读
		StagingRetentionHours: 336,
	}

	arch := service.NewTraceArchiveService(db, rdb, cfg)
	return &e2eEnv{
		db:     db,
		rdb:    rdb,
		root:   root,
		loc:    loc,
		day:    day,
		cfg:    cfg,
		arch:   arch,
		jan:    service.NewTraceArchiveJanitor(db, rdb, arch, cfg),
		reader: archivepkg.NewLocalStore(root, ""),
		ctx:    ctx,
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (e *e2eEnv) dayDir(d time.Time) string {
	return filepath.Join(e.root, "dt="+d.Format("2006-01-02"))
}

// resetTables 清空两张表，让每个用例从确定状态开始。
func (e *e2eEnv) resetTables(t *testing.T) {
	t.Helper()
	if err := e.db.Exec("TRUNCATE call_traces").Error; err != nil {
		t.Fatalf("TRUNCATE call_traces: %v", err)
	}
	if err := e.db.Exec("DELETE FROM trace_archive_manifests").Error; err != nil {
		t.Fatalf("清空 trace_archive_manifests: %v", err)
	}
}

// bodyPayload 造一段可辨识的正文（内容高度重复，压缩率不具代表性，仅用于功能验收）。
func bodyPayload(kind string, idx, kb int) []byte {
	var b strings.Builder
	b.WriteString(`{"kind":"` + kind + `","idx":` + strconv.Itoa(idx) + `,"items":[`)
	for i := 0; i < kb*8; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"i":%d,"t":"segment-%d-%s"}`, i, i, strings.Repeat("x", 24))
	}
	b.WriteString("]}")
	return []byte(b.String())
}

// seedDay 在指定天插入 bodyRows 行带正文 + noBodyRows 行无正文，返回 body 的 request_id。
func (e *e2eEnv) seedDay(t *testing.T, bodyRows, noBodyRows int) []string {
	t.Helper()
	base := e.day.Add(10 * time.Hour) // 当天 10:00，远离边界
	rows := make([]obsmodel.CallTrace, 0, bodyRows+noBodyRows)
	rids := make([]string, 0, bodyRows)

	for i := 0; i < bodyRows; i++ {
		// 前 10 行用大写前缀 + 连字符，专门逼出 COLLATE "C" 与默认 collation 的分歧面。
		var rid string
		if i < 10 {
			rid = fmt.Sprintf("REQ-%03d-A", i)
		} else {
			rid = fmt.Sprintf("req-%04d", i)
		}
		kb := 32
		if i%40 == 0 {
			kb = 256 // 少量大正文，制造"单行远大于 1 个块"的边界
		}
		rows = append(rows, obsmodel.CallTrace{
			RequestID:      rid,
			TS:             base.Add(time.Duration(i) * time.Second),
			RequestHeaders: datatypes.JSON(`{"h":"v"}`),
			RequestBody:    datatypes.JSON(bodyPayload("req", i, kb)),
			ResponseBody:   datatypes.JSON(bodyPayload("resp", i, 4)),
		})
		rids = append(rids, rid)
	}
	for i := 0; i < noBodyRows; i++ {
		rows = append(rows, obsmodel.CallTrace{
			RequestID: fmt.Sprintf("nobody-%04d", i),
			TS:        base.Add(time.Duration(bodyRows+i) * time.Second),
		})
	}
	if err := e.db.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatalf("插入测试数据: %v", err)
	}
	sort.Strings(rids)
	return rids
}

// nullifiedRow 描述一个已被置空的行。
type nullifiedRow struct {
	RequestID   string `gorm:"column:request_id"`
	ArchiveRef  string `gorm:"column:archive_ref"`
	HasReqBody  bool   `gorm:"column:has_req_body"`
	HasRespBody bool   `gorm:"column:has_resp_body"`
}

// assertNullifiedAreRecoverable 断言【唯一安全不变量】：
// 凡 archive_status=2 的行，其正文必须能从归档回读出来。
//
// 这是 F1（盖戳集合 ⊋ 归档集合）与 F2（索引损坏却判 ok）的端到端探测器：
// 任何「正文已离场但归档里没有」的行都会在这里失败。
// 返回被校验的行数。
func (e *e2eEnv) assertNullifiedAreRecoverable(t *testing.T) int {
	t.Helper()
	var rows []nullifiedRow
	if err := e.db.Raw(`SELECT request_id, coalesce(archive_ref,'') AS archive_ref,
		(request_body IS NOT NULL) AS has_req_body,
		(response_body IS NOT NULL) AS has_resp_body
		FROM call_traces WHERE archive_status = 2 ORDER BY request_id`).Scan(&rows).Error; err != nil {
		t.Fatalf("查询已置空行: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("没有任何已置空行——测试场景未生效")
	}
	bad := 0
	for _, r := range rows {
		if r.ArchiveRef == "" {
			t.Errorf("已置空行 %s 的 archive_ref 为空——无归档指向，正文已不可恢复", r.RequestID)
			bad++
			continue
		}
		if r.HasReqBody || r.HasRespBody {
			t.Errorf("已置空行 %s 在 PG 里仍有正文，置空不彻底", r.RequestID)
			bad++
			continue
		}
		obj, err := e.reader.ReadBody(e.ctx, r.ArchiveRef, r.RequestID)
		if err != nil {
			t.Errorf("【不可逆丢失】已置空行 %s 无法从归档 %s 回读: %v", r.RequestID, r.ArchiveRef, err)
			bad++
			continue
		}
		if len(obj.RequestBody) == 0 && len(obj.ResponseBody) == 0 {
			t.Errorf("【不可逆丢失】已置空行 %s 回读到的正文为空", r.RequestID)
			bad++
		}
	}
	if bad > 0 {
		t.Fatalf("置空⟹可回读 不变量被破坏：%d/%d 行不可恢复", bad, len(rows))
	}
	return len(rows)
}

// TestArchiveEndToEndRealPostgres 跑通完整链条：
// 冻结 → 导出（多块 zstd）→ 四步校验 → 置 verified → 写指针 → 巡检 → 置空 → 点读回读。
func TestArchiveEndToEndRealPostgres(t *testing.T) {
	e := setupE2E(t)
	e.resetTables(t)
	const bodyRows, noBodyRows = 240, 60
	rids := e.seedDay(t, bodyRows, noBodyRows)

	// 天边界对侧：前一天 23:00 与次日 01:00 各一行，用于验证分天谓词。
	// 注意：janitor 的补归档阶段会把这些"已关闭天的泄漏行"各自归档到它们自己的 dt，
	// 这是设计行为，因此不能断言它们保持 archive_status=0；正确断言是"落到各自的 dt"。
	prevDay := e.day.AddDate(0, 0, -1)
	nextDay := e.day.AddDate(0, 0, 1)
	outside := []obsmodel.CallTrace{
		{RequestID: "outside-prev", TS: e.day.Add(-time.Hour), RequestBody: datatypes.JSON(`{"p":"prev"}`)},
		{RequestID: "outside-next", TS: e.day.AddDate(0, 0, 1).Add(time.Hour), RequestBody: datatypes.JSON(`{"p":"next"}`)},
	}
	if err := e.db.Create(&outside).Error; err != nil {
		t.Fatalf("插入天外数据: %v", err)
	}

	// —— 阶段 1：归档该天 ——
	worked, err := e.arch.ArchiveDay(e.ctx, e.day, false)
	if err != nil {
		t.Fatalf("ArchiveDay 失败: %v", err)
	}
	if !worked {
		t.Fatalf("ArchiveDay worked=false，说明该天未被实际归档")
	}

	dir := e.dayDir(e.day)
	for _, f := range []string{"_SUCCESS", "index.jsonl", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("归档产物缺失 %s: %v", f, err)
		}
	}
	parts, _ := filepath.Glob(filepath.Join(dir, "part-*.jsonl.zst"))
	if len(parts) < 2 {
		t.Errorf("期望多块归档（chunk_mb=1），实际只有 %d 个 part", len(parts))
	}
	if spool, _ := filepath.Glob(filepath.Join(dir, ".spool", "*")); len(spool) > 0 {
		t.Errorf("暂存目录未清空: %v", spool)
	}

	// 独立冻结期望集必须正确（real PG 上的两条基准 SQL）
	var m cpmodel.TraceArchiveManifest
	if err := e.db.Where("dt = ?", e.day.Format("2006-01-02")).First(&m).Error; err != nil {
		t.Fatalf("读取 manifest: %v", err)
	}
	if m.ExpectedTotal != bodyRows+noBodyRows {
		t.Errorf("expected_total=%d，期望 %d（全量行数，不含 body 谓词）", m.ExpectedTotal, bodyRows+noBodyRows)
	}
	if m.ExpectedBodyCount != bodyRows {
		t.Errorf("expected_body_count=%d，期望 %d", m.ExpectedBodyCount, bodyRows)
	}
	if m.ExpectedBodyRIDsSHA256 == nil || *m.ExpectedBodyRIDsSHA256 == "" {
		t.Error("expected_body_rids_sha256 为空——独立基准未落库")
	}
	if m.State != cpmodel.ArchiveStateVerified {
		t.Errorf("归档后 state=%q，期望 verified", m.State)
	}

	// 指针阶段：有正文行被盖戳，无正文行【绝不】被盖戳（F1 的直接体现）
	var stamped, stampedNoBody int64
	e.db.Raw(`SELECT count(*) FROM call_traces WHERE ts >= ? AND ts < ? AND archive_status=1
		AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		e.day, e.day.AddDate(0, 0, 1)).Scan(&stamped)
	e.db.Raw(`SELECT count(*) FROM call_traces WHERE ts >= ? AND ts < ? AND archive_status<>0
		AND request_body IS NULL AND response_body IS NULL`,
		e.day, e.day.AddDate(0, 0, 1)).Scan(&stampedNoBody)
	if stamped != bodyRows {
		t.Errorf("已盖戳且有正文的行数=%d，期望 %d", stamped, bodyRows)
	}
	if stampedNoBody != 0 {
		t.Errorf("有 %d 行无正文却被盖戳——F1 回归", stampedNoBody)
	}

	// index.jsonl 必须是 C 序、块间不重叠（点读二分的前提）
	idxRaw, err := os.ReadFile(filepath.Join(dir, "index.jsonl"))
	if err != nil {
		t.Fatalf("读 index.jsonl: %v", err)
	}
	var prevLast string
	for _, line := range bytes.Split(bytes.TrimSpace(idxRaw), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry struct {
			Part     string `json:"part"`
			FirstRID string `json:"first_rid"`
			LastRID  string `json:"last_rid"`
			Rows     int64  `json:"rows"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("解析 index 行: %v", err)
		}
		if entry.Rows <= 0 {
			t.Errorf("index 条目 %s rows=%d", entry.Part, entry.Rows)
		}
		if entry.FirstRID > entry.LastRID {
			t.Errorf("index 条目 %s first_rid(%s) > last_rid(%s)", entry.Part, entry.FirstRID, entry.LastRID)
		}
		if prevLast != "" && prevLast >= entry.FirstRID {
			t.Errorf("块间顺序违约: 前块 last_rid(%s) >= 本块 first_rid(%s)", prevLast, entry.FirstRID)
		}
		prevLast = entry.LastRID
	}

	// —— 阶段 2：janitor 全周期（含巡检与置空）——
	if err := e.jan.RunCycle(e.ctx); err != nil {
		t.Fatalf("janitor RunCycle 失败: %v", err)
	}

	// 置空后：本天有正文行 status=2；无正文行保持 status=0 且正文为空
	var nulled, stillHot int64
	e.db.Raw(`SELECT count(*) FROM call_traces WHERE ts >= ? AND ts < ? AND archive_status=2`,
		e.day, e.day.AddDate(0, 0, 1)).Scan(&nulled)
	e.db.Raw(`SELECT count(*) FROM call_traces WHERE ts >= ? AND ts < ? AND archive_status=0
		AND request_body IS NULL AND response_body IS NULL`,
		e.day, e.day.AddDate(0, 0, 1)).Scan(&stillHot)
	if nulled != bodyRows {
		t.Errorf("本天已置空行数=%d，期望 %d", nulled, bodyRows)
	}
	if stillHot != noBodyRows {
		t.Errorf("无正文且未盖戳行数=%d，期望 %d", stillHot, noBodyRows)
	}
	if err := e.db.Where("dt = ?", e.day.Format("2006-01-02")).First(&m).Error; err != nil {
		t.Fatalf("重读 manifest: %v", err)
	}
	if m.State != cpmodel.ArchiveStateCleaned {
		t.Errorf("置空后 state=%q，期望 cleaned", m.State)
	}
	if m.VerifyState != cpmodel.ArchiveVerifyOK {
		t.Errorf("巡检后 verify_state=%q，期望 ok（inspect_error=%v）", m.VerifyState, m.InspectError)
	}

	// —— 天边界：相邻天的行必须落到各自的 dt，不能混入本天 ——
	for _, tc := range []struct {
		rid string
		dt  time.Time
	}{
		{"outside-prev", prevDay},
		{"outside-next", nextDay},
	} {
		var ref string
		e.db.Raw(`SELECT coalesce(archive_ref,'') FROM call_traces WHERE request_id=?`, tc.rid).Scan(&ref)
		if ref == "" {
			t.Errorf("%s 未被归档（archive_ref 为空）", tc.rid)
			continue
		}
		if want := "dt=" + tc.dt.Format("2006-01-02"); !strings.Contains(ref, want) {
			t.Errorf("%s 的 archive_ref=%q，期望指向 %s —— 分天谓词错误", tc.rid, ref, want)
		}
	}
	// 本天归档指针下绝不能含本天之外的行
	var crossed int64
	e.db.Raw(`SELECT count(*) FROM call_traces WHERE archive_ref = ? AND ts < ?`,
		"dt="+e.day.Format("2006-01-02")+"/", e.day).Scan(&crossed)
	if crossed != 0 {
		t.Errorf("本天归档指针下混入了 %d 行更早时间的数据", crossed)
	}

	// —— 阶段 3：唯一安全不变量 ——
	verified := e.assertNullifiedAreRecoverable(t)
	t.Logf("已校验 %d 个置空行全部可从归档回读", verified)

	// —— 阶段 4：点读细节（C 序首/中/尾覆盖索引二分三个区间）——
	var ref string
	e.db.Raw(`SELECT archive_ref FROM call_traces WHERE request_id=?`, rids[len(rids)-1]).Scan(&ref)
	if ref == "" {
		t.Fatalf("request_id=%s 的 archive_ref 为空", rids[len(rids)-1])
	}
	sample := []string{rids[0], rids[len(rids)/2], rids[len(rids)-1]}
	for _, rid := range sample {
		obj, err := e.reader.ReadBody(e.ctx, ref, rid)
		if err != nil {
			t.Errorf("点读 %s 失败: %v", rid, err)
			continue
		}
		var got struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(obj.RequestBody, &got); err != nil {
			t.Errorf("点读 %s 的 JSON 非法: %v", rid, err)
			continue
		}
		if got.Kind != "req" {
			t.Errorf("点读 %s 内容不符: kind=%q（疑似定位到别的行）", rid, got.Kind)
		}
	}
	// 不存在的 rid 必须明确报错，而不是静默返回空对象
	if _, err := e.reader.ReadBody(e.ctx, ref, "definitely-not-here"); err == nil {
		t.Error("点读不存在的 request_id 应当报错，实际返回 nil error")
	}
	// _SUCCESS 缺失时必须判为"该天未归档"，而不是返回空正文
	succ := filepath.Join(dir, "_SUCCESS")
	bak := succ + ".bak"
	if err := os.Rename(succ, bak); err != nil {
		t.Fatalf("移走 _SUCCESS: %v", err)
	}
	if _, err := e.reader.ReadBody(e.ctx, ref, sample[0]); err == nil {
		t.Error("_SUCCESS 缺失时点读仍成功——会把「未归档」误呈现为「数据不存在」")
	}
	if err := os.Rename(bak, succ); err != nil {
		t.Fatalf("恢复 _SUCCESS: %v", err)
	}
}

// TestArchiveLateArrivalIsNotLost 验证 F1/F4 组合在真实 PG 上的行为：
// 归档完成之后才落入该天的新行，要么保持热存，要么被补归档后安全置空，
// 但【绝不能】出现「正文已离场且归档里没有」。安全不变量由回读断言兜底。
func TestArchiveLateArrivalIsNotLost(t *testing.T) {
	e := setupE2E(t)
	e.resetTables(t)
	const bodyRows = 60
	e.seedDay(t, bodyRows, 10)

	if _, err := e.arch.ArchiveDay(e.ctx, e.day, false); err != nil {
		t.Fatalf("ArchiveDay 失败: %v", err)
	}

	// 归档完成后往同一天插入一行新正文（模拟"导出读完 → 盖戳之间"的竞态胜者）。
	late := obsmodel.CallTrace{
		RequestID:   "late-arrival",
		TS:          e.day.Add(23*time.Hour + 30*time.Minute),
		RequestBody: datatypes.JSON(`{"late":true}`),
	}
	if err := e.db.Create(&late).Error; err != nil {
		t.Fatalf("插入 late 行: %v", err)
	}

	if err := e.jan.RunCycle(e.ctx); err != nil {
		t.Fatalf("janitor RunCycle 失败: %v", err)
	}

	// 核心断言：无论 late 行处于哪个状态，只要它已离场，就必须能回读。
	var status int16
	e.db.Raw(`SELECT archive_status FROM call_traces WHERE request_id='late-arrival'`).Row().Scan(&status)
	switch status {
	case 0:
		// 保持热存——最保守的结果，正文还在 PG
		var bodyLen int64
		e.db.Raw(`SELECT coalesce(length(request_body::text),0) FROM call_traces
			WHERE request_id='late-arrival'`).Row().Scan(&bodyLen)
		if bodyLen == 0 {
			t.Error("late-arrival 未归档却正文为空——数据丢失")
		}
		t.Log("late-arrival 保持热存（status=0），正文完整")
	case 1, 2:
		// 已被补归档——必须能从归档回读
		var ref string
		e.db.Raw(`SELECT coalesce(archive_ref,'') FROM call_traces WHERE request_id='late-arrival'`).Scan(&ref)
		if ref == "" {
			t.Fatalf("late-arrival archive_status=%d 但 archive_ref 为空——无归档指向", status)
		}
		obj, err := e.reader.ReadBody(e.ctx, ref, "late-arrival")
		if err != nil {
			t.Fatalf("【不可逆丢失】late-arrival 已离场（status=%d）却无法从归档回读: %v", status, err)
		}
		if len(obj.RequestBody) == 0 {
			t.Fatalf("【不可逆丢失】late-arrival 回读到的正文为空")
		}
		t.Logf("late-arrival 已被补归档（status=%d）且可从归档回读", status)
	default:
		t.Fatalf("late-arrival 的 archive_status=%d 非法", status)
	}

	// 全局安全不变量
	verified := e.assertNullifiedAreRecoverable(t)
	t.Logf("已校验 %d 个置空行全部可从归档回读", verified)
}

// TestArchiveUnreadableIndexFailsClosed 证明「置空⟹可回读」这条断言【有牙齿】：
// 索引被毁之后，点读必须失败而不是静默返回空，否则 assertNullifiedAreRecoverable
// 就只是走过场。这是 F2（索引不可读却被判 ok）的端到端探测器。
func TestArchiveUnreadableIndexFailsClosed(t *testing.T) {
	e := setupE2E(t)
	e.resetTables(t)
	e.seedDay(t, 40, 10)

	if _, err := e.arch.ArchiveDay(e.ctx, e.day, false); err != nil {
		t.Fatalf("ArchiveDay 失败: %v", err)
	}

	var ref string
	e.db.Raw(`SELECT coalesce(archive_ref,'') FROM call_traces WHERE request_id='req-0020'`).Scan(&ref)
	if ref == "" {
		t.Fatal("req-0020 的 archive_ref 为空")
	}

	// 基线：索引完好时必须能读到
	if _, err := e.reader.ReadBody(e.ctx, ref, "req-0020"); err != nil {
		t.Fatalf("索引完好时点读失败: %v", err)
	}

	// 破坏索引：点读必须失败（fail-closed），绝不能静默返回空正文。
	idx := filepath.Join(e.dayDir(e.day), "index.jsonl")
	if err := os.Rename(idx, idx+".gone"); err != nil {
		t.Fatalf("移走 index.jsonl: %v", err)
	}
	if _, err := e.reader.ReadBody(e.ctx, ref, "req-0020"); err == nil {
		t.Error("索引缺失时点读仍成功——会把「归档不可读」误呈现为「数据不存在」")
	}
	if err := os.Rename(idx+".gone", idx); err != nil {
		t.Fatalf("恢复 index.jsonl: %v", err)
	}

	// 破坏一个块文件：同理必须失败，而不是返回截断/错误内容。
	parts, _ := filepath.Glob(filepath.Join(e.dayDir(e.day), "part-*.jsonl.zst"))
	if len(parts) == 0 {
		t.Fatal("没有 part 文件")
	}
	if err := os.Rename(parts[0], parts[0]+".gone"); err != nil {
		t.Fatalf("移走 part: %v", err)
	}
	// 该块内的第一条 rid 现在必然读不到
	var firstRID string
	e.db.Raw(`SELECT min(request_id) FROM call_traces WHERE archive_ref = ?`,
		"dt="+e.day.Format("2006-01-02")+"/").Scan(&firstRID)
	if _, err := e.reader.ReadBody(e.ctx, ref, firstRID); err == nil {
		t.Errorf("块文件缺失时点读 %s 仍成功——内容完整性未兜底", firstRID)
	}
	if err := os.Rename(parts[0]+".gone", parts[0]); err != nil {
		t.Fatalf("恢复 part: %v", err)
	}

	// 还原后应恢复正常，证明前面的失败确实由我们制造的破坏引起
	if _, err := e.reader.ReadBody(e.ctx, ref, "req-0020"); err != nil {
		t.Errorf("还原后点读仍失败: %v", err)
	}
}
