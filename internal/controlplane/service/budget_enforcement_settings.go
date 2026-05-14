package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

const (
	SettingKeyBudgetPositiveEnforcement = "budget_positive_enforcement_enabled"
	SettingKeyBudgetZeroBlock           = "budget_zero_block_enabled"
	SettingKeyBudgetShadow              = "budget_shadow_enabled"
	SettingKeyBudgetV2TotalEnforcement  = "budget_v2_total_enforcement_enabled"
)

var (
	ErrBudgetShadowAndPositive = errors.New("影子模式与正向拦截不能同时开启")
	ErrBudgetMasterDisabled    = errors.New("预算总开关未开启（master_enabled），无法启用拦截类开关")
)

// BudgetEnforcementSeed 部署配置中的拦截默认值（仅在 DB 尚无对应 key 时写入）。
type BudgetEnforcementSeed struct {
	MasterEnabled              bool
	PositiveEnforcementEnabled bool
	ZeroBlockEnabled           bool
	ShadowEnabled              bool
	V2TotalEnforcementEnabled  bool
}

type budgetEnforcementPublisher interface {
	UpdateEnforcement(BudgetEnforcementFlags)
	MarkDirtyAndPublish(context.Context) error
}

func budgetEnforcementSettingKeys() []string {
	return []string{
		SettingKeyBudgetPositiveEnforcement,
		SettingKeyBudgetZeroBlock,
		SettingKeyBudgetShadow,
		SettingKeyBudgetV2TotalEnforcement,
	}
}

// WithBudgetEnforcement 注入部署层 master 与 snapshot publisher，供设置页热切换。
func (s *SettingsService) WithBudgetEnforcement(masterEnabled bool, publisher budgetEnforcementPublisher) *SettingsService {
	s.budgetMasterEnabled = masterEnabled
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
	setIfMissing(SettingKeyBudgetPositiveEnforcement, seed.PositiveEnforcementEnabled)
	setIfMissing(SettingKeyBudgetZeroBlock, seed.ZeroBlockEnabled)
	setIfMissing(SettingKeyBudgetShadow, seed.ShadowEnabled)
	setIfMissing(SettingKeyBudgetV2TotalEnforcement, seed.V2TotalEnforcementEnabled)
	if len(updates) > 0 {
		_ = s.repo.SetMultiple(updates)
	}
}

func validateBudgetEnforcement(settings *model.SystemSettings, masterEnabled bool) error {
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
	if anyOn && !masterEnabled {
		return ErrBudgetMasterDisabled
	}
	return nil
}

func (s *SettingsService) publishBudgetEnforcement(ctx context.Context, settings *model.SystemSettings) error {
	if s.budgetPublisher == nil || settings == nil {
		return nil
	}
	s.budgetPublisher.UpdateEnforcement(BudgetEnforcementFlags{
		PositiveEnforcementEnabled: settings.BudgetPositiveEnforcementEnabled,
		ZeroBlockEnabled:           settings.BudgetZeroBlockEnabled,
		ShadowEnabled:              settings.BudgetShadowEnabled,
		V2TotalEnforcementEnabled:  settings.BudgetV2TotalEnforcementEnabled,
	})
	if err := s.budgetPublisher.MarkDirtyAndPublish(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrBudgetConfigSyncPending, err)
	}
	return nil
}
