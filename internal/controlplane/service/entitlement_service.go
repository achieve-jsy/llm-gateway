package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

var (
	ErrEntitlementNotFound  = errors.New("entitlement not found")
	ErrInvalidOpenMode      = errors.New("open_mode must be 'public' or 'controlled'")
	ErrEntitlementForbidden = errors.New("insufficient permission to manage entitlement")
)

// EntitlementService 处理模型组开放模式设置与两级授权（工作室+部门）。
type EntitlementService struct {
	entRepo   *repository.EntitlementRepo
	groupRepo *repository.GroupRepo
	publisher EventPublisher
}

func NewEntitlementService(
	entRepo *repository.EntitlementRepo,
	groupRepo *repository.GroupRepo,
	publisher EventPublisher,
) *EntitlementService {
	return &EntitlementService{
		entRepo:   entRepo,
		groupRepo: groupRepo,
		publisher: publisher,
	}
}

// SetOpenMode 设置模型组的开放模式（平台管理员）。
// openMode: "public" | "controlled"
func (s *EntitlementService) SetOpenMode(ctx context.Context, groupID uint, openMode string) error {
	if openMode != model.GroupOpenModePublic && openMode != model.GroupOpenModeControlled {
		return ErrInvalidOpenMode
	}
	g, err := s.groupRepo.FindByID(groupID)
	if err != nil || g == nil {
		return ErrGroupNotFound // 复用 group_service.go 中定义的 ErrGroupNotFound
	}
	if err := s.groupRepo.UpdateOpenMode(groupID, openMode); err != nil {
		return fmt.Errorf("update open_mode: %w", err)
	}
	// 授权变更：失效相关 Key 缓存（由调用方决定是否需要，此处不强制）
	return nil
}

// Grant 下放/开通授权：平台授权工作室（scope_type=studio）或工作室/负责人授权部门（scope_type=dept）。
func (s *EntitlementService) Grant(ctx context.Context, groupID uint, scopeType, scopeID string, grantedBy uuid.UUID) error {
	if scopeType != model.EntitlementScopeStudio && scopeType != model.EntitlementScopeDept {
		return fmt.Errorf("invalid scope_type: %s", scopeType)
	}
	e := &model.GroupEntitlement{
		GroupID:   groupID,
		ScopeType: scopeType,
		ScopeID:   scopeID,
		Enabled:   true,
		GrantedBy: grantedBy,
	}
	if err := s.entRepo.Upsert(e); err != nil {
		return fmt.Errorf("grant entitlement: %w", err)
	}
	return nil
}

// Revoke 撤销授权（软撤销，enabled=false）。
// 平台撤销 studio 授权后，下属部门的 dept 行因级联判断即时失效，无需逐行清理。
func (s *EntitlementService) Revoke(ctx context.Context, groupID uint, scopeType, scopeID string) error {
	return s.entRepo.Revoke(groupID, scopeType, scopeID)
}

// ListByGroup 查询某模型组的所有授权记录。
func (s *EntitlementService) ListByGroup(ctx context.Context, groupID uint) ([]model.GroupEntitlement, error) {
	return s.entRepo.FindByGroup(groupID)
}

// UsableGroupNames 计算某用户（部门+工作室维度）可用的模型组名称集合：
//
//	可用 = 公开组（open_mode=public）∪ 本部门已开通的受控组
//
// 返回组名称集合（string slice），供 verify-key 快照下发。
// 如果 deptID 或 studioID 为空则只返回公开组。
func (s *EntitlementService) UsableGroupNames(ctx context.Context, deptID, studioID string) ([]string, error) {
	// 1. 取所有公开组 ID
	pubIDs, err := s.groupRepo.IDsByOpenMode(model.GroupOpenModePublic)
	if err != nil {
		return nil, fmt.Errorf("list public groups: %w", err)
	}

	// 2. 取本部门已开通的受控组 ID（级联判断：dept enabled AND studio enabled）
	var ctlIDs []uint
	if deptID != "" && studioID != "" {
		ctlIDs, err = s.entRepo.EnabledControlledGroupIDs(deptID, studioID)
		if err != nil {
			return nil, fmt.Errorf("list controlled groups: %w", err)
		}
	}

	// 3. 合并去重
	seen := make(map[uint]struct{})
	allIDs := make([]uint, 0, len(pubIDs)+len(ctlIDs))
	for _, id := range pubIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			allIDs = append(allIDs, id)
		}
	}
	for _, id := range ctlIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			allIDs = append(allIDs, id)
		}
	}

	if len(allIDs) == 0 {
		return []string{}, nil
	}

	// 4. 查组名
	var groups []model.Group
	if err := s.groupRepo.DB().Where("id IN ? AND enabled = true AND deleted_at IS NULL", allIDs).
		Select("id, name").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("fetch group names: %w", err)
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names, nil
}
