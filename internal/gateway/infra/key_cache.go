package infra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyCacheRedisPrefix = "gateway:keycache:"
	// keyCacheRedisTTL / keyCacheLocalTTL：Key 验证结果缓存时长。
	// 主动吊销通过 HandleChangeEvent（key_disabled/key_deleted）立即失效，
	// 因此可以设置较长的 TTL 而不影响安全性。
	// 5s/15s 是历史遗留的过保守值，已导致高频 Redis/控制面穿透，影响 TTFT。
	keyCacheRedisTTL = 5 * time.Minute
	keyCacheLocalTTL = 60 * time.Second
)

type cachedKeyEntry struct {
	data   *VerifyKeyResp
	expiry time.Time
}

// KeyCache 提供 API Key 验证结果的双层缓存（本地内存 + Redis）。
// 保证多实例部署下同 key 只穿透一次到 control-plane。
type KeyCache struct {
	local     sync.Map
	rdb       *redis.Client
	closeCh   chan struct{}
	closeOnce sync.Once
}

// NewKeyCache 创建 KeyCache 实例。
// rdb 为 nil 时仅使用本地内存缓存（单实例模式）。
func NewKeyCache(rdb *redis.Client) *KeyCache {
	kc := &KeyCache{
		rdb:     rdb,
		closeCh: make(chan struct{}),
	}
	go kc.cleanupLoop()
	return kc
}

// Close 停止后台清理协程。
func (kc *KeyCache) Close() {
	kc.closeOnce.Do(func() { close(kc.closeCh) })
}

func (kc *KeyCache) keyHash(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// Get 从缓存中获取 key 的验证结果。
// 先查本地内存（纳秒级），再查 Redis（毫秒级），均未命中返回 nil, false。
func (kc *KeyCache) Get(ctx context.Context, rawKey string) (*VerifyKeyResp, bool) {
	hash := kc.keyHash(rawKey)

	if entry, ok := kc.local.Load(hash); ok {
		e := entry.(*cachedKeyEntry)
		if time.Now().Before(e.expiry) {
			return e.data, true
		}
		kc.local.Delete(hash)
	}

	if kc.rdb != nil {
		data, err := kc.rdb.Get(ctx, keyCacheRedisPrefix+hash).Bytes()
		if err == nil {
			var resp VerifyKeyResp
			if json.Unmarshal(data, &resp) == nil {
				kc.local.Store(hash, &cachedKeyEntry{
					data:   &resp,
					expiry: time.Now().Add(keyCacheLocalTTL),
				})
				return &resp, true
			}
		}
	}

	return nil, false
}

// Set 将验证结果写入双层缓存。
// 仅缓存 active 状态的 key，避免 disabled key 被持续命中。
func (kc *KeyCache) Set(ctx context.Context, rawKey string, resp *VerifyKeyResp) {
	if resp == nil || resp.Data.Status != "active" {
		return
	}

	hash := kc.keyHash(rawKey)

	kc.local.Store(hash, &cachedKeyEntry{
		data:   resp,
		expiry: time.Now().Add(keyCacheLocalTTL),
	})

	if kc.rdb != nil {
		data, err := json.Marshal(resp)
		if err != nil {
			slog.Warn("key cache: marshal failed", "error", err)
			return
		}
		if err := kc.rdb.Set(ctx, keyCacheRedisPrefix+hash, data, keyCacheRedisTTL).Err(); err != nil {
			slog.Warn("key cache: redis set failed", "error", err)
		}
	}
}

// Invalidate 从本地缓存和 Redis 中主动剔除指定 key 的缓存条目。
func (kc *KeyCache) Invalidate(ctx context.Context, rawKey string) {
	hash := kc.keyHash(rawKey)
	kc.local.Delete(hash)
	if kc.rdb != nil {
		kc.rdb.Del(ctx, keyCacheRedisPrefix+hash)
	}
}

// HandleChangeEvent 处理配置变更事件中的 key 吊销 / 日志开关变更通知。
// 当收到 key_disabled、key_deleted 或 key_log_changed 事件时，
// 遍历本地缓存剔除匹配的条目，并同步清除 Redis 双层缓存。
func (kc *KeyCache) HandleChangeEvent(event ChangeEvent) {
	if event.ResourceType != "api_key" {
		return
	}
	switch event.Type {
	case "key_disabled", "key_deleted", "key_log_changed", "key_updated":
	default:
		return
	}

	keyID := event.ResourceID
	ctx := context.Background()
	var deleted int
	kc.local.Range(func(k, v any) bool {
		e := v.(*cachedKeyEntry)
		if e.data.Data.KeyID == keyID {
			hash := k.(string)
			kc.local.Delete(k)
			if kc.rdb != nil {
				// 清除 gateway 自身的 KeyCache Redis 条目
				kc.rdb.Del(ctx, keyCacheRedisPrefix+hash)
				// 同时清除 auth service 的 Redis 缓存（key:auth:verify:{hash}）
				kc.rdb.Del(ctx, "key:auth:verify:"+hash)
			}
			deleted++
		}
		return true
	})
	if deleted > 0 {
		slog.Info("key cache: invalidated entries", "event_type", event.Type, "key_id", keyID, "entries", deleted)
	}
}

func (kc *KeyCache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-kc.closeCh:
			return
		case <-ticker.C:
			kc.local.Range(func(key, value any) bool {
				e := value.(*cachedKeyEntry)
				if time.Now().After(e.expiry) {
					kc.local.Delete(key)
				}
				return true
			})
		}
	}
}