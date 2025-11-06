import http from './http'
import type { Provider, ProviderPayload, ProviderDefinition } from '@/types/provider'
import type { Model, ModelPayload } from '@/types/model'
import type { ModelTag, ModelTagPayload } from '@/types/model-tag'
import type { SyncResult, ConfirmSyncPayload, ConfirmSyncResult } from '@/types/sync'
import type { ApiResponse, PaginatedResponse } from '@/types/api'

// ===================== 渠道定义（Registry） =====================

/** 获取所有渠道定义（含 credentialSchema、default_models） */
export async function getProviderDefinitions(): Promise<ProviderDefinition[]> {
  const response = await http.get<ApiResponse<ProviderDefinition[]>>('/api/v1/provider-definitions')
  return response.data.data
}

// ===================== 渠道管理 =====================

/** 获取渠道列表（支持筛选和分页） */
export async function getProviders(opts?: { page?: number; pageSize?: number; name?: string; definition_id?: string; tag?: string }): Promise<{ data: Provider[]; total: number }> {
  const params: Record<string, any> = {}
  if (opts?.page !== undefined) params.page = opts.page
  if (opts?.pageSize !== undefined) params.page_size = opts.pageSize
  if (opts?.name !== undefined) params.name = opts.name
  if (opts?.definition_id !== undefined) params.definition_id = opts.definition_id
  if (opts?.tag !== undefined) params.tag = opts.tag
  const response = await http.get<ApiResponse<Provider[]> & { total: number }>('/api/v1/providers', { params })
  return { data: response.data.data, total: response.data.total }
}

/** 获取单个渠道详情 */
export async function getProvider(id: string): Promise<Provider> {
  const response = await http.get<ApiResponse<Provider>>(`/api/v1/providers/${id}`)
  return response.data.data
}

/** 创建渠道 */
export async function createProvider(payload: ProviderPayload): Promise<Provider> {
  const response = await http.post<ApiResponse<Provider>>('/api/v1/providers', payload)
  return response.data.data
}

/** 更新渠道 */
export async function updateProvider(id: string, payload: ProviderPayload): Promise<Provider> {
  const response = await http.put<ApiResponse<Provider>>(`/api/v1/providers/${id}`, payload)
  return response.data.data
}

/** 删除渠道（可能返回 409 表示有关联模型） */
export async function deleteProvider(id: string): Promise<void> {
  await http.delete(`/api/v1/providers/${id}`)
}

/** 启用/禁用渠道，返回更新后的渠道对象 */
export async function toggleProviderStatus(id: string, enabled: boolean): Promise<Provider> {
  const response = await http.patch<ApiResponse<Provider>>(`/api/v1/providers/${id}/status`, { enabled })
  return response.data.data
}

/** 测试结果 */
export interface TestProviderResult {
  success: boolean
  response_time: number
  status_code?: number
  model_count?: number
  error?: string
  // 转发测试结果
  chat_success?: boolean
  chat_response_time?: number
  chat_model?: string
  chat_error?: string
}

/** 测试渠道连通性（可选包含转发测试） */
export async function testProvider(id: string, testChat?: boolean): Promise<TestProviderResult> {
  const response = await http.post<ApiResponse<TestProviderResult>>(
    `/api/v1/providers/${id}/test`,
    testChat ? { test_chat: true } : undefined
  )
  return response.data.data
}

// ===================== 模型管理 =====================

/** 获取模型列表（支持筛选和分页） */
export async function getModels(providerId?: string, opts?: { page?: number; pageSize?: number; name?: string; model_type?: string }): Promise<{ data: Model[]; total: number }> {
  const params: Record<string, any> = {}
  if (providerId) params.provider_id = providerId
  if (opts?.page !== undefined) params.page = opts.page
  if (opts?.pageSize !== undefined) params.page_size = opts.pageSize
  if (opts?.name !== undefined) params.name = opts.name
  if (opts?.model_type !== undefined) params.model_type = opts.model_type
  const response = await http.get<PaginatedResponse<Model>>('/api/v1/models', { params })
  return { data: response.data.data, total: response.data.total }
}

/** 创建/注册模型 */
export async function createModel(payload: ModelPayload): Promise<Model> {
  const response = await http.post<ApiResponse<Model>>('/api/v1/models', payload)
  return response.data.data
}

