export interface Notification {
  id: string
  user_id: string
  type: 'budget_alert' | 'expansion_approved' | 'expansion_rejected' | string
  level: 'info' | 'warning' | 'critical'
  title: string
  content: string
  ref_type: string
  ref_id: string
  is_read: boolean
  read_at: string | null
  created_at: string
}

/** 管理端通知历史统一行：站内信 + email/webhook 投递记录 */
export interface AdminNotification {
  channel: 'inapp' | 'email' | 'webhook'
  id: string
  user_id: string
  user_name: string
  user_email: string
  notification_type: string
  level: string // 仅站内信有；email/webhook 为空串
  title: string
  content: string
  is_read: boolean
  read_at: string | null
  created_at: string
  // 投递状态（仅 email/webhook）；站内信为空串
  delivery_status: string
  delivered_at: string | null
  last_error: string
  attempts: number
}

export interface AdminNotificationListResponse {
  items: AdminNotification[]
  total: number
  page: number
  page_size: number
}
