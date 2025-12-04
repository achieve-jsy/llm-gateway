// dashboard.ts — 分层用量看板相关类型

export type DashScope = 'company' | 'studio' | 'dept' | 'user'

export interface DashboardSummary {
  total_tokens: number
  cost_estimate: number
  user_count: number
  call_count: number
}

export interface DashboardBreakdownItem {
  id: string
  name: string
  total_tokens: number
  cost_estimate: number
  user_count: number
  rank: number
}

export interface DashboardResponse {
  scope: DashScope
  id: string
  scope_name: string
  period: { start: string; end: string }
  summary: DashboardSummary
  breakdown: DashboardBreakdownItem[]
  total: number
  page: number
  page_size: number
}

export interface BreadcrumbItem {
  scope: DashScope
  id: string
  name: string
}
