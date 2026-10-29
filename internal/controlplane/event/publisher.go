package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

const configChangeChannel = "config:change"

// Redis key prefixes for auth caches.
// 必须与 internal/gateway/infra/key_cache.go、internal/auth/service 里的常量保持一致；
// 网关侧前缀带版本号（v2），改版本时这里要同时保留旧前缀，否则吊销会漏删。
const (
	authVerifyCachePrefix = "key:auth:verify:"     // 内嵌 auth service 的 Redis 缓存
	gatewayCachePrefixV1  = "gateway:keycache:"    // 旧版网关 KeyCache（兼容保留）
	gatewayCachePrefixV2  = "gateway:keycache:v2:" // 当前网关 KeyCache
)

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

// InvalidateKeyAuthCache 主动删除 API Key 的所有 auth 缓存条目（不依赖 TTL）。
// keyHash 为 sha256(plaintext_key)，与 api_keys.key_hash 一致。
func (p *Publisher) InvalidateKeyAuthCache(ctx context.Context, keyHash string) {
	if p.rdb == nil || keyHash == "" {
		return
	}
	keys := []string{
		authVerifyCachePrefix + keyHash,
		gatewayCachePrefixV1 + keyHash,
		gatewayCachePrefixV2 + keyHash,
	}
	if err := p.rdb.Del(ctx, keys...).Err(); err != nil {
		slog.Warn("failed to invalidate key auth cache", "key_hash", keyHash[:8]+"...", "error", err)
	}
}
