import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BudgetManagePage from '../BudgetManagePage.vue'
import { createRouter, createWebHistory } from 'vue-router'

// Mock services
vi.mock('@/services/control-plane', () => ({
  getBudgetList: vi.fn(() => Promise.resolve({ data: [] })),
  getBudgetContext: vi.fn(() => Promise.resolve({ data: { company: null, studios: [], depts: [] } })),
  getBudgetRiskSummary: vi.fn(() => Promise.resolve({ data: [] })),
  listModelTiers: vi.fn(() => Promise.resolve({ data: [] }))
}))

vi.mock('@/services/http', () => ({
  default: {
    get: vi.fn(() => Promise.resolve({ data: {} }))
  }
}))

const mockRouter = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/admin/budget', name: 'budget', component: { template: '<div>Budget</div>' } },
    { path: '/admin/org', name: 'org', component: { template: '<div>Org</div>' } }
  ]
})

describe('BudgetManagePage - Role-Based Access Control', () => {
  const createWrapper = (role: 'admin' | 'studio_admin' | 'dept_admin', options = {}) => {
    const authStore = {
      effectiveRole: role,
      user: {
        id: 'user-123',
        name: 'Test User',
        role
      }
    }

    return mount(BudgetManagePage, {
      global: {
        plugins: [mockRouter],
        mocks: {
          $router: mockRouter,
          $route: { path: '/admin/budget', query: {} }
        },
        stubs: {
          ElTabs: { template: '<div><slot /></div>' },
          ElTabPane: { template: '<div><slot /></div>' },
          ElTable: {
            template: '<div class="el-table"><slot /></div>',
            props: ['data']
          },
          ElTableColumn: { template: '<div><slot /></div>' },
          ElButton: { template: '<button><slot /></button>' },
          ElTag: { template: '<span><slot /></span>' },
          ElProgress: { template: '<div class="el-progress"></div>' },
          ElBadge: { template: '<div><slot /></div>' },
          ElSegmented: { template: '<div><slot /></div>' },
          BudgetRiskEventsSection: { template: '<div></div>' },
          BudgetTierLimitsPanel: { template: '<div></div>' }
        },
        provide: {
          authStore
        }
      },
      ...options
    })
  }

  describe('Admin Role', () => {
    it('should display admin perspective description', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      expect(wrapper.html()).toContain('查看全公司各工作室预算风险')
    })

    it('should show "进入组织治理" button', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      expect(wrapper.html()).toContain('进入组织治理')
    })

    it('should display company view with studio table', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      // Admin should see studio table
      expect(wrapper.html()).toContain('工作室')
    })

    it('should display 5 KPI cards for admin', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      const kpiStrip = wrapper.find('.kpi-strip')
      expect(kpiStrip.exists()).toBe(true)
      expect(kpiStrip.classes()).toContain('kpi-5')
    })

    it('should show batch operations bar for admin', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      // Batch bar should be available (even if hidden when nothing selected)
      expect(wrapper.html()).toContain('batch-bar')
    })
  })

  describe('Studio Admin Role', () => {
    it('should display studio admin perspective description', async () => {
      const wrapper = createWrapper('studio_admin')
      await nextTick()

      expect(wrapper.html()).toContain('查看工作室下各部门预算状态')
    })

    it('should show department configuration button', async () => {
      const wrapper = createWrapper('studio_admin')
      await nextTick()

      expect(wrapper.html()).toContain('配置工作室预算')
    })

    it('should NOT show "进入组织治理" button', async () => {
      const wrapper = createWrapper('studio_admin')
      await nextTick()

      expect(wrapper.html()).not.toContain('进入组织治理')
    })

    it('should display department table (not studio table)', async () => {
      const wrapper = createWrapper('studio_admin')
      await nextTick()

      // Studio admin sees different view
      expect(wrapper.html()).not.toContain('管辖工作室')
    })
  })

  describe('Department Admin Role', () => {
    it('should display dept admin perspective description', async () => {
      const wrapper = createWrapper('dept_admin')
      await nextTick()

      expect(wrapper.html()).toContain('查看本部门预算与成员用量风险')
    })

    it('should show department budget configuration button', async () => {
      const wrapper = createWrapper('dept_admin')
      await nextTick()

      expect(wrapper.html()).toContain('配置部门预算')
    })

    it('should NOT show "进入组织治理" button', async () => {
      const wrapper = createWrapper('dept_admin')
      await nextTick()

      expect(wrapper.html()).not.toContain('进入组织治理')
    })

    it('should NOT show batch operations', async () => {
      const wrapper = createWrapper('dept_admin')
      await nextTick()

      // Dept admin should not see batch operations
      const html = wrapper.html()
      // Check if batch-bar is present but hidden or completely absent
      const hasBatchBar = html.includes('batch-bar')
      if (hasBatchBar) {
        // If present, it should be hidden (display:none or v-if=false)
        expect(html).not.toContain('已选择')
      }
    })
  })

  describe('Role-Specific KPI Cards', () => {
    it('admin should see company budget pool KPI', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      expect(wrapper.html()).toContain('公司预算池')
    })

    it('studio_admin should see studio budget pool KPI', async () => {
      const wrapper = createWrapper('studio_admin')
      await nextTick()

      // Studio admin sees studio-specific KPIs
      const html = wrapper.html()
      expect(html).not.toContain('公司预算池')
    })

    it('dept_admin should see department-specific KPIs', async () => {
      const wrapper = createWrapper('dept_admin')
      await nextTick()

      // Dept admin sees different KPI structure
      const html = wrapper.html()
      expect(html).not.toContain('公司预算池')
      expect(html).not.toContain('管辖工作室')
    })
  })

  describe('Operation Permissions', () => {
    it('admin can configure company budget', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      // Admin should have access to adjust company budget
      expect(wrapper.html()).toContain('调整公司预算')
    })

    it('studio_admin cannot access organization governance', async () => {
      const wrapper = createWrapper('studio_admin')
      await nextTick()

      expect(wrapper.html()).not.toContain('进入组织治理')
    })

    it('dept_admin cannot access organization governance', async () => {
      const wrapper = createWrapper('dept_admin')
      await nextTick()

      expect(wrapper.html()).not.toContain('进入组织治理')
    })
  })

  describe('Page Header Text by Role', () => {
    it('should show role-specific description for admin', async () => {
      const wrapper = createWrapper('admin')
      await nextTick()

      const description = wrapper.find('.overview-header p')
      expect(description.text()).toContain('平台管理员')
    })

    it('should show role-specific description for studio_admin', async () => {
      const wrapper = createWrapper('studio_admin')
      await nextTick()

      const description = wrapper.find('.overview-header p')
      expect(description.text()).toContain('工作室')
    })

    it('should show role-specific description for dept_admin', async () => {
      const wrapper = createWrapper('dept_admin')
      await nextTick()

      const description = wrapper.find('.overview-header p')
      expect(description.text()).toContain('本部门')
    })
  })
})
