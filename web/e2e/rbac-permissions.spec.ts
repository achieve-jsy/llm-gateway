import { test, expect, type Page, type Route } from '@playwright/test'
import { boot, fixtures, installCommonApiMocks, loginAs } from './support/e2e-mock'

type Role = 'user' | 'dept_admin' | 'studio_admin' | 'admin'

const studios = [
  { id: 'studio-platform', name: '效率平台', created_at: '2026-07-01T00:00:00Z', updated_at: '2026-07-01T00:00:00Z' },
]

const departments = [
  { dept_id: 'dept-ai', dept_name: 'AI 研发部', studio_id: 'studio-platform', studio_name: '效率平台', status: 'active' },
]

const roleRows = [
  {
    id: 1,
    user_id: 'admin-user',
    user_name: '平台管理员',
    email: 'admin-user@example.com',
    dept_name: '',
    role: 'admin',
    scope_type: 'company',
    scope_id: 'GLOBAL',
    granted_by: 'system',
    created_at: '2026-07-01T00:00:00Z',
  },
  {
    id: 2,
    user_id: 'studio-admin-user',
    user_name: '工作室负责人',
    email: 'studio-admin-user@example.com',
    dept_name: '平台治理部',
    role: 'studio_admin',
    scope_type: 'studio',
    scope_id: 'studio-platform',
    granted_by: 'admin-user',
    created_at: '2026-07-02T00:00:00Z',
  },
  {
    id: 3,
    user_id: 'dept-admin-user',
    user_name: '部门负责人',
    email: 'dept-admin-user@example.com',
    dept_name: 'AI 研发部',
    role: 'dept_admin',
    scope_type: 'dept',
    scope_id: 'dept-ai',
    granted_by: 'admin-user',
    created_at: '2026-07-03T00:00:00Z',
  },
]

const modelTiers = [
  {
    id: 'tier-premium',
    name: '高级',
    description: '高能力模型',
    color: '#8b5cf6',
    sort_order: 1,
    enabled: true,
    archived_at: null,
    group_count: 2,
    budget_count: 1,
    history_count: 0,
    reference_count: 3,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
  },
]

async function fulfillJSON(route: Route, body: unknown) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

async function installRbacPageMocks(page: Page) {
  await page.route('**/api/v1/org/all-roles', route => fulfillJSON(route, { roles: roleRows }))
  await page.route('**/api/v1/org/studios**', route => fulfillJSON(route, { studios, total: studios.length }))
  await page.route('**/api/v1/org/departments**', route => fulfillJSON(route, { departments, total: departments.length }))
  await page.route('**/api/v1/budgets/*/tier-limits**', route => fulfillJSON(route, { data: { tier_limits: [] } }))
  await page.route('**/api/v1/budgets/tier-usage**', route => fulfillJSON(route, { items: [] }))
  await page.route('**/api/v1/budget/risk-events/summary**', route => fulfillJSON(route, { data: { total: 0, by_type: {}, by_severity: {} } }))
  await page.route('**/api/v1/budget/risk-events**', route => fulfillJSON(route, { data: { items: [], total: 0, page: 1, page_size: 50 } }))
  await page.route('**/api/v1/budgets/context**', route => fulfillJSON(route, {
    context: [
      { scope_type: 'company', scope_id: 'GLOBAL', label: '全公司预算', amount_usd: 100000, used_usd: 30000, used_ratio: 0.3 },
      { scope_type: 'studio', scope_id: 'studio-platform', label: '效率平台', amount_usd: 20000, used_usd: 12400, used_ratio: 0.62 },
      { scope_type: 'dept_daily', scope_id: 'dept-ai', label: 'AI 研发部', amount_usd: 3000, used_usd: 2600, used_ratio: 0.86 },
    ],
  }))
  await page.route('**/api/v1/model-tiers**', route => fulfillJSON(route, { data: modelTiers }))
  await page.route('**/api/v1/usage/dashboard**', route => fulfillJSON(route, { breakdown: [] }))
  await page.route('**/api/v1/my/budget**', route => fulfillJSON(route, { chain: fixtures.budgetItems }))
  await page.route('**/api/v1/expansion-requests/pending**', route => fulfillJSON(route, { items: fixtures.expansions }))
  await page.route('**/api/v1/expansion-requests**', route => fulfillJSON(route, { items: fixtures.expansions }))
  await page.route('**/api/v1/budgets**', route => fulfillJSON(route, { items: fixtures.budgetItems }))
}

async function bootWithRbacMocks(page: Page, path: string, role: Role) {
  await installRbacPageMocks(page)
  await boot(page, path, role)
}

function sidebar(page: Page) {
  return page.locator('#app-navigation')
}

async function expectForbidden(page: Page) {
  await expect(page).toHaveURL(/\/403$/)
  await expect(sidebar(page)).toBeVisible()
}

