// n10_readonly_test.go —— WS-F：N10「write=false, read=true → 点读仍可用」的装配级验收。
//
// 契约 §7.1：归档写路径与点读器是两个独立开关。关写（回滚）时：
//   - 写服务 Enabled()==false，ArchiveDay / RunCycle 不得触碰任何数据（连一条 SQL 都不发）；
//   - 点读器仍按 archive.read.enabled 独立构造，能按 archive_ref 从归档读回正文。
//
// 假 DB 的 handler 一律报错，因此「关写却发了 SQL」会立刻让用例失败（无真实 PG 依赖）。
package archive_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/archive"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	"github.com/example/llm-gateway/internal/observability/service"
	"github.com/example/llm-gateway/tests/archive/fakedb"
)

// TestN10_ReadPathIndependentOfWriteSwitch 覆盖 N10 主断言。
func TestN10_ReadPathIndependentOfWriteSwitch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	day := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	dir := "dt=" + day.Format("2006-01-02")

	// 先用写路径（模拟开关开启期间）落好一天归档。
	writeStore := newStore(t, root)
	rids := []string{"req_000", "req_001", "req_002"}
	writeDay(t, writeStore, dir, rids, 1<<20)

	// 关闭写、打开读（回滚态）。
	cfg := cpconfig.ArchiveConfig{
		WriteEnabled:       false,
		ReadEnabled:        true,
		Root:               root,
		TZ:                 "UTC",
		MaxConcurrentReads: 2,
		ZstdWindowMB:       8,
	}

	// 假 DB：任何 SQL 都会返回错误 → 关写路径一旦访问数据即失败。
	db := fakedb.New(func(q string, _ []any) (fakedb.Result, error) {
		return fakedb.Result{}, fmt.Errorf("N10 违规：write=false 时不得访问数据库: %s",
			strings.Join(strings.Fields(q), " "))
	})
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	svc := service.NewTraceArchiveService(gdb, nil, cfg)

	if svc.Enabled() {
		t.Fatal("write=false 时 Enabled() 必须为 false（否则回滚后仍会跑归档）")
	}
	settled, err := svc.ArchiveDay(ctx, day, true)
	if err != nil {
		t.Fatalf("write=false 时 ArchiveDay 必须是 no-op: %v", err)
	}
	if settled {
		t.Fatal("write=false 时不得报告某天已归档")
	}
	if err := svc.RunCycle(ctx); err != nil {
		t.Fatalf("write=false 时 RunCycle 必须是 no-op: %v", err)
	}
	if stmts := db.Statements(); len(stmts) != 0 {
		t.Fatalf("write=false 时不得发出任何 SQL，实际 %d 条：%s", len(stmts), stmts[0].SQL)
	}

	// 点读器（与 router 装配同形）：read=true 即可独立工作。
	reader := archive.NewLocalStore(cfg.Root, "")
	reader.SetMaxConcurrentReads(cfg.MaxConcurrentReads)
	reader.SetZstdWindowBytes(cfg.ZstdWindowMB << 20)

	if got := reader.MaxConcurrentReads(); got != 2 {
		t.Fatalf("MaxConcurrentReads = %d want 2", got)
	}
	if got := reader.ZstdWindowBytes(); got != 8<<20 {
		t.Fatalf("ZstdWindowBytes = %d want %d", got, 8<<20)
	}

	for _, rid := range rids {
		obj, err := reader.ReadBody(ctx, dir+"/", rid)
		if err != nil {
			t.Fatalf("write=false 时点读仍必须可用，ReadBody(%s): %v", rid, err)
		}
		want := fmt.Sprintf(`{"q":%q}`, rid)
		if !bytes.Equal(obj.RequestBody, []byte(want)) {
			t.Fatalf("ReadBody(%s) 正文 = %s want %s", rid, obj.RequestBody, want)
		}
	}

	// 读路径不得在天目录留下任何写入痕迹（只读实现，不影响归档内容）。
	parts, err := archive.LoadIndex(ctx, reader, dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	rb, err := archive.ReadBack(ctx, reader, dir, parts)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	if rb.RIDsSHA256 != frozenRIDsSHA256(rids) {
		t.Fatalf("点读后离线自校验摘要变化: %s", rb.RIDsSHA256)
	}
}

// TestN10_WriteEnabledPathStillRequiresReadSwitch 反向对照：
// 即使写路径打开，点读器也只在 read.enabled 下构造（read.root 为空时装配层显式禁用）。
// 这里断言两开关互不牵连：写开关不改读路径行为，读开关不改写路径行为。
func TestN10_WriteEnabledPathStillRequiresReadSwitch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	day := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	dir := "dt=" + day.Format("2006-01-02")

	writeStore := newStore(t, root)
	rids := []string{"req_aaa", "req_bbb"}
	writeDay(t, writeStore, dir, rids, 1<<20)

	// write=true, read=false：写路径可用（Enabled()==true），本次不做导出；
	// 点读器不被装配的判据落在调用方（router 只在 ReadEnabled 时构造），
	// 这里断言「同类 LocalStore 即使被构造也能读」不会因写开关变化而失效。
	cfg := cpconfig.ArchiveConfig{
		WriteEnabled: true,
		ReadEnabled:  false,
		Root:         root,
		TZ:           "UTC",
		ZstdWindowMB: 8,
	}
	db := fakedb.New(func(q string, _ []any) (fakedb.Result, error) {
		return fakedb.Result{}, fmt.Errorf("unexpected SQL: %s", strings.Join(strings.Fields(q), " "))
	})
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	svc := service.NewTraceArchiveService(gdb, nil, cfg)
	if !svc.Enabled() {
		t.Fatal("write=true 时 Enabled() 必须为 true")
	}

	reader := archive.NewLocalStore(filepath.Join(cfg.Root), "")
	reader.SetZstdWindowBytes(cfg.ZstdWindowMB << 20)
	if _, err := reader.ReadBody(ctx, dir+"/", "req_aaa"); err != nil {
		t.Fatalf("点读实现不依赖服务开关: %v", err)
	}
	if n := len(db.Statements()); n != 0 {
		t.Fatalf("本用例不应触发任何 SQL，实际 %d 条", n)
	}
}
