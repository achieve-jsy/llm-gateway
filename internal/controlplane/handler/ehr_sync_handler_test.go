package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/ehrsync"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func openEhrTestDB(t *testing.T) *gorm.DB {
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
		t.Skipf("AutoMigrate failed: %v", err)
	}
	return db
}

type mockScheduler struct {
	running bool
	batchID string
	err     error
}

func (m mockScheduler) TriggerManual(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.batchID, nil
}

func (m mockScheduler) IsRunning() bool { return m.running }

type ehrHandlerTestFixture struct {
	db      *gorm.DB
	handler *EhrSyncHandler
	router  *gin.Engine
}

func setupEhrHandler(t *testing.T, sched EhrSyncSchedulerInterface) *ehrHandlerTestFixture {
	t.Helper()
	db := openEhrTestDB(t)
	repo := ehrsync.NewRepository(db)
	h := NewEhrSyncHandler(db, repo, sched)

	r := gin.New()
	r.GET("/api/v1/ehr/organizations", h.ListOrganizations)
	r.GET("/api/v1/ehr/departments", h.ListDepartments)
	r.GET("/api/v1/ehr/employees", h.ListEmployees)
	r.POST("/api/v1/ehr/sync", h.TriggerSync)
	r.GET("/api/v1/ehr/status", h.GetStatus)
	r.POST("/api/v1/ehr/override", h.SetOverride)
	r.DELETE("/api/v1/ehr/override/:entity/:org_id/:entity_id", h.ReleaseOverride)
	r.GET("/api/v1/ehr/overrides", h.ListOverrides)
	r.GET("/api/v1/ehr/logs/executions", h.ListExecutionLogs)
	r.GET("/api/v1/ehr/logs/changes", h.ListChangeLogs)
	r.GET("/api/v1/ehr/logs/errors", h.ListErrorLogs)
	r.GET("/api/v1/ehr/logs/diffs", h.ListDiffReports)
	r.GET("/api/v1/ehr/logs/skips", h.ListSkipLogs)

	return &ehrHandlerTestFixture{db: db, handler: h, router: r}
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v\nbody: %s", err, w.Body.String())
	}
	return body
}

func TestEhrSyncHandler_ListOrganizations(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrOrganization{OrgID: "org-1", OrgName: "Alpha", SyncedAt: now})
	f.db.Create(&model.EhrOrganization{OrgID: "org-2", OrgName: "Beta", SyncedAt: now})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/ehr/organizations", nil)
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	total, ok := body["total"].(float64)
	if !ok || int(total) != 2 {
		t.Fatalf("expected total=2, got %v", body["total"])
	}
	items, ok := body["data"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 items, got %v", body["data"])
	}
}

func TestEhrSyncHandler_ListDepartments(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrDepartment{OrgID: "org-1", DepartmentID: "d1", DepartmentName: "Engineering", SyncedAt: now})
	f.db.Create(&model.EhrDepartment{OrgID: "org-1", DepartmentID: "d2", DepartmentName: "Marketing", SyncedAt: now})
	f.db.Create(&model.EhrDepartment{OrgID: "org-2", DepartmentID: "d3", DepartmentName: "Sales", SyncedAt: now})

	// Filter by org_id
	t.Run("FilterByOrgID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/departments?org_id=org-1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2 for org-1, got %d", total)
		}
	})

	// No filter returns all
	t.Run("NoFilter", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/departments", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 3 {
			t.Fatalf("expected total=3, got %d", total)
		}
	})

	t.Run("FilterByNameILIKE_SkipSQLite", func(t *testing.T) {
		if os.Getenv("TEST_POSTGRES") == "" {
			t.Skip("ILIKE not supported by SQLite; requires PostgreSQL")
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/departments?name=engin", nil)
		f.router.ServeHTTP(w, req)
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for name=engin, got %d", total)
		}
	})
}

func TestEhrSyncHandler_ListEmployees(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrEmployee{OrgID: "org-1", EmployeeID: "e1", PersonName: "Alice", DepartmentID: "dept-1", PersonStatus: 1, Passport: "P1", SyncedAt: now})
	f.db.Create(&model.EhrEmployee{OrgID: "org-1", EmployeeID: "e2", PersonName: "Bob", DepartmentID: "dept-1", PersonStatus: 1, Passport: "P2", SyncedAt: now})
	f.db.Create(&model.EhrEmployee{OrgID: "org-1", EmployeeID: "e3", PersonName: "Charlie", DepartmentID: "dept-2", PersonStatus: 0, Passport: "P3", SyncedAt: now})
	f.db.Create(&model.EhrEmployee{OrgID: "org-2", EmployeeID: "e4", PersonName: "Diana", DepartmentID: "dept-3", PersonStatus: 1, Passport: "P4", SyncedAt: now})

	// Filter by org_id
	t.Run("FilterByOrgID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/employees?org_id=org-1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 3 {
			t.Fatalf("expected total=3 for org-1, got %d", total)
		}
	})

	// Filter by dept_id
	t.Run("FilterByDeptID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/employees?dept_id=dept-1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2 for dept-1, got %d", total)
		}
	})

	// Filter by status
	t.Run("FilterByStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/employees?status=1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 3 {
			t.Fatalf("expected total=3 for status=1, got %d", total)
		}
	})

	// Filter by org_id + status combined
	t.Run("FilterByOrgAndStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/employees?org_id=org-1&status=1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2 for org-1+status=1, got %d", total)
		}
	})
}

