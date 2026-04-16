import { expect, type Page, type Route } from '@playwright/test'

type Role = 'user' | 'dept_admin' | 'studio_admin' | 'admin'

const roleNames: Record<Role, string> = {
  user: '普通用户',
  dept_admin: '部门负责人',
  studio_admin: '工作室负责人',
  admin: '平台管理员',
}

function base64Url(input: string) {
  return Buffer.from(input).toString('base64url')
}

export function tokenFor(role: Role, userId = `${role}-user`) {
  const header = base64Url(JSON.stringify({ alg: 'none', typ: 'JWT' }))
  const payload = base64Url(JSON.stringify({
    user_id: userId,
    name: roleNames[role],
    email: `${userId}@example.com`,
    dept_id: role === 'user' ? 'dept-ai' : '',
    dept_name: role === 'user' ? 'AI 研发部' : '',
    role,
    exp: Math.floor(Date.now() / 1000) + 3600,
  }))
  return `${header}.${payload}.sig`
}

export async function loginAs(page: Page, role: Role = 'admin') {
  const token = tokenFor(role)
  await page.addInitScript(({ token, role }) => {
    localStorage.setItem('access_token', token)
    localStorage.setItem('refresh_token', 'refresh-token')
    localStorage.setItem('save_user', JSON.stringify({
      role,
      user: {
        id: `${role}-user`,
        name: role === 'admin' ? '平台管理员' : role === 'studio_admin' ? '工作室负责人' : role === 'dept_admin' ? '部门负责人' : '普通用户',
        email: `${role}-user@example.com`,
        departmentId: role === 'user' ? 'dept-ai' : '',
        departmentName: role === 'user' ? 'AI 研发部' : '',
      },
    }))
  }, { token, role })
}

async function fulfillJSON(route: Route, body: unknown) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

