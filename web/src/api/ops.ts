import http from '@/services/http'

// ============ 类型定义 ============

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
  ttft_p50_ms: number
  ttft_p90_ms: number
  ttft_p95_ms: number
  ttft_p99_ms: number
  ttft_avg_ms: number
}

export interface OpsErrors {
  request_errors: number
  upstream_errors: number
  business_errors: number
}

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
  category: 'client' | 'upstream' | 'other'
}

// ============ API 方法 ============

export const opsAPI = {
  getDashboard: (timeRange: string = '1h') =>
    http.get<OpsDashboardResponse>('/api/v1/ops/dashboard', { params: { time_range: timeRange } }),

  getTrafficTrend: (timeRange: string = '1h') =>
    http.get<{ points: OpsTrafficPoint[]; bucket: string }>('/api/v1/ops/traffic-trend', { params: { time_range: timeRange } }),

  getErrorTrend: (timeRange: string = '1h') =>
    http.get<{ points: OpsErrorPoint[] }>('/api/v1/ops/error-trend', { params: { time_range: timeRange } }),

  getLatencyHistogram: (timeRange: string = '1h') =>
    http.get<{ buckets: OpsLatencyBucket[]; total_requests: number }>('/api/v1/ops/latency-histogram', { params: { time_range: timeRange } }),

  getErrorDistribution: (timeRange: string = '1h') =>
    http.get<{ distribution: OpsErrorDistributionItem[] }>('/api/v1/ops/error-distribution', { params: { time_range: timeRange } }),
}
