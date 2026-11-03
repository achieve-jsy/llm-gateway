import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus, { ElMessage } from 'element-plus'
import FeedbackListPage from '../FeedbackListPage.vue'
import type { FeedbackAttachment, FeedbackItem } from '@/api/feedback'

// ── Mock element-plus icons（页面只用到 Refresh）────────────────────
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
}))

// ── Mock API：拦截 @/api/feedback，禁止发真实请求 ──────────────────
const mockListFeedback = vi.fn()
const mockUpdateFeedbackStatus = vi.fn()
const mockFetchFeedbackAttachmentBlob = vi.fn()

vi.mock('@/api/feedback', () => ({
  listFeedback: (...args: unknown[]) => mockListFeedback(...args),
  updateFeedbackStatus: (...args: unknown[]) => mockUpdateFeedbackStatus(...args),
  fetchFeedbackAttachmentBlob: (...args: unknown[]) => mockFetchFeedbackAttachmentBlob(...args),
}))

// ── Mock vue-router（页面当前未用路由，保持与其他 spec 一致）──────
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useRoute: () => ({ query: {} }),
}))

// ── Fixtures ───────────────────────────────────────────────────────
function makeFeedback(overrides: Partial<FeedbackItem> = {}): FeedbackItem {
  return {
    id: 'fb-1',
    type: 'suggestion',
    content: '希望支持批量导出账单',
    contact: 'zhangsan@example.com',
    status: 'pending',
    user_id: 'user-0001',
    user_name: '张三',
    user_email: 'zhangsan@example.com',
    created_at: '2026-07-01T08:00:00Z',
    handled_at: null,
    handled_by: null,
    handle_note: null,
    ...overrides,
  }
}

const suggestionItem = makeFeedback({
  id: 'fb-1',
  type: 'suggestion',
  content: '希望支持批量导出账单',
  user_name: '张三',
})

/** 第二条：user_name 为空，走 submitterName 的 user_id 前 8 位退化展示 */
const bugItem = makeFeedback({
  id: 'fb-2',
  type: 'bug',
  content: '导出功能点击后一直转圈不报错',
  user_name: '',
  user_id: 'abcdef1234567890',
  status: 'processing',
  contact: '',
})

function listResponse(items: FeedbackItem[], total = items.length) {
  return { items, total, page: 1, page_size: 20 }
}

// ── 附件 fixture ───────────────────────────────────────────────────
function makeAttachment(id: string, feedbackId = 'fb-3'): FeedbackAttachment {
  return {
    id,
    feedback_id: feedbackId,
    file_name: `${id}.webp`,
    mime: 'image/webp',
    size_bytes: 128,
    width: 1600,
    height: 900,
    created_at: '2026-07-01T08:00:00Z',
  }
}

/** 带两张贴图的反馈 */
const attachedItem = makeFeedback({
  id: 'fb-3',
  type: 'bug',
  content: '截图见附件，点开大图能看清报错',
  attachments: [makeAttachment('att-1'), makeAttachment('att-2')],
})

// jsdom 未实现 object URL；返回唯一 URL 便于断言 revoke
const createObjectURL = vi.fn()
const revokeObjectURL = vi.fn()
let objectUrlSeed = 0

/** 等待列表 + 缩略图预取（并发 3）全部落定 */
async function flushAll() {
  await flushPromises()
  await flushPromises()
  await flushPromises()
}

// ── Helpers ────────────────────────────────────────────────────────
let errorSpy: ReturnType<typeof vi.spyOn>

function mountPage() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(FeedbackListPage, {
    global: { plugins: [pinia, ElementPlus] },
  })
}

/** 真实交互：点开 el-select 下拉（teleport 到 body）并点选指定中文选项 */
async function pickSelectOption(
  wrapper: ReturnType<typeof mountPage>,
  selectSelector: string,
  optionText: string,
) {
  await wrapper.get(`${selectSelector} .el-select__wrapper`).trigger('click')
  await flushPromises()

  const options = Array.from(
    document.querySelectorAll<HTMLElement>('.el-select-dropdown__item'),
  )
  const target = options.find(el => el.textContent?.trim() === optionText)
  expect(target, `下拉中未找到选项「${optionText}」`).toBeTruthy()

  target!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
}

