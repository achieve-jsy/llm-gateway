import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import KeyRequestApprovalPage from '../KeyRequestApprovalPage.vue'
import { useAuthStore } from '@/stores/auth'

const mockGetPendingKeyRequests = vi.fn()
const mockGetPendingKeyRequestCounts = vi.fn()
const mockGetKeyRequestHistory = vi.fn()
const mockGetDeptKeyCreationPolicy = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getPendingKeyRequests: (...args: unknown[]) => mockGetPendingKeyRequests(...args),
  getPendingKeyRequestCounts: (...args: unknown[]) => mockGetPendingKeyRequestCounts(...args),
  getKeyRequestHistory: (...args: unknown[]) => mockGetKeyRequestHistory(...args),
  getDeptKeyCreationPolicy: (...args: unknown[]) => mockGetDeptKeyCreationPolicy(...args),
  setDeptKeyCreationPolicy: vi.fn(),
  decideKeyRequest: vi.fn(),
}))

const pendingRequest = {
  id: 'request-1', key_name: 'analysis-key', requester_id: 'member-1', requester_name: '成员',
  budget_monthly: 0, status: 'pending', created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-01T00:00:00Z',
}

async function mountPage() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.effectiveRole = 'studio_admin'
  auth.user = { id: 'manager-1', name: '负责人', email: 'manager@example.com' } as never
  auth.orgRoles = [
    { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-a' },
    { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-b' },
    { role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-a' },
  ]
  const wrapper = mount(KeyRequestApprovalPage, {
    global: { plugins: [pinia, ElementPlus] },
  })
  await flushPromises()
  return wrapper
}

describe('KeyRequestApprovalPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetPendingKeyRequests.mockResolvedValue({ items: [pendingRequest], total: 1, page: 1, pageSize: 10 })
    mockGetPendingKeyRequestCounts.mockResolvedValue({ count: 1, budgetCount: 0 })
    mockGetKeyRequestHistory.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 10 })
    mockGetDeptKeyCreationPolicy.mockResolvedValue({ key_creation_policy: 'free' })
  })

  it('only exposes directly managed departments for a multi-role approver policy', async () => {
    const wrapper = await mountPage()

    expect(wrapper.text()).toContain('部门负责人：2 个部门；工作室负责人：1 个工作室')
    const policyDepartment = wrapper.get('[data-test="policy-department"]')
    expect(policyDepartment.text()).toContain('部门 dept-a')
    expect(policyDepartment.text()).toContain('部门 dept-b')
    expect(policyDepartment.text()).not.toContain('工作室 studio-a')
  })

  it('keeps loaded pending requests visible when refresh fails and offers a retry', async () => {
    const wrapper = await mountPage()
    mockGetPendingKeyRequests.mockRejectedValueOnce(new Error('network unavailable'))
    mockGetPendingKeyRequestCounts.mockRejectedValueOnce(new Error('network unavailable'))

    await (wrapper.vm as any).load()
    await flushPromises()

    expect(wrapper.text()).toContain('analysis-key')
    expect(wrapper.text()).toContain('待审批列表加载失败')
    expect(wrapper.get('[data-test="retry-pending"]')).toBeTruthy()
  })
})
