import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import ManualPage from '../ManualPage.vue'
import { MANUAL_CHAPTERS, MANUAL_FAQS, MANUAL_META, MANUAL_TOC } from '../manual-data'
import type { ManualStep } from '../manual-data'

// 手册页只用登录态决定吸顶偏移，这里固定为未登录，避免依赖真实 store
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isLoggedIn: false }),
}))

// 反馈弹窗依赖 element-plus 的 el-dialog（会 teleport 到 body，不在组件子树内），
// 本用例只验证手册页到弹窗的接线，故用最小替身：modelValue 为真时渲染出 data-test 标记
vi.mock('@/components/common/FeedbackDialog.vue', () => ({
  default: {
    name: 'FeedbackDialog',
    props: ['modelValue'],
    template: '<div v-if="modelValue" data-test="feedback-dialog" />',
  },
}))

/**
 * 暗色模式防回归：直接读样式源文件文本。
 * 手册页的颜色必须全部走 Element Plus 变量，任何硬编码白色都会让 html.dark 下整页变白板。
 */
const MANUAL_CSS = readFileSync(
  // jsdom 环境下的 URL 会把相对路径解析到 http://localhost:3000，故这里用 Node 的 fileURLToPath
  resolve(dirname(fileURLToPath(import.meta.url)), '../manual.css'),
  'utf8',
)

/** 手册正文里渲染出的全部插图：章内直属步骤 + 各小节步骤，每步 figure 在前、duo 在后 */
function allFigures() {
  const steps: ManualStep[] = MANUAL_CHAPTERS.flatMap((chapter) => [
    ...chapter.steps,
    ...chapter.subs.flatMap((sub) => sub.steps),
  ])
  return steps.flatMap((step) => [...step.figures, ...(step.duo ?? [])])
}

/** 造一个 DOMRect 替身，让锚点滚动的目标位置可断言 */
function rect(top: number): DOMRect {
  return {
    top,
    bottom: top,
    left: 0,
    right: 0,
    width: 0,
    height: 0,
    x: 0,
    y: top,
    toJSON: () => ({}),
  } as DOMRect
}

// 每次挂载都放进一个真实的 .main-content 宿主里：手册页的滚动容器是 App.vue 的 .main-content，
// 不给它就没有可断言的滚动容器（会回退 window）。
let host: HTMLElement | null = null

function mountManual(): VueWrapper {
  host = document.createElement('div')
  host.className = 'main-content'
  document.body.appendChild(host)
  // 装上 pinia 让 getActivePinia() 能拿到实例（auth store 已 mock，仅用于消除 injection 警告）
  return mount(ManualPage, {
    global: { plugins: [createPinia()] },
    attachTo: host,
  })
}

