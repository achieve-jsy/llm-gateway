package ehrsync

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Skipf("sqlite not available: %v", err)
		return nil
	}
	if err := db.AutoMigrate(
		&model.EhrOrganization{},
		&model.EhrCompany{},
		&model.EhrDepartment{},
		&model.EhrEmployee{},
		&model.EhrPosition{},
		&model.EhrSyncExecutionLog{},
		&model.EhrSyncChangeLog{},
		&model.EhrSyncErrorLog{},
		&model.EhrSyncDiffReport{},
		&model.EhrSyncSkipLog{},
		&model.EhrSyncStatSnapshot{},
	); err != nil {
		t.Helper()
		t.Skipf("AutoMigrate on sqlite failed (possibly due to gen_random_uuid in tag; check BeforeCreate hook): %v", err)
	}
	return db
}

func TestUpsertOrganizations_createsThenUpdates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewRepository(db)

	items := []model.EhrOrganization{
		{
			OrgID:         "org-1",
			OrgName:       "Alpha",
			OrgCode:       "A",
			EhrModifyDate: "2025-01-01",
			SyncedAt:      time.Now(),
		},
	}
	res, err := repo.UpsertOrganizations(items)
	if err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	if res.Created != 1 || res.Updated != 0 {
		t.Fatalf("expected 1 created, got %+v", res)
	}

	// 第二次 upsert，名称变 → update
	items[0].OrgName = "Alpha-Updated"
	res, err = repo.UpsertOrganizations(items)
	if err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if res.Created != 0 || res.Updated != 1 {
		t.Fatalf("expected 1 updated, got %+v", res)
	}

	// 验证 DB 值
	var got model.EhrOrganization
	if err := db.WithContext(ctx).Where("org_id = ?", "org-1").First(&got).Error; err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrgName != "Alpha-Updated" {
		t.Fatalf("expected Alpha-Updated, got %s", got.OrgName)
	}
}

func TestUpsertDepartments_overrideSkips(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)

	// 1. 插入一条记录，打 override 标记
	now := time.Now()
	db.Create(&model.EhrDepartment{
		OrgID:              "org-1",
		DepartmentID:       "dept-1",
		DepartmentName:     "ManualName",
		EhrModifyDate:      "2025-01-01",
		ManualOverrideAt:   &now,
		ManualOverrideBy:   "admin",
		SyncedAt:           now,
	})

	// 2. 模拟 EHR 拉取的变更（名称变更）
	items := []model.EhrDepartment{
		{
			OrgID:          "org-1",
			DepartmentID:   "dept-1",
			DepartmentName: "EHRNewName",
			ParentID:       "",
			EhrModifyDate:  "2025-01-02",
			SyncedAt:       time.Now(),
		},
	}

	res, diffs, err := repo.UpsertDepartments(items)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if res.Skipped != 1 {
		t.Fatalf("expected 1 skipped, got %+v", res)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff entry, got %d", len(diffs))
	}

	// 3. 验证 DB 值未被覆盖
	var got model.EhrDepartment
	if err := db.Where("org_id = ? AND department_id = ?", "org-1", "dept-1").First(&got).Error; err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DepartmentName != "ManualName" {
		t.Fatalf("expected ManualName preserved, got %s", got.DepartmentName)
	}
}

func TestUpsertEmployees_createdOnly(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)

	items := []model.EhrEmployee{
		{
			OrgID:         "org-1",
			EmployeeID:    "emp-1",
			PersonName:    "Alice",
			Passport:      "PA001",
			Email:         "alice@example.com",
			SyncedAt:      time.Now(),
		},
		{
			OrgID:         "org-1",
			EmployeeID:    "emp-2",
			PersonName:    "Bob",
			Passport:      "PA002",
			SyncedAt:      time.Now(),
		},
	}
	res, _, err := repo.UpsertEmployees(items)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if res.Created != 2 || res.Updated != 0 {
		t.Fatalf("expected 2 created, got %+v", res)
	}
}

