import { expect, test, type Page, type Route } from '@playwright/test'
import { fixtures, installCommonApiMocks, loginAs } from './support/e2e-mock'

type DecisionPayload = {
  decisions?: Array<{ id: string; action: string; comment: string; precheck_version: string }>
  action?: string
  comment?: string
  precheck_version?: string
}

const pendingRequests = [
  {
    ...fixtures.expansions[0],
    id: 'exp-ai-premium',
    scope_type: 'dept_daily',
    scope_id: 'dept-ai',
    scope_name: 'AI 研发部',
    requester_id: 'dept-admin-user',
    requester_name: '部门负责人',
    current_level: 'studio',
    model_tier_id: 'tier-premium',
    model_tier_name: '高级模型',
    current_amount_snapshot: '500.000000',
    used_amount_snapshot: '430.000000',
    target_amount_usd: '800.000000',
    requested_amount: 800,
    reason: '项目冲刺需要临时提升高级模型额度',
    urgent_reason: '上线窗口只剩 2 天',
    precheck_version: 'pv-approval-1',
  },
]

const mineRequests = [
  {
    ...fixtures.expansions[0],
    id: 'exp-studio-history',
    scope_type: 'studio',
    scope_id: 'studio-platform',
    scope_name: '效率平台工作室',
    requester_id: 'studio-admin-user',
    requester_name: '工作室负责人',
    current_level: 'platform',
    dimension_type: 'total',
    model_tier_id: null,
    model_tier_name: null,
    current_amount_snapshot: '20000.000000',
    used_amount_snapshot: '12400.000000',
    target_amount_usd: '26000.000000',
    requested_amount: 26000,
    priority: 'normal',
    reason: '新增团队预算池',
    urgent_reason: '',
    precheck_version: 'pv-history-1',
  },
]

const affectedGroups = Array.from({ length: 10 }, (_, index) => ({
  id: `group-${index + 1}`,
  name: `影响模型组 ${index + 1}`,
}))

function expansionDetail() {
  return {
    request: pendingRequests[0],
    submitted_snapshot: {
      current_limit_usd: '500.000000',
      used_usd: '430.000000',
      target_amount_usd: '800.000000',
    },
    live_snapshot: {
      can_submit: true,
      can_notify_owner: false,
      current_limit_usd: '500.000000',
      current_used_usd: '450.000000',
      target_amount_usd: '800.000000',
      increase_usd: '300.000000',
      effective_after_usd: '800.000000',
      parent_constraints: [
        { source_level: 'studio', scope_type: 'studio', scope_id: 'studio-platform', limit_usd: '20000.000000', used_usd: '12400.000000', status: 'available' },
        { source_level: 'company', scope_type: 'company', scope_id: 'GLOBAL', limit_usd: '100000.000000', used_usd: '30000.000000', status: 'available' },
      ],
      affected_groups: affectedGroups,
      visible_scopes: [],
      precheck_version: 'pv-approval-1',
      data_as_of: '2026-07-15T10:30:00Z',
      stale: false,
    },
    parent_constraints: [
      { source_level: 'studio', scope_type: 'studio', scope_id: 'studio-platform', limit_usd: '20000.000000', used_usd: '12400.000000', status: 'available' },
      { source_level: 'company', scope_type: 'company', scope_id: 'GLOBAL', limit_usd: '100000.000000', used_usd: '30000.000000', status: 'available' },
    ],
    approval_pool: ['studio-admin-user', 'platform-admin-user', 'backup-admin-user'],
    affected_groups: affectedGroups,
    capabilities: {
      can_edit: false,
      can_decide: true,
      can_cancel: false,
      can_reprecheck: false,
    },
    steps: [
      { id: 'step-submit', level: 'studio', approver_id: 'dept-admin-user', action: 'submitted', comment: '项目冲刺需要临时提升高级模型额度', created_at: '2026-07-15T10:00:00Z' },
    ],
    data_as_of: '2026-07-15T10:30:00Z',
  }
}

function okJson(route: Route, body: unknown) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

function byTest(page: Page, testId: string) {
  return page.locator(`[data-test="${testId}"]`)
}

async function installExpansionMocks(page: Page) {
  const batchDecisions: DecisionPayload[] = []
  const detailDecisions: DecisionPayload[] = []

  await page.route('**/api/v1/model-tiers**', route => okJson(route, {
    data: [
      {
        id: 'tier-premium',
        name: '高级模型',
        description: '高能力模型额度',
        color: '#2563eb',
        sort_order: 1,
        enabled: true,
        archived_at: null,
        group_count: 10,
        budget_count: 3,
        history_count: 0,
        reference_count: 13,
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-15T00:00:00Z',
      },
    ],
  }))

  await page.route('**/api/v1/expansion-requests**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/pending')) return okJson(route, { items: pendingRequests })
    if (path.endsWith('/batch-decision')) {
      const payload = route.request().postDataJSON() as DecisionPayload
      batchDecisions.push(payload)
      return okJson(route, {
        results: (payload.decisions || []).map(decision => ({ id: decision.id, ok: true })),
      })
    }
    if (path.endsWith('/exp-ai-premium/decision')) {
      const payload = route.request().postDataJSON() as DecisionPayload
      detailDecisions.push(payload)
      return okJson(route, { data: expansionDetail() })
    }
    if (path.endsWith('/exp-ai-premium')) return okJson(route, { data: expansionDetail() })
    return okJson(route, { items: mineRequests })
  })

  return { batchDecisions, detailDecisions }
}

