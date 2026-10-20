<template>
  <div
    ref="rootEl"
    class="manual-page"
    :class="{ 'manual-page--logged-in': isLoggedIn }"
    :style="{ '--manual-sticky-top': stickyTop }"
    data-test="manual-page"
  >
    <!-- 阅读进度条：sticky 2px，吸在 SPA 顶栏之下（页内吸顶导航条已删除，上方只剩这一条） -->
    <div class="manual-progress" aria-hidden="true"><i :style="{ width: progress + '%' }" /></div>

    <div class="manual-shell">
      <div class="manual-body">
        <!-- 左侧目录：整个页面唯一一份导航（页内吸顶条与 hero 快捷卡已删除）。
             放在 grid 第一列，与右侧正文的文档头同起一行——不再被通栏页头压到下面 -->
        <aside class="manual-toc" data-test="manual-toc">
          <h5>目录</h5>
          <ol>
            <li v-for="item in MANUAL_TOC" :key="item.id" :class="{ t2: item.level === 2 }">
              <a
                role="button"
                tabindex="0"
                :class="{ active: activeId === item.id }"
                @click.prevent="scrollToId(item.id)"
                @keydown.enter.prevent="scrollToId(item.id)"
                @keydown.space.prevent="scrollToId(item.id)"
              >{{ item.label }}</a>
            </li>
          </ol>
        </aside>

        <main class="manual-main">
          <!-- 文档头属于正文列的一部分（不是通栏页头）：标题 + 元信息一行 + 引导语，
               这样左侧目录能从页面顶部开始，而不是被压到介绍块下面。
               类名沿用全站页面骨架约定（.page-header / .page-title / .header-actions） -->
          <header class="page-header manual-head">
            <div class="manual-head-row">
              <h1 class="page-title">使用手册</h1>
              <div class="header-actions manual-head-actions">
                <button
                  type="button"
                  class="manual-feedback-btn"
                  data-test="manual-feedback-cta"
                  @click="feedbackVisible = true"
                >
                  提交反馈
                </button>
              </div>
            </div>

            <!-- 元信息一行：原 hero 的 5 项信息一条不丢，但不再各占一行/一块。
                 取值全部来自 MANUAL_META，模板里不写死副本 -->
            <p class="manual-meta">
              <span class="manual-meta-item">面向全体员工</span>
              <span class="manual-meta-item">约 10 分钟读完</span>
              <span class="manual-meta-item">版本 {{ MANUAL_META.version }}</span>
              <span class="manual-meta-item">更新于 {{ MANUAL_META.updated }}</span>
              <span class="manual-meta-item">登录地址 <code>{{ MANUAL_META.loginUrl }}</code></span>
              <span class="manual-meta-item">API Base URL <code>{{ MANUAL_META.baseUrl }}</code></span>
            </p>

            <p class="manual-lede">{{ MANUAL_META.lede }}</p>
          </header>

          <!-- 正文 4 章：带 subs 的章按小节拆成多个「步骤块」，否则整章一块，模板只写一份 steps 结构 -->
          <section v-for="chapter in MANUAL_CHAPTERS" :id="chapter.id" :key="chapter.id" class="chap rv">
            <div class="chap-head">
              <span class="chap-no">{{ chapter.no }}</span>
              <h2>{{ chapter.title }}</h2>
              <!-- 章节导语是第一方静态内容（manual-data.ts 逐字取自源 HTML，含 span.ui 内联标记），故用 v-html -->
              <p v-html="chapter.intro" />
            </div>

            <div
              v-for="(block, bi) in chapterBlocks(chapter)"
              :id="block.id"
              :key="block.id ?? bi"
              :class="block.title ? 'sub rv' : ''"
            >
              <h3 v-if="block.title">{{ block.title }}</h3>
              <ol class="steps">
                <li v-for="(step, si) in block.steps" :key="si" class="step">
                  <h4>{{ step.title }}</h4>
                  <!-- 段落同样是第一方静态内容（em/strong/code/span.ui 内联标记），故用 v-html -->
                  <p v-for="(para, pi) in step.body" :key="pi" v-html="para" />

                  <ManualShot
                    v-for="figure in step.figures"
                    :key="figure.name"
                    :figure="figure"
                    @zoom="openLightbox"
                  />

                  <!-- 源 div.duo：两张并排截图，窄屏退化成单列 -->
                  <div v-if="step.duo?.length" class="duo">
                    <ManualShot
                      v-for="figure in step.duo"
                      :key="figure.name"
                      :figure="figure"
                      @zoom="openLightbox"
                    />
                  </div>

                  <div v-if="step.tip" class="note tip">
                    <svg
                      class="n-i"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="1.8"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    >
                      <circle cx="12" cy="12" r="9" />
                      <path d="M12 16v-5" />
                      <path d="M12 8h.01" />
                    </svg>
                    <span v-html="step.tip" />
                  </div>
                </li>
              </ol>
            </div>
          </section>

          <!-- 常见问题：源里也是一章（章头「附」/「常见问题」），契约中 MANUAL_FAQS 只有 q/a，章头按 manual-data.ts 注释还原 -->
          <section id="faq" class="chap rv">
            <div class="chap-head">
              <span class="chap-no">附</span>
              <h2>常见问题</h2>
              <p>用顺手之后最常被问到的几个。</p>
            </div>
            <div class="faq">
              <details v-for="(item, fi) in MANUAL_FAQS" :key="fi" class="q">
                <summary>{{ item.q }}</summary>
                <div class="a">
                  <p v-for="(para, pi) in item.a" :key="pi" v-html="para" />
                </div>
              </details>
            </div>
          </section>

          <!-- 正文末尾的反馈入口（与页面头那颗按钮同一接线，不再单独占用 data-test） -->
          <div class="manual-cta rv">
            <p>手册没覆盖到的问题，可以直接提交给模型网关维护团队。</p>
            <button type="button" class="manual-feedback-btn" @click="feedbackVisible = true">
              没找到答案？提交反馈
            </button>
          </div>
        </main>
      </div>

      <footer class="manual-foot">
        <p>模型网关 · 员工使用手册 {{ MANUAL_META.version }} · {{ MANUAL_META.updated }}</p>
        <div class="r">
          <a role="button" tabindex="0" @click="scrollToId('login')" @keydown.enter.prevent="scrollToId('login')">回到开头</a>
          <a role="button" tabindex="0" @click="scrollToId('faq')" @keydown.enter.prevent="scrollToId('faq')">常见问题</a>
          <a :href="loginHref" target="_blank" rel="noopener">打开网关</a>
        </div>
      </footer>
    </div>

    <!-- 回到顶部 -->
    <button type="button" class="totop" :class="{ on: totopOn }" aria-label="回到顶部" @click="scrollToTop">
      <svg
        viewBox="0 0 24 24"
        width="17"
        height="17"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <path d="M12 19V5" />
        <path d="M5 12l7-7 7 7" />
      </svg>
    </button>

    <ManualLightbox
      v-if="lightboxShots.length"
      :shots="lightboxShots"
      :index="lightboxIndex"
      @close="closeLightbox"
      @update:index="lightboxIndex = $event"
    />

    <FeedbackDialog v-model="feedbackVisible" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { getActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import FeedbackDialog from '@/components/common/FeedbackDialog.vue'
import ManualShot from './components/ManualShot.vue'
import ManualLightbox from './components/ManualLightbox.vue'
import type { ManualChapter, ManualFigure, ManualStep } from './manual-data'
import { MANUAL_CHAPTERS, MANUAL_FAQS, MANUAL_META, MANUAL_TOC } from './manual-data'
// 手册样式是整段迁移的静态表，作用域已收敛在 .manual-page 之下
import './manual.css'

/** SPA 顶栏高度：登录态下 .top-bar 是 sticky 52px，会盖住内容区顶部，吸顶元素要让开这一段 */
const STICKY_TOP_LOGGED_IN = 52
/** 回到顶部按钮出现阈值（px） */
const TOTOP_SHOW_AT = 600
/** 锚点跳转后，目标标题与吸顶区之间保留的呼吸间距（px） */
const ANCHOR_GAP = 16

// Pinia 已安装时读用户登录态；单独挂载本组件（没有 Pinia 实例）时按未登录处理，不直接抛错
const authStore = getActivePinia() ? useAuthStore() : null
const isLoggedIn = computed(() => authStore?.isLoggedIn === true)

/** 供 manual.css 里的 sticky 偏移使用：登录态 52px，未登录 0px */
const stickyTopPx = computed(() => (isLoggedIn.value ? STICKY_TOP_LOGGED_IN : 0))
const stickyTop = computed(() => `${stickyTopPx.value}px`)

interface ManualBlock {
  /** 小节锚点 id（章内直属步骤没有 id） */
  id?: string
  /** 小节标题（章内直属步骤没有标题） */
  title?: string
  steps: ManualStep[]
}

/** 把一章摊平成「一个或多个步骤块」：有小节的章每小节一块，否则整章一块 */
function chapterBlocks(chapter: ManualChapter): ManualBlock[] {
  if (chapter.subs.length) {
    return chapter.subs.map((sub) => ({ id: sub.id, title: sub.title, steps: sub.steps }))
  }
  return chapter.steps.length ? [{ steps: chapter.steps }] : []
}

/** 灯箱可浏览的插图全集：按正文出现顺序摊平（figure 在前、duo 在后） */
const ALL_FIGURES: ManualFigure[] = MANUAL_CHAPTERS.flatMap((chapter) =>
  chapterBlocks(chapter).flatMap((block) =>
    block.steps.flatMap((step) => [...step.figures, ...(step.duo ?? [])]),
  ),
)

const rootEl = ref<HTMLElement | null>(null)

const progress = ref(0)
const totopOn = ref(false)
const activeId = ref(MANUAL_TOC[0].id)

const feedbackVisible = ref(false)

const lightboxShots = ref<ManualFigure[]>([])
const lightboxIndex = ref(0)

/** 打开灯箱：浏览全文插图并定位到被点的那张 */
function openLightbox(figure: ManualFigure) {
  lightboxShots.value = ALL_FIGURES
  const idx = ALL_FIGURES.indexOf(figure)
  lightboxIndex.value = idx >= 0 ? idx : 0
}

function closeLightbox() {
  lightboxShots.value = []
}

// ── 滚动容器 ──────────────────────────────────────────────────────────────
// SPA 的滚动容器不是 window，而是 App.vue 里的 .main-content（global.scss 中 overflow-y:auto），
// 因此滚动监听、度量与 IntersectionObserver 的 root 都以它为基准，找不到才回退 window。
const scrollerEl = ref<HTMLElement | null>(null)
let listenerTarget: EventTarget | null = null

function resolveScroller() {
  const root = rootEl.value
  scrollerEl.value = null
  if (!root) return

  // 1) 最近的 .main-content 祖先
  const main = root.closest('.main-content')
  if (main instanceof HTMLElement) {
    scrollerEl.value = main
    return
  }

  // 2) 兜底：向上找第一个真正可滚动的祖先（overflow-y 为 auto/scroll 且内容已溢出）
  let node: HTMLElement | null = root.parentElement
  while (node) {
    const overflowY = window.getComputedStyle(node).overflowY
    if ((overflowY === 'auto' || overflowY === 'scroll') && node.scrollHeight > node.clientHeight) {
      scrollerEl.value = node
      return
    }
    node = node.parentElement
  }

  // 3) 都没有：scrollerEl 保持 null，调用方回退 window
}

/** 当前滚动容器的度量：top 已滚动距离、view 可视高度、full 内容高度 */
function scrollMetrics() {
  const el = scrollerEl.value
  if (el) return { top: el.scrollTop, view: el.clientHeight, full: el.scrollHeight }
  return {
    top: window.scrollY || window.pageYOffset || 0,
    view: window.innerHeight,
    full: document.documentElement.scrollHeight,
  }
}

/** 滚动容器可视区顶边在视口中的位置（window 滚动时为 0） */
function viewportTop() {
  return scrollerEl.value ? scrollerEl.value.getBoundingClientRect().top : 0
}

/** 滚到指定位置：不支持 scrollTo 的环境退回直接赋值 scrollTop */
function scrollContainerTo(container: HTMLElement | null, top: number) {
  if (!container) {
    window.scrollTo({ top, behavior: 'smooth' })
    return
  }
  if (typeof container.scrollTo === 'function') container.scrollTo({ top, behavior: 'smooth' })
  else container.scrollTop = top
}

/** 当前章节 = 最后一个已越过基准线（吸顶条下方）的锚点 */
function updateActive() {
  const root = rootEl.value
  if (!root) return
  const line = viewportTop() + stickyTopPx.value + 12
  let current = MANUAL_TOC[0].id
  for (const item of MANUAL_TOC) {
    const el = root.querySelector<HTMLElement>(`[id="${item.id}"]`)
    if (!el) continue
    if (el.getBoundingClientRect().top - line <= 0) current = item.id
  }
  // 滚到底时最后一项可能永远越不过基准线，直接高亮它
  const { top, view, full } = scrollMetrics()
  if (full - view - top <= 2) current = MANUAL_TOC[MANUAL_TOC.length - 1].id
  activeId.value = current
}

function updateScrollState() {
  const { top, view, full } = scrollMetrics()
  const max = Math.max(1, full - view)
  progress.value = Math.min(100, Math.max(0, (top / max) * 100))
  totopOn.value = top > TOTOP_SHOW_AT
  updateActive()
}

// rAF 节流：滚动事件里只标脏，真正的计算每帧最多一次
let rafId = 0

function onScroll() {
  if (rafId) return
  rafId = window.requestAnimationFrame(() => {
    rafId = 0
    updateScrollState()
  })
}

function bindScrollTarget() {
  const target: EventTarget = scrollerEl.value ?? window
  if (listenerTarget === target) return
  listenerTarget?.removeEventListener('scroll', onScroll)
  target.addEventListener('scroll', onScroll, { passive: true })
  listenerTarget = target
}

function onResize() {
  resolveScroller()
  bindScrollTarget()
  updateScrollState()
}

// ── .rv 进场动画 ──────────────────────────────────────────────────────────
let revealObserver: IntersectionObserver | null = null

function setupReveal() {
  const root = rootEl.value
  if (!root) return
  const items = Array.from(root.querySelectorAll<HTMLElement>('.rv'))
  if (typeof IntersectionObserver === 'undefined') {
    items.forEach((el) => el.classList.add('in'))
    return
  }
  revealObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        entry.target.classList.add('in')
        revealObserver?.unobserve(entry.target)
      }
    },
    // root 用同一个滚动容器；元素被滚动容器裁剪，判定与视觉一致
    { root: scrollerEl.value, threshold: 0.05, rootMargin: '0px 0px -40px 0px' },
  )
  for (const el of items) revealObserver.observe(el)
}

