package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID     uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Sub    string     `gorm:"type:varchar(128);not null;uniqueIndex:uniq_users_sub" json:"sub"`
	DeptID *uuid.UUID `gorm:"type:uuid;index:idx_users_dept_id" json:"dept_id,omitempty"`
	Name   string     `gorm:"type:varchar(128);not null" json:"name"`
	Email  string     `gorm:"type:varchar(256);not null;index:idx_users_email" json:"email"`
	Source string     `gorm:"type:varchar(32);not null;default:oidc" json:"source"`
	Role   string     `gorm:"type:varchar(16);not null;default:user" json:"role"`
	// HR 扩展字段，通过员工花名册导入写入
	EmployeeID  string `gorm:"type:varchar(64);index:idx_users_employee_id" json:"employee_id,omitempty"`
	JobFamily   string `gorm:"type:varchar(128)" json:"job_family,omitempty"`
	JobCategory string `gorm:"type:varchar(128)" json:"job_category,omitempty"`
	JobTitle    string `gorm:"type:varchar(128)" json:"job_title,omitempty"`
	// 部门归属来源：sso（跟随 SSO 自动）/ manual（手动指定，不被 SSO 覆盖）
	DeptSource    string     `gorm:"type:varchar(16);not null;default:sso" json:"dept_source"`
	DeptUpdatedBy string     `gorm:"type:varchar(128)" json:"dept_updated_by,omitempty"`
	DeptUpdatedAt *time.Time `gorm:"type:timestamptz" json:"dept_updated_at,omitempty"`
	// 登录状态：active（已登录）/ pending（花名册导入，尚未 SSO 登录）
	LoginStatus string `gorm:"type:varchar(16);not null;default:active" json:"login_status"`
	// 四层配额体系（Phase 1）
	// NULL = 继承全局默认 default_user；0 = 显式不限
	QuotaRPM            *int      `gorm:"column:quota_rpm" json:"quota_rpm,omitempty"`
	QuotaMaxConcurrency *int      `gorm:"column:quota_max_concurrency" json:"quota_max_concurrency,omitempty"`
	CreatedAt           time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
	// DeletedAt 软删除时间。EHR 花名册把离职人员软删：GORM 查询自动过滤，
	// 管理页/组织统计里直接消失，但行还在（审计、历史 Key、调用记录仍指得到）。
	// 注意：SSO 登录必须先按 Unscoped 查一次再决定是否新建，否则等于删了又建。
	DeletedAt gorm.DeletedAt `gorm:"index:idx_users_deleted_at" json:"-"`
}

func (User) TableName() string { return "users" }
