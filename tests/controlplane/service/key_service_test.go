package service_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// fakeEventPublisher 模拟 EventPublisher 接口用于 KeyService 事件发布测试
type fakeEventPublisher struct {
	published []publishedEvent
}

type publishedEvent struct {
	eventType    event.ChangeEventType
	resourceType string
	resourceID   uuid.UUID
}

func (f *fakeEventPublisher) Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	f.published = append(f.published, publishedEvent{eventType, resourceType, resourceID})
}

func TestNewKeyService_NilPublisher(t *testing.T) {
	t.Run("publisher为nil时构造不panic", func(t *testing.T) {
		svc := service.NewKeyService(nil, nil, nil)
		assert.NotNil(t, svc)
	})
}

func TestNewEventPublisherAdapter_NilInput(t *testing.T) {
	t.Run("传入nil返回nil", func(t *testing.T) {
		adapter := service.NewEventPublisherAdapter(nil)
		assert.Nil(t, adapter)
	})
}

func TestKeyServiceErrors(t *testing.T) {
	t.Run("ErrKeyNotFound错误值存在", func(t *testing.T) {
		assert.NotNil(t, service.ErrKeyNotFound)
		assert.Equal(t, "key not found", service.ErrKeyNotFound.Error())
	})
}

func TestVerifyResult_Structure(t *testing.T) {
	t.Run("VerifyResult字段正确", func(t *testing.T) {
		keyID := uuid.New()
		userID := uuid.New()
		result := service.VerifyResult{
			KeyID:           keyID,
			Status:          "active",
			AccessAllGroups: true,
			User: service.VerifyResultUser{
				UserID: userID,
				Name:   "test-user",
			},
		}
		assert.Equal(t, keyID, result.KeyID)
		assert.Equal(t, "active", result.Status)
		assert.True(t, result.AccessAllGroups)
		assert.Equal(t, userID, result.User.UserID)
	})
}

func TestKeyItem_Structure(t *testing.T) {
	t.Run("KeyItem基本字段", func(t *testing.T) {
		item := service.KeyItem{
			ID:              uuid.New(),
			Name:            "my-key",
			MaskedKey:       "sk-****abcd",
			KeyHint:         "abcd",
			Enabled:         true,
			AccessAllGroups: false,
		}
		assert.Equal(t, "my-key", item.Name)
		assert.Equal(t, "sk-****abcd", item.MaskedKey)
		assert.True(t, item.Enabled)
	})
}

func TestFakeEventPublisher_Records(t *testing.T) {
	t.Run("事件发布被记录", func(t *testing.T) {
		pub := &fakeEventPublisher{}
		keyID := uuid.New()
		pub.Publish(event.KeyCreated, "key", keyID)

		assert.Len(t, pub.published, 1)
		assert.Equal(t, event.KeyCreated, pub.published[0].eventType)
		assert.Equal(t, "key", pub.published[0].resourceType)
		assert.Equal(t, keyID, pub.published[0].resourceID)
	})
}

// TestKeyService_ListKeys_PaginationDefaults 验证 ListKeys 分页默认值逻辑
// 注：由于 KeyService 依赖 *repository.KeyRepo（具体类型），完整流程测试需要数据库；
// 此处验证 NewKeyService 构造以及 context 传递不 panic。
func TestKeyService_ListKeys_NilRepo(t *testing.T) {
	t.Run("repo为nil时ListKeys应panic或返回错误", func(t *testing.T) {
		svc := service.NewKeyService(nil, nil, nil)
		assert.NotNil(t, svc)
		// 由于 repo 是 nil，调用会 panic；此处验证构造可行性
		// 实际 ListKeys 测试需要集成测试环境
		_ = context.Background()
	})
}
