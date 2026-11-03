import http from '@/services/http'
import type { ApiResponse } from '@/types/api'
import type {
  InsightRequest,
  InsightResponse,
  ModelHourlyParams,
  ModelHourlyResponse,
  ModelUsersParams,
  ModelUsersResponse,
} from '@/types/analytics'

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

  /**
   * 结构化洞察 POST /api/v1/analytics/insight
   * 输入区间 + 范围，输出三段式（事实 / 原因 / 建议）富文本，每条引用真实数字并携带下钻路径。
   */
  async getInsight(params: InsightRequest): Promise<InsightResponse> {
    const res = await http.post('/api/v1/analytics/insight', params, { _silent: true } as any)
    return res.data as InsightResponse
  },

  /**
   * 模型小时级用量 GET /api/v1/usage/dashboard/model-hourly
   * 按小时聚合 token / 费用 / 调用，可 group_by=model|tier。
   */
  async getModelHourly(params: ModelHourlyParams): Promise<ModelHourlyResponse> {
    const res = await http.get('/api/v1/usage/dashboard/model-hourly', {
      params,
      _silent: true,
    } as any)
    return res.data as ModelHourlyResponse
  },

  /**
   * 模型 × 用户用量 GET /api/v1/usage/dashboard/model-users
   * 部门汇总 / 用户逐日 / 月度三种模式，支撑「下钻到部门/用户」透视。
   */
  async getModelUsers(params: ModelUsersParams): Promise<ModelUsersResponse> {
    const res = await http.get('/api/v1/usage/dashboard/model-users', {
      params,
      _silent: true,
    } as any)
    return res.data as ModelUsersResponse
  },
}
