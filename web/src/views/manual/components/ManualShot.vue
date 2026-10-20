<template>
  <figure class="shot">
    <!-- 简洁圆角图卡：无 macOS 三点 chrome；整块图是可点击/可键盘触发的放大按钮 -->
    <button
      type="button"
      class="shot-canvas"
      data-test="manual-shot-zoom"
      :aria-label="`放大查看：${figure.path}`"
      @click="emit('zoom', figure)"
    >
      <img
        :src="assetUrl"
        :width="figure.width"
        :height="figure.height"
        :alt="figure.alt"
        loading="lazy"
        decoding="async"
      />
      <span class="shot-zoom" aria-hidden="true">点击放大</span>
    </button>

    <!-- 说明文案是第一方静态内容（manual-data.ts 逐字取自源 HTML，允许 em/strong/code 内联标记），故用 v-html -->
    <figcaption v-if="figure.caption" class="shot-foot">
      <i aria-hidden="true" />
      <span v-html="figure.caption" />
    </figcaption>
  </figure>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ManualFigure } from '../manual-data'

const props = defineProps<{
  /** 插图数据 */
  figure: ManualFigure
}>()

const emit = defineEmits<{
  (e: 'zoom', figure: ManualFigure): void
}>()

/** 素材地址：源约定 /manual/assets/<name>.webp（web/public/manual/assets 下） */
const assetUrl = computed(() => `/manual/assets/${props.figure.name}.webp`)
</script>
