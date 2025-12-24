import http from '@/services/http'
import type {
  Group,
  GroupListItem,
  GroupModel,
  CreateGroupRequest,
  UpdateGroupRequest,
  AddModelToGroupRequest,
  ModelOption,
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
  // 价格区间（USD/1M tokens），仅有价格数据时存在
  input_price_min?: number
  input_price_max?: number
  output_price_min?: number
  output_price_max?: number
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

  listAllModels(params?: { page_size?: number }) {
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
}
