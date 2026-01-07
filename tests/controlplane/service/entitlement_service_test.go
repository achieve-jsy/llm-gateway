package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Mock 实现 ────────────────────────────────────────────────────────────────

// stubEntitlementGroupRepo 模拟 EntitlementGroupRepo 接口
type stubEntitlementGroupRepo struct {
	groups       map[uint]*model.Group
	namesByMode  map[string][]string
	namesByIDs   map[uint]string
	updateErr    error
	findByIDErr  error
}

func newStubEntitlementGroupRepo() *stubEntitlementGroupRepo {
	return &stubEntitlementGroupRepo{
		groups:      make(map[uint]*model.Group),
		namesByMode: make(map[string][]string),
		namesByIDs:  make(map[uint]string),
	}
}

func (f *stubEntitlementGroupRepo) FindByID(id uint) (*model.Group, error) {
	if f.findByIDErr != nil {
		return nil, f.findByIDErr
	}
	if g, ok := f.groups[id]; ok {
		return g, nil
	}
	return nil, errors.New("record not found")
}

func (f *stubEntitlementGroupRepo) FindNamesByOpenMode(openMode string) ([]string, error) {
	return f.namesByMode[openMode], nil
}

func (f *stubEntitlementGroupRepo) FindNamesByIDs(ids []uint) ([]string, error) {
	var names []string
	for _, id := range ids {
		if name, ok := f.namesByIDs[id]; ok {
			names = append(names, name)
		}
	}
	return names, nil
}

func (f *stubEntitlementGroupRepo) UpdateOpenMode(id uint, openMode string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	if g, ok := f.groups[id]; ok {
		g.OpenMode = openMode
	}
	return nil
}

func (f *stubEntitlementGroupRepo) addGroup(id uint, name, openMode string) {
	f.groups[id] = &model.Group{ID: id, Name: name, OpenMode: openMode, Enabled: true}
	f.namesByIDs[id] = name
}

// stubEntitlementEntitlementRepo 模拟 EntitlementEntitlementRepo 接口
type stubEntitlementEntitlementRepo struct {
	rows           map[uuid.UUID]*model.GroupEntitlement
	controlledIDs  []uint
	controlledErr  error
	keyIDs         []uuid.UUID
	keyIDsErr      error
	createErr      error
	findOneErr     map[string]error
}

func newStubEntitlementEntitlementRepo() *stubEntitlementEntitlementRepo {
	return &stubEntitlementEntitlementRepo{
		rows:       make(map[uuid.UUID]*model.GroupEntitlement),
		findOneErr: make(map[string]error),
	}
}

func (f *stubEntitlementEntitlementRepo) Create(ent *model.GroupEntitlement) error {
	if f.createErr != nil {
		return f.createErr
	}
	if ent.ID == uuid.Nil {
		ent.ID = uuid.New()
	}
	f.rows[ent.ID] = ent
	return nil
}

func (f *stubEntitlementEntitlementRepo) FindByID(id uuid.UUID) (*model.GroupEntitlement, error) {
	if ent, ok := f.rows[id]; ok {
		return ent, nil
	}
	return nil, errors.New("record not found")
}

func (f *stubEntitlementEntitlementRepo) Delete(id uuid.UUID) error {
	delete(f.rows, id)
	return nil
}

func (f *stubEntitlementEntitlementRepo) SetEnabled(id uuid.UUID, enabled bool) error {
	if ent, ok := f.rows[id]; ok {
		ent.Enabled = enabled
	}
	return nil
}

func (f *stubEntitlementEntitlementRepo) ListByGroup(groupID uint, scopeType string) ([]model.GroupEntitlement, error) {
	var ents []model.GroupEntitlement
	for _, ent := range f.rows {
		if ent.GroupID == groupID {
			if scopeType == "" || ent.ScopeType == scopeType {
				ents = append(ents, *ent)
			}
		}
	}
	return ents, nil
}

func (f *stubEntitlementEntitlementRepo) FindOne(groupID uint, scopeType, scopeID string) (*model.GroupEntitlement, error) {
	key := scopeType + ":" + scopeID
	if err, ok := f.findOneErr[key]; ok {
		return nil, err
	}
	for _, ent := range f.rows {
		if ent.GroupID == groupID && ent.ScopeType == scopeType && ent.ScopeID == scopeID {
			return ent, nil
		}
	}
	return nil, errors.New("record not found")
}

