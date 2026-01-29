import http from '@/services/http'

// ============ 核心类型定义 ============

export type OpsSeverity = 'P0' | 'P1' | 'P2' | 'P3'
export type OpsQueryMode = 'auto' | 'raw' | 'preagg'
export type AlertEventStatus = 'firing' | 'resolved' | 'manual_resolved'
export type MetricType =
  | 'success_rate' | 'error_rate' | 'upstream_error_rate'
  | 'cpu_usage_percent' | 'memory_usage_percent' | 'concurrency_queue_depth'
  | 'group_available_accounts' | 'group_available_ratio' | 'group_rate_limit_ratio'
  | 'account_rate_limited_count' | 'account_error_count' | 'account_error_ratio'
  | 'overload_account_count'
export type Operator = '>' | '>=' | '<' | '<=' | '==' | '!='

// ============ Dashboard 核心类型 ============

export interface OpsDashboardResponse {
  overview: OpsOverview
  timestamp: string
}

export interface OpsOverview {
  health_score: number
  system_metrics: OpsSystemMetrics
  traffic: OpsTraffic
  sla: OpsSLA
  latency: OpsLatency
  errors: OpsErrors
  jobs?: OpsJobHeartbeat[]
}

export interface OpsSystemMetrics {
  cpu_usage_percent: number
  memory_used_mb: number
  memory_total_mb: number
  memory_usage_percent: number
  goroutine_count: number
  db_active_count: number
  db_idle_count: number
  db_waiting_count: number
  db_max_open_conns: number
  redis_total_conns: number
  redis_idle_conns: number
  redis_pool_size: number
  collected_at: string
}

export interface OpsJobHeartbeat {
  id: number
  name: string
  status: string
  last_duration_ms: number
  last_success_at: string | null
  last_error_at: string | null
  last_result: string
  last_error: string
  updated_at: string
}

export interface OpsTraffic {
  total_requests: number
  total_tokens: number
  avg_qps: number
  avg_tps: number
  current_qps: number
  current_tps: number
  peak_qps: number
  peak_tps: number
}

export interface OpsRealtimeTrafficSummary {
  current_qps: number
  current_tps: number
  window: string
  sample_count: number
}

export interface OpsGroupOption {
  id: number
  name: string
}

export interface OpsSLA {
  success_rate: number
  error_rate: number
  sla_error_count: number
}

export interface OpsLatency {
  duration_p50_ms: number
  duration_p90_ms: number
  duration_p95_ms: number
  duration_p99_ms: number
  duration_avg_ms: number
  duration_max_ms: number
  ttft_p50_ms: number
  ttft_p90_ms: number
  ttft_p95_ms: number
  ttft_p99_ms: number
  ttft_avg_ms: number
  ttft_max_ms: number
}

export interface OpsErrors {
  request_errors: number
  upstream_errors: number
  business_errors: number
  upstream_errors_excl_429_529: number
  rate_limit_429_529: number
}

// ============ 趋势 / 分布图表类型 ============

export interface OpsTrafficPoint {
  timestamp: string
  qps: number
  tps: number
  request_count: number
  token_count: number
}

export interface OpsErrorPoint {
  timestamp: string
  request_errors: number
  upstream_errors: number
  business_errors: number
}

export interface OpsLatencyBucket {
  range: string
  count: number
}

export interface OpsErrorDistributionItem {
  status_code: number
  count: number
  category: 'client' | 'upstream' | 'system' | 'other'
}

export interface OpsSwitchRatePoint {
  timestamp: string
  switch_rate: number
  switch_count: number
  request_count: number
}

export interface OpsThroughputByProvider {
  provider_id: number
  provider_name: string
  request_count: number
  token_count: number
}

export interface OpsThroughputByGroup {
  group_id: number
  group_name: string
  request_count: number
  token_count: number
}

export interface OpsThroughputTrendResponse {
  points: OpsTrafficPoint[]
  by_provider: OpsThroughputByProvider[]
  top_groups: OpsThroughputByGroup[]
}

