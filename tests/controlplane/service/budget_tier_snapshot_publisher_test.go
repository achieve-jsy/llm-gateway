package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type snapshotSource struct {
	rows []repository.BudgetTierGroupSnapshotRow
}

func TestBudgetTierSnapshotReconcileRepairsCleanRedisDrift(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	events := &snapshotEvents{}
	source := snapshotSource{rows: []repository.BudgetTierGroupSnapshotRow{{
		GroupName: "gpt-5", TierID: uuid.New(), TierName: "高级", TierEnabled: true,
	}}}
	p := service.NewBudgetTierSnapshotPublisher(source, rdb, events, service.BudgetTierSnapshotSettings{}, time.Now)
	first, err := p.Publish(context.Background())
	require.NoError(t, err)
	drifted := *first
	drifted.GroupTiers = map[string]configsync.BudgetTierGroupSnapshot{}
	raw, err := json.Marshal(&drifted)
	require.NoError(t, err)
	require.NoError(t, rdb.Set(context.Background(), configsync.BudgetTierSnapshotKey, raw, 0).Err())
	require.NoError(t, p.Reconcile(context.Background()))
	repaired, err := configsync.LoadBudgetTierSnapshot(context.Background(), rdb)
	require.NoError(t, err)
	require.Len(t, repaired.GroupTiers, 1)
	require.Equal(t, int64(2), repaired.Revision)
}

func (s snapshotSource) ListBudgetTierGroupSnapshots(context.Context) ([]repository.BudgetTierGroupSnapshotRow, error) {
	return s.rows, nil
}

type snapshotEvents struct{ events []*event.ChangeEvent }

func (p *snapshotEvents) Publish(_ context.Context, value *event.ChangeEvent) error {
	p.events = append(p.events, value)
	return nil
}

func TestBudgetTierSnapshotPublisherWritesCompleteMonotonicSnapshot(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	events := &snapshotEvents{}
	source := snapshotSource{rows: []repository.BudgetTierGroupSnapshotRow{
		{GroupName: "claude-opus", TierID: uuid.New(), TierName: "高级", TierEnabled: true},
		{GroupName: "gpt-5", TierID: uuid.New(), TierName: "高级", TierEnabled: true},
	}}
	p := service.NewBudgetTierSnapshotPublisher(source, rdb, events, service.BudgetTierSnapshotSettings{}, time.Now)
	first, err := p.Publish(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Revision)
	require.Len(t, first.GroupTiers, 2)
	second, err := p.Publish(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Revision)
	require.Equal(t, event.BudgetTierSnapshotUpdated, events.events[0].Type)
	require.Equal(t, event.AuthConfigChanged, events.events[1].Type)
}

func TestBudgetTierSnapshotRejectsInvalidPeriodSettingsWithoutReplacingLastGood(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	source := snapshotSource{rows: []repository.BudgetTierGroupSnapshotRow{{
		GroupName: "gpt-5", TierID: uuid.New(), TierName: "高级", TierEnabled: true,
	}}}
	valid := service.NewBudgetTierSnapshotPublisher(source, rdb, &snapshotEvents{}, service.BudgetTierSnapshotSettings{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}, time.Now)
	lastGood, err := valid.Publish(context.Background())
	require.NoError(t, err)

	for _, settings := range []service.BudgetTierSnapshotSettings{
		{Timezone: "Mars/Olympus", WeekStart: "monday"},
		{Timezone: "Asia/Shanghai", WeekStart: "sunday"},
	} {
		invalid := service.NewBudgetTierSnapshotPublisher(source, rdb, &snapshotEvents{}, settings, time.Now)
		_, err = invalid.Publish(context.Background())
		require.Error(t, err)
		current, loadErr := configsync.LoadBudgetTierSnapshot(context.Background(), rdb)
		require.NoError(t, loadErr)
		require.Equal(t, lastGood.Revision, current.Revision)
		require.Equal(t, lastGood.Timezone, current.Timezone)
		require.Equal(t, lastGood.WeekStart, current.WeekStart)
	}
}

func TestBudgetTierSnapshotReconcileStopsWhenLifecycleContextIsCancelled(t *testing.T) {
	mr := miniredis.RunT(t)
	publisher := service.NewBudgetTierSnapshotPublisher(
		snapshotSource{}, redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		&snapshotEvents{}, service.BudgetTierSnapshotSettings{}, time.Now,
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		publisher.StartReconcile(ctx, time.Hour)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("snapshot reconcile did not stop after context cancellation")
	}
}
