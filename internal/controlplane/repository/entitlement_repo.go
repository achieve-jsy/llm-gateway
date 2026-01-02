package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// EntitlementRepo 处理模型组两级授权表的增删查。
type EntitlementRepo struct {
	db *gorm.DB
}

func NewEntitlementRepo(db *gorm.DB) *EntitlementRepo {
	return &EntitlementRepo{db: db}
}

// Upsert 创建或启用一条授权记录（已存在则置 enabled=true）。
func (r *EntitlementRepo) Upsert(e *model.GroupEntitlement) error {
	return r.db.Where(model.GroupEntitlement{
		GroupID:   e.GroupID,
		ScopeType: e.ScopeType,
		ScopeID:   e.ScopeID,
	}).Assign(model.GroupEntitlement{
		Enabled:   true,
		GrantedBy: e.GrantedBy,
	}).FirstOrCreate(e).Error
}

// Revoke 撤销授权：将 enabled 置为 false（软撤销，保留审计记录）。
func (r *EntitlementRepo) Revoke(groupID uint, scopeType, scopeID string) error {
	return r.db.Model(&model.GroupEntitlement{}).
		Where("group_id = ? AND scope_type = ? AND scope_id = ?", groupID, scopeType, scopeID).
		Update("enabled", false).Error
}

// FindByGroup 查询某模型组的所有授权记录（含 disabled）。
func (r *EntitlementRepo) FindByGroup(groupID uint) ([]model.GroupEntitlement, error) {
	var list []model.GroupEntitlement
	err := r.db.Where("group_id = ?", groupID).Find(&list).Error
	return list, err
}

// EnabledControlledGroupIDs 返回受控组中，满足「dept 级 enabled 且其 studio 级也 enabled」的组 ID 集合。
// deptID：SSO dept_id；studioID：工作室 UUID 字符串。
// 实现级联判断：先找 dept 级 enabled 行，再交叉验证 studio 级也 enabled。
func (r *EntitlementRepo) EnabledControlledGroupIDs(deptID, studioID string) ([]uint, error) {
	// 找出该部门已开通的受控组 group_id 集合
	var deptGroupIDs []uint
	err := r.db.Model(&model.GroupEntitlement{}).
		Where("scope_type = ? AND scope_id = ? AND enabled = true", model.EntitlementScopeDept, deptID).
		Pluck("group_id", &deptGroupIDs).Error
	if err != nil || len(deptGroupIDs) == 0 {
		return nil, err
	}

	// 在上述 group_id 集合中，过滤出工作室级也 enabled 的（级联校验）
	var result []uint
	err = r.db.Model(&model.GroupEntitlement{}).
		Where("group_id IN ? AND scope_type = ? AND scope_id = ? AND enabled = true",
			deptGroupIDs, model.EntitlementScopeStudio, studioID).
		Pluck("group_id", &result).Error
	return result, err
}
