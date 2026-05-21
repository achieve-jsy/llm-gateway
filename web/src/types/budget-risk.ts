import type { BudgetScopeType } from './budget'
import type { ExpansionDimension, ExpansionPeriod, MoneyUSD } from './expansion'

export type BudgetRiskType = 'threshold_reached' | 'budget_exhausted' | 'request_blocked'
export type BudgetRiskSeverity = 'warning' | 'critical'
export type BudgetRiskSortBy = 'usage_ratio' | 'blocked_requests' | 'detected_at'

export interface BudgetRiskEvent {
  id: string
  risk_type: BudgetRiskType
  severity: BudgetRiskSeverity
  scope_type: BudgetScopeType
  scope_id: string
  scope_name: string
  period_type: ExpansionPeriod
  dimension_type: ExpansionDimension
  model_tier_id: string | null
  model_tier_name: string
  effective_limit_usd: MoneyUSD
  used_usd: MoneyUSD
  usage_ratio: string
  limit_source: string
  blocked_requests: number
  detected_at: string
  data_as_of: string
  stale: boolean
}

export interface BudgetRiskFilter {
  scope_type?: BudgetScopeType
  scope_id?: string
  period_type?: ExpansionPeriod
  dimension_type?: ExpansionDimension
  model_tier_id?: string
  risk_type?: BudgetRiskType
  severity?: BudgetRiskSeverity
  q?: string
  sort_by?: BudgetRiskSortBy | ''
  sort_order?: 'asc' | 'desc' | ''
  page?: number
  page_size?: number
}

export interface BudgetRiskPage {
  items: BudgetRiskEvent[]
  total: number
  page?: number
  page_size?: number
  data_as_of: string
  stale: boolean
}

export interface BudgetRiskSummary {
  total: number
  warning: number
  critical: number
  by_type?: Partial<Record<BudgetRiskType, number>>
  blocked_requests_total?: number
  data_as_of: string
  stale: boolean
}
