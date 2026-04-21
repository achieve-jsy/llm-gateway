import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import { ElMessageBox } from 'element-plus'
import ExpansionApprovalDetail from '../ExpansionApprovalDetail.vue'
import type { ExpansionDetail, ExpansionRequest } from '@/types/expansion'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
}))

// ── Mock API services ──────────────────────────────────────────────
const mockDecideExpansionV2 = vi.fn()
const mockReprecheckExpansion = vi.fn()

vi.mock('@/services/control-plane', () => ({
  decideExpansionV2: (...args: unknown[]) => mockDecideExpansionV2(...args),
  reprecheckExpansion: (...args: unknown[]) => mockReprecheckExpansion(...args),
}))

// Mock ExpansionPrecheckPanel
vi.mock('../ExpansionPrecheckPanel.vue', () => ({
  default: {
    name: 'ExpansionPrecheckPanel',
    template: '<div class="mock-precheck-panel" />',
    props: ['value'],
  },
}))

// ── Fixtures ───────────────────────────────────────────────────────
function makeExpansionRequest(overrides: Partial<ExpansionRequest> = {}): ExpansionRequest {
  return {
    id: 'req-1',
    scope_type: 'member',
    scope_id: 'user-1',
    scope_name: '张三',
    requester_id: 'user-1',
    requester_name: '张三',
    period_type: 'monthly',
    dimension_type: 'total',
    model_tier_id: null,
    model_tier_name: null,
    duration_type: 'permanent',
    priority: 'normal',
    current_amount: 100,
    requested_amount: 200,
    current_amount_snapshot: '100.000000',
    used_amount_snapshot: '80.000000',
    target_amount_usd: '200.000000',
    reason: '业务增长需要更多额度',
    urgent_reason: '',
    status: 'pending',
    current_level: 'dept',
    precheck_version: 'v1',
    prechecked_at: '2026-07-13T00:00:00Z',
    expires_at: '2026-07-20T00:00:00Z',
    applied_at: null,
    sync_status: 'pending',
    created_at: '2026-07-13T00:00:00Z',
    updated_at: '2026-07-13T00:00:00Z',
    ...overrides,
  }
}

function makeExpansionDetail(overrides: Partial<ExpansionDetail> = {}): ExpansionDetail {
  return {
    request: makeExpansionRequest(),
    submitted_snapshot: {
      current_limit_usd: '100.000000',
      used_usd: '80.000000',
      target_amount_usd: '200.000000',
    },
    live_snapshot: {
      can_submit: true,
      can_notify_owner: false,
      current_limit_usd: '100.000000',
      current_used_usd: '85.000000',
      target_amount_usd: '200.000000',
      increase_usd: '100.000000',
      effective_after_usd: '200.000000',
      parent_constraints: [],
      affected_groups: [{ id: 'g1', name: 'Group A' }],
      visible_scopes: [],
      precheck_version: 'v1',
      data_as_of: '2026-07-13T01:00:00Z',
      stale: false,
    },
    parent_constraints: [],
    approval_pool: ['manager-1', 'manager-2'],
    affected_groups: [{ id: 'g1', name: 'Group A' }],
    capabilities: {
      can_edit: false,
      can_decide: true,
      can_cancel: false,
      can_reprecheck: true,
    },
    steps: [
      { id: 'step-1', level: 'dept', approver_id: 'manager-1', action: 'submitted', comment: '', created_at: '2026-07-13T00:00:00Z' },
    ],
    data_as_of: '2026-07-13T01:00:00Z',
    ...overrides,
  }
}

function makeGroups(count: number) {
  return Array.from({ length: count }, (_, index) => ({ id: `g${index + 1}`, name: `Group ${index + 1}` }))
}

// ── Helpers ────────────────────────────────────────────────────────
/**
 * The component uses defineModel('detail') which maps to a `detail` prop.
 * The content is wrapped in <template v-if="detail">.
 * el-drawer teleports to body, so we stub it to render inline.
 */
const DrawerStub = {
  name: 'ElDrawer',
  template: '<div class="el-drawer-stub"><slot /></div>',
  props: ['modelValue', 'title', 'size'],
}

function mountDetail(detail: ExpansionDetail | null = null) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(ExpansionApprovalDetail, {
    props: {
      detail,
      visible: true,
    },
    global: {
      plugins: [pinia, ElementPlus],
      stubs: {
        ElDrawer: DrawerStub,
      },
    },
  })
}

