<template>
  <el-drawer
    :model-value="modelValue"
    direction="rtl"
    :size="size ?? '800px'"
    destroy-on-close
    class="budget-edit-drawer"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <template #header>
      <div class="bed-header">
        <div class="bed-header-info">
          <span class="bed-title">{{ title }}</span>
          <span class="bed-sub">{{ scopeType === 'studio' ? '工作室预算配置' : '部门预算配置' }}</span>
        </div>
      </div>
    </template>

    <div v-if="resolving" v-loading="true" style="height: 240px" />
    <BudgetScopePanel
      v-else
      :scope-type="scopeType"
      :scope-id="scopeId"
      :title="title"
      :child-scope-ids="resolvedChildIds"
      :can-edit="canEdit"
      :can-edit-self="canEditSelf"
      @navigate="$emit('navigate', $event)"
    />
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import BudgetScopePanel from './BudgetScopePanel.vue'
import { getBudgetList } from '@/services/control-plane'

const props = defineProps<{
  modelValue: boolean
  scopeType: 'studio' | 'dept'
  scopeId: string
  title: string
  /** studio 模式下子部门 ID 列表；不传时自动从 API 加载 */
  childScopeIds?: string[]
  canEdit?: boolean
  canEditSelf?: boolean
  size?: string
}>()

defineEmits<{
  'update:modelValue': [value: boolean]
  navigate: [payload: { scopeType: 'dept' | 'member'; scopeId: string; label: string }]
}>()

const resolving = ref(false)
const resolvedChildIds = ref<string[]>(props.childScopeIds ?? [])

watch(
  () => [props.modelValue, props.scopeType, props.scopeId] as const,
  async ([visible, sType, sId]) => {
    if (!visible) return
    if (sType === 'studio' && !props.childScopeIds?.length) {
      resolving.value = true
      try {
        const items = await getBudgetList(undefined, sId, 'studio')
        const ids = new Set<string>()
        for (const item of items) {
          if (item.scope_type === 'dept_daily' || item.scope_type === 'dept_service') {
            ids.add(item.scope_id)
          }
        }
        resolvedChildIds.value = Array.from(ids)
      } finally {
        resolving.value = false
      }
    } else {
      resolvedChildIds.value = props.childScopeIds ?? []
    }
  },
  { immediate: true },
)
</script>

<style scoped>
.bed-header {
  display: flex;
  align-items: center;
  width: 100%;
}
.bed-header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.bed-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.bed-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
:deep(.budget-edit-drawer .el-drawer__body) {
  padding: 20px 24px;
  overflow-y: auto;
}
</style>
