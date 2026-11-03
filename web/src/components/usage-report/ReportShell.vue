<template>
  <section class="rs" :class="{ 'rs--no-title': !title }">
    <header v-if="title || $slots.actions" class="rs__head">
      <h3 class="rs__title">{{ title }}</h3>
      <div class="rs__actions"><slot name="actions" /></div>
    </header>
    <div class="rs__body"><slot /></div>
  </section>
</template>

<script setup lang="ts">
defineProps<{
  title?: string
}>()
</script>

<style scoped>
.rs {
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.rs__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.rs__title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.rs__actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
</style>
