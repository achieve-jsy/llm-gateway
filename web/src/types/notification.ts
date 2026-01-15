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
