
// v0.7/v0.8 预算与额度相关类型

export type BudgetScopeType = 'company' | 'studio' | 'dept_daily' | 'dept_service' | 'member'

export type BudgetView = 'company' | 'studio' | 'department'

export interface BudgetItem {
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
  studio: '#2BA8A2',
  dept_daily: '#f59e0b',
  dept_service: '#8b5cf6',
  member: '#10b981',
}
