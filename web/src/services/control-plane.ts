import http from '@/lib/http'
import type { Provider, ProviderPayload, ProviderDefinition } from '@/types/provider'
import type { Model, ModelPayload } from '@/types/model'
import type { ModelTag, ModelTagPayload } from '@/types/model-tag'
import type { SyncResult, ConfirmSyncPayload, ConfirmSyncResult, CheckModelsPayload, CheckModelsResult } from '@/types/sync'
import type {
  ModelCatalog, PriceDiffEntry, SyncTriggerResponse, ApplyDiffResponse,
  CreateFromCatalogPayload, ResettableOverrideField, AutoBindResult,
  BindCatalogPayload, BatchBindItem, BatchBindResult, CatalogBindingStats, CatalogSyncStatus
} from '@/types/catalog'
import type { ApiResponse, PaginatedResponse } from '@/types/api'
import type { ListKeysResponse, CreateKeyWrappedResponse } from '@/types/api-key'
import type { SystemSettings, PublicSettings } from '@/types/settings'

// ===================== 渠道定义 =====================

export async function getProviderDefinitions(): Promise<ProviderDefinition[]> {
  const r = await http.get<ApiResponse<ProviderDefinition[]>>('/api/v1/provider-definitions')
  return r.data.data
}

// ===================== 渠道管理 =====================

export async function getProviders(opts?: { page?: number; pageSize?: number; name?: string; definition_id?: string; tag?: string }): Promise<{ data: Provider[]; total: number }> {
  const params: Record<string, any> = {}
  if (opts?.page !== undefined) params.page = opts.page
  if (opts?.pageSize !== undefined) params.page_size = opts.pageSize
  if (opts?.name !== undefined) params.name = opts.name
  if (opts?.definition_id !== undefined) params.definition_id = opts.definition_id
  if (opts?.tag !== undefined) params.tag = opts.tag
  const r = await http.get<ApiResponse<Provider[]> & { total: number }>('/api/v1/providers', { params })
  return { data: r.data.data, total: r.data.total ?? r.data.data.length }
}

export async function getProvider(id: string): Promise<Provider> {
  const r = await http.get<ApiResponse<Provider>>(`/api/v1/providers/${id}`)
  return r.data.data
}

export async function createProvider(payload: ProviderPayload): Promise<Provider> {
  const r = await http.post<ApiResponse<Provider>>('/api/v1/providers', payload)
  return r.data.data
}

export async function updateProvider(id: string, payload: ProviderPayload): Promise<Provider> {
  const r = await http.put<ApiResponse<Provider>>(`/api/v1/providers/${id}`, payload)
  return r.data.data
}

export async function deleteProvider(id: string): Promise<void> {
  await http.delete(`/api/v1/providers/${id}`)
}

export async function toggleProviderStatus(id: string, enabled: boolean): Promise<Provider> {
  const r = await http.patch<ApiResponse<Provider>>(`/api/v1/providers/${id}/status`, { enabled })
  return r.data.data
}

export interface TestProviderResult {
  success: boolean; response_time: number; status_code?: number; model_count?: number; error?: string
  chat_success?: boolean; chat_response_time?: number; chat_model?: string; chat_error?: string
}

export async function testProvider(id: string, testChat?: boolean): Promise<TestProviderResult> {
  const r = await http.post<ApiResponse<TestProviderResult>>(`/api/v1/providers/${id}/test`, testChat ? { test_chat: true } : undefined)
  return r.data.data
}

// ===================== 渠道健康检测 =====================

export interface CheckResult {
  provider_id: string; provider_name: string; success: boolean; response_time: number; error?: string; auto_disabled: boolean
}

export interface CheckAllResult {
  total: number; success: number; failed: number; auto_disabled: number; results: CheckResult[]
}

export async function checkAllProviders(): Promise<CheckAllResult> {
  const r = await http.post<ApiResponse<CheckAllResult>>('/api/v1/providers/check-all', undefined, { timeout: 180000 })
  return r.data.data
}

