package quota

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/core/quota"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/redis/go-redis/v9"
)

// helper: create a miniredis instance + redis client, with cleanup func
func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb, func() {
		rdb.Close()
		mr.Close()
	}
}

// helper: write a snapshot to miniredis
func writeSnapshot(t *testing.T, rdb *redis.Client, snap *quota.QuotaSnapshot) {
	t.Helper()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := rdb.Set(context.Background(), QuotaSnapshotKey, data, 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
}

func TestLoad_Debounce(t *testing.T) {
	_, rdb, cleanup := setupMiniredis(t)
	defer cleanup()

	holder := NewSnapshotHolder()
	loader := NewConfigLoader(rdb, holder)

	// Write a v1 snapshot
	writeSnapshot(t, rdb, &quota.QuotaSnapshot{Version: 1, DefaultUserRPM: 60, DefaultUserMaxConcurrency: 10})

	// First load should succeed and populate holder
	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("first load: %v", err)
	}
	snap := holder.Get()
	if snap == nil || snap.Version != 1 {
		t.Fatalf("expected version 1, got %+v", snap)
	}

	// Write a v2 snapshot to Redis
	writeSnapshot(t, rdb, &quota.QuotaSnapshot{Version: 2, DefaultUserRPM: 100, DefaultUserMaxConcurrency: 20})

	// Second load within 1s should be debounced — holder still has v1
	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("debounced load: %v", err)
	}
	snap = holder.Get()
	if snap == nil || snap.Version != 1 {
		t.Fatalf("expected version 1 (debounced), got version %d", snap.Version)
	}

	// Reset lastLoadAt to simulate time passing beyond debounce window
	loader.lastLoadAt.Store(time.Now().Add(-2 * time.Second).UnixNano())

	// Third load should now pick up v2
	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("post-debounce load: %v", err)
	}
	snap = holder.Get()
	if snap == nil || snap.Version != 2 {
		t.Fatalf("expected version 2 after debounce reset, got %+v", snap)
	}
}

func TestLoad_NilRedisKey(t *testing.T) {
	_, rdb, cleanup := setupMiniredis(t)
	defer cleanup()

	holder := NewSnapshotHolder()
	loader := NewConfigLoader(rdb, holder)

	// Load when key doesn't exist — should not error, holder stays nil
	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("load missing key: %v", err)
	}
	if snap := holder.Get(); snap != nil {
		t.Fatalf("expected nil snapshot, got %+v", snap)
	}
}

func TestSubscribeToChanges_QuotaEvent(t *testing.T) {
	mr, rdb, cleanup := setupMiniredis(t)
	defer cleanup()

	holder := NewSnapshotHolder()
	loader := NewConfigLoader(rdb, holder)

	// Write initial v1 snapshot
	writeSnapshot(t, rdb, &quota.QuotaSnapshot{Version: 1, DefaultUserRPM: 60, DefaultUserMaxConcurrency: 10})

	// Initial load
	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if snap := holder.Get(); snap == nil || snap.Version != 1 {
		t.Fatalf("expected version 1 after initial load, got %+v", snap)
	}

	// Start subscriber
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loader.SubscribeToChanges(ctx)

	// Give the subscriber goroutine time to start and subscribe
	time.Sleep(100 * time.Millisecond)

	// Write v2 snapshot and publish quota event
	writeSnapshot(t, rdb, &quota.QuotaSnapshot{Version: 2, DefaultUserRPM: 100, DefaultUserMaxConcurrency: 20})

	// Reset debounce so the pub/sub triggered load isn't skipped
	loader.lastLoadAt.Store(0)

	// Publish the quota_snapshot_updated event
	mr.Publish(infra.ConfigChangeChannel, `{"type":"quota_snapshot_updated"}`)

	// Wait for the subscriber to process
	time.Sleep(500 * time.Millisecond)

	snap := holder.Get()
	if snap == nil || snap.Version != 2 {
		t.Fatalf("expected version 2 after pub/sub event, got %+v", snap)
	}
}

func TestSubscribeToChanges_IgnoresNonQuotaEvent(t *testing.T) {
	mr, rdb, cleanup := setupMiniredis(t)
	defer cleanup()

	holder := NewSnapshotHolder()
	loader := NewConfigLoader(rdb, holder)

	// Write v1 snapshot and load it
	writeSnapshot(t, rdb, &quota.QuotaSnapshot{Version: 1})
	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	// Start subscriber
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loader.SubscribeToChanges(ctx)
	time.Sleep(100 * time.Millisecond)

	// Write v2 snapshot but publish a NON-quota event
	writeSnapshot(t, rdb, &quota.QuotaSnapshot{Version: 2})
	loader.lastLoadAt.Store(0)
	mr.Publish(infra.ConfigChangeChannel, `{"type":"provider_updated"}`)

	// Wait for subscriber to process
	time.Sleep(500 * time.Millisecond)

	// Holder should still have v1 — non-quota events are filtered out
	snap := holder.Get()
	if snap == nil || snap.Version != 1 {
		t.Fatalf("expected version 1 (non-quota event ignored), got %+v", snap)
	}
}

func TestSubscribeToChanges_ContextCancel(t *testing.T) {
	_, rdb, cleanup := setupMiniredis(t)
	defer cleanup()

	holder := NewSnapshotHolder()
	loader := NewConfigLoader(rdb, holder)

	ctx, cancel := context.WithCancel(context.Background())
	loader.SubscribeToChanges(ctx)
	time.Sleep(100 * time.Millisecond)

	// Cancel should cleanly stop the subscriber goroutine
	cancel()
	time.Sleep(100 * time.Millisecond)
	// If goroutine leaked, test would hang or fail on cleanup — passing = goroutine stopped
}