func TestEhrSyncHandler_TriggerSync_Success(t *testing.T) {
	f := setupEhrHandler(t, mockScheduler{running: true, batchID: "batch-123"})

	w := httptest.NewRecorder()
	body := `{"sync_type":"incremental"}`
	req := httptest.NewRequest("POST", "/api/v1/ehr/sync", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", resp["data"])
	}
	if data["batch_id"] != "batch-123" {
		t.Fatalf("expected batch_id=batch-123, got %v", data["batch_id"])
	}
	if data["sync_type"] != "incremental" {
		t.Fatalf("expected sync_type=incremental, got %v", data["sync_type"])
	}
	if data["status"] != "running" {
		t.Fatalf("expected status=running, got %v", data["status"])
	}
}

func TestEhrSyncHandler_TriggerSync_InvalidType(t *testing.T) {
	f := setupEhrHandler(t, mockScheduler{batchID: "batch-1"})

	w := httptest.NewRecorder()
	body := `{"sync_type":"partial"}`
	req := httptest.NewRequest("POST", "/api/v1/ehr/sync", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEhrSyncHandler_SetOverride(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrDepartment{OrgID: "org-1", DepartmentID: "d1", DepartmentName: "Eng", SyncedAt: now})

	w := httptest.NewRecorder()
	body := `{"entity":"department","entity_id":"d1","org_id":"org-1","reason":"urgent fix"}`
	req := httptest.NewRequest("POST", "/api/v1/ehr/override", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	data := resp["data"].(map[string]any)
	if data["entity"] != "department" {
		t.Fatalf("expected entity=department, got %v", data["entity"])
	}
	if data["override_by"] != "unknown" {
		t.Fatalf("expected override_by=unknown, got %v", data["override_by"])
	}

	var dept model.EhrDepartment
	f.db.Where("org_id = ? AND department_id = ?", "org-1", "d1").First(&dept)
	if dept.ManualOverrideAt == nil {
		t.Fatalf("expected manual_override_at to be set in DB")
	}
}

func TestEhrSyncHandler_ReleaseOverride(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrDepartment{
		OrgID:              "org-1",
		DepartmentID:       "d1",
		DepartmentName:     "Eng",
		ManualOverrideAt:   &now,
		ManualOverrideBy:   "admin",
		ManualOverrideReason: "old reason",
		SyncedAt:           now,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/ehr/override/department/org-1/d1", nil)
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	data := resp["data"].(map[string]any)
	if data["released"] != true {
		t.Fatalf("expected released=true, got %v", data["released"])
	}

	var dept model.EhrDepartment
	f.db.Where("org_id = ? AND department_id = ?", "org-1", "d1").First(&dept)
	if dept.ManualOverrideAt != nil {
		t.Fatalf("expected manual_override_at to be nil after release")
	}
}

func TestEhrSyncHandler_ListOverrides(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrDepartment{OrgID: "org-1", DepartmentID: "d1", DepartmentName: "Eng", ManualOverrideAt: &now, ManualOverrideBy: "admin", SyncedAt: now})
	f.db.Create(&model.EhrEmployee{OrgID: "org-1", EmployeeID: "e1", PersonName: "Alice", ManualOverrideAt: &now, ManualOverrideBy: "admin", Passport: "P99", SyncedAt: now})
	f.db.Create(&model.EhrDepartment{OrgID: "org-1", DepartmentID: "d2", DepartmentName: "Sales", SyncedAt: now})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/ehr/overrides", nil)
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	data := resp["data"].(map[string]any)
	total := int(data["total"].(float64))
	if total != 2 {
		t.Fatalf("expected total=2 overrides, got %d", total)
	}
}

func TestEhrSyncHandler_GetStatus(t *testing.T) {
	f := setupEhrHandler(t, mockScheduler{running: true})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/ehr/status", nil)
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	data := resp["data"].(map[string]any)
	running, ok := data["scheduler_running"].(bool)
	if !ok || !running {
		t.Fatalf("expected scheduler_running=true, got %v", data["scheduler_running"])
	}
	if _, ok := data["override_count"].(float64); !ok {
		t.Fatalf("expected override_count in response, got %v", data)
	}
}

func TestEhrSyncHandler_ListExecutionLogs(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrSyncExecutionLog{BatchID: "b1", Entity: "department", OrgID: "org-1", SyncType: "incremental", Status: "success", StartedAt: now})
	f.db.Create(&model.EhrSyncExecutionLog{BatchID: "b2", Entity: "department", OrgID: "org-1", SyncType: "incremental", Status: "failed", StartedAt: now})
	f.db.Create(&model.EhrSyncExecutionLog{BatchID: "b3", Entity: "employee", OrgID: "org-1", SyncType: "full", Status: "success", StartedAt: now})

	// Filter by entity
	t.Run("FilterByEntity", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/executions?entity=department", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2 for entity=department, got %d", total)
		}
	})

	// Filter by status
	t.Run("FilterByStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/executions?status=success", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2 for status=success, got %d", total)
		}
	})

	// Filter by entity + status
	t.Run("FilterByEntityAndStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/executions?entity=department&status=success", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for entity=department+status=success, got %d", total)
		}
	})

	// No filter
	t.Run("NoFilter", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/executions", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 3 {
			t.Fatalf("expected total=3, got %d", total)
		}
	})
}

