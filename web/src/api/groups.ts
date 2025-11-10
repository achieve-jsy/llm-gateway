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
}
