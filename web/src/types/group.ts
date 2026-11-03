export interface Group {
  id: number
  name: string
  description: string
  enabled: boolean
  strategy: string          // 真实字段名（非 routing_strategy）
  routing_strategy?: string // 兼容旧字段
  max_switches: number
  same_model_retries: number
  failover_cooldown_seconds: number
  model_count: number
  created_at: string
  updated_at: string
}

export interface GroupModel {
  id: number
  group_id: number
  model_id: string
  model_name: string
  priority: number | null
  weight: number | null
  created_at: string
  updated_at: string
}

export interface GroupPayload {
  name: string
  description?: string
  enabled?: boolean
  strategy?: string
  max_switches?: number
  same_model_retries?: number
  failover_cooldown_seconds?: number
}
