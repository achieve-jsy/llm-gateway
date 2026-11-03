import { test, expect, type Page } from '@playwright/test'
import crypto from 'crypto'

/**
 * AI 分析看板 E2E — 真后端回归
 *
 * 前置条件:
 *   - control-plane 运行在 :8181,连 e2e 导入库
 *     (llm_gateway_import_20260715_224428,数据窗口 2026-06-03 ~ 2026-07-16)
 *   - vite dev 代理 /api/v1/* → :8181(playwright.config 自动起)
 *   - 用 HS256 签名的 admin JWT 注入 localStorage 登录(绕开 OAuth)
 *
 * 数据窗口策略:
 *   页面默认日期是「近7天」(基于浏览器 new Date())。e2e 库只含 6/03~7/16 数据,
 *   真实今天(8 月)默认窗口为空。用 page.clock 把浏览器时钟固定到 2026-06-18,
 *   使默认近7天 = 2026-06-12 ~ 2026-06-18,落在数据窗口内,页面呈数据态。
 */

const JWT_SECRET = process.env.JWT_SECRET || 'd8f7a3b2c1e9f4567890abc123def456'

const ADMIN = {
  id: '3e706288-0cc4-4bb9-96d1-19308e5e86ab',
  name: '洪岗峰',
  email: 'honggangfeng@example.com',
  deptId: 'cecf7c85-052b-445e-a3ec-4e0bb669206d',
}

function b64url(o: unknown) {
  return Buffer.from(JSON.stringify(o)).toString('base64url')
}

function adminToken() {
  const now = Math.floor(Date.now() / 1000)
  const header = b64url({ alg: 'HS256', typ: 'JWT' })
  const payload = b64url({
    sub: ADMIN.id,
    user_id: ADMIN.id,
    name: ADMIN.name,
    email: ADMIN.email,
    dept_id: ADMIN.deptId,
    role: 'admin',
    iat: now,
    exp: now + 3600,
  })
  const sig = crypto.createHmac('sha256', JWT_SECRET).update(`${header}.${payload}`).digest('base64url')
  return `${header}.${payload}.${sig}`
}

/** 固定浏览器时钟到数据窗口内,默认近7天 = 06-12~06-18(有数据) */
async function bootAnalytics(page: Page) {
  await page.clock.install()
  await page.clock.setFixedTime(new Date(2026, 5, 18, 12, 0, 0).getTime())
  await page.addInitScript(
    ({ token, admin }) => {
      localStorage.setItem('access_token', token)
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem(
        'save_user',
        JSON.stringify({
          role: 'admin',
          user: {
            id: admin.id,
            name: admin.name,
            email: admin.email,
            departmentId: admin.deptId,
          },
        }),
      )
    },
    { token: adminToken(), admin: ADMIN },
  )
  // 未读公告弹窗(AnnouncementPopup,.overlay 全屏)会拦截点击,让接口返回空数组即可不渲染
  await page.route('**/api/v1/announcements/unread*', route =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: { data: [] } }),
    }),
  )
  await page.goto('/admin/usage/analytics')
}

/** 记录核心分析端点的 HTTP 状态,供链路断言 */
function trackAnalyticsResponses(page: Page) {
  const statuses: Record<string, number> = {}
  page.on('response', resp => {
    const url = resp.url()
    if (
      url.includes('/api/v1/analytics/insight') ||
      url.includes('/api/v1/usage/dashboard/model-hourly') ||
      url.includes('/api/v1/usage/dashboard/model-users') ||
      url.includes('/api/v1/usage/dashboard/models')
    ) {
      const key = url.includes('insight')
        ? 'insight'
        : url.includes('model-hourly')
          ? 'model-hourly'
          : url.includes('model-users')
            ? 'model-users'
            : 'models'
      statuses[key] = resp.status()
    }
  })
  return statuses
}

test.describe('AI 分析看板 · 真后端', () => {
  test('五个核心端点全部 200(数据链路通)', async ({ page }) => {
    const statuses = trackAnalyticsResponses(page)
    await bootAnalytics(page)

    // 等 6 路数据管线 settle(4 种端点类型全部有响应)
    await expect
      .poll(async () => Object.keys(statuses).length)
      .toBeGreaterThanOrEqual(4, { timeout: 20_000 })
    for (const key of ['insight', 'model-hourly', 'model-users', 'models']) {
      expect(statuses[key], `${key} 应返回 200`).toBe(200)
    }
  })

  test('裁决条 + KPI + AI 卡渲染真实数据', async ({ page }) => {
    await bootAnalytics(page)

    // 裁决条:出现任一档位(受控/需关注/需干预/超支)
    const status = page.locator('.verdict .status')
    await expect(status).toBeVisible({ timeout: 15_000 })
    await expect(status).toHaveText(/受控|需关注|需干预|超支/)

    // KPI 四格:总费用非零
    const kpis = page.locator('.kpi-row .kpi')
    await expect(kpis).toHaveCount(4, { timeout: 15_000 })
    const totalVal = kpis.first().locator('.kpi-val')
    await expect(totalVal).not.toHaveText('$0.00')

    // AI 卡三段:至少「事实」段有文本
    await expect(page.locator('.ai-card')).toBeVisible()
    const factTag = page.locator('.ai-sec-tag', { hasText: '事实' })
    await expect(factTag.first()).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('.ai-sec-text').first()).not.toHaveText('')
  })

  test('异常清单区块渲染(无异常时显示受控空态)', async ({ page }) => {
    await bootAnalytics(page)
    const alerts = page.locator('.alerts, .alerts--empty')
    await expect(alerts.first()).toBeVisible({ timeout: 15_000 })
  })

  test('模型表加载真实模型行,点击展开 24h 明细', async ({ page }) => {
    await bootAnalytics(page)

    const rows = page.locator('.model-row')
    await expect(rows.first()).toBeVisible({ timeout: 15_000 })
    const count = await rows.count()
    expect(count).toBeGreaterThanOrEqual(1)

    // 窗口内确定出现的模型(qwen3.6-plus 是费用大头)
    await expect(page.locator('.model-row .nm', { hasText: 'qwen3.6-plus' }).first()).toBeVisible()

    // 点击展开 24h 明细(小时费用/小时 Token/热力)
    await rows.first().click()
    const detail = page.locator('.model-detail.open')
    await expect(detail).toBeVisible({ timeout: 5_000 })
    await expect(detail).toContainText('小时费用')
    await expect(detail).toContainText('小时 Token')
    await expect(detail).toContainText('小时费用热力')
  })

  test('部门透视与用户排行渲染', async ({ page }) => {
    await bootAnalytics(page)

    // 部门透视表
    const deptWrap = page.locator('.dept-wrap')
    await expect(deptWrap).toBeVisible({ timeout: 15_000 })
    const deptRows = deptWrap.locator('.dept-tr')
    if ((await deptRows.count()) > 0) {
      await deptRows.first().click()
      await expect(deptWrap.locator('.drill-row, .sub table').first()).toBeVisible({ timeout: 5_000 })
    }

    // 用户排行表
    const ur = page.locator('.ur-wrap')
    await expect(ur).toBeVisible({ timeout: 15_000 })
  })
})
