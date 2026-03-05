package ehrsync

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

func TestReconcileEntity_SoftDeletesMissing(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, d := range []string{"d1", "d2", "d3"} {
		db.Create(&model.EhrDepartment{
			OrgID:          "org1",
			DepartmentID:   d,
			DepartmentName: "Dept-" + d,
			EhrModifyDate:  "2025-01-01",
			SyncedAt:       now,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	err := ReconcileEntity(ctx, db, uuid.NewString(), "org1", "department", 10, 3, []string{"d2"}, logger)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var dept model.EhrDepartment
	if err := db.Where("org_id = ? AND department_id = ?", "org1", "d2").First(&dept).Error; err != nil {
		t.Fatalf("get d2: %v", err)
	}
	if dept.EhrModifyDate != "" {
		t.Fatalf("expected ehr_modify_date cleared, got %q", dept.EhrModifyDate)
	}

	var diff model.EhrSyncDiffReport
	if err := db.Where("entity = ?", "department").First(&diff).Error; err != nil {
		t.Fatalf("get diff: %v", err)
	}
	if diff.MissingCount != 1 {
		t.Fatalf("expected missing_count=1, got %d", diff.MissingCount)
	}
}

func TestCountOverride_NoOverrides(t *testing.T) {
	db := openTestDB(t)
	cnt, err := CountOverride(db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("expected 0, got %d", cnt)
	}
}

func TestCountOverride_DeptOnly(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	for i := 0; i < 3; i++ {
		db.Create(&model.EhrDepartment{
			OrgID:              "o1",
			DepartmentID:       "d" + string(rune('0'+i)),
			ManualOverrideAt:   &now,
			ManualOverrideBy:   "admin",
			SyncedAt:           now,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}

	cnt, err := CountOverride(db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 3 {
		t.Fatalf("expected 3, got %d", cnt)
	}
}

func TestReconcileEntity_Employee(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, eid := range []string{"e1", "e2", "e3"} {
		db.Create(&model.EhrEmployee{
			OrgID:      "org-1",
			EmployeeID: eid,
			Passport:   "P-" + eid,
			PersonName: "Name-" + eid,
			SyncedAt:   now.Add(-time.Hour),
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	err := ReconcileEntity(ctx, db, uuid.NewString(), "org-1", "employee", 10, 3, []string{"e2"}, logger)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var emp model.EhrEmployee
	if err := db.Where("org_id = ? AND employee_id = ?", "org-1", "e2").First(&emp).Error; err != nil {
		t.Fatalf("get e2: %v", err)
	}
	if emp.SyncedAt.Before(now.Add(-time.Minute)) {
		t.Fatalf("expected e2 synced_at updated, got %v", emp.SyncedAt)
	}

	var e1 model.EhrEmployee
	db.Where("org_id = ? AND employee_id = ?", "org-1", "e1").First(&e1)
	if e1.SyncedAt.After(now.Add(-time.Minute)) {
		t.Fatalf("expected e1 synced_at unchanged")
	}
}

func TestReconcileEntity_Company(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, cid := range []string{"c1", "c2", "c3"} {
		db.Create(&model.EhrCompany{
			OrgID:       "org-1",
			CompanyID:   cid,
			CompanyName: "Company-" + cid,
			SyncedAt:    now,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	err := ReconcileEntity(ctx, db, uuid.NewString(), "org-1", "company", 10, 3, []string{"c2"}, logger)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var count int64
	db.Model(&model.EhrCompany{}).Where("org_id = ? AND company_id = ?", "org-1", "c2").Count(&count)
	if count != 0 {
		t.Fatalf("expected c2 hard-deleted, got count=%d", count)
	}

	db.Model(&model.EhrCompany{}).Where("org_id = ?", "org-1").Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 remaining companies, got %d", count)
	}

	var diff model.EhrSyncDiffReport
	if err := db.Where("entity = ?", "company").First(&diff).Error; err != nil {
		t.Fatalf("get diff: %v", err)
	}
	if diff.MissingCount != 1 {
		t.Fatalf("expected missing_count=1, got %d", diff.MissingCount)
	}
}

func TestReconcileEntity_UnknownEntity(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	err := ReconcileEntity(ctx, db, uuid.NewString(), "org-1", "organization", 5, 3, []string{"o1"}, logger)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var diff model.EhrSyncDiffReport
	if err := db.Where("entity = ?", "organization").First(&diff).Error; err != nil {
		t.Fatalf("get diff: %v", err)
	}
	if diff.MissingCount != 1 {
		t.Fatalf("expected missing_count=1, got %d", diff.MissingCount)
	}
	if diff.TotalInEHR != 5 || diff.TotalInLocal != 3 {
		t.Fatalf("expected ehr=5 local=3, got ehr=%d local=%d", diff.TotalInEHR, diff.TotalInLocal)
	}
}

func TestCountOverride_EmployeesOnly(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	for i := 0; i < 4; i++ {
		eid := "e" + string(rune('0'+i))
		db.Create(&model.EhrEmployee{
			OrgID:              "o1",
			EmployeeID:         eid,
			Passport:           "P-" + eid,
			ManualOverrideAt:   &now,
			ManualOverrideBy:   "hr",
			SyncedAt:           now,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}

	db.Create(&model.EhrEmployee{
		OrgID:      "o1",
		EmployeeID: "e9",
		Passport:   "P-e9",
		SyncedAt:   now,
		CreatedAt:  now,
		UpdatedAt:  now,
	})

	cnt, err := CountOverride(db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 4 {
		t.Fatalf("expected 4, got %d", cnt)
	}
}

func TestCountOverride_Mixed(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	for i := 0; i < 2; i++ {
		db.Create(&model.EhrDepartment{
			OrgID:              "o1",
			DepartmentID:       "d" + string(rune('0'+i)),
			ManualOverrideAt:   &now,
			ManualOverrideBy:   "admin",
			SyncedAt:           now,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}

	for i := 0; i < 3; i++ {
		eid := "e" + string(rune('0'+i))
		db.Create(&model.EhrEmployee{
			OrgID:              "o1",
			EmployeeID:         eid,
			Passport:           "P-" + eid,
			ManualOverrideAt:   &now,
			ManualOverrideBy:   "hr",
			SyncedAt:           now,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}

	cnt, err := CountOverride(db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 5 {
		t.Fatalf("expected 5, got %d", cnt)
	}
}
