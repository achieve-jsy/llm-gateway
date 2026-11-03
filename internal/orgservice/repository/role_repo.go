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
	if err != nil || len(roles) > 0 {
		return roles, err
	}
	legacyRole, legacyErr := r.legacyUserRole(userID)
	if legacyErr != nil || legacyRole == "" || legacyRole == string(model.RoleUser) {
		return roles, legacyErr
	}
	return []model.UserRole{{UserID: userID, Role: model.RoleType(legacyRole), ScopeType: model.ScopeSelf, ScopeID: ""}}, nil
}

func (r *RoleRepo) legacyUserRole(userID string) (string, error) {
	var row struct{ Role string }
	if err := r.db.Table("users").Select("role").Where("id = ?", userID).Limit(1).Scan(&row).Error; err != nil {
		return "", err
	}
	if row.Role != "" {
		return row.Role, nil
	}
	if err := r.db.Table("dev_users").Select("role").Where("id = ?", userID).Limit(1).Scan(&row).Error; err != nil {
		return "", err
	}
	return row.Role, nil
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

// SyncUserRoleField 撤销角色后同步 users.role 字段：
//   - 若该用户在 user_roles 中已无任何管理角色（admin/studio_admin/dept_admin，不含 user），
//     则将 users 表 role 置为 'user'，保证下次 SSO 登录签发的 JWT 不再携带 admin；
//   - 若用户在 users 表查不到（dev 账号），兼容回退更新 dev_users 表（与 legacyUserRole 双表逻辑一致）；
//   - 若用户仍持有任意管理角色，保持 users.role 不动，避免误伤多角色用户。
func (r *RoleRepo) SyncUserRoleField(userID string) error {
	// 1. 查询 user_roles 中是否仍存在管理角色
	var count int64
	if err := r.db.Model(&model.UserRole{}).
		Where("user_id = ? AND role != ?", userID, model.RoleUser).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// 2. 无管理角色：优先更新 users 表，未命中（dev 账号）回退 dev_users 表
	update := r.db.Table("users").Where("id = ?", userID).Update("role", string(model.RoleUser))
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected > 0 {
		return nil
	}
	return r.db.Table("dev_users").Where("id = ?", userID).Update("role", string(model.RoleUser)).Error
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
