import http from '@/services/http'

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

export async function listQuotaExemptions() {
  return http.get('/api/v1/quota/exemptions')
}

export async function createQuotaExemption(data: QuotaExemptionCreateRequest) {
  return http.post('/api/v1/quota/exemptions', data)
}

export async function deleteQuotaExemption(id: string) {
  return http.delete(`/api/v1/quota/exemptions/${id}`)
}
