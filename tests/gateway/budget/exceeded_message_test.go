package budget_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExceededMessage(t *testing.T) {
	tests := []struct {
		name string
		c    budget.Constraint
		want string
	}{
		{
			name: "tier monthly with name",
			c: budget.Constraint{
				Dimension: budget.DimensionTier, TierName: "超高级模型",
				PeriodType: budgetdomain.PeriodMonthly,
			},
			want: "超高级模型月预算已用尽，请联系负责人扩容",
		},
		{
			name: "tier daily without name",
			c: budget.Constraint{
				Dimension: budget.DimensionTier, PeriodType: budgetdomain.PeriodDaily,
			},
			want: "该模型等级日预算已用尽，请联系负责人扩容",
		},
		{
			name: "tier weekly static zero with name",
			c: budget.Constraint{
				Dimension: budget.DimensionTier, TierName: "高等级模型",
				PeriodType: budgetdomain.PeriodWeekly, StaticZero: true,
			},
			want: "高等级模型周预算为 0，无法调用，请联系负责人开通",
		},
		{
			name: "tier static zero without name",
			c: budget.Constraint{
				Dimension: budget.DimensionTier, PeriodType: budgetdomain.PeriodMonthly, StaticZero: true,
			},
			want: "该模型等级月预算为 0，无法调用，请联系负责人开通",
		},
		{
			name: "total daily",
			c: budget.Constraint{
				Dimension: budget.DimensionTotal, PeriodType: budgetdomain.PeriodDaily,
			},
			want: "总预算日限额已用尽，请联系负责人扩容",
		},
		{
			name: "total weekly",
			c: budget.Constraint{
				Dimension: budget.DimensionTotal, PeriodType: budgetdomain.PeriodWeekly,
			},
			want: "总预算周限额已用尽，请联系负责人扩容",
		},
		{
			name: "total monthly",
			c: budget.Constraint{
				Dimension: budget.DimensionTotal, PeriodType: budgetdomain.PeriodMonthly,
			},
			want: "总预算月限额已用尽，请联系负责人扩容",
		},
		{
			name: "total static zero",
			c: budget.Constraint{
				Dimension: budget.DimensionTotal, PeriodType: budgetdomain.PeriodDaily, StaticZero: true,
			},
			want: "总预算日限额为 0，无法调用，请联系负责人开通",
		},
		{
			name: "unknown period fallback",
			c: budget.Constraint{
				Dimension: budget.DimensionTotal, PeriodType: budgetdomain.PeriodType("yearly"),
			},
			want: "总预算周期限额已用尽，请联系负责人扩容",
		},
		{
			name: "tier name whitespace treated as empty",
			c: budget.Constraint{
				Dimension: budget.DimensionTier, TierName: "  ",
				PeriodType: budgetdomain.PeriodMonthly,
			},
			want: "该模型等级月预算已用尽，请联系负责人扩容",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, budget.ExceededMessage(tt.c))
		})
	}
}

func TestExceededError_WriteHTTPResponse_UsesChineseMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	err := &budget.ExceededError{
		Code: "tier_budget_exceeded",
		Primary: budget.Constraint{
			Dimension:  budget.DimensionTier,
			TierName:   "超高级模型",
			PeriodType: budgetdomain.PeriodMonthly,
		},
	}
	err.WriteHTTPResponse(c)

	require.Equal(t, http.StatusPaymentRequired, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj := body["error"].(map[string]any)
	assert.Equal(t, "超高级模型月预算已用尽，请联系负责人扩容", errObj["message"])
	assert.Equal(t, "tier_budget_exceeded", errObj["code"])
	assert.Equal(t, "budget", errObj["gate"])
}
