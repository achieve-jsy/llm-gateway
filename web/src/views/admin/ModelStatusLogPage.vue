<template>
  <div class="status-log-page">
    <div class="page-header">
      <h2 class="page-title">模型状态变更日志</h2>
    </div>

    <div class="filter-bar">
      <el-input
        v-model="searchModelName"
        placeholder="搜索模型名称"
        clearable
        style="width: 200px"
        @change="handleFilterChange"
      />
      <el-select
        v-model="filterProviderId"
        placeholder="按渠道筛选"
        clearable
        style="width: 180px"
        @change="handleFilterChange"
      >
        <el-option
          v-for="p in providerOptions"
          :key="p.id"
          :label="p.name"
          :value="p.id"
        />
      </el-select>
      <el-select
        v-model="filterChangeType"
        placeholder="按变更类型筛选"
        clearable
        style="width: 160px"
        @change="handleFilterChange"
      >
        <el-option label="自动禁用" value="auto_disabled" />
        <el-option label="自动恢复" value="auto_recovered" />
        <el-option label="手动禁用" value="manual_disabled" />
        <el-option label="手动启用" value="manual_enabled" />
      </el-select>
      <el-date-picker
        v-model="dateRange"
        type="datetimerange"
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        style="width: 360px"
        @change="handleFilterChange"
      />
    </div>

    <el-table v-loading="loading" :data="logs" border stripe>
      <el-table-column prop="model_name" label="模型名" min-width="180" show-overflow-tooltip />
      <el-table-column prop="provider_name" label="渠道名" width="150" />
      <el-table-column label="变更类型" width="120" align="center">
        <template #default="{ row }">
          <el-tag :type="changeTypeTag(row.change_type)">
            {{ changeTypeLabel(row.change_type) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="reason" label="变更原因" min-width="220" show-overflow-tooltip />
      <el-table-column label="时间" width="180" align="center">
        <template #default="{ row }">
          <el-tooltip :content="formatFullTime(row.created_at)" placement="top">
            <span>{{ formatRelativeTime(row.created_at) }}</span>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column prop="operator_name" label="操作人" width="120" />
    </el-table>

    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="loadLogs"
        @size-change="handleSizeChange"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getModelStatusLogs, getProviders } from '@/services/control-plane'
import type { ModelStatusLog } from '@/services/control-plane'
import type { Provider } from '@/types/provider'

const route = useRoute()

const logs = ref<ModelStatusLog[]>([])
const providerOptions = ref<Provider[]>([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const searchModelName = ref('')
const filterProviderId = ref('')
const filterChangeType = ref('')
const dateRange = ref<[Date, Date] | null>(null)

function changeTypeLabel(type: string): string {
  const map: Record<string, string> = {
    auto_disabled: '自动禁用',
    auto_recovered: '自动恢复',
    manual_disabled: '手动禁用',
    manual_enabled: '手动启用',
  }
  return map[type] || type
}

function changeTypeTag(type: string): 'danger' | 'success' | 'warning' | '' {
  const map: Record<string, 'danger' | 'success' | 'warning' | ''> = {
    auto_disabled: 'danger',
    auto_recovered: 'success',
    manual_disabled: 'warning',
    manual_enabled: '',
  }
  return map[type] || ''
}

function formatRelativeTime(time: string): string {
  const now = Date.now()
  const then = new Date(time).getTime()
  const diff = now - then
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}秒前`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}小时前`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}天前`
  return formatFullTime(time)
}

function formatFullTime(time: string): string {
  const date = new Date(time)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

async function loadLogs() {
  loading.value = true
  try {
    const params: any = {
      page: currentPage.value,
      page_size: pageSize.value,
    }
    if (filterProviderId.value) params.provider_id = filterProviderId.value
    if (filterChangeType.value) params.change_type = filterChangeType.value
    if (dateRange.value) {
      params.start_time = dateRange.value[0].toISOString()
      params.end_time = dateRange.value[1].toISOString()
    }
    if (searchModelName.value) params.model_name = searchModelName.value

    const result = await getModelStatusLogs(params)
    logs.value = result.items
    total.value = result.total
  } catch {
    ElMessage.error('加载日志失败')
  } finally {
    loading.value = false
  }
}

async function loadProviders() {
  try {
    const result = await getProviders({ page: 1, pageSize: 1000 })
    providerOptions.value = result.data
  } catch {
    ElMessage.error('加载渠道列表失败')
  }
}

function handleFilterChange() {
  currentPage.value = 1
  loadLogs()
}

function handleSizeChange() {
  currentPage.value = 1
  loadLogs()
}

onMounted(() => {
  const modelId = route.query.model_id as string | undefined
  if (modelId) {
    searchModelName.value = modelId
  }
  Promise.all([loadProviders(), loadLogs()])
})
</script>

<style scoped>
.status-log-page {
  padding: 0;
}

.page-header {
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0;
}

.filter-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