export async function checkProvider(id: string): Promise<CheckResult> {
  const r = await http.post<ApiResponse<CheckResult>>(`/api/v1/providers/${id}/check`)
  return r.data.data
}

// ===================== 健康检测配置 =====================

export interface HealthCheckConfig {
  interval_minutes: number; interval_seconds: number; max_failures: number; enabled: boolean
  check_providers: boolean; check_models: boolean; check_mode: string; timeout_seconds: number
  failure_action: string; model_check_enabled: boolean; model_check_interval_minutes: number
  model_check_interval_seconds: number; model_check_failure_threshold: number
}

export async function getHealthCheckConfig(): Promise<HealthCheckConfig> {
  const r = await http.get<ApiResponse<HealthCheckConfig>>('/api/v1/health-check/config')
  return r.data.data
}

export async function updateHealthCheckConfig(payload: Partial<HealthCheckConfig>): Promise<HealthCheckConfig> {
  const r = await http.put<ApiResponse<HealthCheckConfig>>('/api/v1/health-check/config', payload)
  return r.data.data
}

// ===================== 健康监控时间线 =====================

export interface TimelineEntry {
  checked_at: string; success: boolean; response_time_ms: number; error_message?: string
}

export interface TimelineGroup {
  target_name: string; target_type: string; health_percent: number; total_checks: number
  success_checks: number; provider_count: number; key_count: number; timeline: TimelineEntry[]
}

export interface TimelineResult {
  lookback: string; total: number; groups: TimelineGroup[]
}

export async function getHealthTimeline(lookback = '6h', search = '', offset = 0, limit = 0): Promise<TimelineResult> {
  const r = await http.get<ApiResponse<TimelineResult>>('/api/v1/health-check/timeline', { params: { lookback, search, offset, limit } })
  return r.data.data
}

export interface GroupTimelineGroup {
  group_name: string; strategy: string; model_count: number; health_percent: number
  total_checks: number; success_checks: number; timeline: TimelineEntry[]
}

export interface GroupTimelineResult {
  lookback: string; total: number; groups: GroupTimelineGroup[]
}

export async function getGroupHealthTimeline(lookback = '6h', search = '', offset = 0, limit = 0): Promise<GroupTimelineResult> {
  const r = await http.get<ApiResponse<GroupTimelineResult>>('/api/v1/health-check/group-timeline', { params: { lookback, search, offset, limit } })
  return r.data.data
}

export async function checkAllGroups(): Promise<{ total: number; success: number; failed: number }> {
  const r = await http.post<ApiResponse<{ total: number; success: number; failed: number }>>('/api/v1/groups/check-all', undefined, { timeout: 180000 })
  return r.data.data
}

export async function checkGroup(id: number): Promise<{ group_id: number; group_name: string; success: boolean; response_time: number; error?: string }> {
  const r = await http.post<ApiResponse<{ group_id: number; group_name: string; success: boolean; response_time: number; error?: string }>>(`/api/v1/groups/${id}/check`)
  return r.data.data
}

// ===================== 模型管理 =====================

export interface ModelCheckResult {
  model_id: string; model_name: string; success: boolean; response_time: number; error?: string; auto_disabled: boolean; auto_reenabled: boolean
}

export interface ModelCheckAllResult {
  total: number; success: number; failed: number; failure_summary?: Record<string, number>; results: ModelCheckResult[]
}

export async function checkModel(id: string): Promise<ModelCheckResult> {
  const r = await http.post<ApiResponse<ModelCheckResult>>(`/api/v1/models/${id}/check`)
  return r.data.data
}

export async function checkAllModels(): Promise<ModelCheckAllResult> {
  const r = await http.post<ApiResponse<ModelCheckAllResult>>('/api/v1/models/check-all', undefined, { timeout: 180000 })
  return r.data.data
}

