package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/core/quota"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/redis/go-redis/v9"
)

const (
	// QuotaSnapshotKey 配额快照在 Redis 中的键
	QuotaSnapshotKey = "quota:snapshot:v1"
)

// ConfigLoader 从 Redis 热加载配额快照，支持 config:change pub/sub 与 reconcile 轮询。
type ConfigLoader struct {
	rdb        *redis.Client
	holder     *SnapshotHolder
	lastLoadAt atomic.Int64 // UnixNano，用于防抖
}

// NewConfigLoader 创建配额配置加载器。
func NewConfigLoader(rdb *redis.Client, holder *SnapshotHolder) *ConfigLoader {
	return &ConfigLoader{rdb: rdb, holder: holder}
}

// Load 从 Redis 加载快照并更新 holder（幂等，1 秒内防抖）。
func (l *ConfigLoader) Load(ctx context.Context) error {
	// Debounce: skip if loaded less than 1 second ago
	now := time.Now().UnixNano()
	last := l.lastLoadAt.Load()
	if last > 0 && now-last < int64(time.Second) {
		slog.Debug("quota: debounce skip, loaded recently")
		return nil
	}

	if l.rdb == nil {
		return fmt.Errorf("redis client is nil")
	}
	raw, err := l.rdb.Get(ctx, QuotaSnapshotKey).Result()
	if err != nil {
		// 失败时不消耗防抖窗口，下次调用可以立即重试
		if err == redis.Nil {
			slog.Debug("quota: no snapshot in redis, using fallback")
			return nil
		}
		return fmt.Errorf("get quota snapshot: %w", err)
	}

	var snap quota.QuotaSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return fmt.Errorf("unmarshal quota snapshot: %w", err)
	}

	// 成功后再更新 lastLoadAt，确保失败不消耗防抖窗口
	l.lastLoadAt.Store(now)

	l.holder.Set(&snap)
	slog.Debug("quota: snapshot loaded", "version", snap.Version)
	return nil
}

// StartPolling 后台 reconcile 轮询（接受 context 控制 goroutine 生命周期）。
func (l *ConfigLoader) StartPolling(ctx context.Context, interval time.Duration) {
	// 初始加载
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := l.Load(loadCtx); err != nil {
		slog.Warn("quota: initial snapshot load failed, using fallback", "error", err)
	}
	cancel()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("quota: polling stopped")
				return
			case <-ticker.C:
				loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				if err := l.Load(loadCtx); err != nil {
					slog.Debug("quota: reconcile load failed", "error", err)
				}
				cancel()
			}
		}
	}()
}

// SubscribeToChanges 订阅 Redis config:change 通道，收到 quota_snapshot_updated
// 事件时立即调用 Load 刷新配额快照，使变更在 1 秒内生效（替代等待 30s 轮询）。
// 内置重连循环：Pub/Sub 断线时自动重连。
func (l *ConfigLoader) SubscribeToChanges(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("quota: config change subscriber stopped")
				return
			default:
			}

			pubsub := l.rdb.Subscribe(ctx, infra.ConfigChangeChannel)
			slog.Info("quota: config change subscriber started", "channel", infra.ConfigChangeChannel)

			ch := pubsub.Channel()
		recvLoop:
			for {
				select {
				case <-ctx.Done():
					pubsub.Close()
					slog.Info("quota: config change subscriber stopped")
					return
				case msg, ok := <-ch:
					if !ok {
						// channel 关闭（断线），退出内层循环触发重连
						pubsub.Close()
						break recvLoop
					}
					// 只响应 quota_snapshot_updated 事件，忽略其他 config:change 消息
					if !strings.Contains(msg.Payload, "quota_snapshot_updated") {
						continue
					}
					slog.Debug("quota: snapshot update event received, reloading", "payload", msg.Payload)
					loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					if err := l.Load(loadCtx); err != nil {
						slog.Warn("quota: pub/sub triggered load failed", "error", err)
					}
					cancel()
				}
			}

			// 重连前等待 1 秒，避免快速重连循环
			slog.Warn("quota: pub/sub disconnected, reconnecting in 1s")
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()
}

// SaveQuotaSnapshot 将配额快照写入 Redis（控制面调用）。
func SaveQuotaSnapshot(ctx context.Context, rdb *redis.Client, snap *quota.QuotaSnapshot) error {
	if rdb == nil {
		return fmt.Errorf("redis client is nil")
	}
	if snap == nil {
		return fmt.Errorf("quota snapshot is nil")
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal quota snapshot: %w", err)
	}
	return rdb.Set(ctx, QuotaSnapshotKey, payload, 0).Err()
}

// Holder 返回快照持有者（供 middleware 使用）。
func (l *ConfigLoader) Holder() *SnapshotHolder {
	return l.holder
}

// Get 返回当前快照（可能为 nil）。
func (h *SnapshotHolder) GetSnapshot() *quota.QuotaSnapshot {
	v := h.current.Load()
	if v == nil {
		return nil
	}
	return v.(*quota.QuotaSnapshot)
}

// AtomicValue 返回底层 atomic.Value（供外部读取）。
func (h *SnapshotHolder) AtomicValue() *atomic.Value {
	return &h.current
}