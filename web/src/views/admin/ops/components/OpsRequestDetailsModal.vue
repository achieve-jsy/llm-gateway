<template>
  <el-dialog
    :model-value="show"
    title="请求明细"
    width="1000px"
    top="3vh"
    @update:model-value="$emit('update:show', $event)"
  >
    <!-- Filters -->
    <div class="ops-modal-filters">
      <el-select v-model="filters.statusCode" size="small" class="ops-filter-select" clearable placeholder="状态码">
        <el-option label="全部" value="" />
        <el-option label="200" :value="200" />
        <el-option label="400" :value="400" />
        <el-option label="401" :value="401" />
        <el-option label="403" :value="403" />
        <el-option label="429" :value="429" />
        <el-option label="500" :value="500" />
        <el-option label="502" :value="502" />
        <el-option label="503" :value="503" />
        <el-option label="504" :value="504" />
      </el-select>
      <el-select v-model="filters.provider" size="small" class="ops-filter-select" clearable placeholder="渠道" allow-create filterable />
      <el-input v-model="filters.model" size="small" class="ops-filter-input-sm" clearable placeholder="模型" />
      <el-input v-model="filters.search" size="small" class="ops-filter-input" clearable placeholder="搜索..." />
      <el-input-number
        v-model="filters.minDuration"
        size="small"
        :min="0"
        :step="100"
        placeholder="最小耗时 ms"
        controls-position="right"
        class="ops-filter-input-num"
      />
    </div>

    <!-- Sort presets -->
    <div class="ops-sort-bar">
      <span class="ops-sort-label">排序:</span>
      <el-radio-group v-model="sortPreset" size="small" @change="applySortPreset">
        <el-radio-button value="time">按时间</el-radio-button>
        <el-radio-button value="duration">按耗时</el-radio-button>
        <el-radio-button value="tokens">按 Token</el-radio-button>
      </el-radio-group>
    </div>

    <!-- Table -->
    <div v-loading="loading" class="ops-modal-table">
      <table class="ops-table ops-table--compact" v-if="items.length > 0">
        <thead>
          <tr>
            <th>时间</th>
            <th>状态</th>
            <th>模型</th>
            <th>渠道</th>
            <th>渠道组</th>
            <th>耗时</th>
            <th>Tokens</th>
            <th>request_id</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td class="ops-td-time">{{ formatTime(item.ts) }}</td>
            <td>
              <el-tag size="small" :type="statusType(item.status)" effect="plain">{{ item.status }}</el-tag>
            </td>
            <td>{{ item.model }}</td>
            <td>{{ item.provider }}</td>
            <td>{{ item.group_name || '-' }}</td>
            <td>{{ item.duration_ms }}ms</td>
            <td>{{ formatNum(item.total_tokens) }}</td>
            <td>
              <span
                v-if="item.request_id"
                class="ops-request-id"
                @click.stop="copyRequestId(item.request_id!)"
                title="点击复制"
              >
                {{ truncate(item.request_id, 16) }}
              </span>
              <span v-else class="ops-td-dash">-</span>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="ops-modal-empty">
        {{ loading ? '加载中...' : '暂无数据' }}
      </div>
    </div>

    <!-- Pagination -->
    <div class="ops-pagination" v-if="total > 0">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        small
        @current-change="fetchData"
      />
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { opsAPI } from '@/api/ops'
import type { OpsRequestDetail } from '@/api/ops'

const props = defineProps<{
  show: boolean
  timeRange: string
}>()

defineEmits<{
  'update:show': [val: boolean]
}>()