func TestCountOverride(t *testing.T) {
	db := openTestDB(t)

	now := time.Now()
	db.Create(&model.EhrDepartment{
		OrgID: "o1", DepartmentID: "d1", ManualOverrideAt: &now, SyncedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	db.Create(&model.EhrDepartment{
		OrgID: "o1", DepartmentID: "d2", SyncedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	db.Create(&model.EhrEmployee{
		OrgID: "o1", EmployeeID: "e1", ManualOverrideAt: &now, SyncedAt: now, CreatedAt: now, UpdatedAt: now,
	})

	cnt, err := CountOverride(db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("expected 2 override records, got %d", cnt)
	}
}

func TestEmpDiffFn_NoOverrideWithChanges(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	now := time.Now()

	db.Create(&model.EhrEmployee{
		OrgID:             "org-1",
		EmployeeID:        "emp-1",
		PersonName:        "Alice",
		DepartmentID:      "dept-old",
		ManagerEmployeeID: "mgr-old",
		EhrModifyDate:     "2025-01-01",
		SyncedAt:          now,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	items := []model.EhrEmployee{
		{
			OrgID:             "org-1",
			EmployeeID:        "emp-1",
			PersonName:        "Alice",
			DepartmentID:      "dept-new",
			ManagerEmployeeID: "mgr-new",
			EhrModifyDate:     "2025-01-02",
			SyncedAt:          now,
		},
	}

	res, diffs, err := repo.UpsertEmployees(items)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if res.Created != 0 || res.Updated != 1 {
		t.Fatalf("expected 1 updated, got %+v", res)
	}
	if res.Skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", res.Skipped)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff entry, got %d", len(diffs))
	}

	found := map[string]bool{}
	for _, k := range diffs[0].DifferingFields {
		found[k] = true
	}
	for _, want := range []string{"department_id", "manager_employee_id", "ehr_modify_date"} {
		if !found[want] {
			t.Fatalf("expected diff for %q, got keys=%v", want, diffs[0].DifferingFields)
		}
	}

	var got model.EhrEmployee
	db.Where("org_id = ? AND employee_id = ?", "org-1", "emp-1").First(&got)
	if got.DepartmentID != "dept-new" {
		t.Fatalf("expected dept=new, got %s", got.DepartmentID)
	}
}

func TestEmpDiffFn_OverrideSkips(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	now := time.Now()

	db.Create(&model.EhrEmployee{
		OrgID:             "org-1",
		EmployeeID:        "emp-1",
		PersonName:        "Alice",
		DepartmentID:      "dept-local",
		ManagerEmployeeID: "mgr-local",
		ManualOverrideAt:  &now,
		ManualOverrideBy:  "hr",
		SyncedAt:          now,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	items := []model.EhrEmployee{
		{
			OrgID:             "org-1",
			EmployeeID:        "emp-1",
			PersonName:        "Alice",
			DepartmentID:      "dept-ehr",
			ManagerEmployeeID: "mgr-ehr",
			EhrModifyDate:     "2025-06-01",
			SyncedAt:          now,
		},
	}

	res, diffs, err := repo.UpsertEmployees(items)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if res.Skipped != 1 {
		t.Fatalf("expected 1 skipped, got %+v", res)
	}
	if res.Updated != 0 {
		t.Fatalf("expected 0 updated, got %d", res.Updated)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff entry (skip), got %d", len(diffs))
	}
	if diffs[0].LocalOverrideAt == nil {
		t.Fatalf("expected skip entry to carry override timestamp")
	}

	var got model.EhrEmployee
	db.Where("org_id = ? AND employee_id = ?", "org-1", "emp-1").First(&got)
	if got.DepartmentID != "dept-local" {
		t.Fatalf("expected dept-local preserved, got %s", got.DepartmentID)
	}
}

func TestDeptDiffFn_UpdateOnChangedFields(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	now := time.Now()

	db.Create(&model.EhrDepartment{
		OrgID:          "org-1",
		DepartmentID:   "dept-1",
		DepartmentName: "OldName",
		ParentID:       "parent-old",
		EhrModifyDate:  "2025-01-01",
		SyncedAt:       now,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	items := []model.EhrDepartment{
		{
			OrgID:          "org-1",
			DepartmentID:   "dept-1",
			DepartmentName: "NewName",
			ParentID:       "parent-new",
			EhrModifyDate:  "2025-06-01",
			SyncedAt:       now,
		},
	}

	res, diffs, err := repo.UpsertDepartments(items)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if res.Updated != 1 {
		t.Fatalf("expected 1 updated, got %+v", res)
	}
	if res.Skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", res.Skipped)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff entry, got %d", len(diffs))
	}

	found := map[string]bool{}
	for _, k := range diffs[0].DifferingFields {
		found[k] = true
	}
	for _, want := range []string{"department_name", "parent_id", "ehr_modify_date"} {
		if !found[want] {
			t.Fatalf("expected diff for %q, got %v", want, diffs[0].DifferingFields)
		}
	}

	var got model.EhrDepartment
	db.Where("org_id = ? AND department_id = ?", "org-1", "dept-1").First(&got)
	if got.DepartmentName != "NewName" {
		t.Fatalf("expected NewName, got %s", got.DepartmentName)
	}
	if got.ParentID != "parent-new" {
		t.Fatalf("expected parent-new, got %s", got.ParentID)
	}
}

func TestUpsertCompanies_BasicCreate(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)

	items := []model.EhrCompany{
		{
			OrgID:         "org-1",
			CompanyID:     "c1",
			CompanyName:   "Acme Corp",
			CompanyCode:   "ACME",
			SyncedAt:      time.Now(),
		},
		{
			OrgID:         "org-1",
			CompanyID:     "c2",
			CompanyName:   "Globex Inc",
			CompanyCode:   "GLOBEX",
			ParentID:      "c1",
			SyncedAt:      time.Now(),
		},
	}

	res, err := repo.UpsertCompanies(items)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if res.Created != 2 {
		t.Fatalf("expected 2 created, got %+v", res)
	}
	if res.Updated != 0 {
		t.Fatalf("expected 0 updated, got %+v", res)
	}
	if res.Fetched != 2 {
		t.Fatalf("expected fetched=2, got %d", res.Fetched)
	}

	var count int64
	db.Model(&model.EhrCompany{}).Where("org_id = ?", "org-1").Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}

	// second upsert same data → no update (no diff)
	res2, err := repo.UpsertCompanies(items)
	if err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if res2.Created != 0 || res2.Updated != 0 {
		t.Fatalf("expected no-op on identical data, got %+v", res2)
	}
}

func TestSetAndClearOverride(t *testing.T) {
	db := openTestDB(t)

	now := time.Now()
	db.Create(&model.EhrEmployee{
		OrgID: "o1", EmployeeID: "e1", PersonName: "Alice",
		SyncedAt: now, CreatedAt: now, UpdatedAt: now,
	})

	// Set
	if err := SetOverride[*model.EhrEmployee](db, "o1", "e1", "admin", "urgent move"); err != nil {
		t.Fatalf("set: %v", err)
	}
	var got model.EhrEmployee
	db.Where("org_id = ? AND employee_id = ?", "o1", "e1").First(&got)
	if got.ManualOverrideAt == nil {
		t.Fatalf("expected override_at set")
	}
	if got.ManualOverrideBy != "admin" {
		t.Fatalf("expected override_by=admin, got %s", got.ManualOverrideBy)
	}

	// Clear
	if err := ClearOverride[*model.EhrEmployee](db, "o1", "e1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	db.Where("org_id = ? AND employee_id = ?", "o1", "e1").First(&got)
	if got.ManualOverrideAt != nil {
		t.Fatalf("expected override_at cleared")
	}
}
