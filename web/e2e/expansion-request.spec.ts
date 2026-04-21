import { expect, test } from '@playwright/test'
import { fixtures, installCommonApiMocks, loginAs } from './support/e2e-mock'

test.describe('Team B · 扩容申请 E2E', () => {
  test('普通用户只能查看本人申请并可发起临时/长期扩容', async ({ page }) => {
    let createdPayload: any = null
    await installCommonApiMocks(page, 'user')
    await page.route('**/api/v1/expansion-requests', async route => {
      if (route.request().method() === 'POST') {
        createdPayload = route.request().postDataJSON()
        await route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ ...fixtures.expansions[0], id: 'created-exp', ...createdPayload }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) })
    })
    await page.route('**/api/v1/expansion-requests/precheck', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: { can_submit: true, can_notify_owner: false, current_limit_usd: '500.000000', current_used_usd: '200.000000', target_amount_usd: '900.000000', increase_usd: '400.000000', effective_after_usd: '900.000000', parent_constraints: [], affected_groups: [], visible_scopes: [], precheck_version: 'pv-new', data_as_of: new Date().toISOString(), stale: false } }) }))
    await loginAs(page, 'user')
    await page.goto('/admin/expansion-requests')
    await page.waitForLoadState('networkidle')

    await expect(page.getByTestId('approval-permission-user')).toContainText('普通用户')
    await expect(page.getByTestId('approval-tab-mine')).toBeVisible()
    await expect(page.getByTestId('approval-tab-pending')).toHaveCount(0)

    await page.getByRole('button', { name: '新建申请' }).click()
    const dialog = page.getByRole('dialog', { name: '新建扩容申请' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('临时扩容')).toBeVisible()
    await expect(dialog.getByText('长期调额').first()).toBeVisible()

    await dialog.getByText('临时扩容').click()
    await dialog.getByLabel('目标额度（USD）').fill('900')
    await dialog.getByLabel('申请原因').fill('业务冲刺需要临时提高额度')
    await expect(dialog.getByRole('button', { name: '提交申请' })).toBeEnabled()
    await dialog.getByRole('button', { name: '提交申请' }).click()

    await expect.poll(() => createdPayload?.duration_type).toBe('current_period')
    expect(createdPayload.reason).toContain('业务冲刺')
  })
})
