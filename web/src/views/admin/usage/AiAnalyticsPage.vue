<template>
  <div class="aap">
    <!-- 页头：标题 + 范围 + 刷新 -->
    <header class="aap-top">
      <div class="aap-titles">
        <h1 class="aap-h1">AI 分析看板</h1>
        <span class="aap-scope">{{ insight.label.value }}</span>
        <span class="aap-last">更新于 {{ lastUpdated || '—' }}</span>
      </div>
      <div class="aap-actions">
        <el-button size="small" :loading="loading" @click="loadAll">刷新</el-button>
      </div>
    </header>

    <!-- 锚点导航 -->
    <nav class="aap-nav" aria-label="区块导航">
      <a
        v-for="a in anchors"
        :key="a.id"
        :class="{ on: activeAnchor === a.id }"
        href="#sec-overview"
        @click.prevent="scrollTo(a.id)"
      >{{ a.label }}</a>
    </nav>

    <!-- 筛选 -->
    <UsageFilterBar
      page-role="dashboard"
      capability="chat"
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
    />

    <div class="aap-body">
      <!-- 概览：裁决 + KPI + AI 卡 -->
      <section id="sec-overview" class="aap-sec">
        <VerdictBar :metrics="insight.metrics.value" @jump="scrollTo" />
        <AnalyticsKpiRow
          :metrics="insight.metrics.value"
          :summary="usersSummary"
          :days="days"
        />
        <AiAnalysisCard
          :scope-label="insight.label.value"
          :segments="insight.segments.value"
          :loading="insight.loading.value"
          @drill="handleAiDrill"
        />
      </section>

      <!-- 异常清单 -->
      <section id="sec-alerts" class="aap-sec">
        <h2 class="aap-h2">异常清单</h2>
        <div class="panel">
          <AlertsList
            :dept-rows="deptRows"
            :gmed="insight.gmed.value"
            :loading="loading"
            @drill="handleAlertDrill"
          />
        </div>
      </section>

      <!-- 趋势 × 部门横条 + 模型表 -->
      <section id="sec-models" class="aap-sec">
        <h2 class="aap-h2">费用趋势与模型</h2>
        <div class="grid-2">
          <div class="panel">
            <h3 class="aap-h3">日费用趋势（灰=上区间，彩=当前堆叠）</h3>
            <CostTrendChart
              :hourly="hourlyCur"
              :prev-hourly="hourlyPrev"
              :curr-dates="currDates"
              :prev-dates="prevDates"
            />
          </div>
          <div class="panel">
            <h3 class="aap-h3">部门费用分布</h3>
            <DeptHighTierChart
              :dept-rows="deptRows"
              :high-tier-pct="insight.metrics.value?.high_tier_pct"
            />
          </div>
        </div>
        <div class="panel">
          <h3 class="aap-h3">模型费用明细（点击行展开 24h）</h3>
          <ModelTable :hourly="hourlyCur" :prev-hourly="hourlyPrev" />
        </div>
      </section>

      <!-- 部门透视 -->
      <section id="sec-dept" class="aap-sec">
        <h2 class="aap-h2">部门透视</h2>
        <div class="panel">
          <DeptTable
            :dept-rows="deptRows"
            :prev-dept-rows="deptPrev"
            :user-rows="userRows"
            :gmed="insight.gmed.value"
            :period-label="periodLabel"
          />
        </div>
      </section>

      <!-- 用户排行 -->
      <section id="sec-users" class="aap-sec">
        <h2 class="aap-h2">用户排行</h2>
        <div class="panel">
          <UserRankTable :user-rows="userRows" />
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import UsageFilterBar from '@/components/usage/UsageFilterBar.vue'
import AiAnalysisCard from '@/views/admin/usage/components/AiAnalysisCard.vue'
import VerdictBar from './analytics/VerdictBar.vue'
import AnalyticsKpiRow from './analytics/AnalyticsKpiRow.vue'
import AlertsList from './analytics/AlertsList.vue'
import CostTrendChart from './analytics/CostTrendChart.vue'
import DeptHighTierChart from './analytics/DeptHighTierChart.vue'
import ModelTable from './analytics/ModelTable.vue'
import DeptTable from './analytics/DeptTable.vue'
import UserRankTable from './analytics/UserRankTable.vue'
import { useAiInsight, type InsightCrumb } from '@/composables/useAiInsight'
import { usageApi } from '@/api/usage'
import http from '@/services/http'
import { countActiveFilters } from '@/utils/usageFilterConfig'
import type {
  InsightCardDrillPayload,
  ModelDeptSummaryRow,
  ModelHourlyPoint,
  ModelUsersSummary,
  ModelUserDayRow,
} from '@/types/analytics'

defineOptions({ name: 'AiAnalyticsPage' })

// ── 日期 ──────────────────────────────────────────────────────
const fmt = (d: Date) => d.toISOString().split('T')[0]
const today = new Date()
const daysAgo = (n: number) => { const d = new Date(today); d.setDate(d.getDate() - n); return d }

