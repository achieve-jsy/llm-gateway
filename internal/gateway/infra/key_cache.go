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
	// keyCacheRedisPrefix 带版本号，避免滚动发布/蓝绿并存（旧色 drain 600s）期间两代进程
	// 互相读到对方写入的条目：VerifyKeyResp 的字段一直在增量演进（key_type / budget_chain /
	// usable_groups / bypass_org_check…），旧条目被新代码读到会出现"零值字段"（预算链为空、
	// 可用组为空），表现为 403 或"模型组不可用"，用户侧同样归类成"密钥不可用"。
	// 改这个前缀时必须同步 internal/controlplane/event/publisher.go 的吊销列表，
	// 否则吊销的 key 会在新网关里继续命中旧前缀条目。
	keyCacheRedisPrefix = "gateway:keycache:v2:"
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

// HandleChangeEvent 处理配置变更事件中的 key 吊销 / 日志开关 / 授权配置变更通知。
// 当收到 key_disabled、key_deleted 或 key_log_changed 事件时，
// 遍历本地缓存剔除匹配的条目，并同步清除 Redis 双层缓存。
// 当收到 auth_config_changed（v0.7）时，清空全部 KeyCache 条目：授权/预算配置变更
// 会影响所有 key 的 usable_groups / budget_chain 快照，必须强制重新拉取。
func (kc *KeyCache) HandleChangeEvent(event ChangeEvent) {
	// v0.7: 授权 / 预算配置全局性变更——清空所有 key 缓存
	if event.ResourceType == "auth_config" || event.Type == "auth_config_changed" {
		kc.FlushAll("auth_config_changed")
		return
	}
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

// FlushAll 清空本地内存缓存 + Redis 缓存中所有 KeyCache 条目。
// 用于授权 / 预算等全局性配置变更，强制下次请求重新 verify-key 以拿到最新快照。
func (kc *KeyCache) FlushAll(reason string) {
	ctx := context.Background()
	var localDeleted int
	kc.local.Range(func(k, _ any) bool {
		kc.local.Delete(k)
		localDeleted++
		return true
	})
	var redisDeleted int
	if kc.rdb != nil {
		// SCAN + DEL 模式，避免阻塞 Redis
		iter := kc.rdb.Scan(ctx, 0, keyCacheRedisPrefix+"*", 200).Iterator()
		for iter.Next(ctx) {
			kc.rdb.Del(ctx, iter.Val())
			redisDeleted++
		}
		_ = iter.Err()
	}
	slog.Info("key cache: flushed all entries",
		"reason", reason,
		"local_deleted", localDeleted,
		"redis_deleted", redisDeleted,
	)
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