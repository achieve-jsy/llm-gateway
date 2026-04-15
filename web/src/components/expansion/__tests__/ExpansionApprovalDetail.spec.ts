import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
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
    mockDecideExpansionV2.mockResolvedValue(makeExpansionDetail())
    mockReprecheckExpansion.mockResolvedValue(makeExpansionDetail())
  })

  describe('详情渲染', () => {
    it('按原型顺序展示比较、效果、预检和审批流水四块信息', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const text = wrapper.text()
      for (const label of ['提交时上限', '当前已用', '申请目标', '调整类型', '批准后效果', '申请信息与上级限制预检', '审批流水与审批意见']) {
        expect(text).toContain(label)
      }
    })

    it('渲染提交快照信息', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('提交快照')
      expect(html).toContain('100.000000')
      expect(html).toContain('80.000000')
    })

    it('渲染实时快照信息', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('实时额度')
      expect(html).toContain('85.000000')
    })

    it('live_snapshot 为 null 时显示"不可用"', async () => {
      const detail = makeExpansionDetail({ live_snapshot: null })
      const wrapper = mountDetail(detail)
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('不可用')
    })

    it('显示审批流程步骤 (timeline)', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('submitted')
      expect(html).toContain('dept')
    })

    it('显示审批池信息', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('manager-1')
      expect(html).toContain('manager-2')
    })

    it('显示影响范围', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('Group A')
    })
  })

  describe('数据变化警告', () => {
    it('precheck_required 状态时显示重新预检警告', async () => {
      const detail = makeExpansionDetail({
        request: makeExpansionRequest({ status: 'precheck_required' }),
      })
      const wrapper = mountDetail(detail)
      await flushPromises()

      expect(wrapper.html()).toContain('数据已变化')
    })

    it('stale 的 live_snapshot 显示警告', async () => {
      const detail = makeExpansionDetail()
      detail.live_snapshot!.stale = true
      const wrapper = mountDetail(detail)
      await flushPromises()

      expect(wrapper.html()).toContain('数据已变化')
    })

    it('precheck_version 不匹配时显示警告', async () => {
      const detail = makeExpansionDetail()
      detail.live_snapshot!.precheck_version = 'v2'
      detail.request.precheck_version = 'v1'
      const wrapper = mountDetail(detail)
      await flushPromises()

      expect(wrapper.html()).toContain('数据已变化')
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
      const approveBtn = buttons.find(b => b.text() === '批准')
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

      const approveBtn = wrapper.findAll('button').find(b => b.text() === '批准')
      expect(approveBtn).toBeDefined()
      await approveBtn!.trigger('click')
      await flushPromises()

      expect(mockDecideExpansionV2).toHaveBeenCalledWith('req-1', expect.objectContaining({
        action: 'approve',
        precheck_version: 'v1',
      }))
    })

    it('审批后触发 changed 事件', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const rejectBtn = wrapper.findAll('button').find(b => b.text() === '驳回')
      await rejectBtn!.trigger('click')
      await flushPromises()

      expect(wrapper.emitted('changed')).toBeDefined()
    })

    it('live_snapshot stale 时批准按钮被禁用', async () => {
      const detail = makeExpansionDetail()
      detail.live_snapshot!.stale = true
      const wrapper = mountDetail(detail)
      await flushPromises()

      const approveBtn = wrapper.findAll('button').find(b => b.text() === '批准')
      expect(approveBtn).toBeDefined()
      expect(approveBtn!.attributes('disabled')).toBeDefined()
    })
  })

  describe('重新预检', () => {
    it('can_reprecheck 为 true 时显示重新预检按钮', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const recheckBtn = wrapper.findAll('button').find(b => b.text() === '重新预检')
      expect(recheckBtn).toBeDefined()
    })

    it('点击"重新预检"调用 reprecheckExpansion API', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      const recheckBtn = wrapper.findAll('button').find(b => b.text() === '重新预检')
      await recheckBtn!.trigger('click')
      await flushPromises()

      expect(mockReprecheckExpansion).toHaveBeenCalledWith('req-1')
    })
  })

  describe('父级约束', () => {
    it('无父级约束时显示"无父级瓶颈"', async () => {
      const wrapper = mountDetail(makeExpansionDetail())
      await flushPromises()

      expect(wrapper.html()).toContain('无父级瓶颈')
    })

    it('有父级约束时显示约束标签', async () => {
      const detail = makeExpansionDetail({
        parent_constraints: [
          { source_level: 'studio', scope_type: 'studio', scope_id: 's1', limit_usd: '1000.000000', status: 'blocking' },
        ],
      })
      const wrapper = mountDetail(detail)
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('studio')
      expect(html).toContain('blocking')
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