export async function getModels(providerId?: string, opts?: { page?: number; pageSize?: number; name?: string; model_type?: string; enabled?: boolean }): Promise<{ data: Model[]; total: number }> {
  const params: Record<string, any> = {}
  if (providerId) params.provider_id = providerId
  if (opts?.page !== undefined) params.page = opts.page
  if (opts?.pageSize !== undefined) params.page_size = opts.pageSize
  if (opts?.name !== undefined) params.name = opts.name
  if (opts?.model_type !== undefined) params.model_type = opts.model_type
  if (opts?.enabled !== undefined) params.enabled = opts.enabled
  const r = await http.get<PaginatedResponse<Model>>('/api/v1/models', { params })
  return { data: r.data.data, total: r.data.total }
}

export async function createModel(payload: ModelPayload): Promise<Model> {
  const r = await http.post<ApiResponse<Model>>('/api/v1/models', payload)
  return r.data.data
}

export async function updateModel(id: string, payload: ModelPayload): Promise<Model> {
  const r = await http.put<ApiResponse<Model>>(`/api/v1/models/${id}`, payload)
  return r.data.data
}

export async function deleteModel(id: string): Promise<void> {
  await http.delete(`/api/v1/models/${id}`)
}

export async function batchDeleteModels(modelIds: string[]): Promise<{ deleted: number }> {
  const r = await http.delete<ApiResponse<{ deleted: number }>>('/api/v1/models/batch', { data: { model_ids: modelIds } })
  return r.data.data
}

export async function batchUpdateModelStatus(modelIds: string[], enabled: boolean): Promise<{ updated: number }> {
  const r = await http.patch<ApiResponse<{ updated: number }>>('/api/v1/models/batch-status', { model_ids: modelIds, enabled })
  return r.data.data
}

export async function checkModelNameUnique(name: string): Promise<boolean> {
  const r = await http.get<ApiResponse<{ available: boolean }>>('/api/v1/models/check-name', { params: { name } })
  return r.data.data.available
}

// ===================== 模型标签 =====================

export async function getModelTags(): Promise<ModelTag[]> {
  const r = await http.get<ApiResponse<ModelTag[]>>('/api/v1/model-tags')
  return r.data.data
}

export async function createModelTag(payload: ModelTagPayload): Promise<ModelTag> {
  const r = await http.post<ApiResponse<ModelTag>>('/api/v1/model-tags', payload)
  return r.data.data
}

export async function updateModelTag(id: string, payload: ModelTagPayload): Promise<ModelTag> {
  const r = await http.put<ApiResponse<ModelTag>>(`/api/v1/model-tags/${id}`, payload)
  return r.data.data
}

export async function deleteModelTag(id: string): Promise<void> {
  await http.delete(`/api/v1/model-tags/${id}`)
}

export async function getModelTagsForModel(modelId: string): Promise<ModelTag[]> {
  const r = await http.get<ApiResponse<ModelTag[]>>(`/api/v1/models/${modelId}/tags`)
  return r.data.data
}

export async function setModelTags(modelId: string, tagIds: string[]): Promise<ModelTag[]> {
  const r = await http.put<ApiResponse<ModelTag[]>>(`/api/v1/models/${modelId}/tags`, { tag_ids: tagIds })
  return r.data.data
}

export async function batchSetModelTags(modelIds: string[], tagIds: string[]): Promise<{ updated: number }> {
  const r = await http.put<ApiResponse<{ updated: number }>>('/api/v1/models/batch-tags', { model_ids: modelIds, tag_ids: tagIds })
  return r.data.data
}

export async function batchGetModelTags(modelIds: string[]): Promise<Record<string, ModelTag[]>> {
  const r = await http.post<ApiResponse<Record<string, ModelTag[]>>>('/api/v1/models/batch-tags/query', { model_ids: modelIds })
  return r.data.data
}

// ===================== 模型同步 =====================

export async function syncModels(providerId: string): Promise<SyncResult> {
  const r = await http.post<ApiResponse<SyncResult>>(`/api/v1/providers/${providerId}/sync-models`)
  return r.data.data
}

export async function confirmSync(providerId: string, payload: ConfirmSyncPayload): Promise<ConfirmSyncResult> {
  const r = await http.post<ApiResponse<ConfirmSyncResult>>(`/api/v1/providers/${providerId}/sync-models/confirm`, payload)
  return r.data.data
}

