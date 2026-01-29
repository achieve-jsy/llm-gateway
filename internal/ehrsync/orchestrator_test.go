package ehrsync

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/ehrclient"
	"github.com/redis/go-redis/v9"
)

func newTestOrchestrator(t *testing.T, handler http.Handler) (*Orchestrator, *miniredis.Miniredis, func()) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	srv := httptest.NewServer(handler)

	cfg := ehrclient.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Token = "test-token"
	cfg.RateLimit = 0
	cfg.MaxRetries = 0
	cfg.Timeout = 5 * time.Second
	client := ehrclient.NewClient(cfg)

	db := openTestDB(t)
	repo := NewRepository(db)
	cursor := NewCursorManager(rdb)
	recorder := NewLogRecorder(repo)

	orch := NewOrchestrator(OrchestratorDeps{
		Client:    client,
		Repo:      repo,
		Cursor:    cursor,
		Recorder:  recorder,
		Log:       slog.Default(),
		BatchSize: 100,
	})

	cleanup := func() {
		srv.Close()
		mr.Close()
		_ = rdb.Close()
	}
	return orch, mr, cleanup
}

func healthyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/open-apis/ehr-apic/resource/v1/organizations"):
			w.Write([]byte(`{"items":[{"orgId":"org-1","orgName":"Alpha Org","orgCode":"A","createDate":"2024-01-01","lastModifyDate":"2025-01-15 10:00:00"}]}`))
		case strings.HasPrefix(r.URL.Path, "/open-apis/ehr-apic/resource/v1/companies"):
			w.Write([]byte(`{"items":[{"companyId":"c1","companyName":"AlphaCo","companyCode":"AC","parentId":"","lastModifyDate":"2025-01-15 10:00:00"}]}`))
		case strings.HasPrefix(r.URL.Path, "/open-apis/ehr-apic/resource/v1/departments"):
			w.Write([]byte(`{"items":[{"departmentId":"d1","departmentName":"Engineering","departmentCode":"ENG","departmentType":1,"departmentTypeName":"Tech","parentId":"","lastModifyDate":"2025-01-15 10:00:00","leaderName":"","leaderPassport":"","leaderPersonId":"","leaderEmployeeId":"","managerName":"","managerPassport":"","managerPersonId":"","managerEmployeeId":"","hrBpName":"","hrBpEmployeeId":""}]}`))
		case strings.HasPrefix(r.URL.Path, "/open-apis/ehr-apic/resource/v1/employees"):
			w.Write([]byte(`{"items":[{"employeeId":"e1","personId":"pid1","passport":"P001","appointName":"Alice","appointCode":"A1","office":"HQ","email":"alice@x.com","personStatus":1,"personType":0,"isDefault":true,"companyId":"c1","companyName":"AlphaCo","departmentId":"d1","departmentName":"Engineering","positionId":"pos1","positionName":"SWE","positionCategoryId":"cat1","managerEmployeeId":"","managerPersonId":"","managerPassport":"","managerName":"","personName":"Alice","groupENumber":"E001","gender":1,"joinDate":"2024-01-01","onBoardDate":"2024-01-02","location":"NYC","dismissDate":"","lastModifyDate":"2025-01-15 10:00:00"}]}`))
		case strings.HasPrefix(r.URL.Path, "/open-apis/ehr-apic/resource/v1/positions"):
			w.Write([]byte(`{"items":[{"id":"pos1","name":"Software Engineer","type":1,"namePath":"Software Engineer","items":[]}]}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func TestOrchestrator_RunIncremental_BasicFlow(t *testing.T) {
	orch, mr, cleanup := newTestOrchestrator(t, healthyHandler())
	defer cleanup()

	ctx := context.Background()
	res, err := orch.RunIncremental(ctx)
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}
	if res.ErrorCount != 0 {
		t.Fatalf("expected 0 errors, got %d", res.ErrorCount)
	}
	if res.BatchID == "" {
		t.Fatalf("expected non-empty BatchID")
	}
	orgRes, ok := res.ByOrg["org-1"]
	if !ok {
		t.Fatalf("expected org-1 in ByOrg, keys: %v", keysOf(res.ByOrg))
	}

	if e := orgRes.ByEntity["company"]; e == nil || e.Created != 1 {
		t.Errorf("company: expected 1 created, got %+v", e)
	}
	if e := orgRes.ByEntity["department"]; e == nil || e.Created != 1 {
		t.Errorf("department: expected 1 created, got %+v", e)
	}
	if e := orgRes.ByEntity["employee"]; e == nil || e.Created != 1 {
		t.Errorf("employee: expected 1 created, got %+v", e)
	}

	repo := orch.repo
	var orgs []model.EhrOrganization
	if err := repo.db.Find(&orgs).Error; err != nil || len(orgs) != 1 {
		t.Fatalf("orgs: err=%v len=%d", err, len(orgs))
	}
	if orgs[0].OrgName != "Alpha Org" {
		t.Errorf("org name: %s", orgs[0].OrgName)
	}

	var companies []model.EhrCompany
	if err := repo.db.Find(&companies).Error; err != nil || len(companies) != 1 {
		t.Fatalf("companies: err=%v len=%d", err, len(companies))
	}
	if companies[0].CompanyName != "AlphaCo" {
		t.Errorf("company name: %s", companies[0].CompanyName)
	}

	var depts []model.EhrDepartment
	if err := repo.db.Find(&depts).Error; err != nil || len(depts) != 1 {
		t.Fatalf("departments: err=%v len=%d", err, len(depts))
	}
	if depts[0].DepartmentName != "Engineering" {
		t.Errorf("department name: %s", depts[0].DepartmentName)
	}

	var emps []model.EhrEmployee
	if err := repo.db.Find(&emps).Error; err != nil || len(emps) != 1 {
		t.Fatalf("employees: err=%v len=%d", err, len(emps))
	}
	if emps[0].PersonName != "Alice" {
		t.Errorf("employee name: %s", emps[0].PersonName)
	}

	var execCount int64
	repo.db.Table("ehr_sync_execution_logs").Count(&execCount)
	if execCount != 1 {
		t.Fatalf("execution_logs: expected 1 row, got %d", execCount)
	}
	var execStatus string
	repo.db.Table("ehr_sync_execution_logs").Select("status").Pluck("status", &execStatus)
	if execStatus != "completed" {
		t.Errorf("execution_log status: %s (want completed)", execStatus)
	}

	cur, err := orch.cursor.Get(ctx, "company", "org-1")
	if err != nil || cur != "2025-01-15 10:00:00" {
		t.Errorf("company cursor: got %q err=%v", cur, err)
	}
	cur, err = orch.cursor.Get(ctx, "department", "org-1")
	if err != nil || cur != "2025-01-15 10:00:00" {
		t.Errorf("department cursor: got %q err=%v", cur, err)
	}
	_ = mr
}

func TestOrchestrator_RunIncremental_WithOverride(t *testing.T) {
	orch, _, cleanup := newTestOrchestrator(t, healthyHandler())
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	orch.repo.db.Create(&model.EhrDepartment{
		OrgID:              "org-1",
		DepartmentID:       "d1",
		DepartmentName:     "Manual Engineering",
		EhrModifyDate:      "2024-12-01",
		ManualOverrideAt:   &now,
		ManualOverrideBy:   "admin",
		ManualOverrideReason: "custom name",
		SyncedAt:           now,
	})

	res, err := orch.RunIncremental(ctx)
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}

	orgRes := res.ByOrg["org-1"]
	deptRes := orgRes.ByEntity["department"]
	if deptRes == nil {
		t.Fatalf("no department result")
	}
	if deptRes.Skipped != 1 {
		t.Errorf("expected 1 skipped (override), got %+v", deptRes)
	}

	var got model.EhrDepartment
	if err := orch.repo.db.Where("org_id = ? AND department_id = ?", "org-1", "d1").First(&got).Error; err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DepartmentName != "Manual Engineering" {
		t.Errorf("override NOT preserved: got %q want %q", got.DepartmentName, "Manual Engineering")
	}
	if got.ManualOverrideAt == nil {
		t.Errorf("override_at was cleared")
	}

	var skips []model.EhrSyncSkipLog
	if err := orch.repo.db.Find(&skips).Error; err != nil {
		t.Fatalf("skip_logs: %v", err)
	}
	if len(skips) < 1 {
		t.Errorf("expected at least 1 skip_log, got %d", len(skips))
	}
}

func TestOrchestrator_RunIncremental_EHRDown(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
	})
	orch, _, cleanup := newTestOrchestrator(t, handler)
	defer cleanup()

	ctx := context.Background()
	res, err := orch.RunIncremental(ctx)
	if err == nil {
		t.Fatalf("expected error when EHR is down, got nil")
	}
	if res == nil {
		t.Fatalf("expected non-nil result even on error")
	}
	if res.ErrorCount == 0 {
		t.Errorf("expected ErrorCount > 0, got %d", res.ErrorCount)
	}

	var execLogs []model.EhrSyncExecutionLog
	orch.repo.db.Find(&execLogs)
	if len(execLogs) == 0 {
		t.Logf("no execution_log written (expected on early exit)")
	} else {
		for _, l := range execLogs {
			if l.Status == "completed" {
				t.Errorf("execution_log status should not be completed, got %s", l.Status)
			}
		}
	}
}

func TestOrchestrator_RunFullSync(t *testing.T) {
	orch, _, cleanup := newTestOrchestrator(t, healthyHandler())
	defer cleanup()

	ctx := context.Background()
	res, err := orch.RunFullSync(ctx)
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if res.ErrorCount != 0 {
		t.Fatalf("expected 0 errors, got %d", res.ErrorCount)
	}
	orgRes, ok := res.ByOrg["org-1"]
	if !ok {
		t.Fatalf("org-1 missing from ByOrg")
	}
	for _, name := range []string{"company", "department", "employee", "position"} {
		e := orgRes.ByEntity[name]
		if e == nil {
			t.Errorf("missing entity result: %s", name)
			continue
		}
		if e.Created < 1 {
			t.Errorf("%s: expected >=1 created, got %+v", name, e)
		}
	}

	var positions []model.EhrPosition
	if err := orch.repo.db.Find(&positions).Error; err != nil || len(positions) != 1 {
		t.Fatalf("positions: err=%v len=%d", err, len(positions))
	}
	if positions[0].Name != "Software Engineer" {
		t.Errorf("position name: %s", positions[0].Name)
	}

	var snaps []model.EhrSyncStatSnapshot
	if err := orch.repo.db.Find(&snaps).Error; err != nil || len(snaps) != 1 {
		t.Fatalf("stat_snapshots: err=%v len=%d", err, len(snaps))
	}
	if snaps[0].LastFullReconcileMs <= 0 {
		t.Errorf("expected LastFullReconcileMs > 0, got %d", snaps[0].LastFullReconcileMs)
	}
}

func keysOf[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
