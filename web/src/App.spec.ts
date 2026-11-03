import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import ElementPlus from 'element-plus'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import App from './App.vue'

const mockGetPendingExpansions = vi.fn()
const mockPush = vi.fn()
const route = reactive({ path: '/access/unified-entry', query: {} as Record<string, string> })
const authStore = reactive({
  isLoggedIn: true,
  isAdmin: true,
  isManager: true,
  effectiveRole: 'admin',
  userName: '管理员',
  departmentName: '',
  deptStatus: null,
  handleLogout: vi.fn(),
})

vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: mockPush }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/settings', () => ({
  useSettingsStore: () => ({
    publicSettings: null,
    loadPublicSettings: vi.fn(),
  }),
}))

vi.mock('@/services/control-plane', () => ({
  getUnreadAnnouncements: vi.fn().mockResolvedValue([]),
  getPendingExpansions: (...args: unknown[]) => mockGetPendingExpansions(...args),
  getMyBudget: vi.fn().mockResolvedValue([]),
}))

vi.mock('@/components/common/NotificationBell.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/AnnouncementBanner.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/AnnouncementPopup.vue', () => ({ default: { template: '<div />' } }))

function setRole(role: 'admin' | 'studio_admin' | 'dept_admin' | 'user') {
  authStore.effectiveRole = role
  authStore.isAdmin = role === 'admin'
  authStore.isManager = role !== 'user'
}

function mountAppAs(role: 'admin' | 'studio_admin' | 'dept_admin' | 'user') {
  setRole(role)
  return mount(App, {
    global: {
      plugins: [ElementPlus],
      stubs: { RouterView: { template: '<div />' } },
    },
  })
}

describe('App navigation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    sessionStorage.clear()
    document.body.style.overflow = ''
    route.path = '/access/unified-entry'
    route.query = {}
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1280 })
    mockGetPendingExpansions.mockResolvedValue([])
  })

  afterEach(() => {
    document.body.style.overflow = ''
  })

  it('菜单按员工、管理、平台、运维分组', () => {
    const wrapper = mountAppAs('admin')
    expect(wrapper.findAll('.menu-group-label').map(node => node.text()))
      .toEqual(['员工', '管理', '平台', '运维'])
    wrapper.unmount()
  })

  it('管理组中的扩容申请显示真实待审批数', async () => {
    mockGetPendingExpansions.mockResolvedValue([{ id: '1' }, { id: '2' }])
    const wrapper = mountAppAs('studio_admin')
    await flushPromises()

    const labels = wrapper.findAll('.menu-group-label')
    const managementLabel = labels.find(node => node.text() === '管理')!.element
    const menuItem = wrapper.get('[data-test="menu-expansion-requests"]').element
    const badge = wrapper.get('[data-test="expansion-pending-badge"]')
    expect(managementLabel.compareDocumentPosition(menuItem) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(badge.text()).toContain('2')
    expect(mockGetPendingExpansions).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('普通用户看不到审批菜单，仍保留我的预算', () => {
    const wrapper = mountAppAs('user')
    expect(wrapper.text()).toContain('我的预算')
    expect(wrapper.find('[data-test="menu-expansion-requests"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('平台管理员同时显示我的预算与预算管理', () => {
    const wrapper = mountAppAs('admin')
    expect(wrapper.text()).toContain('我的预算')
    expect(wrapper.text()).toContain('预算管理')
    wrapper.unmount()
  })

  it('部门和工作室管理员仍保留我的预算入口', () => {
    const studioWrapper = mountAppAs('studio_admin')
    expect(studioWrapper.text()).toContain('我的预算')
    studioWrapper.unmount()

    const deptWrapper = mountAppAs('dept_admin')
    expect(deptWrapper.text()).toContain('我的预算')
    deptWrapper.unmount()
  })

  it('移动侧栏支持 Escape 关闭并在打开时锁定滚动', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    const wrapper = mountAppAs('admin')
    const trigger = wrapper.get('.hamburger-btn')

    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.body.style.overflow).toBe('hidden')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.body.style.overflow).toBe('')
    wrapper.unmount()
  })

  it('审计日志路由只声明一次', () => {
    const routerSource = readFileSync(
      resolve(process.cwd(), 'src/router/index.ts'),
      'utf8',
    )
    expect(routerSource.match(/path:\s*['"]\/admin\/audit-logs['"]/g)).toHaveLength(1)
  })

  it('平台管理员可直接访问我的预算路由', () => {
    const routerSource = readFileSync(
      resolve(process.cwd(), 'src/router/index.ts'),
      'utf8',
    )
    expect(routerSource).not.toContain("to.path === '/my/budget' && authStore.effectiveRole === 'admin'")
    expect(routerSource).not.toMatch(/\/my\/budget'.*\n.*next\('\/admin\/budget'\)/)
  })
})
