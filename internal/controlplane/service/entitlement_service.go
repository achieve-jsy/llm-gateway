package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrEntitlementNotFound   = errors.New("entitlement not found")
	ErrEntitlementExists     = errors.New("entitlement already exists")
	ErrGroupMustBeControlled = errors.New("group must be controlled")
	ErrStudioNotGranted      = errors.New("studio must be granted first")
	ErrEntitlementForbidden  = errors.New("insufficient permission")
)

// EntitlementComputer 供 KeyService 消费的最小接口，避免循环依赖。
type EntitlementComputer interface {
	ComputeUsableGroups(ctx context.Context, deptID, studioID string) ([]string, error)
}

type EntitlementItem struct {
	ID            uuid.UUID  `json:"id"`
	GroupID       uint       `json:"group_id"`
	GroupName     string     `json:"group_name"`
	ScopeType     string     `json:"scope_type"`
	ScopeID       string     `json:"scope_id"`
	ScopeName     string     `json:"scope_name"`
	Enabled       bool       `json:"enabled"`
	GrantedBy     *uuid.UUID `json:"granted_by,omitempty"`
	GrantedByName string     `json:"granted_by_name,omitempty"`
	CreatedAt     string     `json:"created_at"`
}

// EntitlementGroupRepo 授权业务所需的组仓库能力。
type EntitlementGroupRepo interface {
	FindByID(id uint) (*model.Group, error)
	FindNamesByOpenMode(openMode string) ([]string, error)
	FindNamesByIDs(ids []uint) ([]string, error)
	UpdateOpenMode(id uint, openMode string) error
}

// EntitlementEntitlementRepo 授权业务所需的授权数据仓库能力。
type EntitlementEntitlementRepo interface {
	Create(ent *model.GroupEntitlement) error
	FindByID(id uuid.UUID) (*model.GroupEntitlement, error)
	Delete(id uuid.UUID) error
	SetEnabled(id uuid.UUID, enabled bool) error
	ListByGroup(groupID uint, scopeType string) ([]model.GroupEntitlement, error)
	FindOne(groupID uint, scopeType, scopeID string) (*model.GroupEntitlement, error)
	EnabledControlledGroupIDs(deptID, studioID string) ([]uint, error)
	FindKeyIDsByDeptOrStudio(deptID, studioID string) ([]uuid.UUID, error)
	FindStudioName(studioID uuid.UUID) (string, error)
	FindDeptName(deptID string) (string, error)
	FindUserName(userID uuid.UUID) (string, error)
}

// EntitlementEventPublisher 授权变更事件的发布能力。
type EntitlementEventPublisher interface {
	Publish(ctx context.Context, event *event.ChangeEvent) error
	InvalidateKeyAuthCache(ctx context.Context, keyHash string)
}

// EntitlementInvalidator 通知 gateway 本地 KeyCache 失效的能力（通过发布 key_updated 事件）。
type EntitlementInvalidator interface {
	Publish(ctx context.Context, event *event.ChangeEvent) error
}

type EntitlementService struct {
	repo      EntitlementEntitlementRepo
	groupRepo EntitlementGroupRepo
	publisher EntitlementEventPublisher
}

func NewEntitlementService(repo EntitlementEntitlementRepo, groupRepo EntitlementGroupRepo, publisher EntitlementEventPublisher) *EntitlementService {
	return &EntitlementService{
		repo:      repo,
		groupRepo: groupRepo,
		publisher: publisher,
	}
}

func (s *EntitlementService) SetOpenMode(ctx context.Context, groupID uint, openMode string, operatorID uuid.UUID) error {
	if openMode != "public" && openMode != "controlled" {
		return errors.New("invalid open_mode: must be 'public' or 'controlled'")
	}

	group, err := s.groupRepo.FindByID(groupID)
	if err != nil {
		return ErrGroupNotFound
	}

	prevMode := group.OpenMode

	if err := s.groupRepo.UpdateOpenMode(groupID, openMode); err != nil {
		return err
	}

	s.publishEvent(event.OpenModeChanged, "group", uuid.UUID{}, groupID)

	if prevMode == "controlled" && openMode == "public" {
		s.invalidateByGroup(ctx, groupID)
	}

	return nil
}

