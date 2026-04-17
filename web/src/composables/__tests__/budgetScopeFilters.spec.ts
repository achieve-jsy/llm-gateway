import { describe, expect, it } from 'vitest'
import type { BudgetItem } from '@/types/budget'
import {
  batchEntryModeToWizardMode,
  filterDeptPoolItems,
  filterMemberItemsByDept,
} from '../budgetScopeFilters'

function item(overrides: Partial<BudgetItem>): BudgetItem {
  return {
    allocation_id: 'a',
    scope_type: 'member',
    scope_id: 'm1',
    label: 'x',
    amount_usd: 10,
    used_usd: 1,
    used_ratio: 0.1,
    ...overrides,
  } as BudgetItem
}

describe('budgetScopeFilters', () => {
  it('batchEntryModeToWizardMode 映射三键', () => {
    expect(batchEntryModeToWizardMode('absolute')).toBe('total_fixed')
    expect(batchEntryModeToWizardMode('percentage')).toBe('total_percent')
    expect(batchEntryModeToWizardMode('tier')).toBe('tier')
  })

  it('filterDeptPoolItems 无 deptId 返回全部池；有 deptId 仅该部门', () => {
    const rows = [
      item({ scope_type: 'dept_daily', scope_id: 'd1' }),
      item({ scope_type: 'dept_service', scope_id: 'd1' }),
      item({ scope_type: 'dept_daily', scope_id: 'd2' }),
      item({ scope_type: 'studio', scope_id: 's1' }),
    ]
    expect(filterDeptPoolItems(rows, null)).toHaveLength(3)
    expect(filterDeptPoolItems(rows, 'd1').map(r => r.scope_type)).toEqual([
      'dept_daily',
      'dept_service',
    ])
  })

  it('filterMemberItemsByDept 按 parent_scope_id 过滤', () => {
    const rows = [
      item({ scope_type: 'member', scope_id: 'u1', parent_scope_id: 'd1' }),
      item({ scope_type: 'member', scope_id: 'u2', parent_scope_id: 'd2' }),
      item({ scope_type: 'dept_daily', scope_id: 'd1' }),
    ]
    expect(filterMemberItemsByDept(rows, null)).toHaveLength(2)
    expect(filterMemberItemsByDept(rows, 'd1').map(r => r.scope_id)).toEqual(['u1'])
  })
})
