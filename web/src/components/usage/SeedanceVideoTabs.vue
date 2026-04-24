<template>
  <div class="svt">
    <el-tabs v-model="activeSubTab" class="svt-tabs">
      <el-tab-pane label="Video 任务列表" name="tasks">
        <VideoKpiCards
          :refresh-token="refreshToken"
          :start="start"
          :end="end"
          :model-name="modelName"
          :provider-name="providerName || undefined"
          :mine="mine"
        />
        <VideoTrendChart
          :refresh-token="refreshToken"
          :start="start"
          :end="end"
          :model-name="modelName"
          :provider-name="providerName || undefined"
          :mine="mine"
        />
        <VideoDistributionCharts
          :refresh-token="refreshToken"
          :start="start"
          :end="end"
          :model-name="modelName"
          :provider-name="providerName || undefined"
          :mine="mine"
        />
        <VideoTaskTable
          :refresh-token="refreshToken"
          controlled
          :mine="mine"
          :status="taskStatus"
          :model-name="modelName"
          :provider-name="providerName"
        />
      </el-tab-pane>

      <el-tab-pane label="Seedance 请求日志" name="logs">
        <UsageLogSection
          title="Seedance 请求日志"
          :api-path="apiPath"
          :trace-api-path="traceApiPath"
          :params="seedanceLogParams"
          :currency="currency"
          export-prefix="seedance_logs"
          :table-props="mergedLogTableProps"
        />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import UsageLogSection from '@/components/usage/UsageLogSection.vue'
import VideoKpiCards from '@/components/usage/VideoKpiCards.vue'
import VideoTrendChart from '@/components/usage/VideoTrendChart.vue'
import VideoDistributionCharts from '@/components/usage/VideoDistributionCharts.vue'
import VideoTaskTable from '@/components/usage/VideoTaskTable.vue'
import type { Currency } from '@/utils/currency'

const props = withDefaults(defineProps<{
  refreshToken?: number
  start?: string
  end?: string
  modelName?: string
  providerName?: string
  taskStatus?: string
  mine?: boolean
  apiPath: string
  traceApiPath: string
  logParams?: Record<string, any>
  logTableProps?: Record<string, any>
  currency?: Currency
}>(), {
  refreshToken: 0,
  start: '',
  end: '',
  modelName: '',
  providerName: '',
  taskStatus: '',
  mine: false,
  logParams: () => ({}),
  logTableProps: () => ({}),
  currency: 'USD',
})

const activeSubTab = ref<'tasks' | 'logs'>('tasks')

const seedanceLogParams = computed(() => {
  const params: Record<string, any> = {
    ...props.logParams,
    seedance: true,
  }
  delete params.endpoint
  const status = String(params.status || '')
  if (status && !['success', 'error'].includes(status)) {
    delete params.status
  }
  if (props.providerName) {
    params.provider = props.providerName
  }
  if (props.modelName) {
    params.model_name = props.modelName
    delete params.group_name
  }
  return params
})

const mergedLogTableProps = computed(() => ({
  showProvider: true,
  showGroup: true,
  showKey: true,
  showKeyType: true,
  showSource: 'full',
  sortableColumns: ['duration_ms', 'est_cost_usd', 'ts'] as const,
  endpointType: 'chat',
  ...props.logTableProps,
}))

</script>

<style scoped>
.svt {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.svt-tabs {
  --el-tabs-header-height: 44px;
}
:deep(.el-tabs__header) {
  margin: 0 0 12px;
}
</style>
