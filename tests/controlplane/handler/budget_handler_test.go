package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type myBudgetAllocationRepo struct {
	allocations map[string]*model.BudgetAllocation
}

func (r *myBudgetAllocationRepo) UpsertAllocation(*model.BudgetAllocation) error { return nil }
func (r *myBudgetAllocationRepo) GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error) {
	return r.allocations[scopeType+":"+scopeID], nil
}
func (r *myBudgetAllocationRepo) GetConsumption(string, string, string) (*model.BudgetConsumption, error) {
	return nil, nil
}
func (r *myBudgetAllocationRepo) ListScoped([]string, []string) ([]model.BudgetAllocation, error) {
	return nil, nil
}
func (r *myBudgetAllocationRepo) ListMemberIDsByDeptID(string) ([]string, error) {
	return nil, nil
}
func (r *myBudgetAllocationRepo) MemberInDeptIDs(string, []string) (bool, error) {
	return false, nil
}
func (r *myBudgetAllocationRepo) ListDeptIDsByStudioID(string) ([]string, error) {
	return nil, nil
}
func (r *myBudgetAllocationRepo) ListDeptStudioRows() ([]model.DeptStudioRow, error) {
	return nil, nil
}
func (r *myBudgetAllocationRepo) ResolveScopeNames(string, []string) (map[string]string, error) {
	return nil, nil
}
func (r *myBudgetAllocationRepo) DeleteAllocation(string, string) error { return nil }

type myBudgetAlertRepo struct{}

func (myBudgetAlertRepo) DeleteByScope(string, string, string) error { return nil }

func TestMyBudgetHidesUpstreamAmountsForEmployee(t *testing.T) {
	response := requestMyBudget(t, rbac.Actor{UserID: "member-a"})

	member := findBudgetChainItem(t, response, model.BudgetScopeMember)
	require.Equal(t, true, member["visible_amounts"])
	require.Equal(t, float64(100), member["amount_usd"])

	for _, scopeType := range []string{
		model.BudgetScopeDeptDaily,
		model.BudgetScopeStudio,
		model.BudgetScopeCompany,
	} {
		item := findBudgetChainItem(t, response, scopeType)
		require.Equal(t, false, item["visible_amounts"], scopeType)
		for _, key := range []string{
			"amount_usd", "daily_amount_usd", "weekly_amount_usd",
			"used_usd", "daily_used_usd", "weekly_used_usd", "used_ratio",
		} {
			require.NotContains(t, item, key, "%s must not expose %s", scopeType, key)
		}
	}
}

func TestMyBudgetKeepsAmountsVisibleForAuthorizedScopes(t *testing.T) {
	t.Run("global admin sees the full chain", func(t *testing.T) {
		response := requestMyBudget(t, rbac.Actor{UserID: "admin-a", IsGlobal: true})
		for _, scopeType := range []string{
			model.BudgetScopeDeptDaily,
			model.BudgetScopeStudio,
			model.BudgetScopeCompany,
		} {
			item := findBudgetChainItem(t, response, scopeType)
			require.Equal(t, true, item["visible_amounts"], scopeType)
			require.Contains(t, item, "amount_usd", scopeType)
		}
	})

	t.Run("scoped manager sees only managed upstream scopes", func(t *testing.T) {
		response := requestMyBudget(t, rbac.Actor{
			UserID: "manager-a", StudioIDs: []string{"studio-a"}, DeptIDs: []string{"dept-a"},
		})
		for _, scopeType := range []string{model.BudgetScopeDeptDaily, model.BudgetScopeStudio} {
			item := findBudgetChainItem(t, response, scopeType)
			require.Equal(t, true, item["visible_amounts"], scopeType)
			require.Contains(t, item, "amount_usd", scopeType)
		}
		company := findBudgetChainItem(t, response, model.BudgetScopeCompany)
		require.Equal(t, false, company["visible_amounts"])
		require.NotContains(t, company, "amount_usd")
	})
}

func TestSetBudgetNextPeriodRequiresExistingAllocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &myBudgetAllocationRepo{allocations: map[string]*model.BudgetAllocation{}}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&orgmodel.UserRole{}))
	require.NoError(t, db.Create(&orgmodel.UserRole{
		UserID: "admin-a", Role: orgmodel.RoleAdmin, ScopeType: orgmodel.ScopeSelf,
		ScopeID: "", GrantedBy: "test",
	}).Error)

	checker := rbac.NewChecker(orgrepo.NewRoleRepo(db), nil)
	h := handler.NewBudgetHandler(service.NewBudgetService(repo, myBudgetAlertRepo{}), checker)
	router := gin.New()
	router.PUT("/api/v1/budgets", func(c *gin.Context) {
		c.Set("user_id", "admin-a")
		h.Set(c)
	})

	body := `{"scope_type":"member","scope_id":"member-missing","period_type":"monthly","target_limit_usd":"1000.000000","effective_mode":"next_period","reason":"schedule missing","preview_only":true}`
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/budgets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusConflict, recorder.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	errObj, ok := response["error"].(map[string]any)
	require.True(t, ok, "response must carry an error object: %s", recorder.Body.String())
	require.Equal(t, "BUDGET_NEXT_PERIOD_REQUIRES_ALLOCATION", errObj["code"])
}

func requestMyBudget(t *testing.T, actor rbac.Actor) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &myBudgetAllocationRepo{allocations: map[string]*model.BudgetAllocation{
		model.BudgetScopeMember + ":member-a": {
			ScopeType: model.BudgetScopeMember, ScopeID: "member-a",
			AmountUSD: 100, DailyAmountUSD: 10, WeeklyAmountUSD: 50, Enabled: true,
		},
		model.BudgetScopeDeptDaily + ":dept-a": {
			ScopeType: model.BudgetScopeDeptDaily, ScopeID: "dept-a",
			AmountUSD: 200, DailyAmountUSD: 20, WeeklyAmountUSD: 100, Enabled: true,
		},
		model.BudgetScopeStudio + ":studio-a": {
			ScopeType: model.BudgetScopeStudio, ScopeID: "studio-a",
			AmountUSD: 300, DailyAmountUSD: 30, WeeklyAmountUSD: 150, Enabled: true,
		},
		model.BudgetScopeCompany + ":" + model.BudgetScopeGlobal: {
			ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
			AmountUSD: 400, DailyAmountUSD: 40, WeeklyAmountUSD: 200, Enabled: true,
		},
	}}
	h := handler.NewBudgetHandler(service.NewBudgetService(repo, myBudgetAlertRepo{}), nil)
	router := gin.New()
	router.GET("/api/v1/my/budget", func(c *gin.Context) {
		c.Set("user_id", "member-a")
		c.Set("user_dept_id", "dept-a")
		c.Set("user_studio_id", "studio-a")
		rbac.SetActor(c, actor)
		h.MyBudget(c)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/my/budget", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func findBudgetChainItem(t *testing.T, response map[string]any, scopeType string) map[string]any {
	t.Helper()
	chain, ok := response["chain"].([]any)
	require.True(t, ok)
	for _, raw := range chain {
		item, ok := raw.(map[string]any)
		require.True(t, ok)
		if item["scope_type"] == scopeType {
			return item
		}
	}
	t.Fatalf("scope %s not found in budget chain", scopeType)
	return nil
}