const dateRange = ref<[string, string]>([fmt(daysAgo(6)), fmt(today)])
const activePreset = ref('近7天')
const datePresets = [
  { label: '今天',   days: 0 },
  { label: '近7天',  days: 6 },
  { label: '近30天', days: 29 },
]

const days = computed(() => {
  const [s, e] = dateRange.value
  if (!s || !e) return 1
  return Math.round((new Date(e).getTime() - new Date(s).getTime()) / 86400000) + 1
})

/** 上一等长区间（环比 / 趋势灰段） */
const prevRange = computed<[string, string]>(() => {
  const [s, e] = dateRange.value
  if (!s || !e) return dateRange.value
  const pe = new Date(s); pe.setDate(pe.getDate() - 1)
  const ps = new Date(pe); ps.setDate(ps.getDate() - days.value + 1)
  return [fmt(ps), fmt(pe)]
})

function dateList(start: string, end: string): string[] {
  const out: string[] = []
  const s = new Date(start + 'T00:00:00'), e = new Date(end + 'T00:00:00')
  for (let d = new Date(s); d <= e; d.setDate(d.getDate() + 1)) out.push(fmt(d))
  return out
}
const currDates = computed(() => dateList(dateRange.value[0], dateRange.value[1]))
const prevDates = computed(() => dateList(prevRange.value[0], prevRange.value[1]))
const periodLabel = computed(() => `本期 ${dateRange.value[0]} ~ ${dateRange.value[1]}（${days.value} 天）`)

// ── 筛选 ──────────────────────────────────────────────────────
const filterModel = ref('')
const filterStatus = ref('')
const filterKeyType = ref('')
const filterUserName = ref('')
const filterProvider = ref('')
const filterTier = ref('')
const filterDept = ref('')
const filterStudio = ref('')

const modelOptions = ref<string[]>([])
const providerOptions = ref<string[]>([])
const deptOptions = ref<Array<{ label: string; value: string }>>([])
const studioOptions = ref<Array<{ label: string; value: string }>>([])

