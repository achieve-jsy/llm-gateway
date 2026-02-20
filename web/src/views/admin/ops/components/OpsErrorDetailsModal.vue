<template>
  <el-dialog
    :model-value="show"
    :title="titleText"
    width="900px"
    top="4vh"
    @update:model-value="$emit('update:show', $event)"
  >
    <!-- Filters inside modal -->
    <div class="ops-modal-filters">
      <el-select v-model="filters.statusCode" size="small" class="ops-filter-select" clearable placeholder="状态码">
        <el-option label="全部" value="" />
        <el-option label="400" :value="400" />
        <el-option label="401" :value="401" />
        <el-option label="403" :value="403" />
        <el-option label="404" :value="404" />
        <el-option label="429" :value="429" />
        <el-option label="500" :value="500" />
        <el-option label="502" :value="502" />
        <el-option label="503" :value="503" />
        <el-option label="504" :value="504" />
        <el-option label="529" :value="529" />
      </el-select>
      <el-select v-model="filters.provider" size="small" class="ops-filter-select" clearable placeholder="渠道" allow-create filterable />
      <el-input v-model="filters.search" size="small" class="ops-filter-input" clearable placeholder="搜索..." @keyup.enter="fetchData" />
    </div>

    <!-- Table -->
    <div v-loading="loading" class="ops-modal-table">
      <table class="ops-table ops-table--compact" v-if="items.length > 0">
        <thead>
          <tr>
            <th>时间</th>
            <th>状态</th>
            <th>渠道</th>
            <th>模型</th>
            <th>耗时</th>
            <th>错误信息</th>
            <th v-if="errorType === 'request'">request_id</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.request_id || item.id"
            class="ops-table-row"
            @click="$emit('open-detail', item.request_id || item.id)"
          >
            <td class="ops-td-time">{{ formatTime(item.ts) }}</td>
            <td>
              <el-tag size="small" :type="statusType(item.status)" effect="plain">{{ item.status }}</el-tag>
            </td>
            <td>{{ item.provider }}</td>
            <td>{{ item.model }}</td>
            <td>{{ item.duration_ms }}ms</td>
            <td class="ops-td-msg">{{ truncate(item.error_message, 60) }}</td>
            <td v-if="errorType === 'request'">{{ (item as any).request_id || '-' }}</td>
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

const props = defineProps<{
  show: boolean
  timeRange: string
  errorType: 'request' | 'upstream'
}>()

defineEmits<{
  'update:show': [val: boolean]
  // B05/WP3: open-detail 携带 request_id（字符串），detail 端点按 request_id 查。
  'open-detail': [id: string]
}>()

const titleText = ref(props.errorType === 'request' ? '请求错误列表' : '上游错误列表')

const loading = ref(false)
const items = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 30

const filters = reactive({
  statusCode: undefined as number | undefined,
  provider: '',
  search: '',
})

watch(() => props.show, (val) => {
  if (val) {
    page.value = 1
    fetchData()
  }
})

watch(() => [filters.statusCode, filters.provider], () => {
  page.value = 1
  fetchData()
})

function statusType(status: number): 'danger' | 'warning' | 'info' {
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
  return `${M}-${D} ${h}:${m}`
}

function truncate(s: string | null, n: number): string {
  if (!s) return '-'
  return s.length > n ? s.slice(0, n) + '...' : s
}

async function fetchData() {
  if (!props.show) return
  loading.value = true
  try {
    const params = {
      time_range: props.timeRange,
      page: page.value,
      page_size: pageSize,
      status: filters.statusCode,
      provider: filters.provider || undefined,
      search: filters.search || undefined,
    }
    if (props.errorType === 'request') {
      const res = await opsAPI.listRequestErrors(params)
      items.value = res.items
      total.value = res.total
    } else {
      const res = await opsAPI.listUpstreamErrors(params)
      items.value = res.items
      total.value = res.total
    }
  } catch {
    ElMessage.error('加载数据失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>

.ops-filter-select {
  width: 140px;
}

.ops-filter-input {
  width: 200px;
}






.ops-table-row:hover {
  background: var(--bg-subtle, #f8fafc);
}


.ops-td-msg {
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #ef4444;
}


</style>