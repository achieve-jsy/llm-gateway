package repository

import (
	"context"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type BudgetRiskRepo struct {
	db *gorm.DB
}

func (r *BudgetRiskRepo) ListAlertRecords(
	ctx context.Context,
	scopeTypes, scopeIDs, periodTypes []string,
) ([]model.BudgetAlertRecord, error) {
	items := make([]model.BudgetAlertRecord, 0)
	query := r.db.WithContext(ctx).Preload("ModelTier")
	if len(scopeTypes) > 0 {
		query = query.Where("scope_type IN ?", scopeTypes)
	}
	if len(scopeIDs) > 0 {
		query = query.Where("scope_id IN ?", scopeIDs)
	}
	if len(periodTypes) > 0 {
		query = query.Where("period_type IN ?", periodTypes)
	}
	err := query.Order("notified_at DESC").Find(&items).Error
	return items, err
}

func NewBudgetRiskRepo(db *gorm.DB) *BudgetRiskRepo {
	return &BudgetRiskRepo{db: db}
}

type BlockedRequestQuery struct {
	From        time.Time
	To          time.Time
	ScopeTypes  []string
	ScopeIDs    []string
	PeriodTypes []string
}

type BlockedRequestAggregate struct {
	Code           string
	DimensionType  string
	ScopeType      string
	ScopeID        string
	PeriodType     string
	ModelTierID    *uuid.UUID
	Count          int64
	FirstBlockedAt time.Time
	LastBlockedAt  time.Time
}

func (r *BudgetRiskRepo) AggregateBlockedRequests(
	ctx context.Context,
	query BlockedRequestQuery,
) ([]BlockedRequestAggregate, error) {
	type aggregateRow struct {
		Code, DimensionType, ScopeType, ScopeID, PeriodType string
		ModelTierID                                         *string
		Count                                               int64
		FirstBlockedAt, LastBlockedAt                       string
	}
	rawRows := make([]aggregateRow, 0)
	db := r.db.WithContext(ctx).Table("call_logs").
		Select(`budget_block_code AS code,
			budget_block_dimension AS dimension_type,
			budget_block_scope_type AS scope_type,
			budget_block_scope_id AS scope_id,
			budget_block_period_type AS period_type,
			CASE WHEN budget_block_dimension = 'tier' THEN budget_tier_id ELSE NULL END AS model_tier_id,
			COUNT(*) AS count, MIN(ts) AS first_blocked_at,
			MAX(ts) AS last_blocked_at`).
		Where("status = ?", 402).
		Where("ts >= ? AND ts < ?", query.From, query.To).
		Where("budget_block_code IN ?", []string{"budget_exceeded", "tier_budget_exceeded"}).
		Where("budget_block_dimension IN ?", []string{"total", "tier"}).
		Where("(budget_block_dimension <> 'tier' OR budget_tier_id IS NOT NULL)").
		Where("budget_block_period_type IN ?", []string{"daily", "weekly", "monthly"})
	if len(query.ScopeTypes) > 0 {
		db = db.Where("budget_block_scope_type IN ?", query.ScopeTypes)
	}
	if len(query.ScopeIDs) > 0 {
		db = db.Where("budget_block_scope_id IN ?", query.ScopeIDs)
	}
	if len(query.PeriodTypes) > 0 {
		db = db.Where("budget_block_period_type IN ?", query.PeriodTypes)
	}
	err := db.Group(`budget_block_code, budget_block_dimension,
			budget_block_scope_type, budget_block_scope_id,
			budget_block_period_type,
			CASE WHEN budget_block_dimension = 'tier' THEN budget_tier_id ELSE NULL END`).
		Order("first_blocked_at DESC").
		Scan(&rawRows).Error
	if err != nil {
		return nil, err
	}
	rows := make([]BlockedRequestAggregate, 0, len(rawRows))
	for _, raw := range rawRows {
		var tierID *uuid.UUID
		if raw.ModelTierID != nil {
			if parsed, parseErr := uuid.Parse(*raw.ModelTierID); parseErr == nil {
				tierID = &parsed
			}
		}
		rows = append(rows, BlockedRequestAggregate{
			Code: raw.Code, DimensionType: raw.DimensionType, ScopeType: raw.ScopeType,
			ScopeID: raw.ScopeID, PeriodType: raw.PeriodType, ModelTierID: tierID,
			Count: raw.Count, FirstBlockedAt: parseDatabaseTime(raw.FirstBlockedAt),
			LastBlockedAt: parseDatabaseTime(raw.LastBlockedAt),
		})
	}
	return rows, nil
}

func parseDatabaseTime(raw string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
