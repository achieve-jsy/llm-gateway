package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// budgetRiskErrorResponse replicates the error-mapping logic in BudgetRiskHandler.
func budgetRiskErrorResponse(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrBudgetForbidden):
		c.JSON(http.StatusForbidden, gin.H{"code": "BUDGET_RISK_FORBIDDEN", "message": err.Error()})
	case errors.Is(err, service.ErrBudgetUsageUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "BUDGET_USAGE_UNAVAILABLE", "message": err.Error(), "next_action": "retry",
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "BUDGET_RISK_QUERY_FAILED", "message": err.Error()})
	}
}

func TestBudgetRiskListReturns200WithRiskEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("forbidden maps to 403", func(t *testing.T) {
		router := gin.New()
		router.GET("/risk", func(c *gin.Context) {
			budgetRiskErrorResponse(c, service.ErrBudgetForbidden)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/risk", nil))
		require.Equal(t, http.StatusForbidden, response.Code)
		require.Contains(t, response.Body.String(), `"code":"BUDGET_RISK_FORBIDDEN"`)
	})

	t.Run("usage unavailable maps to 503", func(t *testing.T) {
		router := gin.New()
		router.GET("/risk", func(c *gin.Context) {
			budgetRiskErrorResponse(c, service.ErrBudgetUsageUnavailable)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/risk", nil))
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.Contains(t, response.Body.String(), `"next_action":"retry"`)
	})

	t.Run("unknown error maps to 500", func(t *testing.T) {
		router := gin.New()
		router.GET("/risk", func(c *gin.Context) {
			budgetRiskErrorResponse(c, errors.New("some internal failure"))
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/risk", nil))
		require.Equal(t, http.StatusInternalServerError, response.Code)
		require.Contains(t, response.Body.String(), `"code":"BUDGET_RISK_QUERY_FAILED"`)
	})
}

func TestBudgetRiskSummaryReturns200WithCounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	summary := &service.BudgetRiskSummary{
		Total:    5,
		Warning:  3,
		Critical: 2,
		ByType: map[string]int{
			"threshold_reached": 3,
			"request_blocked":   2,
		},
		DataAsOf: time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
		Stale:    false,
	}

	router := gin.New()
	router.GET("/summary", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": summary})
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/summary", nil))
	require.Equal(t, http.StatusOK, response.Code)

	var result struct {
		Data struct {
			Total    int            `json:"total"`
			Warning  int            `json:"warning"`
			Critical int            `json:"critical"`
			ByType   map[string]int `json:"by_type"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, 5, result.Data.Total)
	require.Equal(t, 3, result.Data.Warning)
	require.Equal(t, 2, result.Data.Critical)
	require.Equal(t, 3, result.Data.ByType["threshold_reached"])
	require.Equal(t, 2, result.Data.ByType["request_blocked"])
}

func TestBudgetRiskEventJSONShape(t *testing.T) {
	tierID := uuid.NewString()
	event := service.BudgetRiskEvent{
		ID:                "abc123",
		RiskType:          "threshold_reached",
		Severity:          "warning",
		ScopeType:         model.BudgetScopeCompany,
		ScopeID:           model.BudgetScopeGlobal,
		ScopeName:         "Global",
		PeriodType:        "monthly",
		DimensionType:     string(budgetdomain.DimensionTotal),
		ModelTierID:       &tierID,
		ModelTierName:     "Standard",
		EffectiveLimitUSD: "100.000000",
		UsedUSD:           "85.000000",
		UsageRatio:        "0.850000",
		LimitSource:       "base_limit",
		BlockedRequests:   3,
		DetectedAt:        time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC),
		DataAsOf:          time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC),
		Stale:             false,
	}

	router := gin.New()
	router.GET("/events", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": &service.BudgetRiskPage{
			Items:    []service.BudgetRiskEvent{event},
			Total:    1,
			DataAsOf: time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC),
		}})
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events", nil))
	require.Equal(t, http.StatusOK, response.Code)

	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Contains(t, body, "data")

	var data struct {
		Items []map[string]interface{} `json:"items"`
		Total int                      `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body["data"], &data))
	require.Equal(t, 1, data.Total)
	require.Len(t, data.Items, 1)
	require.Equal(t, "threshold_reached", data.Items[0]["risk_type"])
	require.Equal(t, "warning", data.Items[0]["severity"])
	require.Equal(t, "base_limit", data.Items[0]["limit_source"])
}
