package config_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/config"
	"github.com/stretchr/testify/require"
)

func TestBudgetTimezoneUsesCanonicalBudgetNamespace(t *testing.T) {
	t.Setenv("APP_ENV", "nonexistent-test-config")
	t.Setenv("BUDGET_TIMEZONE", "UTC")
	t.Setenv("BUDGET_WEEK_START", "monday")
	t.Setenv("BUDGET_TIER_TIMEZONE", "America/Los_Angeles")

	cfg := config.Load()
	require.Equal(t, "UTC", cfg.Budget.Timezone)
	require.Equal(t, "monday", cfg.Budget.WeekStart)
}
