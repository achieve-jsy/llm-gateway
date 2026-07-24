<template>
  <section aria-labelledby="risk-heading">
    <header class="section-header">
      <div>
        <h2 id="risk-heading">实时风险事件</h2>
        <p>
          事件由实时用量和实际阻断投影，风险消失后会自动移除。
          <span v-if="lastRefreshedLabel" class="header-meta">上次刷新 {{ lastRefreshedLabel }}</span>
        </p>
      </div>
      <div class="header-actions">
        <label class="auto-refresh" data-test="risk-auto-refresh">
          <el-switch v-model="autoRefresh" size="small" />
          <span>自动刷新 30s</span>
        </label>
        <el-button :loading="loading" @click="load">刷新</el-button>
      </div>
    </header>
    <BudgetKpiStrip :items="kpiItems" />
    <div class="filters budget-filter-bar" aria-label="风险筛选">
      <el-select v-model="filter.period_type" placeholder="全部周期" clearable data-test="risk-filter-period">
        <el-option label="日" value="daily" />
        <el-option label="周" value="weekly" />
        <el-option label="月" value="monthly" />
      </el-select>
      <el-select v-model="filter.dimension_type" placeholder="全部维度" clearable data-test="risk-filter-dimension">
        <el-option label="总预算" value="total" />
        <el-option label="模型等级" value="tier" />
      </el-select>
      <el-select v-model="filter.risk_type" placeholder="全部类型" clearable data-test="risk-filter-type">
        <el-option label="达到阈值" value="threshold_reached" />
        <el-option label="预算耗尽" value="budget_exhausted" />
        <el-option label="请求被阻断" value="request_blocked" />
      </el-select>
      <el-select v-model="filter.severity" placeholder="全部严重度" clearable data-test="risk-filter-severity">
        <el-option label="警告" value="warning" />
        <el-option label="严重" value="critical" />
      </el-select>
      <div class="keyword-input" data-test="risk-filter-q">
        <el-input
          v-model="keyword"
          clearable
          placeholder="搜索影响对象（人名/部门）"
        />
      </div>
    </div>
    <DataFreshnessNotice :stale="page.stale || summary.stale" :updated-at="page.data_as_of || summary.data_as_of" />
    <el-table
      v-loading="loading"
      :data="page.items"
      row-class-name="risk-row"
      :default-sort="defaultSort"
      @row-click="open"
      @sort-change="onSortChange"
    >
      <template #empty>
        <div class="risk-empty" data-test="risk-empty">
          <p>当前没有预算风险，系统持续监控中</p>
          <p class="risk-empty__meta">上次刷新：{{ lastRefreshedLabel || '—' }}</p>
        </div>
      </template>
      <el-table-column label="严重度" width="90">
        <template #default="{ row }">
          <el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'">
            {{ row.severity === 'critical' ? '严重' : '警告' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="scope_name" label="影响对象" min-width="150" />
      <el-table-column label="维度" min-width="130">
        <template #default="{ row }">
          {{ row.dimension_type === 'tier' ? (row.model_tier_name || '模型等级') : '总预算' }} / {{ periodLabel(row.period_type) }}
        </template>
      </el-table-column>
      <el-table-column label="额度 / 已用" min-width="180">
        <template #default="{ row }">
          {{ formatBudgetUSD(row.effective_limit_usd) }} / {{ formatBudgetUSD(row.used_usd) }}
        </template>
      </el-table-column>
      <el-table-column prop="usage_ratio" label="使用率" width="140" sortable="custom">
        <template #default="{ row }">
          <div class="usage-cell" :class="{ 'is-over': usageRatio(row) >= 1 }" data-test="risk-usage-bar">
            <el-progress
              :percentage="usageBarPercent(row)"
              :stroke-width="8"
              :color="usageColor(row)"
              :show-text="false"
            />
            <span class="usage-pct">{{ usagePercentLabel(row) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="限制来源" min-width="140">
        <template #default="{ row }">{{ limitSourceLabel(row.limit_source) }}</template>
      </el-table-column>
      <el-table-column prop="blocked_requests" label="阻断次数" width="110" sortable="custom" />
      <el-table-column prop="detected_at" label="发现时间" min-width="170" sortable="custom">
        <template #default="{ row }">{{ formatTime(row.detected_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="168" fixed="right">
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            data-test="risk-inline-adjust"
            :disabled="row.stale"
            @click.stop="adjustBudget(row)"
          >
            调整额度
          </el-button>
          <el-button link data-test="risk-inline-usage" @click.stop="viewUsage(row)">
            查看用量
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <div class="pagination-bar">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="page.total"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next"
        background
        data-test="budget-risk-pagination"
        @current-change="load"
        @size-change="onPageSizeChange"
      />
    </div>

    <el-drawer v-model="drawer" title="风险处置" size="min(620px, 100%)" class="budget-drawer">
      <template v-if="selected">
        <div class="risk-overview">
          <div>
            <span class="risk-overview__eyebrow">{{ selected.severity === 'critical' ? '严重风险' : '风险预警' }}</span>
            <h3>{{ selected.scope_name }}</h3>
            <p>{{ dimensionLabel(selected) }} · {{ periodLabel(selected.period_type) }}预算</p>
          </div>
          <el-tag :type="selected.severity === 'critical' ? 'danger' : 'warning'">
            {{ riskStatusLabel(selected) }}
          </el-tag>
        </div>

        <DataFreshnessNotice class="drawer-freshness" :stale="selected.stale" :updated-at="selected.data_as_of" />
        <el-alert
          v-if="selected.stale"
          class="stale-alert"
          title="数据已过期"
          description="请先刷新风险数据，再调整预算额度。"
          type="warning"
          :closable="false"
          show-icon
        />

        <section class="drawer-section" aria-labelledby="risk-chain-heading">
          <h4 id="risk-chain-heading">生效预算链</h4>
          <div class="budget-chain">
            <div
              v-for="row in chainRows"
              :key="row.label"
              class="budget-chain__row"
              :class="{ 'is-limiting': row.limiting }"
            >
              <span>{{ row.label }}</span>
              <strong>{{ row.detail }}</strong>
            </div>
          </div>
          <p class="chain-note">仅展示当前事件可确认的限制来源，不推导上级预算金额。</p>
        </section>

        <section class="drawer-section suggestion" aria-labelledby="risk-suggestion-heading">
          <div>
            <h4 id="risk-suggestion-heading">调整建议</h4>
            <p>按当前已用金额增加 15% 缓冲，建议额度</p>
          </div>
          <strong>{{ formatBudgetUSD(suggestedLimit) }}</strong>
        </section>

        <section class="drawer-section" aria-labelledby="risk-actions-heading">
          <h4 id="risk-actions-heading">处置方式</h4>
          <div class="disposition-actions">
            <button
              type="button"
              class="disposition-action"
              data-test="risk-adjust"
              :disabled="selected.stale"
              @click="adjustBudget(selected)"
            >
              <strong>调整额度</strong>
              <span>打开当前对象的预算配置</span>
            </button>
            <button type="button" class="disposition-action" :disabled="notifying" @click="notifyOwner">
              <strong>通知负责人</strong>
              <span>发送风险与预算维度提醒</span>
            </button>
            <button type="button" class="disposition-action" @click="viewUsage(selected)">
              <strong>查看用量</strong>
              <span>跳转到该对象的用量明细</span>
            </button>
          </div>
        </section>

        <div class="drawer-footer">
          <el-button @click="drawer = false">关闭</el-button>
          <el-button type="primary" data-test="risk-adjust-footer" :disabled="selected.stale" @click="adjustBudget(selected)">
            调整额度
          </el-button>
        </div>
      </template>
    </el-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import BudgetKpiStrip from '@/components/budget/BudgetKpiStrip.vue'
import DataFreshnessNotice from '@/components/common/DataFreshnessNotice.vue'
import { formatBudgetUSD } from '@/composables/useBudgetDisplay'
import { limitSourceLabel } from '@/utils/budget-risk-labels'
import { getBudgetRiskSummary, listBudgetRisks, notifyBudgetOwner } from '@/services/control-plane'
import type {
  BudgetRiskEvent,
  BudgetRiskFilter,
  BudgetRiskPage,
  BudgetRiskSortBy,
  BudgetRiskSummary,
} from '@/types/budget-risk'

const AUTO_KEY = 'budget-risk-auto-refresh'
const AUTO_MS = 30_000
const QUERY_PREFIX = 'rf_'

const emit = defineEmits<{ viewBudget: [event: BudgetRiskEvent] }>()
const router = useRouter()
const route = useRoute()

const loading = ref(false)
const notifying = ref(false)
const drawer = ref(false)
const selected = ref<BudgetRiskEvent | null>(null)
const keyword = ref('')
const sortBy = ref<BudgetRiskSortBy | ''>('')
const sortOrder = ref<'asc' | 'desc' | ''>('')
const currentPage = ref(1)
const pageSize = ref(20)
const lastRefreshedAt = ref<Date | null>(null)
const autoRefresh = ref(typeof localStorage !== 'undefined' ? localStorage.getItem(AUTO_KEY) !== '0' : true)

const filter = reactive<Pick<BudgetRiskFilter, 'period_type' | 'dimension_type' | 'risk_type' | 'severity'>>({})
const page = reactive<BudgetRiskPage>({ items: [], total: 0, data_as_of: '', stale: false })
const summary = reactive<BudgetRiskSummary>({
  total: 0, warning: 0, critical: 0, blocked_requests_total: 0, data_as_of: '', stale: false,
})

let keywordTimer: ReturnType<typeof setTimeout> | undefined
let refreshTimer: ReturnType<typeof setInterval> | undefined
let syncingFromQuery = false
const ready = ref(false)

const lastRefreshedLabel = computed(() =>
  lastRefreshedAt.value ? lastRefreshedAt.value.toLocaleString() : '',
)

const defaultSort = computed(() => {
  if (!sortBy.value || !sortOrder.value) return undefined
  return {
    prop: sortBy.value,
    order: sortOrder.value === 'asc' ? 'ascending' as const : 'descending' as const,
  }
})

const kpiItems = computed(() => [
  { key: 'total', label: '当前风险', value: summary.total, tone: 'neutral' as const },
  { key: 'critical', label: '严重风险', value: summary.critical, tone: 'danger' as const },
  { key: 'warning', label: '即将耗尽', value: summary.warning, tone: 'warning' as const },
  { key: 'blocked-events', label: '实际阻断', value: summary.by_type?.request_blocked ?? 0, tone: 'danger' as const },
  {
    key: 'blocked-requests',
    label: '阻断请求',
    value: summary.blocked_requests_total ?? 0,
    tone: 'danger' as const,
    note: '全量合计',
  },
])

const chainRows = computed(() => {
  if (!selected.value) return []
  return [
    {
      label: selected.value.scope_name,
      detail: `${formatBudgetUSD(selected.value.used_usd)} / ${formatBudgetUSD(selected.value.effective_limit_usd)}`,
      limiting: false,
    },
    {
      label: '限制来源',
      detail: limitSourceLabel(selected.value.limit_source),
      limiting: true,
    },
  ]
})

const suggestedLimit = computed(() =>
  Math.ceil(Number(selected.value?.used_usd ?? 0) * 1.15 * 100) / 100,
)

function queryStr(key: string): string {
  const raw = route.query[QUERY_PREFIX + key]
  return typeof raw === 'string' ? raw : ''
}

function hydrateFromQuery() {
  syncingFromQuery = true
  const period = queryStr('period')
  const dimension = queryStr('dimension')
  const riskType = queryStr('type')
  const severity = queryStr('severity')
  const q = queryStr('q')
  const pageRaw = Number(queryStr('page'))
  const sizeRaw = Number(queryStr('ps'))
  const sort = queryStr('sort')
  const order = queryStr('order')

  filter.period_type = (period || undefined) as BudgetRiskFilter['period_type']
  filter.dimension_type = (dimension || undefined) as BudgetRiskFilter['dimension_type']
  filter.risk_type = (riskType || undefined) as BudgetRiskFilter['risk_type']
  filter.severity = (severity || undefined) as BudgetRiskFilter['severity']
  keyword.value = q
  currentPage.value = Number.isFinite(pageRaw) && pageRaw > 0 ? pageRaw : 1
  pageSize.value = [10, 20, 50].includes(sizeRaw) ? sizeRaw : 20
  sortBy.value = (['usage_ratio', 'blocked_requests', 'detected_at'].includes(sort)
    ? sort
    : '') as BudgetRiskSortBy | ''
  sortOrder.value = order === 'asc' || order === 'desc' ? order : ''
  syncingFromQuery = false
}

function syncQuery() {
  const next: Record<string, string | undefined> = {
    ...Object.fromEntries(
      Object.entries(route.query).map(([k, v]) => [k, Array.isArray(v) ? v[0] : v ?? undefined]),
    ),
    [`${QUERY_PREFIX}period`]: filter.period_type || undefined,
    [`${QUERY_PREFIX}dimension`]: filter.dimension_type || undefined,
    [`${QUERY_PREFIX}type`]: filter.risk_type || undefined,
    [`${QUERY_PREFIX}severity`]: filter.severity || undefined,
    [`${QUERY_PREFIX}q`]: keyword.value.trim() || undefined,
    [`${QUERY_PREFIX}page`]: currentPage.value > 1 ? String(currentPage.value) : undefined,
    [`${QUERY_PREFIX}ps`]: pageSize.value !== 20 ? String(pageSize.value) : undefined,
    [`${QUERY_PREFIX}sort`]: sortBy.value || undefined,
    [`${QUERY_PREFIX}order`]: sortBy.value && sortOrder.value ? sortOrder.value : undefined,
  }
  // 清掉空值，避免 URL 堆满空参数
  for (const key of Object.keys(next)) {
    if (next[key] == null || next[key] === '') delete next[key]
  }
  void router.replace({ query: next })
}

function listParams(): BudgetRiskFilter {
  return {
    ...filter,
    q: keyword.value.trim() || undefined,
    sort_by: sortBy.value || undefined,
    sort_order: sortBy.value && sortOrder.value ? sortOrder.value : undefined,
    page: currentPage.value,
    page_size: pageSize.value,
  }
}

function summaryParams(): BudgetRiskFilter {
  return {
    ...filter,
    q: keyword.value.trim() || undefined,
  }
}

async function load() {
  loading.value = true
  try {
    const [nextPage, nextSummary] = await Promise.all([
      listBudgetRisks(listParams()),
      getBudgetRiskSummary(summaryParams()),
    ])
    Object.assign(page, nextPage)
    Object.assign(summary, nextSummary)
    lastRefreshedAt.value = new Date()
    syncQuery()
    const maxPage = Math.max(1, Math.ceil((nextPage.total || 0) / pageSize.value))
    if (currentPage.value > maxPage) {
      currentPage.value = maxPage
      await load()
    }
  } finally {
    loading.value = false
  }
}

function resetAndLoad() {
  if (syncingFromQuery) return
  currentPage.value = 1
  return load()
}

function onPageSizeChange() {
  currentPage.value = 1
  return load()
}

function onSortChange(payload: { prop: string; order: 'ascending' | 'descending' | null }) {
  if (!payload.order || !payload.prop) {
    sortBy.value = ''
    sortOrder.value = ''
  } else {
    sortBy.value = payload.prop as BudgetRiskSortBy
    sortOrder.value = payload.order === 'ascending' ? 'asc' : 'desc'
  }
  currentPage.value = 1
  return load()
}

function open(event: BudgetRiskEvent) {
  selected.value = event
  drawer.value = true
}

function adjustBudget(event: BudgetRiskEvent | null) {
  if (!event || event.stale) return
  emit('viewBudget', event)
  drawer.value = false
}

function viewUsage(event: BudgetRiskEvent | null) {
  if (!event) return
  router.push({
    path: '/admin/usage/dashboard',
    query: {
      scope_type: event.scope_type,
      scope_id: event.scope_id,
      model_tier_id: event.model_tier_id || undefined,
    },
  })
}

async function notifyOwner() {
  if (!selected.value) return
  notifying.value = true
  try {
    await notifyBudgetOwner({
      scope_type: selected.value.scope_type,
      scope_id: selected.value.scope_id,
      period_type: selected.value.period_type,
      dimension_type: selected.value.dimension_type,
      model_tier_id: selected.value.model_tier_id,
    })
    ElMessage.success('已通知预算负责人')
  } finally {
    notifying.value = false
  }
}

function usageRatio(event: BudgetRiskEvent): number {
  const value = Number(event.usage_ratio)
  return Number.isFinite(value) ? value : 0
}

function usageBarPercent(event: BudgetRiskEvent): number {
  return Math.min(100, Math.max(0, Math.round(usageRatio(event) * 100)))
}

function usagePercentLabel(event: BudgetRiskEvent): string {
  const pct = Math.round(usageRatio(event) * 1000) / 10
  return Number.isInteger(pct) ? `${pct}%` : `${pct.toFixed(1)}%`
}

function usageColor(event: BudgetRiskEvent): string {
  const ratio = usageRatio(event)
  if (ratio >= 1) return '#f56c6c'
  if (ratio >= 0.8) return '#e6a23c'
  return '#36B34E'
}

const periodLabel = (value: string) => ({ daily: '日', weekly: '周', monthly: '月' }[value] || value)
const dimensionLabel = (event: BudgetRiskEvent) =>
  event.dimension_type === 'tier' ? event.model_tier_name || '模型等级' : '总预算'
const riskStatusLabel = (event: BudgetRiskEvent) => {
  if (event.risk_type === 'request_blocked') return '已阻断'
  if (event.risk_type === 'budget_exhausted') return '预算耗尽'
  return '即将耗尽'
}
const formatTime = (value: string) => new Date(value).toLocaleString()

function setupAutoRefresh() {
  if (refreshTimer) clearInterval(refreshTimer)
  refreshTimer = undefined
  if (!autoRefresh.value) return
  refreshTimer = setInterval(() => {
    if (!loading.value) void load()
  }, AUTO_MS)
}

watch(
  () => [filter.period_type, filter.dimension_type, filter.risk_type, filter.severity],
  () => {
    if (!ready.value || syncingFromQuery) return
    void resetAndLoad()
  },
)

watch(keyword, () => {
  if (!ready.value || syncingFromQuery) return
  if (keywordTimer) clearTimeout(keywordTimer)
  keywordTimer = setTimeout(() => { void resetAndLoad() }, 300)
})

watch(autoRefresh, (enabled) => {
  if (!ready.value) return
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(AUTO_KEY, enabled ? '1' : '0')
  }
  setupAutoRefresh()
})

onMounted(async () => {
  hydrateFromQuery()
  await load()
  ready.value = true
  setupAutoRefresh()
})

onUnmounted(() => {
  if (keywordTimer) clearTimeout(keywordTimer)
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<style scoped>
section { display: grid; gap: 16px; }
.section-header { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.header-actions { display: flex; align-items: center; gap: 12px; flex-shrink: 0; }
.auto-refresh { display: inline-flex; align-items: center; gap: 8px; color: var(--el-text-color-secondary); font-size: 13px; cursor: pointer; }
.header-meta { margin-left: 8px; }
h2 { margin: 0 0 6px; font-size: 18px; }
p { margin: 0; color: var(--el-text-color-secondary); }
.filters { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.filters .el-select { width: 150px; }
.keyword-input { width: 220px; }
.keyword-input :deep(.el-input) { width: 100%; }
.pagination-bar { display: flex; justify-content: flex-end; }
.risk-row { cursor: pointer; }
.usage-cell { display: grid; grid-template-columns: 1fr auto; gap: 8px; align-items: center; }
.usage-pct { font-variant-numeric: tabular-nums; font-size: 12px; color: var(--el-text-color-regular); min-width: 3.2em; text-align: right; }
.usage-cell.is-over .usage-pct { color: var(--el-color-danger); font-weight: 600; }
.risk-empty { padding: 28px 12px; color: var(--el-text-color-secondary); }
.risk-empty p { margin: 0; }
.risk-empty__meta { margin-top: 8px; font-size: 12px; }
.risk-overview { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.risk-overview__eyebrow { color: var(--el-text-color-secondary); font-size: 12px; }
.risk-overview h3 { margin: 4px 0; font-size: 20px; }
.drawer-freshness, .stale-alert, .drawer-section { margin-top: 20px; }
.drawer-section { display: grid; gap: 12px; }
.drawer-section h4 { margin: 0; color: var(--el-text-color-primary); font-size: 15px; }
.budget-chain { overflow: hidden; border: 1px solid var(--budget-panel-border); border-radius: var(--budget-panel-radius); }
.budget-chain__row { display: flex; justify-content: space-between; gap: 16px; padding: 12px 14px; border-bottom: 1px solid var(--budget-row-border); }
.budget-chain__row:last-child { border-bottom: 0; }
.budget-chain__row.is-limiting { background: var(--budget-stale-bg); }
.budget-chain__row span { color: var(--el-text-color-secondary); }
.chain-note { font-size: 12px; }
.suggestion { grid-template-columns: 1fr auto; align-items: center; padding: 14px; background: var(--el-fill-color-light); border-radius: var(--budget-panel-radius); }
.suggestion h4 { margin-bottom: 4px; }
.suggestion > strong { color: var(--budget-status-warning); font-size: 22px; font-variant-numeric: tabular-nums; }
.disposition-actions { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.disposition-action { display: grid; gap: 4px; padding: 14px; color: var(--el-text-color-primary); background: var(--el-bg-color); border: 1px solid var(--budget-panel-border); border-radius: var(--budget-panel-radius); font: inherit; text-align: left; cursor: pointer; }
.disposition-action:hover:not(:disabled), .disposition-action:focus-visible { border-color: var(--el-color-primary); background: var(--el-fill-color-light); }
.disposition-action:disabled { cursor: not-allowed; opacity: .55; }
.disposition-action span { color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.5; }
.drawer-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 24px; padding-top: 16px; border-top: 1px solid var(--budget-row-border); }
@media (max-width: 768px) {
  .filters .el-select, .keyword-input { width: calc(50% - 5px); }
  .disposition-actions { grid-template-columns: 1fr; }
  .header-actions { flex-direction: column; align-items: flex-end; }
}
</style>
