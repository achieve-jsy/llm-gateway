import { test, expect, type Page } from '@playwright/test'

/**
 * Dashboard E2E — Core User Path
 *
 * Covers:
 *   1. 打开看板 → KPI 卡片加载
 *   2. 用户排行表排序
 *   3. 点击用户 → 详情面板展开
 *   4. 点击模型 → 交叉跳转到模型详情
 *   5. 长尾模型告警折叠/展开
 *
 * Prerequisites:
 *   - Backend running on localhost:8081 with dev auth enabled
 *   - Dev login: POST /auth/dev/login { username: 'admin', password: 'admin123' }
 */

/** Helper: login via dev endpoint and navigate to dashboard */
async function loginAndGoToDashboard(page: Page) {
  // Dev login
  const resp = await page.request.post('/api/v1/auth/dev/login', {
    data: { username: 'admin', password: 'admin123' },
  })
  expect(resp.ok()).toBeTruthy()

  // Navigate to the dashboard page
  await page.goto('/admin/usage')
  await page.waitForLoadState('networkidle')
}

// ─── Test Suite ──────────────────────────────────────────────

test.describe('Dashboard Core Path', () => {
  test.beforeEach(async ({ page }) => {
    await loginAndGoToDashboard(page)
  })

  test('KPI cards load with data', async ({ page }) => {
    // Wait for KPI cards to appear
    const kpiCards = page.locator('.kpi-card')
    await expect(kpiCards.first()).toBeVisible({ timeout: 10_000 })

    // Should have at least 4 KPI cards
    const count = await kpiCards.count()
    expect(count).toBeGreaterThanOrEqual(4)

    // Each card should have a value (not loading skeleton)
    for (let i = 0; i < Math.min(count, 4); i++) {
      const card = kpiCards.nth(i)
      const value = card.locator('.kpi-card__value, .kpi-value, [class*="value"]')
      await expect(value).toBeVisible({ timeout: 5_000 })
    }
  })

  test('User ranking table loads and supports sorting', async ({ page }) => {
    // Table should be visible
    const table = page.locator('table, .el-table, [class*="ranking"]')
    await expect(table.first()).toBeVisible({ timeout: 10_000 })

    // Find a sortable column header and click it
    const sortHeader = page.locator('[class*="col-sortable"], th.sortable, th[class*="sort"]').first()
    if (await sortHeader.isVisible()) {
      await sortHeader.click()
      // After click, should have active class or sort indicator
      await expect(sortHeader).toHaveClass(/active|sort/, { timeout: 2_000 })
    }
  })

  test('Click user → detail panel expands', async ({ page }) => {
    // Wait for table rows
    const rows = page.locator('tbody tr, .el-table__row, [class*="rank-row"]')
    await expect(rows.first()).toBeVisible({ timeout: 10_000 })

    // Click the first user row
    await rows.first().click()

    // Detail panel should appear
    const panel = page.locator('[class*="ud-panel"], [class*="detail-panel"], [class*="user-detail"]')
    await expect(panel.first()).toBeVisible({ timeout: 5_000 })
  })

  test('Long-tail alerts section exists and can toggle', async ({ page }) => {
    // Look for the long-tail section
    const ltSection = page.locator('[class*="lt-section"]')

    // May or may not exist depending on data — only test if present
    if (await ltSection.isVisible({ timeout: 3_000 }).catch(() => false)) {
      // Header should be clickable
      const header = ltSection.locator('[class*="lt-header"]')
      await expect(header).toBeVisible()

      // Click to expand
      await header.click()

      // Body should become visible (grid-template-rows: 1fr)
      const body = ltSection.locator('[class*="lt-body"]')
      await expect(body).toHaveClass(/open/, { timeout: 2_000 })
    }
  })
})

// ─── Cross-navigation test ──────────────────────────────────

test.describe('Cross-panel Navigation', () => {
  test('User detail → click model → switches to model panel', async ({ page }) => {
    await loginAndGoToDashboard(page)

    // Wait for table and click first user
    const rows = page.locator('tbody tr, .el-table__row, [class*="rank-row"]')
    await expect(rows.first()).toBeVisible({ timeout: 10_000 })
    await rows.first().click()

    // Wait for user detail panel
    const userPanel = page.locator('[class*="ud-panel"], [class*="user-detail"]')
    await expect(userPanel.first()).toBeVisible({ timeout: 5_000 })

    // Look for model tags/links inside user detail
    const modelTag = userPanel.locator('[class*="model-tag"], [class*="model-link"], a[class*="model"]').first()
    if (await modelTag.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await modelTag.click()

      // Model detail panel should now appear (user panel should hide)
      const modelPanel = page.locator('[class*="md-panel"], [class*="model-detail"]')
      await expect(modelPanel.first()).toBeVisible({ timeout: 5_000 })
    }
  })
})
