package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// StickyStore manages session-to-model sticky mappings in Redis
type StickyStore struct {
	client *redis.Client
	prefix string
}

// NewStickyStore creates a sticky store with the given Redis client
// client should be *redis.Client, passed as interface{} to avoid import cycle
func NewStickyStore(client interface{}) *StickyStore {
	rdb, ok := client.(*redis.Client)
	if !ok {
		// Return a no-op store if cast fails
		return &StickyStore{client: nil, prefix: "llm:sticky:"}
	}
	return &StickyStore{
		client: rdb,
		prefix: "llm:sticky:",
	}
}

// Get retrieves the pinned model ID for a session, returns "" if not found
func (s *StickyStore) Get(ctx context.Context, groupName, sessionKey string) (string, error) {
	if s.client == nil || sessionKey == "" {
		return "", nil
	}

	key := s.buildKey(groupName, sessionKey)
	modelID, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil // Not found is not an error
	}
	if err != nil {
		slog.Warn("sticky: redis get failed", "key", key, "error", err)
		return "", err // Return error but routing can continue
	}

	return modelID, nil
}

// Set pins a session to a model with TTL
func (s *StickyStore) Set(ctx context.Context, groupName, sessionKey, modelID string, ttl time.Duration) error {
	if s.client == nil || sessionKey == "" {
		return nil // No-op if no session key or client
	}

	key := s.buildKey(groupName, sessionKey)
	err := s.client.Set(ctx, key, modelID, ttl).Err()
	if err != nil {
		slog.Warn("sticky: redis set failed", "key", key, "model_id", modelID, "error", err)
		return err // Return error but routing already succeeded
	}

	return nil
}

// Delete removes a sticky pin (used when model permanently fails)
func (s *StickyStore) Delete(ctx context.Context, groupName, sessionKey string) error {
	if s.client == nil || sessionKey == "" {
		return nil
	}

	key := s.buildKey(groupName, sessionKey)
	err := s.client.Del(ctx, key).Err()
	if err != nil {
		slog.Warn("sticky: redis delete failed", "key", key, "error", err)
	}
	return err
}

func (s *StickyStore) buildKey(groupName, sessionKey string) string {
	return fmt.Sprintf("%s%s:%s", s.prefix, groupName, sessionKey)
}

// encodeStickyBinding 编码 sticky 绑定值:模型名 + 渠道名(ProviderName)。
// 同名多渠道的组靠渠道维度区分;provider 为空时退回纯模型名,兼容单渠道组。
func encodeStickyBinding(modelName, providerName string) string {
	if providerName == "" {
		return modelName
	}
	return modelName + "|" + providerName
}

// parseStickyBinding 解码 sticky 绑定值。旧格式(无 |)返回 provider 为空。
func parseStickyBinding(v string) (modelName, providerName string) {
	modelName, providerName, _ = strings.Cut(v, "|")
	return modelName, providerName
}
