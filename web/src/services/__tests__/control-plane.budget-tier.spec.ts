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
})
