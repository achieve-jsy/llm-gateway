package repository_test

import (
	"context"
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
			current_amount REAL,
			requested_amount REAL NOT NULL,
			reason TEXT,
			status TEXT NOT NULL,
			current_level TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create expansion_requests: %v", err)
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
	return repository.NewExpansionRepo(db)
}

func TestCreateRequestForStudioBudgetGoesToPlatformLevel(t *testing.T) {
	repo := newExpansionTestRepo(t)
	svc := service.NewExpansionService(repo)

	req, err := svc.CreateRequest(context.Background(), model.BudgetScopeStudio, "studio-a", uuid.New(), 1000, 2000, "workspace needs more budget")
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

	req, err := svc.CreateRequest(context.Background(), model.BudgetScopeMember, memberID.String(), memberID, 10, 50, "need more personal quota")
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

	got, err := repo.ListPendingForApproverScoped(model.ExpansionLevelStudio, []string{"dept-a"}, nil, nil)
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

	got, err := repo.ListPendingForApproverScoped(model.ExpansionLevelDept, nil, nil, []string{"member-a"})
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
