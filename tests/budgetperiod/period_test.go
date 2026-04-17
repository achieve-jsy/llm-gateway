package budgetperiod_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/stretchr/testify/require"
)

func TestResolveUsesShanghaiBoundaries(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	config := budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"}

	daily, err := budgetperiod.Resolve(time.Date(2026, 7, 11, 16, 30, 0, 0, time.UTC), budgetdomain.PeriodDaily, config)
	require.NoError(t, err)
	require.Equal(t, "2026-07-12", daily.Key)
	require.Equal(t, time.Date(2026, 7, 13, 0, 0, 0, 0, location), daily.ResetAt)

	weekly, err := budgetperiod.Resolve(time.Date(2026, 7, 12, 16, 30, 0, 0, time.UTC), budgetdomain.PeriodWeekly, config)
	require.NoError(t, err)
	require.Equal(t, "2026-W29", weekly.Key)
	require.Equal(t, time.Date(2026, 7, 20, 0, 0, 0, 0, location), weekly.ResetAt)

	monthly, err := budgetperiod.Resolve(time.Date(2026, 7, 31, 16, 30, 0, 0, time.UTC), budgetdomain.PeriodMonthly, config)
	require.NoError(t, err)
	require.Equal(t, "2026-08", monthly.Key)
	require.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, location), monthly.ResetAt)
}

func TestResolveRejectsUnsupportedConfigurationAndPeriod(t *testing.T) {
	_, err := budgetperiod.Resolve(time.Now(), budgetdomain.PeriodDaily, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "sunday",
	})
	require.Error(t, err)

	_, err = budgetperiod.Resolve(time.Now(), budgetdomain.PeriodType("yearly"), budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	require.Error(t, err)
}
