import { expect, test, type Page, type Route } from '@playwright/test'
import { installCommonApiMocks, loginAs } from './support/e2e-mock'

const API_BASE_URL = 'http://gateway.test'
const FULL_KEY = 'sk-test-personal-key-123456'

type KeyPolicy = 'free' | 'approval'

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

function keyListResponse() {
  return {
    code: 0,
    message: 'ok',
    data: {
      items: [
        {
          id: 'key-personal-1',
          name: 'daily_dev_key',
          masked_key: 'sk-test...3456',
          key_hint: '3456',
          enabled: true,
          log_enabled: true,
          access_all_groups: false,
          bound_groups: [{ id: 101, name: 'gpt-4o' }],
          created_at: '2026-07-15T09:30:00Z',
          updated_at: '2026-07-15T09:30:00Z',
          expires_at: null,
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
    },
  }
}

function emptyKeyRequestPage() {
  return { items: [], total: 0, page: 1, page_size: 10 }
}

async function installKeyPageMocks(page: Page, policy: KeyPolicy = 'free') {
  let createdKeyRequests = 0
  let modelsValidationCalls = 0

  await page.route('**/api/v1/settings/public', route => fulfillJSON(route, {
    data: {
      site_name: 'LLM Gateway',
      api_base_url: API_BASE_URL,
      rate_limit_per_minute: 60,
      max_concurrency_per_key: 10,
      supplier_name: '某公司',
      pricing_usd_to_cny_rate: 7.2,
    },
  }))

  await page.route('**/api/v1/keys?page=*', route => fulfillJSON(route, keyListResponse()))
  await page.route('**/api/v1/keys/key-personal-1/reveal', route => fulfillJSON(route, {
    data: { key: FULL_KEY },
  }))
  await page.route('**/api/v1/groups/available', route => fulfillJSON(route, {
    data: {
      groups: [
        { id: 101, name: 'gpt-4o', description: '主力模型组', strategy: 'priority' },
        { id: 102, name: 'claude-sonnet', description: 'Claude 模型组', strategy: 'round_robin' },
      ],
    },
  }))

  await page.route('**/api/v1/key-requests/policy**', route => fulfillJSON(route, {
    data: {
      policy,
      requires_approval: policy === 'approval',
      dept_id: 'dept-ai',
      dept_name: 'AI 研发部',
    },
  }))
  await page.route('**/api/v1/key-requests/summary', route => fulfillJSON(route, {
    data: policy === 'approval'
      ? { pending: createdKeyRequests, approved: 0, rejected: 0 }
      : { pending: 0, approved: 0, rejected: 0 },
  }))
  await page.route('**/api/v1/key-requests?page=*', route => fulfillJSON(route, policy === 'approval' && createdKeyRequests > 0
    ? {
        items: [{
          id: 'req-personal-1',
          key_name: 'apply_dev_key',
          status: 'pending',
          purpose: 'E2E 申请流程',
          budget_monthly: 0,
          created_at: '2026-07-15T10:00:00Z',
          updated_at: '2026-07-15T10:00:00Z',
          approver_name: '',
        }],
        total: 1,
        page: 1,
        page_size: 10,
      }
    : emptyKeyRequestPage()))
  await page.route('**/api/v1/key-requests', async route => {
    if (route.request().method() !== 'POST') {
      await fulfillJSON(route, emptyKeyRequestPage())
      return
    }
    const body = route.request().postDataJSON() as { key_name?: string; purpose?: string }
    createdKeyRequests += 1
    await fulfillJSON(route, {
      data: {
        id: 'req-personal-1',
        key_name: body.key_name,
        status: 'pending',
        purpose: body.purpose || '',
        budget_monthly: 0,
        created_at: '2026-07-15T10:00:00Z',
        updated_at: '2026-07-15T10:00:00Z',
      },
    })
  })

  await page.route(`${API_BASE_URL}/v1/models`, async route => {
    modelsValidationCalls += 1
    expect(route.request().headers().authorization).toBe(`Bearer ${FULL_KEY}`)
    await fulfillJSON(route, {
      object: 'list',
      data: [
        { id: 'gpt-4o', object: 'model', owned_by: 'llm-gateway' },
        { id: 'claude-sonnet-4-6', object: 'model', owned_by: 'llm-gateway' },
      ],
    })
  })

  return {
    getCreatedKeyRequests: () => createdKeyRequests,
    getModelsValidationCalls: () => modelsValidationCalls,
  }
}

async function bootKeys(page: Page, policy: KeyPolicy = 'free') {
  await installCommonApiMocks(page, 'user')
  await loginAs(page, 'user')
  const state = await installKeyPageMocks(page, policy)
  await page.goto('/keys')
  await page.waitForLoadState('networkidle')
  await expect(page.getByRole('heading', { name: 'API 密钥' })).toBeVisible()
  return state
}

test.describe('个人 Key 校验', () => {
  test('普通用户能看到我的密钥和 BASE URL，使用密钥会 reveal 并校验模型列表', async ({ page }) => {
    const state = await bootKeys(page)

    await expect(page.getByText('BASE URL')).toBeVisible()
    await expect(page.getByText(`${API_BASE_URL}/v1`)).toBeVisible()
    await expect(page.getByRole('cell', { name: 'daily_dev_key' })).toBeVisible()
    await expect(page.getByText('sk-test...3456')).toBeVisible()
    await expect(page.getByText('gpt-4o')).toBeVisible()

    await page.getByRole('button', { name: '使用密钥' }).click()
    const dialog = page.getByRole('dialog', { name: '使用密钥' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText(FULL_KEY).first()).toBeVisible()

    await dialog.getByRole('tab', { name: 'OpenCode' }).click()
    await expect(dialog.locator('code:visible').filter({ hasText: 'claude-sonnet-4-6' }).first()).toBeVisible()
    await expect(dialog.locator('code:visible').filter({ hasText: `${API_BASE_URL}/v1` }).first()).toBeVisible()
    expect(state.getModelsValidationCalls()).toBe(1)
  })

  test('创建 Key 表单校验名称；部门开启审批时创建进入申请流程并出现在我的申请', async ({ page }) => {
    const state = await bootKeys(page, 'approval')

    await expect(page.getByText('本部门开启了 Key 创建审批')).toBeVisible()
    await expect(page.getByRole('tab', { name: /我的申请/ })).toBeVisible()

    await page.getByRole('button', { name: '申请创建 Key' }).first().click()
    const applyDialog = page.getByRole('dialog', { name: '申请创建 Key' })
    await expect(applyDialog).toBeVisible()
    await applyDialog.getByLabel('Key 名称').fill('bad key name')
    await applyDialog.getByRole('button', { name: '提交申请（仅 Key）' }).click()
    await expect(page.getByText('仅允许字母、数字、下划线和连字符')).toBeVisible()
    expect(state.getCreatedKeyRequests()).toBe(0)

    await applyDialog.getByLabel('Key 名称').fill('apply_dev_key')
    await applyDialog.getByLabel('用途说明').fill('E2E 申请流程')
    await applyDialog.getByRole('button', { name: '提交申请（仅 Key）' }).click()
    await expect(applyDialog.getByText('申请已提交')).toBeVisible()
    expect(state.getCreatedKeyRequests()).toBe(1)

    await expect(applyDialog).toBeHidden()
    await page.getByRole('tab', { name: /我的申请/ }).click()
    await expect(page.getByRole('cell', { name: 'apply_dev_key' })).toBeVisible()
    await expect(page.getByText('审批中').first()).toBeVisible()
    await expect(page.getByText('E2E 申请流程')).toBeVisible()
  })
})
