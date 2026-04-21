package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrBudgetConfigSyncPending = errors.New("budget configuration persisted but snapshot synchronization is pending")

type BudgetTierSnapshotSource interface {
	ListBudgetTierGroupSnapshots(context.Context) ([]repository.BudgetTierGroupSnapshotRow, error)
}

type SnapshotEventPublisher interface {
	Publish(context.Context, *event.ChangeEvent) error
}

type BudgetConfigPublisher interface {
	MarkDirtyAndPublish(context.Context) error
}

type BudgetTierSnapshotSettings struct {
	MasterEnabled              bool
	AccountingEnabled          bool
	PositiveEnforcementEnabled bool
	ZeroBlockEnabled           bool
	ShadowEnabled              bool
	V2TotalEnforcementEnabled  bool
	LegacyTotalWriteEnabled    bool
	Timezone                   string
	WeekStart                  string
}

type BudgetTierSnapshotPublisher struct {
	source   BudgetTierSnapshotSource
	rdb      *redis.Client
	events   SnapshotEventPublisher
	settings BudgetTierSnapshotSettings
	clock    func() time.Time
	dirty    atomic.Bool
}

func NewBudgetTierSnapshotPublisher(source BudgetTierSnapshotSource, rdb *redis.Client, events SnapshotEventPublisher, settings BudgetTierSnapshotSettings, clock func() time.Time) *BudgetTierSnapshotPublisher {
	if clock == nil {
		clock = time.Now
	}
	if settings.Timezone == "" {
		settings.Timezone = budgetperiod.DefaultTimezone
	}
	if settings.WeekStart == "" {
		settings.WeekStart = budgetperiod.CanonicalWeekStart
	}
	return &BudgetTierSnapshotPublisher{source: source, rdb: rdb, events: events, settings: settings, clock: clock}
}

func (p *BudgetTierSnapshotPublisher) Publish(ctx context.Context) (snapshot *configsync.BudgetTierSnapshot, err error) {
	defer func() {
		if err != nil {
			recordBudgetConfigSync(ctx, "failed")
			return
		}
		recordBudgetConfigSync(ctx, "success")
	}()
	snapshot, err = p.buildSnapshot(ctx)
	if err != nil {
		p.dirty.Store(true)
		return nil, err
	}
	for {
		err = p.rdb.Watch(ctx, func(tx *redis.Tx) error {
			rawRevision, getErr := tx.Get(ctx, configsync.BudgetTierSnapshotRevisionKey).Result()
			if getErr != nil && getErr != redis.Nil {
				return getErr
			}
			revision, _ := strconv.ParseInt(rawRevision, 10, 64)
			snapshot.Revision = revision + 1
			snapshot.UpdatedAt = p.clock().UTC()
			raw, marshalErr := json.Marshal(snapshot)
			if marshalErr != nil {
				return marshalErr
			}
			_, pipeErr := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, configsync.BudgetTierSnapshotRevisionKey, snapshot.Revision, 0)
				pipe.Set(ctx, configsync.BudgetTierSnapshotKey, raw, 0)
				return nil
			})
			return pipeErr
		}, configsync.BudgetTierSnapshotRevisionKey)
		if err != redis.TxFailedErr {
			break
		}
	}
	if err != nil {
		p.dirty.Store(true)
		return nil, err
	}
	for _, change := range []*event.ChangeEvent{
		event.NewChangeEvent(event.BudgetTierSnapshotUpdated, "budget_tier_snapshot", uuid.Nil, snapshot.Revision),
		event.NewChangeEvent(event.AuthConfigChanged, "auth_config", uuid.Nil, snapshot.Revision),
	} {
		if err := p.events.Publish(ctx, change); err != nil {
			p.dirty.Store(true)
			return snapshot, err
		}
	}
	p.dirty.Store(false)
	return snapshot, nil
}

func (p *BudgetTierSnapshotPublisher) buildSnapshot(ctx context.Context) (*configsync.BudgetTierSnapshot, error) {
	if _, err := time.LoadLocation(p.settings.Timezone); err != nil {
		return nil, fmt.Errorf("invalid budget timezone %q: %w", p.settings.Timezone, err)
	}
	if p.settings.WeekStart != budgetperiod.CanonicalWeekStart {
		return nil, fmt.Errorf("invalid budget week_start %q: only %q is supported", p.settings.WeekStart, budgetperiod.CanonicalWeekStart)
	}
	rows, err := p.source.ListBudgetTierGroupSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1,
		MasterEnabled: p.settings.MasterEnabled, AccountingEnabled: p.settings.AccountingEnabled,
		PositiveEnforcementEnabled: p.settings.PositiveEnforcementEnabled, ZeroBlockEnabled: p.settings.ZeroBlockEnabled,
		ShadowEnabled: p.settings.ShadowEnabled, V2TotalEnforcementEnabled: p.settings.V2TotalEnforcementEnabled,
		LegacyTotalWriteEnabled: p.settings.LegacyTotalWriteEnabled,
		Timezone:                p.settings.Timezone, WeekStart: p.settings.WeekStart,
		GroupTiers: make(map[string]configsync.BudgetTierGroupSnapshot, len(rows)),
	}
	for _, row := range rows {
		snapshot.GroupTiers[row.GroupName] = configsync.BudgetTierGroupSnapshot{TierID: row.TierID, TierName: row.TierName, Enabled: row.TierEnabled}
	}
	return snapshot, nil
}

func (p *BudgetTierSnapshotPublisher) MarkDirtyAndPublish(ctx context.Context) error {
	p.dirty.Store(true)
	_, err := p.Publish(ctx)
	return err
}

func (p *BudgetTierSnapshotPublisher) Dirty() bool { return p.dirty.Load() }

func (p *BudgetTierSnapshotPublisher) Reconcile(ctx context.Context) error {
	desired, err := p.buildSnapshot(ctx)
	if err != nil {
		p.dirty.Store(true)
		return err
	}
	current, err := configsync.LoadBudgetTierSnapshot(ctx, p.rdb)
	if err == nil && stableSnapshotHash(current) == stableSnapshotHash(desired) && !p.dirty.Load() {
		return nil
	}
	_, err = p.Publish(ctx)
	return err
}

func (p *BudgetTierSnapshotPublisher) StartReconcile(ctx context.Context, interval time.Duration) {
	if ctx.Err() != nil {
		return
	}
	_ = p.Reconcile(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.Reconcile(ctx)
		}
	}
}

func stableSnapshotHash(snapshot *configsync.BudgetTierSnapshot) [32]byte {
	if snapshot == nil {
		return sha256.Sum256(nil)
	}
	projection := *snapshot
	projection.Revision = 0
	projection.UpdatedAt = time.Time{}
	raw, _ := json.Marshal(projection)
	return sha256.Sum256(raw)
}
