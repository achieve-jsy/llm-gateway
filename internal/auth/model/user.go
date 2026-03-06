package model

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Sub         string     `gorm:"type:varchar(128);not null;uniqueIndex:uniq_users_sub" json:"sub"`
	DeptID      *uuid.UUID `gorm:"type:uuid;index:idx_users_dept_id" json:"dept_id,omitempty"`
	Name        string     `gorm:"type:varchar(128);not null" json:"name"`
	Email       string     `gorm:"type:varchar(256);not null;index:idx_users_email" json:"email"`
	Source      string     `gorm:"type:varchar(32);not null;default:oidc" json:"source"`
	Role        string     `gorm:"type:varchar(16);not null;default:user" json:"role"`
	// HR 扩展字段，通过员工花名册导入写入
	EmployeeID  string     `gorm:"type:varchar(64);index:idx_users_employee_id" json:"employee_id,omitempty"`
	JobFamily   string     `gorm:"type:varchar(128)" json:"job_family,omitempty"`
	JobCategory string     `gorm:"type:varchar(128)" json:"job_category,omitempty"`
	JobTitle    string     `gorm:"type:varchar(128)" json:"job_title,omitempty"`
	// 部门归属来源：sso（跟随 SSO 自动）/ manual（手动指定，不被 SSO 覆盖）
	DeptSource    string     `gorm:"type:varchar(16);not null;default:sso" json:"dept_source"`
	DeptUpdatedBy string     `gorm:"type:varchar(128)" json:"dept_updated_by,omitempty"`
	DeptUpdatedAt *time.Time `gorm:"type:timestamptz" json:"dept_updated_at,omitempty"`
	// 登录状态：active（已登录）/ pending（花名册导入，尚未 SSO 登录）
	LoginStatus   string     `gorm:"type:varchar(16);not null;default:active" json:"login_status"`
	// 四层配额体系（Phase 1）
	// NULL = 继承全局默认 default_user；0 = 显式不限
	QuotaRPM            *int `gorm:"column:quota_rpm" json:"quota_rpm,omitempty"`
	QuotaMaxConcurrency *int `gorm:"column:quota_max_concurrency" json:"quota_max_concurrency,omitempty"`
	CreatedAt     time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (User) TableName() string { return "users" }
