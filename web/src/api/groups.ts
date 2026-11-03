import http from '@/lib/http'
import type { Group, GroupPayload, GroupModel } from '@/types/group'
import type { Model } from '@/types/model'
import type { ApiResponse } from '@/types/api'

export const groupsApi = {
  async createGroup(payload: GroupPayload): Promise<Group> {
    const r = await http.post<ApiResponse<Group>>('/api/v1/groups', payload)
    return r.data.data
  },

  async listGroups(opts?: { page?: number; pageSize?: number; name?: string }): Promise<{ data: Group[]; total: number }> {
    const params: Record<string, any> = {}
    if (opts?.page) params.page = opts.page
    if (opts?.pageSize) params.page_size = opts.pageSize
    if (opts?.name) params.search = opts.name  // 后端用 search 参数
    const r = await http.get<{ data: Group[]; page: number; page_size: number; total: number }>('/api/v1/groups', { params })
    return { data: r.data.data, total: r.data.total ?? (r.data.data?.length ?? 0) }
  },

  async getGroup(id: number): Promise<Group> {
    const r = await http.get<ApiResponse<Group>>(`/api/v1/groups/${id}`)
    return r.data.data
  },

  async updateGroup(id: number, payload: GroupPayload): Promise<Group> {
    const r = await http.put<ApiResponse<Group>>(`/api/v1/groups/${id}`, payload)
    return r.data.data
  },

  async deleteGroup(id: number): Promise<void> {
    await http.delete(`/api/v1/groups/${id}`)
  },

  async addModelsToGroup(groupId: number, modelIds: string[]): Promise<void> {
    await http.post(`/api/v1/groups/${groupId}/models`, { model_ids: modelIds })
  },

  async listGroupModels(groupId: number): Promise<GroupModel[]> {
    const r = await http.get<ApiResponse<GroupModel[]>>(`/api/v1/groups/${groupId}/models`)
    return r.data.data
  },

  async removeModelFromGroup(groupId: number, modelId: string): Promise<void> {
    await http.delete(`/api/v1/groups/${groupId}/models/${modelId}`)
  },

  async listAllModels(): Promise<Model[]> {
    const r = await http.get<ApiResponse<Model[]>>('/api/v1/models')
    return r.data.data
  },

  async getGroupDefaults(): Promise<{ max_switches: number; same_model_retries: number; failover_cooldown_seconds: number }> {
    const r = await http.get<ApiResponse<{ max_switches: number; same_model_retries: number; failover_cooldown_seconds: number }>>('/api/v1/groups/defaults')
    return r.data.data
  },
}
