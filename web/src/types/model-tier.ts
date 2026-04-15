import type { BudgetScopeType } from './budget'
import type { ExpansionPeriod, MoneyUSD } from './expansion'

export interface ModelTier {
  id: string
  name: string
  description: string
  color: string
  sort_order: number
  enabled: boolean
  archived_at: string | null
  group_count: number
  budget_count: number
  history_count: number
  reference_count: number
  created_at: string
  updated_at: string
}

export interface ModelTierInput {
  name: string
  description: string
  color: string
}

export type TierLimitState = 'unconfigured' | 'enabled' | 'zero_blocked' | 'disabled'

export interface TierPeriodUsage {
  period_type: ExpansionPeriod
  configured: boolean
  base_limit_usd?: MoneyUSD
  temporary_target_usd?: MoneyUSD
  effective_limit_usd?: MoneyUSD
  used_usd?: MoneyUSD
  remaining_usd?: MoneyUSD
  reset_at: string
  status: TierLimitState | string
  limiting_source: {
    scope_type: BudgetScopeType
    remaining_usd?: MoneyUSD
  }
}

export interface MyBudgetSourceSummary {
  mode: 'personal' | 'shared' | 'mixed'
  primary_scope_type: BudgetScopeType
  has_shared_pool: boolean
  has_temporary_override: boolean
}

export interface EffectiveTotalPeriod {
  period_type: ExpansionPeriod
  configured: boolean
  base_limit_usd?: MoneyUSD
  temporary_target_usd?: MoneyUSD
  effective_limit_usd?: MoneyUSD
  used_usd?: MoneyUSD
  remaining_usd?: MoneyUSD
  reset_at: string
  status: string
}

export interface BudgetChainNode {
  scope_type: BudgetScopeType
  label: string
  visible_amounts: boolean
  periods?: EffectiveTotalPeriod[]
}

export interface MyBudgetTierLimit {
  model_tier_id: string
  model_tier_name: string
  enabled: boolean
  periods: TierPeriodUsage[]
}

export interface MyBudgetTiersResponse {
  source_summary: MyBudgetSourceSummary
  effective_total: EffectiveTotalPeriod[]
  budget_chain: BudgetChainNode[]
  tier_limits: MyBudgetTierLimit[]
  data_updated_at: string
  stale: boolean
}

export interface BudgetTierLimit {
  id?: string
  budget_allocation_id: string
  model_tier_id: string
  model_tier_name?: string
  period_type: ExpansionPeriod
  limit_usd: MoneyUSD
  enabled: boolean
  state?: TierLimitState
  used_usd?: MoneyUSD
}

export interface BudgetTierUsage {
  allocation_id: string
  model_tier_id: string
  period_type: ExpansionPeriod
  used_usd: MoneyUSD
  data_as_of: string
  stale: boolean
}

export interface TierLimitAdjustment {
  budget_allocation_id: string
  model_tier_id: string
  period_type: ExpansionPeriod
  target_limit_usd: MoneyUSD
  enabled: boolean
}

export interface TierAdjustmentPreview {
  preview_token: string
  expires_at: string
  data_as_of: string
  stale: boolean
  items: Array<{
    item_id: string
    old_limit_usd?: MoneyUSD
    new_limit_usd: MoneyUSD
    current_used_usd: MoneyUSD
    below_used: boolean
    zero_block: boolean
    stale: boolean
    status: string
    error_code?: string
  }>
}