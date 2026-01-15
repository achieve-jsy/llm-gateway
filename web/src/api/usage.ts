import http from '@/services/http'
import type { ApiResponse } from '@/types/api'

export interface UsagePercentiles {
  p90_daily_avg_calls: number
  p90_cache_hit_pct: number
  p90_output_ratio: number
  p90_unique_models: number
  p90_reasoning_share: number
  calculated_at?: string
}

export const usageApi = {
  getPercentiles() {
    return http.get<ApiResponse<UsagePercentiles>>('/api/v1/my/usage/percentiles', {
      _silent: true,
    } as any)
  },
}
