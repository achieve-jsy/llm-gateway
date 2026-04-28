import http from '@/services/http'

export interface VideoTask {
  task_id: string
  api_key_id?: string
  api_key_name?: string
  api_key_type?: string
  user_id?: string
  user_name?: string
  model: string
  provider_name: string
  provider_type?: string
  status: string
  video_url?: string
  request_body?: string
  response_body?: string
  error_message?: string
  created_at: string
  updated_at?: string
  events?: VideoTaskEvent[]
}

export interface VideoTaskEvent {
  id?: number
  task_id?: string
  event_type: string
  event_data?: string
  created_at: string
}

export interface VideoTaskListResponse {
  data: VideoTask[]
  total: number
  page: number
  page_size: number
}

export interface VideoTaskStats {
  total: number
  submitted: number
  running: number
  succeeded: number
  failed: number
  avg_duration_seconds: number
}

export interface VideoTaskTrendRow {
  date: string
  tasks: number
  succeeded: number
  failed: number
  submitted: number
  running: number
}

export interface VideoTaskProviderDistributionRow {
  provider_name: string
  count: number
}

export interface VideoTaskStatusDistributionRow {
  status: string
  count: number
}

export interface VideoTaskModelDistributionRow {
  model: string
  count: number
  succeeded: number
  failed: number
}

export interface VideoTaskDistribution {
  providers: VideoTaskProviderDistributionRow[]
  statuses: VideoTaskStatusDistributionRow[]
  models: VideoTaskModelDistributionRow[]
}

export interface VideoTaskListParams {
  page?: number
  page_size?: number
  provider_name?: string
  model_name?: string
  status?: string
}

export interface VideoTaskStatsParams {
  start?: string
  end?: string
  provider_name?: string
  model_name?: string
  user_id?: string
}

export interface VideoTaskApiOptions {
  mine?: boolean
}

export const videoTasksAPI = {
  list: (params: VideoTaskListParams = {}, opts?: VideoTaskApiOptions) => {
    const path = opts?.mine ? '/api/v1/my/video/tasks' : '/api/v1/video/tasks'
    return http.get<VideoTaskListResponse>(path, { params }).then(r => r.data)
  },

  get: (taskId: string, opts?: VideoTaskApiOptions) => {
    const path = opts?.mine ? '/api/v1/my/video/tasks' : '/api/v1/video/tasks'
    return http.get<{ data: VideoTask }>(`${path}/${encodeURIComponent(taskId)}`).then(r => r.data.data)
  },

  stats: (params: VideoTaskStatsParams = {}, opts?: VideoTaskApiOptions) => {
    const path = opts?.mine ? '/api/v1/my/video/tasks/stats' : '/api/v1/video/tasks/stats'
    return http.get<{ data: VideoTaskStats }>(path, { params }).then(r => r.data.data)
  },

  trend: (params: VideoTaskStatsParams = {}, opts?: VideoTaskApiOptions) => {
    const path = opts?.mine ? '/api/v1/my/video/tasks/stats/trend' : '/api/v1/video/tasks/stats/trend'
    return http.get<{ data: VideoTaskTrendRow[] }>(path, { params }).then(r => r.data.data)
  },

  distribution: (params: VideoTaskStatsParams = {}, opts?: VideoTaskApiOptions) => {
    const path = opts?.mine ? '/api/v1/my/video/tasks/stats/distribution' : '/api/v1/video/tasks/stats/distribution'
    return http.get<{ data: VideoTaskDistribution }>(path, { params }).then(r => r.data.data)
  },
}
