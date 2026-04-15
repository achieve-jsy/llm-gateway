<template>
  <div class="budget-empty-state" :data-test="`budget-empty-${level}`">
    <div class="budget-empty-state__icon" aria-hidden="true">◇</div>
    <strong>{{ copy.title }}</strong>
    <p>{{ copy.description }}</p>
    <el-button
      v-if="actionLabel"
      type="primary"
      :data-test="actionTest"
      @click="emit('action')"
    >
      {{ actionLabel }}
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

type EmptyLevel = 'company' | 'studio' | 'dept' | 'member'

const props = defineProps<{
  level: EmptyLevel
  actionLabel?: string
  actionTest?: string
}>()

const emit = defineEmits<{
  action: []
}>()

const emptyCopy: Record<EmptyLevel, { title: string; description: string }> = {
  company: {
    title: '尚未向工作室分配预算',
    description: '先设置公司预算，再选择工作室进行单项或批量分配。',
  },
  studio: {
    title: '暂无部门预算',
    description: '新增部门预算后，可分别管理成员日常池和程序服务池。',
  },
  dept: {
    title: '暂无成员独立额度',
    description: '未设置独立额度的成员继续使用部门共享池。',
  },
  member: {
    title: '暂无个人预算限制',
    description: '该成员当前继续使用部门共享预算池。',
  },
}

const copy = computed(() => emptyCopy[props.level])
</script>

<style scoped>
.budget-empty-state {
  display: grid;
  min-height: 220px;
  padding: var(--space-6) var(--space-4);
  place-items: center;
  align-content: center;
  gap: var(--space-2);
  color: var(--text-regular);
  text-align: center;
}

.budget-empty-state__icon {
  display: grid;
  width: 40px;
  height: 40px;
  place-items: center;
  color: var(--text-placeholder);
  background: var(--bg-subtle);
  border-radius: var(--radius-full);
  font-size: var(--font-size-title);
}

.budget-empty-state p {
  max-width: 480px;
  margin: 0 0 var(--space-2);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}
</style>
