package configsync_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBudgetTierSnapshotUsesCanonicalFlags(t *testing.T) {
	raw, err := json.Marshal(configsync.BudgetTierSnapshot{
		SchemaVersion:     1,
		Revision:          42,
		MasterEnabled:     true,
		AccountingEnabled: true,
		ShadowEnabled:     true,
		Timezone:          "Asia/Shanghai",
		WeekStart:         "monday",
		GroupTiers:        map[string]configsync.BudgetTierGroupSnapshot{},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
	  "schema_version":1,"revision":42,"updated_at":"0001-01-01T00:00:00Z",
	  "master_enabled":true,"accounting_enabled":true,
	  "positive_enforcement_enabled":false,"zero_block_enabled":false,
	  "shadow_enabled":true,"v2_total_enforcement_enabled":false,
	  "legacy_total_write_enabled":false,"timezone":"Asia/Shanghai",
	  "week_start":"monday","group_tiers":{}
	}`, string(raw))
}

func TestBudgetTierSnapshotSaveAndLoad(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	want := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1,
		Revision:      7,
		Timezone:      "Asia/Shanghai",
		WeekStart:     "monday",
		GroupTiers:    map[string]configsync.BudgetTierGroupSnapshot{},
	}
	require.NoError(t, configsync.SaveBudgetTierSnapshot(context.Background(), client, want))
	got, err := configsync.LoadBudgetTierSnapshot(context.Background(), client)
	require.NoError(t, err)
	require.Equal(t, want, got)
}
