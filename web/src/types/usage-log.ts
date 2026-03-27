/**
 * 调用记录 & Trace 数据类型定义
 *
 * 从 UsageTraceDrawer.vue 中提取为独立文件，
 * 以便 composable / 非 .vue 文件可以安全导入。
 */

export interface LogRow {
  request_id: string
  ts: string
  key_id: string
  key_name?: string
  key_type?: 'personal' | 'service'
  user_id: string
  user_name?: string
  dept_id: string
  dept_name?: string
  studio_name?: string
  model: string
  routed_model: string
  routed_via_group: boolean
  group_name: string
  provider: string
  prompt_tokens: number
  cached_tokens: number
  cache_write_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  total_tokens: number
  status: number
  upstream_status: number
  duration_ms: number
  first_token_ms?: number | null
  stage_ttfb_ms?: number | null
  stage_auth_ms?: number | null
  stage_route_ms?: number | null
  stage_upstream_ms?: number | null
  stream: boolean
  failover_used: boolean
  usage_missing: boolean
  reasoning_effort?: string
  user_agent?: string
  ip_address?: string
  input_cost_usd: number
  output_cost_usd: number
  cache_read_cost_usd: number
  est_cost_usd: number
  input_price_snapshot?: number | null
  output_price_snapshot?: number | null
  /** 调用的端点路径，如 /v1/chat/completions, /v1/embeddings, /v1/rerank */
  inbound_endpoint?: string
  /** 原始请求体（列表 API 自 call_traces JOIN，用于 rerank 文档数等） */
  request_body?: unknown
  /** Rerank：documents 数组长度（后端 SQL 提取，优先于解析 request_body） */
  documents_count?: number | null
}

export interface TraceData {
  request_id: string
  ts: string
  stage_auth_ms?: number
  stage_route_ms?: number
  stage_upstream_ms?: number
  stage_ttfb_ms?: number
  stage_ttft_ms?: number
  stage_total_ms?: number
  request_headers?: Record<string, string>
  request_body?: any
  response_body?: any
  error_code?: string
  error_message?: string
}