export async function installCommonApiMocks(page: Page, role: Role = 'admin') {
  await page.route('**/api/v1/settings/public', route => fulfillJSON(route, {
    data: { gateway_url: 'http://localhost:8080', site_title: 'LLM Gateway' },
  }))
  await page.route('**/api/v1/org/roles/me', route => fulfillJSON(route, {
    effective_role: role,
    roles: role === 'user' ? [] : [{ role, scope_type: role === 'admin' ? 'company' : role === 'studio_admin' ? 'studio' : 'dept', scope_id: role === 'admin' ? 'GLOBAL' : role === 'studio_admin' ? 'studio-platform' : 'dept-ai' }],
  }))
  await page.route('**/api/v1/me', route => fulfillJSON(route, { dept_status: 'mapped' }))
  await page.route('**/api/v1/announcements/unread', route => fulfillJSON(route, { data: [] }))
  await page.route('**/api/v1/announcements/unread/count', route => fulfillJSON(route, { data: 0 }))
  await page.route('**/api/v1/notifications/unread', route => fulfillJSON(route, { data: [] }))
  await page.route('**/api/v1/notifications/unread/count', route => fulfillJSON(route, { data: 0 }))
  await page.route('**/api/v1/expansion-requests/pending**', route => fulfillJSON(route, { items: [] }))
  await page.route('**/api/v1/org/studios**', route => fulfillJSON(route, { studios: fixtures.studios, total: fixtures.studios.length }))
  await page.route('**/api/v1/org/departments/unmapped**', route => fulfillJSON(route, { departments: [], total: 0 }))
  await page.route('**/api/v1/org/departments**', route => fulfillJSON(route, { departments: fixtures.departments, total: fixtures.departments.length }))
  await page.route('**/api/v1/org/dept-studio-map**', route => fulfillJSON(route, { mappings: [{ dept_id: 'dept-ai', studio_id: 'studio-platform' }], total: 1 }))
  await page.route('**/api/v1/org/all-roles', route => fulfillJSON(route, { roles: fixtures.roles }))
  await page.route('**/api/v1/org/users**', route => fulfillJSON(route, { users: fixtures.users }))
  await page.route('**/api/v1/budgets/context', route => fulfillJSON(route, { context: fixtures.budgetContext }))
  await page.route('**/api/v1/budgets/tier-usage**', route => fulfillJSON(route, { items: fixtures.tierUsage }))
  await page.route('**/api/v1/budgets?**', route => fulfillJSON(route, { items: fixtures.budgetItems }))
  await page.route('**/api/v1/budgets', route => fulfillJSON(route, { items: fixtures.budgetItems }))
  await page.route('**/api/v1/model-tiers**', route => fulfillJSON(route, { data: fixtures.modelTiers }))
  await page.route('**/api/v1/budget/risk-events/summary**', route => fulfillJSON(route, { data: { total: 1, warning: 1, critical: 0, by_type: { threshold_reached: 1 }, data_as_of: new Date().toISOString(), stale: false } }))
  await page.route('**/api/v1/budget/risk-events**', route => fulfillJSON(route, { data: { items: fixtures.budgetRisks, total: fixtures.budgetRisks.length, data_as_of: new Date().toISOString(), stale: false } }))
  await page.route('**/api/v1/my/budget/tiers', route => fulfillJSON(route, { data: fixtures.myBudgetTiers }))
  await page.route('**/api/v1/my/budget', route => fulfillJSON(route, { chain: fixtures.myBudgetChain }))
  await page.route('**/api/v1/key-requests/summary', route => fulfillJSON(route, { data: { pending: 1, approved: 1, rejected: 0 } }))
  await page.route('**/api/v1/key-requests/pending/count', route => fulfillJSON(route, { data: { count: 1, budget_count: 1 } }))
  await page.route('**/api/v1/key-requests/pending**', route => fulfillJSON(route, { items: fixtures.keyRequests, total: fixtures.keyRequests.length, page: 1, page_size: 10 }))
  await page.route('**/api/v1/key-requests/history**', route => fulfillJSON(route, { items: fixtures.keyHistory, total: fixtures.keyHistory.length, page: 1, page_size: 10 }))
  await page.route('**/api/v1/key-requests/policy**', route => fulfillJSON(route, { data: { requires_approval: false, policy: 'free' } }))
  await page.route('**/api/v1/key-requests**', route => fulfillJSON(route, { items: fixtures.keyRequests, total: fixtures.keyRequests.length, page: 1, page_size: 20 }))
  await page.route('**/api/v1/dept-policies/**/key-creation', route => fulfillJSON(route, { data: { dept_id: 'dept-ai', key_creation_policy: 'approval' } }))
  await page.route('**/api/v1/keys/*/reveal', route => fulfillJSON(route, { key: 'sk-test-full-key' }))
  await page.route('**/api/v1/keys**', route => fulfillJSON(route, { code: 0, message: 'ok', data: { items: fixtures.keys, total: fixtures.keys.length, page: 1, page_size: 20 } }))
  await page.route('**/api/v1/available-groups', route => fulfillJSON(route, { data: { groups: fixtures.groups } }))
  await page.route('**/v1/models', route => fulfillJSON(route, { object: 'list', data: [{ id: 'gpt-4', object: 'model', owned_by: 'llm-gateway' }, { id: 'claude-sonnet-4-5', object: 'model', owned_by: 'llm-gateway' }] }))
}

export async function boot(page: Page, path: string, role: Role = 'admin') {
  await installCommonApiMocks(page, role)
  await loginAs(page, role)
  await page.goto(path)
  await page.waitForLoadState('networkidle')
}

export async function expectNoConsoleErrors(page: Page) {
  const errors: string[] = []
  page.on('console', msg => {
    if (msg.type() === 'error') errors.push(msg.text())
  })
  await page.waitForTimeout(100)
  expect(errors).toEqual([])
}

