package event

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const configChangeChannel = "config:change"

// Publisher Redis 变更事件发布器
type Publisher struct {
	rdb *redis.Client
}

// NewPublisher 创建发布器实例
func NewPublisher(rdb *redis.Client) *Publisher {
	return &Publisher{rdb: rdb}
}

// Publish 发布变更事件到 Redis pub/sub
func (p *Publisher) Publish(ctx context.Context, event *ChangeEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	if err := p.rdb.Publish(ctx, configChangeChannel, string(data)).Err(); err != nil {
		return fmt.Errorf("publish to %s: %w", configChangeChannel, err)
	}

	return nil
}
