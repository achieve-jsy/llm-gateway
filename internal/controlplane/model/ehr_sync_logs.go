package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EhrSyncExecutionLog struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	BatchID       string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_ehr_exec_batch" json:"batch_id"`
	Entity        string     `gorm:"type:varchar(32);not null;index:idx_ehr_exec_entity" json:"entity"`
	OrgID         string     `gorm:"type:varchar(64);not null;index:idx_ehr_exec_org" json:"org_id"`
	SyncType      string     `gorm:"type:varchar(16);not null;index:idx_ehr_exec_type" json:"sync_type"`
	Status        string     `gorm:"type:varchar(16);not null;index:idx_ehr_exec_status" json:"status"`
	TriggerType   string     `gorm:"type:varchar(16);not null;default:cron" json:"trigger_type"`
	TriggerUser   string     `gorm:"type:varchar(128);not null;default:''" json:"trigger_user"`
	StartedAt     time.Time  `gorm:"not null" json:"started_at"`
	FinishedAt    *time.Time `gorm:"type:timestamp with time zone" json:"finished_at,omitempty"`
	DurationMs    int64      `gorm:"type:bigint;not null;default:0" json:"duration_ms"`
	RecordsFetched int64     `gorm:"type:bigint;not null;default:0" json:"records_fetched"`
	RecordsCreated int64     `gorm:"type:bigint;not null;default:0" json:"records_created"`
	RecordsUpdated int64     `gorm:"type:bigint;not null;default:0" json:"records_updated"`
	RecordsSkipped int64     `gorm:"type:bigint;not null;default:0" json:"records_skipped"`
	RecordsDeleted int64     `gorm:"type:bigint;not null;default:0" json:"records_deleted"`
	ErrorMsg      string     `gorm:"type:text;not null;default:''" json:"error_msg"`
	CreatedAt     time.Time  `gorm:"not null;autoCreateTime;index:idx_ehr_exec_created" json:"created_at"`
}

func (EhrSyncExecutionLog) TableName() string { return "ehr_sync_execution_logs" }

func (m *EhrSyncExecutionLog) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type EhrSyncChangeLog struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	BatchID     string    `gorm:"type:varchar(64);not null;index:idx_ehr_change_batch" json:"batch_id"`
	Entity      string    `gorm:"type:varchar(32);not null;index:idx_ehr_change_entity" json:"entity"`
	EntityID    string    `gorm:"type:varchar(64);not null;index:idx_ehr_change_entity_id" json:"entity_id"`
	OrgID       string    `gorm:"type:varchar(64);not null;index:idx_ehr_change_org" json:"org_id"`
	ChangeType  string    `gorm:"type:varchar(16);not null;index:idx_ehr_change_type" json:"change_type"`
	OldValue    string    `gorm:"type:jsonb;not null;default:'{}'" json:"old_value"`
	NewValue    string    `gorm:"type:jsonb;not null;default:'{}'" json:"new_value"`
	Source      string    `gorm:"type:varchar(16);not null" json:"source"`
	OccurredAt  time.Time `gorm:"not null;index:idx_ehr_change_occurred" json:"occurred_at"`
}

func (EhrSyncChangeLog) TableName() string { return "ehr_sync_change_logs" }

func (m *EhrSyncChangeLog) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type EhrSyncErrorLog struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	BatchID     string    `gorm:"type:varchar(64);not null;index:idx_ehr_err_batch" json:"batch_id"`
	Entity      string    `gorm:"type:varchar(32);not null;index:idx_ehr_err_entity" json:"entity"`
	OrgID       string    `gorm:"type:varchar(64);not null;index:idx_ehr_err_org" json:"org_id"`
	Method      string    `gorm:"type:varchar(255);not null" json:"method"`
	QueryParams string    `gorm:"type:text;not null;default:''" json:"query_params"`
	StatusCode  int       `gorm:"type:bigint;not null;default:0" json:"status_code"`
	Response    string    `gorm:"type:text;not null;default:''" json:"response"`
	ErrorMsg    string    `gorm:"type:text;not null" json:"error_msg"`
	AttemptNum  int       `gorm:"type:bigint;not null;default:1" json:"attempt_num"`
	MaxRetries  int       `gorm:"type:bigint;not null;default:3" json:"max_retries"`
	BackoffMs   int       `gorm:"type:bigint;not null;default:0" json:"backoff_ms"`
	StackTrace  string    `gorm:"type:text;not null;default:''" json:"stack_trace"`
	OccurredAt  time.Time `gorm:"not null;index:idx_ehr_err_occurred" json:"occurred_at"`
}

