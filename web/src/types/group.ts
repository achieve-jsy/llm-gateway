export type Strategy = 'round_robin' | 'priority' | 'weighted' | 'auto'

export type ComplexityTier = 'SIMPLE' | 'MEDIUM' | 'COMPLEX' | 'REASONING'

export type OpenMode = 'public' | 'controlled'

export interface Group {
  id: number
  name: string
  description: string
  strategy: Strategy
  model_type: string
  enabled: boolean
  max_switches: number
  same_model_retries: number
  failover_cooldown_seconds: number
  timeout_seconds: number
  model_count?: number
  model_tier_id?: string | null
  model_tier_name?: string | null
  models?: GroupModel[]
  open_mode: OpenMode
  is_capability_override: boolean
  override_supports_vision: boolean
  override_supports_function_calling: boolean
  override_supports_tool_choice: boolean
  // 组级「视频输入」声明（仅手动覆盖模式生效）；开启后整组按支持视频路由
  override_supports_video_input: boolean
  override_mode: string
  override_context_length: number | null
  override_max_output_tokens: number | null
  sticky_enabled: boolean | null
  sticky_ttl_seconds: number | null
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
  provider_name?: string
  priority: number | null
  weight: number | null
  complexity_tier?: ComplexityTier
}

export interface GroupListItem extends Group {
  strategy_label: string
  model_tier_id?: string | null
  model_tier_name?: string | null
}

export interface CreateGroupRequest {
  name: string
  description: string
  strategy: Strategy
  model_type?: string
  max_switches?: number
  same_model_retries?: number
  failover_cooldown_seconds?: number
  timeout_seconds?: number
  sticky_enabled?: boolean | null
  sticky_ttl_seconds?: number | null
}

export interface UpdateGroupRequest {
  name?: string
  description?: string
  strategy?: Strategy
  model_type?: string
  enabled?: boolean
  max_switches?: number
  same_model_retries?: number
  failover_cooldown_seconds?: number
  timeout_seconds?: number
  is_capability_override?: boolean
  override_supports_vision?: boolean
  override_supports_function_calling?: boolean
  override_supports_tool_choice?: boolean
  override_supports_video_input?: boolean
  override_mode?: string
  override_context_length?: number | null
  override_max_output_tokens?: number | null
  sticky_enabled?: boolean | null
  sticky_ttl_seconds?: number | null
}

export interface AddModelToZoneRequest {
  model_ids: string[]
  priorities?: Record<string, number>
  weights?: Record<string, number>
  complexity_tiers?: Record<string, ComplexityTier>
}

export interface AddModelToGroupRequest {
  model_ids: string[]
  priorities?: Record<string, number>
  weights?: Record<string, number>
  complexity_tiers?: Record<string, ComplexityTier>
}

export interface ModelOption {
  id: string
  name: string
  model_type: string
  provider?: { name: string }
}

// ─── 授权管理 ───────────────────────────────────────────────────────────────

export type EntitlementScopeType = 'studio' | 'dept'

export interface GroupEntitlement {
  id: string
  group_id: number
  scope_type: EntitlementScopeType
  scope_id: string
  scope_name: string
  enabled: boolean
  granted_by: string
  granted_by_name?: string
  created_at: string
  updated_at: string
}

export interface GrantEntitlementRequest {
  scope_type: EntitlementScopeType
  scope_id: string
}

export interface EntitlementListResponse {
  items: GroupEntitlement[]
}
