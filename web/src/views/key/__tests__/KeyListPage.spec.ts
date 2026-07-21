import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import ElementPlus from 'element-plus'
import KeyListPage from '../KeyListPage.vue'

const mockListKeys = vi.fn()
const mockGetKeyCreationPolicy = vi.fn()
const mockGetMyKeyRequests = vi.fn()
const mockGetMyKeyRequestSummary = vi.fn()
const mockGetDeptKeyCreationPolicy = vi.fn()
const mockRouterReplace = vi.fn()
const route = reactive({ path: '/keys', query: {} as Record<string, string> })

const authStore = reactive({
  effectiveRole: 'user' as string | null,
  isManager: false,
  isAdmin: false,
  hasMultipleApprovalRoles: false,
  departmentAdminRoles: [] as Array<{ role: string; scope_id: string }> ,
  studioAdminRoles: [] as Array<{ role: string; scope_id: string }> ,
  orgRoles: [] as Array<{ role: string; scope_id: string }> ,
  user: { id: 'member-1', name: '普通成员', email: 'member@example.com', departmentId: 'dept-xlab', departmentName: 'X-Lab' },
  fetchOrgRoles: vi.fn().mockResolvedValue(undefined),
})

vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ replace: mockRouterReplace }),
}))

vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/stores/key-cache', () => ({
  useKeyCacheStore: () => ({ cacheKey: vi.fn(), clearKey: vi.fn() }),
}))
vi.mock('@/stores/settings', () => ({
  useSettingsStore: () => ({
    loadPublicSettings: vi.fn(),
    getBaseUrl: () => 'https://aimeter.xk-devops.com',
  }),
}))
vi.mock('@/utils/clipboard', () => ({ copyToClipboard: vi.fn() }))

vi.mock('@/services/control-plane', () => ({
  listKeys: (...args: unknown[]) => mockListKeys(...args),
  getKeyCreationPolicy: (...args: unknown[]) => mockGetKeyCreationPolicy(...args),
  getMyKeyRequests: (...args: unknown[]) => mockGetMyKeyRequests(...args),
  getMyKeyRequestSummary: (...args: unknown[]) => mockGetMyKeyRequestSummary(...args),
  getDeptKeyCreationPolicy: (...args: unknown[]) => mockGetDeptKeyCreationPolicy(...args),
  disableKey: vi.fn(), enableKey: vi.fn(), deleteKey: vi.fn(), resetKey: vi.fn(), revealKey: vi.fn(),
  updateKeyGroups: vi.fn(), getAvailableGroups: vi.fn(), toggleAccessAllGroups: vi.fn(), toggleLogEnabled: vi.fn(),
  decideKeyRequest: vi.fn(), getPendingKeyRequests: vi.fn(), getPendingKeyRequestCounts: vi.fn(),
  getKeyRequestHistory: vi.fn(), setDeptKeyCreationPolicy: vi.fn(), cancelKeyRequest: vi.fn(),
}))

vi.mock('@/components/key/KeyCreateDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/key/KeyApplyDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/key/KeyAutoApproveDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/key/KeyRequestBudgetTag.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/key/KeyRequestApproverCell.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/key/UseKeyModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/key/OpenCodeConfigDialog.vue', () => ({ default: { template: '<div />' } }))

describe('KeyListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    route.path = '/keys'
    route.query = {}
    authStore.effectiveRole = 'user'
    authStore.isManager = false
    authStore.isAdmin = false
    authStore.departmentAdminRoles = []
    authStore.studioAdminRoles = []
    authStore.orgRoles = []
    mockListKeys.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })
    mockGetKeyCreationPolicy.mockResolvedValue({ requires_approval: true, policy: 'approval' })
    mockGetMyKeyRequests.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 10 })
    mockGetMyKeyRequestSummary.mockResolvedValue({ pending: 0, approved: 0, rejected: 0 })
    mockGetDeptKeyCreationPolicy.mockResolvedValue({ key_creation_policy: 'approval' })
  })

  it('普通成员不请求部门负责人专用的 Key 审批策略接口', async () => {
    mount(KeyListPage, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(mockGetKeyCreationPolicy).toHaveBeenCalledTimes(1)
    expect(mockGetDeptKeyCreationPolicy).not.toHaveBeenCalled()
  })

  it('直属部门负责人仍会读取自己的 Key 审批策略', async () => {
    authStore.effectiveRole = 'dept_admin'
    authStore.isManager = true
    authStore.departmentAdminRoles = [{ role: 'dept_admin', scope_id: 'dept-xlab' }]
    authStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-xlab' }]

    mount(KeyListPage, { global: { plugins: [ElementPlus] } })
    await flushPromises()

    expect(mockGetDeptKeyCreationPolicy).toHaveBeenCalledWith('dept-xlab')
  })
})
