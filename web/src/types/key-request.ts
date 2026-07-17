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
  approver_name?: string
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
  /** 当前用户创建 Key 是否需走申请（负责人为 false） */
  requires_approval: boolean
  /** 部门 Key 创建策略 */
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

/** 解析申请中的月预算金额（兼容字符串） */
export function keyRequestBudgetAmount(budgetMonthly?: number | string | null): number {
  const n = Number(budgetMonthly)
  return Number.isFinite(n) && n > 0 ? n : 0
}

/** 是否附带预算申请 */
export function keyRequestHasBudget(budgetMonthly?: number | string | null): boolean {
  return keyRequestBudgetAmount(budgetMonthly) > 0
}

/** 格式化月预算展示，如 $100/月 */
export function formatKeyRequestBudget(budgetMonthly?: number | string | null): string {
  const n = keyRequestBudgetAmount(budgetMonthly)
  if (n <= 0) return ''
  return n % 1 === 0 ? `$${n}/月` : `$${n.toFixed(2)}/月`
}

export const KEY_REQUEST_BUDGET_NONE_LABEL = '未申请预算'
export const KEY_REQUEST_BUDGET_APPLIED_LABEL = '已申请预算'

export type AutoApproveItemStatus = 'approved' | 'skipped' | 'failed'

export interface KeyRequestAutoApproveItem {
  request_id: string
  key_name: string
  requester_name: string
  budget_monthly?: number
  status: AutoApproveItemStatus
  error?: string
  created_key_id?: string
  notified: boolean
}

export interface KeyRequestAutoApproveResult {
  total: number
  success: number
  failed: number
  items: KeyRequestAutoApproveItem[]
}

export interface SetDeptKeyCreationPolicyResult {
  dept_id: string
  policy: KeyCreationPolicy
  auto_approve?: KeyRequestAutoApproveResult
}
