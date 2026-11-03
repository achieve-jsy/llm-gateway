//go:build integration

// export_spill_test.go —— 生产量级的「导出不落临时文件」端到端测试。
//
// 【它守护什么，以及为什么值得存在】
//
// call_traces 上「带 ORDER BY 且对正文做投影」的查询已被实测证明能写满 /data：
// 在生产上以 `json_build_object(…)::text … ORDER BY request_id COLLATE "C" LIMIT 25000`
// 触发过 ENOSPC，PG 报：
//
//	ERROR: could not write to file "base/pgsql_tmp/...": No space left on device
//
// 对照实验（30000 行 × 226KB 正文，TOAST 7.0 GB，temp_file_limit=3GB 保险丝）定出了边界：
//
//	对正文做投影的查询（json_build_object::text）, LIMIT 25000  →  超过 3 GB（撞保险丝）
//	归档 fetchBatch 的原始列（无投影）,          LIMIT 25000  →  2.8 MB
//
// 也就是说：**当前实现是安全的**（只取原始列，排序中流动的是 TOAST 指针），
// 但这个安全性**没有任何测试守护** —— 一旦有人往 fetchBatch 的 SELECT 列表里加了
// 计算列或对正文的投影，就会一步退回 GB 级溢出，而且在只有 1087 行的验收环境上
// 根本测不出来（走不到并行计划，也堆不出足以溢出的正文量）。
//
// 所以本测试的价值是【守住这个不变量】：用生产量级的数据跑真实归档导出，
// 断言导出期间数据库写入的临时文件字节数保持在极小值。
//
// 【防空洞通过的硬闸】——「导出什么都没做」也会让 delta=0：
//   - 断言 TOAST 非空（正文必须真的走行外存储，见 seedDayBySQL 的说明）
//   - 断言 manifest.row_count == 种子行数（导出必须真的覆盖了全部行）
//
// 环境变量（除必填的两个外）：
//
//	ARCHIVE_TEST_VOLUME_ROWS  种子行数，默认 30000
//	ARCHIVE_TEST_VOLUME_KB    每行请求正文的 KB 数，默认 32
//
// 用 SQL 侧 generate_series 生成，不在客户端构造，避免 GB 级内存占用。
package archive_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// 临时文件字节数上限：修复生效时导出几乎不产生临时文件（Sort 只持有 TOAST 指针）。
// 给足余量以吸收 statistics 噪声，但仍远低于「物化正文」时的 GB 量级。
const exportTempBytesLimit = 32 << 20 // 32 MiB

func volumeEnv(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// seedDayBySQL 用 SQL 侧生成指定行数与正文大小的数据（不在客户端构造）。
//
// 【必须 SET STORAGE EXTERNAL】否则本用例会空洞通过：
// 若正文用 repeat('x', N) 这类极端可压缩的内容，pglz 会把它压到几字节并【内联进 heap】，
// 正文根本不进 TOAST —— 于是既没有 detoast 成本，也没有 §10 的危害机制，
// 测试会在「什么都没验证」的情况下变绿（首次实现即踩到这个坑，toast=0 bytes）。
// 关闭该列的压缩后，大值必然走 TOAST，detoast 成本与生产一致。
// 内容用 md5 重复块即可：本用例断言的是【临时文件字节数】，
// 不依赖正文本身的压缩率（那由压缩比测试单独覆盖）。
func (e *e2eEnv) seedDayBySQL(t *testing.T, rows, bodyKB int) {
	t.Helper()
	for _, col := range []string{"request_body", "response_body"} {
		if err := e.db.Exec(
			"ALTER TABLE call_traces ALTER COLUMN " + col + " SET STORAGE EXTERNAL",
		).Error; err != nil {
			t.Fatalf("关闭 %s 压缩（确保走 TOAST）: %v", col, err)
		}
	}
	base := e.day.Add(10 * time.Hour) // 当天 10:00，远离边界
	err := e.db.Exec(`
		INSERT INTO call_traces (request_id, ts, request_headers, request_body, response_body)
		SELECT
			'vol-' || lpad(g::text, 7, '0'),
			?::timestamptz + (g || ' milliseconds')::interval,
			'{"h":"v"}'::jsonb,
			('{"kind":"req","idx":' || g || ',"blob":"' || repeat(md5(g::text), ?) || '"}')::jsonb,
			('{"kind":"resp","idx":' || g || ',"blob":"' || repeat(md5((g + 7)::text), 128) || '"}')::jsonb
		FROM generate_series(1, ?) g`,
		base, bodyKB*1024/32, rows,
	).Error
	if err != nil {
		t.Fatalf("SQL 造数（%d 行 × %d KB）: %v", rows, bodyKB, err)
	}
	// 统计信息决定计划形状（并行与否），必须让 planner 看到真实基数。
	if err := e.db.Exec("ANALYZE call_traces").Error; err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}
}

// dbTempBytes 读取当前库累计写入的临时文件字节数。
func (e *e2eEnv) dbTempBytes(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := e.db.Raw(
		`SELECT temp_bytes FROM pg_stat_database WHERE datname = current_database()`,
	).Scan(&n).Error; err != nil {
		t.Fatalf("读取 temp_bytes: %v", err)
	}
	return n
}

