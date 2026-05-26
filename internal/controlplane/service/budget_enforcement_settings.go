package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

const (
	SettingKeyBudgetMaster             = "budget_master_enabled"
	SettingKeyBudgetAccounting         = "budget_accounting_enabled"
	SettingKeyBudgetPositiveEnforcement = "budget_positive_enforcement_enabled"
	SettingKeyBudgetZeroBlock           = "budget_zero_block_enabled"
	SettingKeyBudgetShadow              = "budget_shadow_enabled"
	SettingKeyBudgetV2TotalEnforcement  = "budget_v2_total_enforcement_enabled"
	SettingKeyBudgetLegacyTotalWrite    = "budget_legacy_total_write_enabled"
)

var (
	ErrBudgetShadowAndPositive = errors.New("影子模式与正向拦截不能同时开启")
	ErrBudgetMasterDisabled    = errors.New("预算总开关未开启（master_enabled），无法启用拦截类开关")
)

// BudgetEnforcementSeed 部署配置中的拦截默认值（仅在 DB 尚无对应 key 时写入）。
type BudgetEnforcementSeed struct {
	MasterEnabled              bool
	AccountingEnabled          bool
	PositiveEnforcementEnabled bool
	ZeroBlockEnabled           bool
	ShadowEnabled              bool
	V2TotalEnforcementEnabled  bool
	LegacyTotalWriteEnabled    bool
}

type budgetEnforcementPublisher interface {
	UpdateEnforcement(BudgetEnforcementFlags)
	MarkDirtyAndPublish(context.Context) error
}

func budgetEnforcementSettingKeys() []string {
	return []string{
		SettingKeyBudgetMaster,
		SettingKeyBudgetAccounting,
		SettingKeyBudgetPositiveEnforcement,
		SettingKeyBudgetZeroBlock,
		SettingKeyBudgetShadow,
		SettingKeyBudgetV2TotalEnforcement,
		SettingKeyBudgetLegacyTotalWrite,
	}
}

// WithBudgetEnforcement 注入 snapshot publisher，供设置页热切换。
func (s *SettingsService) WithBudgetEnforcement(publisher budgetEnforcementPublisher) *SettingsService {
	s.budgetPublisher = publisher
	return s
}

// SeedBudgetEnforcementDefaults 仅补齐缺失 key，已有 DB 值不被部署 env 覆盖。
func (s *SettingsService) SeedBudgetEnforcementDefaults(seed BudgetEnforcementSeed) {
	existing, _ := s.repo.GetMultiple(budgetEnforcementSettingKeys())
	updates := map[string]string{}
	setIfMissing := func(key string, value bool) {
		if _, ok := existing[key]; !ok {
			updates[key] = fmt.Sprintf("%t", value)
		}
	}
	setIfMissing(SettingKeyBudgetMaster, seed.MasterEnabled)
	setIfMissing(SettingKeyBudgetAccounting, seed.AccountingEnabled)
	setIfMissing(SettingKeyBudgetPositiveEnforcement, seed.PositiveEnforcementEnabled)
	setIfMissing(SettingKeyBudgetZeroBlock, seed.ZeroBlockEnabled)
	setIfMissing(SettingKeyBudgetShadow, seed.ShadowEnabled)
	setIfMissing(SettingKeyBudgetV2TotalEnforcement, seed.V2TotalEnforcementEnabled)
	setIfMissing(SettingKeyBudgetLegacyTotalWrite, seed.LegacyTotalWriteEnabled)
	if len(updates) > 0 {
		_ = s.repo.SetMultiple(updates)
	}
}

func validateBudgetEnforcement(settings *model.SystemSettings) error {
	if settings == nil {
		return nil
	}
	if settings.BudgetShadowEnabled && settings.BudgetPositiveEnforcementEnabled {
		return ErrBudgetShadowAndPositive
	}
	anyOn := settings.BudgetPositiveEnforcementEnabled ||
		settings.BudgetZeroBlockEnabled ||
		settings.BudgetShadowEnabled ||
		settings.BudgetV2TotalEnforcementEnabled
	if anyOn && !settings.BudgetMasterEnabled {
		return ErrBudgetMasterDisabled
	}
	return nil
}

func (s *SettingsService) publishBudgetEnforcement(ctx context.Context, settings *model.SystemSettings) error {
	if s.budgetPublisher == nil || settings == nil {
		return nil
	}
	s.budgetPublisher.UpdateEnforcement(BudgetEnforcementFlags{
		MasterEnabled:              settings.BudgetMasterEnabled,
		AccountingEnabled:          settings.BudgetAccountingEnabled || settings.BudgetPositiveEnforcementEnabled || settings.BudgetShadowEnabled,
		PositiveEnforcementEnabled: settings.BudgetPositiveEnforcementEnabled,
		ZeroBlockEnabled:           settings.BudgetZeroBlockEnabled,
		ShadowEnabled:              settings.BudgetShadowEnabled,
		V2TotalEnforcementEnabled:  settings.BudgetV2TotalEnforcementEnabled,
		LegacyTotalWriteEnabled:    settings.BudgetLegacyTotalWriteEnabled,
	})
	if err := s.budgetPublisher.MarkDirtyAndPublish(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrBudgetConfigSyncPending, err)
	}
	return nil
}
