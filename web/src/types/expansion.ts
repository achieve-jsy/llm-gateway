
// v0.7 扩容申请类型

export type ExpansionStatus = 'pending' | 'approved' | 'rejected' | 'cancelled'
export type ExpansionLevel = 'dept' | 'studio' | 'platform'
export type ExpansionAction = 'approve' | 'reject' | 'escalate' | 'cancel'

export interface ExpansionRequest {
  id: string
  scope_type: string
  scope_id: string
  requester_id: string
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
  requested_amount: number
  reason?: string
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
