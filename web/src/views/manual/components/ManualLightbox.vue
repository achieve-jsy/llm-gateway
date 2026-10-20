<template>
  <!-- 灯箱：遮罩 + 面板（面板用主题变量，遮罩是规格允许的 rgba(0,0,0,.82)） -->
  <div
    v-if="current"
    class="lb"
    :class="{ vis: visible }"
    data-test="manual-lightbox"
    role="dialog"
    aria-modal="true"
    :aria-label="`查看大图：${current.path}`"
    @click.self="emit('close')"
  >
    <div class="lb-panel">
      <div class="lb-bar">
        <span class="lb-path">{{ current.path }}</span>
        <span class="sp">
          <button type="button" class="lb-btn" @click="prev">上一张</button>
          <button type="button" class="lb-btn" @click="next">下一张</button>
          <button type="button" class="lb-btn" @click="emit('close')">关闭 (Esc)</button>
        </span>
      </div>

      <img :src="assetUrl" :alt="current.alt" />

      <div class="lb-cap">
        <b>{{ current.path }}</b>
        <!-- 说明文案是第一方静态内容（manual-data.ts 逐字取自源 HTML），故用 v-html -->
        <span v-if="current.caption"> · <span v-html="current.caption" /></span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { ManualFigure } from '../manual-data'

const props = defineProps<{
  /** 当前可浏览的插图集合 */
  shots: ManualFigure[]
  /** 当前下标；切换通过 update:index 交给父级维护 */
  index: number
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'update:index', value: number): void
}>()

const current = computed<ManualFigure | undefined>(() => props.shots[props.index])
const assetUrl = computed(() => (current.value ? `/manual/assets/${current.value.name}.webp` : ''))

/** 淡入标记：先渲染出来再补 .vis，保证 opacity 过渡生效 */
const visible = ref(false)

/** 上一张：越界回到最后一张（循环切换） */
function prev() {
  if (!props.shots.length) return
  emit('update:index', (props.index - 1 + props.shots.length) % props.shots.length)
}

/** 下一张：越界回到第一张（循环切换） */
function next() {
  if (!props.shots.length) return
  emit('update:index', (props.index + 1) % props.shots.length)
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    emit('close')
  } else if (event.key === 'ArrowLeft') {
    event.preventDefault()
    prev()
  } else if (event.key === 'ArrowRight') {
    event.preventDefault()
    next()
  }
}

// 打开时锁 body 滚动，卸载时恢复原值
let bodyOverflow = ''
let rafId = 0

onMounted(() => {
  bodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  window.addEventListener('keydown', onKeydown)
  rafId = window.requestAnimationFrame(() => {
    visible.value = true
  })
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  if (rafId) window.cancelAnimationFrame(rafId)
  document.body.style.overflow = bodyOverflow
})
</script>
