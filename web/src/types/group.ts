export type Strategy = 'round_robin' | 'priority' | 'weighted'

export interface Group {
  id: number
  name: string
  description: string
  strategy: Strategy
  enabled: boolean
  max_switches: number
  same_model_retries: number
  failover_cooldown_seconds: number
  model_count?: number
  models?: GroupModel[]
  created_at: string
  updated_at: string
}

export interface GroupForm {
  name: string
  description: string
  strategy: Strategy
}

export interface GroupModel {
  id: number
  group_id: number
  model_id: string
  model_name: string
  priority: number | null
  weight: number | null
}

export interface GroupListItem extends Group {
  strategy_label: string
}

export interface CreateGroupRequest {
  name: string
  description: string
  strategy: Strategy
  max_switches?: number
  same_model_retries?: number
  failover_cooldown_seconds?: number
}

export interface UpdateGroupRequest {
  name?: string
  description?: string
  strategy?: Strategy
  enabled?: boolean
  max_switches?: number
  same_model_retries?: number
  failover_cooldown_seconds?: number
}

export interface AddModelToGroupRequest {
  model_ids: string[]
  priorities?: Record<string, number>
  weights?: Record<string, number>
}

export interface ModelOption {
  id: string
  name: string
  model_type: string
}
