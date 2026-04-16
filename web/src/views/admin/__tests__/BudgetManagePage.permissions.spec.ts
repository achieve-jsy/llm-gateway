import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import BudgetManagePage from '../BudgetManagePage.vue'

// Mock components
vi.mock('@/components/budget/BudgetRiskEventsSection.vue', () => ({
  default: { name: 'BudgetRiskEventsSection', template: '<div />' },
}))
vi.mock('@/components/budget/BudgetTierLimitsPanel.vue', () => ({
  default: { name: 'BudgetTierLimitsPanel', template: '<div />' },
}))
vi.mock('@/components/budget/BudgetEditDrawer.vue', () => ({
  default: { name: 'BudgetEditDrawer', template: '<div />' },
}))
vi.mock('@/components/budget/MemberQuotaDialog.vue', () => ({
  default: { name: 'MemberQuotaDialog', template: '<div />' },
}))

// Mock store
const mockAuthStore = {
  userName: 'Test User',
  user: { id: 'user-123', name: 'Test User' },
  effectiveRole: 'admin',
  orgRoles: [] as Array<{ role: string; scope_id: string; scope_type: string }>,
}

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => mockAuthStore,
}))

// Mock vue-router (only useRouter/useRoute, no createRouter)
const mockPush = vi.fn()
const mockReplace = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  useRoute: () => ({ query: {} }),
}))

// Mock API services
const mockGetBudgetList = vi.fn()
const mockGetBudgetContext = vi.fn()
const mockGetBudgetRiskSummary = vi.fn()
const mockListModelTiers = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getBudgetList: (...args: unknown[]) => mockGetBudgetList(...args),
  getBudgetContext: (...args: unknown[]) => mockGetBudgetContext(...args),
  getBudgetRiskSummary: (...args: unknown[]) => mockGetBudgetRiskSummary(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  rebuildBudgetCounters: vi.fn(),
  adjustBudget: vi.fn(),
  previewBudgetAdjustment: vi.fn(),
}))

vi.mock('@/services/http', () => ({
  default: {
    get: vi.fn().mockResolvedValue({ data: { items: [] } }),
  },
}))

function mountPage(role: string, orgRoles: Array<{ role: string; scope_id: string; scope_type: string }> = []) {
  const pinia = createPinia()
  setActivePinia(pinia)
  mockAuthStore.effectiveRole = role
  mockAuthStore.orgRoles = orgRoles
  return mount(BudgetManagePage, {
    global: {
      plugins: [pinia, ElementPlus],
      stubs: { 'el-icon': true },
    },
  })
}

