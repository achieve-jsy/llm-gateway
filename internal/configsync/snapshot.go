package configsync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/redis/go-redis/v9"
)

const (
	// ProviderSnapshotKey stores the latest available provider/model snapshot.
	ProviderSnapshotKey = "gateway:providers:snapshot:v1"
)

// ProviderSnapshot is the shared Redis snapshot consumed by gateway instances.
type ProviderSnapshot struct {
	Version   int64                        `json:"version"`
	UpdatedAt time.Time                    `json:"updated_at"`
	Entries   []gatewaymodel.ProviderEntry `json:"entries"`
}

// LoadProviderSnapshot reads the shared provider snapshot from Redis.
func LoadProviderSnapshot(ctx context.Context, rdb *redis.Client) (*ProviderSnapshot, error) {
	if rdb == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	raw, err := rdb.Get(ctx, ProviderSnapshotKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, err
		}
		return nil, fmt.Errorf("get provider snapshot: %w", err)
	}

	var snapshot ProviderSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal provider snapshot: %w", err)
	}

	return &snapshot, nil
}

// SaveProviderSnapshot writes the latest available provider snapshot to Redis.
func SaveProviderSnapshot(ctx context.Context, rdb *redis.Client, snapshot *ProviderSnapshot) error {
	if rdb == nil {
		return fmt.Errorf("redis client is nil")
	}
	if snapshot == nil {
		return fmt.Errorf("provider snapshot is nil")
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal provider snapshot: %w", err)
	}

	if err := rdb.Set(ctx, ProviderSnapshotKey, payload, 0).Err(); err != nil {
		return fmt.Errorf("set provider snapshot: %w", err)
	}

	return nil
}