describe('ManualPage 使用手册页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    host?.remove()
    host = null
  })

  it('挂载不报错，根元素带 .manual-page 与 data-test="manual-page"', () => {
    const wrapper = mountManual()

    const root = wrapper.get('[data-test="manual-page"]')
    expect(root.classes()).toContain('manual-page')
    expect(root.attributes('data-test')).toBe('manual-page')
    wrapper.unmount()
  })

  it('页面头压扁成标题 + meta 行，且已移除页内吸顶导航条与 hero 四张快捷卡', () => {
    const wrapper = mountManual()

    // 独立全屏页的外壳（页内吸顶导航条 / eyebrow 药丸 / 四张快捷卡）必须不再存在
    expect(wrapper.find('.topbar').exists()).toBe(false)
    expect(wrapper.find('.eyebrow').exists()).toBe(false)
    expect(wrapper.find('.qn').exists()).toBe(false)

    expect(wrapper.get('h1').text()).toBe('使用手册')

    // 原 hero 的 5 项信息压成页头一行，取值来自 MANUAL_META，不是模板里硬编码的副本
    const meta = wrapper.get('.manual-meta').text()
    // eyebrow 已拆成两个 chip，内容仍来自 MANUAL_META 的语义（不是硬编码副本）
    expect(MANUAL_META.eyebrow).toContain('面向全体员工')
    expect(meta).toContain('面向全体员工')
    expect(meta).toContain(MANUAL_META.version)
    expect(meta).toContain(MANUAL_META.updated)
    expect(meta).toContain(MANUAL_META.loginUrl)
    expect(meta).toContain(MANUAL_META.baseUrl)
    expect(wrapper.get('.manual-lede').text()).toBe(MANUAL_META.lede)
    // 每项一个原子 chip：窄屏折行时按整块排，不再断成散乱灰字
    expect(wrapper.findAll('.manual-meta-item').length).toBeGreaterThanOrEqual(5)

    // 导航只剩左侧目录一份；反馈 CTA 也只有一个（在页面头）
    expect(wrapper.findAll('[data-test="manual-toc"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-test="manual-feedback-cta"]')).toHaveLength(1)

    // 文档头必须属于正文列（.manual-main 的子节点），而不是 .manual-body 的兄弟/子项：
    // 只有这样左侧目录才能与标题同起一行；一旦挪回通栏，目录又会被压到介绍块下面。
    expect(wrapper.find('.manual-main .manual-head').exists()).toBe(true)
    expect(wrapper.find('.manual-body > .manual-head').exists()).toBe(false)
    expect(wrapper.find('.manual-body > .manual-toc').exists()).toBe(true)

    wrapper.unmount()
  })

  it('目录 8 项，点第 6 项让滚动容器滚到锚点并让开 SPA 顶栏', async () => {
    const wrapper = mountManual()
    const scroller = wrapper.get('[data-test="manual-page"]').element.closest('.main-content')
    expect(scroller).toBe(host)

    const scrollTo = vi.fn()
    ;(scroller as HTMLElement).scrollTo = scrollTo

    const items = wrapper.findAll('[data-test="manual-toc"] li')
    expect(items).toHaveLength(8)
    expect(items).toHaveLength(MANUAL_TOC.length)

    // 目标元素落在容器可视区下方 500px，容器顶边在 0：
    // 未登录时 stickyTop = 0，预期 top = 500 - 0 - 16 = 484
    wrapper.get('#budget').element.getBoundingClientRect = () => rect(500)
    ;(scroller as HTMLElement).getBoundingClientRect = () => rect(0)

    // onMounted 是 async：等它把 .main-content 解析成滚动容器之后再点，否则会回退到 window
    await flushPromises()

    await wrapper.findAll('[data-test="manual-toc"] a')[5].trigger('click')

    expect(scrollTo).toHaveBeenCalledTimes(1)
    expect(scrollTo).toHaveBeenCalledWith({ top: 484, behavior: 'smooth' })

    wrapper.unmount()
  })

  it('渲染出每一章的标题与全部 7 条 FAQ 问题', () => {
    const wrapper = mountManual()

    const headings = wrapper.findAll('section.chap h2').map((node) => node.text())
    for (const chapter of MANUAL_CHAPTERS) {
      expect(headings).toContain(chapter.title)
    }

    const faqItems = wrapper.findAll('details.q')
    expect(faqItems).toHaveLength(MANUAL_FAQS.length)
    expect(MANUAL_FAQS).toHaveLength(7)
    const text = wrapper.text()
    for (const faq of MANUAL_FAQS) {
      expect(text).toContain(faq.q)
    }

    wrapper.unmount()
  })

  it('渲染出的图片数量与 manual-data 中 figure 总数一致，且 src 指向 /manual/assets/*.webp', () => {
    const wrapper = mountManual()

    const figures = allFigures()
    const images = wrapper.findAll('img')
    expect(images).toHaveLength(figures.length)
    expect(images.length).toBeGreaterThan(0)

    for (const img of images) {
      const src = img.attributes('src') ?? ''
      expect(src.startsWith('/manual/assets/')).toBe(true)
      expect(src.endsWith('.webp')).toBe(true)
    }

    wrapper.unmount()
  })

  it('点图卡的「放大」打开灯箱，← 循环到最后一张，按 Esc 关闭', async () => {
    const wrapper = mountManual()
    const figures = allFigures()
    const first = figures[0]
    const last = figures[figures.length - 1]

    expect(wrapper.find('[data-test="manual-lightbox"]').exists()).toBe(false)

    await wrapper.get('[data-test="manual-shot-zoom"]').trigger('click')

    const lightbox = wrapper.get('[data-test="manual-lightbox"]')
    expect(lightbox.get('img').attributes('src')).toBe(`/manual/assets/${first.name}.webp`)

    // 首张按 ← 应循环到最后一张
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft' }))
    await flushPromises()
    expect(wrapper.get('[data-test="manual-lightbox"]').get('img').attributes('src')).toBe(
      `/manual/assets/${last.name}.webp`,
    )

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    expect(wrapper.find('[data-test="manual-lightbox"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('点页面头的「提交反馈」打开反馈弹窗', async () => {
    const wrapper = mountManual()

    expect(wrapper.find('[data-test="feedback-dialog"]').exists()).toBe(false)

    await wrapper.get('[data-test="manual-feedback-cta"]').trigger('click')

    expect(wrapper.find('[data-test="feedback-dialog"]').exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'FeedbackDialog' }).props('modelValue')).toBe(true)
    wrapper.unmount()
  })

  it('卸载不抛错且移除了滚动容器上的 scroll 监听与 window 上的 resize 监听', async () => {
    const addSpy = vi.spyOn(window, 'addEventListener')
    const removeSpy = vi.spyOn(window, 'removeEventListener')
    const wrapper = mountManual()
    const scroller = host as HTMLElement
    const hostAdd = vi.spyOn(scroller, 'addEventListener')
    const hostRemove = vi.spyOn(scroller, 'removeEventListener')
    // onMounted 是 async，等它绑完监听再卸载，否则测不到移除行为
    await flushPromises()

    expect(hostAdd).toHaveBeenCalledWith('scroll', expect.any(Function), { passive: true })
    expect(addSpy).toHaveBeenCalledWith('resize', expect.any(Function))

    expect(() => wrapper.unmount()).not.toThrow()
    expect(hostRemove).toHaveBeenCalledWith('scroll', expect.any(Function))
    expect(removeSpy).toHaveBeenCalledWith('resize', expect.any(Function))

    addSpy.mockRestore()
    removeSpy.mockRestore()
    hostAdd.mockRestore()
    hostRemove.mockRestore()
  })

  it('manual.css 不含硬编码白色背景（暗色模式防回归）', () => {
    expect(MANUAL_CSS.length).toBeGreaterThan(0)

    // 硬编码白底是暗色模式变白板的直接原因：这两条必须保持不匹配
    expect(MANUAL_CSS).not.toMatch(/background[^;]*#fff/i)
    expect(MANUAL_CSS).not.toMatch(/rgb\(255,\s*255,\s*255\)/i)
    // 更宽的兜底：整个样式表里不允许出现任何白色字面量
    expect(MANUAL_CSS).not.toMatch(/#fff/i)
    expect(MANUAL_CSS).not.toMatch(/rgba?\(\s*255\s*,\s*255\s*,\s*255/i)

    // 正向对照：颜色确实走的是 Element Plus 变量，而不是把断言写成恒真
    expect(MANUAL_CSS).toMatch(/var\(--el-bg-color\)/)
    expect(MANUAL_CSS).toMatch(/var\(--el-text-color-primary\)/)
  })
})