func (f *stubEntitlementEntitlementRepo) EnabledControlledGroupIDs(_, _ string) ([]uint, error) {
	return f.controlledIDs, f.controlledErr
}

func (f *stubEntitlementEntitlementRepo) FindKeyIDsByDeptOrStudio(_, _ string) ([]uuid.UUID, error) {
	return f.keyIDs, f.keyIDsErr
}

// stubEntitlementEventPublisher 模拟 EntitlementEventPublisher 接口，记录发布的事件
type stubEntitlementEventPublisher struct {
	events          []*event.ChangeEvent
	publishErr      error
	invalidatedKeys []string
}

func newStubEntitlementEventPublisher() *stubEntitlementEventPublisher {
	return &stubEntitlementEventPublisher{}
}

func (f *stubEntitlementEventPublisher) Publish(_ context.Context, ev *event.ChangeEvent) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.events = append(f.events, ev)
	return nil
}

func (f *stubEntitlementEventPublisher) InvalidateKeyAuthCache(_ context.Context, keyHash string) {
	f.invalidatedKeys = append(f.invalidatedKeys, keyHash)
}

// ── 测试 ─────────────────────────────────────────────────────────────────────

func TestComputeUsableGroups_EmptyDeptID(t *testing.T) {
	// 测试目的：当 deptID 为空时（旧 Key 或 Service Key），只返回 public 组，跳过 controlled 级联判定
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.namesByMode["public"] = []string{"deepseek-chat", "qwen-turbo"}

	entRepo := newStubEntitlementEntitlementRepo()
	pub := newStubEntitlementEventPublisher()

	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	groups, err := svc.ComputeUsableGroups(context.Background(), "", "studio-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"deepseek-chat", "qwen-turbo"}, groups)
}

func TestComputeUsableGroups_PublicOnly(t *testing.T) {
	// 测试目的：deptID+studioID 都有值，但无 controlled 组被开通，仅返回 public 组
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.namesByMode["public"] = []string{"deepseek-chat"}

	entRepo := newStubEntitlementEntitlementRepo()
	entRepo.controlledIDs = nil

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	groups, err := svc.ComputeUsableGroups(context.Background(), "dept-1", "studio-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"deepseek-chat"}, groups)
}

func TestComputeUsableGroups_PublicAndControlled(t *testing.T) {
	// 测试目的：dept D1 已开通 claude-opus，应返回 public + controlled 组名合并去重
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.namesByMode["public"] = []string{"deepseek-chat", "qwen-turbo"}
	grpRepo.addGroup(10, "deepseek-chat", "public")
	grpRepo.addGroup(20, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	entRepo.controlledIDs = []uint{20}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	groups, err := svc.ComputeUsableGroups(context.Background(), "dept-1", "studio-1")
	require.NoError(t, err)
	assert.Len(t, groups, 3)
	assert.Contains(t, groups, "deepseek-chat")
	assert.Contains(t, groups, "qwen-turbo")
	assert.Contains(t, groups, "claude-opus")
}

func TestGrantStudio_PublicGroupRejected(t *testing.T) {
	// 测试目的：对 public 组调用 GrantStudio 必须返回 ErrGroupMustBeControlled
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "deepseek-chat", "public")

	entRepo := newStubEntitlementEntitlementRepo()
	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	_, err := svc.GrantStudio(context.Background(), 1, "studio-1", uuid.New())
	assert.ErrorIs(t, err, service.ErrGroupMustBeControlled)
}

func TestGrantStudio_Success(t *testing.T) {
	// 测试目的：对 controlled 组首次授权工作室，应成功创建 entitlement 并发布 EntitlementGranted 事件
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	// FindOne 返回 not found，模拟首次授权
	entRepo.findOneErr["studio:studio-1"] = errors.New("record not found")

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	operator := uuid.New()
	item, err := svc.GrantStudio(context.Background(), 1, "studio-1", operator)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus", item.GroupName)
	assert.Equal(t, "studio", item.ScopeType)
	assert.Equal(t, "studio-1", item.ScopeID)
	assert.True(t, item.Enabled)

	// 验证发布了 EntitlementGranted 事件
	require.Len(t, pub.events, 1)
	assert.Equal(t, event.EntitlementGranted, pub.events[0].Type)
}

func TestGrantStudio_DuplicateRejected(t *testing.T) {
	// 测试目的：工作室已被授权（enabled=true），重复授权应返回 ErrEntitlementExists
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	existingID := uuid.New()
	entRepo.rows[existingID] = &model.GroupEntitlement{
		ID:        existingID,
		GroupID:   1,
		ScopeType: "studio",
		ScopeID:   "studio-1",
		Enabled:   true,
	}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	_, err := svc.GrantStudio(context.Background(), 1, "studio-1", uuid.New())
	assert.ErrorIs(t, err, service.ErrEntitlementExists)
}

func TestGrantDept_NoStudioGrant(t *testing.T) {
	// 测试目的：工作室未下放时，部门开通应返回 ErrStudioNotGranted
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	entRepo.findOneErr["studio:studio-1"] = errors.New("record not found")

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	actor := &rbac.Actor{IsGlobal: true}
	_, err := svc.GrantDept(context.Background(), 1, "dept-1", "studio-1", uuid.New(), actor)
	assert.ErrorIs(t, err, service.ErrStudioNotGranted)
}

func TestGrantDept_Forbidden(t *testing.T) {
	// 测试目的：非 admin 且不管辖目标工作室时，返回 ErrEntitlementForbidden
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	studioEntID := uuid.New()
	entRepo.rows[studioEntID] = &model.GroupEntitlement{
		ID: studioEntID, GroupID: 1, ScopeType: "studio", ScopeID: "studio-1", Enabled: true,
	}
	entRepo.findOneErr["dept:dept-1"] = errors.New("record not found")

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	// actor 不是 global，也不管辖 studio-1
	actor := &rbac.Actor{IsGlobal: false, StudioIDs: []string{"other-studio"}}
	_, err := svc.GrantDept(context.Background(), 1, "dept-1", "studio-1", uuid.New(), actor)
	assert.ErrorIs(t, err, service.ErrEntitlementForbidden)
}

func TestGrantDept_Success(t *testing.T) {
	// 测试目的：studio 已下放 + actor 为 admin + 部门未开通 → 成功创建 dept 授权
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	studioEntID := uuid.New()
	entRepo.rows[studioEntID] = &model.GroupEntitlement{
		ID: studioEntID, GroupID: 1, ScopeType: "studio", ScopeID: "studio-1", Enabled: true,
	}
	entRepo.findOneErr["dept:dept-1"] = errors.New("record not found")
	entRepo.keyIDs = []uuid.UUID{uuid.New(), uuid.New()}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	actor := &rbac.Actor{IsGlobal: true}
	item, err := svc.GrantDept(context.Background(), 1, "dept-1", "studio-1", uuid.New(), actor)
	require.NoError(t, err)
	assert.Equal(t, "dept", item.ScopeType)
	assert.Equal(t, "dept-1", item.ScopeID)

	// 验证发布了 EntitlementGranted + key_updated 事件
	hasGranted := false
	hasKeyUpdated := false
	for _, ev := range pub.events {
		if ev.Type == event.EntitlementGranted {
			hasGranted = true
		}
		if ev.Type == event.KeyUpdated && ev.ResourceType == "api_key" {
			hasKeyUpdated = true
		}
	}
	assert.True(t, hasGranted, "应发布 EntitlementGranted 事件")
	assert.True(t, hasKeyUpdated, "应发布 key_updated 事件通知 gateway 本地缓存失效")
}

func TestRevoke_NotFound(t *testing.T) {
	// 测试目的：撤销不存在的 entitlement，返回 ErrEntitlementNotFound
	grpRepo := newStubEntitlementGroupRepo()
	entRepo := newStubEntitlementEntitlementRepo()
	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	err := svc.Revoke(context.Background(), uuid.New(), uuid.New())
	assert.ErrorIs(t, err, service.ErrEntitlementNotFound)
}

func TestRevoke_Success(t *testing.T) {
	// 测试目的：撤销已有 entitlement，应删除记录、发布 EntitlementRevoked 并触发本地缓存失效
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	entID := uuid.New()
	entRepo.rows[entID] = &model.GroupEntitlement{
		ID: entID, GroupID: 1, ScopeType: "dept", ScopeID: "dept-1", Enabled: true,
	}
	entRepo.keyIDs = []uuid.UUID{uuid.New()}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	err := svc.Revoke(context.Background(), entID, uuid.New())
	require.NoError(t, err)

	// 验证记录被删除
	_, err = entRepo.FindByID(entID)
	assert.Error(t, err, "记录应已被删除")

	// 验证发布了 EntitlementRevoked 事件
	hasRevoked := false
	hasKeyUpdated := false
	for _, ev := range pub.events {
		if ev.Type == event.EntitlementRevoked {
			hasRevoked = true
		}
		if ev.Type == event.KeyUpdated {
			hasKeyUpdated = true
		}
	}
	assert.True(t, hasRevoked, "应发布 EntitlementRevoked 事件")
	assert.True(t, hasKeyUpdated, "应发布 key_updated 事件通知 gateway 本地缓存失效")
}

func TestSetOpenMode_PublicToControlled(t *testing.T) {
	// 测试目的：public→controlled 不触发缓存失效（无 key 受影响，因为 controlled 默认不可用）
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "deepseek-chat", "public")

	entRepo := newStubEntitlementEntitlementRepo()
	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	err := svc.SetOpenMode(context.Background(), 1, "controlled", uuid.New())
	require.NoError(t, err)

	// 验证 open_mode 已更新
	assert.Equal(t, "controlled", grpRepo.groups[1].OpenMode)

	// 应有 OpenModeChanged 事件，但不应该有 key_updated（controlled→public 才需要）
	hasModeChanged := false
	hasKeyUpdated := false
	for _, ev := range pub.events {
		if ev.Type == event.OpenModeChanged {
			hasModeChanged = true
		}
		if ev.Type == event.KeyUpdated {
			hasKeyUpdated = true
		}
	}
	assert.True(t, hasModeChanged)
	assert.False(t, hasKeyUpdated, "public→controlled 不应触发本地缓存失效")
}

func TestSetOpenMode_ControlledToPublic_TriggersInvalidation(t *testing.T) {
	// 测试目的：controlled→public 触发缓存失效，因为原本依赖 controlled 授权的 key 现在可能受影响
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	// 模拟该组有 studio 域授权
	studioEntID := uuid.New()
	entRepo.rows[studioEntID] = &model.GroupEntitlement{
		ID: studioEntID, GroupID: 1, ScopeType: "studio", ScopeID: "studio-1", Enabled: true,
	}
	entRepo.keyIDs = []uuid.UUID{uuid.New(), uuid.New()}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	err := svc.SetOpenMode(context.Background(), 1, "public", uuid.New())
	require.NoError(t, err)

	// 应为每个受影响的 scope 发布 key_updated 事件
	hasKeyUpdated := false
	for _, ev := range pub.events {
		if ev.Type == event.KeyUpdated && ev.ResourceType == "api_key" {
			hasKeyUpdated = true
			break
		}
	}
	assert.True(t, hasKeyUpdated, "controlled→public 应触发 gateway 本地缓存失效")
}

func TestSetOpenMode_InvalidMode(t *testing.T) {
	// 测试目的：非法 open_mode 值应被拒绝
	grpRepo := newStubEntitlementGroupRepo()
	entRepo := newStubEntitlementEntitlementRepo()
	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	err := svc.SetOpenMode(context.Background(), 1, "invalid", uuid.New())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid open_mode")
}

func TestSetOpenMode_GroupNotFound(t *testing.T) {
	// 测试目的：组不存在时返回 ErrGroupNotFound
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.findByIDErr = errors.New("record not found")

	entRepo := newStubEntitlementEntitlementRepo()
	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	err := svc.SetOpenMode(context.Background(), 999, "public", uuid.New())
	assert.ErrorIs(t, err, service.ErrGroupNotFound)
}

func TestListEntitlements_Success(t *testing.T) {
	// 测试目的：列出组的授权记录，包含 group_name
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	ent1ID := uuid.New()
	ent2ID := uuid.New()
	entRepo.rows[ent1ID] = &model.GroupEntitlement{
		ID: ent1ID, GroupID: 1, ScopeType: "studio", ScopeID: "s1", Enabled: true,
	}
	entRepo.rows[ent2ID] = &model.GroupEntitlement{
		ID: ent2ID, GroupID: 1, ScopeType: "dept", ScopeID: "d1", Enabled: true,
	}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	items, err := svc.ListEntitlements(context.Background(), 1, "")
	require.NoError(t, err)
	assert.Len(t, items, 2)
	for _, item := range items {
		assert.Equal(t, "claude-opus", item.GroupName)
	}
}

func TestListEntitlements_ScopeFilter(t *testing.T) {
	// 测试目的：scopeType 过滤只返回指定域类型的记录
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	studioID := uuid.New()
	deptID := uuid.New()
	entRepo.rows[studioID] = &model.GroupEntitlement{
		ID: studioID, GroupID: 1, ScopeType: "studio", ScopeID: "s1", Enabled: true,
	}
	entRepo.rows[deptID] = &model.GroupEntitlement{
		ID: deptID, GroupID: 1, ScopeType: "dept", ScopeID: "d1", Enabled: true,
	}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	items, err := svc.ListEntitlements(context.Background(), 1, "studio")
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "studio", items[0].ScopeType)
}

