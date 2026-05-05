import type { BudgetTierLimit } from '@/types/model-tier'
import type { ExpansionPeriod } from '@/types/expansion'

export interface TierDetailRow {
  id: string
  name: string
  color?: string
  enabled: boolean
  monthly: number | null
  weekly: number | null
  daily: number | null
  used: number
  ratio: number
}

function findPeriod(limits: BudgetTierLimit[], period: ExpansionPeriod) {
  return limits.find(item => item.period_type === period)
}

function parseLimit(limit?: BudgetTierLimit): number | null {
  if (!limit) return null
  const limitValue = limit.base_limit_usd ?? limit.limit_usd
  const safeLimit = Number(limitValue ?? NaN)
  return Number.isFinite(safeLimit) ? safeLimit : null
}

function isUUIDLike(value?: string): boolean {
  return Boolean(value && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value))
}

function displayTierName(limit: BudgetTierLimit): string {
  if (limit.model_tier_name && !isUUIDLike(limit.model_tier_name)) return limit.model_tier_name
  if (limit.model_tier_id && !isUUIDLike(limit.model_tier_id)) return limit.model_tier_id
  return '未知等级'
}

function usageAmount(limit?: BudgetTierLimit): number {
  const raw = limit as any
  return Number(raw?.used_usd ?? raw?.accounting_cost_usd ?? raw?.reported_cost_usd ?? 0)
}

/** 将多周期 limit 行按等级聚合为一行（月/周/日）。 */
export function buildTierDetailRows(tierLimits: BudgetTierLimit[] = []): TierDetailRow[] {
  if (!tierLimits.length) return []

  const byTier = new Map<string, BudgetTierLimit[]>()
  for (const limit of tierLimits) {
    const key = limit.model_tier_id
    const list = byTier.get(key) ?? []
    list.push(limit)
    byTier.set(key, list)
  }

  return [...byTier.entries()].map(([id, limits]) => {
    const monthly = findPeriod(limits, 'monthly')
    const weekly = findPeriod(limits, 'weekly')
    const daily = findPeriod(limits, 'daily')
    const any = monthly ?? weekly ?? daily ?? limits[0]
    const monthlyLimit = parseLimit(monthly)
    const used = usageAmount(monthly)
    const ratio = monthlyLimit !== null && monthlyLimit > 0 ? used / monthlyLimit : 0

    return {
      id,
      name: displayTierName(any),
      color: undefined,
      enabled: any.enabled ?? true,
      monthly: monthlyLimit,
      weekly: parseLimit(weekly),
      daily: parseLimit(daily),
      used: Number.isFinite(used) ? used : 0,
      ratio,
    }
  })
}
