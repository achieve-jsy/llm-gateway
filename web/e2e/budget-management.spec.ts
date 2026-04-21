import { expect, test, type Page, type Route } from '@playwright/test'
import { fixtures, installCommonApiMocks, loginAs } from './support/e2e-mock'

async function fulfillJSON(route: Route, body: unknown) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

const modelTiers = [
  {
    id: 'tier-premium',
    name: '高级',
    description: '高能力模型',
    color: '#8b5cf6',
    sort_order: 1,
    enabled: true,
    archived_at: null,
    group_count: 4,
    budget_count: 1,
    history_count: 0,
    reference_count: 1,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-15T10:00:00Z',
  },
]

const riskSummary = {
  total: 2,
  warning: 1,
  critical: 1,
  by_type: { request_blocked: 1, threshold_reached: 1 },
  data_as_of: '2026-07-15T10:00:00Z',
  stale: false,
}

const riskEvents = [
  {
    id: 'risk-ai-tier-premium',
    risk_type: 'request_blocked',
    severity: 'critical',
    scope_type: 'dept_daily',
    scope_id: 'dept-ai',
    scope_name: 'AI 研发部',
    period_type: 'monthly',
    dimension_type: 'tier',
    model_tier_id: 'tier-premium',
    model_tier_name: '高级',
    effective_limit_usd: '500.000000',
    used_usd: '430.000000',
    usage_ratio: '0.86',
    limit_source: '部门等级预算',
    blocked_requests: 3,
    detected_at: '2026-07-15T09:45:00Z',
    data_as_of: '2026-07-15T10:00:00Z',
    stale: false,
  },
]

const tierUsage = [
  {
    allocation_id: 'alloc-dept',
    scope_type: 'dept_daily',
    scope_id: 'dept-ai',
    tier_name: '高级',
    model_tier_id: 'tier-premium',
    period_type: 'monthly',
    used_usd: '430.000000',
    used_ratio: 0.86,
    enabled: true,
    data_as_of: '2026-07-15T10:00:00Z',
    stale: false,
  },
]

const budgetContext = [
  { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算' },
  { scope_type: 'studio', scope_id: 'studio-platform', label: '效率平台' },
  { scope_type: 'dept_daily', scope_id: 'dept-ai', label: 'AI 研发部' },
]

async function installBudgetApiMocks(page: Page) {
  await page.route('**/api/v1/budget/risk-events**', route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname.endsWith('/summary')) return fulfillJSON(route, { data: riskSummary })
    return fulfillJSON(route, {
      data: {
        items: riskEvents,
        total: riskEvents.length,
        page: 1,
        page_size: 50,
        data_as_of: '2026-07-15T10:00:00Z',
        stale: false,
      },
    })
  })
  await page.route('**/api/v1/budgets**', route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname.endsWith('/context')) return fulfillJSON(route, { context: budgetContext })
    if (pathname.endsWith('/tier-usage')) return fulfillJSON(route, { items: tierUsage })
    if (/\/api\/v1\/budgets\/[^/]+\/tier-limits$/.test(pathname)) {
      return fulfillJSON(route, { data: { tier_limits: fixtures.tierLimits } })
    }
    return fulfillJSON(route, { items: fixtures.budgetItems })
  })
  await page.route('**/api/v1/expansion-requests**', route => fulfillJSON(route, { items: fixtures.expansions }))
  await page.route('**/api/v1/model-tiers**', route => fulfillJSON(route, { data: modelTiers }))
}

async function bootBudget(page: Page, path: string, role: 'admin' | 'user' = 'admin') {
  await installCommonApiMocks(page, role)
  await installBudgetApiMocks(page)
  await loginAs(page, role)
  await page.goto(path)
  await page.waitForLoadState('networkidle')
}

test.describe('预算管理 E2E', () => {
  test('管理员访问预算管理并查看预算总览 KPI 与表格', async ({ page }) => {
    await bootBudget(page, '/admin/budget', 'admin')

    await expect(page).toHaveURL(/\/admin\/budget/)
    await expect(page.getByRole('heading', { name: '预算总览' })).toBeVisible()
    await expect(page.locator('[data-test="adjust-company-budget"]')).toBeVisible()
    await expect(page.locator('[data-test="budget-manage-overview"]')).toBeVisible()

    const overview = page.locator('[data-test="budget-manage-overview"]')
    await expect(overview.getByText('公司预算池')).toBeVisible()
    await expect(overview.getByText('$100,000.00').first()).toBeVisible()
    await expect(overview.getByText('未下发余量')).toBeVisible()
    await expect(overview.locator('[data-test="kpi-risk"]')).toContainText('2')
    await expect(overview.locator('[data-test="kpi-expansion"]')).toContainText('查看')

    await expect(overview.getByText('工作室预算')).toBeVisible()
    const studioRow = overview.locator('tbody tr').filter({ hasText: '效率平台' })
    await expect(studioRow).toBeVisible()
    await expect(studioRow).toContainText('$20,000.00')
    await expect(studioRow).toContainText('$12,400.00')
    await expect(studioRow).toContainText('高级 86%')
  })

  test('管理员切换到风险事件 tab 后看到风险 KPI 与事件列表', async ({ page }) => {
    await bootBudget(page, '/admin/budget', 'admin')

    await page.getByRole('tab', { name: /风险事件/ }).click()

    await expect(page.getByRole('heading', { name: '实时风险事件' })).toBeVisible()
    await expect(page.locator('[data-test="budget-kpi-total"]')).toContainText('2')
    await expect(page.locator('[data-test="budget-kpi-critical"]')).toContainText('1')
    await expect(page.getByText('AI 研发部')).toBeVisible()
    await expect(page.getByText('高级 / 月')).toBeVisible()
    await expect(page.getByText('部门等级预算')).toBeVisible()
  })

  test('管理员点击扩容申请 KPI 进入扩容申请页', async ({ page }) => {
    await bootBudget(page, '/admin/budget', 'admin')

    await page.locator('[data-test="kpi-expansion"]').click()

    await expect(page).toHaveURL(/\/admin\/expansion-requests/)
    await expect(page.getByRole('heading', { name: '平台扩容审批' })).toBeVisible()
    await expect(page.locator('[data-test="approval-permission-admin"]')).toContainText('平台管理员')
    await expect(page.locator('[data-test="approval-tab-pending"]')).toBeVisible()
    await expect(page.getByText('AI 研发部').first()).toBeVisible()
    await expect(page.getByText('部门负责人').first()).toBeVisible()
  })

  test('普通用户访问管理员预算页会被路由到 403', async ({ page }) => {
    await installCommonApiMocks(page, 'user')
    await loginAs(page, 'user')

    await page.goto('/admin/budget')
    await page.waitForLoadState('networkidle')

    await expect(page).toHaveURL(/\/403$/)
  })
})
