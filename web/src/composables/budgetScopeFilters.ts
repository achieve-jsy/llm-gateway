import type { BudgetItem } from '@/types/budget'

/** 批量调整入口模式（对齐原型 selection-bar 三键） */
export type BudgetBatchEntryMode = 'absolute' | 'percentage' | 'tier'

export function batchEntryModeToWizardMode(
  mode: BudgetBatchEntryMode,
): 'total_fixed' | 'total_percent' | 'tier' {
  if (mode === 'percentage') return 'total_percent'
  if (mode === 'tier') return 'tier'
  return 'total_fixed'
}

/** 部门下钻 / 多部门筛选时，仅保留目标部门的日常+服务池 */
export function filterDeptPoolItems(
  items: BudgetItem[],
  deptId: string | null | undefined,
): BudgetItem[] {
  const pools = items.filter(
    item => item.scope_type === 'dept_daily' || item.scope_type === 'dept_service',
  )
  if (!deptId) return pools
  return pools.filter(item => item.scope_id === deptId)
}

/** 成员列表按所属部门过滤（parent_scope_id === deptId） */
export function filterMemberItemsByDept(
  items: BudgetItem[],
  deptId: string | null | undefined,
): BudgetItem[] {
  const members = items.filter(item => item.scope_type === 'member')
  if (!deptId) return members
  return members.filter(item => item.parent_scope_id === deptId)
}