async function openExpansionPage(page: Page, role: 'studio_admin' | 'admin' = 'studio_admin') {
  await installCommonApiMocks(page, role)
  await page.unroute('**/api/v1/expansion-requests/pending**')
  await loginAs(page, role)
  const api = await installExpansionMocks(page)
  await page.goto('/admin/expansion-requests')
  await page.waitForLoadState('networkidle')
  await expect(page.getByRole('heading', { name: role === 'admin' ? '平台扩容审批' : '工作室扩容审批' })).toBeVisible()
  await expect(byTest(page, 'request-row-exp-ai-premium')).toBeVisible()
  return api
}

test.describe('扩容审批 E2E', () => {
  test('工作室管理员进入审批页可看到权限说明、待我审批和列表操作', async ({ page }) => {
    await openExpansionPage(page, 'studio_admin')

    await expect(byTest(page, 'approval-permission-studio_admin')).toContainText('工作室管理员')
    await expect(byTest(page, 'approval-permission-studio_admin')).toContainText('审批范围：工作室级')
    await expect(byTest(page, 'approval-tab-pending')).toContainText('待我审批')
    await expect(byTest(page, 'request-row-exp-ai-premium')).toContainText('AI 研发部')
    await expect(page.locator('.approval-table-wrap').getByText('$500.00 → $800.00')).toBeVisible()
    await expect(byTest(page, 'request-approve-exp-ai-premium')).toBeVisible()
    await expect(byTest(page, 'request-reject-exp-ai-premium')).toBeVisible()
    await expect(byTest(page, 'request-detail-exp-ai-premium')).toBeVisible()
  })

  test('平台管理员也可访问审批页并看到平台级审批权限', async ({ page }) => {
    await openExpansionPage(page, 'admin')

    await expect(byTest(page, 'approval-permission-admin')).toContainText('平台管理员')
    await expect(byTest(page, 'approval-permission-admin')).toContainText('审批范围：平台级')
    await expect(byTest(page, 'approval-tab-pending')).toContainText('待我审批')
    await expect(byTest(page, 'approval-tab-all')).toContainText('权限范围内')
  })

  test('单行批准需要二次确认并调用 batch decision', async ({ page }) => {
    const { batchDecisions } = await openExpansionPage(page)

    await byTest(page, 'request-approve-exp-ai-premium').click()
    await expect(page.getByText('确认批准扩容申请')).toBeVisible()
    expect(batchDecisions).toEqual([])

    await page.getByRole('button', { name: '确认批准' }).click()
    await expect.poll(() => batchDecisions.length).toBe(1)
    expect(batchDecisions[0]).toEqual({
      decisions: [
        {
          id: 'exp-ai-premium',
          action: 'approve',
          comment: '',
          precheck_version: 'pv-approval-1',
        },
      ],
    })
  })

  test('单行驳回必须填写理由并通过 batch decision 提交', async ({ page }) => {
    const { batchDecisions } = await openExpansionPage(page)

    await byTest(page, 'request-reject-exp-ai-premium').click()
    await expect(page.getByText('驳回扩容申请')).toBeVisible()
    await page.getByRole('button', { name: '确认驳回' }).click()
    expect(batchDecisions).toEqual([])
    await expect(page.getByText('驳回必须填写理由')).toBeVisible()

    await page.getByRole('textbox', { name: '请填写驳回理由' }).fill('上级预算需要先完成月度复核')
    await page.getByRole('button', { name: '确认驳回' }).click()

    await expect.poll(() => batchDecisions.length).toBe(1)
    expect(batchDecisions[0]).toEqual({
      decisions: [
        {
          id: 'exp-ai-premium',
          action: 'reject',
          comment: '上级预算需要先完成月度复核',
          precheck_version: 'pv-approval-1',
        },
      ],
    })
  })

  test('详情页展示额度变化、上级约束，并可查看全部影响范围', async ({ page }) => {
    await openExpansionPage(page)

    await byTest(page, 'request-detail-exp-ai-premium').click()
    const drawer = page.locator('.el-drawer').filter({ hasText: '扩容审批详情' })
    await expect(drawer).toBeVisible()
    await expect(drawer.getByRole('heading', { name: '额度变化' })).toBeVisible()
    await expect(drawer.getByText('提交时上限')).toBeVisible()
    await expect(drawer.getByText('$500.00').first()).toBeVisible()
    await expect(drawer.getByText('当前已用')).toBeVisible()
    await expect(drawer.getByText('$450.00')).toBeVisible()
    await expect(drawer.getByText('申请目标')).toBeVisible()
    await expect(drawer.getByText('$800.00').first()).toBeVisible()

    await expect(drawer.getByRole('heading', { name: '上级约束' })).toBeVisible()
    await expect(drawer.getByText('工作室', { exact: true })).toBeVisible()
    await expect(drawer.getByText('$12,400.00 / $20,000.00')).toBeVisible()
    await expect(drawer.getByText('公司', { exact: true })).toBeVisible()
    await expect(drawer.getByText('$30,000.00 / $100,000.00')).toBeVisible()

    await expect(drawer.getByRole('heading', { name: '影响范围' })).toBeVisible()
    await expect(drawer.getByText('10 个模型组')).toBeVisible()
    await expect(drawer.getByText('影响模型组 8')).toBeVisible()
    await expect(drawer.getByText('影响模型组 9')).toBeHidden()
    await drawer.getByRole('button', { name: '查看全部 10 个' }).click()
    await expect(drawer.getByText('影响模型组 9')).toBeVisible()
    await expect(drawer.getByRole('button', { name: '收起' })).toBeVisible()
  })
})