// exportPlanIsParallel 诊断用：在不关并行的情况下，导出查询的计划是否含并行节点。
// 若为 false，说明本次种子量级不足以触发并行，测试对该量级不具备区分力（会显式记录）。
func (e *e2eEnv) exportPlanIsParallel(t *testing.T) bool {
	t.Helper()
	rows, err := e.db.Raw(`
		EXPLAIN SELECT ct.request_id, ct.ts, ct.request_headers, ct.request_body, ct.response_body
		  FROM call_traces ct
		 WHERE ct.ts >= ? AND ct.ts < ?
		   AND (ct.request_body IS NOT NULL OR ct.response_body IS NOT NULL)
		   AND ct.request_id COLLATE "C" > ''
		 ORDER BY ct.request_id COLLATE "C" LIMIT 1000`,
		e.day, e.day.Add(24*time.Hour),
	).Rows()
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("扫描 EXPLAIN 行: %v", err)
		}
		for _, marker := range []string{"Parallel", "Gather"} {
			if len(line) > 0 && containsFold(line, marker) {
				t.Logf("计划节点: %s", line)
				return true
			}
		}
	}
	return false
}

// containsFold 大小写不敏感的子串判断（避免为此引入 strings 之外的工具）。
func containsFold(s, sub string) bool {
	n, m := len(s), len(sub)
	if m == 0 || n < m {
		return false
	}
	for i := 0; i+m <= n; i++ {
		ok := true
		for j := 0; j < m; j++ {
			a, b := s[i+j], sub[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// TestArchiveExportDoesNotSpillTempFilesAtVolume 用生产量级数据验证导出不落临时文件。
func TestArchiveExportDoesNotSpillTempFilesAtVolume(t *testing.T) {
	e := setupE2E(t)
	e.resetTables(t)

	rows := volumeEnv("ARCHIVE_TEST_VOLUME_ROWS", 30000)
	bodyKB := volumeEnv("ARCHIVE_TEST_VOLUME_KB", 32)
	e.seedDayBySQL(t, rows, bodyKB)

	// 统计种子规模，便于在失败时判断量级是否足够。
	var seeded int64
	var toastBytes int64
	var heapSize, toastSize string
	if err := e.db.Raw(`SELECT count(*) FROM call_traces`).Scan(&seeded).Error; err != nil {
		t.Fatalf("统计种子行数: %v", err)
	}
	if err := e.db.Raw(`
		SELECT pg_relation_size(t.oid), pg_relation_size(t.reltoastrelid),
		       pg_size_pretty(pg_relation_size(t.oid)), pg_size_pretty(pg_relation_size(t.reltoastrelid))
		  FROM pg_class t
		 WHERE t.relname = 'call_traces' AND t.relnamespace = 'public'::regnamespace`).
		Row().Scan(new(int64), &toastBytes, &heapSize, &toastSize); err != nil {
		t.Fatalf("读取表大小: %v", err)
	}
	t.Logf("种子规模：%d 行，每行请求正文 %d KB；heap=%s toast=%s", seeded, bodyKB, heapSize, toastSize)

	// 【硬闸】正文必须真的落到 TOAST 里，否则本用例会空洞通过：
	// 若正文可被 pglz 压到几字节并内联进 heap，就没有 detoast 成本，
	// §10 的危害机制整个不存在（首次实现即踩到此坑：toast=0 bytes 而测试变绿）。
	if toastBytes == 0 {
		t.Fatalf("TOAST 为空（heap=%s）：正文没有走行外存储，本用例无法验证 §10 的 detoast 溢出机制。"+
			"请确认种子列已 SET STORAGE EXTERNAL 且正文确实超过 TOAST 阈值", heapSize)
	}

	// 诊断：不关并行时的计划形状。为 false 说明该量级不足以触发并行，
	// 本测试对这一量级就失去区分力——必须显式记录，不能让它悄悄通过。
	if !e.exportPlanIsParallel(t) {
		t.Logf("⚠️ 该种子量级下导出查询【未走并行计划】，本用例对此量级不具备区分力；" +
			"如需复现 §10 需提高 ARCHIVE_TEST_VOLUME_ROWS")
	}

	before := e.dbTempBytes(t)

	if _, err := e.arch.ArchiveDay(e.ctx, e.day, false); err != nil {
		t.Fatalf("ArchiveDay: %v", err)
	}

	after := e.dbTempBytes(t)
	delta := after - before
	t.Logf("导出期间 temp_bytes：%d → %d（Δ=%d 字节）", before, after, delta)

	// 先确认导出确实做了事：否则「什么都没做所以没有临时文件」会空洞通过。
	var manifestRows int64
	if err := e.db.Raw(
		`SELECT row_count FROM trace_archive_manifests WHERE dt = ?::date`,
		e.day.Format("2006-01-02"),
	).Scan(&manifestRows).Error; err != nil {
		t.Fatalf("读取 manifest row_count: %v", err)
	}
	if manifestRows != seeded {
		t.Fatalf("导出未覆盖全部种子行：manifest row_count=%d，种子=%d（本用例失去意义）",
			manifestRows, seeded)
	}

	if delta > exportTempBytesLimit {
		t.Fatalf("导出期间写入了 %d 字节临时文件（上限 %d）——"+
			"说明 Sort 把正文物化到了磁盘，§10 的关并行修复失效或未生效",
			delta, exportTempBytesLimit)
	}
	fmt.Fprintf(os.Stderr, "[volume] rows=%d bodyKB=%d tempDelta=%d manifestRows=%d\n",
		seeded, bodyKB, delta, manifestRows)
}
