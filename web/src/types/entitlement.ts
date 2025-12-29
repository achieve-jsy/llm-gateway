
// v0.7 模型组授权类型

export type GroupOpenMode = 'public' | 'controlled'

export type EntitlementScopeType = 'studio' | 'dept'

export interface GroupEntitlement {
  id: string
  group_id: number
  scope_type: EntitlementScopeType
  scope_id: string
  enabled: boolean
  granted_by: string
  created_at: string
  updated_at: string
}

export interface SetOpenModePayload {
  open_mode: GroupOpenMode
}

export interface EntitlementPayload {
  group_id: number
  scope_type: EntitlementScopeType
  scope_id: string
}

export const OPEN_MODE_LABELS: Record<GroupOpenMode, string> = {
  public: '公开',
  controlled: '受控',
}
