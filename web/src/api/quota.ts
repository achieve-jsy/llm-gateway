import http from '@/services/http'

// 各主体类型默认配额常量（与后端保持一致）
export const QUOTA_DEFAULTS = {
  user: { rpm: 60, max_concurrency: 10 },
  service_key: { rpm: 120, max_concurrency: 20 },
  group: { rpm: 0, max_concurrency: 0 },
  model: { rpm: 0, max_concurrency: 0 },
} as const

// 配额 API 响应类型
export interface QuotaEffective {
  rpm: number
  max_concurrency: number
  source: 'default_user' | 'default_service_key' | 'override' | 'unset'
  subject_type: 'user' | 'service_key' | 'group' | 'model'
  subject_id: string
  default_rpm?: number
  default_max_concurrency?: number
}

export interface QuotaCurrentUsage {
  rpm: number
  max_concurrency: number
}

export interface MyQuotaResponse {
  effective: QuotaEffective
  current_usage: QuotaCurrentUsage
}

export interface QuotaPatchRequest {
  rpm: number | null
  max_concurrency: number | null
}

// 用户配额
export async function getUserQuota(userId: string) {
  return http.get(`/api/v1/users/${userId}/quota`)
}

export async function setUserQuota(userId: string, data: QuotaPatchRequest) {
  return http.patch(`/api/v1/users/${userId}/quota`, data)
}

// Service Key 配额
export async function getServiceKeyQuota(keyId: string) {
  return http.get(`/api/v1/keys/${keyId}/quota`)
}

export async function setServiceKeyQuota(keyId: string, data: QuotaPatchRequest) {
  return http.patch(`/api/v1/keys/${keyId}/quota`, data)
}

// 模型组配额
export async function getGroupQuota(groupId: string) {
  return http.get(`/api/v1/groups/${groupId}/quota`)
}

export async function setGroupQuota(groupId: string, data: QuotaPatchRequest) {
  return http.patch(`/api/v1/groups/${groupId}/quota`, data)
}

// 模型配额
export async function getModelQuota(modelId: string) {
  return http.get(`/api/v1/models/${modelId}/quota`)
}

export async function setModelQuota(modelId: string, data: QuotaPatchRequest) {
  return http.patch(`/api/v1/models/${modelId}/quota`, data)
}

// 我的配额（只读）
export async function getMyQuota() {
  return http.get('/api/v1/me/quota')
}

// ── 配额豁免 ─────────────────────────────────────────────
export interface QuotaExemption {
  id: string
  subject_type: 'user' | 'service_key'
  subject_id: string
  subject_name: string
  reason: string
  expires_at: string
  created_by: string
  created_by_name: string
  created_at: string
}

export interface QuotaExemptionCreateRequest {
  subject_type: 'user' | 'service_key'
  subject_id: string
  reason: string
  expires_at: string
}

export interface ListExemptionsParams {
  page?: number
  pageSize?: number
  keyword?: string
  subject_type?: string
  status?: string
}

export async function listQuotaExemptions(params: ListExemptionsParams = {}) {
  const { page = 1, pageSize = 20, keyword, subject_type, status } = params
  let url = `/api/v1/quota/exemptions?page=${page}&page_size=${pageSize}`
  if (keyword) url += `&keyword=${encodeURIComponent(keyword)}`
  if (subject_type) url += `&subject_type=${encodeURIComponent(subject_type)}`
  if (status) url += `&status=${encodeURIComponent(status)}`
  return http.get(url)
}

export async function createQuotaExemption(data: QuotaExemptionCreateRequest) {
  return http.post('/api/v1/quota/exemptions', data)
}

export async function deleteQuotaExemption(id: string) {
  return http.delete(`/api/v1/quota/exemptions/${id}`)
}
