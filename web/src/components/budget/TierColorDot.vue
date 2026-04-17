<template>
  <span
    class="tier-color-dot"
    data-test="tier-color-dot"
    aria-hidden="true"
    :style="{ '--tier-color': safeColor }"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  color?: string
}>()

const SAFE_COLOR = /^(?:#[\da-f]{3,8}|(?:rgb|hsl)a?\([\d\s.,%+-]+\)|[a-z]+)$/i

const safeColor = computed(() => {
  const value = props.color?.trim()
  return value && SAFE_COLOR.test(value) ? value : 'var(--budget-tier-muted)'
})
</script>

<style scoped>
.tier-color-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  flex: none;
  border-radius: var(--radius-full);
  background-color: var(--tier-color);
}
</style>
