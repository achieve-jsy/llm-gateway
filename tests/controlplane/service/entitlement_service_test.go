package service_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fakes
// ─────────────────────────────────────────────────────────────────────────────

// fakeEntitlementRepo 模拟 EntitlementRepo
type fakeEntitlementRepo struct {
	upsertCalls       []*model.GroupEntitlement
	revokeCalls       []revokeCall
	findByGroupCalls  []uint
	controlledIDs     []uint // EnabledControlledGroupIDs 返回
	controlledErr     error
	upsertErr         error
	revokeErr         error
	findByGroupResult []model.GroupEntitlement
	findByGroupErr    error
}

type revokeCall struct {
	GroupID   uint
	ScopeType string
	ScopeID   string
}

func (f *fakeEntitlementRepo) Upsert(e *model.GroupEntitlement) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upsertCalls = append(f.upsertCalls, e)
	return nil
}

func (f *fakeEntitlementRepo) Revoke(groupID uint, scopeType, scopeID string) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revokeCalls = append(f.revokeCalls, revokeCall{groupID, scopeType, scopeID})
	return nil
}

func (f *fakeEntitlementRepo) FindByGroup(groupID uint) ([]model.GroupEntitlement, error) {
	f.findByGroupCalls = append(f.findByGroupCalls, groupID)
	return f.findByGroupResult, f.findByGroupErr
}

func (f *fakeEntitlementRepo) EnabledControlledGroupIDs(deptID, studioID string) ([]uint, error) {
	return f.controlledIDs, f.controlledErr
}

// fakeEntitlementGroupRepo 模拟 EntitlementGroupRepo
type fakeEntitlementGroupRepo struct {
	groups          map[uint]*model.Group
	updateCalls     []updateOpenModeCall
	listByIDsCalls  [][]uint
	idsByOpenMode   map[string][]uint // openMode -> []id
	idsByOpenModeErr error
	updateErr       error
	listByIDsErr    error
	listByIDsResult []model.Group // 预设结果（优先使用）
}

type updateOpenModeCall struct {
	GroupID  uint
	OpenMode string
}

func newFakeGroupRepo() *fakeEntitlementGroupRepo {
	return &fakeEntitlementGroupRepo{
		groups:        make(map[uint]*model.Group),
		idsByOpenMode: make(map[string][]uint),
	}
}

func (f *fakeEntitlementGroupRepo) addGroup(id uint, name, openMode string, enabled bool) {
	f.groups[id] = &model.Group{ID: id, Name: name, OpenMode: openMode, Enabled: enabled}
}

func (f *fakeEntitlementGroupRepo) FindByID(id uint) (*model.Group, error) {
	g, ok := f.groups[id]
	if !ok {
		return nil, nil
	}
	return g, nil
}

func (f *fakeEntitlementGroupRepo) UpdateOpenMode(id uint, openMode string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updateCalls = append(f.updateCalls, updateOpenModeCall{id, openMode})
	if g, ok := f.groups[id]; ok {
		g.OpenMode = openMode
	}
	return nil
}

func (f *fakeEntitlementGroupRepo) IDsByOpenMode(openMode string) ([]uint, error) {
	if f.idsByOpenModeErr != nil {
		return nil, f.idsByOpenModeErr
	}
	return f.idsByOpenMode[openMode], nil
}

