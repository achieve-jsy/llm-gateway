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
import type { Announcement, AnnouncementPayload, UpdateAnnouncementPayload } from '@/types/announcement'

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
  // 模型自动恢复配置
  model_recovery_enabled: boolean
  model_recovery_success_threshold: number
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

// ===================== 模型状态变更日志 =====================

/** 模型状态变更日志 */
export interface ModelStatusLog {
  id: string
  model_id: string
  model_name: string
  provider_id: string
  provider_name: string
  change_type: 'auto_disabled' | 'auto_recovered' | 'manual_disabled' | 'manual_enabled'
  reason: string
  error_message: string
  operator_id?: string
  operator_name: string
  created_at: string
}

/** 获取模型状态变更日志列表（支持筛选和分页） */
export async function getModelStatusLogs(params: {
  model_id?: string
  provider_id?: string
  change_type?: string
  start_time?: string
  end_time?: string
  page?: number
  page_size?: number
}): Promise<{ items: ModelStatusLog[]; total: number; page: number; page_size: number }> {
  const response = await http.get<ApiResponse<{ items: ModelStatusLog[]; total: number; page: number; page_size: number }>>('/api/v1/model-status-logs', { params })
  return response.data.data
}

/** 获取单个模型的状态变更日志 */
export async function getModelStatusLogsByModel(modelId: string, limit?: number): Promise<ModelStatusLog[]> {
  const response = await http.get<ApiResponse<ModelStatusLog[]>>(`/api/v1/models/${modelId}/status-logs`, { params: { limit } })
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
  provider_name: string
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

// ===================== 模型组健康监控时间线 =====================

export interface GroupTimelineGroup {
  group_name: string
  strategy: string
  model_count: number
  health_percent: number
  total_checks: number
  success_checks: number
  timeline: TimelineEntry[]
}

export interface GroupTimelineResult {
  lookback: string
  total: number
  groups: GroupTimelineGroup[]
}

export async function getGroupHealthTimeline(lookback: string = '6h', search: string = '', offset: number = 0, limit: number = 0): Promise<GroupTimelineResult> {
  const response = await http.get<ApiResponse<GroupTimelineResult>>('/api/v1/health-check/group-timeline', { params: { lookback, search, offset, limit } })
  return response.data.data
}

/** 触发所有模型组的健康检测（异步，超时 3 分钟） */
export async function checkAllGroups(): Promise<{ total: number; success: number; failed: number }> {
  const response = await http.post<ApiResponse<{ total: number; success: number; failed: number }>>('/api/v1/groups/check-all', undefined, { timeout: 180000 })
  return response.data.data
}

/** 触发单个模型组的健康检测 */
export async function checkGroup(id: number): Promise<{ group_id: number; group_name: string; success: boolean; response_time: number; error?: string }> {
  const response = await http.post<ApiResponse<{ group_id: number; group_name: string; success: boolean; response_time: number; error?: string }>>(`/api/v1/groups/${id}/check`)
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

export async function toggleLogEnabled(keyId: string, value: boolean): Promise<void> {
  await http.patch(`/api/v1/keys/${keyId}/log-enabled`, { log_enabled: value })
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

// ===================== OpenCode 配置 =====================

/** 生成一次性 token（同时把 provider 信息存入 Redis） */
export async function generateOpencodeConfigToken(
  keyId: string,
  params: { provider_id?: string; provider_name?: string; base_url: string; api_key: string }
): Promise<{ token: string; ttl_seconds: number }> {
  const response = await http.post<{ code: number; data: { token: string; ttl_seconds: number } }>(
    `/api/v1/keys/${keyId}/opencode-config/token`,
    params
  )
  return response.data.data
}

/** 获取脚本内容的完整 URL（用于 curl | bash 或 iex 命令） */
export function getScriptURL(token: string, os: 'mac' | 'windows'): string {
  const params = new URLSearchParams({ token, os })
  return `/api/v1/opencode-config/script?${params.toString()}`
}

/** 轮询配置写入状态 */
export async function getOpencodeConfigStatus(
  keyId: string,
  token: string
): Promise<{ status: 'pending' | 'success' | 'failed'; msg?: string }> {
  const response = await http.get<{ code: number; data: { status: string; msg?: string } }>(
    `/api/v1/keys/${keyId}/opencode-config/status`,
    { params: { token } }
  )
  return response.data.data as { status: 'pending' | 'success' | 'failed'; msg?: string }
}

// ===================== 公告管理 =====================

export async function getActiveAnnouncements(page = 1, pageSize = 10): Promise<{ data: Announcement[]; total: number }> {
  const response = await http.get<ApiResponse<Announcement[]> & { total: number }>(
    '/api/v1/public/announcements',
    { params: { page, page_size: pageSize } }
  )
  return { data: response.data.data || [], total: response.data.total ?? 0 }
}

export async function getPublicAnnouncement(id: string): Promise<Announcement> {
  const response = await http.get<ApiResponse<Announcement>>(`/api/v1/public/announcements/${id}`)
  return response.data.data
}

export async function getAnnouncements(opts?: { status?: string; page?: number; page_size?: number }): Promise<{ data: Announcement[]; total: number; page: number; page_size: number }> {
  const params: Record<string, any> = {}
  if (opts?.status) params.status = opts.status
  if (opts?.page !== undefined) params.page = opts.page
  if (opts?.page_size !== undefined) params.page_size = opts.page_size
  const response = Object.keys(params).length > 0
    ? await http.get<ApiResponse<Announcement[]> & { total: number; page: number; page_size: number }>('/api/v1/announcements', { params })
    : await http.get<ApiResponse<Announcement[]> & { total: number; page: number; page_size: number }>('/api/v1/announcements')
  return { data: response.data.data, total: response.data.total ?? response.data.data.length, page: response.data.page ?? 1, page_size: response.data.page_size ?? 10 }
}

export async function getAnnouncement(id: string): Promise<Announcement> {
  const response = await http.get<ApiResponse<Announcement>>(`/api/v1/announcements/${id}`)
  return response.data.data
}

export async function createAnnouncement(payload: AnnouncementPayload): Promise<Announcement> {
  const response = await http.post<ApiResponse<Announcement>>('/api/v1/announcements', payload)
  return response.data.data
}

export async function updateAnnouncement(id: string, payload: UpdateAnnouncementPayload): Promise<Announcement> {
  const response = await http.put<ApiResponse<Announcement>>(`/api/v1/announcements/${id}`, payload)
  return response.data.data
}

export async function deleteAnnouncement(id: string): Promise<void> {
  await http.delete(`/api/v1/announcements/${id}`)
}

export async function publishAnnouncement(id: string): Promise<void> {
  await http.patch(`/api/v1/announcements/${id}/publish`)
}

export async function archiveAnnouncement(id: string): Promise<void> {
  await http.patch(`/api/v1/announcements/${id}/archive`)
}

export async function getUnreadAnnouncements(): Promise<Announcement[]> {
  const response = await http.get<ApiResponse<Announcement[]>>('/api/v1/announcements/unread', { _silent: true } as any)
  return response.data.data
}

export async function getUnreadAnnouncementCount(): Promise<number> {
  const response = await http.get<{ count: number }>('/api/v1/announcements/unread/count', { _silent: true } as any)
  return response.data.count
}

export async function getReadHistoryAnnouncements(): Promise<Announcement[]> {
  const response = await http.get<ApiResponse<Announcement[]>>('/api/v1/announcements/history', { _silent: true } as any)
  return response.data.data
}

export async function markAnnouncementRead(id: string): Promise<void> {
  await http.post(`/api/v1/announcements/${id}/read`, {}, { _silent: true } as any)
}

// ===================== 模型性能看板 =====================

export interface PerformanceMember {
  model_name: string
  provider: string
  traffic_pct: number
  req_count: number
  p50_ms: number
  p95_ms: number
  success_rate: number
}

export interface PerformanceItem {
  name: string
  strategy?: string
  model_count?: number
  used_by_groups?: number  // model view
  req_count: number
  success_rate: number
  p50_ms: number
  p95_ms: number
  ttft_p50_ms: number
  ttft_p95_ms: number
  failover_count: number
  pct_in_24h_p50?: number | null
  err_429: number
  err_5xx: number
  members?: PerformanceMember[]
}

export interface PerformanceResult {
  window: string
  generated_at: string
  items: PerformanceItem[]
  total: number
}

export async function getPerformance(
  window: string = '1h',
  view: string = 'group',
  search: string = '',
  limit: number = 50,
  offset: number = 0
): Promise<PerformanceResult> {
  const response = await http.get<ApiResponse<PerformanceResult>>('/api/v1/health/performance', {
    params: { window, view, search, limit, offset }
  })
  return response.data.data
}

// ===================== v0.7 预算管理 =====================

import type { BudgetItem, BudgetScopeType, BudgetSetPayload, BudgetChainItem, BudgetContextItem } from '@/types/budget'
import type { EntitlementPayload, GroupEntitlement } from '@/types/entitlement'
import type { RawServiceKeyItem, ServiceKeyItem, CreateServiceKeyResponse } from '@/types/service-key'
import type { ExpansionRequest, CreateExpansionPayload } from '@/types/expansion'
import { mapServiceKey } from '@/types/service-key'

/** 获取预算列表（按 scope 类型和 id 过滤，或按 view 参数） */
export async function getBudgetList(scopeType?: BudgetScopeType, scopeId?: string, view?: string): Promise<BudgetItem[]> {
  const params: Record<string, string> = {}
  if (view) params.view = view
  if (scopeType) params.scope_type = scopeType
  if (scopeId) params.scope_id = scopeId
  const res = await http.get<ApiResponse<{ items: BudgetItem[] }>>('/api/v1/budgets', { params })
  return res.data.items
}

/** 设置预算额度 */
export async function setBudget(payload: BudgetSetPayload): Promise<void> {
  await http.put('/api/v1/budgets', payload)
}

/** 批量设置预算额度 */
export async function setBudgetBatch(items: BudgetSetPayload[]): Promise<{ results: Array<{ scope_type: string; scope_id: string; ok: boolean; error?: string }> }> {
  const res = await http.put<ApiResponse<{ results: Array<{ scope_type: string; scope_id: string; ok: boolean; error?: string }> }>>('/api/v1/budgets/batch', { items })
  return res.data
}

/** 从 call_logs 重建 Redis 预算计数器（Redis 丢失恢复用） */
export async function rebuildBudgetCounters(): Promise<{ keys_written: number; month_period: string }> {
  const res = await http.post<{ result: { keys_written: number; month_period: string } }>('/api/v1/budget/rebuild-counters')
  return res.data.result
}

/** 删除成员单独配额，还原为共享部门预算池 */
export async function deleteMemberBudget(scopeType: string, scopeId: string): Promise<void> {
  await http.delete('/api/v1/budgets', { data: { scope_type: scopeType, scope_id: scopeId } })
}

/** 获取预算链路上下文（面包屑用） */
export async function getBudgetContext(): Promise<BudgetContextItem[]> {
  const res = await http.get<ApiResponse<{ context: BudgetContextItem[] }>>('/api/v1/budgets/context')
  return res.data.context
}

/** 获取我的预算链 */
export async function getMyBudget(): Promise<BudgetChainItem[]> {
  const res = await http.get<ApiResponse<{ chain: BudgetChainItem[] }>>('/api/v1/my/budget')
  return res.data.chain
}

// ===================== v0.7 模型组授权 =====================

/** 设置模型组开放模式 */
export async function setGroupOpenMode(groupId: number, openMode: 'public' | 'controlled'): Promise<void> {
  await http.put(`/api/v1/groups/${groupId}/open-mode`, { open_mode: openMode })
}

/** 获取模型组授权列表 */
export async function getGroupEntitlements(groupId: number): Promise<GroupEntitlement[]> {
  const res = await http.get<ApiResponse<{ items: GroupEntitlement[] }>>(`/api/v1/groups/${groupId}/entitlements`)
  return res.data.items
}

/** 授权（下放给工作室或开通部门） */
export async function grantEntitlement(payload: EntitlementPayload): Promise<void> {
  await http.post('/api/v1/group-entitlements', payload)
}

/** 撤销授权 */
export async function revokeEntitlement(payload: EntitlementPayload): Promise<void> {
  await http.delete('/api/v1/group-entitlements', { data: payload })
}

// ===================== v0.7 Service Key =====================

/** 获取部门 Service Key 列表（分页） */
export async function listServiceKeys(
  deptId: string,
  page = 1,
  pageSize = 20
): Promise<{ items: ServiceKeyItem[]; total: number }> {
  const params: Record<string, string | number> = { page, page_size: pageSize }
  if (deptId) params.dept_id = deptId
  const res = await http.get<{ items: RawServiceKeyItem[]; total: number }>(
    '/api/v1/service-keys', { params }
  )
  return {
    items: (res.data.items || []).map(mapServiceKey),
    total: res.data.total || 0,
  }
}

/** 创建 Service Key（返回明文 key） */
export async function createServiceKey(deptId: string, name: string, purpose: string): Promise<CreateServiceKeyResponse> {
  const res = await http.post<CreateServiceKeyResponse>('/api/v1/service-keys', {
    dept_id: deptId, name, purpose
  })
  return res.data
}

/** 启用/停用 Service Key */
export async function toggleServiceKey(keyId: string, enabled: boolean): Promise<void> {
  await http.patch(`/api/v1/service-keys/${keyId}/status`, { enabled })
}

/** 吊销 Service Key */
export async function deleteServiceKey(keyId: string): Promise<void> {
  await http.delete(`/api/v1/service-keys/${keyId}`)
}

// ===================== v0.7 扩容审批 =====================

/** 发起扩容申请 */
export async function createExpansionRequest(payload: CreateExpansionPayload): Promise<ExpansionRequest> {
  // 后端直接返回裸对象（HTTP 201），无 ApiResponse 包装
  const res = await http.post<ExpansionRequest>('/api/v1/expansion-requests', payload)
  return res.data
}

/** 获取我的扩容申请列表 */
export async function getMyExpansionRequests(status?: string): Promise<ExpansionRequest[]> {
  const params: Record<string, string> = {}
  if (status) params.status = status
  const res = await http.get<ApiResponse<{ items: ExpansionRequest[] }>>('/api/v1/expansion-requests', { params })
  return res.data.items
}

/** 获取待审批列表，name 非空时按申请人姓名模糊搜索 */
export async function getPendingExpansions(name?: string): Promise<ExpansionRequest[]> {
  const params: Record<string, string> = {}
  if (name) params.name = name
  const res = await http.get<ApiResponse<{ items: ExpansionRequest[] }>>('/api/v1/expansion-requests/pending', { params })
  return res.data.items
}

/** 审批扩容申请 */
export async function decideExpansion(requestId: string, action: 'approve' | 'reject' | 'escalate' | 'cancel', comment?: string): Promise<void> {
  await http.post(`/api/v1/expansion-requests/${requestId}/decision`, { action, comment })
}

/** 批量审批扩容申请 */
export async function batchDecideExpansion(decisions: Array<{ id: string; action: string; comment: string }>): Promise<{ results: Array<{ id: string; ok: boolean; error?: string }> }> {
  const res = await http.post<ApiResponse<{ results: Array<{ id: string; ok: boolean; error?: string }> }>>('/api/v1/expansion-requests/batch-decision', { decisions })
  return res.data
}

/** 获取扩容申请的审批流水 */
export async function getExpansionSteps(requestId: string): Promise<Array<{ id: string; level: string; approver_id?: string; action: string; comment: string; created_at: string }>> {
  const res = await http.get<ApiResponse<{ steps: Array<{ id: string; level: string; approver_id?: string; action: string; comment: string; created_at: string }> }>>(`/api/v1/expansion-requests/${requestId}/steps`)
  return res.data.steps || []
}