export async function checkModels(providerId: string, payload: CheckModelsPayload): Promise<CheckModelsResult> {
  const r = await http.post<ApiResponse<CheckModelsResult>>(`/api/v1/providers/${providerId}/check-models`, payload)
  return r.data.data
}

// ===================== Catalog =====================

export async function searchCatalog(opts?: { q?: string; provider?: string; mode?: string; limit?: number }): Promise<{ data: ModelCatalog[]; total: number }> {
  const params: Record<string, any> = {}
  if (opts?.q !== undefined) params.q = opts.q
  if (opts?.provider !== undefined) params.provider = opts.provider
  if (opts?.mode !== undefined) params.mode = opts.mode
  if (opts?.limit !== undefined) params.limit = opts.limit
  const r = await http.get<ApiResponse<ModelCatalog[]> & { total: number }>('/api/v1/model-catalog/search', { params })
  return { data: r.data.data, total: r.data.total }
}

export async function getCatalogBySource(sourceModelId: string): Promise<ModelCatalog> {
  const r = await http.get<ApiResponse<ModelCatalog>>('/api/v1/model-catalog/by-source', { params: { source_model_id: sourceModelId } })
  return r.data.data
}

export async function triggerCatalogSync(): Promise<SyncTriggerResponse> {
  const r = await http.post<ApiResponse<SyncTriggerResponse>>('/api/v1/model-catalog/sync')
  return r.data.data
}

export async function getCatalogBindingStats(): Promise<CatalogBindingStats> {
  const r = await http.get<ApiResponse<CatalogBindingStats>>('/api/v1/model-catalog/binding-stats')
  return r.data.data
}

export async function getCatalogSyncStatus(): Promise<CatalogSyncStatus> {
  const r = await http.get<ApiResponse<CatalogSyncStatus>>('/api/v1/model-catalog/sync-status')
  return r.data.data
}

export async function getCatalogDiff(): Promise<{ data: PriceDiffEntry[]; total: number }> {
  const r = await http.get<ApiResponse<PriceDiffEntry[]> & { total: number }>('/api/v1/model-catalog/diff')
  return { data: r.data.data, total: r.data.total }
}

export async function applyCatalogDiff(modelIds: string[]): Promise<ApplyDiffResponse> {
  const r = await http.post<ApiResponse<ApplyDiffResponse>>('/api/v1/model-catalog/apply-diff', { model_ids: modelIds })
  return r.data.data
}

export async function autoBindCatalog(): Promise<AutoBindResult> {
  const r = await http.post<ApiResponse<AutoBindResult>>('/api/v1/model-catalog/auto-bind')
  return r.data.data
}

export async function createModelFromCatalog(payload: CreateFromCatalogPayload): Promise<Model> {
  const r = await http.post<ApiResponse<Model>>('/api/v1/models/from-catalog', payload)
  return r.data.data
}

export async function resetModelOverride(modelId: string, field: ResettableOverrideField): Promise<Model> {
  const r = await http.post<ApiResponse<Model>>(`/api/v1/models/${modelId}/reset-override`, { field })
  return r.data.data
}

export async function bindModelCatalog(modelId: string, payload: BindCatalogPayload): Promise<Model> {
  const r = await http.post<ApiResponse<Model>>(`/api/v1/models/${modelId}/bind-catalog`, payload)
  return r.data.data
}

export async function unbindModelCatalog(modelId: string): Promise<Model> {
  const r = await http.post<ApiResponse<Model>>(`/api/v1/models/${modelId}/unbind-catalog`)
  return r.data.data
}

export async function batchBindModelCatalog(bindings: BatchBindItem[]): Promise<BatchBindResult> {
  const r = await http.post<ApiResponse<BatchBindResult>>('/api/v1/models/batch-bind-catalog', { bindings })
  return r.data.data
}

// ===================== API Key =====================

export interface AvailableGroupsResponse {
  data: { groups: Array<{ id: number; name: string; description: string; strategy: string }> }
}

