
import type { BudgetScopeType } from './budget'

export type MoneyUSD = string
export type ExpansionStatus =
  | 'pending'
  | 'precheck_required'
  | 'pending_reconfirmation'
  | 'approved'
  | 'rejected'
  | 'cancelled'
  | 'expired'
export type ExpansionLevel = 'dept' | 'studio' | 'platform'
export type ExpansionAction = 'approve' | 'reject' | 'escalate' | 'cancel'
export type ExpansionPeriod = 'daily' | 'weekly' | 'monthly'
export type ExpansionDimension = 'total' | 'tier'
export type ExpansionDuration = 'permanent' | 'current_period'
export type ExpansionPriority = 'normal' | 'urgent'
export type ExpansionListTab = 'pending' | 'mine' | 'all'

export interface ExpansionListFilter {
  dimension: 'all' | ExpansionDimension
  modelTierId: 'all' | string
  duration: 'all' | ExpansionDuration
  priority: 'all' | ExpansionPriority
  status: 'all' | ExpansionStatus
  keyword: string
}

export interface ExpansionRequest {
  id: string
  scope_type: BudgetScopeType
  scope_id: string
  scope_name: string
  requester_id: string
  requester_name: string
  period_type: ExpansionPeriod
  dimension_type: ExpansionDimension
  model_tier_id: string | null
  model_tier_name: string | null
  duration_type: ExpansionDuration
  priority: ExpansionPriority
  current_amount: number
  requested_amount: number
  current_amount_snapshot: MoneyUSD
  used_amount_snapshot: MoneyUSD
  target_amount_usd: MoneyUSD
  reason: string
  urgent_reason: string
  status: ExpansionStatus
  current_level: ExpansionLevel
  precheck_version: string
  prechecked_at: string
  expires_at: string
  applied_at: string | null
  sync_status: 'pending' | 'synced' | 'failed' | null
  current_usage_ratio?: number  // 审批列表用：申请人当前使用率
  created_at: string
  updated_at: string
  _checked?: boolean
}

export type ExpansionTargetInput =
  | { requested_amount: number; target_amount_usd?: never }
  | { requested_amount?: number; target_amount_usd: MoneyUSD }

export interface CreateExpansionPayload {
  scope_type: string
  scope_id: string
  period_type: ExpansionPeriod
  dimension_type?: ExpansionDimension
  model_tier_id?: string | null
  duration_type?: ExpansionDuration
  priority?: ExpansionPriority
  requested_amount: number
  target_amount_usd?: MoneyUSD
  reason: string
  urgent_reason?: string
}

export interface ExpansionPrecheckInput {
  scope_type: BudgetScopeType
  scope_id: string
  period_type: ExpansionPeriod
  dimension_type: ExpansionDimension
  model_tier_id?: string | null
  duration_type: ExpansionDuration
  priority: ExpansionPriority
  target_amount_usd: MoneyUSD
}

export interface ExpansionConstraint {
  source_level: string
  scope_type?: BudgetScopeType
  scope_id?: string
  limit_usd?: MoneyUSD
  used_usd?: MoneyUSD
  status: string
  reset_at?: string
}

export interface ExpansionPrecheck {
  can_submit: boolean
  can_notify_owner: boolean
  current_limit_usd: MoneyUSD
  current_used_usd: MoneyUSD
  target_amount_usd: MoneyUSD
  increase_usd: MoneyUSD
  effective_after_usd: MoneyUSD
  blocking_reason?: {
    code: string
    source_level: string
    scope_type?: BudgetScopeType
    scope_id?: string
    limit_usd?: MoneyUSD
    status: string
    reset_at?: string
  }
  parent_constraints: ExpansionConstraint[]
  affected_groups: Array<{ id: string; name: string }>
  visible_scopes: Array<{ type: BudgetScopeType; id?: string }>
  precheck_version: string
  data_as_of: string
  stale: boolean
}

export interface ExpansionCapabilities {
  can_edit: boolean
  can_decide: boolean
  can_cancel: boolean
  can_reprecheck: boolean
}

export interface ExpansionDetail {
  request: ExpansionRequest
  submitted_snapshot: Record<string, MoneyUSD>
  live_snapshot: ExpansionPrecheck | null
  parent_constraints: ExpansionConstraint[]
  approval_pool: string[]
  affected_groups: Array<{ id: string; name: string }>
  capabilities: ExpansionCapabilities
  steps: Array<{ id: string; level: string; approver_id?: string; action: string; comment: string; created_at: string }>
  data_as_of: string
}

export interface NotifyOwnerInput {
  scope_type: BudgetScopeType
  scope_id: string
  period_type: ExpansionPeriod
  dimension_type: ExpansionDimension
  model_tier_id?: string | null
  message?: string
}

export const PERIOD_LABELS: Record<ExpansionPeriod, string> = {
  daily: '日额度',
  weekly: '周额度',
  monthly: '月额度',
}

export const PERIOD_SUFFIX: Record<ExpansionPeriod, string> = {
  daily: '/ 天',
  weekly: '/ 周',
  monthly: '/ 月',
}

export interface DecideExpansionPayload {
  action: ExpansionAction
  comment?: string
}

export const STATUS_LABELS: Record<ExpansionStatus, string> = {
  pending: '审批中',
  precheck_required: '待重新预检',
  pending_reconfirmation: '待再次确认',
  approved: '已批准',
  rejected: '已拒绝',
  cancelled: '已取消',
  expired: '已过期',
}

export const STATUS_COLORS: Record<ExpansionStatus, string> = {
  pending: '#f59e0b',
  precheck_required: '#f59e0b',
  pending_reconfirmation: '#f59e0b',
  approved: '#10b981',
  rejected: '#ef4444',
  cancelled: '#6b7280',
  expired: '#6b7280',
}

export const LEVEL_LABELS: Record<ExpansionLevel, string> = {
  dept: '部门级',
  studio: '工作室级',
  platform: '平台级',
}
