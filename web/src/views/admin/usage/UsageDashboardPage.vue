<template>
  <div class="ud">
    <DashboardHeader
      v-model:currency="currency"
      :scope-label="scopeLabel"
      :loading="summaryLoading || logsLoading"
      @refresh="activeTab === 'video' ? (videoRefreshToken++) : loadAll()"
    />
    <UsageFilterBar
      page-role="dashboard"
      :capability="(activeTab as any)"
      :date-presets="datePresets"
      v-model:date-range="dateRange"
      v-model:active-preset="activePreset"
      v-model:model-name="filterModel"
      v-model:status="filterStatus"
      v-model:key-type="filterKeyType"
      v-model:user-name="filterUserName"
      v-model:provider="filterProvider"
      v-model:tier="filterTier"
      v-model:dept-name="filterDept"
      v-model:studio-id="filterStudio"
      :model-options="modelOptions"
      :provider-options="providerOptions"
      :dept-options="deptOptions"
      :studio-options="studioOptions"
      :active-count="activeFilterCount"
      @apply-preset="applyPreset"
      @change="onFilterChange"
      @date-change="onDateChange"
      @clear="clearFilters"
    >
      <template #tabs>
        <div class="usage-type-tabs">
          <el-radio-group v-model="activeTab" size="default" @change="onTabChange">
            <el-radio-button value="chat">Chat</el-radio-button>
            <el-radio-button value="embedding">Embedding</el-radio-button>
            <el-radio-button value="rerank">Rerank</el-radio-button>
            <el-radio-button value="rerank_vl">Rerank VL</el-radio-button>
            <el-radio-button value="video">Video</el-radio-button>
          </el-radio-group>
        </div>
      </template>
    </UsageFilterBar>

    <template v-if="activeTab === 'video'">
      <SeedanceVideoTabs
        :refresh-token="videoRefreshToken"
        :start="videoDateStart"
        :end="videoDateEnd"
        :model-name="videoModelParam"
        :provider-name="filterProvider"
        :task-status="filterStatus"
        api-path="/api/v1/usage/scoped/logs"
        trace-api-path="/api/v1/usage/logs"
        :params="logParams"
        :currency="currency"
        :table-props="logTableProps"
      />
    </template>
    <template v-else>
      <KpiCards
        id="sec-overview"
        :summary="summary"
        :loading="summaryLoading"
        :currency="currency"
        :cost-change="costChange"
        :calls-change="callsChange"
        :prev-period-label="prevPeriodLabel"
        :current-budget="currentBudget"
        :budget-level="budgetLevel"
        :current-scope="currentScope"
        :avg-cost-per-call="avgCostPerCall"
        :endpoint-type="logEndpointType"
      />
      <TrendChartSection
        :loading="trendLoading"
        :trend-data="trendData"
        :prev-trend-data="isCompactUsageTab ? [] : prevTrendData"
        v-model:trend-metric="trendMetric"
        v-model:trend-granularity="trendGranularity"
        :currency="currency"
        :compact="isCompactUsageTab"
      />
      <CostBreakdownSection
        v-if="!isCompactUsageTab"
        :show="showCostBreakdown"
        :current-scope="currentScope"
        :date-range="dateRange"
        :budget-pool-items="budgetPoolItems"
        :top5-breakdown="top5Breakdown"
        :top5-max="top5Max"
        :total-cost="totalCostForBreakdown"
        :currency="currency"
      />
      <div v-if="!isCompactUsageTab" class="ud-main-row">
        <RankingTable
          id="sec-dept"
          :loading="summaryLoading"
          :displayed-breakdown="displayedBreakdown"
          :current-scope="currentScope"
          :can-drill="canDrill"
          :breadcrumbs="breadcrumbs"
          :top-label="topLabel"
          :rank-title="rankTitle"
          :rank-total="rankTotal"
          :rank-page-size="rankPageSize"
          :rank-sort="rankSort"
          :total-cost="totalCost"
          :currency="currency"
          :user-top-models="userTopModels"
          :enhanced-user-map="enhancedUserMap"
          v-model:rank-page="rankPage"
          v-model:rank-search="rankSearch"
          @drill-down="drillDown"
          @toggle-sort="toggleSort"
          @reset-to-top="resetToTop"
          @pop-to-level="popToLevel"
          @load-summary="loadSummary"
          @user-click="handleUserClick"
        />
        <ModelChartSection
          id="sec-models"
          :loading="modelsLoading"
          :models-data="modelsData"
          v-model:model-metric="modelMetric"
          v-model:model-chart-type="modelChartType"
          :currency="currency"
          @switch-metric="switchModelMetric"
          @switch-chart-type="switchModelChartType"
          @model-click="handleModelClick"
        />
        <LongTailAlerts
          :models-data="modelsData"
          :currency="currency"
          :sort-by="ltSortBy"
          :sort-order="ltSortOrder"
          @model-click="handleModelClick"
          @update:sort-by="ltSortBy = $event"
          @update:sort-order="ltSortOrder = $event"
        />
      </div>
      <Transition name="ud-slide">
        <UserDetailPanel
          v-if="!isCompactUsageTab && userType"
          :user-id="userType.id"
          :user-name="panelUserName"
          :dept-name="panelDeptName"
          :date-range="dateRange"
          :currency="currency"
          @model-click="handleModelClickFromPanel"
          @close="closePanel"
        />
      </Transition>
      <Transition name="md-slide">
        <ModelDetailPanel
          v-if="!isCompactUsageTab && modelType"
          :model-name="modelType.id"
          :date-range="dateRange"
          :currency="currency"
          :team-avg="teamAvg"
          @user-click="handleUserClickFromModelPanel"
          @close="closePanel"
        />
      </Transition>
      <UsageLogSection
        ref="logSectionRef"
        id="sec-users"
        title="调用明细"
        api-path="/api/v1/usage/scoped/logs"
        trace-api-path="/api/v1/usage/logs"
        :params="logParams"
        :currency="currency"
        export-prefix="scoped_logs"
        :table-props="logTableProps"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import DashboardHeader from './components/DashboardHeader.vue'
