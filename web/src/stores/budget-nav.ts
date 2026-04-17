import { defineStore } from 'pinia'
import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import type { BudgetScopeType } from '@/types/budget'

export type BudgetNodeType = BudgetScopeType
export interface BudgetNavNode {
  type: BudgetNodeType
  id: string
  label?: string
}

function arrayQuery(value: LocationQuery[string]): string[] {
  if (Array.isArray(value)) return value.filter((item): item is string => typeof item === 'string')
  return typeof value === 'string' ? [value] : []
}

export const useBudgetNavStore = defineStore('budgetNav', {
  state: () => ({
    tab: 'overview' as 'overview' | 'risks',
    current: { type: 'company', id: 'GLOBAL' } as BudgetNavNode,
    breadcrumbs: [] as BudgetNavNode[],
  }),
  actions: {
    hydrate(query: LocationQuery) {
      this.tab = query.tab === 'risks' ? 'risks' : 'overview'
      const types = arrayQuery(query.parent_scope_type)
      const ids = arrayQuery(query.parent_scope_id)
      const parents = ids.map((id, index) => ({
        type: (types[index] || 'company') as BudgetNodeType,
        id,
      }))
      this.current = {
        type: (typeof query.scope_type === 'string' ? query.scope_type : 'company') as BudgetNodeType,
        id: typeof query.scope_id === 'string' ? query.scope_id : 'GLOBAL',
      }
      this.breadcrumbs = [...parents, this.current]
    },
    navigate(node: BudgetNavNode) {
      const index = this.breadcrumbs.findIndex(item => item.type === node.type && item.id === node.id)
      this.breadcrumbs = index >= 0 ? this.breadcrumbs.slice(0, index + 1) : [...this.breadcrumbs, node]
      this.current = node
    },
    toQuery(): LocationQueryRaw {
      return {
        tab: this.tab === 'risks' ? 'risks' : undefined,
        scope_type: this.current.type,
        scope_id: this.current.id,
        parent_scope_type: this.breadcrumbs.slice(0, -1).map(item => item.type),
        parent_scope_id: this.breadcrumbs.slice(0, -1).map(item => item.id),
      }
    },
  },
})
