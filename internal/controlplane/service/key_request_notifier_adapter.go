package service

import (
	"context"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// KeyRequestNotifierAdapter 把 keyRequestNotifier.Create 接进统一通知管线：
// 构造的 model.Notification 交给 Notify，按配置路由到 inapp/email/webhook。
type KeyRequestNotifierAdapter struct {
	notifier Notifier
}

var _ keyRequestNotifier = (*KeyRequestNotifierAdapter)(nil)

func NewKeyRequestNotifierAdapter(n Notifier) keyRequestNotifier {
	return &KeyRequestNotifierAdapter{notifier: n}
}

func (a *KeyRequestNotifierAdapter) Create(n *model.Notification) error {
	return a.notifier.Notify(context.Background(), n)
}