func TestEhrSyncHandler_ListChangeLogs(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrSyncChangeLog{BatchID: "b1", Entity: "department", EntityID: "d1", OrgID: "org-1", ChangeType: "create", Source: "sync", OccurredAt: now})
	f.db.Create(&model.EhrSyncChangeLog{BatchID: "b1", Entity: "department", EntityID: "d2", OrgID: "org-1", ChangeType: "update", Source: "sync", OccurredAt: now})
	f.db.Create(&model.EhrSyncChangeLog{BatchID: "b2", Entity: "employee", EntityID: "e1", OrgID: "org-1", ChangeType: "create", Source: "sync", OccurredAt: now})

	// Filter by entity
	t.Run("FilterByEntity", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/changes?entity=department", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2 for entity=department, got %d", total)
		}
	})

	// Filter by change_type
	t.Run("FilterByChangeType", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/changes?change_type=create", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2 for change_type=create, got %d", total)
		}
	})

	// Filter by entity + change_type
	t.Run("FilterByEntityAndChangeType", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/changes?entity=department&change_type=create", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for entity=department+change_type=create, got %d", total)
		}
	})
}

func TestEhrSyncHandler_ListErrorLogs(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrSyncErrorLog{BatchID: "b1", Entity: "department", OrgID: "org-1", Method: "GET", ErrorMsg: "timeout", OccurredAt: now})
	f.db.Create(&model.EhrSyncErrorLog{BatchID: "b1", Entity: "employee", OrgID: "org-1", Method: "POST", ErrorMsg: "invalid data", OccurredAt: now})

	// Filter by entity
	t.Run("FilterByEntity", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/errors?entity=department", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for entity=department, got %d", total)
		}
	})

	// No filter
	t.Run("NoFilter", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/errors", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2, got %d", total)
		}
	})
}

func TestEhrSyncHandler_ListDiffReports(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrSyncDiffReport{BatchID: "b1", OrgID: "org-1", Entity: "department", TotalInEHR: 10, TotalInLocal: 8, CreatedCount: 2, Status: "ok", CreatedAt: now})
	f.db.Create(&model.EhrSyncDiffReport{BatchID: "b1", OrgID: "org-2", Entity: "employee", TotalInEHR: 20, TotalInLocal: 18, CreatedCount: 2, Status: "ok", CreatedAt: now})

	// Filter by entity
	t.Run("FilterByEntity", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/diffs?entity=department", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for entity=department, got %d", total)
		}
	})

	// Filter by org_id
	t.Run("FilterByOrgID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/diffs?org_id=org-1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for org_id=org-1, got %d", total)
		}
	})

	// Filter by entity + org_id
	t.Run("FilterByEntityAndOrgID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/diffs?entity=department&org_id=org-1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for entity=department+org_id=org-1, got %d", total)
		}
	})
}

func TestEhrSyncHandler_ListSkipLogs(t *testing.T) {
	f := setupEhrHandler(t, nil)
	now := time.Now()

	f.db.Create(&model.EhrSyncSkipLog{BatchID: "b1", Entity: "department", EntityID: "d1", OrgID: "org-1", OverrideAt: now, OverrideBy: "admin"})
	f.db.Create(&model.EhrSyncSkipLog{BatchID: "b1", Entity: "employee", EntityID: "e1", OrgID: "org-1", OverrideAt: now, OverrideBy: "admin"})

	// Filter by entity
	t.Run("FilterByEntity", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/skips?entity=department", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for entity=department, got %d", total)
		}
	})

	// Filter by entity_id
	t.Run("FilterByEntityID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/skips?entity_id=e1", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected total=1 for entity_id=e1, got %d", total)
		}
	})

	// No filter
	t.Run("NoFilter", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/ehr/logs/skips", nil)
		f.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := parseJSON(t, w)
		total := int(body["total"].(float64))
		if total != 2 {
			t.Fatalf("expected total=2, got %d", total)
		}
	})
}
