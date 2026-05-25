<template>
  <div class="ops-card ops-top-issues" v-loading="loading">
    <div class="ops-card-header">
      <h3 class="ops-card-title">Top 问题路径</h3>
      <span class="ops-top-hint">近窗口 · 点击行可筛选</span>
    </div>

    <div class="ops-top-grid">
      <!-- Top 慢 -->
      <div class="ops-top-panel">
        <div class="ops-top-panel-title">Top 慢路径</div>
        <div class="ops-table-wrap" v-if="slow.length > 0">
          <table class="ops-table ops-table--compact">
            <thead>
              <tr>
                <th>#</th>
                <th>模型</th>
                <th>渠道</th>
                <th>P95</th>
                <th>Avg</th>
                <th>请求</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, idx) in slow"
                :key="'s-' + idx"
                class="ops-top-row"
                @click="onSelect(row.provider, row.model)"
              >
                <td class="ops-rank">{{ idx + 1 }}</td>
                <td class="ops-cell-ellipsis" :title="row.model">{{ row.model }}</td>
                <td class="ops-cell-ellipsis" :title="row.provider">{{ row.provider }}</td>
                <td :class="{ 'ops-lat--high': row.duration_p95_ms >= 5000 }">
                  {{ formatLatencyMs(row.duration_p95_ms) }}
                </td>
                <td>{{ formatLatencyMs(row.duration_avg_ms) }}</td>
                <td>{{ row.request_count }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="ops-card-empty ops-card-empty--sm">
          {{ loading ? '加载中...' : '暂无慢路径（需 ≥3 次请求）' }}
        </div>
      </div>

      <!-- Top 错误 -->
      <div class="ops-top-panel">
        <div class="ops-top-panel-title">Top 错误路径</div>
        <div class="ops-table-wrap" v-if="errors.length > 0">
          <table class="ops-table ops-table--compact">
            <thead>
              <tr>
                <th>#</th>
                <th>模型</th>
                <th>渠道</th>
                <th>错误</th>
                <th>错误率</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, idx) in errors"
                :key="'e-' + idx"
                class="ops-top-row"
                @click="onSelect(row.provider, row.model)"
              >
                <td class="ops-rank">{{ idx + 1 }}</td>
                <td class="ops-cell-ellipsis" :title="row.model">{{ row.model }}</td>
                <td class="ops-cell-ellipsis" :title="row.provider">{{ row.provider }}</td>
                <td class="ops-err-count">{{ row.error_count }}</td>
                <td :class="errorRateClass(row.error_rate)">
                  {{ (row.error_rate * 100).toFixed(1) }}%
                </td>
                <td>
                  <el-tag
                    v-if="row.top_status > 0"
                    size="small"
                    :type="row.top_status >= 500 ? 'danger' : 'warning'"
                    effect="plain"
                    round
                  >
                    {{ row.top_status }}
                  </el-tag>
                  <span v-else>—</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="ops-card-empty ops-card-empty--sm">
          {{ loading ? '加载中...' : '暂无错误路径' }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { opsAPI } from '@/api/ops'
import type { OpsTopSlowItem, OpsTopErrorItem, OpsFilterParams } from '@/api/ops'

const props = withDefaults(
  defineProps<{
    timeRange?: string
    groupId?: number | null
    provider?: string
    model?: string
    endpoint?: string
    customStart?: string | null
    customEnd?: string | null
    refreshToken?: number
  }>(),
  {
    timeRange: '1h',
    groupId: null,
    provider: '',
    model: '',
    endpoint: '',
    customStart: null,
    customEnd: null,
    refreshToken: 0,
  },
)

const emit = defineEmits<{
  'select-provider': [provider: string]
  'select-model': [model: string]
}>()

const loading = ref(false)
const slow = ref<OpsTopSlowItem[]>([])
const errors = ref<OpsTopErrorItem[]>([])

function buildFilter(): OpsFilterParams & { limit: number } {
  const filter: OpsFilterParams & { limit: number } = {
    time_range: props.timeRange,
    limit: 10,
  }
  if (props.timeRange === 'custom' && props.customStart && props.customEnd) {
    filter.start = props.customStart
    filter.end = props.customEnd
  }
  if (props.groupId != null) filter.group_id = props.groupId
  if (props.provider) filter.provider = props.provider
  if (props.model) filter.model = props.model
  if (props.endpoint) filter.endpoint = props.endpoint
  return filter
}

function formatLatencyMs(ms: number | null | undefined): string {
  const n = Number(ms)
  if (!Number.isFinite(n) || n <= 0) return '—'
  if (n < 1000) return `${Math.round(n)}ms`
  if (n < 60000) return `${(n / 1000).toFixed(1)}s`
  const m = Math.floor(n / 60000)
  const s = ((n % 60000) / 1000).toFixed(1)
  return `${m}m ${s}s`
}

function errorRateClass(rate: number): string {
  if (rate >= 0.1) return 'ops-err-rate--bad'
  if (rate >= 0.05) return 'ops-err-rate--warn'
  return ''
}

function onSelect(providerName: string, modelName: string) {
  if (providerName && providerName !== '(unknown)') {
    emit('select-provider', providerName)
  }
  if (modelName && modelName !== '(unknown)') {
    emit('select-model', modelName)
  }
}

async function fetchData() {
  loading.value = true
  try {
    const resp = await opsAPI.getTopIssues(buildFilter())
    slow.value = resp.slow || []
    errors.value = resp.errors || []
  } catch {
    slow.value = []
    errors.value = []
  } finally {
    loading.value = false
  }
}

watch(
  () => [
    props.refreshToken,
    props.timeRange,
    props.groupId,
    props.provider,
    props.model,
    props.endpoint,
    props.customStart,
    props.customEnd,
  ],
  () => fetchData(),
)

onMounted(() => fetchData())
</script>

<style scoped>
.ops-top-hint {
  font-size: var(--font-size-xxs);
  color: var(--text-placeholder);
}

.ops-top-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.ops-top-panel-title {
  font-size: 12px;
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  margin-bottom: 8px;
}

.ops-table-wrap {
  overflow-x: auto;
}

.ops-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.ops-table th {
  text-align: left;
  padding: 6px 8px;
  color: var(--text-placeholder);
  font-weight: var(--font-weight-medium);
  border-bottom: 1px solid var(--border-subtle, #e5e7eb);
  white-space: nowrap;
}

.ops-table td {
  padding: 7px 8px;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-subtle, #f3f4f6);
  font-variant-numeric: tabular-nums;
}

.ops-top-row {
  cursor: pointer;
  transition: background 0.15s ease;
}

.ops-top-row:hover {
  background: var(--bg-subtle, #f9fafb);
}

.ops-rank {
  color: var(--text-placeholder);
  width: 24px;
}

.ops-cell-ellipsis {
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ops-lat--high {
  color: #f59e0b;
  font-weight: var(--font-weight-semibold);
}

.ops-err-count {
  color: #ef4444;
  font-weight: var(--font-weight-semibold);
}

.ops-err-rate--warn {
  color: #f59e0b;
  font-weight: var(--font-weight-semibold);
}

.ops-err-rate--bad {
  color: #ef4444;
  font-weight: var(--font-weight-semibold);
}

.ops-card-empty--sm {
  padding: 24px 12px;
  font-size: 12px;
}

@media (max-width: 900px) {
  .ops-top-grid {
    grid-template-columns: 1fr;
  }
}
</style>