const activeFilterCount = computed(() =>
  countActiveFilters('dashboard', 'chat', {
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

function applyPreset(p: { label: string; days: number }) {
  dateRange.value = [fmt(daysAgo(p.days)), fmt(today)]
  activePreset.value = p.label
  loadAll()
}
function onDateChange() {
  activePreset.value = ''
  loadAll()
}
function onFilterChange() {
  loadAll()
}
function clearFilters() {
  filterModel.value = ''
  filterStatus.value = ''
  filterKeyType.value = ''
  filterUserName.value = ''
  filterProvider.value = ''
  filterTier.value = ''
  filterDept.value = ''
  filterStudio.value = ''
  loadAll()
}

// ── AI 洞察（裁决 / KPI / AI 卡） ────────────────────────────
const breadcrumbs = ref<InsightCrumb[]>([])
const insight = useAiInsight({
  filterModel,
  filterDept,
  filterTier,
  breadcrumbs,
  dateRange,
  onDrill: handleAiDrill,
})

// ── 数据管线 ─────────────────────────────────────────────────
const loading = ref(false)
const lastUpdated = ref('')
const hourlyCur = ref<ModelHourlyPoint[] | null>(null)
const hourlyPrev = ref<ModelHourlyPoint[] | null>(null)
const deptRows = ref<ModelDeptSummaryRow[] | null>(null)
const deptPrev = ref<ModelDeptSummaryRow[] | null>(null)
const userRows = ref<ModelUserDayRow[] | null>(null)
const usersSummary = ref<ModelUsersSummary | null>(null)

function hourlyParams(start: string, end: string) {
  const p: Record<string, string> = { from: start, to: end, group_by: 'model' }
  if (filterModel.value) p.model_name = filterModel.value
  if (filterTier.value) p.tier_name = filterTier.value
  return p
}
function usersParams(group_by: string, period: string, start?: string, end?: string) {
  const p: Record<string, string> = {
    from: start || dateRange.value[0],
    to: end || dateRange.value[1],
    group_by,
    period,
  }
  if (filterModel.value) p.model_name = filterModel.value
  if (filterTier.value) p.tier_name = filterTier.value
  if (filterDept.value) p.dept_name = filterDept.value
  return p
}

async function loadAnalytics() {
  const [start, end] = dateRange.value
  if (!start || !end) return
  const [ps, pe] = prevRange.value
  loading.value = true
  try {
    const [hourlyR, hourlyPrevR, deptR, deptPrevR, userR, modelsR] = await Promise.all([
      usageApi.getModelHourly(hourlyParams(start, end) as any),
      usageApi.getModelHourly(hourlyParams(ps, pe) as any).catch(() => null),
      usageApi.getModelUsers(usersParams('dept', 'day') as any).catch(() => null),
      usageApi.getModelUsers(usersParams('dept', 'day', ps, pe) as any).catch(() => null),
      usageApi.getModelUsers(usersParams('user', 'day') as any).catch(() => null),
      http
        .get('/api/v1/usage/dashboard/models', { params: { start, end, limit: 30 } })
        .catch(() => ({ data: { rows: [] as any[] } })),
    ])
    hourlyCur.value = hourlyR?.hourly ?? null
    hourlyPrev.value = hourlyPrevR?.hourly ?? null
    deptRows.value = deptR?.dept_rows ?? null
    deptPrev.value = deptPrevR?.dept_rows ?? null
    userRows.value = userR?.user_day_rows ?? null
    usersSummary.value = userR?.summary ?? null
    modelOptions.value = (modelsR.data?.rows ?? [])
      .map((r: any) => r.model)
      .filter((name: string) => name && name.trim())
    lastUpdated.value = new Date().toLocaleTimeString()
  } catch (e) {
    console.warn('Failed to load analytics:', e)
  } finally {
    loading.value = false
  }
}

async function loadAll() {
  await Promise.all([insight.refresh(), loadAnalytics()])
}

async function loadOptions() {
  try {
    const r = await http.get('/api/v1/usage/options')
    providerOptions.value = r.data.providers || []
    deptOptions.value = (r.data.depts || []).map((d: any) => ({ label: d.name || d.id, value: d.id }))
    studioOptions.value = (r.data.studios || []).map((s: any) => ({ label: s.name || s.id, value: s.id }))
  } catch { /* ignore */ }
}

// ── 下钻分发 ─────────────────────────────────────────────────
function handleAiDrill(payload: InsightCardDrillPayload) {
  if (payload.dept) {
    filterDept.value = payload.dept
    loadAll()
  }
  scrollTo(payload.scroll || (payload.model ? 'models' : payload.dept ? 'dept' : 'overview'))
}

function handleAlertDrill({ dept, scroll }: { dept: string; scroll?: string }) {
  if (dept) {
    filterDept.value = dept
    loadAll()
  }
  scrollTo(scroll || 'dept')
}

// ── 锚点 / scrollspy ─────────────────────────────────────────
const anchors = [
  { id: 'overview', label: '概览' },
  { id: 'alerts', label: '异常清单' },
  { id: 'models', label: '趋势与模型' },
  { id: 'dept', label: '部门透视' },
  { id: 'users', label: '用户排行' },
]
const activeAnchor = ref('overview')

function scrollTo(sec?: string) {
  const el = document.getElementById('sec-' + (sec || 'overview'))
  if (!el) return
  el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeAnchor.value = sec || 'overview'
}

function onScroll() {
  let cur = 'overview'
  for (const a of anchors) {
    const el = document.getElementById('sec-' + a.id)
    if (el && el.getBoundingClientRect().top <= 140) cur = a.id
  }
  activeAnchor.value = cur
}

// ── Lifecycle ────────────────────────────────────────────────
let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  loadOptions()
  loadAll()
  timer = setInterval(() => loadAll(), 30000)
  window.addEventListener('scroll', onScroll, { passive: true })
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
  insight.clearCache()
  window.removeEventListener('scroll', onScroll)
})
</script>

<style scoped>
.aap {
  --brand-green: #36b34e;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-bottom: 40px;
}
.aap-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.aap-titles { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.aap-h1 { font-size: 18px; font-weight: 700; color: var(--el-text-color-primary); margin: 0; }
.aap-scope {
  font-size: 12px; color: var(--brand-green, #2f9e4a);
  background: rgba(54, 179, 78, 0.1); border: 1px solid rgba(54, 179, 78, 0.3);
  border-radius: 999px; padding: 2px 10px;
}
.aap-last { font-size: 11.5px; color: var(--el-text-color-secondary); }

.aap-nav {
  position: sticky; top: 0; z-index: 10;
  display: flex; gap: 4px; flex-wrap: wrap;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px; padding: 6px 8px;
}
.aap-nav a {
  font-size: 12.5px; color: var(--el-text-color-secondary);
  text-decoration: none; padding: 4px 12px; border-radius: 999px;
}
.aap-nav a:hover { color: var(--el-color-primary); }
.aap-nav a.on {
  color: #fff; background: var(--brand-green, #36b34e); font-weight: 600;
}

.aap-sec { display: flex; flex-direction: column; gap: 12px; scroll-margin-top: 60px; }
.aap-h2 {
  font-size: 15px; font-weight: 700; color: var(--el-text-color-primary);
  margin: 4px 0 0; padding-left: 10px; border-left: 3px solid var(--brand-green, #36b34e);
}
.aap-h3 { font-size: 12.5px; font-weight: 600; color: var(--el-text-color-regular); margin: 0 0 10px; }
.panel {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: var(--radius-base, 8px);
  padding: 14px 16px;
  box-shadow: var(--shadow-sm);
}
.grid-2 {
  display: grid;
  grid-template-columns: 3fr 2fr;
  gap: 12px;
}
@media (max-width: 960px) { .grid-2 { grid-template-columns: 1fr; } }
</style>