func (f *fakeEntitlementGroupRepo) ListGroupsByIDs(ids []uint) ([]model.Group, error) {
	f.listByIDsCalls = append(f.listByIDsCalls, ids)
	if f.listByIDsErr != nil {
		return nil, f.listByIDsErr
	}
	if f.listByIDsResult != nil {
		return f.listByIDsResult, nil
	}
	// 兜底：从 in-memory 数据查
	out := make([]model.Group, 0, len(ids))
	for _, id := range ids {
		if g, ok := f.groups[id]; ok && g.Enabled {
			out = append(out, *g)
		}
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: SetOpenMode
// ─────────────────────────────────────────────────────────────────────────────

// TestEntitlementService_SetOpenMode_InvalidMode 验证无效 open_mode 直接返回错误，
// 不查 DB、不写 DB、不发事件。
func TestEntitlementService_SetOpenMode_InvalidMode(t *testing.T) {
	gRepo := newFakeGroupRepo()
	eRepo := &fakeEntitlementRepo{}
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewEntitlementService(eRepo, gRepo, pub)

	err := svc.SetOpenMode(context.Background(), 1, "invalid-mode")

	assert.Equal(t, service.ErrInvalidOpenMode, err)
	assert.Empty(t, gRepo.updateCalls, "DB 不应被调用")
	assert.Empty(t, pub.events)
}

// TestEntitlementService_SetOpenMode_GroupNotFound 验证 group 不存在时返回 ErrGroupNotFound。
func TestEntitlementService_SetOpenMode_GroupNotFound(t *testing.T) {
	gRepo := newFakeGroupRepo()
	// 不预设任何 group
	svc := service.NewEntitlementService(&fakeEntitlementRepo{}, gRepo, nil)

	err := svc.SetOpenMode(context.Background(), 999, "public")

	assert.Equal(t, service.ErrGroupNotFound, err)
	assert.Empty(t, gRepo.updateCalls)
}

// TestEntitlementService_SetOpenMode_UpdateFails 验证 DB UpdateOpenMode 失败时：
// 返回 error，不发事件。
func TestEntitlementService_SetOpenMode_UpdateFails(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.addGroup(1, "gpt-4o", "public", true)
	gRepo.updateErr = fmt.Errorf("db error")
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewEntitlementService(&fakeEntitlementRepo{}, gRepo, pub)

	err := svc.SetOpenMode(context.Background(), 1, "controlled")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update open_mode")
	assert.Empty(t, pub.events, "DB 失败不应发事件")
}

// TestEntitlementService_SetOpenMode_Success 验证金线路径：
// DB 更新 + 广播 auth_config_changed 事件。
func TestEntitlementService_SetOpenMode_Success(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.addGroup(1, "gpt-4o", "public", true)
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewEntitlementService(&fakeEntitlementRepo{}, gRepo, pub)

	err := svc.SetOpenMode(context.Background(), 1, "controlled")

	assert.NoError(t, err)
	require.Len(t, gRepo.updateCalls, 1)
	assert.Equal(t, uint(1), gRepo.updateCalls[0].GroupID)
	assert.Equal(t, "controlled", gRepo.updateCalls[0].OpenMode)
	assert.Equal(t, "controlled", gRepo.groups[1].OpenMode, "内存中 open_mode 应被更新")

	// 广播事件
	require.Len(t, pub.events, 1)
	assert.Equal(t, event.AuthConfigChanged, pub.events[0].EventType)
	assert.Equal(t, "auth_config", pub.events[0].ResourceType)
	assert.Equal(t, uuid.Nil, pub.events[0].ResourceID)
}

// TestEntitlementService_SetOpenMode_NilPublisher 验证 publisher 为 nil 时
// 不 panic，DB 仍更新。
func TestEntitlementService_SetOpenMode_NilPublisher(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.addGroup(1, "gpt-4o", "public", true)

	svc := service.NewEntitlementService(&fakeEntitlementRepo{}, gRepo, nil)

	err := svc.SetOpenMode(context.Background(), 1, "controlled")

	assert.NoError(t, err)
	assert.Len(t, gRepo.updateCalls, 1)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Grant / Revoke
// ─────────────────────────────────────────────────────────────────────────────

// TestEntitlementService_Grant_InvalidScopeType 验证无效 scope_type 返回 error，
// 不写 DB、不发事件。
func TestEntitlementService_Grant_InvalidScopeType(t *testing.T) {
	pub := &fakeBudgetEventPublisher{}
	svc := service.NewEntitlementService(&fakeEntitlementRepo{}, newFakeGroupRepo(), pub)

	err := svc.Grant(context.Background(), 1, "user", "user-001", uuid.New())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid scope_type")
	assert.Empty(t, pub.events)
}

// TestEntitlementService_Grant_StudioScope 验证 studio 范围授权的正常路径。
func TestEntitlementService_Grant_StudioScope(t *testing.T) {
	eRepo := &fakeEntitlementRepo{}
	pub := &fakeBudgetEventPublisher{}
	grantedBy := uuid.New()

	svc := service.NewEntitlementService(eRepo, newFakeGroupRepo(), pub)

	err := svc.Grant(context.Background(), 1, model.EntitlementScopeStudio, "studio-1", grantedBy)

	assert.NoError(t, err)
	require.Len(t, eRepo.upsertCalls, 1)
	assert.Equal(t, uint(1), eRepo.upsertCalls[0].GroupID)
	assert.Equal(t, model.EntitlementScopeStudio, eRepo.upsertCalls[0].ScopeType)
	assert.Equal(t, "studio-1", eRepo.upsertCalls[0].ScopeID)
	assert.True(t, eRepo.upsertCalls[0].Enabled)
	assert.Equal(t, grantedBy, eRepo.upsertCalls[0].GrantedBy)

	// 事件发布
	require.Len(t, pub.events, 1)
	assert.Equal(t, event.AuthConfigChanged, pub.events[0].EventType)
}

// TestEntitlementService_Grant_DeptScope 验证部门范围授权的正常路径。
func TestEntitlementService_Grant_DeptScope(t *testing.T) {
	eRepo := &fakeEntitlementRepo{}
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewEntitlementService(eRepo, newFakeGroupRepo(), pub)

	err := svc.Grant(context.Background(), 1, model.EntitlementScopeDept, "dept-001", uuid.New())

	assert.NoError(t, err)
	assert.Equal(t, model.EntitlementScopeDept, eRepo.upsertCalls[0].ScopeType)
	assert.Len(t, pub.events, 1)
}

// TestEntitlementService_Grant_UpsertFails 验证 DB 失败时返回 error，不发事件。
func TestEntitlementService_Grant_UpsertFails(t *testing.T) {
	eRepo := &fakeEntitlementRepo{upsertErr: fmt.Errorf("db err")}
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewEntitlementService(eRepo, newFakeGroupRepo(), pub)

	err := svc.Grant(context.Background(), 1, model.EntitlementScopeDept, "d-1", uuid.New())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "grant entitlement")
	assert.Empty(t, pub.events)
}

// TestEntitlementService_Revoke_Success 验证撤销路径：DB revoke + 广播事件。
func TestEntitlementService_Revoke_Success(t *testing.T) {
	eRepo := &fakeEntitlementRepo{}
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewEntitlementService(eRepo, newFakeGroupRepo(), pub)

	err := svc.Revoke(context.Background(), 1, model.EntitlementScopeDept, "dept-001")

	assert.NoError(t, err)
	require.Len(t, eRepo.revokeCalls, 1)
	assert.Equal(t, uint(1), eRepo.revokeCalls[0].GroupID)
	assert.Equal(t, model.EntitlementScopeDept, eRepo.revokeCalls[0].ScopeType)
	assert.Len(t, pub.events, 1)
}

// TestEntitlementService_Revoke_DBFails 验证 DB 失败时返回 error，不发事件。
func TestEntitlementService_Revoke_DBFails(t *testing.T) {
	eRepo := &fakeEntitlementRepo{revokeErr: fmt.Errorf("db err")}
	pub := &fakeBudgetEventPublisher{}

	svc := service.NewEntitlementService(eRepo, newFakeGroupRepo(), pub)

	err := svc.Revoke(context.Background(), 1, model.EntitlementScopeDept, "d-1")

	assert.Error(t, err)
	assert.Empty(t, pub.events)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: UsableGroupNames (核心级联逻辑)
// ─────────────────────────────────────────────────────────────────────────────

// TestEntitlementService_UsableGroupNames_OnlyPublic 验证无 deptID/studioID 时
// 只返回 public 组。
func TestEntitlementService_UsableGroupNames_OnlyPublic(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.addGroup(1, "gpt-4o", "public", true)
	gRepo.addGroup(2, "claude-opus", "controlled", true)
	gRepo.idsByOpenMode["public"] = []uint{1}

	eRepo := &fakeEntitlementRepo{controlledIDs: []uint{2}} // 部门已开通 claude-opus
	svc := service.NewEntitlementService(eRepo, gRepo, nil)

	// 注意：deptID 和 studioID 都为空
	names, err := svc.UsableGroupNames(context.Background(), "", "")

	assert.NoError(t, err)
	assert.Equal(t, []string{"gpt-4o"}, names, "无 deptID 应只返回 public 组")
	// 验证没调用 controlled 路径
}

// TestEntitlementService_UsableGroupNames_MissingStudio 验证 studioID 为空时
// 只返回 public（不跑受控组查询，因为级联校验需要 studioID）。
func TestEntitlementService_UsableGroupNames_MissingStudio(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.addGroup(1, "gpt-4o", "public", true)
	gRepo.idsByOpenMode["public"] = []uint{1}

	eRepo := &fakeEntitlementRepo{}
	svc := service.NewEntitlementService(eRepo, gRepo, nil)

	names, err := svc.UsableGroupNames(context.Background(), "dept-001", "" /*空 studio*/)

	assert.NoError(t, err)
	assert.Equal(t, []string{"gpt-4o"}, names)
}

// TestEntitlementService_UsableGroupNames_ControlledCascadeEnabled 验证 dept+studio
// 都 enabled 时，受控组正确返回。
func TestEntitlementService_UsableGroupNames_ControlledCascadeEnabled(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.addGroup(1, "gpt-4o", "public", true)
	gRepo.addGroup(2, "claude-opus", "controlled", true)
	gRepo.idsByOpenMode["public"] = []uint{1}

	// 模拟：dept-001 在 studio-1 开通下开通了 claude-opus（受控组 ID=2），且 studio-1 已下放
	eRepo := &fakeEntitlementRepo{controlledIDs: []uint{2}}
	svc := service.NewEntitlementService(eRepo, gRepo, nil)

	names, err := svc.UsableGroupNames(context.Background(), "dept-001", "studio-1")

	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"gpt-4o", "claude-opus"}, names)
}

// TestEntitlementService_UsableGroupNames_Dedupe 验证同一 group_id 同时出现在
// public 和 controlled 时只返回一次。
func TestEntitlementService_UsableGroupNames_Dedupe(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.addGroup(1, "gpt-4o", "public", true) // group_id=1 是 public
	// 但同一 group_id=1 也出现在 controlled 的返回（理论上不应该，但测代码的健壮性）
	gRepo.idsByOpenMode["public"] = []uint{1}

	eRepo := &fakeEntitlementRepo{controlledIDs: []uint{1}} // 重复
	svc := service.NewEntitlementService(eRepo, gRepo, nil)

	names, err := svc.UsableGroupNames(context.Background(), "dept-001", "studio-1")

	assert.NoError(t, err)
	require.Len(t, names, 1, "去重后应只出现 1 次 gpt-4o")
	assert.Equal(t, "gpt-4o", names[0])
}

// TestEntitlementService_UsableGroupNames_Empty 验证没有任何组时返回空切片（非 nil）。
func TestEntitlementService_UsableGroupNames_Empty(t *testing.T) {
	gRepo := newFakeGroupRepo()
	// 不预设 public 也不预设 controlled
	eRepo := &fakeEntitlementRepo{}
	svc := service.NewEntitlementService(eRepo, gRepo, nil)

	names, err := svc.UsableGroupNames(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, names, "应返回空切片而非 nil")
	assert.Empty(t, names)
}

// TestEntitlementService_UsableGroupNames_PublicQueryFails 验证 public 组查询失败
// 时向上传播错误。
func TestEntitlementService_UsableGroupNames_PublicQueryFails(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.idsByOpenModeErr = fmt.Errorf("db err")
	svc := service.NewEntitlementService(&fakeEntitlementRepo{}, gRepo, nil)

	_, err := svc.UsableGroupNames(context.Background(), "", "")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "list public groups")
}