import KpiCards from './components/KpiCards.vue'
import TrendChartSection from './components/TrendChartSection.vue'
import CostBreakdownSection from './components/CostBreakdownSection.vue'
import RankingTable from './components/RankingTable.vue'
import ModelChartSection from './components/ModelChartSection.vue'
import LongTailAlerts from './components/LongTailAlerts.vue'
import UsageLogSection from '@/components/usage/UsageLogSection.vue'
import UsageFilterBar from '@/components/usage/UsageFilterBar.vue'
import SeedanceVideoTabs from '@/components/usage/SeedanceVideoTabs.vue'
import UserDetailPanel from './components/UserDetailPanel.vue'
import ModelDetailPanel from './components/ModelDetailPanel.vue'
import { usePanelManager } from '@/composables/usePanelManager'
import { useAuthStore } from '@/stores/auth'
import { useOrgStore } from '@/stores/org'
import { useRoute } from 'vue-router'
import http from '@/services/http'
import { useSettingsStore } from '@/stores/settings'
import { fetchUsersRanking } from '@/api/dashboard-enhanced'
import type { UserRankRow } from '@/types/dashboard-enhanced'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'
import type { UsageEndpointType } from '@/utils/usage-endpoint'
import {
  countActiveFilters,
  isStatusCompatible,
  videoModelNameParam,
} from '@/utils/usageFilterConfig'

