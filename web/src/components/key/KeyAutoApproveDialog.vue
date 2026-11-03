<template>
  <el-dialog
    :model-value="modelValue"
    title="自动通过待审批申请"
    width="560px"
    :close-on-click-modal="!loading"
    :close-on-press-escape="!loading"
    :show-close="!loading"
    @update:model-value="emit('update:modelValue', $event)"
    @closed="emit('closed')"
  >
    <div v-if="loading" class="auto-approve-loading">
      <el-progress :percentage="100" :indeterminate="true" :duration="1.2" />
      <p class="auto-approve-loading-text">
        正在关闭部门审批并自动处理
        <strong v-if="pendingCount > 0">{{ pendingCount }}</strong>
        条待审批申请，请稍候…
      </p>
      <p class="auto-approve-loading-hint">全部在后端一次性完成，无需逐条操作</p>
    </div>

    <template v-else-if="result">
      <el-alert
        :type="result.failed > 0 ? 'warning' : 'success'"
        :closable="false"
        show-icon
        class="auto-approve-summary"
      >
        <template #title>
          共 {{ result.total }} 条：成功 {{ result.success }} 条
          <template v-if="result.failed > 0">，失败 {{ result.failed }} 条</template>
        </template>
        <template v-if="result.success > 0" #default>
          已通过站内通知告知申请人，对方可在「我的密钥」查看新 Key。
        </template>
      </el-alert>

      <el-table :data="result.items" size="small" max-height="320" class="auto-approve-table">
        <el-table-column label="Key 名称" prop="key_name" min-width="110" show-overflow-tooltip />
        <el-table-column label="申请人" prop="requester_name" width="100" show-overflow-tooltip />
        <el-table-column label="预算申请" min-width="158" align="center" class-name="key-request-budget-col">
          <template #default="{ row }">
            <KeyRequestBudgetTag :budget-monthly="row.budget_monthly" :show-icon="false" />
          </template>
        </el-table-column>
        <el-table-column label="状态" width="88">
          <template #default="{ row }">
            <el-tag :type="row.status === 'approved' ? 'success' : row.status === 'skipped' ? 'info' : 'danger'" size="small">
              {{ row.status === 'approved' ? '已通过' : row.status === 'skipped' ? '已跳过' : '失败' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="通知" width="72" align="center">
          <template #default="{ row }">
            <el-icon v-if="row.status === 'approved' && row.notified" color="var(--el-color-success)"><CircleCheck /></el-icon>
            <span v-else-if="row.status === 'approved'" class="text-muted">未发送</span>
            <span v-else class="text-muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="说明" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.error" class="error-text">{{ row.error }}</span>
            <span v-else-if="row.status === 'skipped'" class="text-muted">已由其他审批流程处理</span>
            <span v-else class="text-muted">Key 已创建</span>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <template #footer>
      <el-button v-if="!loading" type="primary" @click="emit('update:modelValue', false)">知道了</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { CircleCheck } from '@element-plus/icons-vue'
import KeyRequestBudgetTag from '@/components/key/KeyRequestBudgetTag.vue'
import type { KeyRequestAutoApproveResult } from '@/types/key-request'

defineProps<{
  modelValue: boolean
  loading: boolean
  pendingCount?: number
  result: KeyRequestAutoApproveResult | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  closed: []
}>()
</script>

<style scoped>
.auto-approve-loading {
  padding: 8px 0 4px;
}
.auto-approve-loading-text {
  margin: 16px 0 6px;
  font-size: 14px;
  color: var(--el-text-color-primary);
}
.auto-approve-loading-hint {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.auto-approve-summary {
  margin-bottom: 12px;
}
.auto-approve-table {
  width: 100%;
}
.text-muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.error-text {
  color: var(--el-color-danger);
  font-size: 12px;
}
</style>
