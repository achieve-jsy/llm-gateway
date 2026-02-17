/**
 * dashboard-enhanced.ts — Phase B 增强接口类型定义
 *
 * 对应 API 契约 v2:
 * - GET /api/v1/usage/dashboard/users      (增强版用户排行)
 * - GET /api/v1/usage/dashboard/models     (增强版模型排行)
 * - GET /api/v1/usage/dashboard/user-detail (用户详情)
 * - GET /api/v1/usage/dashboard/model-detail (模型详情)
 */

// ── 用户排行增强 (GET /users) ─────────────────────────────────

export interface UsersApiSummary {
  total_users: number
  total_cost_usd: number
  total_calls: number
  active_days: number
  avg_cost_per_call: number
}

export interface TopModelItem {
  model: string
  est_cost_usd: number
  call_count: number
  total_tokens: number
}

export interface VsAvg {
  cost_ratio_vs_avg: number
  call_count_vs_avg: number
  error_rate_vs_avg: number
}

export interface UserRankRow {
  user_id: string
  user_name: string
  dept_name: string
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  est_cost_usd: number
  call_count: number
  error_count: number
  model_count: number
  /** 成本占比 % */
  cost_ratio: number
  /** 环比变化 % */
  cost_change_pct: number
  /** 每千 token 成本 */
  cost_per_1k_tokens: number
  /** 错误率 % */
  error_rate: number
  /** Top 3 模型 */
  top_models: TopModelItem[]
  /** 与均值比较 */
  vs_avg: VsAvg
}

export interface UsersApiResponse {
  summary: UsersApiSummary
  rows: UserRankRow[]
  page: number
  page_size: number
  total_count: number
}

// ── 模型排行增强 (GET /models) ─────────────────────────────────

export interface ModelRankRow {
  model: string
  calls: number
  total_tokens: number
  cost: number
  /** 平均响应时间 ms */
  avg_duration_ms: number
  /** P90 响应时间 ms */
  p90_duration_ms: number
  /** 错误率 % */
  error_rate: number
  /** 平均首 token 延迟 ms */
  avg_first_token_ms: number
  /** 是否长尾模型 */
  is_long_tail: boolean
}

export interface ModelsApiResponse {
  rows: ModelRankRow[]
}

// ── 用户详情 (GET /user-detail) ────────────────────────────────

export interface UserDetailSummary {
  total_calls: number
  success_count: number
  total_tokens: number
  est_cost_usd: number
  model_count: number
  avg_duration_ms: number
  error_rate: number
}

export interface DailyTrendItem {
  date: string
  calls: number
  cost: number
  tokens: number
}

export interface ModelDistributionItem {
  model: string
  calls: number
  cost: number
  tokens: number
}

export interface HourlyDistributionItem {
  /** 小时 0-23 */
  hour: number
  calls: number
}

export interface UserDetailResponse {
  summary: UserDetailSummary
  daily_trend: DailyTrendItem[]
  model_distribution: ModelDistributionItem[]
  hourly_distribution: HourlyDistributionItem[]
}

// ── 模型详情 (GET /model-detail) ───────────────────────────────

export interface ModelDetailSummary {
  total_calls: number
  success_count: number
  total_tokens: number
  est_cost_usd: number
  user_count: number
  dept_count: number
  avg_duration_ms: number
  p90_duration_ms: number
  avg_first_token_ms: number
  error_rate: number
}

export interface ModelUserDistributionItem {
  user_id: string
  user_name: string
  dept_name: string
  calls: number
  cost: number
  tokens: number
}

export interface ModelDetailResponse {
  summary: ModelDetailSummary
  daily_trend: DailyTrendItem[]
  user_distribution: ModelUserDistributionItem[]
}

// ── 面板状态 ───────────────────────────────────────────────────

export type PanelType = 'user' | 'model'

export interface ActivePanel {
  type: PanelType
  /** 用户 ID 或模型名称 */
  id: string
  /** 显示名称 */
  name: string
}