// ============ 请求 / 错误详情类型 ============

export interface OpsRequestDetail {
  id: number
  ts: string
  model: string
  provider: string
  group_name: string
  status: number
  duration_ms: number
  first_token_ms: number | null
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  error_message: string | null
  upstream_status: number | null
  client_ip: string | null
  request_id: string | null
  upstream_context?: OpsUpstreamContext
  latency_breakdown?: OpsLatencyBreakdown
}

export interface OpsRequestDetailsParams {
  time_range: string
  page?: number
  page_size?: number
  sort_by?: string
  sort_order?: 'asc' | 'desc'
  status?: number
  provider?: string
  group_id?: number
  model?: string
  search?: string
  min_duration_ms?: number
}

export interface OpsRequestDetailsResponse {
  items: OpsRequestDetail[]
  total: number
  page: number
  page_size: number
}

export interface OpsUpstreamError {
  id: number
  ts: string
  provider: string
  model: string
  group_name: string
  status: number
  error_message: string
  duration_ms: number
  resolved: boolean
  upstream_context?: OpsUpstreamContext
  latency_breakdown?: OpsLatencyBreakdown
}

export interface OpsErrorLogEntry {
  id: number
  ts: string
  model: string
  provider: string
  group_name: string
  status: number
  error_message: string
  duration_ms: number
  request_id: string | null
  phase: string
  resolved: boolean
}

export interface OpsErrorListParams {
  time_range: string
  page?: number
  page_size?: number
  status?: number
  provider?: string
  group_id?: number
  phase?: string
  search?: string
}

export interface OpsErrorListResponse {
  items: OpsErrorLogEntry[]
  total: number
  page: number
  page_size: number
}

// ============ 并发统计类型 ============

export interface OpsProviderConcurrency {
  provider_id: number
  provider_name: string
  provider_type: string
  status: 'available' | 'rate_limited' | 'overloaded' | 'error' | 'unavailable'
  current_load: number
  max_capacity: number
  active_requests: number
  waiting_queue: number
  is_rate_limited: boolean
  rate_limit_remaining: number
  rate_limit_reset_seconds: number
  error_count_5min: number
  last_error_at: string | null
  last_error_message: string | null
}

export interface OpsUserConcurrency {
  user_id: string
  request_count: number
  error_count: number
  avg_duration_ms: number
  status: 'healthy' | 'warning' | 'critical'
}

export interface OpsUserConcurrencyResponse {
  users: OpsUserConcurrency[]
  total_users: number
}

export interface OpsAccountAvailability {
  provider_id: number
  provider_name: string
  is_rate_limited: boolean
  rate_limit_remaining: number
  rate_limit_reset_seconds: number
  error_count_5min: number
  last_error_at: string | null
  last_error_message: string | null
}

export interface OpsKeyConcurrency {
  key_id: number
  key_name: string
  status: 'available' | 'rate_limited' | 'overloaded' | 'error' | 'unavailable'
  current_load: number
  max_capacity: number
  active_requests: number
  waiting_queue: number
}

export interface OpsConcurrencyStatsResponse {
  providers: OpsProviderConcurrency[]
  total_providers: number
  available_providers: number
  rate_limited_providers: number
  error_providers: number
  total_active_requests: number
  total_waiting_queue: number
}

// ============ 告警类型 ============