/**
 * 锚点跳转：不用原生 href（避免改 URL hash 与 vue-router 打架），改为滚动到目标元素。
 *
 * 遮挡问题用「显式滚动补偿」解决，而不是 CSS scroll-margin-top：手册的滚动容器是外部的
 * .main-content，且要避开的吸顶高度随登录态在 0 / 52px 之间变化，CSS 里只能写死一个值；
 * 把偏移量集中算在 JS 里（目标位置 - stickyTop - 16），滚动容器与顶栏高度变化时只有一处需要维护，
 * 测试也能直接断言算出来的 top。
 */
function scrollToId(id: string) {
  const el = rootEl.value?.querySelector<HTMLElement>(`[id="${id}"]`)
  if (!el) return
  const container = scrollerEl.value
  const rect = el.getBoundingClientRect()
  const base = container
    ? container.scrollTop + rect.top - container.getBoundingClientRect().top
    : (window.scrollY || window.pageYOffset || 0) + rect.top
  const target = base - stickyTopPx.value - ANCHOR_GAP
  scrollContainerTo(container, Math.max(0, target))
}

function scrollToTop() {
  scrollContainerTo(scrollerEl.value, 0)
}

/** 登录页地址：手册正文里的 https://<loginUrl>/login */
const loginHref = computed(() => `https://${MANUAL_META.loginUrl}/login`)

onMounted(async () => {
  await nextTick()
  resolveScroller()
  bindScrollTarget()
  window.addEventListener('resize', onResize)
  updateScrollState()
  setupReveal()
})

onUnmounted(() => {
  listenerTarget?.removeEventListener('scroll', onScroll)
  listenerTarget = null
  window.removeEventListener('resize', onResize)
  if (rafId) window.cancelAnimationFrame(rafId)
  revealObserver?.disconnect()
  revealObserver = null
})
</script>
