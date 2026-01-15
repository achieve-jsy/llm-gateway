// 审计日志类型定义

export interface AuditLog {
  id: string
  user_id: string
  user_name: string
  action: string
  resource_type: string
  resource_id: string
  resource_name: string
  dept_id: string
  dept_name: string
  status_code: number
  client_ip: string
  user_agent: string
  details: string
  created_at: string
}

export interface AuditLogQuery {
  user_id?: string
  action?: string
  resource_type?: string
  dept_id?: string
  from?: string
  to?: string
  page?: number
  page_size?: number
}

export interface AuditLogListResponse {
  items: AuditLog[]
  total: number
  page: number
  page_size: number
}

// 操作类型常量（用于下拉筛选）
export const AUDIT_ACTIONS = [
  { value: '', label: '全部操作' },
  { value: 'service_key.create', label: 'Service Key 创建' },
  { value: 'service_key.delete', label: 'Service Key 删除' },
  { value: 'service_key.toggle', label: 'Service Key 启停' },
  { value: 'service_key.ip_allowlist_changed', label: 'IP 白名单变更' },
  { value: 'provider.create', label: '渠道创建' },
  { value: 'provider.update', label: '渠道更新' },
  { value: 'provider.delete', label: '渠道删除' },
  { value: 'provider.toggle', label: '渠道启停' },
  { value: 'model.create', label: '模型创建' },
  { value: 'model.update', label: '模型更新' },
  { value: 'model.delete', label: '模型删除' },
  { value: 'group.create', label: '分组创建' },
  { value: 'group.update', label: '分组更新' },
  { value: 'group.delete', label: '分组删除' },
  { value: 'budget.set', label: '预算设置' },
  { value: 'user.login', label: '用户登录' },
]

// 资源类型常量
export const RESOURCE_TYPES = [
  { value: '', label: '全部资源' },
  { value: 'service_key', label: 'Service Key' },
  { value: 'provider', label: '渠道' },
  { value: 'model', label: '模型' },
  { value: 'group', label: '分组' },
  { value: 'budget', label: '预算' },
  { value: 'user', label: '用户' },
]
