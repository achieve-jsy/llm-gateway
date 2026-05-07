import { describe, expect, it, vi } from 'vitest'
import { getBudgetTierUsage } from '../control-plane'
import http from '../http'

vi.mock('../http', () => ({
  default: {
    get: vi.fn(),
  },
}))

describe('control-plane budget tier usage service', () => {
  it('读取后端 data.items 并归一化真实计费字段为 used_usd', async () => {
    vi.mocked(http.get).mockResolvedValueOnce({
      data: {
        data: {
          data_as_of: '2026-07-21T00:00:00Z',
          stale: false,
          items: [{
            scope_type: 'dept_daily',
            scope_id: 'dept-1',
            model_tier_id: 'tier-high',
            period_type: 'monthly',
            accounting_cost_usd: '21273.560000',
          }],
        },
      },
    })

    const items = await getBudgetTierUsage({
      scope_type: 'dept_daily',
      scope_id: 'dept-1',
      period_type: 'monthly',
    })

    expect(http.get).toHaveBeenCalledWith('/api/v1/budgets/tier-usage', {
      params: { scope_type: 'dept_daily', scope_id: 'dept-1', period_type: 'monthly' },
    })
    expect(items).toEqual([{
      scope_type: 'dept_daily',
      scope_id: 'dept-1',
      model_tier_id: 'tier-high',
      period_type: 'monthly',
      accounting_cost_usd: '21273.560000',
      used_usd: '21273.560000',
      data_as_of: '2026-07-21T00:00:00Z',
      stale: false,
    }])
  })

  it('accounting 为 0 时使用 reported 成本回显已用额度', async () => {
    vi.mocked(http.get).mockResolvedValueOnce({
      data: {
        data: {
          items: [{
            scope_type: 'studio',
            scope_id: 'studio-1',
            model_tier_id: 'tier-high',
            period_type: 'monthly',
            accounting_cost_usd: '0.000000',
            reported_cost_usd: '24267.680000',
          }],
        },
      },
    })

    const items = await getBudgetTierUsage({
      scope_type: 'studio',
      scope_id: 'studio-1',
      period_type: 'monthly',
    })

    expect(items[0].used_usd).toBe('24267.680000')
  })

  it('后端未返回 period_type 时使用请求周期兜底，保证高级设置日周用量可匹配', async () => {
    vi.mocked(http.get).mockResolvedValueOnce({
      data: {
        data: {
          items: [{
            scope_type: 'studio',
            scope_id: 'studio-1',
            model_tier_id: 'tier-high',
            accounting_cost_usd: '12.340000',
          }],
        },
      },
    })

    const items = await getBudgetTierUsage({
      scope_type: 'studio',
      scope_id: 'studio-1',
      period_type: 'daily',
    })

    expect(items[0].period_type).toBe('daily')
    expect(items[0].used_usd).toBe('12.340000')
  })
})
