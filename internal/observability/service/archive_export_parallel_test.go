// archive_export_parallel_test.go —— §10 修复的回归测试：导出读必须关闭并行。
//
// 背景（生产事故，见实现报告 §10）：整天批处理的查询计划是并行的
// （Gather Merge -> Sort -> Parallel Bitmap Heap Scan）。并行 worker 要把元组经共享内存
// 队列交回 leader，而跨进程传递必须先把 TOAST 值 detoast（TOAST 指针指向源进程的缓冲区，
// 在别的进程里无效）。于是 call_traces 的正文（生产实测约 226 KB/行）在排序之前就被展开，
// leader 的 top-N 排序再把这些展开后的元组物化到磁盘。
//
// 生产实测：只读测量查询以 LIMIT 25000 即写满了 /data 上的 base/pgsql_tmp，PG 报
//	ERROR: could not write to file "base/pgsql_tmp/...": No space left on device
// 配合生产默认的 temp_file_limit=-1（无上限），足以写满 /data 并危及 PostgreSQL。
//
// 本测试守护的重点是【每一批】都关，而不是只在开头关一次——keyset 分页下每天有数十批，
// 任何一批漏关都会重新引入溢出面。
package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	"github.com/example/llm-gateway/tests/archive/fakedb"
)

// exportQueryMarker 是 fetchBatch 导出查询的识别特征（与实现里的 FROM 子句一致）。
const exportQueryMarker = "FROM call_traces ct"

// parallelGUC 是被关闭的会话级 GUC 名。
const parallelGUC = "max_parallel_workers_per_gather"

func TestExportReadSessionDisablesParallelPerBatch(t *testing.T) {
	const batches = 3

	var exportQueries int
	db := fakedb.New(func(raw string, _ []any) (fakedb.Result, error) {
		if strings.Contains(raw, exportQueryMarker) {
			exportQueries++
			// 返回空结果集即可：本测试关心的是语句序列，不是导出行内容。
			return fakedb.Result{Columns: []string{
				"request_id", "ts", "request_headers",
				"request_body", "response_body", "error_code", "error_message",
			}}, nil
		}
		return fakedb.Result{}, fmt.Errorf("fakedb: 未脚本化的语句: %s", raw)
	})

	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	svc := NewTraceArchiveService(gdb, nil, cpconfig.ArchiveConfig{Root: t.TempDir()})

	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	for i := 0; i < batches; i++ {
		if _, err := svc.fetchBatch(context.Background(), start, end, ""); err != nil {
			t.Fatalf("第 %d 批 fetchBatch: %v", i+1, err)
		}
	}

	if exportQueries != batches {
		t.Fatalf("导出查询次数 = %d，期望 %d", exportQueries, batches)
	}

	// ① 每一批都要关并行：SET LOCAL 的次数必须与批次数相等。
	sets := db.FindContains(parallelGUC)
	if len(sets) != batches {
		t.Fatalf("SET LOCAL %s 出现 %d 次，期望每批一次共 %d 次——"+
			"漏关的那一批会把正文 detoast 进排序并物化到磁盘（§10）",
			parallelGUC, len(sets), batches)
	}

	// ② SET 必须是 SET LOCAL（而非会话级 SET，后者会污染连接池里的连接）。
	for _, st := range sets {
		if !strings.Contains(strings.ToUpper(st.SQL), "SET LOCAL") {
			t.Errorf("关并行必须用 SET LOCAL（限定事务内），实得: %s", st.SQL)
		}
	}

	// ③ 顺序必须严格交替：每一条导出查询之前都紧邻一条 SET LOCAL。
	//    （GORM 的 Transaction 会把两者放在同一事务，fake 只记录这两类语句。）
	var seq []string
	for _, st := range db.Statements() {
		switch {
		case strings.Contains(strings.ToUpper(st.SQL), "SET LOCAL"):
			seq = append(seq, "SET")
		case strings.Contains(st.SQL, exportQueryMarker):
			seq = append(seq, "SELECT")
		}
	}
	want := make([]string, 0, batches*2)
	for i := 0; i < batches; i++ {
		want = append(want, "SET", "SELECT")
	}
	if strings.Join(seq, ",") != strings.Join(want, ",") {
		t.Fatalf("语句序列 = [%s]，期望 [%s]——"+
			"SET LOCAL 必须与导出查询交替出现且在其之前", strings.Join(seq, ","), strings.Join(want, ","))
	}
}

// TestExportReadSessionUsesLocalScopeNotSessionScope 单独守护「用 SET LOCAL 而非 SET」。
//
// 会话级 SET 会在连接归还连接池后继续生效，静默影响该连接上后续无关的查询；
// SET LOCAL 限定在事务内，事务一结束即失效。
func TestExportReadSessionUsesLocalScopeNotSessionScope(t *testing.T) {
	db := fakedb.New(func(raw string, _ []any) (fakedb.Result, error) {
		if strings.Contains(raw, exportQueryMarker) {
			return fakedb.Result{Columns: []string{"request_id"}}, nil
		}
		return fakedb.Result{}, fmt.Errorf("fakedb: 未脚本化的语句: %s", raw)
	})
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	svc := NewTraceArchiveService(gdb, nil, cpconfig.ArchiveConfig{Root: t.TempDir()})

	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	if _, err := svc.fetchBatch(context.Background(), start, start.Add(24*time.Hour), ""); err != nil {
		t.Fatalf("fetchBatch: %v", err)
	}

	for _, st := range db.Statements() {
		up := strings.ToUpper(st.SQL)
		if strings.Contains(up, parallelGUC) && !strings.Contains(up, "SET LOCAL") {
			t.Fatalf("检测到非 LOCAL 作用域的关并行语句（会污染连接池连接）: %s", st.SQL)
		}
	}
}
