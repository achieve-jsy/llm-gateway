import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import ExpansionRequestPage from '../ExpansionRequestPage.vue'
import { useAuthStore, type UserRole } from '@/stores/auth'
import type { ExpansionRequest } from '@/types/expansion'

const mockGetPendingExpansions = vi.fn()
const mockGetMyExpansionRequests = vi.fn()
const mockBatchDecideExpansion = vi.fn()
const mockReprecheckExpansion = vi.fn()
const mockGetExpansionDetail = vi.fn()
const mockListModelTiers = vi.fn()
const mockPush = vi.fn()

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ push: mockPush }),
}))

vi.mock('@/services/control-plane', () => ({
  getPendingExpansions: (...args: unknown[]) => mockGetPendingExpansions(...args),
  getMyExpansionRequests: (...args: unknown[]) => mockGetMyExpansionRequests(...args),
  batchDecideExpansion: (...args: unknown[]) => mockBatchDecideExpansion(...args),
  reprecheckExpansion: (...args: unknown[]) => mockReprecheckExpansion(...args),
  getExpansionDetail: (...args: unknown[]) => mockGetExpansionDetail(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  createExpansionRequest: vi.fn(),
  decideExpansion: vi.fn(),
  getExpansionSteps: vi.fn().mockResolvedValue([]),
  getBudgetList: vi.fn().mockResolvedValue([]),
}))

vi.mock('@/components/expansion/ExpansionCreateForm.vue', () => ({
  default: { name: 'ExpansionCreateForm', template: '<div data-test="create-form" />', props: ['modelValue'] },
}))

vi.mock('@/components/expansion/ExpansionApprovalDetail.vue', () => ({
  default: { name: 'ExpansionApprovalDetail', template: '<div data-test="approval-detail" />', props: ['modelValue', 'detail'] },
}))

function makeRequest(overrides: Partial<ExpansionRequest> = {}): ExpansionRequest {
  return {
    id: 'req-1',
    scope_type: 'member',
    scope_id: 'member-1',
    scope_name: '林晓',
    requester_id: 'member-1',
    requester_name: '林晓',
    period_type: 'monthly',
    dimension_type: 'tier',
    model_tier_id: 'tier-1',
    model_tier_name: '高级',
    duration_type: 'current_period',
    priority: 'urgent',
    current_amount: 80,
    requested_amount: 120,
    current_amount_snapshot: '80',
    used_amount_snapshot: '77',
    target_amount_usd: '120',
    reason: '项目冲刺',
    urgent_reason: '本周上线',
    status: 'pending',
    current_level: 'dept',
    precheck_version: 'v1',
    prechecked_at: '2026-07-14T04:00:00Z',
    expires_at: '2026-07-21T04:00:00Z',
    applied_at: null,
    sync_status: 'pending',
    created_at: '2026-07-14T04:36:00Z',
    updated_at: '2026-07-14T04:36:00Z',
    ...overrides,
  }
}

async function mountPageAs(role: UserRole = 'admin') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.effectiveRole = role
  auth.user = { id: 'admin-1', name: '管理员', email: 'admin@example.com' } as never
  const wrapper = mount(ExpansionRequestPage, {
    global: { plugins: [pinia, ElementPlus] },
  })
  await flushPromises()
  return wrapper
}

describe('ExpansionRequestPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetPendingExpansions.mockResolvedValue([
      makeRequest(),
      makeRequest({
        id: 'req-2',
        scope_name: 'AI 研发部',
        requester_name: '周然',
        dimension_type: 'total',
        model_tier_id: null,
        model_tier_name: null,
        duration_type: 'permanent',
        priority: 'normal',
        precheck_version: 'v2',
      }),
    ])
    mockGetMyExpansionRequests.mockResolvedValue([
      makeRequest({ id: 'mine-1', scope_name: '程序服务池', priority: 'normal' }),
    ])
    mockBatchDecideExpansion.mockResolvedValue({
      results: [{ id: 'req-1', ok: true }, { id: 'req-2', ok: true }],
    })
    mockReprecheckExpansion.mockResolvedValue({})
    mockGetExpansionDetail.mockResolvedValue({})
    mockListModelTiers.mockResolvedValue([{ id: 'tier-1', name: '高级', color: '#8b5cf6' }])
  })

  it('管理角色看到待我审批、我发起的、全部申请', async () => {
    const wrapper = await mountPageAs('admin')
    expect(wrapper.findAll('[data-test^="approval-tab-"]').map(node => node.text()))
      .toEqual(expect.arrayContaining(['待我审批', '我发起的', '全部申请']))
  })

  it('桌面表格显示完整审批字段和多维筛选', async () => {
    const wrapper = await mountPageAs('studio_admin')
    for (const label of ['申请主体', '申请内容', '类型', '当前状态', '提交时间', '当前审批层级', '操作']) {
      expect(wrapper.text()).toContain(label)
    }
    for (const filter of ['dimension', 'tier', 'duration', 'priority', 'status', 'keyword']) {
      expect(wrapper.find(`[data-test="approval-filter-${filter}"]`).exists()).toBe(true)
    }
  })

  it('批量批准只提交已选择且可审批的行', async () => {
    const wrapper = await mountPageAs('admin')
    await wrapper.get('[data-test="request-select-req-1"]').setValue(true)
    await wrapper.get('[data-test="request-select-req-2"]').setValue(true)
    await wrapper.get('[data-test="batch-approve"]').trigger('click')
    await flushPromises()
    expect(mockBatchDecideExpansion).toHaveBeenCalledWith([
      expect.objectContaining({ id: 'req-1', action: 'approve', precheck_version: 'v1' }),
      expect.objectContaining({ id: 'req-2', action: 'approve', precheck_version: 'v2' }),
    ])
  })

  it('stale 行显示冻结视觉且主操作为重新预检', async () => {
    mockGetPendingExpansions.mockResolvedValue([
      makeRequest({ status: 'precheck_required' }),
    ])
    const wrapper = await mountPageAs('admin')
    expect(wrapper.get('[data-test="request-row-req-1"]').classes()).toContain('is-stale')
    expect(wrapper.text()).toContain('重新预检')
    expect(wrapper.get('[data-test="request-select-req-1"]').attributes('disabled')).toBeDefined()
  })

  it('待再次确认同样冻结并提供重新预检', async () => {
    mockGetPendingExpansions.mockResolvedValue([
      makeRequest({ status: 'pending_reconfirmation' }),
    ])
    const wrapper = await mountPageAs('admin')
    expect(wrapper.get('[data-test="request-row-req-1"]').classes()).toContain('is-stale')
    expect(wrapper.get('[data-test="request-action-req-1"]').text()).toContain('重新预检')
  })

  it('为每条申请渲染移动卡片并补齐等级、优先级和层级', async () => {
    const wrapper = await mountPageAs('admin')
    const cards = wrapper.findAll('[data-test="expansion-mobile-item"]')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('高级')
    expect(cards[0].text()).toContain('紧急')
    expect(cards[0].text()).toContain('部门级')
  })
})
