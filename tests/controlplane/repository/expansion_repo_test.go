package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newExpansionTestRepo(t *testing.T) *repository.ExpansionRepo {
	t.Helper()
	repo, _ := newExpansionTestDB(t)
	return repo
}

func newExpansionTestDB(t *testing.T) (*repository.ExpansionRepo, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE expansion_requests (
			id TEXT PRIMARY KEY,
			scope_type TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			requester_id TEXT NOT NULL,
			period_type TEXT,
			dimension_type TEXT,
			dimension_key TEXT,
			model_tier_id TEXT,
			duration_type TEXT,
			priority TEXT,
			current_amount REAL,
			requested_amount REAL NOT NULL,
			current_amount_snapshot NUMERIC,
			used_amount_snapshot NUMERIC,
			precheck_version TEXT,
			prechecked_at DATETIME,
			expires_at DATETIME,
			applied_at DATETIME,
			applied_target_type TEXT,
			applied_target_id TEXT,
			sync_status TEXT,
			reason TEXT,
			urgent_reason TEXT,
			status TEXT NOT NULL,
			current_level TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create expansion_requests: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX uniq_expansion_request_pending
		ON expansion_requests(requester_id, scope_type, scope_id, period_type, dimension_key, duration_type)
		WHERE status IN ('pending', 'precheck_required')`).Error; err != nil {
		t.Fatalf("create pending index: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE expansion_approval_steps (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL,
			level TEXT NOT NULL,
			approver_id TEXT,
			action TEXT NOT NULL,
			comment TEXT,
			created_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create expansion_approval_steps: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE studios (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE departments (dept_id TEXT PRIMARY KEY, dept_name TEXT)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create lookup table: %v", err)
		}
	}
	return repository.NewExpansionRepo(db), db
}

func TestExpansionCreateNormalizesPendingUniqueConflict(t *testing.T) {
	repo := newExpansionTestRepo(t)
	requesterID := uuid.New()
	build := func() *model.ExpansionRequest {
		return &model.ExpansionRequest{
			ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: "member-a", RequesterID: requesterID,
			PeriodType: model.ExpansionPeriodMonthly, DimensionType: "total", DimensionKey: "total",
			DurationType: "permanent", RequestedAmount: 100, Status: model.ExpansionStatusPending,
			CurrentLevel: model.ExpansionLevelDept,
		}
	}
	if err := repo.Create(build()); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := repo.Create(build())
	if !errors.Is(err, repository.ErrDuplicatePendingExpansion) {
		t.Fatalf("expected stable duplicate error, got %v", err)
	}
}

func TestCreateRequestForStudioBudgetGoesToPlatformLevel(t *testing.T) {
	repo := newExpansionTestRepo(t)
	svc := service.NewExpansionService(repo)

	req, err := svc.CreateRequest(context.Background(), model.BudgetScopeStudio, "studio-a", uuid.New(), 1000, 2000, model.ExpansionPeriodMonthly, "workspace needs more budget")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	if req.CurrentLevel != model.ExpansionLevelPlatform {
		t.Fatalf("expected studio budget request to go platform level, got %q", req.CurrentLevel)
	}
}

func TestCreateRequestForMemberBudgetGoesToDeptLevel(t *testing.T) {
	repo := newExpansionTestRepo(t)
	svc := service.NewExpansionService(repo)
	memberID := uuid.New()

	req, err := svc.CreateRequest(context.Background(), model.BudgetScopeMember, memberID.String(), memberID, 10, 50, model.ExpansionPeriodMonthly, "need more personal quota")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	if req.CurrentLevel != model.ExpansionLevelDept {
		t.Fatalf("expected member budget request to go dept level, got %q", req.CurrentLevel)
	}
}

func TestListPendingForApproverScopedFiltersStudioLevelByManagedScopes(t *testing.T) {
	repo := newExpansionTestRepo(t)
	requesterID := uuid.New()
	requests := []*model.ExpansionRequest{
		{
			ID:              uuid.New(),
			ScopeType:       model.BudgetScopeDeptDaily,
			ScopeID:         "dept-a",
			RequesterID:     requesterID,
			RequestedAmount: 200,
			Status:          model.ExpansionStatusPending,
			CurrentLevel:    model.ExpansionLevelStudio,
		},
		{
			ID:              uuid.New(),
			ScopeType:       model.BudgetScopeDeptService,
			ScopeID:         "dept-b",
			RequesterID:     requesterID,
			RequestedAmount: 300,
			Status:          model.ExpansionStatusPending,
			CurrentLevel:    model.ExpansionLevelStudio,
		},
		{
			ID:              uuid.New(),
			ScopeType:       model.BudgetScopeStudio,
			ScopeID:         "studio-a",
			RequesterID:     requesterID,
			RequestedAmount: 500,
			Status:          model.ExpansionStatusPending,
			CurrentLevel:    model.ExpansionLevelPlatform,
		},
	}
	for _, req := range requests {
		if err := repo.Create(req); err != nil {
			t.Fatalf("seed request: %v", err)
		}
	}

	got, err := repo.ListPendingForApproverScoped(model.ExpansionLevelStudio, []string{"dept-a"}, nil, nil, "")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 scoped pending request, got %d", len(got))
	}
	if got[0].ScopeID != "dept-a" {
		t.Fatalf("expected dept-a request, got %s", got[0].ScopeID)
	}
}

func TestListPendingForApproverScopedFiltersDeptLevelByManagedMembers(t *testing.T) {
	repo := newExpansionTestRepo(t)
	requesterID := uuid.New()
	requests := []*model.ExpansionRequest{
		{
			ID:              uuid.New(),
			ScopeType:       model.BudgetScopeMember,
			ScopeID:         "member-a",
			RequesterID:     requesterID,
			RequestedAmount: 50,
			Status:          model.ExpansionStatusPending,
			CurrentLevel:    model.ExpansionLevelDept,
		},
		{
			ID:              uuid.New(),
			ScopeType:       model.BudgetScopeMember,
			ScopeID:         "member-b",
			RequesterID:     requesterID,
			RequestedAmount: 80,
			Status:          model.ExpansionStatusPending,
			CurrentLevel:    model.ExpansionLevelDept,
		},
	}
	for _, req := range requests {
		if err := repo.Create(req); err != nil {
			t.Fatalf("seed request: %v", err)
		}
	}

	got, err := repo.ListPendingForApproverScoped(model.ExpansionLevelDept, nil, nil, []string{"member-a"}, "")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 scoped member request, got %d", len(got))
	}
	if got[0].ScopeID != "member-a" {
		t.Fatalf("expected member-a request, got %s", got[0].ScopeID)
	}
}

func TestExpansionListFilterKeywordMatchesScopeDisplayNames(t *testing.T) {
	repo, db := newExpansionTestDB(t)
	requesterID := uuid.New()
	memberID := uuid.New().String()
	otherMemberID := uuid.New().String()
	studioID := uuid.New().String()
	deptID := uuid.New().String()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, name) VALUES (?, ?)`, []any{requesterID.String(), "申请人甲"}},
		{`INSERT INTO users (id, name) VALUES (?, ?)`, []any{memberID, "林晓"}},
		{`INSERT INTO users (id, name) VALUES (?, ?)`, []any{otherMemberID, "周然"}},
		{`INSERT INTO studios (id, name) VALUES (?, ?)`, []any{studioID, "互娱事业部"}},
		{`INSERT INTO departments (dept_id, dept_name) VALUES (?, ?)`, []any{deptID, "技术研发部"}},
	} {
		if err := db.Exec(stmt.sql, stmt.args...).Error; err != nil {
			t.Fatalf("seed lookup: %v", err)
		}
	}

	requests := []*model.ExpansionRequest{
		{
			ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: memberID, RequesterID: requesterID,
			RequestedAmount: 50, Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
			Reason: "日常扩容",
		},
		{
			ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: otherMemberID, RequesterID: requesterID,
			RequestedAmount: 60, Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
			Reason: "日常扩容",
		},
		{
			ID: uuid.New(), ScopeType: model.BudgetScopeStudio, ScopeID: studioID, RequesterID: requesterID,
			RequestedAmount: 100, Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelPlatform,
			Reason: "工作室扩容",
		},
		{
			ID: uuid.New(), ScopeType: model.BudgetScopeDeptDaily, ScopeID: deptID, RequesterID: requesterID,
			RequestedAmount: 80, Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelStudio,
			Reason: "部门扩容",
		},
	}
	for _, req := range requests {
		if err := repo.Create(req); err != nil {
			t.Fatalf("seed request: %v", err)
		}
	}

	gotMember, err := repo.ListByRequesterFiltered(requesterID.String(), repository.ExpansionListFilter{Keyword: "林晓"})
	if err != nil {
		t.Fatalf("keyword member: %v", err)
	}
	if len(gotMember) != 1 || gotMember[0].ScopeID != memberID {
		t.Fatalf("expected member 林晓 hit, got %+v", gotMember)
	}

	gotStudio, err := repo.ListByRequesterFiltered(requesterID.String(), repository.ExpansionListFilter{Keyword: "互娱"})
	if err != nil {
		t.Fatalf("keyword studio: %v", err)
	}
	if len(gotStudio) != 1 || gotStudio[0].ScopeID != studioID {
		t.Fatalf("expected studio hit, got %+v", gotStudio)
	}

	gotDept, err := repo.ListByRequesterFiltered(requesterID.String(), repository.ExpansionListFilter{Keyword: "技术研发"})
	if err != nil {
		t.Fatalf("keyword dept: %v", err)
	}
	if len(gotDept) != 1 || gotDept[0].ScopeID != deptID {
		t.Fatalf("expected dept hit, got %+v", gotDept)
	}

	gotRequester, err := repo.ListByRequesterFiltered(requesterID.String(), repository.ExpansionListFilter{Keyword: "申请人甲"})
	if err != nil {
		t.Fatalf("keyword requester: %v", err)
	}
	if len(gotRequester) != 4 {
		t.Fatalf("expected all 4 rows for requester name, got %d", len(gotRequester))
	}

	// UUID/ID 片段兜底：request_id 与 requester_id 应可被 keyword 命中。
	target := requests[0]
	idFrag := target.ID.String()[:8]
	gotByID, err := repo.ListByRequesterFiltered(requesterID.String(), repository.ExpansionListFilter{Keyword: idFrag})
	if err != nil {
		t.Fatalf("keyword request id fragment: %v", err)
	}
	if len(gotByID) != 1 || gotByID[0].ID != target.ID {
		t.Fatalf("expected request id fragment hit, got %+v", gotByID)
	}

	requesterFrag := requesterID.String()[:8]
	gotByRequesterID, err := repo.ListByRequesterFiltered(requesterID.String(), repository.ExpansionListFilter{Keyword: requesterFrag})
	if err != nil {
		t.Fatalf("keyword requester id fragment: %v", err)
	}
	if len(gotByRequesterID) != 4 {
		t.Fatalf("expected all 4 rows for requester id fragment, got %d", len(gotByRequesterID))
	}
}
