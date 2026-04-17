import { describe, expect, it } from 'vitest'
import { buildTierDetailRows } from '../budgetDetailRows'
import type { BudgetTierLimit } from '@/types/model-tier'

const tierLimits: BudgetTierLimit[] = [
  {
    id: 'l1',
    budget_allocation_id: 'alloc-1',
    model_tier_id: 'high',
    model_tier_name: '高等级',
    period_type: 'monthly',
    limit_usd: '100',
    enabled: true,
    used_usd: '10',
  },
  {
    id: 'l2',
    budget_allocation_id: 'alloc-1',
    model_tier_id: 'high',
    model_tier_name: '高等级',
    period_type: 'weekly',
    limit_usd: '40',
    enabled: true,
  },
  {
    id: 'l3',
    budget_allocation_id: 'alloc-1',
    model_tier_id: 'high',
    model_tier_name: '高等级',
    period_type: 'daily',
    limit_usd: '8',
    enabled: true,
  },
  {
    id: 'l4',
    budget_allocation_id: 'alloc-1',
    model_tier_id: 'mid',
    model_tier_name: '中等级',
    period_type: 'monthly',
    limit_usd: '50',
    enabled: true,
    used_usd: '0',
  },
]

describe('buildTierDetailRows', () => {
  it('按等级聚合并填充月/周/日额度', () => {
    const rows = buildTierDetailRows(tierLimits)
    expect(rows).toHaveLength(2)

    const high = rows.find(row => row.id === 'high')
    expect(high).toMatchObject({
      name: '高等级',
      monthly: 100,
      weekly: 40,
      daily: 8,
      used: 10,
      ratio: 0.1,
    })

    const mid = rows.find(row => row.id === 'mid')
    expect(mid).toMatchObject({
      name: '中等级',
      monthly: 50,
      weekly: null,
      daily: null,
      used: 0,
    })
  })
})
