// analytics.ts — 模型用量分析：AI 洞察卡 / model-hourly / model-users 相关类型

/** 洞察范围类型 */
export type InsightScopeKind = 'company' | 'dept' | 'model' | 'tier'

/** 洞察范围：随筛选 / 下钻联动收窄 */
export interface InsightScope {
  kind: InsightScopeKind
  /** 范围 key：model 名 / dept 名 / tier 枚举；company 不传 */
  key?: string
}

/** 洞察请求体 POST /api/v1/analytics/insight */
export interface InsightRequest {
  start: string
  end: string
  scope: InsightScope
}

/** 下钻目标：AI 卡「下钻」锚点携带的目标 */
export interface Drill {
  dept?: string
  model?: string
  scroll?: string
}

/** 单条洞察文本（text 为可含 <b>/<span class="num"> 的富文本；est 标注估算） */
export interface Segment {
  text: string
  drill?: Drill
  est?: boolean
}

/** 洞察关键指标（供卡片 / 对照渲染） */
export interface InsightMetrics {
  cost: number
  prev_cost: number
  delta_pct: number
  budget_pct: number
  high_tier_pct: number
  cache_rate: number
}

/** 洞察响应 */
export interface InsightResponse {
  scope: { kind: InsightScopeKind; key?: string; label: string }
  /** 全司人均费用中位数（美元），作为「超中位数 Nx」的对照 */
  gmed: number
  metrics: InsightMetrics
  segments: {
    facts: Segment[]
    causes: Segment[]
    suggestions: Segment[]
  }
}

// ── AI 分析卡组件契约（AiAnalysisCard.vue）──────────────────────────────

export type InsightCardTag = '事实' | '原因' | '建议'

export interface InsightCardSegment {
  tag: InsightCardTag
  html: string
}

export type InsightCardDrillPayload = Drill

// ── model-hourly / model-users（后端已就绪，封装等待前端消费）─────────────

export interface ModelHourlyParams {
  from: string
  to: string
  model_name?: string
  tier_name?: string
  group_by?: 'model' | 'tier'
}

export interface ModelHourlyPoint {
  hour: string
  group_key: string
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  cache_write_tokens: number
  est_cost_usd: number
  call_count: number
}

export interface ModelDailyCumulative {
  date: string
  cum_prompt_tokens: number
  cum_completion_tokens: number
  cum_reasoning_tokens: number
  cum_cached_tokens: number
  cum_cache_write_tokens: number
  cum_est_cost_usd: number
  cum_call_count: number
}

export interface ModelHourlyResponse {
  start: string
  end: string
  group_by: string
  hourly: ModelHourlyPoint[]
  daily: ModelDailyCumulative[]
}

export interface ModelUsersParams {
  from: string
  to: string
  model_name?: string
  tier_name?: string
  dept_name?: string
  group_by?: 'dept' | 'user'
  period?: 'day' | 'month'
}

export interface ModelUsersSummary {
  total_users: number
  total_cost_usd: number
  total_calls: number
}

export interface ModelDeptSummaryRow {
  dept_name: string
  user_count: number
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  est_cost_usd: number
  cost_per_user: number
  cost_pct: number
  call_count: number
}

export interface ModelUserDayRow {
  date: string
  user_id: string
  user_name: string
  dept_name: string
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  est_cost_usd: number
  cost_pct: number
  call_count: number
}

export interface ModelMonthlyRow {
  month: string
  group_key: string
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  cached_tokens: number
  est_cost_usd: number
  cost_pct: number
  call_count: number
  user_count: number
}

export interface ModelUsersResponse {
  start: string
  end: string
  group_by: string
  period: string
  summary: ModelUsersSummary
  dept_rows?: ModelDeptSummaryRow[]
  user_day_rows?: ModelUserDayRow[]
  month_rows?: ModelMonthlyRow[]
}
