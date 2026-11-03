package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/observability/report"
)

// newReportSnapshotTestDB 建 report_snapshot + anomaly_review 两张表（内存 sqlite shared-cache）。
func newReportSnapshotTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("file:report_snapshot_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	ddl := []string{
		`CREATE TABLE report_snapshot (
			id TEXT PRIMARY KEY,
			level TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			period_type TEXT NOT NULL DEFAULT 'week',
			period_start DATE NOT NULL,
			period_end DATE NOT NULL,
			report_json TEXT NOT NULL DEFAULT '{}',
			schema_version TEXT NOT NULL DEFAULT '2.0',
			generated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE anomaly_review (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			dept_id TEXT NOT NULL DEFAULT '',
			period_start DATE NOT NULL,
			period_end DATE NOT NULL,
			unrelated_cost_usd REAL NOT NULL DEFAULT 0,
			unrelated_ratio REAL NOT NULL DEFAULT 0,
			top_hours TEXT NOT NULL DEFAULT '',
			top_model TEXT NOT NULL DEFAULT '',
			top_key TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending_review',
			reviewer TEXT NOT NULL DEFAULT '',
			reviewed_at TIMESTAMP,
			appeal_note TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
	}
	for _, s := range ddl {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("exec ddl: %v", err)
		}
	}
	return db
}

// samplePersonalReport 构造一份可校验的 personal Report（Σ归因=0 ≤ 总成本）。
func samplePersonalReport(t *testing.T) *report.Report {
	t.Helper()
	meta := report.Meta{
		Level: report.LevelPersonal, RuleVersion: "v0.6",
		Currency: report.Currency{Report: "USD", USDCNYRate: 1},
		Period:   report.Period{From: "2026-08-24", To: "2026-08-30"},
		Scope:    report.Scope{ID: "u1", Name: "alice"},
	}
	in := report.AssembleInput{
		DataQuality:   report.DataQuality{RealSessionCoverage: 1.0, PriceCatalogCoverage: 1.0, PositionLabelCoverage: 1.0, FailedCostCoverage: 1.0},
		CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 1.0, OutputCostUSD: 2.0, CacheWriteIncluded: true},
	}
	r, err := report.Assemble(meta, in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("assemble sample: %v", err)
	}
	return r
}

func TestReportSnapshot_GetUpsertGet(t *testing.T) {
	db := newReportSnapshotTestDB(t)
	ctx := context.Background()
	svc := NewReportSnapshotService(db)
	start := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	q := ReportSnapshotQuery{Level: report.LevelPersonal, ScopeID: "u1", PeriodType: "week", PeriodStart: start}

	// 未命中 → 返回 nil。
	got, err := svc.Get(ctx, q)
	if err != nil || got != nil {
		t.Fatalf("empty Get: got=%v err=%v", got, err)
	}

	r := samplePersonalReport(t)
	end := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	if err := svc.Upsert(ctx, q, end, r); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// 再取命中。
	got, err = svc.Get(ctx, q)
	if err != nil {
		t.Fatalf("Get after upsert: %v", err)
	}
	if got == nil || got.Meta.Scope.ID != "u1" || got.Meta.Level != report.LevelPersonal {
		t.Fatalf("cached report mismatch: %+v", got)
	}
}

func TestReportSnapshot_Members(t *testing.T) {
	db := newReportSnapshotTestDB(t)
	ctx := context.Background()
	svc := NewReportSnapshotService(db)
	start := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	q := ReportSnapshotQuery{Level: report.LevelStudio, ScopeID: "s1", PeriodType: "week", PeriodStart: start}

	// 未命中 → nil。
	m, err := svc.Members(ctx, q)
	if err != nil || m != nil {
		t.Fatalf("empty Members: m=%v err=%v", m, err)
	}

	// 构造 studio 报告（成员点）。
	in := report.AssembleInput{
		DataQuality:   report.DataQuality{RealSessionCoverage: 1.0, PriceCatalogCoverage: 1.0, PositionLabelCoverage: 1.0, FailedCostCoverage: 1.0},
		CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 5, OutputCostUSD: 5, CacheWriteIncluded: true},
		Members: []report.MemberAssembleInput{
			{Quadrant: report.MemberQuadrantInput{UserID: "u1", Name: "alice", CostUSD: 5, Calls: 5, IssueAttributed: map[string]float64{}}},
			{Quadrant: report.MemberQuadrantInput{UserID: "u2", Name: "bob", CostUSD: 5, Calls: 3, IssueAttributed: map[string]float64{}}},
		},
	}
	meta := report.Meta{
		Level: report.LevelStudio, RuleVersion: "v0.6",
		Currency: report.Currency{Report: "USD", USDCNYRate: 1},
		Period:   report.Period{From: "2026-08-24", To: "2026-08-30"},
		Scope:    report.Scope{ID: "s1", Name: "studio-1"},
	}
	r, err := report.Assemble(meta, in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("assemble studio: %v", err)
	}
	end := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	if err := svc.Upsert(ctx, q, end, r); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	m, err = svc.Members(ctx, q)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if m == nil || len(m.Members) != 2 {
		t.Fatalf("members mismatch: %+v", m)
	}
}

func TestReportSnapshot_WriteAnomalyFeedback(t *testing.T) {
	db := newReportSnapshotTestDB(t)
	ctx := context.Background()
	svc := NewReportSnapshotService(db)
	start := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

	// 先写一条 pending_review 记录。
	rec := cpmodel.AnomalyReview{
		UserID: "u1", PeriodStart: start, PeriodEnd: start.AddDate(0, 0, 6),
		Status: cpmodel.AnomalyReviewStatusPendingReview,
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed anomaly: %v", err)
	}

	// 非法状态 → 报错。
	err := svc.WriteAnomalyFeedback(ctx, "u1", "leader", AnomalyFeedback{Status: "bogus"})
	if err == nil {
		t.Fatalf("invalid status should error")
	}

	// 正常回写 confirmed_ok。
	if err := svc.WriteAnomalyFeedback(ctx, "u1", "leader", AnomalyFeedback{Status: cpmodel.AnomalyReviewStatusConfirmedOK, PeriodStart: "2026-08-24"}); err != nil {
		t.Fatalf("write feedback: %v", err)
	}
	var got cpmodel.AnomalyReview
	if err := db.First(&got, "user_id = ?", "u1").Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != cpmodel.AnomalyReviewStatusConfirmedOK || got.Reviewer != "leader" || got.ReviewedAt == nil {
		t.Fatalf("status not updated: %+v", got)
	}

	// 已终裁记录不可再改 → ErrRecordNotFound。
	err = svc.WriteAnomalyFeedback(ctx, "u1", "leader", AnomalyFeedback{Status: cpmodel.AnomalyReviewStatusNeedCheck})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("already resolved should not be updatable, got %v", err)
	}
}
