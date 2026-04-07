package budget_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTierResolver_FixesRequestSnapshot(t *testing.T) {
	holder := budget.NewSnapshotHolder()
	tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1,
		Revision:      1,
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
			"claude-opus": {TierID: tierID, TierName: "高级", Enabled: true},
		},
	})
	resolver := budget.NewTierResolver(holder)
	got, ok := resolver.Resolve("claude-opus")
	require.True(t, ok)
	require.Equal(t, tierID.String(), got.TierID)
	require.Equal(t, "claude-opus", got.GroupName)
	require.True(t, got.AccountingEligible)
	require.True(t, got.EnforcementEligible)

	holder.Set(&configsync.BudgetTierSnapshot{SchemaVersion: 1, Revision: 2, GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{}})
	require.Equal(t, tierID.String(), got.TierID)
}

func TestTierResolver_DisabledTierKeepsReportingSnapshot(t *testing.T) {
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1,
		Revision:      3,
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
			"disabled": {
				TierID:   uuid.MustParse("7534a819-ceb7-4ce8-a62f-bf78627eabb7"),
				TierName: "归档等级",
				Enabled:  false,
			},
		},
	})
	got, ok := budget.NewTierResolver(holder).Resolve("disabled")
	require.True(t, ok)
	require.NotEmpty(t, got.TierID)
	require.Equal(t, "归档等级", got.TierName)
	require.False(t, got.AccountingEligible)
	require.False(t, got.EnforcementEligible)

	_, ok = budget.NewTierResolver(holder).Resolve("unclassified")
	require.False(t, ok)
}
