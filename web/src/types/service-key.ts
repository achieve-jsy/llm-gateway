
// v0.7 Service Key 类型

export type ServiceKeyType = 'service'

export interface RawServiceKeyItem {
  id: string
  name: string
  masked_key: string
  key_type: 'service'
  purpose: string
  dept_id: string
  dept_name: string
  enabled: boolean
  ip_allowlist?: string
  created_by?: string
  created_at: string
  last_used_at?: string
}

export interface ServiceKeyItem {
  id: string
  name: string
  maskedKey: string
  keyType: 'service'
  purpose: string
  deptId: string
  deptName: string
  enabled: boolean
  ipAllowlist?: string[]
  createdBy?: string
  createdAt: string
  lastUsedAt?: string
}

export interface CreateServiceKeyResponse {
  id: string
  key: string
  key_type: string
  masked_key: string
  dept_id: string
  dept_name: string
  purpose: string
  ip_allowlist?: string
  created_by: string
}

export function mapServiceKey(item: RawServiceKeyItem): ServiceKeyItem {
  let ipAllowlist: string[] = []
  if (item.ip_allowlist) {
    try {
      const parsed = JSON.parse(item.ip_allowlist)
      if (Array.isArray(parsed)) {
        ipAllowlist = parsed
      }
    } catch {
      // ignore parse error
    }
  }

  return {
    id: item.id,
    name: item.name,
    maskedKey: item.masked_key,
    keyType: 'service',
    purpose: item.purpose,
    deptId: item.dept_id,
    deptName: item.dept_name,
    enabled: item.enabled,
    ipAllowlist,
    createdBy: item.created_by,
    createdAt: item.created_at,
    lastUsedAt: item.last_used_at,
  }
}