/** 更新模型 */
export async function updateModel(id: string, payload: ModelPayload): Promise<Model> {
  const response = await http.put<ApiResponse<Model>>(`/api/v1/models/${id}`, payload)
  return response.data.data
}

/** 删除模型 */
export async function deleteModel(id: string): Promise<void> {
  await http.delete(`/api/v1/models/${id}`)
}

/** 批量删除模型 */
export async function batchDeleteModels(modelIds: string[]): Promise<{ deleted: number }> {
  const response = await http.delete<ApiResponse<{ deleted: number }>>('/api/v1/models/batch', { data: { model_ids: modelIds } })
  return response.data.data
}

/** 批量修改模型启用状态 */
export async function batchUpdateModelStatus(modelIds: string[], enabled: boolean): Promise<{ updated: number }> {
  const response = await http.patch<ApiResponse<{ updated: number }>>('/api/v1/models/batch-status', { model_ids: modelIds, enabled })
  return response.data.data
}

/**
 * 检查模型名称是否可用（不与现有模型冲突）。
 *
 * 走独立的 `GET /api/v1/models/check-name?name=…` 端点（S0-3 修复），返回
 * `{ data: { available: boolean } }`。
 *
 * 注意：仅做乐观查重提示，最终唯一性由 CreateModel 的事务校验兜底。
 */
export async function checkModelNameUnique(name: string): Promise<boolean> {
  const response = await http.get<ApiResponse<{ available: boolean }>>(
    '/api/v1/models/check-name',
    { params: { name } }
  )
  return response.data.data.available
}

// ===================== 模型标签管理 =====================

/** 获取所有标签 */
export async function getModelTags(): Promise<ModelTag[]> {
  const response = await http.get<ApiResponse<ModelTag[]>>('/api/v1/model-tags')
  return response.data.data
}

/** 创建标签 */
export async function createModelTag(payload: ModelTagPayload): Promise<ModelTag> {
  const response = await http.post<ApiResponse<ModelTag>>('/api/v1/model-tags', payload)
  return response.data.data
}

/** 更新标签 */
export async function updateModelTag(id: string, payload: ModelTagPayload): Promise<ModelTag> {
  const response = await http.put<ApiResponse<ModelTag>>(`/api/v1/model-tags/${id}`, payload)
  return response.data.data
}

/** 删除标签 */
export async function deleteModelTag(id: string): Promise<void> {
  await http.delete(`/api/v1/model-tags/${id}`)
}

/** 获取模型的标签列表 */
export async function getModelTagsForModel(modelId: string): Promise<ModelTag[]> {
  const response = await http.get<ApiResponse<ModelTag[]>>(`/api/v1/models/${modelId}/tags`)
  return response.data.data
}

/** 设置模型的标签 */
export async function setModelTags(modelId: string, tagIds: string[]): Promise<ModelTag[]> {
  const response = await http.put<ApiResponse<ModelTag[]>>(`/api/v1/models/${modelId}/tags`, { tag_ids: tagIds })
  return response.data.data
}

/** 批量为多个模型设置标签 */
export async function batchSetModelTags(modelIds: string[], tagIds: string[]): Promise<{ updated: number }> {
  const response = await http.put<ApiResponse<{ updated: number }>>('/api/v1/models/batch-tags', { model_ids: modelIds, tag_ids: tagIds })
  return response.data.data
}

/** 批量获取多个模型的标签 */
export async function batchGetModelTags(modelIds: string[]): Promise<Record<string, ModelTag[]>> {
  const response = await http.post<ApiResponse<Record<string, ModelTag[]>>>('/api/v1/models/batch-tags/query', { model_ids: modelIds })
  return response.data.data
}

// ===================== 模型同步 =====================

/** 同步模型（预览） */
export async function syncModels(providerId: string): Promise<SyncResult> {
  const response = await http.post<ApiResponse<SyncResult>>(`/api/v1/providers/${providerId}/sync-models`)
  return response.data.data
}

/** 确认同步（注册模型） */
export async function confirmSync(providerId: string, payload: ConfirmSyncPayload): Promise<ConfirmSyncResult> {
  const response = await http.post<ApiResponse<ConfirmSyncResult>>(`/api/v1/providers/${providerId}/sync-models/confirm`, payload)
  return response.data.data
}
