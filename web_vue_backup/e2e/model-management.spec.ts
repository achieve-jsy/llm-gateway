import { test, expect } from '@playwright/test'

test.describe('模型管理页面 - 基础加载', () => {
  test('页面标题显示为"模型管理"', async ({ page }) => {
    await page.goto('/admin/models')
    await expect(page.locator('.page-title')).toHaveText('模型管理')
  })

  test('表格渲染模型列表', async ({ page }) => {
    await page.goto('/admin/models')
    const table = page.locator('.el-table')
    await expect(table).toBeVisible()
  })

  test('分页组件可见', async ({ page }) => {
    await page.goto('/admin/models')
    await expect(page.locator('.el-pagination')).toBeVisible()
  })
})

test.describe('模型管理页面 - 搜索与筛选', () => {
  test('名称搜索输入框存在并可输入', async ({ page }) => {
    await page.goto('/admin/models')
    const searchInput = page.locator('input[placeholder="搜索模型名称"]')
    await expect(searchInput).toBeVisible()
    await searchInput.fill('gpt')
    await expect(searchInput).toHaveValue('gpt')
  })

  test('渠道筛选下拉可见', async ({ page }) => {
    await page.goto('/admin/models')
    const providerSelect = page.locator('.el-select').nth(1)
    await expect(providerSelect).toBeVisible()
  })

  test('标签筛选多选下拉可见', async ({ page }) => {
    await page.goto('/admin/models')
    const tagSelect = page.locator('.el-select').nth(2)
    await expect(tagSelect).toBeVisible()
  })
})

test.describe('模型管理页面 - 批量操作按钮', () => {
  test('批量操作按钮初始状态为禁用', async ({ page }) => {
    await page.goto('/admin/models')
    const batchEnableBtn = page.locator('button:has-text("批量启用")')
    const batchDisableBtn = page.locator('button:has-text("批量禁用")')
    const batchDeleteBtn = page.locator('button:has-text("批量删除")')
    const batchTagBtn = page.locator('button:has-text("批量设置标签")')

    await expect(batchEnableBtn).toBeDisabled()
    await expect(batchDisableBtn).toBeDisabled()
    await expect(batchDeleteBtn).toBeDisabled()
    await expect(batchTagBtn).toBeDisabled()
  })

  test('选中模型后批量按钮变为启用', async ({ page }) => {
    await page.goto('/admin/models')
    await page.waitForSelector('.el-table__body-wrapper')

    const checkboxes = page.locator('.el-table__body .el-checkbox')
    const count = await checkboxes.count()
    if (count > 0) {
      await checkboxes.first().click()
      const batchEnableBtn = page.locator('button:has-text("批量启用")')
      await expect(batchEnableBtn).toBeEnabled()
    }
  })
})

test.describe('模型管理页面 - 注册模型', () => {
  test('点击"注册模型"按钮打开对话框', async ({ page }) => {
    await page.goto('/admin/models')
    await page.locator('button:has-text("注册模型")').click()
    await expect(page.locator('.el-dialog:visible')).toBeVisible()
    await expect(page.locator('.el-dialog:visible .el-dialog__header')).toContainText('注册模型')
  })

  test('注册对话框包含必填字段', async ({ page }) => {
    await page.goto('/admin/models')
    await page.locator('button:has-text("注册模型")').click()
    await expect(page.locator('.el-dialog:visible')).toBeVisible()

    const nameInput = page.locator('.el-dialog:visible input[placeholder="如 gpt-4o, qwen-turbo"]')
    await expect(nameInput).toBeVisible()

    const providerSelect = page.locator('.el-dialog:visible .el-form-item:has-text("归属渠道") .el-select')
    await expect(providerSelect).toBeVisible()

    const typeSelect = page.locator('.el-dialog:visible .el-form-item:has-text("模型类型") .el-select')
    await expect(typeSelect).toBeVisible()
  })
})

test.describe('模型管理页面 - 编辑模型', () => {
  test('点击"编辑"按钮打开编辑对话框', async ({ page }) => {
    await page.goto('/admin/models')
    await page.waitForSelector('.el-table__body-wrapper')

    const editBtns = page.locator('.el-table__body button:has-text("编辑")')
    const count = await editBtns.count()
    if (count > 0) {
      await editBtns.first().click()
      await expect(page.locator('.el-dialog:visible .el-dialog__header')).toContainText('编辑模型')
    }
  })
})

test.describe('模型管理页面 - 删除模型', () => {
  test('删除按钮触发 popconfirm', async ({ page }) => {
    await page.goto('/admin/models')
    await page.waitForSelector('.el-table__body-wrapper')

    const deleteBtns = page.locator('.el-table__body button:has-text("删除")')
    const count = await deleteBtns.count()
    if (count > 0) {
      await deleteBtns.first().click()
      await expect(page.locator('.el-popconfirm:visible')).toBeVisible()
    }
  })
})

test.describe('模型管理页面 - 模型检测', () => {
  test('检测设置按钮可见', async ({ page }) => {
    await page.goto('/admin/models')
    await page.waitForSelector('.el-table__body-wrapper')
    const iconBtns = page.locator('.el-button .el-icon')
    const count = await iconBtns.count()
    expect(count).toBeGreaterThan(0)
  })

  test('健康状态列显示', async ({ page }) => {
    await page.goto('/admin/models')
    await page.waitForSelector('.el-table__body-wrapper')
    const tableBody = page.locator('.el-table__body-wrapper')
    await expect(tableBody).toBeVisible()
  })
})

test.describe('模型管理页面 - 分页', () => {
  test('默认页码为1', async ({ page }) => {
    await page.goto('/admin/models')
    const activePage = page.locator('.el-pagination .is-active')
    await expect(activePage).toHaveText('1')
  })

  test('切换每页条数', async ({ page }) => {
    await page.goto('/admin/models')
    const pageSizeSelect = page.locator('.el-pagination .el-pagination__sizes .el-select')
    if (await pageSizeSelect.isVisible()) {
      await pageSizeSelect.click()
      await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').nth(1).click()
    }
  })
})