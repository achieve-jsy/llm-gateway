package repository

import (
	"errors"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EntitlementRepo struct {
	db *gorm.DB
}

func NewEntitlementRepo(db *gorm.DB) *EntitlementRepo {
	return &EntitlementRepo{db: db}
}

func (r *EntitlementRepo) Create(ent *model.GroupEntitlement) error {
	return r.db.Create(ent).Error
}

func (r *EntitlementRepo) FindByID(id uuid.UUID) (*model.GroupEntitlement, error) {
	var ent model.GroupEntitlement
	if err := r.db.First(&ent, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &ent, nil
}

func (r *EntitlementRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&model.GroupEntitlement{}, "id = ?", id).Error
}

func (r *EntitlementRepo) SetEnabled(id uuid.UUID, enabled bool) error {
	return r.db.Model(&model.GroupEntitlement{}).Where("id = ?", id).Update("enabled", enabled).Error
}

func (r *EntitlementRepo) ListByGroup(groupID uint, scopeType string) ([]model.GroupEntitlement, error) {
	var ents []model.GroupEntitlement
	query := r.db.Where("group_id = ?", groupID)
	if scopeType != "" {
		query = query.Where("scope_type = ?", scopeType)
	}
	err := query.Order("created_at DESC").Find(&ents).Error
	return ents, err
}

func (r *EntitlementRepo) ListByScope(scopeType, scopeID string) ([]model.GroupEntitlement, error) {
	var ents []model.GroupEntitlement
	err := r.db.Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).
		Order("created_at DESC").Find(&ents).Error
	return ents, err
}

func (r *EntitlementRepo) FindOne(groupID uint, scopeType, scopeID string) (*model.GroupEntitlement, error) {
	var ent model.GroupEntitlement
	err := r.db.Where("group_id = ? AND scope_type = ? AND scope_id = ?", groupID, scopeType, scopeID).
		First(&ent).Error
	if err != nil {
		return nil, err
	}
	return &ent, nil
}

// EnabledControlledGroupIDs 级联判定：返回同时存在 enabled dept 行 + enabled studio 行的 controlled 组 ID
func (r *EntitlementRepo) EnabledControlledGroupIDs(deptID, studioID string) ([]uint, error) {
	if deptID == "" || studioID == "" {
		return nil, nil
	}
	var groupIDs []uint
	err := r.db.Raw(`
		SELECT d.group_id
		FROM group_entitlements d
		JOIN group_entitlements s
		  ON d.group_id = s.group_id
		  AND s.scope_type = 'studio'
		  AND s.scope_id = ?
		  AND s.enabled = true
		JOIN model_groups g
		  ON d.group_id = g.id
		  AND g.open_mode = 'controlled'
		  AND g.enabled = true
		  AND g.deleted_at IS NULL
		WHERE d.scope_type = 'dept'
		  AND d.scope_id = ?
		  AND d.enabled = true
	`, studioID, deptID).Pluck("group_id", &groupIDs).Error
	if err != nil {
		return nil, err
	}
	return groupIDs, nil
}

// FindKeyIDsByDeptOrStudio 查询指定部门或工作室下所有已启用 API Key 的 ID（UUID）。
// 用于授权变更时发布 key_updated 事件，使 gateway 本地 KeyCache 主动失效。
func (r *EntitlementRepo) FindKeyIDsByDeptOrStudio(deptID, studioID string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	query := r.db.Model(&model.ApiKey{}).Where("enabled = ?", true)
	if deptID != "" && studioID != "" {
		query = query.Where("dept_id = ? OR studio_id = ?", deptID, studioID)
	} else if deptID != "" {
		query = query.Where("dept_id = ?", deptID)
	} else if studioID != "" {
		query = query.Where("studio_id = ?", studioID)
	} else {
		return nil, nil
	}
	err := query.Pluck("id", &ids).Error
	return ids, err
}

// FindStudioName 根据工作室 UUID 查询工作室名称。
func (r *EntitlementRepo) FindStudioName(studioID uuid.UUID) (string, error) {
	var name string
	err := r.db.Table("studios").Where("id = ?", studioID).Pluck("name", &name).Error
	if err != nil {
		return "", err
	}
	return name, nil
}

// FindDeptName 根据 SSO dept_id 查询部门名称。
func (r *EntitlementRepo) FindDeptName(deptID string) (string, error) {
	var name string
	err := r.db.Table("departments").Where("dept_id = ?", deptID).Pluck("dept_name", &name).Error
	if err != nil {
		return "", err
	}
	return name, nil
}

// FindUserName 根据用户 UUID 查询用户姓名。
// 优先查 users 表，未找到时 fallback 到 dev_users 表（返回 username）。
func (r *EntitlementRepo) FindUserName(userID uuid.UUID) (string, error) {
	var name string
	err := r.db.Table("users").Where("id = ?", userID).Pluck("name", &name).Error
	if err == nil && name != "" {
		return name, nil
	}
	err = r.db.Table("dev_users").Where("id = ?", userID).Pluck("username", &name).Error
	if err != nil {
		return "", err
	}
	return name, nil
}

var ErrRecordNotFound = errors.New("record not found")
