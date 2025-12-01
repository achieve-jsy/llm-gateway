package repository

import (
	"github.com/example/llm-gateway/internal/orgservice/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RoleRepo RBAC 角色授权数据访问
type RoleRepo struct {
	db *gorm.DB
}

func NewRoleRepo(db *gorm.DB) *RoleRepo {
	return &RoleRepo{db: db}
}

// GetByUserID 查询用户的所有角色（缓存层失效后调用）
func (r *RoleRepo) GetByUserID(userID string) ([]model.UserRole, error) {
	var roles []model.UserRole
	err := r.db.Where("user_id = ?", userID).Find(&roles).Error
	return roles, err
}

// Grant 幂等授予角色（ON CONFLICT DO NOTHING）
func (r *RoleRepo) Grant(role *model.UserRole) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "role"}, {Name: "scope_id"}},
		DoNothing: true,
	}).Create(role).Error
}

// Revoke 卸任角色
func (r *RoleRepo) Revoke(userID string, role model.RoleType, scopeID string) error {
	return r.db.
		Where("user_id = ? AND role = ? AND scope_id = ?", userID, role, scopeID).
		Delete(&model.UserRole{}).Error
}

// HasAdmins 检查系统是否存在任意 admin
func (r *RoleRepo) HasAdmins() (bool, error) {
	var count int64
	err := r.db.Model(&model.UserRole{}).Where("role = ?", model.RoleAdmin).Count(&count).Error
	return count > 0, err
}

// RoleWithUser 角色记录 + 用户信息（供列表展示）
type RoleWithUser struct {
	ID        int64  `gorm:"column:id" json:"id"`
	UserID    string `gorm:"column:user_id" json:"user_id"`
	UserName  string `gorm:"column:user_name" json:"user_name"`
	Email     string `gorm:"column:email" json:"email"`
	DeptName  string `gorm:"column:dept_name" json:"dept_name,omitempty"`
	Role      string `gorm:"column:role" json:"role"`
	ScopeType string `gorm:"column:scope_type" json:"scope_type"`
	ScopeID   string `gorm:"column:scope_id" json:"scope_id"`
	GrantedBy string `gorm:"column:granted_by" json:"granted_by"`
	CreatedAt string `gorm:"column:created_at" json:"created_at"`
}

// ListManagementRolesByActor 查询管理角色列表，由 scopeFn 按 actor 数据范围过滤。
// scopeFn 由 policy.OrgPolicy.RoleListScope(actor) 提供，无需 handler 层感知 SQL 细节。
// actor 参数已编码在 scopeFn 闭包中，不在此处直接引用，避免 repository↔rbac 循环依赖。
func (r *RoleRepo) ListManagementRolesByActor(scopeFn func(*gorm.DB) *gorm.DB) ([]RoleWithUser, error) {
	var rows []RoleWithUser
	q := r.db.Table("user_roles ur").
		Select(`ur.id, ur.user_id, u.name AS user_name, u.email,
                COALESCE(d.dept_name,'') AS dept_name,
                ur.role, ur.scope_type, ur.scope_id,
                ur.granted_by, ur.created_at::text AS created_at`).
		Joins("LEFT JOIN users u ON u.id::text = ur.user_id").
		Joins("LEFT JOIN departments d ON d.id = u.dept_id").
		Scopes(scopeFn).
		Order("ur.created_at DESC")
	err := q.Scan(&rows).Error
	return rows, err
}

// ListManagementRoles 查询所有管理角色（非 user），关联用户信息
func (r *RoleRepo) ListManagementRoles() ([]RoleWithUser, error) {
	var rows []RoleWithUser
	err := r.db.Raw(`
		SELECT ur.id, ur.user_id, u.name AS user_name, u.email,
		       COALESCE(d.dept_name,'') AS dept_name,
		       ur.role, ur.scope_type, ur.scope_id,
		       ur.granted_by,
		       ur.created_at::text AS created_at
		FROM user_roles ur
		LEFT JOIN users u ON u.id::text = ur.user_id
		LEFT JOIN departments d ON d.id = u.dept_id
		WHERE ur.role != 'user'
		ORDER BY ur.created_at DESC
	`).Scan(&rows).Error
	return rows, err
}