const loading = ref(false)
const items = ref<OpsRequestDetail[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 40
const sortPreset = ref('time')

const filters = reactive({
  statusCode: undefined as number | undefined,
  provider: '',
  model: '',
  search: '',
  minDuration: undefined as number | undefined,
})

watch(() => props.show, (val) => {
  if (val) {
    page.value = 1
    fetchData()
  }
})

watch(() => [filters.statusCode, filters.provider, filters.model], () => {
  page.value = 1
  fetchData()
})

function applySortPreset() {
  page.value = 1
  fetchData()
}

function sortParams(): { sort_by: string; sort_order: 'asc' | 'desc' } {
  if (sortPreset.value === 'duration') return { sort_by: 'duration_ms', sort_order: 'desc' }
  if (sortPreset.value === 'tokens') return { sort_by: 'total_tokens', sort_order: 'desc' }
  return { sort_by: 'ts', sort_order: 'desc' }
}

function statusType(status: number): 'success' | 'danger' | 'warning' | 'info' {
  if (status >= 200 && status < 300) return 'success'
  if (status >= 500) return 'danger'
  if (status >= 400) return 'warning'
  return 'info'
}

function formatTime(ts: string): string {
  const d = new Date(ts)
  const M = (d.getMonth() + 1).toString().padStart(2, '0')
  const D = d.getDate().toString().padStart(2, '0')
  const h = d.getHours().toString().padStart(2, '0')
  const m = d.getMinutes().toString().padStart(2, '0')
  const s = d.getSeconds().toString().padStart(2, '0')
  return `${M}-${D} ${h}:${m}:${s}`
}

function truncate(s: string, n: number): string {
  if (!s) return ''
  return s.length > n ? s.slice(0, n) + '...' : s
}

function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}

function copyRequestId(rid: string) {
  navigator.clipboard.writeText(rid).then(() => {
    ElMessage.success('request_id 已复制')
  }).catch(() => {
    ElMessage.warning('复制失败，请手动复制')
  })
}

async function fetchData() {
  if (!props.show) return
  loading.value = true
  try {
    const sort = sortParams()
    const res = await opsAPI.listRequests({
      time_range: props.timeRange,
      page: page.value,
      page_size: pageSize,
      sort_by: sort.sort_by,
      sort_order: sort.sort_order,
      status: filters.statusCode,
      provider: filters.provider || undefined,
      model: filters.model || undefined,
      search: filters.search || undefined,
      min_duration_ms: filters.minDuration,
    })
    items.value = res.items
    total.value = res.total
  } catch {
    ElMessage.error('加载请求列表失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.ops-modal-filters {
  display: flex;
  gap: 8px;
  margin-bottom: var(--space-3, 12px);
  flex-wrap: wrap;
}

.ops-filter-select {
  width: 130px;
}

.ops-filter-input {
  width: 180px;
}

.ops-filter-input-sm {
  width: 130px;
}

.ops-filter-input-num {
  width: 150px;
}

.ops-sort-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: var(--space-3, 12px);
}

.ops-sort-label {
  font-size: var(--font-size-xs, 12px);
  color: var(--text-secondary, #64748b);
}

.ops-modal-table {
  overflow-x: auto;
  max-height: 65vh;
}

.ops-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-xs, 12px);
}

.ops-table th {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-light, #e5e7eb);
  color: var(--text-secondary, #64748b);
  font-weight: 600;
  white-space: nowrap;
  position: sticky;
  top: 0;
  background: var(--bg-surface, #fff);
  z-index: 1;
}

.ops-table td {
  padding: 6px 10px;
  border-bottom: 1px solid var(--border-light, #e5e7eb);
  color: var(--text-primary, #1e293b);
}

.ops-td-time {
  white-space: nowrap;
  color: var(--text-secondary, #64748b);
  font-size: 11px;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
}

.ops-td-dash {
  color: var(--text-placeholder, #94a3b8);
}

.ops-request-id {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 11px;
  color: #3b82f6;
  cursor: pointer;
  text-decoration: underline;
  text-decoration-style: dotted;
}

.ops-request-id:hover {
  color: #2563eb;
}

.ops-modal-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 160px;
  color: var(--text-placeholder, #94a3b8);
}

.ops-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-3, 12px);
}
</style>