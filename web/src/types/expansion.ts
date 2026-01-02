
// v0.7 扩容申请类型

export type ExpansionStatus = 'pending' | 'approved' | 'rejected' | 'cancelled'
export type ExpansionLevel = 'dept' | 'studio' | 'platform'
export type ExpansionAction = 'approve' | 'reject' | 'escalate' | 'cancel'
export type ExpansionPeriod = 'daily' | 'weekly' | 'monthly'

export interface ExpansionRequest {
  id: string
  scope_type: string
  scope_id: string
  requester_id: string
  period_type: ExpansionPeriod
  current_amount?: number
  requested_amount: number
  reason: string
  status: ExpansionStatus
  current_level: ExpansionLevel
  current_usage_ratio?: number  // 审批列表用：申请人当前使用率
  created_at: string
  updated_at: string
}

export interface CreateExpansionPayload {
  scope_type: string
  scope_id: string
  period_type: ExpansionPeriod
  requested_amount: number
  reason?: string
}

export const PERIOD_LABELS: Record<ExpansionPeriod, string> = {
  daily: '日额度',
  weekly: '周额度',
  monthly: '月额度',
}

export const PERIOD_SUFFIX: Record<ExpansionPeriod, string> = {
  daily: '/ 天',
  weekly: '/ 周',
  monthly: '/ 月',
}

export interface DecideExpansionPayload {
  action: ExpansionAction
  comment?: string
}

export const STATUS_LABELS: Record<ExpansionStatus, string> = {
  pending: '审批中',
  approved: '已批准',
  rejected: '已拒绝',
  cancelled: '已取消',
}

export const STATUS_COLORS: Record<ExpansionStatus, string> = {
  pending: '#f59e0b',
  approved: '#10b981',
  rejected: '#ef4444',
  cancelled: '#6b7280',
}

export const LEVEL_LABELS: Record<ExpansionLevel, string> = {
  dept: '部门级',
  studio: '工作室级',
  platform: '平台级',
}
