package service

import (
	"context"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
)

// ChannelGate 按通知类型 + 渠道查询是否允许发送。
type ChannelGate interface {
	IsEnabled(ctx context.Context, notifType, channel string) bool
}

// NotificationChannelService 管理通知渠道规则，同时实现 ChannelGate 接口。
type NotificationChannelService struct {
	repo *repository.NotificationChannelRepo
}

func NewNotificationChannelService(repo *repository.NotificationChannelRepo) *NotificationChannelService {
	return &NotificationChannelService{repo: repo}
}

func (s *NotificationChannelService) GetRules(ctx context.Context) ([]model.NotificationChannelRule, error) {
	return s.repo.GetAll()
}

func (s *NotificationChannelService) UpdateRules(ctx context.Context, rules []model.NotificationChannelRule) error {
	return s.repo.Upsert(rules)
}

// IsEnabled 查表判断通知类型 + 渠道是否启用。缺省规则（DB 无记录时）返回 true。
func (s *NotificationChannelService) IsEnabled(ctx context.Context, notifType, channel string) bool {
	rules, err := s.repo.GetAll()
	if err != nil {
		slog.Warn("notification_channel: load rules failed, defaulting to enabled", "error", err)
		return true
	}
	for _, r := range rules {
		if r.NotificationType == notifType && r.Channel == channel {
			return r.Enabled
		}
	}
	return true
}

// GetMetadata 返回通知类型和渠道的元数据（标签等），用于前端动态渲染表格。
// 类型列表来自统一注册表（notification_registry.go），渠道为注册表中全部可用渠道。
func (s *NotificationChannelService) GetMetadata() model.NotificationMetadata {
	regs := GetNotificationTypeRegistry()
	types := make([]model.NotificationTypeMeta, 0, len(regs))
	for _, t := range regs {
		types = append(types, model.NotificationTypeMeta{Type: t.Name, Label: t.DisplayName})
	}
	channels := make([]model.NotificationChannelMeta, 0, 3)
	for _, ch := range allRegistryChannels() {
		channels = append(channels, model.NotificationChannelMeta{Channel: ch, Label: channelDisplayName(ch)})
	}
	return model.NotificationMetadata{Types: types, Channels: channels}
}
