package configsync

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	BudgetTierSnapshotKey         = "budget:v2:tier:snapshot"
	BudgetTierSnapshotRevisionKey = "budget:v2:tier:snapshot:revision"
	BudgetTierSnapshotUpdated     = "budget_tier_snapshot_updated"
)

type BudgetTierSnapshot struct {
	SchemaVersion              int                                `json:"schema_version"`
	Revision                   int64                              `json:"revision"`
	UpdatedAt                  time.Time                          `json:"updated_at"`
	MasterEnabled              bool                               `json:"master_enabled"`
	AccountingEnabled          bool                               `json:"accounting_enabled"`
	PositiveEnforcementEnabled bool                               `json:"positive_enforcement_enabled"`
	ZeroBlockEnabled           bool                               `json:"zero_block_enabled"`
	ShadowEnabled              bool                               `json:"shadow_enabled"`
	V2TotalEnforcementEnabled  bool                               `json:"v2_total_enforcement_enabled"`
	LegacyTotalWriteEnabled    bool                               `json:"legacy_total_write_enabled"`
	Timezone                   string                             `json:"timezone"`
	WeekStart                  string                             `json:"week_start"`
	GroupTiers                 map[string]BudgetTierGroupSnapshot `json:"group_tiers"`
}

type BudgetTierGroupSnapshot struct {
	TierID   uuid.UUID `json:"tier_id"`
	TierName string    `json:"tier_name"`
	Enabled  bool      `json:"enabled"`
}

func SaveBudgetTierSnapshot(ctx context.Context, client redis.Cmdable, snapshot *BudgetTierSnapshot) error {
	if snapshot == nil {
		return errors.New("budget tier snapshot is nil")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return client.Set(ctx, BudgetTierSnapshotKey, raw, 0).Err()
}

func LoadBudgetTierSnapshot(ctx context.Context, client redis.Cmdable) (*BudgetTierSnapshot, error) {
	raw, err := client.Get(ctx, BudgetTierSnapshotKey).Bytes()
	if err != nil {
		return nil, err
	}
	var snapshot BudgetTierSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
