<template>
  <el-tag
    :type="hasBudget ? 'success' : 'info'"
    size="small"
    :effect="hasBudget ? 'light' : 'plain'"
    class="key-request-budget-tag"
  >
    <el-icon v-if="showIcon" class="key-request-budget-tag__icon"><Wallet /></el-icon>
    <template v-if="hasBudget">{{ KEY_REQUEST_BUDGET_APPLIED_LABEL }} {{ formatted }}</template>
    <template v-else>{{ KEY_REQUEST_BUDGET_NONE_LABEL }}</template>
  </el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Wallet } from '@element-plus/icons-vue'
import {
  formatKeyRequestBudget,
  keyRequestHasBudget,
  KEY_REQUEST_BUDGET_APPLIED_LABEL,
  KEY_REQUEST_BUDGET_NONE_LABEL,
} from '@/types/key-request'

const props = withDefaults(defineProps<{
  budgetMonthly?: number | string | null
  showIcon?: boolean
}>(), {
  showIcon: true,
})

const hasBudget = computed(() => keyRequestHasBudget(props.budgetMonthly))
const formatted = computed(() => formatKeyRequestBudget(props.budgetMonthly))
</script>

<style scoped>
.key-request-budget-tag {
  vertical-align: middle;
  white-space: nowrap;
}
.key-request-budget-tag__icon {
  margin-right: 2px;
  vertical-align: -2px;
}
</style>
