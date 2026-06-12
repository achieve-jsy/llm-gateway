import http from './http'
import type { AuditLogQuery, AuditLogListResponse } from '@/types/audit'

/**
 * 查询审计日志列表
 */
export async function getAuditLogs(query: AuditLogQuery): Promise<AuditLogListResponse> {
  const params: Record<string, any> = {}
  if (query.user_id) params.user_id = query.user_id
  if (query.action) params.action = query.action
  if (query.resource_type) params.resource_type = query.resource_type
  if (query.dept_id) params.dept_id = query.dept_id
  if (query.search) params.search = query.search
  if (query.sort) params.sort = query.sort
  if (query.from) params.from = query.from
  if (query.to) params.to = query.to
  if (query.page) params.page = query.page
  if (query.page_size) params.page_size = query.page_size

  const res = await http.get<AuditLogListResponse>('/api/v1/audit-logs', { params })
  return res.data
}

/**
 * 更新 Service Key IP 白名单
 */
export async function updateServiceKeyIPAllowlist(
  keyId: string,
  ipAllowlist: string[]
): Promise<{ ok: boolean; ip_allowlist: string[] }> {
  const res = await http.patch<{ data: { ok: boolean; ip_allowlist: string[] } }>(
    `/api/v1/service-keys/${keyId}/ip-allowlist`,
    { ip_allowlist: ipAllowlist }
  )
  return res.data.data
}
