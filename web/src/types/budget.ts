
// v0.7 预算与额度相关类型

export type BudgetScopeType = 'company' | 'studio' | 'dept_daily' | 'dept_service' | 'member'

export interface BudgetItem {
  scope_type: BudgetScopeType
  scope_id: string
  label?: string
  amount_usd: number
  period: string
  enabled: boolean
  used_usd: number
  used_ratio: number
}

export interface BudgetSetPayload {
  scope_type: BudgetScopeType
  scope_id: string
  amount_usd: number
}

export interface BudgetChainItem {
  scope_type: BudgetScopeType
  label: string
  amount_usd: number
  period?: string
}

export interface BudgetAllocForm {
  scope_type: BudgetScopeType
  scope_id: string
  amount_usd: number
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
