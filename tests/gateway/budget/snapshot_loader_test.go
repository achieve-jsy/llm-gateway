package budget_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const validSnapshot = `{"schema_version":1,"revision":7,"updated_at":"2026-07-11T08:00:00Z","master_enabled":true,"accounting_enabled":true,"positive_enforcement_enabled":false,"zero_block_enabled":true,"shadow_enabled":true,"v2_total_enforcement_enabled":false,"legacy_total_write_enabled":true,"timezone":"Asia/Shanghai","week_start":"monday","group_tiers":{}}`

func TestConfigLoader_InvalidSnapshotKeepsLastKnownGood(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	loader := budget.NewConfigLoader(rdb, holder)

	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, validSnapshot))
	require.NoError(t, loader.Load(context.Background()))
	require.Equal(t, int64(7), holder.Get().Revision)

	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, `{"revision":`))
	require.Error(t, loader.Load(context.Background()))
	require.Equal(t, int64(7), holder.Get().Revision)
}

func TestSnapshotHolder_RejectsRevisionRegression(t *testing.T) {
	holder := budget.NewSnapshotHolder()
	require.True(t, holder.Set(&configsync.BudgetTierSnapshot{SchemaVersion: 1, Revision: 8}))
	require.False(t, holder.Set(&configsync.BudgetTierSnapshot{SchemaVersion: 1, Revision: 7}))
	require.Equal(t, int64(8), holder.Get().Revision)
}

func TestConfigLoader_HandlesStructuredChangeEvent(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	loader := budget.NewConfigLoader(rdb, holder)
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, validSnapshot))
	require.NoError(t, loader.Load(context.Background()))

	next := `{"schema_version":1,"revision":8,"updated_at":"2026-07-11T08:01:00Z","master_enabled":true,"accounting_enabled":true,"positive_enforcement_enabled":false,"zero_block_enabled":true,"shadow_enabled":true,"v2_total_enforcement_enabled":false,"legacy_total_write_enabled":true,"timezone":"Asia/Shanghai","week_start":"monday","group_tiers":{}}`
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, next))
	raw := `{"type":"budget_tier_snapshot_updated","resource_type":"budget_tier_snapshot","resource_id":"global","snapshot_version":8,"timestamp":"2026-07-11T08:01:00Z"}`
	var event infra.ChangeEvent
	require.NoError(t, json.Unmarshal([]byte(raw), &event))
	loader.HandleChangeEvent(event)
	require.Eventually(t, func() bool {
		return holder.Get() != nil && holder.Get().Revision == 8
	}, time.Second, 10*time.Millisecond)
}

func TestConfigLoader_RejectsContradictorySwitches(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	loader := budget.NewConfigLoader(rdb, holder)
	invalid := `{"schema_version":1,"revision":9,"master_enabled":true,"accounting_enabled":true,"positive_enforcement_enabled":true,"shadow_enabled":true,"timezone":"Asia/Shanghai","week_start":"monday","group_tiers":{}}`
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, invalid))
	require.Error(t, loader.Load(context.Background()))
	require.Nil(t, holder.Get())
}

func TestConfigLoader_RedisFailureKeepsLastKnownGood(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	loader := budget.NewConfigLoader(rdb, holder)
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, validSnapshot))
	require.NoError(t, loader.Load(context.Background()))
	mr.Close()

	require.Error(t, loader.Load(context.Background()))
	require.NotNil(t, holder.Get())
	require.Equal(t, int64(7), holder.Get().Revision)
}

func TestConfigLoader_StartReconcilesWithoutChangeEvent(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	loader := budget.NewConfigLoader(rdb, holder)
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, validSnapshot))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loader.Start(ctx, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		return holder.Get() != nil && holder.Get().Revision == 7
	}, time.Second, 10*time.Millisecond)

	next := `{"schema_version":1,"revision":8,"updated_at":"2026-07-11T08:01:00Z","master_enabled":true,"accounting_enabled":true,"positive_enforcement_enabled":false,"zero_block_enabled":true,"shadow_enabled":true,"v2_total_enforcement_enabled":false,"legacy_total_write_enabled":true,"timezone":"Asia/Shanghai","week_start":"monday","group_tiers":{}}`
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, next))
	require.Eventually(t, func() bool {
		return holder.Get() != nil && holder.Get().Revision == 8
	}, time.Second, 10*time.Millisecond)
}
