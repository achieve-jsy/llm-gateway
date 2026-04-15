import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import BudgetPermissionBanner from '../BudgetPermissionBanner.vue'

describe('BudgetPermissionBanner', () => {
  it.each([
    ['admin', '平台管理员', '可调整当前层级，并继续向下分配'],
    ['studio_admin', '工作室管理员', '可配置下属部门；工作室总额度需申请扩容'],
    ['dept_admin', '部门管理员', '可配置成员额度；部门总预算需申请扩容'],
  ])('根据真实角色 %s 显示权限说明', (role, badge, detail) => {
    const wrapper = mount(BudgetPermissionBanner, {
      props: { role, scopeType: 'studio' },
    })

    expect(wrapper.text()).toContain(badge)
    expect(wrapper.text()).toContain(detail)
  })

  it('未知角色只显示只读权限且不渲染操作插槽', () => {
    const wrapper = mount(BudgetPermissionBanner, {
      props: { role: 'user', scopeType: 'dept' },
      slots: { actions: '<button>越权操作</button>' },
    })

    expect(wrapper.text()).toContain('只读访问')
    expect(wrapper.text()).not.toContain('越权操作')
  })
})
