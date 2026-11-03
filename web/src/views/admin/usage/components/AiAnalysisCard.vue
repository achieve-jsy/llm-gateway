<template>
  <section class="ai-card" @click="onCardClick">
    <!-- 头部：头像 + 名称 + 范围标签 -->
    <div class="ai-hd">
      <div class="ai-avatar" aria-hidden="true">AI</div>
      <div class="ai-name">AI 分析</div>
      <span class="ai-badge">{{ scopeLabel }}</span>
      <span class="ai-badge">规则引擎 · 引用真实数字</span>
    </div>

    <!-- 骨架屏（loading 时 3 条灰色占位条） -->
    <div v-if="loading" class="ai-skeleton" role="status" aria-label="分析加载中">
      <div class="ai-skeleton__bar ai-skeleton__bar--short" />
      <div class="ai-skeleton__bar" />
      <div class="ai-skeleton__bar ai-skeleton__bar--mid" />
    </div>

    <!-- 内容：事实 / 原因 / 建议 三段 -->
    <template v-else>
      <div v-if="segments.length" class="ai-body">
        <div v-for="seg in segments" :key="seg.tag" class="ai-sec">
          <span class="ai-sec-tag">{{ seg.tag }}</span>
          <span class="ai-sec-text" v-html="seg.html" />
        </div>
      </div>
      <div v-else class="ai-body ai-body--empty">当前范围暂无分析，调整筛选后再看。</div>
    </template>

    <!-- 页脚 -->
    <div class="ai-foot">规则引擎生成 · 分析范围随筛选联动 · 引用数值可下钻核实 · 带"估"为估算</div>
  </section>
</template>

<script setup lang="ts">
defineOptions({ name: 'AiAnalysisCard' })

/** 单条洞察：tag 决定段标签，html 为父页面已拼好的内联 HTML（含 <b>/<span class="num">/<a class="ai-drill" data-*> 等）。 */
interface AiSegment {
  tag: '事实' | '原因' | '建议'
  html: string
}

/** 下钻载荷：命中内联 <a> 上的 data-dept / data-model / data-scroll 时向上抛。 */
interface DrillPayload {
  dept?: string
  model?: string
  scroll?: string
}

defineProps<{
  scopeLabel: string
  segments: AiSegment[]
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'drill', payload: DrillPayload): void
}>()

// 事件委托：组件自身不执行滚动，只把 data 属性解析成 payload 交给父页面处理。
function onCardClick(e: MouseEvent) {
  const target = e.target as HTMLElement | null
  if (!target || typeof target.closest !== 'function') return
  const el = target.closest<HTMLElement>('a[data-dept], a[data-model], a[data-scroll]')
  if (!el) return
  const payload: DrillPayload = {}
  const dept = el.dataset.dept
  const model = el.dataset.model
  const scroll = el.dataset.scroll
  if (dept) payload.dept = dept
  if (model) payload.model = model
  if (scroll) payload.scroll = scroll
  emit('drill', payload)
}
</script>

<style scoped>
/* ── AI 分析卡：视觉对齐 prototype/model-analytics-system-prototype.html 的 .ai-card 一族 ──
 * 复用 variables.scss 的 --brand-green / --el-text-color-* / --el-fill-color-* token，
 * 仅在需要明暗差异的场合定义 --ai-* 局部变量并在 html.dark 下覆盖，天然支持暗色。 */
.ai-card {
  /* 局部色板（对应原型 --accent / --accent-dark / --accent-line / --accent-dim / --amber-*） */
  --ai-green: var(--brand-green, #36b34e);
  --ai-green-text: #1f8a3b;
  --ai-green-line: #9ed8ac;
  --ai-green-dim: rgba(54, 179, 78, 0.1);
  --ai-amber-text: #b87112;
  --ai-amber-dim: rgba(230, 162, 60, 0.12);

  margin-top: 16px;
  padding: 15px 18px;
  border-radius: var(--radius-base, 8px);
  background: linear-gradient(135deg, var(--ai-green-dim), transparent 55%);
  border: 1px solid var(--ai-green-line);
  box-shadow: var(--shadow-sm);
}
html.dark .ai-card {
  --ai-green-text: #7fd48e;
  --ai-green-line: rgba(93, 195, 112, 0.35);
  --ai-green-dim: rgba(93, 195, 112, 0.14);
  --ai-amber-text: #f0bb6e;
}

/* 头部 */
.ai-hd {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 9px;
}
.ai-avatar {
  width: 22px;
  height: 22px;
  border-radius: 7px;
  background: var(--ai-green);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.ai-name {
  font-size: 12.5px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.ai-badge {
  font-size: 10.5px;
  color: var(--ai-green-text);
  background: var(--ai-green-dim);
  border: 1px solid var(--ai-green-line);
  border-radius: 999px;
  padding: 1px 8px;
}

/* 主体 */
.ai-body {
  font-size: 13.5px;
  line-height: 1.7;
  color: var(--el-text-color-regular);
}
.ai-body--empty {
  color: var(--el-text-color-secondary);
}
.ai-body :deep(b) {
  color: var(--el-text-color-primary);
  font-weight: 700;
}
.ai-body :deep(.num) {
  font-family: var(--font-mono);
  color: var(--ai-green-text);
  font-weight: 600;
}

/* 三段 */
.ai-sec {
  margin-top: 6px;
}
.ai-sec:first-child {
  margin-top: 0;
}
.ai-sec-tag {
  display: inline-block;
  min-width: 30px;
  margin-right: 8px;
  padding: 1px 7px;
  border-radius: 999px;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.02em;
  text-align: center;
  background: var(--ai-green-dim);
  color: var(--ai-green-text);
  border: 1px solid var(--ai-green-line);
}

/* 内联下钻链接 / 估算标记（来自 v-html，需 :deep） */
.ai-body :deep(.ai-drill) {
  display: inline-block;
  font-size: 11px;
  color: var(--ai-green-text);
  font-weight: 600;
  text-decoration: none;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.55);
  border: 1px solid var(--ai-green-line);
  border-radius: 999px;
  padding: 0 7px;
  margin-left: 3px;
  transition: background 0.15s;
}
html.dark .ai-card :deep(.ai-drill) {
  background: rgba(255, 255, 255, 0.06);
}
.ai-card :deep(.ai-drill:hover) {
  background: var(--ai-green-dim);
}
.ai-card :deep(.ai-flag) {
  display: inline-block;
  font-size: 10px;
  color: var(--ai-amber-text);
  background: var(--ai-amber-dim);
  border-radius: 4px;
  padding: 0 4px;
  margin-left: 4px;
  vertical-align: 1px;
  font-weight: 600;
}
.ai-card :deep(.ai-flag.est) {
  color: var(--ai-green-text);
  background: var(--ai-green-dim);
}

/* 页脚 */
.ai-foot {
  margin-top: 9px;
  font-size: 11.5px;
  color: var(--el-text-color-secondary);
}

/* 骨架屏 */
.ai-skeleton {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ai-skeleton__bar {
  height: 12px;
  border-radius: 6px;
  background: var(--el-fill-color);
  animation: ai-skeleton-pulse 1.4s ease-in-out infinite;
}
.ai-skeleton__bar--short {
  width: 78%;
}
.ai-skeleton__bar--mid {
  width: 92%;
}
@keyframes ai-skeleton-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.45;
  }
}
</style>