/** 返回 { data: { items, page, page_size, total } } */
export async function listKeys(page = 1, size = 20): Promise<ListKeysResponse> {
  const r = await http.get<ListKeysResponse>('/api/v1/keys', { params: { page, page_size: size } })
  return r.data
}

/** 返回 { data: { id, name, key, key_hint, ... } } */
export async function createKey(name: string, expiresAt?: string, groupIds?: number[]): Promise<CreateKeyWrappedResponse> {
  const r = await http.post<CreateKeyWrappedResponse>('/api/v1/keys', {
    name,
    expires_at: expiresAt || undefined,
    group_ids: groupIds?.length ? groupIds : undefined,
  })
  return r.data
}

export async function revealKey(id: string): Promise<{ key: string }> {
  const r = await http.post<{ data: { key: string } }>(`/api/v1/keys/${id}/reveal`)
  return r.data.data
}

/** 返回 { data: { groups: [...] } } */
export async function getAvailableGroups(): Promise<AvailableGroupsResponse> {
  const r = await http.get<AvailableGroupsResponse>('/api/v1/groups/available')
  return r.data
}

export async function updateKeyGroups(keyId: string, add: number[], remove: number[]): Promise<void> {
  await http.post(`/api/v1/keys/${keyId}/groups`, { add, remove })
}

export async function toggleAccessAllGroups(keyId: string, value: boolean): Promise<void> {
  await http.post(`/api/v1/keys/${keyId}/access-all-groups`, { access_all_groups: value })
}

export async function disableKey(id: string): Promise<void> {
  await http.patch(`/api/v1/keys/${id}/disable`)
}

export async function enableKey(id: string): Promise<void> {
  await http.patch(`/api/v1/keys/${id}/enable`)
}

export async function deleteKey(id: string): Promise<void> {
  await http.delete(`/api/v1/keys/${id}`)
}

/** 重置 key，返回结构同 createKey */
export async function resetKey(id: string): Promise<CreateKeyWrappedResponse> {
  const r = await http.post<CreateKeyWrappedResponse>(`/api/v1/keys/${id}/reset`)
  return r.data
}

// ===================== 系统设置 =====================

export async function getPublicSettings(): Promise<PublicSettings> {
  const r = await http.get<{ data: PublicSettings }>('/api/v1/settings/public')
  return r.data.data
}

export async function getSystemSettings(): Promise<SystemSettings> {
  const r = await http.get<{ data: SystemSettings }>('/api/v1/settings')
  return r.data.data
}

export async function updateSystemSettings(settings: SystemSettings): Promise<void> {
  await http.put('/api/v1/settings', settings)
}

export async function testWebhook(): Promise<void> {
  await http.post('/api/v1/settings/test-webhook')
}

export async function exportSettings(): Promise<Record<string, string>> {
  const r = await http.post<{ data: Record<string, string> }>('/api/v1/settings/export')
  return r.data.data
}

export async function importSettings(settings: Record<string, string>): Promise<void> {
  await http.post('/api/v1/settings/import', settings)
}

// ===================== OpenCode 配置 =====================

export async function generateOpencodeConfigToken(keyId: string, params: { provider_id?: string; base_url: string; api_key: string }): Promise<{ token: string; ttl_seconds: number }> {
  const r = await http.post<{ data: { token: string; ttl_seconds: number } }>(`/api/v1/keys/${keyId}/opencode-config/token`, params)
  return r.data.data
}

export function getScriptURL(token: string, os: 'mac' | 'windows'): string {
  return `/api/v1/opencode-config/script?${new URLSearchParams({ token, os })}`
}

export async function getOpencodeConfigStatus(keyId: string, token: string): Promise<{ status: 'pending' | 'success' | 'failed'; msg?: string }> {
  const r = await http.get<{ data: { status: string; msg?: string } }>(`/api/v1/keys/${keyId}/opencode-config/status`, { params: { token } })
  return r.data.data as { status: 'pending' | 'success' | 'failed'; msg?: string }
}
