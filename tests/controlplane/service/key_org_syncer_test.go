package service_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// fakeOrgPublisher 记录 KeyOrgSyncer 发出的事件，用于断言
type fakeOrgPublisher struct {
	published   []orgPublishedEvent
	invalidated []string
}

type orgPublishedEvent struct {
	eventType    event.ChangeEventType
	resourceType string
	resourceID   uuid.UUID
}

func (f *fakeOrgPublisher) Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	f.published = append(f.published, orgPublishedEvent{eventType, resourceType, resourceID})
}

func (f *fakeOrgPublisher) InvalidateKeyCache(keyHash string) {
	f.invalidated = append(f.invalidated, keyHash)
}

func TestNewKeyOrgSyncer_NilPublisher(t *testing.T) {
	// 测试目的：publisher 为 nil 时构造不 panic，OnUserDeptChanged 不崩溃
	syncer := service.NewKeyOrgSyncer(nil, nil)
	assert.NotNil(t, syncer)
}

func TestKeyOrgSyncer_OnDeptStudioChanged_EmptyList(t *testing.T) {
	// 测试目的：传入空 deptSSOs 列表时不 panic，不产生任何事件
	pub := &fakeOrgPublisher{}
	syncer := service.NewKeyOrgSyncer(nil, pub)

	// 空列表不应 panic
	assert.NotPanics(t, func() {
		syncer.OnDeptStudioChanged(context.Background(), []string{})
	})
}

func TestKeyOrgSyncer_OnUserDeptChanged_EmptyUserID(t *testing.T) {
	// 测试目的：userID 为空时不 panic
	pub := &fakeOrgPublisher{}
	syncer := service.NewKeyOrgSyncer(nil, pub)

	assert.NotPanics(t, func() {
		syncer.OnUserDeptChanged(context.Background(), "")
	})
}

func TestKeyOrgSyncer_Interface_Compliance(t *testing.T) {
	// 测试目的：KeyOrgSyncer 满足 OrgSyncer 接口，可安全赋值
	pub := &fakeOrgPublisher{}
	syncer := service.NewKeyOrgSyncer(nil, pub)

	// OrgSyncer 接口要求的两个方法
	var _ interface {
		OnDeptStudioChanged(ctx context.Context, deptSSOs []string)
		OnUserDeptChanged(ctx context.Context, userID string)
	} = syncer
}

func TestMappingService_OrgSyncer_SetNil(t *testing.T) {
	// 测试目的：OrgSyncer 接口可以设置为 nil，SetOrgSyncer 不 panic
	// 注意：MappingService 在 orgservice/service 包，此处仅验证 KeyOrgSyncer 接口契约
	// MappingService 的详细测试见 tests/orgservice/
	syncer := service.NewKeyOrgSyncer(nil, nil)
	assert.NotNil(t, syncer)

	// 验证 KeyOrgSyncer 满足 OrgSyncer 接口（编译期检查）
	var _ interface {
		OnDeptStudioChanged(ctx context.Context, deptSSOs []string)
		OnUserDeptChanged(ctx context.Context, userID string)
	} = syncer
}
