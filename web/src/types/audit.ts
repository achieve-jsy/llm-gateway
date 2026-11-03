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

export interface AuditDetail {
  method?: string
  path?: string
  full_route?: string
  duration_ms?: number
  status_code?: number
  request_id?: string
  referer?: string
  content_type?: string
  request_headers?: Record<string, string>
  request_body?: string
  response_body?: string
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
  // 渠道
  { value: 'provider.create', label: '渠道创建' },
  { value: 'provider.update', label: '渠道更新' },
  { value: 'provider.delete', label: '渠道删除' },
  { value: 'provider.toggle', label: '渠道启停' },
  { value: 'provider.test', label: '渠道测试' },
  { value: 'provider.sync_models', label: '渠道模型同步' },
  { value: 'provider.sync_models_confirm', label: '同步确认' },
  // 模型
  { value: 'model.create', label: '模型创建' },
  { value: 'model.update', label: '模型更新' },
  { value: 'model.delete', label: '模型删除' },
  { value: 'model.batch_update_status', label: '批量状态更新' },
  { value: 'model.check', label: '模型检查' },
  { value: 'model.reset_override', label: '重置覆盖' },
  { value: 'model.bind_catalog', label: '绑定 Catalog' },
  { value: 'model.unbind_catalog', label: '解绑 Catalog' },
  { value: 'model.tag.update', label: '模型标签更新' },
  { value: 'model.tag.query', label: '模型标签查询' },
  // 标签
  { value: 'tag.create', label: '标签创建' },
  { value: 'tag.update', label: '标签更新' },
  { value: 'tag.delete', label: '标签删除' },
  // 健康检查
  { value: 'health_check.run', label: '健康检查执行' },
  // 分组
  { value: 'group.create', label: '分组创建' },
  { value: 'group.update', label: '分组更新' },
  { value: 'group.delete', label: '分组删除' },
  { value: 'group.add_models', label: '添加模型到分组' },
  { value: 'group.remove_model', label: '从分组移除模型' },
  { value: 'group.check', label: '分组检查' },
  { value: 'group.entitlement_grant', label: '授权分组' },
  { value: 'group.entitlement_revoke', label: '撤销分组授权' },
  // Key
  { value: 'key.create', label: 'Key 创建' },
  { value: 'key.disable', label: 'Key 禁用' },
  { value: 'key.enable', label: 'Key 启用' },
  { value: 'key.delete', label: 'Key 删除' },
  { value: 'key.reset', label: 'Key 重置' },
  { value: 'key.reveal', label: 'Key 查看' },
  { value: 'key.bind_groups', label: '绑定 Key 分组' },
  { value: 'key.toggle_access_all', label: '全部模型组访问' },
  { value: 'key.toggle_log', label: '日志开关' },
  { value: 'key.opencode_config', label: 'OpenCode 配置' },
  // Service Key
  { value: 'service_key.create', label: 'Service Key 创建' },
  { value: 'service_key.delete', label: 'Service Key 删除' },
  { value: 'service_key.toggle', label: 'Service Key 启停' },
  { value: 'service_key.ip_allowlist_changed', label: 'IP 白名单变更' },
  // Catalog
  { value: 'catalog.sync', label: 'Catalog 同步' },
  { value: 'catalog.apply_diff', label: 'Catalog 应用差异' },
  { value: 'catalog.auto_bind', label: 'Catalog 自动绑定' },
  // 预算
  { value: 'budget.set', label: '预算设置' },
  { value: 'budget.delete', label: '预算删除' },
  // 扩容
  { value: 'expansion.create', label: '扩容申请' },
  { value: 'expansion.decision', label: '扩容审批' },
  // 公告
  { value: 'announcement.create', label: '公告创建' },
  { value: 'announcement.update', label: '公告更新' },
  { value: 'announcement.delete', label: '公告删除' },
  { value: 'announcement.publish', label: '公告发布' },
  { value: 'announcement.archive', label: '公告归档' },
  { value: 'announcement.read', label: '公告已读' },
  // 通知
  { value: 'notification.read', label: '通知已读' },
  { value: 'notification.read_all', label: '全部已读' },
  // 设置
  { value: 'settings.update', label: '系统设置更新' },
  { value: 'settings.test_webhook', label: 'Webhook 测试' },
  { value: 'settings.export', label: '设置导出' },
  { value: 'settings.import', label: '设置导入' },
  // 组织架构
  { value: 'studio.create', label: '工作室创建' },
  { value: 'studio.delete', label: '工作室删除' },
  { value: 'department.set_studio', label: '部门归属工作室' },
  { value: 'department.import_mapping', label: '部门映射导入' },
  { value: 'role.grant', label: '角色授予' },
  { value: 'role.revoke', label: '角色撤销' },
  { value: 'employee.import', label: '员工导入' },
  { value: 'user.change_department', label: '调整部门' },
  { value: 'user.follow_sso_department', label: '跟随 SSO 部门' },
  // 用户
  { value: 'user.login', label: '用户登录' },
  // 邮件
  { value: 'email.send_test', label: '邮件测试发送' },
  { value: 'email.send_alert', label: '告警邮件发送' },
  // Webhook
  { value: 'webhook.send_alert', label: 'Webhook 告警发送' },
]

// 资源类型常量
export const RESOURCE_TYPES = [
  { value: '', label: '全部资源' },
  { value: 'provider', label: '渠道' },
  { value: 'model', label: '模型' },
  { value: 'group', label: '分组' },
  { value: 'api_key', label: 'Key' },
  { value: 'service_key', label: 'Service Key' },
  { value: 'budget', label: '预算' },
  { value: 'expansion_request', label: '扩容申请' },
  { value: 'announcement', label: '公告' },
  { value: 'settings', label: '系统设置' },
  { value: 'health_check', label: '健康检查' },
  { value: 'user', label: '用户' },
  { value: 'employee', label: '员工' },
  { value: 'studio', label: '工作室' },
  { value: 'department', label: '部门' },
  { value: 'role', label: '角色' },
  { value: 'email', label: '邮件' },
  { value: 'unknown', label: '其他' },
]