// ── Tests ──────────────────────────────────────────────────────────
describe('FeedbackListPage 反馈管理页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    errorSpy = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    mockListFeedback.mockResolvedValue(listResponse([suggestionItem, bugItem]))
    mockUpdateFeedbackStatus.mockResolvedValue(suggestionItem)
    mockFetchFeedbackAttachmentBlob.mockResolvedValue(
      new Blob([new Uint8Array(8)], { type: 'image/webp' }),
    )
    objectUrlSeed = 0
    createObjectURL.mockImplementation(() => `blob:mock-${(objectUrlSeed += 1)}`)
    ;(URL as unknown as Record<string, unknown>).createObjectURL = createObjectURL
    ;(URL as unknown as Record<string, unknown>).revokeObjectURL = revokeObjectURL
  })

  afterEach(() => {
    errorSpy?.mockRestore()
    // 清理 el-select 下拉 teleport 到 body 的残留节点，避免跨用例污染
    document.body.innerHTML = ''
  })

  it('挂载不报错，根元素带 data-test，并在挂载时调用 listFeedback', async () => {
    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.find('[data-test="feedback-list-page"]').exists()).toBe(true)
    expect(wrapper.attributes('data-test')).toBe('feedback-list-page')
    expect(errorSpy).not.toHaveBeenCalled()

    expect(mockListFeedback).toHaveBeenCalledTimes(1)
    expect(mockListFeedback).toHaveBeenCalledWith({
      status: undefined,
      type: undefined,
      page: 1,
      page_size: 20,
    })

    wrapper.unmount()
  })

  it('后端返回两条数据时渲染出内容、提交人展示名与类型中文标签', async () => {
    mockListFeedback.mockResolvedValue(listResponse([suggestionItem, bugItem], 2))

    const wrapper = mountPage()
    await flushPromises()

    // 表格确实拿到了两条数据
    const table = wrapper.findComponent({ name: 'ElTable' })
    expect(table.exists()).toBe(true)
    expect(table.props('data')).toHaveLength(2)

    // 单元格层面确实渲染出了字段值（含类型中文标签列）
    const cellTexts = wrapper.findAll('.el-table__body .cell').map(cell => cell.text())
    expect(cellTexts).toContain('希望支持批量导出账单')
    expect(cellTexts).toContain('导出功能点击后一直转圈不报错')
    expect(cellTexts).toContain('张三')
    expect(cellTexts).toContain('abcdef12…')
    expect(cellTexts).toContain('功能建议')
    expect(cellTexts).toContain('问题反馈')

    const text = wrapper.text()
    // 内容
    expect(text).toContain('希望支持批量导出账单')
    expect(text).toContain('导出功能点击后一直转圈不报错')
    // 提交人展示名：user_name 优先，缺失时退化为 user_id 前 8 位
    expect(text).toContain('张三')
    expect(text).toContain('abcdef12…')
    // 类型中文标签
    expect(text).toContain('功能建议')
    expect(text).toContain('问题反馈')
    // 不应该出现原始英文枚举值
    expect(text).not.toContain('suggestion')
    expect(text).not.toContain('bug')

    wrapper.unmount()
  })

  it('切换状态筛选后以正确的 status 参数并重置页码重新调用 listFeedback', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(mockListFeedback).toHaveBeenCalledTimes(1)

    await pickSelectOption(wrapper, '[data-test="feedback-filter-status"]', '待处理')

    expect(mockListFeedback).toHaveBeenCalledTimes(2)
    expect(mockListFeedback).toHaveBeenLastCalledWith({
      status: 'pending',
      type: undefined,
      page: 1,
      page_size: 20,
    })

    wrapper.unmount()
  })

  it('分页变化时以正确的 page 参数调用 listFeedback', async () => {
    // total=45 / page_size=20 → 3 页，翻页按钮可用
    mockListFeedback.mockResolvedValue(listResponse([suggestionItem, bugItem], 45))

    const wrapper = mountPage()
    await flushPromises()
    expect(mockListFeedback).toHaveBeenCalledTimes(1)
    expect(mockListFeedback).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 1 }),
    )

    const nextBtn = wrapper.get('.el-pagination .btn-next')
    expect(nextBtn.attributes('disabled')).toBeUndefined()
    await nextBtn.trigger('click')
    await flushPromises()

    expect(mockListFeedback).toHaveBeenCalledTimes(2)
    expect(mockListFeedback).toHaveBeenLastCalledWith({
      status: undefined,
      type: undefined,
      page: 2,
      page_size: 20,
    })

    wrapper.unmount()
  })

  it('listFeedback reject 时页面不崩溃并提示错误', async () => {
    mockListFeedback.mockRejectedValue(new Error('Network Error'))

    const wrapper = mountPage()
    await flushPromises()

    // 错误提示
    expect(errorSpy).toHaveBeenCalledWith('加载反馈列表失败')
    // 组件仍然存在且根节点还在
    expect(wrapper.find('[data-test="feedback-list-page"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('反馈管理')
    // 列表按空处理，且 loading 已复位（finally 生效）
    expect(wrapper.findComponent({ name: 'ElTable' }).props('data')).toEqual([])
    expect(wrapper.get('[data-test="feedback-refresh"]').classes()).not.toContain('is-loading')

    wrapper.unmount()
  })

  // ─── 附件缩略图 ────────────────────────────────────────────────

  it('有附件时按附着数量渲染 blob 缩略图，并支持点开大图', async () => {
    mockListFeedback.mockResolvedValue(listResponse([attachedItem]))

    const wrapper = mountPage()
    await flushAll()

    // 二进制接口要 JWT，必须走 axios+blob：每张附件各拉一次，且不重复
    expect(mockFetchFeedbackAttachmentBlob).toHaveBeenCalledTimes(2)
    expect(
      mockFetchFeedbackAttachmentBlob.mock.calls.map((call) => call[0]).sort(),
    ).toEqual(['att-1', 'att-2'])
    expect(errorSpy).not.toHaveBeenCalled()

    // 渲染出与附件数量一致的缩略图
    const thumbs = wrapper.findAll('[data-test="feedback-attachment-thumb"]')
    expect(thumbs).toHaveLength(2)
    expect(wrapper.findAll('[data-test="feedback-attachment-thumb-loading"]')).toHaveLength(0)

    const images = wrapper.findAllComponents({ name: 'ElImage' })
    expect(images).toHaveLength(2)
    const srcs = images.map((img) => String(img.props('src')))
    expect(srcs).toEqual(['blob:mock-1', 'blob:mock-2'])
    // 点开大图：preview-src-list 用的是同一批 blob URL，且 teleport 到 body 不被表格裁剪
    expect(images[0].props('previewSrcList')).toEqual(['blob:mock-1', 'blob:mock-2'])
    expect(images[1].props('initialIndex')).toBe(1)
    expect(images[0].props('previewTeleported')).toBe(true)

    wrapper.unmount()
  })

  it('本页没有任何附件时不渲染附件列，也不发附件请求', async () => {
    const wrapper = mountPage()
    await flushAll()

    expect(wrapper.find('.el-table__header').text()).not.toContain('附件')
    expect(wrapper.findAll('[data-test="feedback-attachment-thumb"]')).toHaveLength(0)
    expect(mockFetchFeedbackAttachmentBlob).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('缩略图加载失败降级为可点重试的占位（不弹全局错误），重试成功后可正常显示', async () => {
    let att2Attempts = 0
    mockFetchFeedbackAttachmentBlob.mockImplementation((id: unknown) => {
      if (id === 'att-1') {
        return Promise.resolve(new Blob([new Uint8Array(8)], { type: 'image/webp' }))
      }
      att2Attempts += 1
      return att2Attempts === 1
        ? Promise.reject(new Error('403 forbidden'))
        : Promise.resolve(new Blob([new Uint8Array(8)], { type: 'image/webp' }))
    })
    mockListFeedback.mockResolvedValue(listResponse([attachedItem]))

    const wrapper = mountPage()
    await flushAll()

    // 一张成功、一张失败
    expect(wrapper.findAll('[data-test="feedback-attachment-thumb"]')).toHaveLength(1)
    const failed = wrapper.findAll('[data-test="feedback-attachment-thumb-error"]')
    expect(failed).toHaveLength(1)
    // 一页几十张图，失败不能用全局 toast 刷屏——由单元格里的重试占位兜住
    expect(errorSpy).not.toHaveBeenCalled()

    await failed[0].trigger('click')
    await flushAll()

    expect(att2Attempts).toBe(2)
    expect(wrapper.findAll('[data-test="feedback-attachment-thumb"]')).toHaveLength(2)
    expect(wrapper.findAll('[data-test="feedback-attachment-thumb-error"]')).toHaveLength(0)

    wrapper.unmount()
  })

  it('详情弹窗展示附件图与元信息（文件名 · 宽×高 · 体积），点开走同一批 blob 大图', async () => {
    mockListFeedback.mockResolvedValue(listResponse([attachedItem]))

    const wrapper = mountPage()
    await flushAll()

    const detailBtn = wrapper.findAll('button').find((b) => b.text() === '查看详情')
    expect(detailBtn, '未找到「查看详情」按钮').toBeTruthy()
    await detailBtn!.trigger('click')
    await flushAll()

    // 详情里能看到图，而不是只有表格那一列
    const imgs = wrapper.findAll('[data-test="feedback-detail-attachment"]')
    expect(imgs).toHaveLength(2)
    const srcs = imgs.map((el) => el.find('img').attributes('src'))
    expect(srcs).toEqual(['blob:mock-1', 'blob:mock-2'])

    // 文件名与尺寸分开成行（合成一行会在数字中间折行，很难看），两项都要有
    expect(wrapper.findAll('.detail-attachment-name').map((m) => m.text())).toEqual([
      'att-1.webp',
      'att-2.webp',
    ])
    expect(wrapper.findAll('.detail-attachment-size').map((m) => m.text())).toEqual([
      '1600×900 · 1 KB',
      '1600×900 · 1 KB',
    ])

    // 详情里给出用户 ID（排障要定位到具体账号），取值来自后端 user_id
    expect(wrapper.get('[data-test="feedback-detail-user-id"]').text()).toBe(attachedItem.user_id)

    // 大图预览用的是同一批 URL，点击哪张看哪张
    const detailImages = wrapper
      .findAllComponents({ name: 'ElImage' })
      .filter((c) => c.classes().includes('detail-attachment-img'))
    expect(detailImages).toHaveLength(2)
    expect(detailImages[0].props('previewSrcList')).toEqual(['blob:mock-1', 'blob:mock-2'])
    expect(detailImages[1].props('initialIndex')).toBe(1)

    wrapper.unmount()
  })

  it('无附件的反馈：详情里不渲染附件行', async () => {
    mockListFeedback.mockResolvedValue(listResponse([suggestionItem]))

    const wrapper = mountPage()
    await flushAll()

    await wrapper.findAll('button').find((b) => b.text() === '查看详情')!.trigger('click')
    await flushAll()

    expect(wrapper.findAll('[data-test="feedback-detail-attachment"]')).toHaveLength(0)
    // 「附件」标签本身也不该出现（内容行里的文字不算）
    expect(wrapper.findAll('.detail-label').map((l) => l.text())).not.toContain('附件')

    wrapper.unmount()
  })

  it('详情里的图加载失败可原地重试，重试成功后显示图片', async () => {
    let attempts = 0
    mockFetchFeedbackAttachmentBlob.mockImplementation(() => {
      attempts += 1
      return attempts === 1
        ? Promise.reject(new Error('403 forbidden'))
        : Promise.resolve(new Blob([new Uint8Array(8)], { type: 'image/webp' }))
    })
    mockListFeedback.mockResolvedValue(listResponse([attachedItem]))

    const wrapper = mountPage()
    await flushAll()

    await wrapper.findAll('button').find((b) => b.text() === '查看详情')!.trigger('click')
    await flushAll()

    const failed = wrapper.findAll('[data-test="feedback-detail-attachment-error"]')
    expect(failed.length).toBeGreaterThan(0)

    await failed[0].trigger('click')
    await flushAll()

    expect(wrapper.findAll('[data-test="feedback-detail-attachment"]').length).toBeGreaterThan(0)
    expect(wrapper.findAll('[data-test="feedback-detail-attachment-error"]')).toHaveLength(0)
    // 失败不许用全局 toast 刷屏
    expect(errorSpy).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('预取还在飞时打开详情：每张附件只发一次请求（去重防重复 URL 泄漏）', async () => {
    let resolveAtt: ((blob: Blob) => void) | undefined
    const pending = new Promise<Blob>((resolve) => { resolveAtt = resolve })
    mockFetchFeedbackAttachmentBlob.mockImplementation((id: unknown) =>
      id === 'att-1' ? pending : Promise.resolve(new Blob([new Uint8Array(8)], { type: 'image/webp' })),
    )
    mockListFeedback.mockResolvedValue(listResponse([attachedItem]))

    const wrapper = mountPage()
    await flushAll()

    // att-1 仍在请求中（未 resolve），此时打开详情会触发兜底补拉
    await wrapper.findAll('button').find((b) => b.text() === '查看详情')!.trigger('click')
    await flushAll()

    const callsForAtt1 = mockFetchFeedbackAttachmentBlob.mock.calls.filter((c) => c[0] === 'att-1')
    expect(callsForAtt1).toHaveLength(1)

    resolveAtt!(new Blob([new Uint8Array(8)], { type: 'image/webp' }))
    await flushAll()

    // 只建了一个 URL：重复建会覆盖旧串且旧串永不 revoke
    expect(createObjectURL.mock.calls.length).toBe(2)
    expect(wrapper.findAll('[data-test="feedback-detail-attachment"]')).toHaveLength(2)

    wrapper.unmount()
  })

  it('页面卸载时释放全部缩略图 blob URL（防内存泄漏）', async () => {
    mockListFeedback.mockResolvedValue(listResponse([attachedItem]))

    const wrapper = mountPage()
    await flushAll()

    const urls = createObjectURL.mock.results.map((r) => r.value as string)
    expect(urls).toHaveLength(2)

    wrapper.unmount()

    expect(revokeObjectURL).toHaveBeenCalledTimes(2)
    expect(revokeObjectURL).toHaveBeenCalledWith(urls[0])
    expect(revokeObjectURL).toHaveBeenCalledWith(urls[1])
  })
})