export interface AlertRule {
  id?: number
  name: string
  description: string
  enabled: boolean
  metric_type: MetricType
  operator: Operator
  threshold: number
  window_minutes: number
  sustained_minutes: number
  severity: OpsSeverity
  cooldown_minutes: number
  notify_email: boolean
  filters?: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

export interface AlertEvent {
  id: number
  rule_id: number
  rule_name?: string
  severity: OpsSeverity
  status: AlertEventStatus
  title: string
  description: string
  metric_value: number
  threshold_value: number
  dimensions: Record<string, unknown>
  fired_at: string
  resolved_at: string | null
  email_sent: boolean
  created_at: string
}

export interface AlertEventsQuery {
  time_range: string
  severity?: string
  status?: string
  platform?: string
  group_id?: number
  email_sent?: boolean
  limit: number
  before_fired_at?: string
  before_id?: number
}

export interface AlertSilenceCreate {
  rule_id: number
  platform?: string
  group_id?: number
  region?: string
  until: string
  reason: string
}

// ============ 系统日志类型 ============

export interface OpsSystemLog {
  id: number
  ts: string
  level: string
  component: string
  message: string
  request_id: string | null
  client_request_id: string | null
  user_id: string | null
  provider: string | null
  model: string | null
  group: string | null
  duration_ms: number | null
  status: number | null
  error_message: string | null
  extra: Record<string, unknown> | null
}

export interface OpsSystemLogQuery {
  time_range: string
  level?: string
  component?: string
  request_id?: string
  user_id?: string
  provider?: string
  model?: string
  group?: string
  search?: string
  page?: number
  page_size?: number
}

export interface OpsSystemLogResponse {
  items: OpsSystemLog[]
  total: number
  page: number
  page_size: number
}

export interface OpsSystemLogSinkHealth {
  queue_depth: number
  queue_capacity: number
  dropped: number
  write_failures: number
  avg_write_delay_ms: number
  status: string
}

export interface OpsRuntimeLogConfig {
  level: string
  sampling_initial: number
  sampling_thereafter: number
  enable_caller: boolean
  enable_stacktrace: boolean
  stacktrace_level: string
  retention_days: number
}

// ============ Token 统计类型 ============

export interface OpsTokenStatsItem {
  provider: string
  model: string
  request_count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  avg_duration_ms: number
}

export interface OpsTokenStatsParams {
  time_range: string
  page?: number
  page_size?: number
  sort_by?: string
  sort_order?: 'asc' | 'desc'
  provider?: string
  model?: string
  top_n?: number
}

export interface OpsTokenStatsResponse {
  items: OpsTokenStatsItem[]
  total: number
  total_requests: number
  total_tokens: number
}

// ============ 设置 / 阈值类型 ============

export interface OpsMetricThresholds {
  health_score_warn: number
  health_score_critical: number
  sla_warn: number
  sla_critical: number
  cpu_warn: number
  cpu_critical: number
  memory_warn: number
  memory_critical: number
  ttft_p99_warn_ms: number
  ttft_p99_critical_ms: number
  error_rate_warn: number
  error_rate_critical: number
  upstream_error_rate_warn: number
  upstream_error_rate_critical: number
  goroutine_warn: number
  goroutine_critical: number
}

export interface OpsDashboardSettings {
  auto_refresh_interval: number
  show_alert_events: boolean
  show_token_stats: boolean
  show_concurrency_card: boolean
  show_switch_rate_chart: boolean
  default_query_mode: OpsQueryMode
}

export interface OpsAdvancedSettings {
  auto_refresh_enabled: boolean
  data_retention_days: number
  error_log_retention_days: number
  ignore_context_canceled: boolean
  ignore_429_errors: boolean
}

// ============ 用户并发统计类型 ============

export interface OpsUserConcurrencyItem {
  user_id: string
  request_count: number
  error_count: number
  avg_duration_ms: number
}

export interface OpsUserConcurrencyResponseNew {
  items: OpsUserConcurrencyItem[]
  total: number
}

// ============ 账户可用性统计类型 ============

export interface OpsAccountAvailabilityItem {
  provider_id: string
  is_rate_limited: boolean
  rate_limit_remaining: number
  rate_limit_reset_seconds: number
  error_count: number
}

export interface OpsAccountAvailabilityResponse {
  items: OpsAccountAvailabilityItem[]
  total: number
}

// ============ 请求错误关联类型 ============

export interface OpsErrorCorrelationItem {
  provider: string
  upstream_status: number
  upstream_error_message: string
  ts: string
  duration_ms: number
}

export interface OpsErrorCorrelationResponse {
  request_id: string
  items: OpsErrorCorrelationItem[]
  total: number
}

// ============ 详情增强类型 ============

export interface OpsUpstreamContext {
  upstream_status_code: number
  upstream_error_message: string
  upstream_endpoint: string
}

export interface OpsLatencyBreakdown {
  auth_ms: number
  routing_ms: number
  upstream_ms: number
  response_ms: number
  ttft_ms: number
}

// ============ API 方法 ============

export const opsAPI = {
  // ---- Dashboard 核心 ----
  getDashboard: (timeRange: string = '1h') =>
    http.get<OpsDashboardResponse>('/api/v1/ops/dashboard', { params: { time_range: timeRange } }),

  // ---- 实时流量摘要（按时间窗口，用于实时流量窗口切换） ----
  getRealtimeTrafficSummary: (window: string = '1min') =>
    http.get<OpsRealtimeTrafficSummary>('/api/v1/ops/realtime-traffic/summary', { params: { window } }),

  // ---- 模型组下拉列表（运维过滤用） ----
  getGroupOptions: () =>
    http.get<OpsGroupOption[]>('/api/v1/ops/groups'),

  getTrafficTrend: (timeRange: string = '1h') =>
    http.get<{ points: OpsTrafficPoint[]; bucket: string }>('/api/v1/ops/traffic-trend', { params: { time_range: timeRange } }),

  getThroughputTrend: (timeRange: string = '1h') =>
    http.get<OpsThroughputTrendResponse>('/api/v1/ops/throughput-trend', { params: { time_range: timeRange } }),

  getErrorTrend: (timeRange: string = '1h') =>
    http.get<{ points: OpsErrorPoint[] }>('/api/v1/ops/error-trend', { params: { time_range: timeRange } }),

  getLatencyHistogram: (timeRange: string = '1h') =>
    http.get<{ buckets: OpsLatencyBucket[]; total_requests: number }>('/api/v1/ops/latency-histogram', { params: { time_range: timeRange } }),

  getErrorDistribution: (timeRange: string = '1h') =>
    http.get<{ distribution: OpsErrorDistributionItem[] }>('/api/v1/ops/error-distribution', { params: { time_range: timeRange } }),

  getSwitchRateTrend: (timeRange: string = '5h') =>
    http.get<{ points: OpsSwitchRatePoint[] }>('/api/v1/ops/switch-rate-trend', { params: { time_range: timeRange } }),

  // ---- 请求 / 错误详情 ----
  listRequests: (params: OpsRequestDetailsParams) =>
    http.get<OpsRequestDetailsResponse>('/api/v1/ops/requests', { params }),

  listRequestErrors: (params: OpsErrorListParams) =>
    http.get<OpsErrorListResponse>('/api/v1/ops/request-errors', { params }),

  getRequestErrorDetail: (id: number) =>
    http.get<OpsRequestDetail>(`/api/v1/ops/request-errors/${id}`),

  listUpstreamErrors: (params: OpsErrorListParams) =>
    http.get<OpsErrorListResponse>('/api/v1/ops/upstream-errors', { params }),

  getUpstreamErrorDetail: (id: number) =>
    http.get<OpsUpstreamError>(`/api/v1/ops/upstream-errors/${id}`),

  updateRequestErrorResolved: (id: number, resolved: boolean) =>
    http.put(`/api/v1/ops/request-errors/${id}/resolve`, { resolved }),

  updateUpstreamErrorResolved: (id: number, resolved: boolean) =>
    http.put(`/api/v1/ops/upstream-errors/${id}/resolve`, { resolved }),

  // ---- 并发统计 ----
  getConcurrencyStats: () =>
    http.get<OpsConcurrencyStatsResponse>('/api/v1/ops/concurrency'),

  getUserConcurrency: () =>
    http.get<OpsUserConcurrencyResponseNew>('/api/v1/ops/user-concurrency'),

  getAccountAvailability: () =>
    http.get<OpsAccountAvailabilityResponse>('/api/v1/ops/account-availability'),

  // ---- 实时流量窗口 ----
  getRealtimeTraffic: (params?: { window?: string; platform?: string; group_id?: number }) =>
    http.get<OpsTraffic>('/api/v1/ops/realtime-traffic', { params }),

  // ---- 告警 ----
  listAlertRules: () =>
    http.get<AlertRule[]>('/api/v1/ops/alert-rules'),

  createAlertRule: (rule: Omit<AlertRule, 'id' | 'created_at' | 'updated_at'>) =>
    http.post<AlertRule>('/api/v1/ops/alert-rules', rule),

  updateAlertRule: (id: number, rule: Partial<AlertRule>) =>
    http.put<AlertRule>(`/api/v1/ops/alert-rules/${id}`, rule),

  deleteAlertRule: (id: number) =>
    http.delete(`/api/v1/ops/alert-rules/${id}`),

  listAlertEvents: (query: AlertEventsQuery) =>
    http.get<AlertEvent[]>('/api/v1/ops/alert-events', { params: query }),

  getAlertEvent: (id: number) =>
    http.get<AlertEvent>(`/api/v1/ops/alert-events/${id}`),

  updateAlertEventStatus: (id: number, status: AlertEventStatus) =>
    http.put(`/api/v1/ops/alert-events/${id}/status`, { status }),

  createAlertSilence: (silence: AlertSilenceCreate) =>
    http.post('/api/v1/ops/alert-silences', silence),

  // ---- 系统日志 ----
  listSystemLogs: (query: OpsSystemLogQuery) =>
    http.get<OpsSystemLogResponse>('/api/v1/ops/system-logs', { params: query }),

  getSystemLogSinkHealth: () =>
    http.get<OpsSystemLogSinkHealth>('/api/v1/ops/system-logs/health'),

  getRuntimeLogConfig: () =>
    http.get<OpsRuntimeLogConfig>('/api/v1/ops/runtime/logging'),

  updateRuntimeLogConfig: (config: Partial<OpsRuntimeLogConfig>) =>
    http.put<OpsRuntimeLogConfig>('/api/v1/ops/runtime/logging', config),

  resetRuntimeLogConfig: () =>
    http.post<OpsRuntimeLogConfig>('/api/v1/ops/runtime/logging/reset'),

  cleanupSystemLogs: (retention_days: number) =>
    http.post<{ deleted: number }>('/api/v1/ops/system-logs/cleanup', { retention_days }),

  // ---- Token 统计 ----
  getTokenStats: (params: OpsTokenStatsParams) =>
    http.get<OpsTokenStatsResponse>('/api/v1/ops/token-stats', { params }),

  // ---- 设置 ----
  getMetricThresholds: () =>
    http.get<OpsMetricThresholds>('/api/v1/ops/settings/metric-thresholds'),

  updateMetricThresholds: (thresholds: Partial<OpsMetricThresholds>) =>
    http.put<OpsMetricThresholds>('/api/v1/ops/settings/metric-thresholds', thresholds),

  getDashboardSettings: () =>
    http.get<OpsDashboardSettings>('/api/v1/ops/settings/dashboard'),

  updateDashboardSettings: (settings: Partial<OpsDashboardSettings>) =>
    http.put<OpsDashboardSettings>('/api/v1/ops/settings/dashboard', settings),

  // ---- Jobs ----
  listJobs: () =>
    http.get<OpsJobHeartbeat[]>('/api/v1/ops/jobs'),

  // ---- 高级设置 ----
  getAdvancedSettings: () =>
    http.get<OpsAdvancedSettings>('/api/v1/ops/settings/advanced'),

  updateAdvancedSettings: (settings: Partial<OpsAdvancedSettings>) =>
    http.put<OpsAdvancedSettings>('/api/v1/ops/settings/advanced', settings),

  // ---- 请求错误关联 ----
  getRequestErrorCorrelation: (requestId: string) =>
    http.get<OpsErrorCorrelationResponse>(`/api/v1/ops/request-errors/${requestId}/correlation`),
}