export const fixtures = {
  budgetItems: [
    { allocation_id: 'alloc-company', scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算', amount_usd: 100000, daily_amount_usd: -1, weekly_amount_usd: -1, period: '2026-07', enabled: true, used_usd: 30000, used_ratio: 0.3 },
    { allocation_id: 'alloc-studio', scope_type: 'studio', scope_id: 'studio-platform', label: '效率平台', amount_usd: 20000, daily_amount_usd: -1, weekly_amount_usd: -1, period: '2026-07', enabled: true, used_usd: 12400, used_ratio: 0.62 },
    { allocation_id: 'alloc-dept', scope_type: 'dept_daily', scope_id: 'dept-ai', label: 'AI 研发部', amount_usd: 3000, daily_amount_usd: 200, weekly_amount_usd: 800, period: '2026-07', enabled: true, used_usd: 2600, used_ratio: 0.86 },
  ],
  myBudgetChain: [
    { scope_type: 'member', scope_id: 'user-user', label: '普通用户的配额', amount_usd: 500, daily_amount_usd: 30, weekly_amount_usd: 150, used_usd: 200, used_ratio: 0.4, period: '2026-07' },
  ],
  budgetContext: [
    { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算', amount_usd: 100000, used_usd: 30000, used_ratio: 0.3 },
    { scope_type: 'studio', scope_id: 'studio-platform', label: '效率平台', amount_usd: 20000, used_usd: 12400, used_ratio: 0.62 },
    { scope_type: 'dept_daily', scope_id: 'dept-ai', label: 'AI 研发部', amount_usd: 3000, used_usd: 2600, used_ratio: 0.86 },
  ],
  budgetRisks: [
    { id: 'risk-1', risk_type: 'threshold_reached', severity: 'warning', scope_type: 'dept_daily', scope_id: 'dept-ai', scope_name: 'AI 研发部', period_type: 'monthly', dimension_type: 'tier', model_tier_id: 'tier-premium', model_tier_name: '高级', effective_limit_usd: '500.000000', used_usd: '430.000000', usage_ratio: '0.86', limit_source: 'base_limit', blocked_requests: 0, detected_at: new Date().toISOString(), data_as_of: new Date().toISOString(), stale: false },
  ],
  tierLimits: [
    { id: 'limit-premium', budget_allocation_id: 'alloc-dept', model_tier_id: 'tier-premium', model_tier_name: '高级', period_type: 'monthly', base_limit_usd: '500.000000', effective_limit_usd: '500.000000', enabled: true, used_usd: '430.000000' },
  ],
  tierUsage: [
    { allocation_id: 'alloc-dept', scope_type: 'dept_daily', scope_id: 'dept-ai', tier_name: '高级', amount_usd: 500, used_usd: 430, used_ratio: 0.86, label: 'AI 研发部', enabled: true, state: 'enabled' },
  ],
  modelTiers: [
    { id: 'tier-premium', name: '高级', description: '', color: '#8b5cf6', sort_order: 1, enabled: true, archived_at: null, group_count: 2, budget_count: 1, history_count: 0, reference_count: 3, created_at: '2026-07-01T00:00:00Z', updated_at: '2026-07-01T00:00:00Z' },
    { id: 'tier-standard', name: '标准', description: '', color: '#3b82f6', sort_order: 2, enabled: true, archived_at: null, group_count: 2, budget_count: 1, history_count: 0, reference_count: 3, created_at: '2026-07-01T00:00:00Z', updated_at: '2026-07-01T00:00:00Z' },
  ],
  myBudgetTiers: {
    source_summary: { mode: 'personal', primary_scope_type: 'member', has_shared_pool: false, has_temporary_override: false },
    effective_total: [
      { period_type: 'monthly', configured: true, base_limit_usd: '500.000000', effective_limit_usd: '500.000000', used_usd: '200.000000', remaining_usd: '300.000000', reset_at: '2026-08-01T00:00:00Z', status: 'active' },
      { period_type: 'daily', configured: true, base_limit_usd: '30.000000', effective_limit_usd: '30.000000', used_usd: '10.000000', remaining_usd: '20.000000', reset_at: '2026-07-16T00:00:00Z', status: 'active' },
      { period_type: 'weekly', configured: true, base_limit_usd: '150.000000', effective_limit_usd: '150.000000', used_usd: '50.000000', remaining_usd: '100.000000', reset_at: '2026-07-20T00:00:00Z', status: 'active' },
    ],
    budget_chain: [{ scope_type: 'member', label: '普通用户的配额', visible_amounts: true }],
    tier_limits: [{ model_tier_id: 'tier-premium', model_tier_name: '高级', enabled: true, periods: [{ period_type: 'monthly', configured: true, base_limit_usd: '200.000000', effective_limit_usd: '200.000000', used_usd: '50.000000', remaining_usd: '150.000000', reset_at: '2026-08-01T00:00:00Z', status: 'enabled', limiting_source: { scope_type: 'member' } }] }],
    data_updated_at: new Date().toISOString(),
    stale: false,
  },
  expansions: [
    { id: 'exp-1', scope_type: 'dept_daily', scope_id: 'dept-ai', scope_name: 'AI 研发部', requester_id: 'dept-admin-user', requester_name: '部门负责人', period_type: 'monthly', dimension_type: 'tier', model_tier_id: 'tier-premium', model_tier_name: '高级', duration_type: 'current_period', priority: 'urgent', current_amount: 500, requested_amount: 800, current_amount_snapshot: '500.000000', used_amount_snapshot: '430.000000', target_amount_usd: '800.000000', reason: '项目冲刺', urgent_reason: '上线', status: 'pending', current_level: 'studio', precheck_version: 'pv1', prechecked_at: '2026-07-15T10:00:00Z', expires_at: '2026-08-01T00:00:00Z', applied_at: null, sync_status: 'pending', created_at: '2026-07-15T10:00:00Z', updated_at: '2026-07-15T10:00:00Z' },
  ],
  groups: [
    { id: 1, name: 'gpt-4', description: 'GPT 4', strategy: 'round_robin' },
    { id: 2, name: 'claude-sonnet-4-5', description: 'Claude', strategy: 'round_robin' },
  ],
  keys: [
    { id: 'key-1', name: 'dev-key', masked_key: 'sk-***abcd', key_hint: 'abcd', enabled: true, log_enabled: true, access_all_groups: true, created_at: '2026-07-15T10:00:00Z', updated_at: '2026-07-15T10:00:00Z', bound_groups: [{ id: 1, name: 'gpt-4' }] },
  ],
  keyRequests: [
    { id: 'kr-1', requester_id: 'user-user', requester_name: '普通用户', dept_id: 'dept-ai', dept_name: 'AI 研发部', key_name: 'research-key', key_type: 'personal', purpose: '研究测试', group_ids: '[1]', budget_monthly: 100, status: 'pending', created_at: '2026-07-15T10:00:00Z', updated_at: '2026-07-15T10:00:00Z' },
  ],
  keyHistory: [
    { id: 'kr-h1', requester_id: 'user-user', requester_name: '普通用户', dept_id: 'dept-ai', dept_name: 'AI 研发部', key_name: 'approved-key', key_type: 'personal', purpose: '已通过', group_ids: '[1]', budget_monthly: 50, status: 'approved', approver_name: '部门负责人', approve_comment: '同意', created_at: '2026-07-14T10:00:00Z', updated_at: '2026-07-14T12:00:00Z' },
  ],
  studios: [{ id: 'studio-platform', name: '效率平台' }],
  departments: [{ id: 'dept-row-ai', dept_id: 'dept-ai', dept_name: 'AI 研发部', studio_id: 'studio-platform', status: 'active' }],
  users: [{ id: 'user-user', name: '普通用户', email: 'user-user@example.com', dept_name: 'AI 研发部', dept_id: 'dept-ai' }],
  roles: [
    { id: 1, user_id: 'admin-user', user_name: '平台管理员', email: 'admin@example.com', dept_name: '', role: 'admin', scope_type: 'company', scope_id: 'GLOBAL', granted_by: 'system', created_at: '2026-07-01T00:00:00Z' },
    { id: 2, user_id: 'studio-admin-user', user_name: '工作室负责人', email: 'studio@example.com', dept_name: '效率平台', role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-platform', granted_by: 'admin', created_at: '2026-07-01T00:00:00Z' },
    { id: 3, user_id: 'dept-admin-user', user_name: '部门负责人', email: 'dept@example.com', dept_name: 'AI 研发部', role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-ai', granted_by: 'admin', created_at: '2026-07-01T00:00:00Z' },
  ],
}
