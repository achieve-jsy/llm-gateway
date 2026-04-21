package budgetdomain_test

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAmountJSONUsesSixDecimalString(t *testing.T) {
	var amount budgetdomain.Amount
	require.NoError(t, json.Unmarshal([]byte(`"12.3456784"`), &amount))
	require.Equal(t, "12.345678", amount.String())

	raw, err := json.Marshal(amount)
	require.NoError(t, err)
	require.JSONEq(t, `"12.345678"`, string(raw))
}

func TestAmountRejectsNumberNegativeJSONAndNegativeDatabaseValue(t *testing.T) {
	var amount budgetdomain.Amount
	require.ErrorIs(t, json.Unmarshal([]byte(`12.5`), &amount), budgetdomain.ErrAmountMustBeString)
	require.ErrorIs(t, json.Unmarshal([]byte(`"-0.000001"`), &amount), budgetdomain.ErrAmountNegative)
	require.ErrorIs(t, amount.Scan("-1.000000"), budgetdomain.ErrAmountNegative)
}

func TestParseLimitAmountAllowsUnlimitedAndRejectsFractionalNegative(t *testing.T) {
	unlimited, err := budgetdomain.ParseLimitAmount("-1")
	require.NoError(t, err)
	require.True(t, unlimited.IsUnlimited())
	require.Equal(t, "-1.000000", unlimited.String())

	unlimitedFixed, err := budgetdomain.ParseLimitAmount("-1.000000")
	require.NoError(t, err)
	require.True(t, unlimitedFixed.IsUnlimited())

	zero, err := budgetdomain.ParseLimitAmount("0")
	require.NoError(t, err)
	require.False(t, zero.IsUnlimited())
	require.Equal(t, "0.000000", zero.String())

	_, err = budgetdomain.ParseLimitAmount("-0.5")
	require.ErrorIs(t, err, budgetdomain.ErrAmountInvalidLimit)

	_, err = budgetdomain.ParseLimitAmount("-2")
	require.ErrorIs(t, err, budgetdomain.ErrAmountInvalidLimit)

	// 普通金额解析仍拒绝 -1
	_, err = budgetdomain.ParseAmount("-1")
	require.ErrorIs(t, err, budgetdomain.ErrAmountNegative)
}

func TestDimensionValidationAndKey(t *testing.T) {
	require.NoError(t, budgetdomain.ValidateDimension(budgetdomain.DimensionTotal, nil))
	require.ErrorIs(t, budgetdomain.ValidateDimension(budgetdomain.DimensionTier, nil), budgetdomain.ErrTierIDRequired)

	tierID := uuid.New()
	key, err := budgetdomain.DimensionKey(budgetdomain.DimensionTier, &tierID)
	require.NoError(t, err)
	require.Equal(t, "tier:"+tierID.String(), key)
}

func TestBudgetEnumsRejectUnknownValues(t *testing.T) {
	require.True(t, budgetdomain.PeriodDaily.Valid())
	require.True(t, budgetdomain.PeriodWeekly.Valid())
	require.True(t, budgetdomain.PeriodMonthly.Valid())
	require.False(t, budgetdomain.PeriodType("yearly").Valid())

	require.True(t, budgetdomain.DurationPermanent.Valid())
	require.True(t, budgetdomain.DurationCurrentPeriod.Valid())
	require.False(t, budgetdomain.DurationType("forever").Valid())

	require.True(t, budgetdomain.OverrideActive.Valid())
	require.True(t, budgetdomain.OverrideExpired.Valid())
	require.True(t, budgetdomain.OverrideCancelled.Valid())
	require.True(t, budgetdomain.OverrideFailed.Valid())
	require.False(t, budgetdomain.OverrideStatus("pending").Valid())

	require.True(t, budgetdomain.SchedulePending.Valid())
	require.True(t, budgetdomain.ScheduleApplied.Valid())
	require.True(t, budgetdomain.ScheduleReplaced.Valid())
	require.True(t, budgetdomain.ScheduleCancelled.Valid())
	require.True(t, budgetdomain.ScheduleFailed.Valid())
	require.False(t, budgetdomain.ScheduleStatus("active").Valid())
}
