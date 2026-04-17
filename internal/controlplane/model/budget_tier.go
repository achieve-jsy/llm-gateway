package model

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/google/uuid"
)

type BudgetTierLimit struct {
	ID                 uuid.UUID               `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BudgetAllocationID uuid.UUID               `gorm:"type:uuid;not null;uniqueIndex:uniq_budget_tier_limit" json:"budget_allocation_id"`
	ModelTierID        uuid.UUID               `gorm:"type:uuid;not null;uniqueIndex:uniq_budget_tier_limit" json:"model_tier_id"`
	PeriodType         budgetdomain.PeriodType `gorm:"type:varchar(8);not null;uniqueIndex:uniq_budget_tier_limit;check:period_type IN ('daily','weekly','monthly')" json:"period_type"`
	LimitUSD           budgetdomain.Amount     `gorm:"type:numeric(18,6);not null;check:limit_usd >= 0" json:"limit_usd"`
	EffectiveAt        time.Time               `gorm:"not null" json:"effective_at"`
	Enabled            bool                    `gorm:"not null;default:true" json:"enabled"`
	BudgetAllocation   BudgetAllocation        `gorm:"foreignKey:BudgetAllocationID;constraint:OnDelete:RESTRICT" json:"-"`
	ModelTier          ModelTier               `gorm:"foreignKey:ModelTierID;constraint:OnDelete:RESTRICT" json:"-"`
	CreatedAt          time.Time               `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time               `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (BudgetTierLimit) TableName() string {
	return "budget_tier_limits"
}

type BudgetTemporaryOverride struct {
	ID                 uuid.UUID                   `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BudgetAllocationID uuid.UUID                   `gorm:"type:uuid;not null;uniqueIndex:uniq_budget_override_active,where:status = 'active'" json:"budget_allocation_id"`
	DimensionType      budgetdomain.DimensionType  `gorm:"type:varchar(8);not null;check:dimension_type IN ('total','tier')" json:"dimension_type"`
	DimensionKey       string                      `gorm:"type:varchar(48);not null;uniqueIndex:uniq_budget_override_active,where:status = 'active';check:chk_budget_override_dimension_key,(dimension_type = 'total' AND dimension_key = 'total') OR (dimension_type = 'tier' AND dimension_key = ('tier:' || model_tier_id::text))" json:"dimension_key"`
	ModelTierID        *uuid.UUID                  `gorm:"type:uuid;index;check:chk_budget_override_dimension,(dimension_type = 'total' AND model_tier_id IS NULL) OR (dimension_type = 'tier' AND model_tier_id IS NOT NULL)" json:"model_tier_id,omitempty"`
	PeriodType         budgetdomain.PeriodType     `gorm:"type:varchar(8);not null;uniqueIndex:uniq_budget_override_active,where:status = 'active';check:period_type IN ('daily','weekly','monthly')" json:"period_type"`
	TargetLimitUSD     budgetdomain.Amount         `gorm:"type:numeric(18,6);not null;check:target_limit_usd >= 0" json:"target_limit_usd"`
	StartsAt           time.Time                   `gorm:"not null" json:"starts_at"`
	ExpiresAt          time.Time                   `gorm:"not null;index;check:chk_budget_override_window,expires_at > starts_at" json:"expires_at"`
	ExpansionRequestID *uuid.UUID                  `gorm:"type:uuid;uniqueIndex" json:"expansion_request_id,omitempty"`
	Status             budgetdomain.OverrideStatus `gorm:"type:varchar(16);not null;default:'active';index;check:status IN ('active','expired','cancelled','failed')" json:"status"`
	FailureReason      *string                     `gorm:"type:text" json:"failure_reason,omitempty"`
	BudgetAllocation   BudgetAllocation            `gorm:"foreignKey:BudgetAllocationID;constraint:OnDelete:RESTRICT" json:"-"`
	ModelTier          *ModelTier                  `gorm:"foreignKey:ModelTierID;constraint:OnDelete:RESTRICT" json:"-"`
	CreatedAt          time.Time                   `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time                   `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (BudgetTemporaryOverride) TableName() string {
	return "budget_temporary_overrides"
}

type BudgetLimitSchedule struct {
	ID                 uuid.UUID                   `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BudgetAllocationID uuid.UUID                   `gorm:"type:uuid;not null;uniqueIndex:uniq_budget_schedule_pending,where:status = 'pending'" json:"budget_allocation_id"`
	DimensionType      budgetdomain.DimensionType  `gorm:"type:varchar(8);not null;check:dimension_type IN ('total','tier')" json:"dimension_type"`
	DimensionKey       string                      `gorm:"type:varchar(48);not null;uniqueIndex:uniq_budget_schedule_pending,where:status = 'pending';check:chk_budget_schedule_dimension_key,(dimension_type = 'total' AND dimension_key = 'total') OR (dimension_type = 'tier' AND dimension_key = ('tier:' || model_tier_id::text))" json:"dimension_key"`
	ModelTierID        *uuid.UUID                  `gorm:"type:uuid;index;check:chk_budget_schedule_dimension,(dimension_type = 'total' AND model_tier_id IS NULL) OR (dimension_type = 'tier' AND model_tier_id IS NOT NULL)" json:"model_tier_id,omitempty"`
	PeriodType         budgetdomain.PeriodType     `gorm:"type:varchar(8);not null;uniqueIndex:uniq_budget_schedule_pending,where:status = 'pending';check:period_type IN ('daily','weekly','monthly')" json:"period_type"`
	TargetLimitUSD     budgetdomain.Amount         `gorm:"type:numeric(18,6);not null;check:target_limit_usd >= 0" json:"target_limit_usd"`
	ScheduleOperation  budgetdomain.ScheduleOperation `gorm:"type:varchar(8);not null;default:'set';check:schedule_operation IN ('set','clear')" json:"schedule_operation"`
	EffectiveAt        time.Time                   `gorm:"not null;index" json:"effective_at"`
	Reason             string                      `gorm:"type:text;not null" json:"reason"`
	CreatedBy          uuid.UUID                   `gorm:"type:uuid;not null" json:"created_by"`
	Status             budgetdomain.ScheduleStatus `gorm:"type:varchar(16);not null;default:'pending';index;check:status IN ('pending','applied','replaced','cancelled','failed')" json:"status"`
	FailureReason      *string                     `gorm:"type:text" json:"failure_reason,omitempty"`
	BudgetAllocation   BudgetAllocation            `gorm:"foreignKey:BudgetAllocationID;constraint:OnDelete:RESTRICT" json:"-"`
	ModelTier          *ModelTier                  `gorm:"foreignKey:ModelTierID;constraint:OnDelete:RESTRICT" json:"-"`
	CreatedAt          time.Time                   `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time                   `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (BudgetLimitSchedule) TableName() string {
	return "budget_limit_schedules"
}
