<!--
  UsageLogSection — 调用明细复合组件
  ====================================
  封装了：段落标题 + 导出按钮 + UsageRecordTable + UsageTraceDrawer 状态管理。
  各页面只需传入 API 路径、筛选参数和表格配置即可。

  使用示例：
    <UsageLogSection
      title="调用明细"
      api-path="/api/v1/my/usage/logs"
      trace-api-path="/api/v1/my/usage/logs"
      :params="logParams"
      :table-props="{ showKey: true, showGroup: true, traceMetaMode: 'personal' }"
      :show-export="true"
    />
-->
<template>
  <div class="uls-wrap">
    <!-- 段落标题栏 -->
    <div class="uls-header">
      <div class="uls-title-group">
        <span class="uls-title">{{ title }}</span>
        <span v-if="total > 0" class="uls-count">共 {{ total }} 条</span>
      </div>
      <el-button
        v-if="showExport"
        :icon="Download"
        size="small"
        :disabled="!rows.length"
        @click="handleExport"
      >
        导出{{ rows.length ? ` (${rows.length})` : '' }}
      </el-button>
    </div>

    <!-- 表格 -->
    <UsageRecordTable
      v-bind="mergedTableProps"
      :rows="rows"
      :loading="loading"
      :total="total"
      :page="page"
      :page-size="pageSize"
      :currency="currency"
      :trace-endpoint="fetchTrace"
      @update:page="onPageChange"
      @update:page-size="onPageSizeChange"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, type PropType } from 'vue'
import { Download } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import http from '@/services/http'
import { useSettingsStore } from '@/stores/settings'
import type { Currency } from '@/utils/currency'
import type { LogRow, TraceData } from '@/types/usage-log'
import { useLogExport, type ExportColumn } from '@/composables/useLogExport'
import UsageRecordTable from './UsageRecordTable.vue'

// ── Props ──
const props = defineProps({
  /** 段落标题 */
  title: { type: String, default: '调用明细' },

  /** 日志列表 API 路径，如 /api/v1/my/usage/logs */
  apiPath: { type: String, required: true },

  /** Trace 详情 API 基础路径，如 /api/v1/my/usage/logs
   *  最终拼接为 {traceApiPath}/{requestId}/trace */
  traceApiPath: { type: String, required: true },

  /** 传给 API 的筛选参数（响应式），变更时自动重新加载 */
  params: { type: Object as PropType<Record<string, any>>, default: () => ({}) },

  /** 透传给 UsageRecordTable 的 props（列可见性、排序等） */
  tableProps: { type: Object as PropType<Record<string, any>>, default: () => ({}) },

  /** 是否显示导出按钮 */
  showExport: { type: Boolean, default: true },

  /** 自定义导出列 */
  exportColumns: { type: Array as PropType<ExportColumn[]>, default: undefined },

  /** 导出文件名前缀 */
  exportPrefix: { type: String, default: 'logs' },

  /** 货币 */
  currency: { type: String as PropType<Currency>, default: 'USD' },

  /** 初始页大小 */
  initialPageSize: { type: Number, default: 50 },
})

// ── 内部状态 ──
const loading = ref(false)
const rows = ref<LogRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(props.initialPageSize)

const settingsStore = useSettingsStore()

// ── 合并 tableProps ──
const mergedTableProps = computed(() => ({
  pageSizes: [20, 50, 100, 200],
  paginationLayout: 'total, sizes, prev, pager, next, jumper',
  ...props.tableProps,
}))

// ── API 调用 ──
async function loadLogs() {
  loading.value = true
  try {
    const queryParams: Record<string, any> = {
      ...props.params,
      page: page.value,
      page_size: pageSize.value,
    }
    const r = await http.get(props.apiPath, { params: queryParams })
    rows.value = r.data?.rows ?? []
    total.value = r.data?.total ?? 0
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.details || '明细查询失败')
    rows.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function fetchTrace(requestId: string): Promise<TraceData> {
  const res = await http.get(
    `${props.traceApiPath}/${requestId}/trace`,
    { _silent: true } as any,
  )
  return res.data
}

// ── 分页 ──
function onPageChange(p: number) {
  page.value = p
  loadLogs()
}

function onPageSizeChange(s: number) {
  pageSize.value = s
  page.value = 1
  loadLogs()
}

// ── 导出 ──
const { handleExport } = useLogExport(rows, {
  columns: props.exportColumns,
  filename: () => {
    const raw = props.params?.from ?? props.params?.start
    const datePart = raw
      ? String(raw).slice(0, 10)
      : new Date().toISOString().slice(0, 10)
    return `${props.exportPrefix}_${props.currency}_${datePart}`
  },
})

// ── 外部触发刷新 ──
function refresh() {
  page.value = 1
  loadLogs()
}

// ── 暴露给父组件 ──
defineExpose({
  rows, total, page, pageSize,
  refresh, loadLogs,
})

// ── 监听 params 变更自动刷新（deep watch）──
watch(
  () => props.params,
  () => refresh(),
  { deep: true },
)
</script>

<style scoped>
.uls-wrap {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.uls-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px 12px;
}

.uls-title-group {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.uls-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.uls-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}
</style>
