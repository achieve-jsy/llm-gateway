package infra

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// ConfigChangeChannel Redis pub/sub 通道名
	ConfigChangeChannel = "config:change"
)

// ChangeEvent 配置变更事件（与 control-plane 的 event/change_event.go 结构一致）
type ChangeEvent struct {
	Type            string    `json:"type"`
	ResourceType    string    `json:"resource_type"`
	ResourceID      string    `json:"resource_id"`
	SnapshotVersion int64     `json:"snapshot_version,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// ChangeCallback 配置变更回调函数类型
type ChangeCallback func(event ChangeEvent)

// Subscriber Redis pub/sub 订阅器
type Subscriber struct {
	rdb       *redis.Client
	callbacks []ChangeCallback
}

// NewSubscriber 创建配置变更订阅器
func NewSubscriber(rdb *redis.Client, callback ChangeCallback) *Subscriber {
	return &Subscriber{
		rdb:       rdb,
		callbacks: []ChangeCallback{callback},
	}
}

// AddCallback 追加额外的回调函数。
func (s *Subscriber) AddCallback(cb ChangeCallback) {
	s.callbacks = append(s.callbacks, cb)
}

// Start 启动订阅监听（阻塞直到 ctx 取消）
// 应在 goroutine 中调用
func (s *Subscriber) Start(ctx context.Context) {
	pubsub := s.rdb.Subscribe(ctx, ConfigChangeChannel)
	defer pubsub.Close()

	slog.Info("config change subscriber started", "channel", ConfigChangeChannel)

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			slog.Info("config change subscriber stopping")
			return
		case msg, ok := <-ch:
			if !ok {
				slog.Warn("config change subscriber channel closed")
				return
			}
			s.handleMessage(msg)
		}
	}
}

// handleMessage 处理收到的消息
func (s *Subscriber) handleMessage(msg *redis.Message) {
	var event ChangeEvent
	if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
		slog.Error("failed to unmarshal change event", "error", err, "payload", msg.Payload)
		return
	}

	slog.Info("received config change event",
		"type", event.Type,
		"resource_type", event.ResourceType,
		"resource_id", event.ResourceID,
		"snapshot_version", event.SnapshotVersion,
		"timestamp", event.Timestamp,
	)

	for _, cb := range s.callbacks {
		cb(event)
	}
}
