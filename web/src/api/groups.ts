import http from '@/services/http'
import type {
  Group,
  GroupListItem,
  GroupModel,
  CreateGroupRequest,
  UpdateGroupRequest,
  AddModelToGroupRequest,
  ModelOption,
  OpenMode,
  GroupEntitlement,
  GrantEntitlementRequest,
  EntitlementListResponse,
} from '@/types/group'
import type { ApiResponse, PaginatedResponse } from '@/types/api'

export interface FailoverDefaults {
  max_switches: number
  same_model_retries: number
  failover_cooldown_seconds: number
}

// GroupCapability 模型组聚合能力（与控制面 service.GroupModelInfo 对应）。
// 能力位为「组内全部成员都支持才为 true」；上限取成员最小正值。
export interface GroupCapability {
  mode: string
  max_input_tokens: number
  max_output_tokens: number
  supports_vision: boolean
  supports_function_calling: boolean
  supports_tool_choice: boolean
  // 单价：USD per 1,000,000 tokens；0 表示缺价。
  input_price_per_million: number
  output_price_per_million: number
  is_capability_override: boolean

  // ── 视频模型专用字段（seedance） ──────────────────────────────────────
  // 当 mode == "video" 时由后端填充；非视频模型为 undefined。
  video_supported_actions?: string[]      // 文生视频 / 图生视频 / 多模态参考
  video_supported_resolutions?: string[]  // 720p / 1080p
  video_max_duration_seconds?: number
  video_api_endpoint?: string             // POST /v1/contents/generations/tasks
}

export const groupsApi = {
  createGroup(data: CreateGroupRequest) {
    return http.post<ApiResponse<Group>>('/api/v1/groups', data)
  },

  listGroups(params: { search?: string; page?: number; page_size?: number }) {
    return http.get<PaginatedResponse<GroupListItem>>('/api/v1/groups', { params })
  },

  getGroup(id: number) {
    return http.get<ApiResponse<Group>>(`/api/v1/groups/${id}`)
  },

  updateGroup(id: number, data: UpdateGroupRequest) {
    return http.put<ApiResponse<Group>>(`/api/v1/groups/${id}`, data)
  },

  deleteGroup(id: number) {
    return http.delete(`/api/v1/groups/${id}`)
  },

  addModelsToGroup(id: number, data: AddModelToGroupRequest) {
    return http.post(`/api/v1/groups/${id}/models`, data)
  },

  listGroupModels(id: number) {
    return http.get<ApiResponse<GroupModel[]>>(`/api/v1/groups/${id}/models`)
  },

  removeModelFromGroup(groupId: number, modelId: number) {
    return http.delete(`/api/v1/groups/${groupId}/models/${modelId}`)
  },

  listAllModels(params?: { page?: number; page_size?: number }) {
    return http.get<ApiResponse<ModelOption[]>>('/api/v1/models', { params })
  },

  getGroupDefaults() {
    return http.get<ApiResponse<FailoverDefaults>>('/api/v1/groups/defaults')
  },

  // 返回 { [模型组名]: GroupCapability }，不传 names 时返回所有启用组。
  getGroupCapabilities(names?: string[]) {
    return http.get<ApiResponse<Record<string, GroupCapability>>>('/api/v1/groups/capabilities', {
      params: names && names.length ? { names } : undefined,
    })
  },

  // ─── 开放模式 + 授权管理 ─────────────────────────────────────────────────

  setOpenMode(groupId: number, openMode: OpenMode) {
    return http.put<ApiResponse<Group>>(`/api/v1/groups/${groupId}/open-mode`, { open_mode: openMode })
  },

  listEntitlements(groupId: number, scopeType?: string) {
    return http.get<ApiResponse<EntitlementListResponse>>(`/api/v1/groups/${groupId}/entitlements`, {
      params: scopeType ? { scope_type: scopeType } : undefined,
    })
  },

  grantEntitlement(groupId: number, data: GrantEntitlementRequest) {
    return http.post<ApiResponse<GroupEntitlement>>(`/api/v1/groups/${groupId}/entitlements`, data)
  },

  revokeEntitlement(groupId: number, entitlementId: string) {
    return http.delete(`/api/v1/groups/${groupId}/entitlements/${entitlementId}`)
  },

  resetCapability(groupId: number) {
    return http.post<ApiResponse<Group>>(`/api/v1/groups/${groupId}/reset-capability`)
  },
}
