import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import FeedbackDialog from '../FeedbackDialog.vue'
import { useAuthStore } from '@/stores/auth'

const { submitFeedback, uploadFeedbackAttachment, compressImage, messageSuccess, messageWarning, messageError } =
  vi.hoisted(() => ({
    submitFeedback: vi.fn(),
    uploadFeedbackAttachment: vi.fn(),
    compressImage: vi.fn(),
    messageSuccess: vi.fn(),
    messageWarning: vi.fn(),
    messageError: vi.fn(),
  }))

// 不发真实请求：提交反馈 / 上传贴图都走接口替身
vi.mock('@/api/feedback', () => ({
  submitFeedback,
  uploadFeedbackAttachment,
}))

// 压缩依赖 createImageBitmap / canvas，jsdom 里没有；压缩算法本身在 image-compress.spec.ts 单测
vi.mock('@/utils/image-compress', () => ({
  compressImage,
}))

// 只保留 ElMessage 提示，模板里的 el-* 组件统一用下方 stubs 替身
vi.mock('element-plus', () => ({
  ElMessage: {
    success: messageSuccess,
    warning: messageWarning,
    error: messageError,
  },
}))

const stubs = {
  ElDialog: { template: '<section><slot /><slot name="footer" /></section>' },
  ElForm: { template: '<form><slot /></form>' },
  ElFormItem: { props: ['label'], template: '<label>{{ label }}<slot /></label>' },
  ElSelect: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template: '<div><slot /></div>',
  },
  ElOption: { props: ['label', 'value'], template: '<div>{{ label }}</div>' },
  ElInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template:
      '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
  ElButton: {
    props: ['loading', 'disabled'],
    template: '<button :disabled="disabled || loading"><slot /></button>',
  },
  // 只渲染触发器 + 暴露 onChange，真实选择文件由用例直接调用 onChange 模拟
  ElUpload: {
    props: ['onChange', 'accept', 'listType', 'autoUpload', 'showFileList', 'multiple'],
    template: '<div class="el-upload-stub"><slot /></div>',
  },
  // popconfirm 就近确认：reference 槽渲染「移除」按钮，确认按钮代替用户点确认
  ElPopconfirm: {
    props: ['title'],
    emits: ['confirm'],
    template:
      '<span class="el-popconfirm-stub"><slot name="reference" />' +
      '<button class="popconfirm-confirm" @click="$emit(\'confirm\')">confirm</button></span>',
  },
}

/** jsdom 未实现 createObjectURL，这里给唯一 URL 便于断言 revoke */
const createObjectURL = vi.fn()
const revokeObjectURL = vi.fn()
let objectUrlSeed = 0

function mountDialog() {
  const wrapper = mount(FeedbackDialog, {
    props: { modelValue: true },
    // 粘贴监听挂在 document 上（弹窗 append-to-body，焦点可能在标题栏），
    // 必须真的挂进文档树，事件冒泡才能走到 document——这也正是运行时的真实情况
    attachTo: document.body,
    global: { stubs },
  })
  mountedWrappers.push(wrapper)
  return wrapper
}

/** 用例内已手动 unmount 的也记着，afterEach 只兜底 unmount 还活着的，避免重复卸载告警，
 *  同时保证「断言失败提前退出」时不会把 document 上的粘贴监听泄漏给下一个用例 */
const mountedWrappers: ReturnType<typeof mountDialog>[] = []

/** 填一段合法内容，供「提交」相关用例复用 */
async function fillValidContent(wrapper: ReturnType<typeof mountDialog>, content = '希望支持按上下文长度筛选') {
  await wrapper.get('[data-test="feedback-content"]').setValue(content)
  return content
}

/** 造一张图片文件 */
function makeImageFile(name = 'screenshot.png', type = 'image/png'): File {
  return new File([new Uint8Array(64)], name, { type })
}

/**
 * 在弹窗内派发一次带图片的粘贴事件（真实的 ClipboardEvent 在 jsdom 里拿不到 clipboardData，
 * 这里用普通 Event + defineProperty 注入，事件仍走真实的 DOM 冒泡路径）。
 */