test.describe('RBAC 权限路由', () => {
  test('普通用户访问 admin-only 和 manager-only 页面会被 403 拦截', async ({ page }) => {
    await bootWithRbacMocks(page, '/admin/org/roles', 'user')
    await expectForbidden(page)

    await page.goto('/admin/budget')
    await page.waitForLoadState('networkidle')
    await expectForbidden(page)

    await page.goto('/admin/org')
    await page.waitForLoadState('networkidle')
    await expectForbidden(page)
  })

  test('部门管理员可访问预算管理和扩容申请，但不能访问角色管理', async ({ page }) => {
    await bootWithRbacMocks(page, '/admin/budget', 'dept_admin')
    await expect(page).toHaveURL(/\/admin\/budget/)
    await expect(page.getByRole('heading', { name: '预算总览' })).toBeVisible()
    await expect(page.getByText('查看本部门预算与成员用量风险，可直接调整成员配额。')).toBeVisible()

    await page.goto('/admin/expansion-requests')
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(/\/admin\/expansion-requests/)
    await expect(page.getByRole('heading', { name: '部门额度审批' })).toBeVisible()
    await expect(page.locator('[data-test="approval-permission-dept_admin"]')).toContainText('部门管理员')

    await page.goto('/admin/org/roles')
    await page.waitForLoadState('networkidle')
    await expectForbidden(page)
  })

  test('用量统计仅 admin 可访问，部门管理员被 403 拦截', async ({ page }) => {
    await bootWithRbacMocks(page, '/admin/usage/dashboard', 'dept_admin')
    await expectForbidden(page)

    await page.goto('/admin/usage/report')
    await page.waitForLoadState('networkidle')
    await expectForbidden(page)
  })

  test('管理员可访问角色管理并看到角色统计和授权列表', async ({ page }) => {
    await bootWithRbacMocks(page, '/admin/org/roles', 'admin')

    await expect(page).toHaveURL(/\/admin\/org\/roles/)
    await expect(page.getByRole('heading', { name: '角色管理' })).toBeVisible()
    await expect(page.locator('.role-stat-card.admin')).toContainText('平台管理员')
    await expect(page.locator('.role-stat-card.studio')).toContainText('工作室负责人')
    await expect(page.locator('.role-stat-card.dept')).toContainText('部门负责人')
    await expect(page.locator('.role-stat-card.admin .stat-num')).toHaveText('1')
    await expect(page.locator('.role-stat-card.studio .stat-num')).toHaveText('1')
    await expect(page.locator('.role-stat-card.dept .stat-num')).toHaveText('1')
    await expect(page.getByText('当前授权列表')).toBeVisible()
    await expect(page.getByRole('cell', { name: '平台管理员' }).first()).toBeVisible()
    await expect(page.getByRole('cell', { name: '工作室负责人' }).first()).toBeVisible()
    await expect(page.getByRole('cell', { name: '部门负责人' }).first()).toBeVisible()
    await expect(page.getByText('全公司')).toBeVisible()
    await expect(page.getByText('工作室：效率平台')).toBeVisible()
    await expect(page.getByText('部门：AI 研发部')).toBeVisible()
  })
})

test.describe('RBAC 侧边栏菜单', () => {
  test('普通用户只看到员工菜单，不显示管理和平台入口', async ({ page }) => {
    await installRbacPageMocks(page)
    await installCommonApiMocks(page, 'user')
    await loginAs(page, 'user')
    await page.goto('/access/unified-entry')
    await page.waitForLoadState('networkidle')

    const nav = sidebar(page)
    await expect(nav.locator('.menu-group-label', { hasText: /^员工$/ })).toBeVisible()
    await expect(nav.getByText('首页')).toBeVisible()
    await expect(nav.getByText('我的 Key')).toBeVisible()
    await expect(nav.getByText('我的预算')).toBeVisible()
    await expect(nav.getByText('模型组健康')).toBeVisible()
    await expect(nav.locator('.menu-group-label', { hasText: /^管理$/ })).toHaveCount(0)
    await expect(nav.getByText('预算管理')).toHaveCount(0)
    await expect(nav.getByText('扩容申请')).toHaveCount(0)
    await expect(nav.getByText('角色管理')).toHaveCount(0)
    await expect(nav.getByText('平台')).toHaveCount(0)
    await expect(nav.getByText('渠道管理')).toHaveCount(0)
  })

  test('部门管理员看到管理菜单和范围入口，但不显示平台-only 菜单', async ({ page }) => {
    await installRbacPageMocks(page)
    await installCommonApiMocks(page, 'dept_admin')
    await loginAs(page, 'dept_admin')
    await page.goto('/access/unified-entry')
    await page.waitForLoadState('networkidle')

    const nav = sidebar(page)
    await expect(nav.locator('.menu-group-label', { hasText: /^管理$/ })).toBeVisible()
    await expect(nav.getByText('组织架构')).toBeVisible()
    await expect(nav.getByText('我的范围')).toBeVisible()
    await expect(nav.getByText('预算管理')).toBeVisible()
    await expect(nav.getByText('扩容申请')).toBeVisible()
    await expect(nav.getByText('模型组授权')).toBeVisible()
    await expect(nav.getByText('用量看板')).toHaveCount(0)
    await expect(nav.getByText('角色管理')).toHaveCount(0)
    await expect(nav.locator('.menu-group-label', { hasText: /^平台$/ })).toHaveCount(0)
    await expect(nav.getByText('渠道管理')).toHaveCount(0)
    await expect(nav.getByText('系统设置')).toHaveCount(0)
  })

  test('管理员看到角色管理、平台运维菜单，并保留我的预算入口', async ({ page }) => {
    await installRbacPageMocks(page)
    await installCommonApiMocks(page, 'admin')
    await loginAs(page, 'admin')
    await page.goto('/access/unified-entry')
    await page.waitForLoadState('networkidle')

    const nav = sidebar(page)
    await expect(nav.locator('.menu-group-label', { hasText: /^管理$/ })).toBeVisible()
    await expect(nav.getByText('角色管理')).toBeVisible()
    await expect(nav.getByText('用量看板')).toBeVisible()
    await expect(nav.locator('.menu-group-label', { hasText: /^平台$/ })).toBeVisible()
    await expect(nav.getByText('渠道管理')).toBeVisible()
    await expect(nav.getByText('模型管理')).toBeVisible()
    await expect(nav.locator('.menu-group-label', { hasText: /^运维$/ })).toBeVisible()
    await expect(nav.getByText('系统设置')).toBeVisible()
    await expect(nav.getByText('我的预算')).toBeVisible()
    await expect(nav.getByText('我的范围')).toHaveCount(0)
  })
})