func TestNotifyLocalCacheInvalidation_NilInvalidator(t *testing.T) {
	// 测试目的：invalidator 为 nil 时不 panic
	entRepo := newStubEntitlementEntitlementRepo()
	service.NotifyLocalCacheInvalidation(context.Background(), nil, entRepo, "d1", "s1")
}

func TestNotifyLocalCacheInvalidation_PublishesKeyUpdatedEvents(t *testing.T) {
	// 测试目的：为每个受影响的 key 发布 key_updated 事件，resourceType=api_key
	entRepo := newStubEntitlementEntitlementRepo()
	kid1 := uuid.New()
	kid2 := uuid.New()
	kid3 := uuid.New()
	entRepo.keyIDs = []uuid.UUID{kid1, kid2, kid3}

	pub := newStubEntitlementEventPublisher()
	service.NotifyLocalCacheInvalidation(context.Background(), pub, entRepo, "dept-1", "studio-1")

	// 3 个 key → 3 个 key_updated 事件
	assert.Len(t, pub.events, 3)
	for _, ev := range pub.events {
		assert.Equal(t, event.KeyUpdated, ev.Type)
		assert.Equal(t, "api_key", ev.ResourceType)
	}
	// 验证每个 key ID 都被包含
	publishedIDs := make(map[uuid.UUID]bool)
	for _, ev := range pub.events {
		publishedIDs[ev.ResourceID] = true
	}
	assert.True(t, publishedIDs[kid1])
	assert.True(t, publishedIDs[kid2])
	assert.True(t, publishedIDs[kid3])
}

