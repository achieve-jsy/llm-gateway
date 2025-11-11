import http from './http'
import type { Provider, ProviderPayload, ProviderDefinition } from '@/types/provider'
import type { Model, ModelPayload } from '@/types/model'
import type { ModelTag, ModelTagPayload } from '@/types/model-tag'
import type { SyncResult, ConfirmSyncPayload, ConfirmSyncResult, CheckModelsPayload, CheckModelsResult } from '@/types/sync'
import type {
  ModelCatalog,
  PriceDiffEntry,
  SyncTriggerResponse,
  ApplyDiffResponse,
  CreateFromCatalogPayload,
  ResettableOverrideField,
  AutoBindResult,
  BindCatalogPayload,
  BatchBindItem,
  BatchBindResult,
  CatalogBindingStats,
  CatalogSyncStatus
} from '@/types/catalog'
import type { ApiResponse, PaginatedResponse } from '@/types/api'
import type { ListKeysResponse, CreateKeyWrappedResponse } from '@/types/api-key'

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
  const url = '/api/v1/providers'
  const response = Object.keys(params).length > 0
    ? await http.get<ApiResponse<Provider[]> & { total: number }>(url, { params })
    : await http.get<ApiResponse<Provider[]> & { total: number }>(url)
  return { data: response.data.data, total: response.data.total ?? response.data.data.length }
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

// ===================== 渠道健康检测 =====================

/** 单个渠道检测结果 */
export interface CheckResult {
  provider_id: string
  provider_name: string
  success: boolean
  response_time: number
  error?: string
  auto_disabled: boolean
}

/** 批量检测汇总结果 */
export interface CheckAllResult {
  total: number
  success: number
  failed: number
  auto_disabled: number
  results: CheckResult[]
}

/** 批量检测所有启用渠道（超时设为3分钟） */
export async function checkAllProviders(): Promise<CheckAllResult> {
  const response = await http.post<ApiResponse<CheckAllResult>>('/api/v1/providers/check-all', undefined, { timeout: 180000 })
  return response.data.data
}

/** 检测单个渠道 */
export async function checkProvider(id: string): Promise<CheckResult> {
  const response = await http.post<ApiResponse<CheckResult>>(`/api/v1/providers/${id}/check`)
  return response.data.data
}

/** 健康检测配置 */
export interface HealthCheckConfig {
  interval_minutes: number
  interval_seconds: number
  max_failures: number
  enabled: boolean
  check_providers: boolean
  check_models: boolean
  check_mode: string
  timeout_seconds: number
  failure_action: string
  // 模型检测独立配置
  model_check_enabled: boolean
  model_check_interval_minutes: number
  model_check_interval_seconds: number
  model_check_failure_threshold: number
}

/** 获取健康检测配置 */
export async function getHealthCheckConfig(): Promise<HealthCheckConfig> {
  const response = await http.get<ApiResponse<HealthCheckConfig>>('/api/v1/health-check/config')
  return response.data.data
}

/** 更新健康检测配置 */
export async function updateHealthCheckConfig(payload: Partial<HealthCheckConfig>): Promise<HealthCheckConfig> {
  const response = await http.put<ApiResponse<HealthCheckConfig>>('/api/v1/health-check/config', payload)
  return response.data.data
}

// ===================== 健康监控时间线 =====================

export interface TimelineEntry {
  checked_at: string
  success: boolean
  response_time_ms: number
  error_message?: string
}

export interface TimelineGroup {
  target_name: string
  target_type: string
  health_percent: number
  total_checks: number
  success_checks: number
  provider_count: number
  key_count: number
  timeline: TimelineEntry[]
}

export interface TimelineResult {
  lookback: string
  total: number
  groups: TimelineGroup[]
}

export async function getHealthTimeline(lookback: string = '6h', search: string = '', offset: number = 0, limit: number = 0): Promise<TimelineResult> {
  const response = await http.get<ApiResponse<TimelineResult>>('/api/v1/health-check/timeline', { params: { lookback, search, offset, limit } })
  return response.data.data
}