describe('BudgetManagePage - Role-Based Access Control', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetBudgetList.mockResolvedValue([])
    mockGetBudgetContext.mockResolvedValue([])
    mockGetBudgetRiskSummary.mockResolvedValue({ total: 0 })
    mockListModelTiers.mockResolvedValue([])
    mockAuthStore.effectiveRole = 'admin'
    mockAuthStore.orgRoles = []
  })

  describe('Admin Role', () => {
    it('should display admin perspective description', async () => {
      const wrapper = mountPage('admin')
      await nextTick()
      expect(wrapper.html()).toContain('查看全公司预算风险')
    })

    it('should show company budget adjustment button', async () => {
      const wrapper = mountPage('admin')
      await nextTick()
      expect(wrapper.html()).toContain('调整公司预算')
    })

    it('should display company view with studio table', async () => {
      const wrapper = mountPage('admin')
      await nextTick()
      expect(wrapper.html()).toContain('工作室')
    })

    it('should display unified admin overview', async () => {
      const wrapper = mountPage('admin')
      await nextTick()
      expect(wrapper.find('[data-test="budget-manage-overview"]').exists()).toBe(true)
    })
  })

  describe('Studio Admin Role', () => {
    const studioOrgRoles = [{ role: 'studio_admin', scope_id: 'studio-1', scope_type: 'studio' }]

    it('should display studio admin perspective description', async () => {
      const wrapper = mountPage('studio_admin', studioOrgRoles)
      await nextTick()
      expect(wrapper.html()).toContain('查看工作室预算及下属部门分配情况')
    })

    it('studio_admin should not expose organization governance entry', async () => {
      const wrapper = mountPage('studio_admin', studioOrgRoles)
      await nextTick()
      expect(wrapper.html()).not.toContain('进入组织治理')
    })

    it('should NOT show "进入组织治理" button', async () => {
      const wrapper = mountPage('studio_admin', studioOrgRoles)
      await nextTick()
      expect(wrapper.html()).not.toContain('进入组织治理')
    })
  })

  describe('Department Admin Role', () => {
    const deptOrgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]

    it('should display dept admin perspective description', async () => {
      const wrapper = mountPage('dept_admin', deptOrgRoles)
      await nextTick()
      expect(wrapper.html()).toContain('查看部门预算及成员配额情况')
    })

    it('should show expansion request button instead of department budget configuration', async () => {
      const wrapper = mountPage('dept_admin', deptOrgRoles)
      await nextTick()
      expect(wrapper.html()).toContain('申请扩容')
      expect(wrapper.html()).not.toContain('配置部门预算')
    })

    it('should NOT show "进入组织治理" button', async () => {
      const wrapper = mountPage('dept_admin', deptOrgRoles)
      await nextTick()
      expect(wrapper.html()).not.toContain('进入组织治理')
    })
  })

  describe('Role-Specific KPI Cards', () => {
    it('admin should see company budget pool KPI', async () => {
      const wrapper = mountPage('admin')
      await nextTick()
      expect(wrapper.html()).toContain('公司预算池')
    })

    it('studio_admin should NOT see company budget pool KPI card', async () => {
      const wrapper = mountPage('studio_admin', [{ role: 'studio_admin', scope_id: 'studio-1', scope_type: 'studio' }])
      await nextTick()
      // Check the KPI strip only (el-dialog renders in DOM even when closed)
      const kpiStrip = wrapper.find('.kpi-strip')
      if (kpiStrip.exists()) {
        expect(kpiStrip.html()).not.toContain('公司预算池')
      }
    })

    it('dept_admin should NOT see company or studio pool KPI cards', async () => {
      const wrapper = mountPage('dept_admin', [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }])
      await nextTick()
      const kpiStrip = wrapper.find('.kpi-strip')
      if (kpiStrip.exists()) {
        expect(kpiStrip.html()).not.toContain('公司预算池')
        expect(kpiStrip.html()).not.toContain('工作室预算池')
      }
    })
  })

  describe('Operation Permissions', () => {
    it('admin can adjust company budget', async () => {
      const wrapper = mountPage('admin')
      await nextTick()
      expect(wrapper.html()).toContain('调整公司预算')
    })

    it('studio_admin cannot access organization governance', async () => {
      const wrapper = mountPage('studio_admin', [{ role: 'studio_admin', scope_id: 'studio-1', scope_type: 'studio' }])
      await nextTick()
      expect(wrapper.html()).not.toContain('进入组织治理')
    })

    it('dept_admin cannot access organization governance', async () => {
      const wrapper = mountPage('dept_admin', [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }])
      await nextTick()
      expect(wrapper.html()).not.toContain('进入组织治理')
    })
  })

  describe('Page Header Text by Role', () => {
    it('should show role-specific description for admin', async () => {
      const wrapper = mountPage('admin')
      await nextTick()
      const description = wrapper.find('.overview-header p')
      expect(description.exists()).toBe(true)
      expect(description.text()).toContain('全公司')
    })

    it('should show role-specific description for studio_admin', async () => {
      const wrapper = mountPage('studio_admin', [{ role: 'studio_admin', scope_id: 'studio-1', scope_type: 'studio' }])
      await nextTick()
      const description = wrapper.find('.overview-header p')
      expect(description.exists()).toBe(true)
      expect(description.text()).toContain('工作室')
    })

    it('should show role-specific description for dept_admin', async () => {
      const wrapper = mountPage('dept_admin', [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }])
      await nextTick()
      const description = wrapper.find('.overview-header p')
      expect(description.exists()).toBe(true)
      expect(description.text()).toContain('部门预算')
    })
  })
})
