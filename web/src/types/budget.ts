
// v0.7/v0.8 预算与额度相关类型

export type BudgetScopeType = 'company' | 'studio' | 'dept_daily' | 'dept_service' | 'member'

export type BudgetView = 'company' | 'studio' | 'department'

export interface BudgetItem {
  allocation_id?: string
  scope_type: BudgetScopeType
  scope_id: string
  /** 仅 scope_type=member 时有值，表示该成员所属部门的 dept_id，多部门场景路由跳转使用 */
  parent_scope_id?: string
  label?: string
  amount_usd: number
  daily_amount_usd: number   // 日限额，-1=不限
  weekly_amount_usd: number  // 周限额，-1=不限
  period: string
  enabled: boolean
  used_usd: number           // 月度已用
  weekly_used_usd?: number   // 本周已用（Redis 实时）
  daily_used_usd?: number    // 今日已用（Redis 实时）
  used_ratio: number
  updated_by?: string         // 最后修改人 ID
  updated_by_name?: string    // 最后修改人名称
  updated_at?: string         // 最后修改时间
}

export interface BudgetSetPayload {
  scope_type: BudgetScopeType
  scope_id: string
  amount_usd: number
  daily_amount_usd?: number   // 可选，默认 -1
  weekly_amount_usd?: number  // 可选，默认 -1
}

export type BudgetEffectiveMode = 'immediate' | 'next_period'

export interface BudgetRiskConfirmation {
  confirm_below_used?: boolean
  confirm_zero_block?: boolean
  confirm_stale?: boolean
  replace_pending_schedule?: boolean
  preview_version?: string
}

export interface BudgetAdjustmentPayload {
  scope_type: BudgetScopeType
  scope_id: string
  period_type: 'daily' | 'weekly' | 'monthly'
  target_limit_usd: string
  effective_mode: BudgetEffectiveMode
  reason: string
  preview_version?: string
  confirm_zero_block?: boolean
  confirm_below_used?: boolean
  confirm_replace_schedule?: boolean
}

export interface BudgetAdjustmentImpact {
  before_limit_usd: string
  after_limit_usd: string
  current_used_usd: string
  below_used: boolean
  zero_block: boolean
  affected_members: number
  affected_groups: string[]
  replaces_pending_schedule: boolean
  pending_schedule_id?: string
  pending_schedule_version?: string
  preview_version: string
  stale: boolean
  data_as_of: string
}

export interface BudgetBatchPayload {
  items: BudgetSetPayload[]
}

export interface BudgetContextItem {
  scope_type: BudgetScopeType
  scope_id: string
  label: string
  amount_usd: number
  used_usd: number
  used_ratio: number
}

export interface BudgetChainItem {
  scope_type: BudgetScopeType
  scope_id?: string
  label: string
  amount_usd: number
  daily_amount_usd?: number
  weekly_amount_usd?: number
  used_usd?: number
  used_ratio?: number
  period?: string
}

export interface BudgetAllocForm {
  scope_type: BudgetScopeType
  scope_id: string
  amount_usd: number
  daily_amount_usd: number
  weekly_amount_usd: number
  label?: string
}

export const SCOPE_TYPE_LABELS: Record<BudgetScopeType, string> = {
  company: '公司全局',
  studio: '工作室',
  dept_daily: '部门日常',
  dept_service: '部门服务',
  member: '成员配额',
}

export const SCOPE_TYPE_COLORS: Record<BudgetScopeType, string> = {
  company: '#6366f1',
  studio: '#36B34E',
  dept_daily: '#f59e0b',
  dept_service: '#8b5cf6',
  member: '#10b981',
}
