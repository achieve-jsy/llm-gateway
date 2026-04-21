import type { BudgetScopeType } from '@/types/budget'

export type BudgetTone = 'success' | 'warning' | 'danger' | 'info' | 'neutral'

export type BudgetSourceKind =
  | 'personal'
  | 'shared'
  | 'mixed'
  | 'temporary'
  | 'unconfigured'

export interface BudgetDisplayStatus {
  label: string
  tone: BudgetTone
  blocked: boolean
}

export interface TierBudgetTagItem {
  id: string
  name: string
  color?: string
  usageRatio?: number
  state: 'normal' | 'warning' | 'blocked' | 'unconfigured' | 'disabled'
  label: string
}

export function formatBudgetUSD(
  value?: string | number | null,
  options?: {
    unlimited?: boolean
    maximumFractionDigits?: number
  },
): string {
  if (
    value === null
    || value === undefined
    || (typeof value === 'string' && value.trim() === '')
  ) {
    return '—'
  }

  const amount = Number(value)
  if (!Number.isFinite(amount)) return '—'
  if (options?.unlimited && amount < 0) return '不限'

  const requestedFractionDigits = options?.maximumFractionDigits
  const maximumFractionDigits = Number.isFinite(requestedFractionDigits)
    ? Math.min(20, Math.max(2, Math.trunc(requestedFractionDigits as number)))
    : 2
  const formatted = new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits,
  }).format(amount)

  return `$${formatted}`
}

export function toUsagePercent(
  used?: string | number,
  limit?: string | number,
): number {
  const usedAmount = Number(used)
  const limitAmount = Number(limit)

  if (!Number.isFinite(usedAmount) || !Number.isFinite(limitAmount) || limitAmount <= 0) {
    return 0
  }

  return Math.min(100, Math.max(0, (usedAmount / limitAmount) * 100))
}

export function getBudgetStatus(input: {
  configured: boolean
  enabled: boolean
  limit?: string | number
  usageRatio?: number
  stale?: boolean
}): BudgetDisplayStatus {
  if (input.stale) {
    return { label: '数据已过期', tone: 'warning', blocked: false }
  }
  if (!input.enabled) {
    return { label: '已停用', tone: 'info', blocked: false }
  }
  if (!input.configured) {
    return { label: '未配置', tone: 'neutral', blocked: false }
  }
  const hasLimit = input.limit !== undefined
    && !(typeof input.limit === 'string' && input.limit.trim() === '')
  if (hasLimit && Number(input.limit) === 0) {
    return { label: '$0 · 不可使用', tone: 'danger', blocked: true }
  }

  const ratio = Number.isFinite(input.usageRatio) ? input.usageRatio ?? 0 : 0
  if (ratio >= 1) {
    return { label: '已超额', tone: 'danger', blocked: true }
  }
  if (ratio >= 0.8) {
    return { label: '预警', tone: 'warning', blocked: false }
  }
  return { label: '正常', tone: 'success', blocked: false }
}

const BUDGET_SOURCE_LABELS: Record<BudgetScopeType, string> = {
  company: '公司总预算',
  studio: '工作室预算',
  dept_daily: '部门共享子预算',
  dept_service: '部门服务预算',
  member: '个人预算',
}

export function getBudgetSourceLabel(
  scopeType?: BudgetScopeType,
  temporary = false,
): string {
  if (scopeType === 'member' && temporary) return '个人临时额度'
  if (!scopeType) return '未配置'
  return BUDGET_SOURCE_LABELS[scopeType]
}

/** 原型：等级标签按等级名上色（高级紫 / 标准蓝 / 经济绿）；仅超额覆盖为红 */
export type TierTagTone = 'purple' | 'blue' | 'green' | 'orange' | 'red' | 'gray'

export function getTierTagTone(
  name?: string,
  state?: TierBudgetTagItem['state'],
): TierTagTone {
  if (state === 'blocked') return 'red'
  if (state === 'disabled' || state === 'unconfigured') return 'gray'

  const key = (name || '').trim().toLowerCase()
  if (!key) return 'purple'
  if (/高级|premium|pro|旗舰/.test(key)) return 'purple'
  if (/标准|standard|中级|常规/.test(key)) return 'blue'
  if (/经济|economy|basic|入门|低成本/.test(key)) return 'green'
  return 'purple'
}
