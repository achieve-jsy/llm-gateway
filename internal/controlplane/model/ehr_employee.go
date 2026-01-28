package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EhrEmployee struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID              string     `gorm:"type:varchar(64);not null;index:idx_ehr_emp_org_id" json:"org_id"`
	EmployeeID         string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_ehr_emp_org_emp" json:"employee_id"`
	PersonID           string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_emp_person_id" json:"person_id"`
	Passport           string     `gorm:"type:varchar(128);not null;default:'';uniqueIndex:idx_ehr_emp_passport" json:"passport"`
	AppointName        string     `gorm:"type:varchar(128);not null;default:''" json:"appoint_name"`
	AppointCode        string     `gorm:"type:varchar(64);not null;default:''" json:"appoint_code"`
	Office             string     `gorm:"type:varchar(128);not null;default:''" json:"office"`
	Email              string     `gorm:"type:varchar(255);not null;default:''" json:"email"`
	PersonStatus       int32      `gorm:"not null;default:-1;index:idx_ehr_emp_status" json:"person_status"`
	PersonType         int32      `gorm:"not null;default:0;index:idx_ehr_emp_type" json:"person_type"`
	IsDefault          bool       `gorm:"not null;default:false" json:"is_default"`
	CompanyID          string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_emp_company" json:"company_id"`
	CompanyName        string     `gorm:"type:varchar(255);not null;default:''" json:"company_name"`
	DepartmentID       string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_emp_dept" json:"department_id"`
	DepartmentName     string     `gorm:"type:varchar(255);not null;default:''" json:"department_name"`
	PositionID         string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_emp_position" json:"position_id"`
	PositionName       string     `gorm:"type:varchar(128);not null;default:''" json:"position_name"`
	PositionCategoryID string     `gorm:"type:varchar(64);not null;default:''" json:"position_category_id"`
	ManagerEmployeeID  string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_emp_manager" json:"manager_employee_id"`
	ManagerPersonID    string     `gorm:"type:varchar(64);not null;default:''" json:"manager_person_id"`
	ManagerPassport    string     `gorm:"type:varchar(128);not null;default:''" json:"manager_passport"`
	ManagerName        string     `gorm:"type:varchar(128);not null;default:''" json:"manager_name"`
	PersonName         string     `gorm:"type:varchar(128);not null;default:''" json:"person_name"`
	GroupENumber       string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_emp_enumber" json:"group_e_number"`
	Gender             int32      `gorm:"not null;default:-1" json:"gender"`
	JoinDate           string     `gorm:"type:varchar(32);not null;default:''" json:"join_date"`
	OnBoardDate        string     `gorm:"type:varchar(32);not null;default:''" json:"on_board_date"`
	Location           string     `gorm:"type:varchar(128);not null;default:''" json:"location"`
	DismissDate        string     `gorm:"type:varchar(32);not null;default:''" json:"dismiss_date"`
	EhrModifyDate      string     `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_emp_modify_date" json:"ehr_modify_date"`
	ManualOverrideAt   *time.Time `gorm:"column:manual_override_at;type:timestamp with time zone;serializer:tztime" json:"manual_override_at,omitempty"`
	ManualOverrideBy   string     `gorm:"type:varchar(128);not null;default:''" json:"manual_override_by"`
	ManualOverrideReason string   `gorm:"type:varchar(500);not null;default:''" json:"manual_override_reason"`
	SyncedAt           time.Time  `gorm:"not null;autoCreateTime" json:"synced_at"`
	CreatedAt          time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (EhrEmployee) TableName() string { return "ehr_employees" }

// GetOverride 满足 ehrsync.HasOverride 接口，用于 skip entry 填充 override 时间
func (m *EhrEmployee) GetOverride() *time.Time { return m.ManualOverrideAt }

func (m *EhrEmployee) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
