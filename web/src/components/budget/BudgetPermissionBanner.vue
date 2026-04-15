<template>
  <aside class="budget-permission-banner permission-banner" :data-test="`budget-permission-${role}`">
    <span class="budget-permission-banner__badge">{{ copy.badge }}</span>
    <div class="budget-permission-banner__copy">
      <strong>{{ copy.title }}</strong>
      <span>{{ copy.detail }}</span>
    </div>
    <div v-if="copy.canOperate && $slots.actions" class="budget-permission-banner__actions">
      <slot name="actions" />
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  role: string
  scopeType?: string
}>()

const copy = computed(() => {
  if (props.role === 'admin') {
    return {
      badge: '平台管理员',
      title: '当前拥有全部预算配置权限',
      detail: '可调整当前层级，并继续向下分配。',
      canOperate: true,
    }
  }
  if (props.role === 'studio_admin') {
    return {
      badge: '工作室管理员',
      title: '当前可管理工作室下属预算',
      detail: '可配置下属部门；工作室总额度需申请扩容。',
      canOperate: true,
    }
  }
  if (props.role === 'dept_admin') {
    return {
      badge: '部门管理员',
      title: '当前可管理部门成员预算',
      detail: '可配置成员额度；部门总预算需申请扩容。',
      canOperate: true,
    }
  }
  return {
    badge: '只读访问',
    title: '当前没有预算配置权限',
    detail: props.scopeType === 'member'
      ? '可查看个人预算，调整额度请联系部门管理员。'
      : '可查看当前范围，调整额度请联系上级管理员。',
    canOperate: false,
  }
})
</script>

<style scoped>
.budget-permission-banner {
  display: flex;
  min-height: 48px;
  padding: var(--space-3) var(--space-4);
  align-items: center;
  gap: var(--space-3);
  color: var(--text-regular);
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--brand-green-light);
  border-radius: var(--budget-panel-radius);
}

.budget-permission-banner__badge {
  flex: none;
  padding: 2px var(--space-2);
  color: var(--brand-green-text);
  background: var(--bg-surface);
  border: 1px solid var(--brand-green-light);
  border-radius: var(--radius-full);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
}

.budget-permission-banner__copy {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.budget-permission-banner__copy span {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.budget-permission-banner__actions {
  display: flex;
  margin-left: auto;
  gap: var(--space-2);
}

@media (max-width: 760px) {
  .budget-permission-banner {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .budget-permission-banner__actions {
    width: 100%;
    margin-left: 0;
  }
}
</style>