const settingsStore = useSettingsStore()
const currency = ref<Currency>('CNY')
const cc = computed(() => ({
  currency: currency.value,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const { userType, modelType, closePanel, togglePanel, jumpToModel, jumpToUser } = usePanelManager()

const authStore = useAuthStore()
const orgStore  = useOrgStore()
const route     = useRoute()

// ── 日期 ──────────────────────────────────────────────────────
const fmt = (d: Date) => d.toISOString().split('T')[0]
const today = new Date()
const daysAgo = (n: number) => { const d = new Date(today); d.setDate(d.getDate() - n); return d }

const dateRange = ref<[string, string]>([fmt(daysAgo(29)), fmt(today)])
const activePreset = ref('近30天')
const videoDateStart = computed(() => dateRange.value?.[0] || '')
const videoDateEnd = computed(() => dateRange.value?.[1] || '')

const datePresets = [
  { label: '今天',   days: 0 },
  { label: '近7天',  days: 6 },
  { label: '近30天', days: 29 },
  { label: '近90天', days: 89 },
]

function applyPreset(p: { label: string; days: number }) {
  dateRange.value = [fmt(daysAgo(p.days)), fmt(today)]
  activePreset.value = p.label
  autoSetGranularity()
  bumpVideoOrLoadAll()
}

function onDateChange() {
  activePreset.value = ''
  autoSetGranularity()
  bumpVideoOrLoadAll()
}

// ── 筛选 ──────────────────────────────────────────────────────
const filterUserName = ref('')
const filterModel    = ref('')
const filterStatus   = ref('')
const filterKeyType  = ref('')
const filterEndpoint = ref('')
const filterProvider = ref('')
const filterTier     = ref('')
const filterDept     = ref('')
const filterStudio   = ref('')
const providerOptions = ref<string[]>([])
const deptOptions = ref<Array<{ label: string; value: string }>>([])
const studioOptions = ref<Array<{ label: string; value: string }>>([])
const allModelOptions = ref<string[]>([])
const activeTab = ref('chat')
const videoRefreshToken = ref(0)
const videoModelParam = computed(() =>
  videoModelNameParam(undefined, filterModel.value),
)

const logEndpointType = computed((): UsageEndpointType => {
  if (activeTab.value === 'embedding' || activeTab.value === 'rerank' || activeTab.value === 'rerank_vl') return activeTab.value
  return 'chat'
})

const isCompactUsageTab = computed(() =>
  activeTab.value === 'embedding' || activeTab.value === 'rerank' || activeTab.value === 'rerank_vl',
)

const logTableProps = computed(() => ({
  showKey: !isCompactUsageTab.value,
  showKeyType: !isCompactUsageTab.value && authStore.effectiveRole !== 'user',
  showUser: !isCompactUsageTab.value,
  userLabel: '成员',
  showStudio: !isCompactUsageTab.value && authStore.effectiveRole === 'admin',
  showDept: !isCompactUsageTab.value && (authStore.effectiveRole === 'admin' || authStore.effectiveRole === 'studio_admin'),
  showProvider: true,
  showSource: isCompactUsageTab.value ? false : 'ip-only',
  sortableColumns: ['duration_ms', 'est_cost_usd', 'ts'] as const,
  pageSizes: [20, 50, 100],
  traceMetaMode: 'full' as const,
  endpointType: logEndpointType.value,
}))

function bumpVideoOrLoadAll() {
  if (activeTab.value === 'video') {
    videoRefreshToken.value++
    return
  }
  loadAll()
}

function onFilterChange() {
  if (filterUserName.value) {
    onUserNameChange()
    return
  }
  bumpVideoOrLoadAll()
}

function onTabChange(value: string | number | boolean) {
  const tab = String(value)
  const endpointMap: Record<string, string> = {
    chat: '/v1/chat/completions',
    embedding: '/v1/embeddings',
    rerank: '/v1/rerank',
    video: '/v1/contents/generations/tasks',
  }
  filterEndpoint.value = endpointMap[tab] || ''
  if (!isStatusCompatible(tab as any, filterStatus.value)) {
    filterStatus.value = ''
  }
  if (tab !== 'video') filterProvider.value = ''
  if (tab === 'video') {
    videoRefreshToken.value++
    return
  }
  // Embedding/Rerank：原型仅展示请求量趋势，强制 calls 并收起 Chat 成本区
  if (tab === 'embedding' || tab === 'rerank') {
    trendMetric.value = 'calls'
    modelMetric.value = 'calls'
    closePanel()
  } else if (trendMetric.value === 'calls' && tab === 'chat') {
    trendMetric.value = 'all'
  }
  loadAll()
}

// ── 长尾告警排序 ──
const ltSortBy    = ref<'calls' | 'cost' | 'error_rate'>('calls')
const ltSortOrder = ref<'asc' | 'desc'>('desc')

const activeFilterCount = computed(() =>
  countActiveFilters('dashboard', activeTab.value as any, {
    userName: filterUserName.value,
    modelName: filterModel.value,
    status: filterStatus.value,
    keyType: filterKeyType.value,
    provider: filterProvider.value,
    tier: filterTier.value,
    deptName: filterDept.value,
    studioId: filterStudio.value,
  }),
)

function clearFilters() {
  filterUserName.value = ''
  filterModel.value    = ''
  filterStatus.value   = ''
  filterKeyType.value  = ''
  filterEndpoint.value = ''
  filterProvider.value = ''
  filterTier.value     = ''
  filterDept.value     = ''
  filterStudio.value   = ''
  if (breadcrumbs.value[breadcrumbs.value.length - 1]?.scope === 'user') {
    breadcrumbs.value = []
  }
  bumpVideoOrLoadAll()
}

async function loadProviderOptions() {
  try {
    const r = await http.get('/api/v1/usage/options')
    providerOptions.value = r.data.providers || []
    deptOptions.value = (r.data.depts || []).map((d: any) => ({
      label: d.name || d.id,
      value: d.id,
    }))
    studioOptions.value = (r.data.studios || []).map((s: any) => ({
      label: s.name || s.id,
      value: s.id,
    }))
    allModelOptions.value = r.data.models || []
  } catch { /* ignore */ }
}

// ── 角色 / 范围标签 ──────────────────────────────────────────
const scopeLabel = computed(() => {
  const map: Record<string, string> = {
    admin: '全局管理员',
    studio_admin: '工作室管理员',
    dept_admin: '部门管理员',
  }
  return map[authStore.effectiveRole || ''] || authStore.userName || '管理视角'
})

const topLabel = computed(() => {
  if (authStore.effectiveRole === 'dept_admin') return '我的部门'
  if (authStore.effectiveRole === 'studio_admin') return '我的工作室'
  return '全公司'
})

const rankTitle = computed(() => {
  const map: Record<string, string> = {
    company: '工作室成本排行',
    studio:  '部门成本排行',
    dept:    '成员成本排行',
    user:    '模型用量',
  }
  return map[currentScope.value] || '成本排行'
})

// ── 汇总 / 排行 ──────────────────────────────────────────────
const summaryLoading = ref(false)
const summary = ref<any>(null)
const breakdown = ref<any[]>([])
const rankPage = ref(1)
const rankPageSize = 20
const rankTotal = ref(0)
const breadcrumbs = ref<Array<{ scope: string; id: string; name: string }>>([])

function scopeParams(): Record<string, string> {
  const last = breadcrumbs.value[breadcrumbs.value.length - 1]
  if (!last?.id) return {}
  if (last.scope === 'user')   return { user_id: last.id }
  if (last.scope === 'dept')   return { dept_id: last.id }
  if (last.scope === 'studio') return { studio_id: last.id }
  return {}
}

const currentScope = computed(() => {
  if (breadcrumbs.value.length > 0)
    return breadcrumbs.value[breadcrumbs.value.length - 1].scope
  if (authStore.effectiveRole === 'dept_admin') return 'dept'
  if (authStore.effectiveRole === 'studio_admin') return 'studio'
  return 'company'
})

const canDrill = computed(() =>
  currentScope.value === 'company' || currentScope.value === 'studio' || currentScope.value === 'dept'
)

const totalCost = computed(() =>
  breakdown.value.reduce((sum, r) => sum + (r.cost_estimate || 0), 0)
)

const avgCostPerCall = computed(() => {
  const s = summary.value
  if (!s?.call_count) return '0.00'
  return fmtCost((s.cost_estimate || 0) / s.call_count)
})

// ── 预算 ─────────────────────────────────────────────────────
const budgetContext = ref<any[]>([])

const currentBudget = computed(() => {
  const valid = budgetContext.value.filter((c: any) => c.amount_usd > 0)
  if (!valid.length) return null
  return valid[valid.length - 1]
})

const budgetLevel = computed(() => {
  const r = currentBudget.value?.used_ratio ?? 0
  if (r >= 1) return 'danger'
  if (r >= 0.8) return 'warn'
  return 'ok'
})

const budgetPoolItems = computed(() =>
  budgetContext.value.filter((c: any) => c.amount_usd > 0)
)

async function loadBudget() {
  try {
    const res = await http.get('/api/v1/budgets/context')
    budgetContext.value = res.data?.context ?? []
  } catch (e) {
    budgetContext.value = []
    console.warn('Failed to load budget context:', e)
  }
}

// ── 花销构成 ──────────────────────────────────────────────────
const showCostBreakdown = computed(() =>
  (currentScope.value === 'studio' || currentScope.value === 'dept') && breakdown.value.length > 0
)

const top5Breakdown = computed(() => breakdown.value.slice(0, 5))

const top5Max = computed(() =>
  Math.max(1, ...top5Breakdown.value.map((r: any) => r.cost_estimate || 0))
)

const totalCostForBreakdown = computed(() =>
  top5Breakdown.value.reduce((sum: number, r: any) => sum + (r.cost_estimate || 0), 0)
)

// ── 排行多维度排序 & 搜索 ─────────────────────────────────────
type SortField = 'cost' | 'calls' | 'tokens' | 'change' | 'cpt' | 'err'
const rankSort = ref<{ field: SortField; order: 'asc' | 'desc' }>({ field: 'cost', order: 'desc' })
const rankSearch = ref('')

const displayedBreakdown = computed(() => {
  let list = [...breakdown.value]
  if (rankSearch.value) {
    const q = rankSearch.value.toLowerCase()
    list = list.filter((r: any) => (r.name || r.id || '').toLowerCase().includes(q))
  }
  const { field, order } = rankSort.value

  // Enhanced fields: look up from enhancedUserMap
  const enhancedFields: Record<string, string> = {
    change: 'cost_change_pct',
    cpt: 'cost_per_1k_tokens',
    err: 'error_rate',
  }

  if (field in enhancedFields) {
    const map = enhancedUserMap.value
    const enhKey = enhancedFields[field]
    list.sort((a: any, b: any) => {
      const av = map[a.id]?.[enhKey] ?? -Infinity
      const bv = map[b.id]?.[enhKey] ?? -Infinity
      return order === 'desc' ? bv - av : av - bv
    })
  } else {
    const key = field === 'cost' ? 'cost_estimate'
      : field === 'calls' ? 'call_count'
      : currentScope.value === 'user' ? 'total_tokens' : 'user_count'
    list.sort((a: any, b: any) => {
      const av = a[key] ?? 0, bv = b[key] ?? 0
      return order === 'desc' ? bv - av : av - bv
    })
  }
  return list
})

function toggleSort(field: SortField) {
  if (rankSort.value.field === field) {
    rankSort.value = { field, order: rankSort.value.order === 'desc' ? 'asc' : 'desc' }
  } else {
    rankSort.value = { field, order: 'desc' }
  }
}

// 环比
const costChange = computed(() => {
  const curr = summary.value?.cost_estimate ?? 0
  const prev = summary.value?.prev_cost_estimate ?? 0
  if (!prev) return null
  return ((curr - prev) / prev) * 100
})

const callsChange = computed(() => {
  const curr = summary.value?.call_count ?? 0
  const prev = summary.value?.prev_call_count ?? 0
  if (!prev) return null
  return ((curr - prev) / prev) * 100
})



const prevPeriodLabel = computed(() => {
  const [start, end] = dateRange.value
  if (!start || !end) return ''
  const s = new Date(start), e = new Date(end)
  const days = Math.round((e.getTime() - s.getTime()) / 86400000) + 1
  const prevEnd = new Date(s); prevEnd.setDate(prevEnd.getDate() - 1)
  const prevStart = new Date(prevEnd); prevStart.setDate(prevStart.getDate() - days + 1)
  const f = (d: Date) => d.toISOString().slice(0, 10)
  return `上期：${f(prevStart)} ~ ${f(prevEnd)}（${days} 天）`
})



async function loadSummary() {
  summaryLoading.value = true
  try {
    const last = breadcrumbs.value[breadcrumbs.value.length - 1]
    const params: Record<string, any> = {
      scope: currentScope.value,
      page: rankPage.value,
      page_size: rankPageSize,
      start: dateRange.value[0],
      end: dateRange.value[1],
    }
    if (last?.id) params.id = last.id
    if (filterModel.value) params.model_name = filterModel.value
    if (filterStatus.value) params.status = filterStatus.value
    if (filterEndpoint.value) params.endpoint = filterEndpoint.value
    const res = await http.get('/api/v1/usage/dashboard', { params })
    summary.value = res.data?.summary ?? null
    breakdown.value = res.data?.breakdown ?? []
    rankTotal.value = res.data?.total ?? 0

    if (currentScope.value === 'user') {
      loadUserTopModels()
    } else {
      userTopModels.value = {}
    }
    // Phase B: 当展示用户列表时(dept scope)，并行获取增强数据
    if (currentScope.value === 'dept') {
      loadEnhancedUsers()
    } else {
      enhancedUserMap.value = {}
    }
  } catch (e) {
    summary.value = null
    breakdown.value = []
    console.warn('Failed to load summary:', e)
  } finally {
    summaryLoading.value = false
  }
}

// ── 用户 Top3 模型标签 ──────────────────────────────────────
const userTopModels = ref<Record<string, { model: string; est_cost_usd: number }[]>>({})
const enhancedUserMap = ref<Record<string, UserRankRow>>({})

async function loadUserTopModels() {
  try {
    const last = breadcrumbs.value[breadcrumbs.value.length - 1]
    const params: Record<string, any> = {
      from: dateRange.value[0],
      to: dateRange.value[1],
      top_n: 3,
    }
    if (last?.id && last.scope === 'studio') params.studio_id = last.id
    if (last?.id && last.scope === 'dept') params.dept_id = last.id
    const res = await http.get('/api/v1/usage/dashboard/user-top-models', { params })
    userTopModels.value = res.data?.models ?? {}
  } catch (e) {
    userTopModels.value = {}
    console.warn('Failed to load user top models:', e)
  }
}

async function loadEnhancedUsers() {
  try {
    const last = breadcrumbs.value[breadcrumbs.value.length - 1]
    const params: any = {
      from: dateRange.value[0],
      to: dateRange.value[1],
      page: rankPage.value,
      page_size: rankPageSize,
    }
    if (last?.id && last.scope === 'dept') params.dept_id = last.id
    if (last?.id && last.scope === 'studio') params.studio_id = last.id
    if (filterModel.value) params.model_name = filterModel.value
    if (filterEndpoint.value) params.endpoint = filterEndpoint.value
    const data = await fetchUsersRanking(params)
    const map: Record<string, UserRankRow> = {}
    for (const row of data.rows) {
      map[row.user_id] = row
    }
    enhancedUserMap.value = map
  } catch (e) {
    enhancedUserMap.value = {}
    console.warn('Failed to load enhanced users:', e)
  }
}



function drillDown(row: any) {
  const next: Record<string, string> = { company: 'studio', studio: 'dept', dept: 'user' }
  const nextScope = next[currentScope.value]
  if (!nextScope) return
  breadcrumbs.value.push({ scope: nextScope, id: row.id, name: row.name || row.id })
  rankPage.value = 1
  loadSummary()
}

// ── Phase B: 面板管理 ─────────────────────────────────────────
function handleUserClick(row: any) {
  const enhanced = enhancedUserMap.value[row.id]
  const userName = enhanced?.user_name || row.name || row.id
  togglePanel('user', row.id, userName)
}

function handleModelClickFromPanel(modelName: string) {
  jumpToModel(modelName)
}

function handleModelClick(modelName: string) {
  togglePanel('model', modelName, modelName)
}

function handleUserClickFromModelPanel(userId: string, userName: string) {
  jumpToUser(userId, userName)
}

const panelUserName = computed(() => {
  if (!userType.value) return ''
  const enhanced = enhancedUserMap.value[userType.value.id]
  return enhanced?.user_name || userType.value.name
})

const panelDeptName = computed(() => {
  if (!userType.value) return ''
  const enhanced = enhancedUserMap.value[userType.value.id]
  return enhanced?.dept_name || ''
})

const teamAvg = computed(() => {
  const data = modelsData.value
  if (!data.length) return undefined
  const totalCalls = data.reduce((s, m) => s + (m.calls ?? 0), 0)
  if (!totalCalls) return undefined
  const weightedSuccess = data.reduce((s, m) => s + (m.calls ?? 0) * (m.success_rate ?? 95), 0)
  const weightedDuration = data.reduce((s, m) => s + (m.calls ?? 0) * (m.avg_duration_ms ?? 3000), 0)
  const totalTokens = data.reduce((s, m) => s + (m.total_tokens ?? 0), 0)
  const totalCost = data.reduce((s, m) => s + (m.cost ?? 0), 0)
  return {
    success_rate: weightedSuccess / totalCalls,
    avg_duration_ms: weightedDuration / totalCalls,
    cost_per_1k_tokens: totalTokens > 0 ? (totalCost / (totalTokens / 1000)) : 0,
  }
})

function popToLevel(idx: number) {
  breadcrumbs.value = breadcrumbs.value.slice(0, idx + 1)
  rankPage.value = 1
  loadSummary()
}

function resetToTop() {
  breadcrumbs.value = []
  rankPage.value = 1
  loadSummary()
}

// ── 趋势图 ────────────────────────────────────────────────────
const trendLoading = ref(false)
const trendData    = ref<any[]>([])
const prevTrendData = ref<any[]>([])
const trendMetric = ref<'all' | 'cost' | 'calls' | 'tokens'>('all')

const trendGranularity = ref<'day' | 'week' | 'month'>('day')
function autoSetGranularity() {
  const [start, end] = dateRange.value
  if (!start || !end) return
  const days = Math.round((new Date(end).getTime() - new Date(start).getTime()) / 86400000) + 1
  if (days > 90) trendGranularity.value = 'month'
  else if (days > 30) trendGranularity.value = 'week'
  else trendGranularity.value = 'day'
}

async function loadTrend() {
  trendLoading.value = true
  try {
    const trendParams: Record<string, any> = { start: dateRange.value[0], end: dateRange.value[1] }
    Object.assign(trendParams, scopeParams())
    if (filterModel.value) trendParams.model_name = filterModel.value
    if (filterStatus.value) trendParams.status = filterStatus.value
    if (filterEndpoint.value) trendParams.endpoint = filterEndpoint.value

    const [start, end] = dateRange.value
    const s = new Date(start), e = new Date(end)
    const days = Math.round((e.getTime() - s.getTime()) / 86400000) + 1
    const prevEnd = new Date(s); prevEnd.setDate(prevEnd.getDate() - 1)
    const prevStart = new Date(prevEnd); prevStart.setDate(prevStart.getDate() - days + 1)
    const prevParams = { ...trendParams, start: fmt(prevStart), end: fmt(prevEnd) }

    const [res, prevRes] = await Promise.all([
      http.get('/api/v1/usage/dashboard/trend', { params: trendParams }),
      http.get('/api/v1/usage/dashboard/trend', { params: prevParams }).catch(() => ({ data: { rows: [] } })),
    ])
    trendData.value     = res.data?.rows     ?? []
    prevTrendData.value = prevRes.data?.rows ?? []
  } catch (e) {
    trendData.value     = []
    prevTrendData.value = []
    console.warn('Failed to load trend data:', e)
  } finally {
    trendLoading.value = false
  }
}

// ── 模型分布图 ────────────────────────────────────────────────
const modelsLoading = ref(false)
const modelsData    = ref<any[]>([])

const modelOptions = computed(() => {
  const seen = new Set(allModelOptions.value)
  for (const m of modelsData.value) {
    const name = m?.model
    if (name && name.trim()) seen.add(name.trim())
  }
  return Array.from(seen).sort((a: string, b: string) => a.localeCompare(b))
})

const modelChartType = ref<'bar' | 'doughnut'>('bar')
const modelMetric    = ref<'cost' | 'calls' | 'tokens'>('cost')

function switchModelChartType(_t: any) { /* ModelChartSection handles rendering */ }
function switchModelMetric(_m: any) { /* ModelChartSection handles rendering */ }

async function loadModels() {
  modelsLoading.value = true
  try {
    const modelParams: Record<string, any> = { start: dateRange.value[0], end: dateRange.value[1] }
    Object.assign(modelParams, scopeParams())
    if (filterModel.value)  modelParams.model_name = filterModel.value
    if (filterStatus.value) modelParams.status = filterStatus.value
    if (filterEndpoint.value) modelParams.endpoint = filterEndpoint.value
    const res = await http.get('/api/v1/usage/dashboard/models', { params: modelParams })
    modelsData.value = res.data?.rows ?? []
  } catch (e) {
    modelsData.value = []
    console.warn('Failed to load models:', e)
  } finally {
    modelsLoading.value = false
  }
}

// ── 调用明细 ──────────────────────────────────────────────────
const logsLoading = ref(false)
const logSectionRef = ref<InstanceType<typeof UsageLogSection> | null>(null)

const logParams = computed(() => {
  if (!dateRange.value?.[0]) return {}
  const p: Record<string, any> = {
    start: dateRange.value[0],
    end:   dateRange.value[1],
  }
  const sp = scopeParams()
  if (sp.user_id) {
    p.user_id = sp.user_id
  } else if (sp.dept_id) {
    p.dept_id = sp.dept_id
  } else if (sp.studio_id) {
    p.studio_id = sp.studio_id
  } else {
    if (filterUserName.value) p.user_name = filterUserName.value
    if (filterDept.value) p.dept_name = filterDept.value
    if (filterStudio.value) p.studio_id = filterStudio.value
  }
  if (filterModel.value)  p.model_name = filterModel.value
  if (filterStatus.value) p.status     = filterStatus.value
  if (filterKeyType.value) p.key_type  = filterKeyType.value
  if (filterEndpoint.value) p.endpoint = filterEndpoint.value
  return p
})

function reloadLogs() {
  logSectionRef.value?.refresh()
}

async function onUserNameChange() {
  const last = breadcrumbs.value[breadcrumbs.value.length - 1]
  if (last?.scope === 'user') {
    breadcrumbs.value = []
  }
  if (filterUserName.value) {
    try {
      const results = await orgStore.searchUsers(filterUserName.value, 1)
      if (results.length > 0) {
        breadcrumbs.value = [{ scope: 'user' as const, id: results[0].id, name: results[0].name }]
      } else {
        breadcrumbs.value = [{ scope: 'user' as const, id: '__no_match__', name: filterUserName.value }]
      }
    } catch {
      breadcrumbs.value = [{ scope: 'user' as const, id: '__no_match__', name: filterUserName.value }]
    }
  }
  loadAll()
}

function loadAll() {
  loadSummary()
  loadTrend()
  loadModels()
  loadBudget()
  nextTick(() => logSectionRef.value?.refresh())
}

// ── 格式化 ────────────────────────────────────────────────────
const fmtCost = (v?: number | null): string => {
  const n = convertCost(Number(v || 0), cc.value)
  const sym = currencySymbol(cc.value.currency)
  if (n === 0) return `${sym}0.00`
  let s: string
  if (Math.abs(n) >= 1)        s = n.toFixed(2)
  else if (Math.abs(n) >= 0.01) s = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001) s = n.toFixed(6)
  else s = n.toFixed(8)
  return sym + s.replace(/(\.\d{2,}?)0+$/, '$1')
}

// ── Lifecycle ─────────────────────────────────────────────────

onMounted(() => {
  const { user_id, user_name, start, end } = route.query
  if (user_id && user_name) {
    breadcrumbs.value = [{ scope: 'user', id: String(user_id), name: String(user_name) }]
    filterUserName.value = String(user_name)
  }
  if (start && end) {
    dateRange.value = [String(start), String(end)]
  }
  loadProviderOptions()
  loadAll()
})

onUnmounted(() => {
  // Chart instances are managed by child components
})

watch(currency, async () => {
  // Chart instances in child components handle re-rendering via their own watchers
  await nextTick()
  // Force log section to re-render with new currency
  logSectionRef.value?.refresh()
})

// ── Expose (for parent/route access if needed) ──
defineExpose({ reloadLogs })
</script>

<style scoped>
/* ── 根容器 ── */
.ud {
  --ud-input:  #36B34E;
  --ud-output: #0EA371;
  --ud-cache:  #5DADE2;
  --ud-cost:   #C7900A;
  --ud-warn:   #E0A100;
  --ud-danger: #E74C3C;
  --ud-purple: #8B5CF6;
  --ud-amber:  #F59E0B;

  --ud-surface: var(--el-fill-color-blank);
  --ud-border:  var(--el-border-color-light);
  --ud-muted:   var(--el-text-color-secondary);
  --ud-faint:   var(--el-text-color-placeholder);
  --ud-ink:     var(--el-text-color-regular);
  --ud-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  min-height: 100%; box-sizing: border-box;
  display: flex; flex-direction: column; gap: 10px;
}

/* ── 两列布局 ── */
.ud-main-row {
  display: grid;
  grid-template-columns: 3fr 2fr;
  gap: 12px;
}
@media (max-width: 960px) { .ud-main-row { grid-template-columns: 1fr; } }

.ud-video-filter {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

/* ── 调用明细列样式 ── */
.ud-key-name    { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
.ud-member-name { font-size: 13px; color: var(--el-text-color-regular); }
.ud-studio-name { font-size: 13px; color: var(--el-text-color-regular); }
.ud-dept-name   { font-size: 13px; color: var(--el-text-color-regular); }

.ud-model-cell { display: flex; flex-direction: column; gap: 2px; }
.ud-model-name { font-size: 13px; color: var(--el-text-color-primary); font-family: var(--ud-mono); }
.ud-model-name--muted { color: var(--ud-muted); font-size: 12px; }
.ud-model-routed { display: flex; align-items: center; gap: 4px; }
.ud-arrow { color: var(--ud-faint); font-size: 11px; }

.ud-token-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ud-token-item { display: inline-flex; align-items: center; gap: 3px; font-size: 12px; }
.ud-token-item--in    { color: var(--ud-input); }
.ud-token-item--out   { color: var(--ud-output); }
.ud-token-item--cache { color: var(--ud-cache); }
.ud-token-icon { font-size: 11px; }
.ud-info { color: var(--ud-faint); cursor: pointer; font-size: 13px; vertical-align: middle; }

.ud-cost-cell { display: flex; align-items: center; justify-content: flex-end; gap: 4px; }
.ud-cost { font-size: 13px; }
.ud-duration { font-size: 12px; font-family: var(--ud-mono); color: var(--ud-muted); }
.ud-dash { color: var(--ud-faint); }
.ud-time { font-size: 12px; color: var(--ud-muted); font-family: var(--ud-mono); }
.ud-ip   { font-size: 12px; font-family: var(--ud-mono); color: var(--ud-muted); }
.ud-flags { display: flex; gap: 4px; flex-wrap: wrap; justify-content: center; }
.ud-tag--effort { text-transform: uppercase; letter-spacing: .3px; }
.ud-source { display: flex; flex-direction: column; gap: 2px; }

/* ── 状态过滤 ── */
.ud-status-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; margin-right: 6px; vertical-align: middle; }
.ud-status-dot--ok  { background: #67c23a; }
.ud-status-dot--err { background: #f56c6c; }

/* ── 空状态 ── */
.ud-empty-block { padding: 48px; text-align: center; color: var(--ud-faint); }
.ud-empty-block p { margin-top: 12px; font-size: 14px; }

/* ── 颜色工具 ── */
.c-blue   { color: #409eff; }
.c-green  { color: #67c23a; }
.c-orange { color: #e6a23c; }
.c-red    { color: #f56c6c; }
.c-cyan   { color: #5DADE2; }
.c-amber  { color: var(--ud-amber); }
.c-purple { color: var(--ud-purple); }
.fw6      { font-weight: 600; }

/* ── Trace Drawer ── */
.ud-trace-header  { display: flex; flex-direction: column; gap: 2px; }
.ud-trace-title   { font-size: 15px; font-weight: 700; color: var(--el-text-color-primary); }
.ud-trace-id      { font-size: 11px; color: var(--el-text-color-secondary); font-family: var(--ud-mono); word-break: break-all; }
.ud-trace-loading { display: flex; justify-content: center; align-items: center; padding: 60px 0; color: var(--el-color-primary); }
.ud-trace-body    { display: flex; flex-direction: column; gap: 20px; }
.ud-trace-empty   { text-align: center; padding: 40px 0; color: var(--el-text-color-secondary); font-size: 13px; }
.ud-trace-meta    { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ud-trace-time    { font-size: 12px; color: var(--el-text-color-secondary); margin-left: 4px; }
.ud-trace-dur     { font-size: 12px; color: var(--el-color-primary); font-weight: 600; }
.ud-trace-tabs    { margin-top: -8px; }
.ud-tag-failover  { cursor: help; }

/* ── Timeline ── */
.ud-timeline {
  display: flex; align-items: flex-start; gap: 0;
  background: var(--el-fill-color-light);
  border-radius: 10px; padding: 16px 20px; overflow-x: auto;
}
.ud-tl-node {
  display: flex; flex-direction: column; align-items: center; gap: 4px;
  position: relative; flex: 1; min-width: 72px;
}
.ud-tl-node + .ud-tl-node::before {
  content: ''; position: absolute; left: -50%; top: 13px;
  width: 100%; height: 2px; background: var(--el-border-color); z-index: 0;
}
.ud-tl-node.ok    + .ud-tl-node::before { background: var(--el-color-success-light-5); }
.ud-tl-node.error + .ud-tl-node::before { background: var(--el-color-danger-light-5); }
.ud-tl-dot {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: var(--el-fill-color); border: 2px solid var(--el-border-color);
  font-size: 14px; position: relative; z-index: 1;
}
.ud-tl-node.ok    .ud-tl-dot { background: color-mix(in srgb, var(--el-color-success) 12%, #fff); border-color: var(--el-color-success); color: var(--el-color-success); }
.ud-tl-node.error .ud-tl-dot { background: color-mix(in srgb, var(--el-color-danger)  12%, #fff); border-color: var(--el-color-danger);  color: var(--el-color-danger); }
.ud-tl-node--failover .ud-tl-dot   { background: color-mix(in srgb, var(--el-color-warning) 12%, #fff); border-color: var(--el-color-warning); color: var(--el-color-warning); }
.ud-tl-node--failover .ud-tl-label { color: var(--el-color-warning-dark-2); }
.ud-tl-node--failover + .ud-tl-node::before { background: var(--el-color-warning-light-7); }
.ud-tl-failover-hint { font-size: 10px; color: var(--el-color-warning-dark-2); font-style: italic; }
.ud-tl-label      { font-size: 11px; color: var(--el-text-color-secondary); font-weight: 600; }
.ud-tl-label-text { cursor: default; }
.ud-tl-measured   { display: block; font-size: 9px; color: var(--el-text-color-placeholder); font-style: italic; }
.ud-tl-ms         { font-size: 11px; color: var(--el-text-color-regular); font-family: var(--ud-mono); }
.ud-tl-warn       { color: var(--el-color-warning); vertical-align: -2px; cursor: help; }

/* ── Meta grid ── */
.ud-meta-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 16px;
}
.ud-meta-section {
  background: var(--el-fill-color-light); border-radius: 8px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 6px;
}
.ud-meta-heading {
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--el-text-color-secondary);
  margin-bottom: 4px; border-bottom: 1px solid var(--el-border-color-lighter); padding-bottom: 6px;
}
.ud-meta-row {
  display: flex; justify-content: space-between; align-items: center; gap: 8px;
  font-size: 12.5px; min-height: 22px;
}
.ud-meta-row > span:first-child { color: var(--el-text-color-secondary); flex-shrink: 0; }
.ud-meta-row > span:last-child, .ud-meta-row > b, .ud-meta-row > code { text-align: right; word-break: break-all; }
.ud-meta-sep { border-top: 1px solid var(--el-border-color); margin-top: 4px; padding-top: 4px; }
.ud-meta-row--wrap { align-items: flex-start; }
.ud-meta-ua  { font-size: 11px; text-align: right; }
.ud-meta-row code { font-family: var(--ud-mono); font-size: 12px; }

/* ── Messages / JSON ── */
.ud-msgs    { display: flex; flex-direction: column; gap: 12px; }
.ud-msg     { border-radius: 8px; overflow: hidden; border: 1px solid var(--el-border-color-light); }
.ud-msg-role {
  display: block; padding: 5px 12px; font-size: 11px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .05em;
  background: var(--el-fill-color-light); color: var(--el-text-color-secondary);
}
.ud-msg--system    .ud-msg-role { background: #fef3c7; color: #92400e; }
.ud-msg--user      .ud-msg-role { background: #dbeafe; color: #1e40af; }
.ud-msg--assistant .ud-msg-role { background: #d1fae5; color: #065f46; }
.ud-msg-content {
  margin: 0; padding: 12px; font-size: 12.5px; line-height: 1.65;
  white-space: pre-wrap; word-break: break-word; overflow-x: auto; -webkit-overflow-scrolling: touch;
  font-family: var(--ud-mono); background: var(--el-bg-color); max-height: 400px; overflow-y: auto;
}
.ud-json-view {
  background: var(--el-fill-color-light); border-radius: 8px; padding: 14px;
  max-height: 500px; overflow-y: auto;
}
.ud-json-view pre {
  margin: 0; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all;
  font-family: var(--ud-mono); overflow-x: auto; -webkit-overflow-scrolling: touch;
}

/* ── Headers table ── */
.ud-headers-table { display: flex; flex-direction: column; gap: 4px; }
.ud-header-row    { display: flex; gap: 12px; font-size: 12px; padding: 4px 0; border-bottom: 1px solid var(--el-border-color-lighter); }
.ud-header-key    { color: var(--el-text-color-secondary); flex-shrink: 0; min-width: 140px; font-family: var(--ud-mono); }
.ud-header-val    { color: var(--el-text-color-primary); word-break: break-all; font-family: var(--ud-mono); }

/* ── Error block ── */
.ud-error-block { background: #fef2f2; border: 1px solid #fca5a5; border-radius: 8px; padding: 16px; }
.ud-error-code  { font-size: 11px; font-weight: 700; color: #dc2626; text-transform: uppercase; margin-bottom: 8px; }
.ud-error-msg   { margin: 0; font-size: 12.5px; color: #7f1d1d; white-space: pre-wrap; word-break: break-word; }

/* ── Phase B: Panel slide transition ── */
.ud-slide-enter-active {
  transition: opacity 0.35s cubic-bezier(0, 0, 0.2, 1),
              transform 0.35s cubic-bezier(0, 0, 0.2, 1);
}
.ud-slide-leave-active {
  transition: opacity 0.2s cubic-bezier(0.4, 0, 1, 1),
              transform 0.2s cubic-bezier(0.4, 0, 1, 1);
}
.ud-slide-enter-from {
  opacity: 0;
  transform: translateY(-16px) scale(0.98);
}
.ud-slide-leave-to {
  opacity: 0;
  transform: translateY(-8px) scale(0.99);
}
.ud-slide-enter-to,
.ud-slide-leave-from {
  opacity: 1;
  transform: translateY(0) scale(1);
}

/* ── Phase B: Model Detail Panel slide transition ── */
.md-slide-enter-active {
  transition: opacity 0.35s cubic-bezier(0, 0, 0.2, 1),
              transform 0.35s cubic-bezier(0, 0, 0.2, 1);
}
.md-slide-leave-active {
  transition: opacity 0.2s cubic-bezier(0.4, 0, 1, 1),
              transform 0.2s cubic-bezier(0.4, 0, 1, 1);
}
.md-slide-enter-from {
  opacity: 0;
  transform: translateY(-16px) scale(0.98);
}
.md-slide-leave-to {
  opacity: 0;
  transform: translateY(-8px) scale(0.99);
}
.md-slide-enter-to,
.md-slide-leave-from {
  opacity: 1;
  transform: translateY(0) scale(1);
}
</style>

<!-- tooltip 内容 teleport 到 body，需要非 scoped 全局样式 -->
<style>
.ud-tt-popper.el-popper {
  background: #1f2937 !important;
  border: 1px solid #374151 !important;
  border-radius: 10px !important;
  box-shadow: 0 12px 32px rgba(0, 0, 0, .35) !important;
  padding: 0 !important;
}
.ud-tt-popper.el-popper .el-popper__arrow::before {
  background: #1f2937 !important;
  border: 1px solid #374151 !important;
}
.ud-tt { padding: 10px 14px; min-width: 200px; font-size: 12px; line-height: 1.7; font-variant-numeric: tabular-nums; }
.ud-tt__title { font-weight: 700; font-size: 12px; margin-bottom: 7px; color: #e5e7eb; border-bottom: 1px solid #374151; padding-bottom: 5px; letter-spacing: .2px; }
.ud-tt__group { margin: 6px 0 3px; font-size: 10px; font-weight: 600; letter-spacing: 0.5px; text-transform: uppercase; color: #38bdf8; padding-left: 6px; border-left: 2px solid rgba(56, 189, 248, 0.4); }
.ud-tt__row { display: flex; justify-content: space-between; gap: 18px; align-items: center; color: #9ca3af; }
.ud-tt__sub { color: #6b7280; padding-left: 2px; }
.ud-tt__row b { color: #f9fafb; font-weight: 600; }
.ud-tt__row b.c-cyan   { color: #38bdf8; }
.ud-tt__row b.c-amber  { color: #fbbf24; }
.ud-tt__row b.c-purple { color: #a78bfa; }
.ud-tt__row b.c-blue   { color: #5eead4; }
</style>
