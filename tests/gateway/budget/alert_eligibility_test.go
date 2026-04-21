package budget_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/stretchr/testify/require"
)

func TestShouldEmitThresholdAlerts_RequiresSuccessfulNonFailOpenRedisCheck(t *testing.T) {
	require.False(t, budget.ShouldEmitThresholdAlerts(&budget.AccountingPlan{
		RedisCheckSucceeded: true, AdmissionInfrastructureFailOpen: true,
	}))
	require.False(t, budget.ShouldEmitThresholdAlerts(&budget.AccountingPlan{
		RedisCheckSucceeded: false,
	}))
	require.True(t, budget.ShouldEmitThresholdAlerts(&budget.AccountingPlan{
		RedisCheckSucceeded: true,
	}))
}