func (s *EntitlementService) GrantStudio(ctx context.Context, groupID uint, studioID string, grantedBy uuid.UUID) (*EntitlementItem, error) {
	group, err := s.groupRepo.FindByID(groupID)
	if err != nil {
		return nil, ErrGroupNotFound
	}
	if group.OpenMode != "controlled" {
		return nil, ErrGroupMustBeControlled
	}

	existing, err := s.repo.FindOne(groupID, model.EntitlementScopeStudio, studioID)
	if err == nil {
		if existing.Enabled {
			return nil, ErrEntitlementExists
		}
		if err := s.repo.SetEnabled(existing.ID, true); err != nil {
			return nil, err
		}
		existing.Enabled = true
		s.publishEvent(event.EntitlementGranted, "group_entitlement", existing.ID, groupID)
		s.invalidateByScope(ctx, studioID)
		return s.toItem(existing, group.Name), nil
	}

	ent := &model.GroupEntitlement{
		GroupID:   groupID,
		ScopeType: model.EntitlementScopeStudio,
		ScopeID:   studioID,
		Enabled:   true,
		GrantedBy: &grantedBy,
	}
	if err := s.repo.Create(ent); err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrEntitlementExists
		}
		return nil, err
	}

	s.publishEvent(event.EntitlementGranted, "group_entitlement", ent.ID, groupID)
	return s.toItem(ent, group.Name), nil
}

func (s *EntitlementService) GrantDept(ctx context.Context, groupID uint, deptID, studioID string, grantedBy uuid.UUID, actor *rbac.Actor) (*EntitlementItem, error) {
	group, err := s.groupRepo.FindByID(groupID)
	if err != nil {
		return nil, ErrGroupNotFound
	}
	if group.OpenMode != "controlled" {
		return nil, ErrGroupMustBeControlled
	}

	studioEnt, err := s.repo.FindOne(groupID, model.EntitlementScopeStudio, studioID)
	if err != nil || !studioEnt.Enabled {
		return nil, ErrStudioNotGranted
	}

	if !actor.IsGlobal && !actor.HasStudio(studioID) && !actor.HasDept(deptID) {
		return nil, ErrEntitlementForbidden
	}

	existing, err := s.repo.FindOne(groupID, model.EntitlementScopeDept, deptID)
	if err == nil {
		if existing.Enabled {
			return nil, ErrEntitlementExists
		}
		if err := s.repo.SetEnabled(existing.ID, true); err != nil {
			return nil, err
		}
		existing.Enabled = true
		s.publishEvent(event.EntitlementGranted, "group_entitlement", existing.ID, groupID)
		s.invalidateByScope(ctx, deptID)
		return s.toItem(existing, group.Name), nil
	}

	ent := &model.GroupEntitlement{
		GroupID:   groupID,
		ScopeType: model.EntitlementScopeDept,
		ScopeID:   deptID,
		Enabled:   true,
		GrantedBy: &grantedBy,
	}
	if err := s.repo.Create(ent); err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrEntitlementExists
		}
		return nil, err
	}

	s.publishEvent(event.EntitlementGranted, "group_entitlement", ent.ID, groupID)
	s.invalidateByScope(ctx, deptID)
	return s.toItem(ent, group.Name), nil
}

func (s *EntitlementService) Revoke(ctx context.Context, entitlementID uuid.UUID, operatorID uuid.UUID) error {
	ent, err := s.repo.FindByID(entitlementID)
	if err != nil {
		return ErrEntitlementNotFound
	}

	if err := s.repo.Delete(entitlementID); err != nil {
		return err
	}

	s.publishEvent(event.EntitlementRevoked, "group_entitlement", entitlementID, ent.GroupID)
	s.invalidateByScope(ctx, ent.ScopeID)
	return nil
}

