package model

import (
	"time"

	"github.com/google/uuid"
)

// OffhoursFilingSubjectType offhours_filing.subject_type 枚举
const (
	OffhoursFilingSubjectUser = "user"
	OffhoursFilingSubjectDept = "dept"
	OffhoursFilingSubjectKey  = "key"
)

// OffhoursFilingType offhours_filing.filing_type 枚举
// 参考 docs/token-usage-anomaly-work-unrelated-design.md §2.2/§5
const (
	OffhoursFilingTypeBatchJob   = "批处理"
	OffhoursFilingTypeTimezone   = "跨时区"
	OffhoursFilingTypeReleaseDuty = "发布值班"
)

// OffhoursFiling 非工作时段消耗备案表（批处理/跨时区/发布值班），命中后从 unrelated_cost 中排除，避免误伤。
// 参考 docs/token-usage-analysis-report-implementation.md §10、docs/token-usage-anomaly-work-unrelated-design.md §2.2/§5
// confirmed_ok（加班/跨时区/值班）可一键沉淀为本表记录，下次自动排除（越查越准）。
type OffhoursFiling struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	SubjectType string    `gorm:"type:varchar(8);not null;index:idx_offhours_filing_subject;check:subject_type IN ('user','dept','key')" json:"subject_type"`
	SubjectID   string    `gorm:"type:text;not null;index:idx_offhours_filing_subject" json:"subject_id"`
	StartAt     time.Time `gorm:"type:timestamptz;not null;index:idx_offhours_filing_window" json:"start_at"`
	EndAt       time.Time `gorm:"type:timestamptz;not null" json:"end_at"`
	FilingType  string    `gorm:"column:filing_type;type:varchar(16);not null" json:"filing_type"`
	Note        string    `gorm:"type:text;not null;default:''" json:"note,omitempty"`
	CreatedBy   string    `gorm:"type:text;not null;default:''" json:"created_by,omitempty"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
}

func (OffhoursFiling) TableName() string { return "offhours_filing" }