func (EhrSyncErrorLog) TableName() string { return "ehr_sync_error_logs" }

func (m *EhrSyncErrorLog) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type EhrSyncDiffReport struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	BatchID         string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_ehr_diff_batch" json:"batch_id"`
	OrgID           string    `gorm:"type:varchar(64);not null;index:idx_ehr_diff_org" json:"org_id"`
	Entity          string    `gorm:"type:varchar(32);not null;index:idx_ehr_diff_entity" json:"entity"`
	TotalInEHR      int64     `gorm:"type:bigint;not null;default:0" json:"total_in_ehr"`
	TotalInLocal    int64     `gorm:"type:bigint;not null;default:0" json:"total_in_local"`
	CreatedCount    int64     `gorm:"type:bigint;not null;default:0" json:"created_count"`
	ModifiedCount   int64     `gorm:"type:bigint;not null;default:0" json:"modified_count"`
	MissingCount    int64     `gorm:"type:bigint;not null;default:0" json:"missing_count"`
	ConsistentCount int64     `gorm:"type:bigint;not null;default:0" json:"consistent_count"`
	OverrideCount   int64     `gorm:"type:bigint;not null;default:0" json:"override_count"`
	Status          string    `gorm:"type:varchar(16);not null" json:"status"`
	Message         string    `gorm:"type:text;not null;default:''" json:"message"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime;index:idx_ehr_diff_created" json:"created_at"`
}

func (EhrSyncDiffReport) TableName() string { return "ehr_sync_diff_reports" }

func (m *EhrSyncDiffReport) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type EhrSyncSkipLog struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	BatchID         string    `gorm:"type:varchar(64);not null;index:idx_ehr_skip_batch" json:"batch_id"`
	Entity          string    `gorm:"type:varchar(32);not null;index:idx_ehr_skip_entity" json:"entity"`
	EntityID        string    `gorm:"type:varchar(64);not null;index:idx_ehr_skip_entity_id" json:"entity_id"`
	OrgID           string    `gorm:"type:varchar(64);not null;index:idx_ehr_skip_org" json:"org_id"`
	OverrideAt      time.Time `gorm:"not null" json:"override_at"`
	OverrideBy      string    `gorm:"type:varchar(128);not null;default:''" json:"override_by"`
	LocalValue      string    `gorm:"type:jsonb;not null;default:'{}'" json:"local_value"`
	EhrValue        string    `gorm:"type:jsonb;not null;default:'{}'" json:"ehr_value"`
	DifferingFields string    `gorm:"type:jsonb;not null;default:'[]'" json:"differing_fields"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime;index:idx_ehr_skip_created" json:"created_at"`
}

func (EhrSyncSkipLog) TableName() string { return "ehr_sync_skip_logs" }

func (m *EhrSyncSkipLog) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type EhrSyncStatSnapshot struct {
	ID                    uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID                 string    `gorm:"type:varchar(64);not null;index:idx_ehr_stat_org" json:"org_id"`
	SyncedAt              time.Time `gorm:"not null;index:idx_ehr_stat_synced" json:"synced_at"`
	TotalOrganizations    int64     `gorm:"type:bigint;not null;default:0" json:"total_organizations"`
	TotalCompanies        int64     `gorm:"type:bigint;not null;default:0" json:"total_companies"`
	TotalDepartments      int64     `gorm:"type:bigint;not null;default:0" json:"total_departments"`
	TotalEmployees        int64     `gorm:"type:bigint;not null;default:0" json:"total_employees"`
	ActiveEmployees       int64     `gorm:"type:bigint;not null;default:0" json:"active_employees"`
	OverrideCount         int64     `gorm:"type:bigint;not null;default:0" json:"override_count"`
	PendingReviewCount    int64     `gorm:"type:bigint;not null;default:0" json:"pending_review_count"`
	LastIncrementalMs     int64     `gorm:"type:bigint;not null;default:0" json:"last_incremental_ms"`
	LastFullReconcileMs   int64     `gorm:"type:bigint;not null;default:0" json:"last_full_reconcile_ms"`
	EHRCallCount          int64     `gorm:"type:bigint;not null;default:0" json:"ehr_call_count"`
	EHRErrorCount         int64     `gorm:"type:bigint;not null;default:0" json:"ehr_error_count"`
}

func (EhrSyncStatSnapshot) TableName() string { return "ehr_sync_stat_snapshots" }

func (m *EhrSyncStatSnapshot) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