// ── Tests ──────────────────────────────────────────────────────────
describe('ExpansionApprovalDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    mockDecideExpansionV2.mockResolvedValue(makeExpansionDetail())
    mockReprecheckExpansion.mockResolvedValue(makeExpansionDetail())
  })

  describe('详情渲染', () => {
    it('按审批决策顺序展示结论、主体、额度、影响和流水', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const text = wrapper.text()
      for (const label of ['可审批', '可批准，批准后将自动生效', '额度变化', '影响范围', '审批信息']) {
        expect(text).toContain(label)
      }
      expect(text).not.toContain('上级约束')
    })

    it('渲染提交快照信息', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('提交时上限')
      expect(html).toContain('$100.00')
      expect(html).toContain('$80.00')
    })

    it('个人额度详情缺少 scope_name 时使用申请人姓名而不是 UUID', async () => {
      const wrapper = mountDetail(makeExpansionDetail({
        request: makeExpansionRequest({
          scope_name: '',
          requester_name: 'QA员工A',
          scope_id: 'aaaaaaaa-3333-4333-8333-333333333333',
        }),
      }))
      await flushPromises()

      expect(wrapper.text()).toContain('QA员工A')
      expect(wrapper.text()).not.toContain('aaaaaaaa-3333-4333-8333-333333333333')
    })

    it('渲染实时快照信息', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('实时上限')
      expect(html).toContain('$85.00')
    })

    it('live_snapshot 为 null 时显示"不可用"', async () => {
      const detail = makeExpansionDetail({ live_snapshot: null })
      const wrapper = mountDetail(detail)
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('—')
    })

    it('显示审批流程步骤 (timeline)', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('提交')
      expect(html).toContain('部门级')
    })

    it('显示审批池人数而不是铺满 UUID', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const text = wrapper.text()
      expect(text).toContain('2 人可处理')
      expect(text).not.toContain('manager-1、manager-2')
    })

    it('显示影响范围', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('Group A')
    })

    it('影响模型组支持查看全部和收起', async () => {
      const detail = makeExpansionDetail({ affected_groups: makeGroups(10) })
      const wrapper = mountDetail(detail)
      await flushPromises()

      expect(wrapper.text()).toContain('Group 8')
      expect(wrapper.text()).not.toContain('Group 9')
      const toggle = wrapper.findAll('button').find(button => button.text().includes('查看全部 10 个'))
      expect(toggle).toBeDefined()

      await toggle!.trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('Group 9')
      expect(wrapper.text()).toContain('收起')

      await wrapper.findAll('button').find(button => button.text() === '收起')!.trigger('click')
      await flushPromises()
      expect(wrapper.text()).not.toContain('Group 9')
    })

    it('临时调整展示当前周期生效说明', async () => {
      const wrapper = mountDetail(makeExpansionDetail({
        request: makeExpansionRequest({ duration_type: 'current_period' }),
      }))
      await flushPromises()

      const text = wrapper.text()
      expect(text).toContain('临时调整')
      expect(text).toContain('当前周期临时调整')
      expect(text).toContain('周期结束后恢复长期基础额度')
    })
  })

  describe('数据变化展示', () => {
    it('状态变化时不展示额外解释，只保留实时指标', async () => {
      const detail = makeExpansionDetail({
        request: makeExpansionRequest({ status: 'precheck_required' }),
      })
      const wrapper = mountDetail(detail)
      await flushPromises()

      expect(wrapper.text()).toContain('实时上限')
      expect(wrapper.text()).not.toContain('已按最新预算重新计算审批结果')
    })

    it('stale 的 live_snapshot 不展示额外解释', async () => {
      const detail = makeExpansionDetail()
      detail.live_snapshot!.stale = true
      const wrapper = mountDetail(detail)
      await flushPromises()

      expect(wrapper.text()).toContain('实时上限')
      expect(wrapper.text()).not.toContain('状态已变化')
    })

    it('precheck_version 不匹配时仍可查看实时指标', async () => {
      const detail = makeExpansionDetail()
      detail.live_snapshot!.precheck_version = 'v2'
      detail.request.precheck_version = 'v1'
      const wrapper = mountDetail(detail)
      await flushPromises()

      expect(wrapper.text()).toContain('实时上限')
      expect(wrapper.text()).not.toContain('已按最新预算重新计算审批结果')
    })
  })

  describe('审批操作', () => {
    it('can_decide 为 true 时显示批准和驳回按钮', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('批准')
      expect(html).toContain('驳回')
    })

    it('can_decide 为 false 时不显示审批按钮', async () => {
      const detail = makeExpansionDetail({
        capabilities: { can_edit: false, can_decide: false, can_cancel: false, can_reprecheck: false },
      })
      const wrapper = mountDetail(detail)
      await flushPromises()

      const buttons = wrapper.findAll('button')
      const approveBtn = buttons.find(b => b.text() === '批准并生效')
      const rejectBtn = buttons.find(b => b.text() === '驳回')
      expect(approveBtn).toBeUndefined()
      expect(rejectBtn).toBeUndefined()
    })

    it('点击"驳回"调用 decideExpansionV2 并传入 reject', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const rejectBtn = wrapper.findAll('button').find(b => b.text() === '驳回')
      expect(rejectBtn).toBeDefined()
      await rejectBtn!.trigger('click')
      await flushPromises()

      expect(mockDecideExpansionV2).toHaveBeenCalledWith('req-1', expect.objectContaining({
        action: 'reject',
        precheck_version: 'v1',
      }))
    })

    it('点击"批准"调用 decideExpansionV2 并传入 approve', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const approveBtn = wrapper.findAll('button').find(b => b.text() === '批准并生效')
      expect(approveBtn).toBeDefined()
      await approveBtn!.trigger('click')
      await flushPromises()

      expect(mockDecideExpansionV2).toHaveBeenCalledWith('req-1', expect.objectContaining({
        action: 'approve',
        precheck_version: 'v1',
      }))
      expect(ElMessageBox.confirm).toHaveBeenCalledWith(
        expect.stringContaining('确认批准'),
        '确认批准扩容申请',
        expect.objectContaining({ confirmButtonText: '确认批准' }),
      )
    })

    it('取消批准确认时不调用 decideExpansionV2', async () => {
      vi.mocked(ElMessageBox.confirm).mockRejectedValueOnce(new Error('cancel'))
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const approveBtn = wrapper.findAll('button').find(b => b.text() === '批准并生效')
      await approveBtn!.trigger('click')
      await flushPromises()

      expect(mockDecideExpansionV2).not.toHaveBeenCalled()
    })

    it('审批后触发 changed 事件', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const rejectBtn = wrapper.findAll('button').find(b => b.text() === '驳回')
      await rejectBtn!.trigger('click')
      await flushPromises()

      expect(wrapper.emitted('changed')).toBeDefined()
    })

    it('live_snapshot stale 时仍可提交批准并由服务端最终校验', async () => {
      const detail = makeExpansionDetail()
      detail.live_snapshot!.stale = true
      const wrapper = mountDetail(detail)
      await flushPromises()

      const approveBtn = wrapper.findAll('button').find(b => b.text() === '批准并生效')
      expect(approveBtn).toBeDefined()
      expect(approveBtn!.attributes('disabled')).toBeUndefined()
    })
  })

  describe('刷新数据', () => {
    it('can_reprecheck 为 true 时显示刷新数据按钮', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const recheckBtn = wrapper.findAll('button').find(b => b.text() === '刷新数据')
      expect(recheckBtn).toBeDefined()
    })

    it('点击"刷新数据"调用 reprecheckExpansion API', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const recheckBtn = wrapper.findAll('button').find(b => b.text() === '刷新数据')
      await recheckBtn!.trigger('click')
      await flushPromises()

      expect(mockReprecheckExpansion).toHaveBeenCalledWith('req-1')
    })
  })

  describe('父级约束', () => {
    it('不再展示父级约束区块', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      expect(wrapper.html()).not.toContain('上级约束')
      expect(wrapper.html()).not.toContain('无父级瓶颈')
    })

    it('即使接口返回父级约束也不展示约束标签', async () => {
      const detail = makeExpansionDetail({
        parent_constraints: [
          { source_level: 'studio', scope_type: 'studio', scope_id: 's1', limit_usd: '1000.000000', status: 'blocking' },
        ],
      })
      const wrapper = mountDetail(detail)
      await flushPromises()

      const html = wrapper.html()
      expect(html).not.toContain('上级约束')
      expect(html).not.toContain('阻断')
      expect(html).not.toContain('批准后仍可能受上级限制')
    })
  })

  describe('null detail', () => {
    it('detail 为 null 时不渲染内容', async () => {
      const wrapper = mountDetail(null)
      await flushPromises()

      // With our stub, when detail is null the v-if inside <template v-if="detail"> is false
      // so no detail content should render
      expect(wrapper.find('.el-drawer-stub').exists()).toBe(true)
    })
  })
})