function pasteImages(wrapper: ReturnType<typeof mountDialog>, files: File[]) {
  const items = files.map((file) => ({
    kind: 'file',
    type: file.type,
    getAsFile: () => file,
  }))
  const event = new Event('paste', { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'clipboardData', { value: { items } })
  wrapper.get('[data-test="feedback-dialog"]').element.dispatchEvent(event)
  return event
}

/** 派发一次纯文本粘贴（不该被图片逻辑拦截） */
function pasteText(wrapper: ReturnType<typeof mountDialog>) {
  const items = [{ kind: 'string', type: 'text/plain', getAsFile: () => null }]
  const event = new Event('paste', { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'clipboardData', { value: { items } })
  wrapper.get('[data-test="feedback-dialog"]').element.dispatchEvent(event)
  return event
}

/** 等所有异步链路（compress → upload → 状态落库）跑完 */
async function flushAll() {
  await flushPromises()
  await flushPromises()
}

/** 上传成功返回体 */
function attachmentResult(id: string) {
  return {
    id,
    file_name: 'screenshot.webp',
    mime: 'image/webp',
    size_bytes: 64,
    width: 1600,
    height: 900,
    created_at: '2026-10-12T00:00:00Z',
  }
}

/** 给当前 Pinia 里的账号设置身份三项，模拟"已登录"（三者都来自 JWT） */
function setAccountIdentity(fields: { name?: string; email?: string; id?: string }) {
  useAuthStore().user = {
    name: fields.name ?? '',
    email: fields.email ?? '',
    id: fields.id ?? '',
  } as never
}

describe('FeedbackDialog 反馈弹窗', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // 组件通过 useAuthStore 读账号邮箱，必须先激活一个干净的 Pinia
    setActivePinia(createPinia())
    objectUrlSeed = 0
    createObjectURL.mockImplementation(() => `blob:mock-${(objectUrlSeed += 1)}`)
    ;(URL as unknown as Record<string, unknown>).createObjectURL = createObjectURL
    ;(URL as unknown as Record<string, unknown>).revokeObjectURL = revokeObjectURL

    submitFeedback.mockResolvedValue({ id: 'fb-1', status: 'pending', created_at: '2026-10-12T00:00:00Z' })
    uploadFeedbackAttachment.mockResolvedValue(attachmentResult('att-1'))
    // 压缩替身：原样透传文件，附带一个确定的尺寸，便于断言 width/height 表单字段
    compressImage.mockImplementation(async (file: File) => ({ file, width: 1600, height: 900 }))
  })

  afterEach(() => {
    // 兜底卸载：任何用例中途断言失败都不会把 document 上的粘贴监听泄漏到下一个用例
    mountedWrappers.splice(0).forEach((wrapper) => {
      const internal = (wrapper.vm as unknown as { $?: { isUnmounted?: boolean } } | undefined)?.$
      if (!internal?.isUnmounted) wrapper.unmount()
    })
    // 清掉 el-dialog 可能的 teleport 残留
    document.body.innerHTML = ''
  })


  it('初始渲染出类型、内容、只读提交身份与提交按钮', () => {
    setAccountIdentity({ name: '张三', email: 'zhangsan@corp.com', id: 'u-123' })
    const wrapper = mountDialog()

    expect(wrapper.find('[data-test="feedback-dialog"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="feedback-type"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="feedback-content"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="feedback-identity"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="feedback-submit"]').exists()).toBe(true)
    // 身份只读：对话框里不该再有联系方式的输入框
    expect(wrapper.find('[data-test="feedback-contact"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('内容为空或不足 5 字时给出警告且不提交', async () => {
    const wrapper = mountDialog()
    const submit = wrapper.get('[data-test="feedback-submit"]')

    await submit.trigger('click')
    expect(submitFeedback).not.toHaveBeenCalled()
    expect(messageWarning).toHaveBeenCalledWith('反馈内容至少 5 个字，请补充说明')

    await wrapper.get('[data-test="feedback-content"]').setValue('太短')
    await submit.trigger('click')
    expect(submitFeedback).not.toHaveBeenCalled()
    expect(messageWarning).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('内容合法时只提交 type/content（身份由后端从 token 落库，前端不传）', async () => {
    setAccountIdentity({ name: '张三', email: 'me@example.com', id: 'u-123' })
    const wrapper = mountDialog()
    const content = await fillValidContent(wrapper)

    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushPromises()

    expect(submitFeedback).toHaveBeenCalledTimes(1)
    const payload = submitFeedback.mock.calls[0][0]
    expect(payload.type).toBe('suggestion')
    expect(payload.content).toBe(content)
    // 前端不再传任何身份字段：user_name / user_email / user_id 由后端从 token 取，
    // 传第二份只会产生可能不一致的数据，也给不出"替别人提交"的口子
    expect(payload.contact).toBeUndefined()
    expect(payload.user_email).toBeUndefined()
    expect(payload.user_name).toBeUndefined()
    expect(payload.user_id).toBeUndefined()
    wrapper.unmount()
  })



  it('提交成功后 emit update:modelValue 为 false', async () => {
    const wrapper = mountDialog()
    await fillValidContent(wrapper)

    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushPromises()

    expect(messageSuccess).toHaveBeenCalledWith('反馈已提交，感谢你的反馈')
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
    wrapper.unmount()
  })

  it('提交身份只读回显用户名 + 邮箱，且不展示用户 ID', async () => {
    setAccountIdentity({ name: '张三', email: 'zhangsan@corp.com', id: '1a2b3c4d-5e6f-7890-abcd-ef1234567890' })
    const wrapper = mountDialog()

    expect(wrapper.get('[data-test="feedback-identity-name"]').text()).toBe('张三')
    expect(wrapper.get('[data-test="feedback-identity-email"]').text()).toBe('zhangsan@corp.com')
    // 用户 ID 对提交人没有意义，不展示（排障用的 ID 在管理端详情里）
    expect(wrapper.find('[data-test="feedback-identity-id"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="feedback-identity"]').text()).not.toContain('1a2b3c4d')
    // 只读：不存在任何身份输入框
    expect(wrapper.find('[data-test="feedback-contact"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="feedback-identity"] input').exists()).toBe(false)

    await fillValidContent(wrapper)
    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushPromises()
    expect(submitFeedback.mock.calls[0][0].contact).toBeUndefined()
    wrapper.unmount()
  })

  it('账号缺少用户名或邮箱时给出占位文案，仍不出现输入框', async () => {
    // 只给 ID，模拟 SSO 未下发 name/email 的账号
    setAccountIdentity({ id: 'u-only' })
    const wrapper = mountDialog()

    expect(wrapper.get('[data-test="feedback-identity-name"]').text()).toBe('（未提供用户名）')
    expect(wrapper.get('[data-test="feedback-identity-email"]').text()).toBe('（未提供邮箱）')
    expect(wrapper.find('[data-test="feedback-contact"]').exists()).toBe(false)

    await fillValidContent(wrapper)
    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushPromises()
    // 没有邮箱时也不让前端补联系方式：身份始终由后端从 token 落库
    expect(submitFeedback.mock.calls[0][0].contact).toBeUndefined()
    wrapper.unmount()
  })

  it('无邮箱时提示语改为"通过你的账号联系你"，不再引导填写联系方式', async () => {
    setAccountIdentity({ name: '李四', id: 'u-2' })
    const wrapper = mountDialog()

    const tip = wrapper.get('.feedback-tip').text()
    expect(tip).toContain('通过你的账号联系你')
    expect(tip).not.toContain('留下联系方式')
    wrapper.unmount()
  })

  it('有邮箱时提示语写明回复去处', async () => {
    setAccountIdentity({ name: '李四', email: 'lisi@corp.com', id: 'u-2' })
    const wrapper = mountDialog()

    const tip = wrapper.get('.feedback-tip').text()
    expect(tip).toContain('lisi@corp.com')
    expect(tip).not.toContain('留下联系方式')
    wrapper.unmount()
  })

  it('提交失败时报错、不关闭弹窗且保留已填内容', async () => {
    submitFeedback.mockRejectedValue(new Error('network error'))
    const wrapper = mountDialog()
    const content = await fillValidContent(wrapper)

    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushPromises()

    expect(messageError).toHaveBeenCalledWith('反馈提交失败，请稍后重试')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.find('[data-test="feedback-dialog"]').exists()).toBe(true)
    expect((wrapper.get('[data-test="feedback-content"]').element as HTMLTextAreaElement).value).toBe(content)
    wrapper.unmount()
  })

  it('提交中禁用提交按钮，重复点击不会二次提交', async () => {
    let resolveSubmit!: (value: unknown) => void
    submitFeedback.mockImplementation(
      () => new Promise((resolve) => { resolveSubmit = resolve }),
    )
    const wrapper = mountDialog()
    await fillValidContent(wrapper)

    const submit = wrapper.get('[data-test="feedback-submit"]')
    await submit.trigger('click')
    expect(submit.attributes('disabled')).toBeDefined()

    await submit.trigger('click')
    expect(submitFeedback).toHaveBeenCalledTimes(1)

    resolveSubmit({ id: 'fb-1', status: 'pending', created_at: '2026-10-12T00:00:00Z' })
    await flushPromises()
    wrapper.unmount()
  })

  // ─── 贴图能力 ─────────────────────────────────────────────────

  it('粘贴一张截图 → 触发上传（带压缩尺寸）→ 提交载荷带 attachment_ids', async () => {
    const wrapper = mountDialog()
    const image = makeImageFile()

    const event = pasteImages(wrapper, [image])
    await flushAll()

    // 命中图片：拦下默认粘贴行为，并真的走了压缩 + 上传
    expect(event.defaultPrevented).toBe(true)
    expect(compressImage).toHaveBeenCalledTimes(1)
    expect(compressImage.mock.calls[0][0]).toBe(image)
    expect(uploadFeedbackAttachment).toHaveBeenCalledTimes(1)
    expect(uploadFeedbackAttachment.mock.calls[0][0]).toBe(image)
    expect(uploadFeedbackAttachment.mock.calls[0][1]).toEqual({ width: 1600, height: 900 })
    expect(wrapper.find('[data-test="feedback-attachment-done"]').exists()).toBe(true)

    await fillValidContent(wrapper)
    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushAll()

    expect(submitFeedback).toHaveBeenCalledTimes(1)
    expect(submitFeedback.mock.calls[0][0].attachment_ids).toEqual(['att-1'])
    wrapper.unmount()
  })

  it('粘贴纯文本不触发图片上传，也不拦截默认行为', async () => {
    const wrapper = mountDialog()

    const event = pasteText(wrapper)
    await flushAll()

    expect(event.defaultPrevented).toBe(false)
    expect(compressImage).not.toHaveBeenCalled()
    expect(uploadFeedbackAttachment).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="feedback-attachments"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('点选文件上传走同一条链路，并释放 el-upload 内部自建的 object URL', async () => {
    const wrapper = mountDialog()
    const image = makeImageFile('picked.png')
    const onChange = wrapper.findComponent(stubs.ElUpload).props('onChange') as (
      file: { raw?: File; url?: string },
    ) => void

    // el-upload picture-card 模式会给每个文件挂一个内部 blob URL
    onChange({ raw: image, url: 'blob:el-upload-internal' })
    await flushAll()

    expect(compressImage.mock.calls[0][0]).toBe(image)
    expect(uploadFeedbackAttachment).toHaveBeenCalledTimes(1)
    expect(uploadFeedbackAttachment.mock.calls[0][0]).toBe(image)
    // 内部那份 URL 要被释放，否则常驻弹窗里会越积越多
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:el-upload-internal')
    wrapper.unmount()
  })

  it('超过 3 张时拦下第 4 张并提示，只上传 3 张', async () => {
    const wrapper = mountDialog()

    for (let i = 0; i < 4; i += 1) {
      pasteImages(wrapper, [makeImageFile(`shot-${i}.png`)])
      await flushAll()
    }

    expect(uploadFeedbackAttachment).toHaveBeenCalledTimes(3)
    expect(messageWarning).toHaveBeenCalledWith('最多只能上传 3 张图片')
    expect(wrapper.findAll('[data-test="feedback-attachment-done"]')).toHaveLength(3)
    wrapper.unmount()
  })

  it('类型不在白名单时提示并跳过，不发起上传', async () => {
    const wrapper = mountDialog()

    // 剪贴板里确实是图片（image/bmp），但不在 png|jpeg|webp|gif 白名单内
    pasteImages(wrapper, [makeImageFile('shot.bmp', 'image/bmp')])
    await flushAll()

    expect(compressImage).not.toHaveBeenCalled()
    expect(uploadFeedbackAttachment).not.toHaveBeenCalled()
    expect(messageError).toHaveBeenCalledWith('仅支持 PNG / JPEG / WebP / GIF 格式的图片')
    wrapper.unmount()
  })

  it('上传失败时给出错误提示、标记失败态，并拦下提交（不静默丢图）', async () => {
    uploadFeedbackAttachment.mockRejectedValue(new Error('boom'))
    const wrapper = mountDialog()

    pasteImages(wrapper, [makeImageFile()])
    await flushAll()

    expect(uploadFeedbackAttachment).toHaveBeenCalledTimes(1)
    expect(messageError).toHaveBeenCalledWith('图片上传失败，请重试或移除该图片')
    expect(wrapper.find('[data-test="feedback-attachment-error"]').exists()).toBe(true)

    await fillValidContent(wrapper)
    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushAll()

    expect(submitFeedback).not.toHaveBeenCalled()
    expect(messageError).toHaveBeenCalledWith('有图片上传失败，请重试或移除后再提交')
    wrapper.unmount()
  })

  it('图片还在上传中时拦下提交，上传完成后可正常提交', async () => {
    let resolveUpload!: (value: unknown) => void
    uploadFeedbackAttachment.mockImplementation(
      () => new Promise((resolve) => { resolveUpload = resolve }),
    )
    const wrapper = mountDialog()

    pasteImages(wrapper, [makeImageFile()])
    await flushAll()
    expect(wrapper.find('[data-test="feedback-attachment-uploading"]').exists()).toBe(true)

    await fillValidContent(wrapper)
    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushAll()

    expect(submitFeedback).not.toHaveBeenCalled()
    expect(messageWarning).toHaveBeenCalledWith('图片正在上传，请稍候再提交')

    resolveUpload(attachmentResult('att-9'))
    await flushAll()

    await wrapper.get('[data-test="feedback-submit"]').trigger('click')
    await flushAll()

    expect(submitFeedback).toHaveBeenCalledTimes(1)
    expect(submitFeedback.mock.calls[0][0].attachment_ids).toEqual(['att-9'])
    wrapper.unmount()
  })

  it('上传失败后可点「重试」重新上传，重试成功后进入可提交状态', async () => {
    uploadFeedbackAttachment.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountDialog()

    pasteImages(wrapper, [makeImageFile()])
    await flushAll()
    expect(wrapper.find('[data-test="feedback-attachment-error"]').exists()).toBe(true)

    await wrapper.get('[data-test="feedback-attachment-retry"]').trigger('click')
    await flushAll()

    expect(uploadFeedbackAttachment).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-test="feedback-attachment-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="feedback-attachment-done"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('移除图片需确认，确认后释放对应 blob URL；关闭弹窗清空并释放全部 blob URL', async () => {
    const wrapper = mountDialog()

    pasteImages(wrapper, [makeImageFile()])
    await flushAll()
    const firstUrl = createObjectURL.mock.results[0].value as string

    await wrapper.get('.popconfirm-confirm').trigger('click')
    await flushAll()

    expect(wrapper.find('[data-test="feedback-attachments"]').exists()).toBe(false)
    expect(revokeObjectURL).toHaveBeenCalledWith(firstUrl)

    // 再贴一张，关闭弹窗时也要清空 + 释放
    pasteImages(wrapper, [makeImageFile('second.png')])
    await flushAll()
    const secondUrl = createObjectURL.mock.results[1].value as string

    await wrapper.setProps({ modelValue: false })
    await flushAll()

    expect(revokeObjectURL).toHaveBeenCalledWith(secondUrl)
    expect(wrapper.find('[data-test="feedback-attachments"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

// ─────────────────────────────────────────────────────────────────
// API 层契约：组件用例里 @/api/feedback 被整体替身了，这里用 importActual 拿**真实实现**，
// 只把 http client 换成替身，专门盯住后端冻结契约（multipart 文件字段必须叫 file、
// 字节接口必须 responseType: 'blob'）——这两点写错前端不会有任何编译期报错。
vi.mock('@/services/http', () => ({
  default: { post: vi.fn(), get: vi.fn() },
}))

describe('feedback API 冻结契约', () => {
  // 外层 beforeEach 只作用于上面那个 describe，这里自己清一次调用记录
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uploadFeedbackAttachment 走 multipart，文件字段名为 file 且带 width/height', async () => {
    const http = (await import('@/services/http')).default as unknown as {
      post: ReturnType<typeof vi.fn>
    }
    const real = await vi.importActual<typeof import('@/api/feedback')>('@/api/feedback')
    http.post.mockResolvedValue({ data: { data: { id: 'att-1', file_name: 'a.webp' } } })
    const file = new File(['x'], 'a.webp', { type: 'image/webp' })

    const result = await real.uploadFeedbackAttachment(file, { width: 1600, height: 900 })

    expect(http.post).toHaveBeenCalledTimes(1)
    const [url, body, config] = http.post.mock.calls[0] as [string, FormData, { headers: Record<string, string> }]
    expect(url).toBe('/api/v1/feedback/attachments')
    expect(body).toBeInstanceOf(FormData)
    expect(body.get('file')).toBeInstanceOf(File)
    expect((body.get('file') as File).name).toBe('a.webp')
    expect(body.get('width')).toBe('1600')
    expect(body.get('height')).toBe('900')
    expect(config.headers['Content-Type']).toBe('multipart/form-data')
    expect(result.id).toBe('att-1')
  })

  it('尺寸未知时不提交 width/height，避免给后端塞 0', async () => {
    const http = (await import('@/services/http')).default as unknown as {
      post: ReturnType<typeof vi.fn>
    }
    const real = await vi.importActual<typeof import('@/api/feedback')>('@/api/feedback')
    http.post.mockResolvedValue({ data: { id: 'att-2' } })

    const result = await real.uploadFeedbackAttachment(new File(['x'], 'b.png', { type: 'image/png' }), {
      width: 0,
      height: 0,
    })

    const body = http.post.mock.calls[0][1] as FormData
    expect(body.get('width')).toBeNull()
    expect(body.get('height')).toBeNull()
    expect(result.id).toBe('att-2')
  })

  it('fetchFeedbackAttachmentBlob 以 responseType=blob 读字节', async () => {
    const http = (await import('@/services/http')).default as unknown as {
      get: ReturnType<typeof vi.fn>
    }
    const real = await vi.importActual<typeof import('@/api/feedback')>('@/api/feedback')
    const blob = new Blob(['img'], { type: 'image/webp' })
    http.get.mockResolvedValue({ data: blob })

    const result = await real.fetchFeedbackAttachmentBlob('att-1')

    expect(http.get).toHaveBeenCalledWith('/api/v1/feedback/attachments/att-1', {
      responseType: 'blob',
    })
    expect(result).toBe(blob)
  })
})
