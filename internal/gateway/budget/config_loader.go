package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/redis/go-redis/v9"
)

type ConfigLoader struct {
	rdb    *redis.Client
	holder *SnapshotHolder
}

func NewConfigLoader(rdb *redis.Client, holder *SnapshotHolder) *ConfigLoader {
	return &ConfigLoader{rdb: rdb, holder: holder}
}

func (l *ConfigLoader) Load(ctx context.Context) error {
	if l == nil || l.rdb == nil || l.holder == nil {
		return fmt.Errorf("budget tier config loader is not initialized")
	}
	raw, err := l.rdb.Get(ctx, configsync.BudgetTierSnapshotKey).Bytes()
	if err != nil {
		return fmt.Errorf("load budget tier snapshot: %w", err)
	}
	var snapshot configsync.BudgetTierSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return fmt.Errorf("decode budget tier snapshot: %w", err)
	}
	if err := validateSnapshot(&snapshot); err != nil {
		return err
	}
	if !l.holder.Set(&snapshot) {
		return fmt.Errorf("budget tier snapshot revision regressed: %d", snapshot.Revision)
	}
	return nil
}

func validateSnapshot(snapshot *configsync.BudgetTierSnapshot) error {
	if snapshot == nil || snapshot.SchemaVersion != 1 {
		return fmt.Errorf("unsupported budget tier snapshot schema")
	}
	if err := budgetperiod.ValidateConfig(budgetperiod.Config{
		Timezone: snapshot.Timezone, WeekStart: snapshot.WeekStart,
	}); err != nil {
		return fmt.Errorf("invalid budget tier period config: %w", err)
	}
	if snapshot.Timezone != budgetperiod.DefaultTimezone {
		return fmt.Errorf("budget tier timezone must be %s", budgetperiod.DefaultTimezone)
	}
	if snapshot.ShadowEnabled && snapshot.PositiveEnforcementEnabled {
		return fmt.Errorf("shadow and positive enforcement cannot both be enabled")
	}
	if snapshot.PositiveEnforcementEnabled && !snapshot.AccountingEnabled {
		return fmt.Errorf("positive enforcement requires accounting")
	}
	return nil
}

func (l *ConfigLoader) Start(ctx context.Context, reconcileEvery time.Duration) {
	if reconcileEvery <= 0 {
		reconcileEvery = 30 * time.Second
	}
	if err := l.Load(ctx); err != nil {
		slog.WarnContext(ctx, "initial budget tier snapshot load failed", "error", err)
	}
	ticker := time.NewTicker(reconcileEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.Load(ctx); err != nil {
				slog.WarnContext(ctx, "budget tier snapshot reconcile failed", "error", err)
			}
		}
	}
}

func (l *ConfigLoader) HandleChangeEvent(event infra.ChangeEvent) {
	if event.Type != configsync.BudgetTierSnapshotUpdated {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.Load(ctx); err != nil {
		slog.Error("reload budget tier snapshot failed",
			"snapshot_version", event.SnapshotVersion,
			"error", err,
		)
	}
}
