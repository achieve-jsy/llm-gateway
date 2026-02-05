<template>
  <div class="ops-card" v-loading="loading">
    <div class="ops-card-header">
      <h3 class="ops-card-title">Token 使用统计</h3>
      <div class="ops-token-summary" v-if="summary">
        <span class="ops-stat-chip">请求总数 {{ formatNum(summary.total_requests) }}</span>
        <span class="ops-stat-chip">Token 总量 {{ formatNum(summary.total_tokens) }}</span>
      </div>
    </div>

    <!-- Filter bar -->
    <div class="ops-filter-bar">
      <el-select v-model="filters.timeRange" size="small" class="ops-filter-select">
        <el-option label="最近 30 分钟" value="30m" />
        <el-option label="最近 1 小时" value="1h" />
        <el-option label="最近 1 天" value="1d" />
        <el-option label="最近 7 天" value="7d" />
        <el-option label="最近 30 天" value="30d" />
      </el-select>
      <el-select v-model="viewMode" size="small" class="ops-filter-select-sm">
        <el-option label="Top N" value="top" />
        <el-option label="分页" value="paged" />
      </el-select>
      <el-select v-model="filters.topN" size="small" class="ops-filter-select-sm" v-if="viewMode === 'top'">
        <el-option label="Top 10" :value="10" />
        <el-option label="Top 20" :value="20" />
        <el-option label="Top 50" :value="50" />
      </el-select>
    </div>

    <!-- Table -->
    <div class="ops-table-wrap" v-if="items.length > 0">
      <table class="ops-table ops-table--compact">
        <thead>
          <tr>
            <th @click="sortBy('provider')">渠道</th>
            <th @click="sortBy('model')">模型</th>
            <th @click="sortBy('request_count')">请求数</th>
            <th @click="sortBy('prompt_tokens')">Prompt</th>
            <th @click="sortBy('completion_tokens')">Completion</th>
            <th @click="sortBy('total_tokens')">Total</th>
            <th @click="sortBy('avg_duration_ms')">平均耗时</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(item, idx) in items" :key="idx">
            <td>{{ item.provider }}</td>
            <td>{{ item.model }}</td>
            <td>{{ formatNum(item.request_count) }}</td>
            <td>{{ formatNum(item.prompt_tokens) }}</td>
            <td>{{ formatNum(item.completion_tokens) }}</td>
            <td>{{ formatNum(item.total_tokens) }}</td>
            <td>{{ item.avg_duration_ms }}ms</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else class="ops-card-empty">
      {{ loading ? '加载中...' : '暂无数据' }}
    </div>

    <div class="ops-pagination" v-if="viewMode === 'paged' && total > 0">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        small
        @current-change="fetchData"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { opsAPI } from '@/api/ops'
import type { OpsTokenStatsItem } from '@/api/ops'

const loading = ref(false)
const items = ref<OpsTokenStatsItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const viewMode = ref<'top' | 'paged'>('top')

const summary = ref<{ total_requests: number; total_tokens: number } | null>(null)

const props = withDefaults(
  defineProps<{
    refreshToken?: number
  }>(),
  { refreshToken: 0 }
)

const filters = reactive({
  timeRange: '1h',
  topN: 10,
  sortBy: 'total_tokens',
  sortOrder: 'desc' as 'asc' | 'desc',
})

watch(() => [filters.timeRange, filters.topN, viewMode.value], () => {
  page.value = 1
  fetchData()
})

watch(viewMode, () => {
  page.value = 1
  fetchData()
})

watch(() => props.refreshToken, () => {
  fetchData()
})

onMounted(fetchData)

function sortBy(field: string) {
  if (filters.sortBy === field) {
    filters.sortOrder = filters.sortOrder === 'desc' ? 'asc' : 'desc'
  } else {
    filters.sortBy = field
    filters.sortOrder = 'desc'
  }
  fetchData()
}

function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}

async function fetchData() {
  loading.value = true
  try {
    const result = await opsAPI.getTokenStats({
      time_range: filters.timeRange,
      top_n: viewMode.value === 'top' ? filters.topN : undefined,
      page: viewMode.value === 'paged' ? page.value : undefined,
      page_size: viewMode.value === 'paged' ? pageSize : undefined,
      sort_by: filters.sortBy,
      sort_order: filters.sortOrder,
    })
    items.value = result.items
    total.value = result.total
    summary.value = { total_requests: result.total_requests, total_tokens: result.total_tokens }
  } catch {
    ElMessage.error('加载 Token 统计失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.ops-card {
  background: var(--bg-card);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-3xl);
  padding: var(--space-4);
}

.ops-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--space-3);
  flex-wrap: wrap;
  gap: var(--space-2);
}

.ops-card-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
  margin: 0;
}

.ops-token-summary {
  display: flex;
  gap: 8px;
}

.ops-stat-chip {
  font-size: var(--font-size-xxs);
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--bg-subtle);
  color: var(--text-secondary);
}

.ops-filter-bar {
  display: flex;
  gap: 8px;
  margin-bottom: var(--space-3);
  flex-wrap: wrap;
}

.ops-filter-select {
  width: 140px;
}

.ops-filter-select-sm {
  width: 100px;
}

.ops-table-wrap {
  overflow-x: auto;
}

.ops-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-xs);
}

.ops-table th {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-light);
  color: var(--text-secondary);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
}

.ops-table th:hover {
  color: var(--text-primary);
}

.ops-table td {
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-light);
  color: var(--text-primary);
}

.ops-card-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 120px;
  color: var(--text-placeholder);
  font-size: var(--font-size-xs);
}

.ops-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-3);
}
</style>