// ===================== 模型管理 =====================

/** 单个模型检测结果 */
export interface ModelCheckResult {
  model_id: string
  model_name: string
  success: boolean
  response_time: number
  error?: string
  auto_disabled: boolean
  auto_reenabled: boolean
}

/** 批量模型检测汇总结果 */
export interface ModelCheckAllResult {
  total: number
  success: number
  failed: number
  failure_summary?: Record<string, number>
  results: ModelCheckResult[]
}

/** 检测单个模型 */
export async function checkModel(id: string): Promise<ModelCheckResult> {
  const response = await http.post<ApiResponse<ModelCheckResult>>(`/api/v1/models/${id}/check`)
  return response.data.data
}

/** 检测所有启用模型（批量操作，超时设为3分钟） */
export async function checkAllModels(): Promise<ModelCheckAllResult> {
  const response = await http.post<ApiResponse<ModelCheckAllResult>>('/api/v1/models/check-all', undefined, { timeout: 180000 })
  return response.data.data
}

/** 获取模型列表（支持筛选和分页） */
export async function getModels(providerId?: string, opts?: { page?: number; pageSize?: number; name?: string; model_type?: string; enabled?: boolean }): Promise<{ data: Model[]; total: number }> {
  const params: Record<string, any> = {}
  if (providerId) params.provider_id = providerId
  if (opts?.page !== undefined) params.page = opts.page
  if (opts?.pageSize !== undefined) params.page_size = opts.pageSize
  if (opts?.name !== undefined) params.name = opts.name
  if (opts?.model_type !== undefined) params.model_type = opts.model_type
  if (opts?.enabled !== undefined) params.enabled = opts.enabled
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

/** 批量检测模型可用性 */
export async function checkModels(providerId: string, payload: CheckModelsPayload): Promise<CheckModelsResult> {
  const response = await http.post<ApiResponse<CheckModelsResult>>(`/api/v1/providers/${providerId}/check-models`, payload)
  return response.data.data
}

// ===================== 模型 Catalog 自动拉取 =====================

/**
 * 检索 catalog（联想填充用）。
 *
 * 后端：`GET /api/v1/model-catalog/search?q=&provider=&mode=&limit=20`
 * 仅传 `params` 中明确给出的字段，避免把 undefined 透传为 `?q=undefined`。
 */
export async function searchCatalog(
  opts?: { q?: string; provider?: string; mode?: string; limit?: number }
): Promise<{ data: ModelCatalog[]; total: number }> {
  const params: Record<string, any> = {}
  if (opts?.q !== undefined) params.q = opts.q
  if (opts?.provider !== undefined) params.provider = opts.provider
  if (opts?.mode !== undefined) params.mode = opts.mode
  if (opts?.limit !== undefined) params.limit = opts.limit
  const response = await http.get<ApiResponse<ModelCatalog[]> & { total: number }>(
    '/api/v1/model-catalog/search',
    { params }
  )
  return { data: response.data.data, total: response.data.total }
}

/** 通过 source_model_id 精确查询单条 catalog 记录 */
export async function getCatalogBySource(sourceModelId: string): Promise<ModelCatalog> {
  const response = await http.get<ApiResponse<ModelCatalog>>(
    '/api/v1/model-catalog/by-source',
    { params: { source_model_id: sourceModelId } }
  )
  return response.data.data
}

/** 触发后台 catalog 同步任务（202 异步） */
export async function triggerCatalogSync(): Promise<SyncTriggerResponse> {
  const response = await http.post<ApiResponse<SyncTriggerResponse>>('/api/v1/model-catalog/sync')
  return response.data.data
}

/**
 * 获取目录绑定状态（catalog 总数 + 本地 model 绑定分布）。
 *
 * CatalogSyncPage 在 diff 为空时用它做语义判定，避免历史 bug —— 把
 * "目录其实有 2719 条只是没绑定" 误显示为"目录是空的"。
 *
 * 后端：GET /api/v1/model-catalog/binding-stats
 */
export async function getCatalogBindingStats(): Promise<CatalogBindingStats> {
  const response = await http.get<ApiResponse<CatalogBindingStats>>(
    '/api/v1/model-catalog/binding-stats'
  )
  return response.data.data
}

/**
 * 获取目录同步任务的当前状态（用于"手动刷新"后的轮询）。
 *
 * 后端：GET /api/v1/model-catalog/sync-status
 */
export async function getCatalogSyncStatus(): Promise<CatalogSyncStatus> {
  const response = await http.get<ApiResponse<CatalogSyncStatus>>(
    '/api/v1/model-catalog/sync-status'
  )
  return response.data.data
}

/** 拉取本地模型 vs catalog 的价格/能力差异列表 */
export async function getCatalogDiff(): Promise<{ data: PriceDiffEntry[]; total: number }> {
  const response = await http.get<ApiResponse<PriceDiffEntry[]> & { total: number }>(
    '/api/v1/model-catalog/diff'
  )
  return { data: response.data.data, total: response.data.total }
}

/** 应用 catalog 差异（按 model_id 批量更新本地价格/能力） */
export async function applyCatalogDiff(modelIds: string[]): Promise<ApplyDiffResponse> {
  const response = await http.post<ApiResponse<ApplyDiffResponse>>(
    '/api/v1/model-catalog/apply-diff',
    { model_ids: modelIds }
  )
  return response.data.data
}

/**
 * 调用后端「智能绑定」自动把本地 model 与 catalog 条目按 name/upstream_model_id
 * 严格同名匹配并写入 catalog_id。多重命中只返回候选，不写库。
 *
 * 后端：POST /api/v1/model-catalog/auto-bind
 */
export async function autoBindCatalog(): Promise<AutoBindResult> {
  const response = await http.post<ApiResponse<AutoBindResult>>(
    '/api/v1/model-catalog/auto-bind'
  )
  return response.data.data
}

/** 基于 catalog 项直接注册一个本地 Model */
export async function createModelFromCatalog(payload: CreateFromCatalogPayload): Promise<Model> {
  const response = await http.post<ApiResponse<Model>>('/api/v1/models/from-catalog', payload)
  return response.data.data
}

/** 取消某字段的人工覆盖，恢复跟随 catalog */
export async function resetModelOverride(
  modelId: string,
  field: ResettableOverrideField
): Promise<Model> {
  const response = await http.post<ApiResponse<Model>>(
    `/api/v1/models/${modelId}/reset-override`,
    { field }
  )
  return response.data.data
}

/**
 * 把 model 关联到 catalog 条目；catalog 已绑定时变成"换绑"。
 *
 * - reset_fields=true 时同步把 InputPrice / OutputPrice / ContextLength /
 *   MaxOutputTokens / UpstreamModelID 重置为新 catalog 当前值；
 * - 标志位 ctx_overridden / max_output_overridden 始终重置为 false（admin 显式选择新关联）。
 *
 * 后端：POST /api/v1/models/:id/bind-catalog
 */
export async function bindModelCatalog(
  modelId: string,
  payload: BindCatalogPayload
): Promise<Model> {
  const response = await http.post<ApiResponse<Model>>(
    `/api/v1/models/${modelId}/bind-catalog`,
    payload
  )
  return response.data.data
}

/**
 * 解除 model 与 catalog 的关联：catalog_id → null；
 * ctx_overridden / max_output_overridden 重置为 false；价格/容量保留 snapshot。
 *
 * 后端：POST /api/v1/models/:id/unbind-catalog
 */
export async function unbindModelCatalog(modelId: string): Promise<Model> {
  const response = await http.post<ApiResponse<Model>>(
    `/api/v1/models/${modelId}/unbind-catalog`
  )
  return response.data.data
}

/**
 * 批量绑定（用于智能绑定结果中 ambiguous 选择保存）。
 * 单条失败不阻塞其他成功项；返回 applied 数 + 失败映射。
 *
 * 后端：POST /api/v1/models/batch-bind-catalog
 */
export async function batchBindModelCatalog(
  bindings: BatchBindItem[]
): Promise<BatchBindResult> {
  const response = await http.post<ApiResponse<BatchBindResult>>(
    '/api/v1/models/batch-bind-catalog',
    { bindings }
  )
  return response.data.data
}

// ===================== API Key 管理 =====================

/** 获取 API Key 列表（分页） */
export async function listKeys(page = 1, size = 20): Promise<ListKeysResponse> {
  const response = await http.get<ListKeysResponse>('/api/v1/keys', { params: { page, page_size: size } })
  return response.data
}

/** 创建 API Key */
export async function createKey(name: string, expiresAt?: string, groupIds?: number[]): Promise<CreateKeyWrappedResponse> {
  const response = await http.post<CreateKeyWrappedResponse>('/api/v1/keys', { name, expires_at: expiresAt || undefined, group_ids: groupIds && groupIds.length > 0 ? groupIds : undefined })
  return response.data
}

/** 查看完整 API Key（解密展示） */
export async function revealKey(id: string): Promise<{ key: string }> {
  const response = await http.post<{ data: { key: string } }>(`/api/v1/keys/${id}/reveal`)
  return response.data.data
}

/** 获取可选模型组列表 */
export async function getAvailableGroups(): Promise<AvailableGroupsResponse> {
  const response = await http.get<AvailableGroupsResponse>('/api/v1/groups/available')
  return response.data
}

/** 更新 Key 的组绑定 */
export async function updateKeyGroups(keyId: string, add: number[], remove: number[]): Promise<void> {
  await http.post(`/api/v1/keys/${keyId}/groups`, { add, remove })
}

/** 切换万能模型组（access_all_groups）*/
export async function toggleAccessAllGroups(keyId: string, value: boolean): Promise<void> {
  await http.post(`/api/v1/keys/${keyId}/access-all-groups`, { access_all_groups: value })
}

/** 禁用 API Key */
export async function disableKey(id: string): Promise<void> {
  await http.patch(`/api/v1/keys/${id}/disable`)
}

/** 启用 API Key */
export async function enableKey(id: string): Promise<void> {
  await http.patch(`/api/v1/keys/${id}/enable`)
}

/** 删除 API Key */
export async function deleteKey(id: string): Promise<void> {
  await http.delete(`/api/v1/keys/${id}`)
}

/** 重置 API Key（重新生成密钥） */
export async function resetKey(id: string): Promise<CreateKeyWrappedResponse> {
  const response = await http.post<CreateKeyWrappedResponse>(`/api/v1/keys/${id}/reset`)
  return response.data
}

// ===================== 系统设置 =====================

import type { SystemSettings, SettingsResponse, PublicSettingsResponse } from '@/types/settings'

export async function getPublicSettings(): Promise<PublicSettingsResponse> {
  const response = await http.get<PublicSettingsResponse>('/api/v1/settings/public')
  return response.data
}

export async function getSystemSettings(): Promise<SettingsResponse> {
  const response = await http.get<SettingsResponse>('/api/v1/settings')
  return response.data
}

export async function updateSystemSettings(settings: SystemSettings): Promise<void> {
  await http.put('/api/v1/settings', settings)
}

export async function testWebhook(): Promise<void> {
  await http.post('/api/v1/settings/test-webhook')
}

export async function exportSettings(): Promise<Record<string, string>> {
  const response = await http.post<{ code: number; data: Record<string, string> }>('/api/v1/settings/export')
  return response.data.data
}

export async function importSettings(settings: Record<string, string>): Promise<void> {
  await http.post('/api/v1/settings/import', settings)
}