// TestEntitlementService_UsableGroupNames_ControlledQueryFails 验证受控组查询失败
// 时向上传播错误（deptID 和 studioID 都非空时才触发）。
func TestEntitlementService_UsableGroupNames_ControlledQueryFails(t *testing.T) {
	gRepo := newFakeGroupRepo()
	gRepo.idsByOpenMode["public"] = []uint{1}
	gRepo.addGroup(1, "gpt-4o", "public", true)

	eRepo := &fakeEntitlementRepo{controlledErr: fmt.Errorf("db err")}
	svc := service.NewEntitlementService(eRepo, gRepo, nil)

	_, err := svc.UsableGroupNames(context.Background(), "dept-001", "studio-1")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "list controlled groups")
}

// TestEntitlementService_ListByGroup_Delegates 验证 ListByGroup 简单委托给 repo。
func TestEntitlementService_ListByGroup_Delegates(t *testing.T) {
	expected := []model.GroupEntitlement{
		{GroupID: 1, ScopeType: "studio", ScopeID: "s-1", Enabled: true},
		{GroupID: 1, ScopeType: "dept", ScopeID: "d-1", Enabled: true},
	}
	eRepo := &fakeEntitlementRepo{findByGroupResult: expected}

	svc := service.NewEntitlementService(eRepo, newFakeGroupRepo(), nil)

	result, err := svc.ListByGroup(context.Background(), 1)

	assert.NoError(t, err)
	assert.Equal(t, expected, result)
	assert.Equal(t, []uint{1}, eRepo.findByGroupCalls)
}
