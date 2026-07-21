package repository

import (
	"strings"

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

// RoleListFilter 角色授权列表的服务端筛选条件。
// Keyword 同时覆盖人员、邮箱、所属部门、角色和管辖范围，避免前端只在当前页做局部过滤。
type RoleListFilter struct {
	Keyword string
	Role    string
}

// RoleListCounts 是当前 actor 可见范围内的管理角色统计。
// 统计不受列表关键词和角色筛选影响，用于稳定展示页面概览。
type RoleListCounts struct {
	Admin       int64 `json:"admin"`
	StudioAdmin int64 `json:"studio_admin"`
	DeptAdmin   int64 `json:"dept_admin"`
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

// ListManagementRolesByActorPaged 在 actor 可见范围内执行服务端搜索和分页。
// 权限范围、关键词、角色筛选均在 COUNT 和分页前应用，避免 total 泄露范围外记录。
func (r *RoleRepo) ListManagementRolesByActorPaged(
	scopeFn func(*gorm.DB) *gorm.DB,
	filter RoleListFilter,
	page, pageSize int,
) ([]RoleWithUser, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	countQuery := r.managementRoleQuery(scopeFn, filter)
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []RoleWithUser{}, 0, nil
	}

	var rows []RoleWithUser
	createdAtSelect := "ur.created_at AS created_at"
	if r.db.Dialector != nil && r.db.Dialector.Name() == "postgres" {
		createdAtSelect = "ur.created_at::text AS created_at"
	}
	err := r.managementRoleQuery(scopeFn, filter).
		Select(`ur.id, ur.user_id, u.name AS user_name, u.email,
                COALESCE(d.dept_name,'') AS dept_name,
                ur.role, ur.scope_type, ur.scope_id,
                ur.granted_by, ` + createdAtSelect).
		Order("ur.created_at DESC, ur.id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&rows).Error
	return rows, total, err
}

// ListManagementRoleCountsByActor 返回 actor 可见范围内的完整角色统计。
func (r *RoleRepo) ListManagementRoleCountsByActor(scopeFn func(*gorm.DB) *gorm.DB) (RoleListCounts, error) {
	var grouped []struct {
		Role  string `gorm:"column:role"`
		Count int64  `gorm:"column:count"`
	}
	if err := r.managementRoleQuery(scopeFn, RoleListFilter{}).
		Select("ur.role, COUNT(*) AS count").
		Group("ur.role").
		Scan(&grouped).Error; err != nil {
		return RoleListCounts{}, err
	}

	counts := RoleListCounts{}
	for _, item := range grouped {
		switch item.Role {
		case string(model.RoleAdmin):
			counts.Admin = item.Count
		case string(model.RoleStudioAdmin):
			counts.StudioAdmin = item.Count
		case string(model.RoleDeptAdmin):
			counts.DeptAdmin = item.Count
		}
	}
	return counts, nil
}

func (r *RoleRepo) managementRoleQuery(scopeFn func(*gorm.DB) *gorm.DB, filter RoleListFilter) *gorm.DB {
	userJoin := "LEFT JOIN users u ON u.id::text = ur.user_id"
	if r.db.Dialector != nil && r.db.Dialector.Name() != "postgres" {
		userJoin = "LEFT JOIN users u ON CAST(u.id AS TEXT) = ur.user_id"
	}
	query := r.db.Table("user_roles ur").
		Joins(userJoin).
		Joins("LEFT JOIN departments d ON d.id = u.dept_id").
		Where("ur.role != ?", model.RoleUser)
	if scopeFn != nil {
		query = query.Scopes(scopeFn)
	}
	if role := strings.TrimSpace(filter.Role); role != "" {
		query = query.Where("ur.role = ?", role)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		pattern := "%" + strings.ToLower(keyword) + "%"
		if r.db.Dialector != nil && r.db.Dialector.Name() == "postgres" {
			query = query.Where(`
				COALESCE(u.name, '') ILIKE ? OR COALESCE(u.email, '') ILIKE ? OR
				COALESCE(d.dept_name, '') ILIKE ? OR COALESCE(ur.role, '') ILIKE ? OR
				COALESCE(ur.scope_id, '') ILIKE ?
			`, pattern, pattern, pattern, pattern, pattern)
		} else {
			query = query.Where(`
				LOWER(COALESCE(u.name, '')) LIKE ? OR LOWER(COALESCE(u.email, '')) LIKE ? OR
				LOWER(COALESCE(d.dept_name, '')) LIKE ? OR LOWER(COALESCE(ur.role, '')) LIKE ? OR
				LOWER(COALESCE(ur.scope_id, '')) LIKE ?
			`, pattern, pattern, pattern, pattern, pattern)
		}
	}
	return query
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