func (s *EntitlementService) ListEntitlements(ctx context.Context, groupID uint, scopeType string) ([]EntitlementItem, error) {
	ents, err := s.repo.ListByGroup(groupID, scopeType)
	if err != nil {
		return nil, err
	}

	group, err := s.groupRepo.FindByID(groupID)
	if err != nil {
		return nil, ErrGroupNotFound
	}

	// 批量解析 scope_name 和 granted_by_name，避免 N+1 查询
	studioNames := make(map[uuid.UUID]string)
	deptNames := make(map[string]string)
	userNames := make(map[uuid.UUID]string)

	items := make([]EntitlementItem, 0, len(ents))
	for i := range ents {
		ent := &ents[i]
		scopeName := ""
		if ent.ScopeType == "studio" {
			if sid, err := uuid.Parse(ent.ScopeID); err == nil {
				if name, ok := studioNames[sid]; ok {
					scopeName = name
				} else if name, err := s.repo.FindStudioName(sid); err == nil {
					scopeName = name
					studioNames[sid] = name
				}
			}
		} else if ent.ScopeType == "dept" {
			if name, ok := deptNames[ent.ScopeID]; ok {
				scopeName = name
			} else if name, err := s.repo.FindDeptName(ent.ScopeID); err == nil {
				scopeName = name
				deptNames[ent.ScopeID] = name
			}
		}

		grantedByName := ""
		if ent.GrantedBy != nil {
			if name, ok := userNames[*ent.GrantedBy]; ok {
				grantedByName = name
			} else if name, err := s.repo.FindUserName(*ent.GrantedBy); err == nil {
				grantedByName = name
				userNames[*ent.GrantedBy] = name
			}
		}

		item := &EntitlementItem{
			ID:            ent.ID,
			GroupID:       ent.GroupID,
			GroupName:     group.Name,
			ScopeType:     ent.ScopeType,
			ScopeID:       ent.ScopeID,
			ScopeName:     scopeName,
			Enabled:       ent.Enabled,
			GrantedBy:     ent.GrantedBy,
			GrantedByName: grantedByName,
			CreatedAt:     ent.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *EntitlementService) ComputeUsableGroups(ctx context.Context, deptID, studioID string) ([]string, error) {
	publicNames, err := s.groupRepo.FindNamesByOpenMode("public")
	if err != nil {
		return nil, err
	}
	if deptID == "" || studioID == "" {
		return publicNames, nil
	}

	controlledIDs, err := s.repo.EnabledControlledGroupIDs(deptID, studioID)
	if err != nil {
		return nil, err
	}
	if len(controlledIDs) == 0 {
		return publicNames, nil
	}

	controlledNames, err := s.groupRepo.FindNamesByIDs(controlledIDs)
	if err != nil {
		return nil, err
	}
	return mergeUnique(publicNames, controlledNames), nil
}

// NotifyLocalCacheInvalidation 为指定 scope（deptID 或 studioID）下的所有已启用 API Key
// 发布 key_updated 事件，使 gateway 订阅者主动清除进程内 KeyCache 的对应条目。
// 与 publisher.InvalidateKeyAuthCache（仅删 Redis 缓存）互补，确保下次请求立即获取最新授权。
func NotifyLocalCacheInvalidation(ctx context.Context, invalidator EntitlementInvalidator, repo EntitlementEntitlementRepo, deptID, studioID string) {
	if invalidator == nil {
		return
	}
	keyIDs, err := repo.FindKeyIDsByDeptOrStudio(deptID, studioID)
	if err != nil {
		slog.Warn("entitlement: find key IDs for local cache invalidation failed", "dept_id", deptID, "studio_id", studioID, "error", err)
		return
	}
	for _, kid := range keyIDs {
		ev := event.NewChangeEvent(event.KeyUpdated, "api_key", kid, 0)
		if err := invalidator.Publish(ctx, ev); err != nil {
			slog.Warn("entitlement: publish key_updated event for local invalidation failed", "key_id", kid, "error", err)
		}
	}
}

func (s *EntitlementService) toItem(ent *model.GroupEntitlement, groupName string) *EntitlementItem {
	item := &EntitlementItem{
		ID:        ent.ID,
		GroupID:   ent.GroupID,
		GroupName: groupName,
		ScopeType: ent.ScopeType,
		ScopeID:   ent.ScopeID,
		Enabled:   ent.Enabled,
		GrantedBy: ent.GrantedBy,
		CreatedAt: ent.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	return item
}

func (s *EntitlementService) publishEvent(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID, groupID uint) {
	if s.publisher == nil {
		return
	}
	ev := event.NewChangeEvent(eventType, resourceType, resourceID, 0)
	if err := s.publisher.Publish(context.Background(), ev); err != nil {
		slog.Error("failed to publish entitlement event", "type", eventType, "group_id", groupID, "error", err)
	}
}

func (s *EntitlementService) invalidateByGroup(ctx context.Context, groupID uint) {
	if s.publisher == nil {
		return
	}
	ents, err := s.repo.ListByGroup(groupID, "")
	if err != nil {
		slog.Warn("entitlement: invalidate cache: list failed", "group_id", groupID, "error", err)
		return
	}
	seen := make(map[string]bool)
	for _, e := range ents {
		if seen[e.ScopeID] {
			continue
		}
		seen[e.ScopeID] = true
		NotifyLocalCacheInvalidation(ctx, s.publisher, s.repo, "", e.ScopeID)
	}
}

func (s *EntitlementService) invalidateByScope(ctx context.Context, scopeID string) {
	if s.publisher == nil || scopeID == "" {
		return
	}
	NotifyLocalCacheInvalidation(ctx, s.publisher, s.repo, "", scopeID)
}

func mergeUnique(a, b []string) []string {
	merged := make([]string, 0, len(a)+len(b))
	seen := make(map[string]bool, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}
	return merged
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
