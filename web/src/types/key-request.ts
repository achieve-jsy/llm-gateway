// Key 创建申请类型

export type KeyRequestStatus = 'pending' | 'approved' | 'rejected' | 'cancelled'
export type KeyCreationPolicy = 'free' | 'approval'

export interface KeyRequest {
  id: string
  requester_id: string
  requester_name?: string
  dept_id?: string
  dept_name?: string
  key_name: string
  key_type: string
  purpose?: string
  group_ids?: string  // JSON string "[1,2,3]"
  expires_at?: string
  budget_monthly: number
  status: KeyRequestStatus
  approver_id?: string
  approve_comment?: string
  created_key_id?: string
  created_at: string
  updated_at: string
}

export interface CreateKeyRequestPayload {
  key_name: string
  purpose?: string
  group_ids?: number[]
  expires_at?: string
  budget_monthly?: number
}

export interface KeyRequestSummary {
  pending: number
  approved: number
  rejected: number
}

export interface KeyCreationPolicyInfo {
  requires_approval: boolean
  policy: KeyCreationPolicy
}

export const KEY_REQUEST_STATUS_LABELS: Record<KeyRequestStatus, string> = {
  pending: '审批中',
  approved: '已通过',
  rejected: '已拒绝',
  cancelled: '已取消',
}

export const KEY_REQUEST_STATUS_COLORS: Record<KeyRequestStatus, string> = {
  pending: 'warning',
  approved: 'success',
  rejected: 'danger',
  cancelled: 'info',
}

export const POLICY_LABELS: Record<KeyCreationPolicy, string> = {
  free: '自由创建',
  approval: '需要审批',
}
