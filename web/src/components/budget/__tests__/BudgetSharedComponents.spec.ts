import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { nextTick } from 'vue'
import AdminPageHeader from '../AdminPageHeader.vue'
import BudgetKpiStrip from '../BudgetKpiStrip.vue'
import BudgetSelectionBar from '../BudgetSelectionBar.vue'
import BudgetStatusTag from '../BudgetStatusTag.vue'
import BudgetSummaryCard from '../BudgetSummaryCard.vue'
import PeriodSegmented from '../PeriodSegmented.vue'
import TierBudgetTags from '../TierBudgetTags.vue'
import TierColorDot from '../TierColorDot.vue'
import type {
  BudgetDisplayStatus,
  TierBudgetTagItem,
} from '@/composables/useBudgetDisplay'

const global = {
  plugins: [ElementPlus],
}

function makeTierTag(index: number): TierBudgetTagItem {
  return {
    id: `tier-${index}`,
    name: `等级 ${index}`,
    color: `#00000${index}`,
    state: 'normal',
    label: `等级 ${index} · $${index * 10}`,
  }
}

describe('预算共享展示组件', () => {
  it('AdminPageHeader 优先渲染 description，并支持 meta 和 actions slot', () => {
    const wrapper = mount(AdminPageHeader, {
      props: {
        title: '预算管理',
        description: '计划契约说明',
        subtitle: '兼容说明',
      },
      slots: {
        meta: '<span data-test="header-meta">刚刚更新</span>',
        actions: '<button type="button">新建预算</button>',
      },
    })

    expect(wrapper.get('h1').text()).toBe('预算管理')
    expect(wrapper.text()).toContain('计划契约说明')
    expect(wrapper.text()).not.toContain('兼容说明')
    expect(wrapper.get('[data-test="header-meta"]').text()).toBe('刚刚更新')
    expect(wrapper.findAll('button').some(button => button.text() === '新建预算')).toBe(true)
  })

  it('AdminPageHeader 刷新按钮暴露 label、loading 状态并 emit refresh', async () => {
    const wrapper = mount(AdminPageHeader, {
      props: { title: '预算管理', loading: false },
    })
    const refresh = wrapper.get('[data-test="admin-page-header-refresh"]')

    expect(refresh.attributes('type')).toBe('button')
    expect(refresh.attributes('aria-label')).toBe('刷新')
    expect(refresh.attributes('aria-busy')).toBe('false')
    await refresh.trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    await wrapper.setProps({ loading: true })
    expect(refresh.attributes('aria-busy')).toBe('true')
    expect(refresh.attributes('disabled')).toBeDefined()
    expect(refresh.text()).toContain('刷新中')
  })

  it('TierColorDot 使用合法 API 颜色并回退非法颜色', () => {
    const valid = mount(TierColorDot, { props: { color: '#12abEF' } })
    const invalid = mount(TierColorDot, { props: { color: 'url(javascript:alert(1))' } })

    expect(valid.get('[data-test="tier-color-dot"]').attributes('style')).toContain('#12abEF')
    expect(invalid.get('[data-test="tier-color-dot"]').attributes('style')).toContain(
      'var(--budget-tier-muted)',
    )
  })

  it('TierBudgetTags 在空列表时显示未配置文案', () => {
    const wrapper = mount(TierBudgetTags, {
      props: { items: [] },
      global,
    })

    expect(wrapper.text()).toContain('未配置子预算')
  })

  it('TierBudgetTags 最多显示三项并汇总剩余数量', () => {
    const wrapper = mount(TierBudgetTags, {
      props: { items: [1, 2, 3, 4, 5].map(makeTierTag) },
      global,
    })

    expect(wrapper.text()).toContain('等级 1 · $10')
    expect(wrapper.text()).toContain('等级 3 · $30')
    expect(wrapper.text()).not.toContain('等级 4 · $40')
    expect(wrapper.text()).toContain('+2')
  })

  it('BudgetKpiStrip 点击可点击指标时 emit select(key)', async () => {
    const wrapper = mount(BudgetKpiStrip, {
      props: {
        items: [
          { key: 'total', label: '总预算', value: '$100', clickable: true },
          { key: 'used', label: '已使用', value: '$20' },
        ],
      },
    })

    await wrapper.get('[data-test="budget-kpi-total"]').trigger('click')
    await wrapper.get('[data-test="budget-kpi-used"]').trigger('click')

    expect(wrapper.emitted('select')).toEqual([['total']])
  })

  it('BudgetKpiStrip 优先显示 note，并限制可访问进度到 0..100', () => {
    const wrapper = mount(BudgetKpiStrip, {
      props: {
        items: [
          {
            key: 'risk',
            label: '风险预算',
            value: 2,
            note: '计划说明',
            meta: '兼容说明',
            progress: 125,
          },
          {
            key: 'remaining',
            label: '剩余预算',
            value: '$0',
            progress: -10,
          },
        ],
      },
    })

    expect(wrapper.text()).toContain('计划说明')
    expect(wrapper.text()).not.toContain('兼容说明')
    const progressbars = wrapper.findAll('[role="progressbar"]')
    expect(progressbars).toHaveLength(2)
    expect(progressbars[0].attributes('aria-valuenow')).toBe('100')
    expect(progressbars[1].attributes('aria-valuenow')).toBe('0')
  })

  it('BudgetSelectionBar count 为 0 时不渲染', () => {
    const wrapper = mount(BudgetSelectionBar, {
      props: { count: 0 },
    })

    expect(wrapper.find('.budget-selection').exists()).toBe(false)
  })

  it('BudgetSelectionBar 显示选中数、余量并可清除', async () => {
    const wrapper = mount(BudgetSelectionBar, {
      props: { count: 3, remainingText: '剩余可分配 $80' },
      slots: { actions: '<button type="button">批量调整</button>' },
    })

    expect(wrapper.text()).toContain('已选择 3 项')
    expect(wrapper.text()).toContain('剩余可分配 $80')
    expect(wrapper.text()).toContain('批量调整')
    expect(wrapper.get('.budget-selection').attributes('role')).toBe('status')
    const clear = wrapper.get('[data-test="budget-selection-clear"]')
    expect(clear.attributes('aria-label')).toBe('清除选择')
    await clear.trigger('click')
    expect(wrapper.emitted('clear')).toHaveLength(1)
  })

  it('BudgetSummaryCard 渲染摘要、状态 slot 和进度', () => {
    const wrapper = mount(BudgetSummaryCard, {
      props: {
        title: '公司预算',
        meta: '月度',
        amount: '$1,000.00',
        usagePercent: 62,
      },
      slots: { status: '<span>正常</span>' },
      global,
    })

    expect(wrapper.text()).toContain('公司预算')
    expect(wrapper.text()).toContain('月度')
    expect(wrapper.text()).toContain('$1,000.00')
    expect(wrapper.text()).toContain('正常')
    expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('62')
  })

  it('PeriodSegmented 提供三个可访问选项并点击月更新 v-model', async () => {
    const wrapper = mount(PeriodSegmented, {
      props: { modelValue: 'daily' },
      attachTo: document.body,
    })

    const group = wrapper.get('.el-segmented[role="radiogroup"]')
    expect(group.attributes('aria-label')).toBe('预算周期')
    const daily = wrapper.get('[data-test="period-daily"]')
    const weekly = wrapper.get('[data-test="period-weekly"]')
    const monthly = wrapper.get('[data-test="period-monthly"]')
    expect(daily.attributes('aria-pressed')).toBe('true')
    expect(weekly.attributes('aria-pressed')).toBe('false')
    expect(monthly.attributes('aria-pressed')).toBe('false')
    expect(group.findAll('input[type="radio"]')).toHaveLength(3)
    expect(group.get('input[type="radio"]').element.checked).toBe(true)

    const optionLabels = group.findAll('label.el-segmented__item')
    expect(optionLabels).toHaveLength(3)
    expect(monthly.element.closest('label')).toBe(optionLabels[2].element)
    optionLabels[2].element.click()
    await nextTick()

    expect(wrapper.emitted('update:modelValue')).toEqual([['monthly']])
    wrapper.unmount()
  })

  it('BudgetStatusTag 映射全部 BudgetDisplayStatus tone', () => {
    const cases: Array<{
      tone: BudgetDisplayStatus['tone']
      expectedType: string
    }> = [
      { tone: 'success', expectedType: 'success' },
      { tone: 'warning', expectedType: 'warning' },
      { tone: 'info', expectedType: 'info' },
      { tone: 'neutral', expectedType: 'info' },
      { tone: 'danger', expectedType: 'danger' },
    ]

    for (const { tone, expectedType } of cases) {
      const status: BudgetDisplayStatus = {
        label: `状态-${tone}`,
        tone,
        blocked: tone === 'danger',
      }
      const wrapper = mount(BudgetStatusTag, {
        props: { status },
        global,
      })

      expect(wrapper.text()).toContain(`状态-${tone}`)
      expect(wrapper.get('.el-tag').classes()).toContain(`el-tag--${expectedType}`)
    }
  })
})
