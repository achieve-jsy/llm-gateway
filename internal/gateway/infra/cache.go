package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/redis/go-redis/v9"
)

// Cache Redis 缓存消费封装
type Cache struct {
	rdb *redis.Client
}

type modelListCacheEnvelope struct {
	Version int64            `json:"version"`
	Data    []model.ModelInfo `json:"data"`
}

// NewCache 创建 Cache 实例
func NewCache(rdb *redis.Client) *Cache {
	return &Cache{rdb: rdb}
}

// Client exposes the underlying Redis client for shared health/readiness checks.
func (c *Cache) Client() *redis.Client {
	return c.rdb
}

const (
	modelListCacheKey    = "gateway:models:list"
	modelListCacheTTL    = 60 * time.Second
	apiKeySetKey         = "gateway:apikeys"
	modelAvailKeyPrefix  = "model:available:"
	modelAvailTTL        = 300 * time.Second
)

// GetModelList 获取模型列表缓存
func (c *Cache) GetModelList(ctx context.Context) ([]model.ModelInfo, error) {
	models, _, err := c.GetModelListWithVersion(ctx)
	return models, err
}

// GetModelListWithVersion 获取模型列表缓存及其绑定的 snapshot version。
func (c *Cache) GetModelListWithVersion(ctx context.Context) ([]model.ModelInfo, int64, error) {
	data, err := c.rdb.Get(ctx, modelListCacheKey).Bytes()
	if err == redis.Nil {
		return nil, 0, nil // cache miss
	}
	if err != nil {
		return nil, 0, fmt.Errorf("get model list cache: %w", err)
	}

	var envelope modelListCacheEnvelope
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Data != nil {
		return envelope.Data, envelope.Version, nil
	}

	var models []model.ModelInfo
	if err := json.Unmarshal(data, &models); err != nil {
		return nil, 0, fmt.Errorf("unmarshal model list: %w", err)
	}
	return models, 0, nil
}

// SetModelList 设置模型列表缓存
func (c *Cache) SetModelList(ctx context.Context, models []model.ModelInfo) error {
	return c.SetModelListWithVersion(ctx, models, 0)
}

// SetModelListWithVersion 设置模型列表缓存，并绑定 provider snapshot version。
func (c *Cache) SetModelListWithVersion(ctx context.Context, models []model.ModelInfo, version int64) error {
	data, err := json.Marshal(modelListCacheEnvelope{Version: version, Data: models})
	if err != nil {
		return fmt.Errorf("marshal model list: %w", err)
	}
	return c.rdb.Set(ctx, modelListCacheKey, data, modelListCacheTTL).Err()
}

// IsAPIKeyValid 校验 API Key 是否在有效集合中
func (c *Cache) IsAPIKeyValid(ctx context.Context, key string) (bool, error) {
	return c.rdb.SIsMember(ctx, apiKeySetKey, key).Result()
}

// IsModelAvailable 检查模型可用性标记
func (c *Cache) IsModelAvailable(ctx context.Context, modelID string) (bool, error) {
	val, err := c.rdb.Get(ctx, modelAvailKeyPrefix+modelID).Result()
	if err == redis.Nil {
		return true, nil // 未标记则默认可用（fail-open）
	}
	if err != nil {
		return true, nil // Redis 不可达时 fail-open
	}
	return val == "1", nil
}

// SetModelAvailable 设置模型可用性标记
func (c *Cache) SetModelAvailable(ctx context.Context, modelID string, available bool) error {
	val := "0"
	if available {
		val = "1"
	}
	return c.rdb.Set(ctx, modelAvailKeyPrefix+modelID, val, modelAvailTTL).Err()
}