func TestGrantStudio_ReEnableExistingDisabled(t *testing.T) {
	// 测试目的：工作室授权已存在但 enabled=false 时，应重新启用而非创建新记录
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.addGroup(1, "claude-opus", "controlled")

	entRepo := newStubEntitlementEntitlementRepo()
	existingID := uuid.New()
	entRepo.rows[existingID] = &model.GroupEntitlement{
		ID: existingID, GroupID: 1, ScopeType: "studio", ScopeID: "studio-1", Enabled: false,
	}
	entRepo.keyIDs = []uuid.UUID{uuid.New()}

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	item, err := svc.GrantStudio(context.Background(), 1, "studio-1", uuid.New())
	require.NoError(t, err)
	assert.True(t, item.Enabled, "应重新启用已有记录")
	assert.Equal(t, existingID, item.ID, "应复用已有记录而非新建")
}

func TestComputeUsableGroups_ErrorPropagation(t *testing.T) {
	// 测试目的：级联判定查询失败时，错误应向上传播
	grpRepo := newStubEntitlementGroupRepo()
	grpRepo.namesByMode["public"] = []string{"deepseek-chat"}

	entRepo := newStubEntitlementEntitlementRepo()
	entRepo.controlledErr = errors.New("db connection failed")

	pub := newStubEntitlementEventPublisher()
	svc := service.NewEntitlementService(entRepo, grpRepo, pub)

	_, err := svc.ComputeUsableGroups(context.Background(), "d1", "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db connection failed")
}
