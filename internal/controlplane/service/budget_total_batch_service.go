package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TotalBatchObject struct {
	ItemID         string `json:"item_id"`
	ScopeType      string `json:"scope_type"`
	ScopeID        string `json:"scope_id"`
	PreviewVersion string `json:"preview_version,omitempty"`
}

type TotalBatchInput struct {
	Operation              string                  `json:"operation"`
	Direction              string                  `json:"direction"`
	Value                  string                  `json:"value"`
	PeriodType             budgetdomain.PeriodType `json:"period_type"`
	EffectiveMode          EffectiveMode           `json:"effective_mode"`
	Reason                 string                  `json:"reason"`
	ActorID                uuid.UUID               `json:"-"`
	ConfirmZeroBlock       bool                    `json:"confirm_zero_block"`
	ConfirmBelowUsed       bool                    `json:"confirm_below_used"`
	ConfirmReplaceSchedule bool                    `json:"confirm_replace_schedule"`
	Items                  []TotalBatchObject      `json:"items"`
}

type TotalBatchItemResult struct {
	ItemID         string                  `json:"item_id"`
	TargetLimitUSD string                  `json:"target_limit_usd"`
	Impact         *BudgetAdjustmentImpact `json:"impact"`
}

type TotalBatchResult struct {
	Items []TotalBatchItemResult `json:"items"`
}

func (s *BudgetService) PreviewTotalBatch(ctx context.Context, input TotalBatchInput) (*TotalBatchResult, error) {
	return s.runTotalBatch(ctx, nil, input, true)
}

func (s *BudgetService) ApplyTotalBatch(ctx context.Context, input TotalBatchInput) (*TotalBatchResult, error) {
	dbProvider, ok := s.repo.(interface{ DB() *gorm.DB })
	if !ok {
		return nil, fmt.Errorf("atomic budget batch repository not configured")
	}
	var result *TotalBatchResult
	err := dbProvider.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txSvc := NewBudgetService(repository.NewBudgetRepo(tx), s.alertRepo)
		txSvc.rdb, txSvc.publisher = s.rdb, s.publisher
		txSvc.scheduler = repository.NewBudgetTierRepo(tx)
		var err error
		result, err = txSvc.runTotalBatch(ctx, tx, input, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *BudgetService) runTotalBatch(ctx context.Context, tx *gorm.DB, input TotalBatchInput, preview bool) (*TotalBatchResult, error) {
	if input.Operation != "set_fixed" && input.Operation != "adjust_percent" {
		return nil, fmt.Errorf("invalid batch operation")
	}
	value, err := decimal.NewFromString(input.Value)
	if err != nil || value.IsNegative() {
		return nil, fmt.Errorf("invalid batch value")
	}
	items := append([]TotalBatchObject(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].ScopeType+":"+items[i].ScopeID < items[j].ScopeType+":"+items[j].ScopeID
	})
	result := &TotalBatchResult{Items: make([]TotalBatchItemResult, 0, len(items))}
	for _, item := range items {
		allocation, err := s.repo.GetAllocation(item.ScopeType, item.ScopeID)
		if err != nil {
			return nil, err
		}
		current := decimal.NewFromInt(-1)
		if allocation != nil {
			current = totalAllocationLimit(*allocation, input.PeriodType)
		} else if input.Operation != "set_fixed" || input.EffectiveMode == EffectiveNextPeriod {
			return nil, ErrBudgetAllocationNotFound
		}
		target := value
		if input.Operation == "adjust_percent" {
			ratio := value.Div(decimal.NewFromInt(100))
			if input.Direction == "decrease" {
				target = current.Mul(decimal.NewFromInt(1).Sub(ratio))
			} else {
				target = current.Mul(decimal.NewFromInt(1).Add(ratio))
			}
			if target.IsNegative() {
				target = decimal.Zero
			}
		}
		adjust := BudgetAdjustmentInput{
			ScopeType: item.ScopeType, ScopeID: item.ScopeID, PeriodType: input.PeriodType,
			TargetLimitUSD: budgetdomain.Amount{Decimal: target.Round(6)}, EffectiveMode: input.EffectiveMode,
			Reason: input.Reason, ActorID: input.ActorID, PreviewOnly: preview,
			BudgetRiskConfirmation: BudgetRiskConfirmation{
				PreviewVersion: item.PreviewVersion, ConfirmZeroBlock: input.ConfirmZeroBlock,
				ConfirmBelowUsed: input.ConfirmBelowUsed, ConfirmReplaceSchedule: input.ConfirmReplaceSchedule,
			},
		}
		impact, err := s.AdjustBudget(ctx, adjust)
		if err != nil {
			return nil, fmt.Errorf("batch item %s: %w", item.ItemID, err)
		}
		result.Items = append(result.Items, TotalBatchItemResult{ItemID: item.ItemID, TargetLimitUSD: target.Round(6).StringFixed(6), Impact: impact})
	}
	_ = tx
	return result, nil
}
