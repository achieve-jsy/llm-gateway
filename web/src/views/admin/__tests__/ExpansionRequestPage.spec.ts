import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import { ElMessageBox } from 'element-plus'
import ExpansionRequestPage from '../ExpansionRequestPage.vue'
import { useAuthStore, type UserRole } from '@/stores/auth'
import { STATUS_FILTER_OPTIONS, type ExpansionRequest } from '@/types/expansion'

const mockGetPendingExpansions = vi.fn()
const mockGetMyExpansionRequests = vi.fn()
const mockBatchDecideExpansion = vi.fn()
const mockReprecheckExpansion = vi.fn()
const mockGetExpansionDetail = vi.fn()
const mockListModelTiers = vi.fn()
const mockPush = vi.fn()
let currentRouteQuery: Record<string, any> = {}

vi.mock('vue-router', () => ({
  useRoute: () => ({ get query() { return currentRouteQuery } }),
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
  default: {
    name: 'ExpansionCreateForm',
    template: '<div data-test="create-form">{{ initialScopeType }} {{ initialScopeId }} {{ fixedManagerScope ? managerScopeLabel : "" }}</div>',
    props: ['modelValue', 'initialScopeType', 'initialScopeId', 'fixedManagerScope', 'managerScopeLabel', 'managerScopeOptions'],
  },
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
    duration_type: 'permanent',
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
    vi.useRealTimers()
    currentRouteQuery = {}
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
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
      .toEqual(expect.arrayContaining(['待我审批', '我发起的', '权限范围内']))
    expect(wrapper.get('[data-test="approval-permission-admin"]').text()).toContain('平台管理员')
    expect(wrapper.text()).toContain('审批范围：平台级')
  })

  it('普通用户只能看到本人申请范围并可以新建申请', async () => {
    const wrapper = await mountPageAs('user')
    expect(wrapper.find('[data-test="approval-tab-pending"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="approval-tab-mine"]').text()).toContain('我发起的')
    expect(wrapper.get('[data-test="approval-permission-user"]').text()).toContain('普通用户')
    expect(wrapper.text()).toContain('发起范围：本人')
    expect(wrapper.findComponent({ name: 'ExpansionCreateForm' }).exists()).toBe(true)
    expect(wrapper.text()).toContain('新建申请')
  })

  it('待我审批表格显示审批决策字段和待办筛选', async () => {
    const wrapper = await mountPageAs('studio_admin')
    for (const label of ['申请主体', '申请人', '限制项', '额度变化', '风险提示', '当前状态', '提交时间', '操作']) {
      expect(wrapper.text()).toContain(label)
    }
    for (const filter of ['dimension', 'tier', 'duration', 'priority', 'keyword']) {
      expect(wrapper.find(`[data-test="approval-filter-${filter}"]`).exists()).toBe(true)
    }
    expect(wrapper.find('[data-test="approval-filter-status"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="approval-permission-studio_admin"]').text()).toContain('工作室管理员')
    expect(wrapper.text()).toContain('审批范围：工作室级')
  })

  it('我发起的页签展示生命周期状态筛选', async () => {
    const wrapper = await mountPageAs('dept_admin')
    await wrapper.get('[data-test="approval-tab-mine"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="approval-filter-status"]').exists()).toBe(true)
  })

  it('列表不直接展示 UUID 主体且申请人有兜底展示', async () => {
    mockGetPendingExpansions.mockResolvedValue([
      makeRequest({
        scope_type: 'studio',
        scope_id: '200f2786-02e0-4397-a336-3a843bc5f1d3',
        scope_name: '200f2786-02e0-4397-a336-3a843bc5f1d3',
        requester_id: '11111111-1111-4111-8111-111111111111',
        requester_name: '',
      }),
    ])
    const wrapper = await mountPageAs('studio_admin')

    expect(wrapper.text()).toContain('工作室预算')
    expect(wrapper.text()).toContain('用户 11111111')
    expect(wrapper.text()).not.toContain('200f2786-02e0-4397-a336-3a843bc5f1d3')
    expect(wrapper.text()).not.toContain('—')
  })

  it('状态筛选只配置一个审批中选项', () => {
    expect(STATUS_FILTER_OPTIONS.map(option => option.label).filter(label => label === '审批中')).toHaveLength(1)
  })

  it('筛选项变化会带参数重新请求列表接口', async () => {
    vi.useFakeTimers()
    const wrapper = await mountPageAs('admin')
    mockGetPendingExpansions.mockClear()
    mockGetMyExpansionRequests.mockClear()

    ;(wrapper.vm as any).filter.priority = 'normal'
    await vi.advanceTimersByTimeAsync(260)
    await flushPromises()

    expect(mockGetPendingExpansions).toHaveBeenCalledWith(expect.objectContaining({ priority: 'normal' }))
    expect(mockGetMyExpansionRequests).toHaveBeenCalledWith(expect.objectContaining({ priority: 'normal' }))
    vi.useRealTimers()
  })

  it('待我审批页签请求不携带生命周期状态筛选', async () => {
    vi.useFakeTimers()
    const wrapper = await mountPageAs('admin')
    mockGetPendingExpansions.mockClear()

    ;(wrapper.vm as any).filter.status = 'approved'
    await vi.advanceTimersByTimeAsync(260)
    await flushPromises()

    expect(mockGetPendingExpansions).toHaveBeenCalledWith(expect.not.objectContaining({ status: 'approved' }))
    vi.useRealTimers()
  })

  it('部门负责人视角使用申请成员列且隐藏申请人列', async () => {
    const wrapper = await mountPageAs('dept_admin')

    expect(wrapper.text()).toContain('申请成员')
    expect(wrapper.text()).not.toContain('申请主体')
    expect(wrapper.text()).not.toContain('申请人')
    expect(wrapper.get('[data-test="approval-permission-dept_admin"]').text()).toContain('部门管理员')
  })

  it('部门负责人新建申请默认指向本部门预算池', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.effectiveRole = 'dept_admin'
    auth.user = { id: 'dept-admin-1', name: '部门负责人', email: 'dept@example.com', departmentId: 'dept-ai', departmentName: 'AI 研发部' } as never
    auth.orgRoles = [{ role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-ai' }]

    const wrapper = mount(ExpansionRequestPage, { global: { plugins: [pinia, ElementPlus] } })
    await flushPromises()

    expect(wrapper.get('[data-test="create-form"]').text()).toContain('dept_daily dept-ai AI 研发部')
  })

  it('工作室负责人从工作室扩容入口进入时固定申请主体', async () => {
    currentRouteQuery = {
      create: '1',
      scope_type: 'studio',
      scope_id: 'studio-1',
      scope_name: '互娱事业部',
    }
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.effectiveRole = 'studio_admin'
    auth.user = { id: 'studio-admin-1', name: '工作室负责人', email: 'studio@example.com' } as never
    auth.orgRoles = [{ role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-1' }]

    const wrapper = mount(ExpansionRequestPage, { global: { plugins: [pinia, ElementPlus] } })
    await flushPromises()

    expect(wrapper.get('[data-test="create-form"]').text()).toContain('studio studio-1 互娱事业部')
  })

  it('部门与工作室负责人同时存在时展示合并审批范围，并只请求一次后端待办', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.effectiveRole = 'studio_admin'
    auth.user = { id: 'dual-role-1', name: '多角色负责人', email: 'dual@example.com' } as never
    auth.orgRoles = [
      { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-a' },
      { role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-a' },
    ]

    const wrapper = mount(ExpansionRequestPage, { global: { plugins: [pinia, ElementPlus] } })
    await flushPromises()

    expect(wrapper.text()).toContain('多角色负责人')
    expect(wrapper.text()).toContain('合并处理部门与工作室待办')
    expect(mockGetPendingExpansions).toHaveBeenCalledTimes(1)
  })

  it('多个直接管辖部门不默认选择第一个，并传递全部可选申请对象', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.effectiveRole = 'dept_admin'
    auth.user = { id: 'multi-dept-1', name: '多部门负责人', email: 'dept@example.com' } as never
    auth.orgRoles = [
      { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-a' },
      { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-b' },
    ]

    const wrapper = mount(ExpansionRequestPage, { global: { plugins: [pinia, ElementPlus] } })
    await flushPromises()
    const form = wrapper.findComponent({ name: 'ExpansionCreateForm' })

    expect(form.props('initialScopeId')).toBe('')
    expect(form.props('managerScopeOptions')).toEqual([
      { scopeType: 'dept_daily', scopeId: 'dept-a', label: '部门 dept-a' },
      { scopeType: 'dept_daily', scopeId: 'dept-b', label: '部门 dept-b' },
    ])
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
    expect(ElMessageBox.confirm).toHaveBeenCalledWith(
      expect.stringContaining('确认批量批准 2 项'),
      '确认批量批准',
      expect.objectContaining({ confirmButtonText: '确认批准' }),
    )
  })

  it('操作列支持单行批准', async () => {
    const wrapper = await mountPageAs('admin')
    await wrapper.get('[data-test="request-approve-req-1"]').trigger('click')
    await flushPromises()
    expect(mockBatchDecideExpansion).toHaveBeenCalledWith([
      expect.objectContaining({ id: 'req-1', action: 'approve', precheck_version: 'v1' }),
    ])
    expect(ElMessageBox.confirm).toHaveBeenCalledWith(
      expect.stringContaining('确认批准'),
      '确认批准扩容申请',
      expect.objectContaining({ confirmButtonText: '确认批准' }),
    )
  })

  it('取消单行批准确认时不提交审批', async () => {
    vi.mocked(ElMessageBox.confirm).mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountPageAs('admin')
    await wrapper.get('[data-test="request-approve-req-1"]').trigger('click')
    await flushPromises()
    expect(mockBatchDecideExpansion).not.toHaveBeenCalled()
  })

  it('操作列支持填写理由后单行驳回', async () => {
    vi.spyOn(ElMessageBox, 'prompt').mockResolvedValueOnce({ value: '额度不合理', action: 'confirm' } as never)
    const wrapper = await mountPageAs('admin')
    await wrapper.get('[data-test="request-reject-req-1"]').trigger('click')
    await flushPromises()
    expect(mockBatchDecideExpansion).toHaveBeenCalledWith([
      expect.objectContaining({ id: 'req-1', action: 'reject', comment: '额度不合理', precheck_version: 'v1' }),
    ])
  })

  it('我发起的审批中申请支持取消', async () => {
    const wrapper = await mountPageAs('dept_admin')
    await wrapper.get('[data-test="approval-tab-mine"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="request-cancel-mine-1"]').trigger('click')
    await flushPromises()

    expect(ElMessageBox.confirm).toHaveBeenCalledWith(
      expect.stringContaining('确认取消'),
      '取消扩容申请',
      expect.objectContaining({ confirmButtonText: '确认取消' }),
    )
    expect(mockBatchDecideExpansion).toHaveBeenCalledWith([
      expect.objectContaining({ id: 'mine-1', action: 'cancel' }),
    ])
  })

  it('stale 行显示冻结视觉且主操作为刷新数据', async () => {
    mockGetPendingExpansions.mockResolvedValue([
      makeRequest({ status: 'precheck_required' }),
    ])
    const wrapper = await mountPageAs('admin')
    expect(wrapper.get('[data-test="request-row-req-1"]').classes()).toContain('is-stale')
    expect(wrapper.text()).toContain('刷新数据')
    expect(wrapper.get('[data-test="request-select-req-1"]').attributes('disabled')).toBeDefined()
  })

  it('待处理状态同样提供刷新数据', async () => {
    mockGetPendingExpansions.mockResolvedValue([
      makeRequest({ status: 'pending_reconfirmation' }),
    ])
    const wrapper = await mountPageAs('admin')
    expect(wrapper.get('[data-test="request-row-req-1"]').classes()).toContain('is-stale')
    expect(wrapper.get('[data-test="request-action-req-1"]').text()).toContain('刷新数据')
    expect(wrapper.find('[data-test="request-approve-req-1"]').exists()).toBe(false)
  })

  it('目标低于当前已用时在列表暴露风险提示', async () => {
    mockGetPendingExpansions.mockResolvedValue([
      makeRequest({ used_amount_snapshot: '130', target_amount_usd: '120', priority: 'normal' }),
    ])
    const wrapper = await mountPageAs('admin')
    expect(wrapper.text()).toContain('目标低于已用')
    expect(wrapper.text()).toContain('需关注申请')
    expect(wrapper.get('[data-test="request-approve-req-1"]').attributes('disabled')).toBeDefined()
  })

  it('为每条申请渲染移动卡片并补齐等级、优先级和层级', async () => {
    mockGetPendingExpansions.mockResolvedValue([makeRequest({ duration_type: 'current_period' })])
    const wrapper = await mountPageAs('admin')
    const cards = wrapper.findAll('[data-test="expansion-mobile-item"]')
    expect(cards).toHaveLength(1)
    expect(cards[0].text()).toContain('高级额度')
    expect(cards[0].text()).toContain('紧急')
    expect(cards[0].text()).toContain('部门级')
    expect(cards[0].text()).toContain('增加 $40.00')
    expect(cards[0].text()).toContain('临时调整')
  })
})
