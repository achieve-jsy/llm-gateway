<template>
  <div class="up">

    <!-- ═══════════════════════════════════════════
         页头
    ═══════════════════════════════════════════ -->
    <div class="up-header">
      <div class="up-header__left">
        <h2 class="up-title">用量统计</h2>
        <span class="up-badge">Token · 成本（估算）</span>
      </div>
      <div class="up-header__right">
        <el-radio-group v-model="currency" size="small">
          <el-radio-button value="USD">$ USD</el-radio-button>
          <el-radio-button value="CNY">¥ CNY</el-radio-button>
        </el-radio-group>
        <el-button :loading="statsLoading" :icon="Refresh" circle size="small" title="刷新" @click="bumpVideoOrLoadAll" />

      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         筛选栏
    ═══════════════════════════════════════════ -->
    <UsageFilterBar
      page-role="cost"
      :capability="(activeTab as any)"
      :date-presets="datePresets"
      v-model:date-range="dateRange"
      v-model:active-preset="activePreset"
      v-model:group-name="filterGroupName"
      v-model:model-name="filterModelName"
      v-model:key-name="filterKeyName"
      v-model:key-type="filterKeyType"
      v-model:status="filterStatus"
      v-model:provider="filterProvider"
      v-model:user-name="filterUserName"
      v-model:dept-name="filterDeptName"
      v-model:studio-id="filterStudioId"
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
      <VideoKpiCards
        :refresh-token="videoRefreshToken"
        :start="videoDateStart"
        :end="videoDateEnd"
        :provider-name="filterProvider || undefined"
        :model-name="videoModelParam"
      />
      <VideoTrendChart
        :refresh-token="videoRefreshToken"
        :start="videoDateStart"
        :end="videoDateEnd"
        :provider-name="filterProvider || undefined"
        :model-name="videoModelParam"
      />
      <VideoDistributionCharts
        :refresh-token="videoRefreshToken"
        :start="videoDateStart"
        :end="videoDateEnd"
        :provider-name="filterProvider || undefined"
        :model-name="videoModelParam"
      />
      <VideoTaskTable
        :refresh-token="videoRefreshToken"
        controlled
        :status="filterStatus"
        :model-name="videoModelParam"
        :provider-name="filterProvider"
      />
    </template>
    <template v-else>


    <!-- ═══════════════════════════════════════════
         统计卡片（sub2api 风格）
    ═══════════════════════════════════════════ -->
    <div class="up-stats" v-loading="statsLoading">
      <div class="up-stat-card">
        <div class="up-stat-card__icon up-stat-card__icon--blue">
          <el-icon size="22"><DataLine /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">总调用次数</p>
          <p class="up-stat-card__value">{{ fmtNum(statsTotal?.req_count) }}</p>
          <p class="up-stat-card__sub">
            成功
            <span :class="['up-stat-card__rate', successRateClass]">{{ successRate }}%</span>
          </p>
        </div>
      </div>

      <div v-if="isCompactUsageTab && activeTab === 'embedding'" class="up-stat-card">
        <div class="up-stat-card__icon up-stat-card__icon--amber">
          <el-icon size="22"><Coin /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">输入 Token</p>
          <p class="up-stat-card__value">{{ fmtTokens(statsTotal?.prompt_tokens) }}</p>
          <p class="up-stat-card__sub">
            合计 <span class="c-blue">{{ fmtTokens((statsTotal?.prompt_tokens ?? 0) + (statsTotal?.completion_tokens ?? 0)) }}</span>
          </p>
        </div>
      </div>

      <template v-if="!isCompactUsageTab">
      <div class="up-stat-card">
        <div class="up-stat-card__icon up-stat-card__icon--amber">
          <el-icon size="22"><Coin /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">总 Token</p>
          <p class="up-stat-card__value">{{ fmtTokens((statsTotal?.prompt_tokens ?? 0) + (statsTotal?.completion_tokens ?? 0)) }}</p>
          <p class="up-stat-card__sub">
            输入 <span class="c-blue">{{ fmtTokens(statsTotal?.prompt_tokens) }}</span>
            · 输出 <span class="c-green">{{ fmtTokens(statsTotal?.completion_tokens) }}</span>
          </p>
        </div>
      </div>

      <div class="up-stat-card">
        <div class="up-stat-card__icon up-stat-card__icon--cyan">
          <el-icon size="22"><MagicStick /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">缓存命中</p>
          <p class="up-stat-card__value">{{ fmtTokens(statsTotal?.cached_tokens) }}</p>
          <p class="up-stat-card__sub">
            命中率 <span class="c-cyan">{{ cacheHitPct }}%</span>
          </p>
        </div>
      </div>

      <div class="up-stat-card up-stat-card--cost">
        <div class="up-stat-card__icon up-stat-card__icon--green">
          <el-icon size="22"><Money /></el-icon>
        </div>
        <div class="up-stat-card__body">
          <p class="up-stat-card__label">估算成本 ({{ currency }})</p>
          <p class="up-stat-card__value c-green">{{ fmtCost(statsTotal?.est_cost_usd) }}</p>
          <p class="up-stat-card__sub">
            均次 <span class="c-orange">{{ avgCostPerReq }}</span>
            <el-tooltip v-if="statsTotal && statsTotal.req_count > 0 && !statsTotal.est_cost_usd" :content="`当前筛选条件下的模型未配置定价，估算成本为 ${currencySymbol(cc.currency)}0`" placement="top">
              <el-icon class="up-no-price-icon"><WarningFilled /></el-icon>
            </el-tooltip>
          </p>
        </div>
      </div>
      </template>
    </div>

    <!-- ═══════════════════════════════════════════
         图表区
    ═══════════════════════════════════════════ -->
    <div class="up-charts" v-if="statsRows.length">
      <!-- 趋势 / Token 分布图 -->
      <div class="up-chart-card">
        <div class="up-chart-card__header">
          <span class="up-chart-card__title">{{ isCompactUsageTab ? '请求趋势' : 'Token 趋势' }}</span>
          <el-radio-group v-if="!isCompactUsageTab" v-model="chartMetric" size="small" @change="renderTrend">
            <el-radio-button value="tokens">Token</el-radio-button>
            <el-radio-button value="reqs">调用</el-radio-button>
            <el-radio-button value="cost">成本</el-radio-button>
          </el-radio-group>
        </div>
        <div class="up-chart-card__body">
          <canvas ref="trendCanvas" />
        </div>
      </div>

      <!-- 模型分布图（Embedding/Rerank 精简：仅调用量） -->
      <div class="up-chart-card">
        <div class="up-chart-card__header">
          <span class="up-chart-card__title">Top 模型</span>
          <el-radio-group v-if="!isCompactUsageTab" v-model="distMetric" size="small" @change="renderDist">
            <el-radio-button value="reqs">调用量</el-radio-button>
            <el-radio-button value="tokens">Token</el-radio-button>
          </el-radio-group>
        </div>
        <div class="up-chart-card__body">
          <canvas ref="distCanvas" />
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         汇总表（按天/模型/部门/Key）
    ═══════════════════════════════════════════ -->
    <div class="up-section">
      <div class="up-section__header">
        <span class="up-section__title">用量汇总</span>
        <el-radio-group v-model="groupBy" size="small" @change="loadStats">
          <el-radio-button value="hour">按小时</el-radio-button>
          <el-radio-button value="day">按天</el-radio-button>
          <el-radio-button value="week">按周</el-radio-button>
          <el-radio-button value="month">按月</el-radio-button>
          <el-radio-button value="model">按模型</el-radio-button>
          <el-radio-button value="dept">按部门</el-radio-button>
          <el-radio-button value="key">按 Key</el-radio-button>
        </el-radio-group>
      </div>
      <!-- 实体维度数量提示 -->
      <div v-if="isEntityGroupBy && statsTotalCount > 0" class="up-topn-hint">
        <el-icon><InfoFilled /></el-icon>
        按用量排序，共 {{ statsTotalCount }} 条
      </div>
      <div class="up-table-wrap" v-loading="statsLoading">
        <el-table :data="statsPageRows" size="small" :header-cell-style="TABLE_HEADER" style="width:100%"
          :default-sort="{ prop: 'est_cost_usd', order: 'descending' }">
          <el-table-column v-if="groupBy==='hour'" prop="stat_date" label="小时" min-width="140" sortable>
            <template #default="{ row }">{{ row.stat_date }}</template>
          </el-table-column>
          <el-table-column v-else-if="groupBy==='day'" prop="stat_date" label="日期" min-width="100" sortable>
            <template #default="{ row }">{{ fmtDate(row.stat_date) }}</template>
          </el-table-column>
          <el-table-column v-else-if="groupBy==='week'" prop="stat_date" label="周起始" min-width="110" sortable>
            <template #default="{ row }">{{ fmtDate(row.stat_date) }}</template>
          </el-table-column>
          <el-table-column v-else-if="groupBy==='month'" prop="stat_date" label="月份" min-width="90" sortable>
            <template #default="{ row }">{{ row.stat_date }}</template>
          </el-table-column>
          <el-table-column v-if="groupBy==='model'" prop="model" label="模型" min-width="160" sortable>
            <template #default="{ row }"><span class="up-model-name">{{ row.model || '—' }}</span></template>
          </el-table-column>
          <el-table-column v-if="groupBy==='dept'" prop="dept_name" label="部门" min-width="130" sortable>
            <template #default="{ row }">
              <span v-if="row.dept_name" class="up-name">{{ row.dept_name }}</span>
              <span v-else-if="row.dept_id" class="up-mono up-mono--sm">{{ row.dept_id?.substring(0, 8) }}…</span>
              <span v-else class="up-dash">—</span>
            </template>
          </el-table-column>
          <el-table-column v-if="groupBy==='key'" prop="key_name" label="Key" min-width="160" sortable>
            <template #default="{ row }">
              <el-tooltip v-if="row.key_name" :content="maskKey(row.key_id)" placement="top">
                <span class="up-name">{{ row.key_name }}</span>
              </el-tooltip>
              <span v-else class="up-mono up-mono--sm">{{ maskKey(row.key_id) }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="req_count" label="调用" min-width="80" align="right" sortable>
            <template #default="{ row }">{{ fmtNum(row.req_count) }}</template>
          </el-table-column>
          <el-table-column prop="prompt_tokens" label="输入 Token" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="up-bar-cell">
                <span>{{ fmtTokens(row.prompt_tokens) }}</span>
                <div class="up-minibar"><div class="up-minibar__fill up-minibar__fill--blue" :style="{ width: pct(row.prompt_tokens, maxPrompt) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="cached_tokens" label="缓存命中" min-width="100" align="right" sortable>
            <template #default="{ row }">
              <span class="c-cyan">{{ fmtTokens(row.cached_tokens) }}</span>
              <span class="up-pct" v-if="row.prompt_tokens">{{ Math.round(row.cached_tokens / row.prompt_tokens * 100) }}%</span>
            </template>
          </el-table-column>
          <el-table-column prop="completion_tokens" label="输出 Token" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="up-bar-cell">
                <span>{{ fmtTokens(row.completion_tokens) }}</span>
                <div class="up-minibar"><div class="up-minibar__fill up-minibar__fill--green" :style="{ width: pct(row.completion_tokens, maxCompletion) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="est_cost_usd" label="估算成本" min-width="110" align="right" sortable>
            <template #default="{ row }">
              <div class="up-bar-cell">
                <span :class="costColor(row.est_cost_usd)">{{ fmtCost(row.est_cost_usd) }}</span>
                <div class="up-minibar"><div class="up-minibar__fill up-minibar__fill--red" :style="{ width: pct(row.est_cost_usd, maxCost) + '%' }" /></div>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="!statsLoading && !statsRows.length" class="up-empty">
          <el-icon size="48" color="#c0c4cc"><Histogram /></el-icon>
          <p>暂无汇总数据</p>
        </div>
      </div>
      <!-- 统计表格分页（仅实体维度且有多页时显示） -->
      <div v-if="isEntityGroupBy && statsTotalCount > statsPageSize" class="up-stats-pager">
        <el-pagination
          v-model:current-page="statsPage"
          :page-size="statsPageSize"
          :total="statsTotalCount"
          layout="prev, pager, next, total"
          small
          background
          @current-change="(p: number) => { statsPage = p; loadStats(false) }"
        />
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         调用明细 (UsageLogSection 复合组件)
    ═══════════════════════════════════════════ -->
    <UsageLogSection
      ref="logSectionRef"
      title="调用明细"
      api-path="/api/v1/usage-logs"
      trace-api-path="/api/v1/usage/logs"
      :params="logParams"
      :currency="currency"
      export-prefix="usage_logs"
      :table-props="logTableProps"
    />

    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import {
  Refresh, Histogram, Money, DataLine, Coin,
  MagicStick, WarningFilled,
} from '@element-plus/icons-vue'
import UsageLogSection from '@/components/usage/UsageLogSection.vue'
import UsageFilterBar from '@/components/usage/UsageFilterBar.vue'
import VideoKpiCards from '@/components/usage/VideoKpiCards.vue'
import VideoTrendChart from '@/components/usage/VideoTrendChart.vue'
import VideoDistributionCharts from '@/components/usage/VideoDistributionCharts.vue'
import VideoTaskTable from '@/components/usage/VideoTaskTable.vue'
import {
  Chart, BarController, LineController, CategoryScale, LinearScale, BarElement, LineElement,
  PointElement, Tooltip, Legend, Filler
} from 'chart.js'
import http from '@/services/http'
import { ElMessage } from 'element-plus'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'
import type { UsageEndpointType } from '@/utils/usage-endpoint'
import { fmtDateTime, fmtDate, fmtDuration } from '@/utils/time'
import {
  countActiveFilters,
  isStatusCompatible,
  videoModelNameParam,
} from '@/utils/usageFilterConfig'

Chart.register(
  BarController, LineController,
  CategoryScale, LinearScale, BarElement, LineElement, PointElement,
  Tooltip, Legend, Filler,
)

const settingsStore = useSettingsStore()
const currency = ref<Currency>('CNY')
const cc = computed(() => ({
  currency: currency.value,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
  borderBottom: '1px solid var(--el-border-color-light)',
}

// 与主题（品牌绿）一致的图表配色
const CHART_C = {
  input:  '#36B34E', // 输入 = 品牌绿
  output: '#0EA371', // 输出 = 成功祖母绿
  cache:  '#5DADE2', // 缓存 = 信息蓝
  cost:   '#E0A100', // 成本 = 金
  req:    '#E0A100', // 调用 = 金
}
const CHART_PALETTE = ['#36B34E', '#0EA371', '#E0A100', '#5DADE2', '#8B5CF6', '#EC4899', '#E67E22', '#14B8A6']

// ── Types ──
interface StatsRow {
  stat_date?: string; model?: string
  dept_id?: string; dept_name?: string
  key_id?: string;   key_name?: string
  req_count: number; success_count?: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number
}
interface StatsTotal { req_count: number; success_count?: number; prompt_tokens: number; cached_tokens: number; completion_tokens: number; est_cost_usd: number }

// ── Date shortcuts ──
const fmt = (d: Date) => d.toISOString().split('T')[0]
const today = new Date()
const daysAgo = (n: number) => { const d = new Date(today); d.setDate(d.getDate() - n); return d }
const dateRange = ref<[string, string]>([fmt(daysAgo(6)), fmt(today)])
const videoDateStart = computed(() => dateRange.value?.[0] || '')
const videoDateEnd = computed(() => dateRange.value?.[1] || '')
const dateShortcuts = [
  { text: '今天',       value: () => [fmt(today), fmt(today)] },
  { text: '昨天',       value: () => { const d = fmt(daysAgo(1)); return [d, d] } },
  { text: '最近 7 天',  value: () => [fmt(daysAgo(6)), fmt(today)] },
  { text: '最近 30 天', value: () => [fmt(daysAgo(29)), fmt(today)] },
  { text: '本月',       value: () => [fmt(new Date(today.getFullYear(), today.getMonth(), 1)), fmt(today)] },
]

// ── Date presets ──
const datePresets = [
  { label: '今天',   days: 0 },
  { label: '近3天',  days: 2 },
  { label: '近7天',  days: 6 },
  { label: '近14天', days: 13 },
  { label: '近30天', days: 29 },
]
const activePreset = ref('近7天')

function applyPreset(p: { label: string; days: number }) {
  const end = new Date()
  const start = new Date()
  start.setDate(end.getDate() - p.days)
  dateRange.value = [fmt(start), fmt(end)]
  activePreset.value = p.label
  bumpVideoOrLoadAll()
}

function onDateChange() {
  activePreset.value = ''
  bumpVideoOrLoadAll()
}

// ── Filters ──
const filterKeyId    = ref('')
const filterKeyName  = ref('')
const filterKeyType  = ref('')
const filterUserName = ref('')
const filterDeptName = ref('')
const filterStudioId = ref('')
const filterProvider = ref('')
const filterStatus   = ref('')
const filterModelName = ref('')
const filterGroupName = ref('')
const filterEndpoint  = ref('')
const activeTab = ref('chat')

const isCompactUsageTab = computed(() =>
  activeTab.value === 'embedding' || activeTab.value === 'rerank' || activeTab.value === 'rerank_vl',
)

const logEndpointType = computed((): UsageEndpointType => {
  if (activeTab.value === 'embedding' || activeTab.value === 'rerank' || activeTab.value === 'rerank_vl') return activeTab.value
  return 'chat'
})

const logTableProps = computed(() => ({
  showKey: !isCompactUsageTab.value,
  showKeyType: !isCompactUsageTab.value,
  showUser: !isCompactUsageTab.value,
  showDept: !isCompactUsageTab.value,
  showStudio: !isCompactUsageTab.value,
  showGroup: !isCompactUsageTab.value,
  showProvider: true,
  showSource: isCompactUsageTab.value ? false : 'full',
  sortableColumns: ['duration_ms', 'est_cost_usd', 'ts'] as const,
  endpointType: logEndpointType.value,
  keyResolver: (row: any) => keyLabel(row.key_id, row.key_name ?? ''),
}))

const videoRefreshToken = ref(0)
const videoModelParam = computed(() =>
  videoModelNameParam(filterGroupName.value, filterModelName.value),
)

function bumpVideoOrLoadAll() {
  if (activeTab.value === 'video') {
    videoRefreshToken.value++
    return
  }
  loadAll()
}

function onFilterChange() {
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
  if (tab === 'video') {
    videoRefreshToken.value++
    return
  }
  if (tab === 'embedding' || tab === 'rerank') {
    chartMetric.value = 'reqs'
    distMetric.value = 'reqs'
  } else if (tab === 'chat' && chartMetric.value === 'reqs') {
    chartMetric.value = 'tokens'
  }
  loadAll()
}

const activeFilterCount = computed(() =>
  countActiveFilters('cost', activeTab.value as any, {
    groupName: filterGroupName.value,
    modelName: filterModelName.value,
    keyName: filterKeyName.value,
    keyType: filterKeyType.value,
    userName: filterUserName.value,
    deptName: filterDeptName.value,
    studioId: filterStudioId.value,
    provider: filterProvider.value,
    status: filterStatus.value,
  }),
)

function clearFilters() {
  filterModelName.value = ''
  filterGroupName.value = ''
  filterKeyName.value   = ''
  filterKeyType.value   = ''
  filterUserName.value  = ''
  filterKeyId.value     = ''
  filterDeptName.value    = ''
  filterStudioId.value  = ''
  filterProvider.value  = ''
  filterStatus.value    = ''
  filterEndpoint.value  = ''
  bumpVideoOrLoadAll()
}
const groupBy = ref('day')

// ── 统计表格分页（后端真实分页）──
const statsPage      = ref(1)
const statsPageSize  = 10
const statsTotalCount = ref(0)        // 后端返回的实体总数
const isEntityGroupBy = computed(() => true) // 所有维度均支持后端分页
const statsPageRows   = computed(() => {
  // 时间维度不分页，直接返回全量
  if (!isEntityGroupBy.value) return statsRows.value
  // 实体维度已由后端分页，返回的就是当页数据
  return statsRows.value
})

// ── Stats state ──
const statsLoading = ref(false)
const statsRows = ref<StatsRow[]>([])
const statsTotal = ref<StatsTotal | null>(null)
const chartMetric = ref<'tokens' | 'reqs' | 'cost'>('tokens')
const distMetric = ref<'reqs' | 'tokens'>('reqs')

// ── Logs (UsageLogSection) ──
const logSectionRef = ref<InstanceType<typeof UsageLogSection> | null>(null)

const logParams = computed(() => {
  if (!dateRange.value?.[0]) return {}
  const p: Record<string, string | number> = {
    from: dateRange.value[0], to: dateRange.value[1],
  }
  if (filterModelName.value) p.model_name = filterModelName.value
  if (filterGroupName.value) p.group_name = filterGroupName.value
  if (filterKeyId.value)     p.key_id     = filterKeyId.value
  if (filterKeyName.value)   p.key_name   = filterKeyName.value
  if (filterUserName.value)  p.user_name  = filterUserName.value
  if (filterDeptName.value)  p.dept_name  = filterDeptName.value
  if (filterStudioId.value)  p.studio_id  = filterStudioId.value
  if (filterProvider.value)  p.provider   = filterProvider.value
  if (filterStatus.value)    p.status     = filterStatus.value
  if (filterKeyType.value)   p.key_type   = filterKeyType.value
  if (filterEndpoint.value)  p.endpoint   = filterEndpoint.value
  return p
})

// ── Derived stats ──
const successRate = computed(() => {
  const t = statsTotal.value
  if (!t?.req_count) return 100
  return Math.round((t.success_count ?? t.req_count) / t.req_count * 100)
})
const successRateClass = computed(() => {
  const r = successRate.value
  return r >= 95 ? 'c-green' : r >= 80 ? 'c-orange' : 'c-red'
})
const cacheHitPct = computed(() => {
  const t = statsTotal.value
  if (!t?.prompt_tokens) return 0
  return Math.round(t.cached_tokens / t.prompt_tokens * 100)
})
const avgCostPerReq = computed(() => {
  const t = statsTotal.value
  if (!t?.req_count) return '0.000000'
  return fmtCost(t.est_cost_usd / t.req_count)
})

// ── Table bar helpers ──
const maxPrompt     = computed(() => Math.max(...statsRows.value.map(r => r.prompt_tokens || 0), 1))
const maxCompletion = computed(() => Math.max(...statsRows.value.map(r => r.completion_tokens || 0), 1))
const maxCost       = computed(() => Math.max(...statsRows.value.map(r => r.est_cost_usd || 0), 0.0001))
const pct = (v: number, max: number) => Math.min(Math.round(v / max * 100), 100)
const costColor = (v: number) => v > maxCost.value * 0.7 ? 'c-red fw6' : v > maxCost.value * 0.3 ? 'c-orange fw6' : 'c-green'

// ── Formatters ──
const fmtNum = (n?: number | null) => n?.toLocaleString() ?? '0'
/** 动态精度：保留足够有效数字，自动去掉尾零 */
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
const fmtCost6 = (v?: number | null): string => {
  const n = convertCost(Number(v || 0), cc.value)
  return `${currencySymbol(cc.value.currency)}${n.toFixed(6)}`
}
const fmtPrice = (n?: number | null) => {
  const sym = currencySymbol(cc.value.currency)
  if (n == null) return '—'
  if (n === 0) return `${sym}0`
  const v = convertCost(n, cc.value)
  return sym + (Math.abs(v) >= 1 ? v.toFixed(2) : v.toFixed(4).replace(/0+$/, '').replace(/\.$/, ''))
}
const fmtTokens = (n?: number | null) => {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}
const maskKey = (k?: string) => !k ? '—' : k.length > 14 ? k.slice(0, 6) + '···' + k.slice(-4) : k

/** 优先显示 key_name，其次从 keyOptions 查名字，最后回退脱敏 ID */
function keyLabel(keyId: string, keyName: string): string {
  if (keyName) return keyName
  const found = keyOptions.value.find(k => k.value === keyId)
  return found?.label || maskKey(keyId)
}


// ── Filter options ──
const keyOptions     = ref<{ value: string; label: string }[]>([])
const deptOptions    = ref<{ value: string; label: string }[]>([])
const studioOptions  = ref<{ value: string; label: string }[]>([])
const providerOptions = ref<string[]>([])

async function loadOptions() {
  try {
    const r = await http.get('/api/v1/usage/options')
    providerOptions.value = r.data.providers || []
    if (r.data.depts?.length) {
      deptOptions.value = r.data.depts.map((d: any) => ({
        value: d.id, label: d.name || d.id
      }))
    }
    if (r.data.studios?.length) {
      studioOptions.value = r.data.studios.map((s: any) => ({
        value: s.id, label: s.name || s.id
      }))
    }
  } catch { /* ignore */ }
}

function refreshFilterOptions(rows: StatsRow[], gb: string) {
  if (gb === 'key') {
    const seen = new Set<string>()
    keyOptions.value = rows
      .filter(r => r.key_id && !seen.has(r.key_id) && seen.add(r.key_id))
      .map(r => ({ value: r.key_id!, label: r.key_name || maskKey(r.key_id) }))
  }
}

// ── Load ──
async function loadStats(resetPage = true) {
  if (!dateRange.value?.[0]) return
  statsLoading.value = true
  if (resetPage) statsPage.value = 1  // 切换维度或刷新时重置到第一页
  try {
    const params: Record<string, string> = {
      from: dateRange.value[0], to: dateRange.value[1],
      group_by: groupBy.value,
    }
    // 实体维度传分页参数
    if (isEntityGroupBy.value) {
      params.page      = String(statsPage.value)
      params.page_size = String(statsPageSize)
    }
    if (filterModelName.value) params.model_name = filterModelName.value
    if (filterGroupName.value) params.group_name = filterGroupName.value
    if (filterKeyId.value)     params.key_id     = filterKeyId.value
    if (filterKeyName.value)   params.key_name   = filterKeyName.value
    if (filterUserName.value)  params.user_name  = filterUserName.value
    if (filterDeptName.value)  params.dept_name  = filterDeptName.value
    if (filterStudioId.value)  params.studio_id  = filterStudioId.value
    if (filterProvider.value)  params.provider   = filterProvider.value
    if (filterStatus.value)    params.status     = filterStatus.value
    if (filterEndpoint.value) params.endpoint   = filterEndpoint.value
    const r = await http.get('/api/v1/usage', { params })
    statsRows.value       = r.data.rows || []
    statsTotal.value      = r.data.total || null
    statsTotalCount.value = r.data.total_count ?? statsRows.value.length
    refreshFilterOptions(statsRows.value, groupBy.value)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.details || e?.response?.data?.error || '用量查询失败')
  } finally {
    statsLoading.value = false
  }
  // 图表渲染与数据查询解耦：渲染异常不应误报“查询失败”
  await nextTick()
  try {
    renderTrend()
    renderDist()
  } catch (e) {
    console.error('[usage] chart render failed', e)
  }
}

function loadAll() {
  if (activeTab.value === 'video') {
    videoRefreshToken.value++
    return
  }
  loadStats()
  nextTick(() => logSectionRef.value?.refresh())
}



// ── Charts ──
const trendCanvas = ref<HTMLCanvasElement | null>(null)
const distCanvas  = ref<HTMLCanvasElement | null>(null)
let trendChart: Chart | null = null
let distChart:  Chart | null = null

function renderTrend() {
  const canvas = trendCanvas.value
  if (!canvas || !statsRows.value.length) return
  trendChart?.destroy()

  const labels = statsRows.value.map(r => {
    if (groupBy.value === 'hour')  return r.stat_date ?? '—'
    if (groupBy.value === 'day')   return fmtDate(r.stat_date)
    if (groupBy.value === 'week')  return fmtDate(r.stat_date)
    if (groupBy.value === 'month') return r.stat_date?.slice(0, 7) ?? '—'  // "2026-06"
    if (groupBy.value === 'model') return r.model || '未知'
    if (groupBy.value === 'dept')  return r.dept_name || r.dept_id || '未知'
    return r.key_name || maskKey(r.key_id)
  })

  const isLine = ['hour', 'day', 'week', 'month'].includes(groupBy.value) && statsRows.value.length > 1
  const type = isLine ? 'line' : 'bar'

  let datasets: any[]
  if (chartMetric.value === 'tokens') {
    datasets = [
      { label: '输入', data: statsRows.value.map(r => r.prompt_tokens), backgroundColor: CHART_C.input + '33', borderColor: CHART_C.input, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
      { label: '输出', data: statsRows.value.map(r => r.completion_tokens), backgroundColor: CHART_C.output + '33', borderColor: CHART_C.output, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
      { label: '缓存', data: statsRows.value.map(r => r.cached_tokens), backgroundColor: CHART_C.cache + '22', borderColor: CHART_C.cache, borderWidth: 1.5, borderDash: [4, 3], fill: false, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
    ]
  } else if (chartMetric.value === 'cost') {
    datasets = [{ label: '成本 (' + currency.value + ')', data: statsRows.value.map(r => r.est_cost_usd), backgroundColor: CHART_C.cost + '33', borderColor: CHART_C.cost, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 }]
  } else {
    datasets = [
      { label: '调用', data: statsRows.value.map(r => r.req_count), backgroundColor: CHART_C.req + '33', borderColor: CHART_C.req, borderWidth: 2, fill: true, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
      { label: '成功', data: statsRows.value.map(r => r.success_count ?? r.req_count), backgroundColor: CHART_C.output + '22', borderColor: CHART_C.output, borderWidth: 2, fill: false, tension: 0.35, pointRadius: 0, pointHoverRadius: 4 },
    ]
  }

  trendChart = new Chart(canvas, {
    type,
    data: { labels, datasets },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: {
        legend: { position: 'top', labels: { usePointStyle: true, pointStyle: 'line', padding: 12, color: '#64748B', font: { size: 11 } } },
        tooltip: {
          callbacks: {
            label: ctx => {
              const v = ctx.raw as number
              if (chartMetric.value === 'cost') return `${ctx.dataset.label}: ${currencySymbol(cc.value.currency)}${convertCost(Number(ctx.raw as number), cc.value).toFixed(4)}`
              if (chartMetric.value === 'tokens') return `${ctx.dataset.label}: ${fmtTokens(v)}`
              return `${ctx.dataset.label}: ${fmtNum(v)}`
            }
          }
        }
      },
      scales: {
        x: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', font: { size: 10 }, maxRotation: 30 } },
        y: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', font: { size: 10 }, callback: (v: any) => chartMetric.value === 'tokens' ? fmtTokens(v) : chartMetric.value === 'cost' ? `${currencySymbol(cc.value.currency)}${convertCost(Number(v), cc.value).toFixed(2)}` : fmtNum(v) } }
      }
    }
  } as any)
}

function renderDist() {
  const canvas = distCanvas.value
  if (!canvas || !statsRows.value.length) return
  distChart?.destroy()

  // 按模型维度聚合
  const modelMap = new Map<string, { reqs: number; tokens: number }>()
  for (const r of statsRows.value) {
    const key = groupBy.value === 'model' ? (r.model || '未知') : (groupBy.value === 'dept' ? (r.dept_name || r.dept_id || '未知') : (groupBy.value === 'key' ? (r.key_name || maskKey(r.key_id)) : fmtDate(r.stat_date)))
    const cur = modelMap.get(key) || { reqs: 0, tokens: 0 }
    modelMap.set(key, { reqs: cur.reqs + r.req_count, tokens: cur.tokens + r.prompt_tokens + r.completion_tokens })
  }
  const sorted = [...modelMap.entries()]
    .sort((a, b) => b[1][distMetric.value === 'reqs' ? 'reqs' : 'tokens'] - a[1][distMetric.value === 'reqs' ? 'reqs' : 'tokens'])
    .slice(0, 8)
  const labels = sorted.map(([k]) => k)
  const data = sorted.map(([, v]) => distMetric.value === 'reqs' ? v.reqs : v.tokens)

  distChart = new Chart(canvas, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: distMetric.value === 'reqs' ? '调用次数' : 'Token 用量',
        data,
        backgroundColor: CHART_PALETTE.map(c => c + 'CC'),
        borderColor:     CHART_PALETTE,
        borderWidth: 0, borderRadius: 6, barThickness: 'flex', maxBarThickness: 26
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false, indexAxis: 'y',
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: ctx => distMetric.value === 'reqs' ? `${fmtNum(ctx.raw as number)} 次` : fmtTokens(ctx.raw as number) } }
      },
      scales: {
        x: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', font: { size: 10 }, callback: (v: any) => distMetric.value === 'reqs' ? fmtNum(v) : fmtTokens(v) } },
        y: { grid: { display: false }, border: { display: false }, ticks: { color: '#64748B', font: { size: 11 } } }
      }
    }
  })
}



watch(distMetric, () => { if (statsRows.value.length) renderDist() })
watch(currency, async () => { await nextTick(); if (statsRows.value.length) { renderTrend(); renderDist() } })

// init
loadOptions()
loadAll()
</script>

<style scoped>
/* ── 调色板（与 Flip7 Teal 主题统一，深浅色自适应） ── */
.up {
  --up-input:  #36B34E;
  --up-output: #0EA371;
  --up-cache:  #5DADE2;
  --up-cost:   #C7900A;
  --up-reason: #8B5CF6;
  --up-danger: #E74C3C;
  --up-warn:   #E0A100;

  --up-surface: var(--el-fill-color-blank);
  --up-border:  var(--el-border-color-light);
  --up-muted:   var(--el-text-color-secondary);
  --up-faint:   var(--el-text-color-placeholder);
  --up-ink:     var(--el-text-color-regular);
  --up-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  min-height: 100vh; box-sizing: border-box;
  display: flex; flex-direction: column; gap: 16px;
  overflow-y: auto;
}

/* ── 页头 ── */
.up-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.up-header__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.up-header__right { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.up-title {
  font-size: 20px; font-weight: 700; margin: 0; letter-spacing: .2px;
  color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 10px;
}
.up-title::before {
  content: ''; width: 4px; height: 18px; border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1F8A3B));
}
.up-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}

/* ── 卡片通用 ── */
.up-filter, .up-stat-card, .up-chart-card, .up-section {
  background: var(--up-surface);
  border: 1px solid var(--up-border);
  border-radius: 14px;
}

/* ── 筛选栏 ── */
.up-filter {
  display: flex; flex-direction: column; gap: 0; padding: 0;
}

/* 主筛选行：日期 + 激活提示 */
.up-filter-main {
  display: flex; align-items: center; gap: 12px;
  padding: 14px 20px 12px;
  border-bottom: 1px solid var(--up-border);
}
.up-filter__date { width: 220px; flex-shrink: 0; }
.up-presets { display: flex; align-items: center; gap: 4px; }
.up-preset-btn {
  padding: 3px 10px;
  border-radius: 20px;
  border: 1px solid #dcdfe6;
  background: #f5f7fa;
  color: #606266;
  font-size: 13px;
  cursor: pointer;
  transition: all .15s;
  white-space: nowrap;
  line-height: 1.6;
}
.up-preset-btn:hover { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.up-preset-btn--active { border-color: #409eff; color: #fff; background: #409eff; font-weight: 500; }
.up-filter-divider { width: 1px; height: 22px; background: #e4e7ed; margin: 0 6px; flex-shrink: 0; }
.up-filter-active { display: flex; align-items: center; gap: 8px; margin-left: 6px; }
.up-filter-clear {
  font-size: 13px; color: var(--el-color-warning); padding: 0;
  height: auto; line-height: 1;
}

/* 次筛选行：维度过滤 */
.up-filter-dims {
  display: flex; align-items: center; gap: 0;
  flex-wrap: wrap; padding: 12px 20px;
  background: var(--el-fill-color-extra-light);
  row-gap: 10px;
}

/* 更多筛选按钮和额外筛选项容器（仅移动端折叠） */
.up-more-filters-btn {
  display: none !important;
}

.up-filter-dims-extra {
  display: contents;
}

@media (max-width: 768px) {
  .up-more-filters-btn {
    display: inline-flex !important;
    align-self: flex-start;
    font-size: 13px;
    color: var(--el-color-primary);
  }
  .up-filter-dims-extra {
    display: none;
    width: 100%;
    gap: 8px;
    flex-direction: column;
    align-items: stretch;
  }
  .up-filter-dims-extra--open {
    display: flex;
  }
  .up-filter-dims-extra .up-filter-sep {
    display: none;
  }
  .up-filter-dims-extra .up-filter-group {
    width: 100%;
  }
}
.up-filter-group {
  display: flex; align-items: center; gap: 7px; flex-shrink: 0;
}
.up-filter-label {
  font-size: 13px; font-weight: 500;
  color: var(--el-text-color-regular);
  white-space: nowrap; flex-shrink: 0;
  letter-spacing: .1px;
}

/* 分隔线 */
.up-filter-sep {
  width: 1px; height: 20px;
  background: var(--up-border);
  margin: 0 14px; flex-shrink: 0;
}

/* 覆写 el-input / el-select 内字体大小（size=small 默认 12px，调到 13px） */
.up-filter-dims :deep(.el-input__inner),
.up-filter-dims :deep(.el-select__selected-item),
.up-filter-dims :deep(.el-select__placeholder) {
  font-size: 13px !important;
}
.up-filter-main :deep(.el-input__inner),
.up-filter-main :deep(.el-range-input) {
  font-size: 13px !important;
}

.up-filter__group    { width: 140px; }
.up-filter__model    { width: 140px; }
.up-filter__provider { width: 120px; }
.up-filter__status   { width: 100px; }
.up-filter__key      { width: 150px; }
.up-filter__user     { width: 130px; }
.up-filter__dept     { width: 130px; }
.up-filter__studio   { width: 130px; }
.up-filter__endpoint { width: 130px; }

/* 状态选项着色 */
.up-status-opt { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.up-status-opt--ok::before  { content: ''; display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #4ade80; }
.up-status-opt--err::before { content: ''; display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #f87171; }

/* ── 统计卡片 ── */
.up-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; }
@media (max-width: 1100px) { .up-stats { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px)  { .up-stats { grid-template-columns: 1fr 1fr; } }

.up-stat-card {
  position: relative; overflow: hidden;
  padding: 18px; display: flex; align-items: flex-start; gap: 14px;
  min-width: 0; transition: box-shadow .25s ease, transform .25s ease, border-color .25s ease;
}
.up-stat-card::after {
  content: ''; position: absolute; inset: 0 0 auto 0; height: 3px;
  background: var(--accent, var(--el-color-primary)); opacity: .9;
}
.up-stat-card:hover {
  box-shadow: 0 8px 24px rgba(54, 179, 78, .14);
  transform: translateY(-2px);
  border-color: var(--el-color-primary-light-5);
}

.up-stat-card__icon {
  width: 46px; height: 46px; border-radius: 13px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.up-stat-card__icon--blue   { background: color-mix(in srgb, var(--up-input) 14%, transparent);  color: var(--up-input); }
.up-stat-card__icon--amber  { background: color-mix(in srgb, var(--up-warn) 16%, transparent);   color: var(--up-warn); }
.up-stat-card__icon--cyan   { background: color-mix(in srgb, var(--up-cache) 16%, transparent);  color: var(--up-cache); }
.up-stat-card__icon--green  { background: color-mix(in srgb, var(--up-output) 14%, transparent); color: var(--up-output); }

.up-stat-card:nth-child(1) { --accent: var(--up-input); }
.up-stat-card:nth-child(2) { --accent: var(--up-warn); }
.up-stat-card:nth-child(3) { --accent: var(--up-cache); }
.up-stat-card:nth-child(4) { --accent: var(--up-output); }

.up-stat-card__body { min-width: 0; flex: 1; }
.up-stat-card__label { font-size: 12px; color: var(--up-muted); margin: 0 0 6px; white-space: nowrap; }
.up-stat-card__value {
  font-size: 25px; font-weight: 700; color: var(--el-text-color-primary);
  margin: 0 0 6px; line-height: 1.15; letter-spacing: -.3px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.up-stat-card__sub  { font-size: 12px; color: var(--up-muted); margin: 0; display: flex; gap: 5px; flex-wrap: wrap; align-items: center; }
.up-no-price-icon   { color: #f59e0b; cursor: help; font-size: 13px; }
.up-stat-card__rate { font-weight: 600; }
.up-stat-card--cost {}

/* ── 图表区 ── */
.up-charts { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
@media (max-width: 900px) { .up-charts { grid-template-columns: 1fr; } }

.up-chart-card { overflow: hidden; }
.up-chart-card__header,
.up-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-bottom: 1px solid var(--up-border); flex-wrap: wrap; gap: 8px;
}
.up-chart-card__title, .up-section__title {
  font-size: 14px; font-weight: 600; color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 8px;
}
.up-chart-card__title::before, .up-section__title::before {
  content: ''; width: 7px; height: 7px; border-radius: 999px;
  background: var(--el-color-primary);
}
.up-chart-card__body { padding: 14px 16px; height: 228px; position: relative; }

/* ── 区块 ── */
.up-section { overflow: hidden; }
.up-section__count { font-size: 12px; color: var(--up-muted); }
.up-table-wrap { overflow-x: auto; }

/* ── 表格细节 ── */
.up-table-wrap :deep(.el-table) {
  --el-table-border-color: var(--up-border);
  --el-table-bg-color: var(--up-surface);
  --el-table-tr-bg-color: var(--up-surface);
  --el-table-row-hover-bg-color: var(--el-color-primary-light-9);
  background: var(--up-surface);
}
/* 固定列需不透明底色才能遮住滚动内容 */
.up-table-wrap :deep(.el-table td.el-table__cell) {
  background: var(--up-surface);
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.up-table-wrap :deep(.el-table__row:hover > td.el-table__cell) { background: var(--el-color-primary-light-9) !important; }
/* 固定列分隔阴影更柔和 */
.up-table-wrap :deep(.el-table .el-table-fixed-column--left.is-last-column.el-table__cell),
.up-table-wrap :deep(.el-table__fixed) {
  box-shadow: 6px 0 12px -6px rgba(0, 0, 0, .12);
}

/* ── 空态 ── */
.up-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 48px 24px; }
.up-empty p { font-size: 13px; color: var(--up-faint); margin: 0; }
.up-topn-hint { display: flex; align-items: center; gap: 5px; padding: 6px 16px; font-size: 12px; color: var(--el-color-info); background: var(--el-color-info-light-9); border-radius: 4px; margin: 8px 0 2px; }
.up-stats-pager { display: flex; justify-content: flex-end; padding: 10px 4px 4px; }

/* ── 分页 ── */
.up-pagination { padding: 12px 16px; border-top: 1px solid var(--up-border); display: flex; justify-content: flex-end; }

/* ── 等宽文本 ── */
.up-mono { font-family: var(--up-mono); font-size: 11px; color: var(--up-muted); }
.up-mono--sm { font-size: 10px; }

/* ── 模型名 ── */
.up-model-name { font-size: 13px; font-weight: 500; color: var(--el-text-color-regular); }
.up-model-name--muted { color: var(--up-muted); font-weight: 400; }
.up-model-cell { display: flex; flex-direction: column; gap: 2px; }
.up-model-routed { display: flex; align-items: center; gap: 3px; }
.up-arrow { color: var(--up-faint); font-size: 10px; }

/* ── 模型组列换行 ── */
.up-col--wrap { white-space: normal !important; word-break: break-all !important; overflow: visible !important; text-overflow: clip !important; }
.up-tag { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
.up-tag--group { max-width: none; white-space: normal; word-break: break-all; }
.up-group-name { font-size: 11px; color: var(--up-muted); word-break: break-all; line-height: 1.5; }
.up-provider { font-size: 11px; color: var(--up-muted); }
.up-dash { color: var(--up-faint); }

.up-token-row { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.up-token-item {
  display: inline-flex; align-items: center; gap: 3px; font-size: 11px; font-weight: 600;
  padding: 1px 7px; border-radius: 999px;
}
.up-token-item--in    { color: var(--up-input);  background: color-mix(in srgb, var(--up-input) 12%, transparent); }
.up-token-item--out   { color: var(--up-output); background: color-mix(in srgb, var(--up-output) 12%, transparent); }
.up-token-item--cache { color: var(--up-cache);  background: color-mix(in srgb, var(--up-cache) 14%, transparent); }
.up-token-item--reason{ color: var(--up-reason); background: color-mix(in srgb, var(--up-reason) 12%, transparent); font-size: 11px; font-weight: 600; }
.up-token-icon { font-size: 10px; }

.up-time { font-size: 12px; color: var(--up-muted); white-space: nowrap; font-family: var(--up-mono); }
.up-duration { font-size: 12px; color: var(--up-ink); font-variant-numeric: tabular-nums; }
.up-cost { font-family: var(--up-mono); font-size: 12px; font-weight: 600; font-variant-numeric: tabular-nums; }
.up-flags { display: flex; flex-wrap: wrap; gap: 3px; justify-content: center; }
.up-pct { font-size: 10px; color: var(--up-cache); margin-left: 4px; }

/* ── 迷你进度条 ── */
.up-bar-cell { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
.up-minibar { width: 100%; height: 4px; background: var(--el-fill-color); border-radius: 999px; overflow: hidden; }
.up-minibar__fill { height: 100%; border-radius: 999px; transition: width .4s ease; }
.up-minibar__fill--blue  { background: linear-gradient(90deg, color-mix(in srgb, var(--up-input) 45%, transparent), var(--up-input)); }
.up-minibar__fill--green { background: linear-gradient(90deg, color-mix(in srgb, var(--up-output) 45%, transparent), var(--up-output)); }
.up-minibar__fill--red   { background: linear-gradient(90deg, color-mix(in srgb, var(--up-cost) 45%, transparent), var(--up-cost)); }

/* ── 颜色工具（与主题统一） ── */
.c-blue   { color: var(--up-input); }
.c-green  { color: var(--up-output); }
.c-cyan   { color: var(--up-cache); }
.c-orange { color: var(--up-warn); }
.c-amber  { color: var(--up-warn); }
.c-purple { color: var(--up-reason); }
.c-red    { color: var(--up-danger); }
.fw6      { font-weight: 600; }

.up-tag--effort { font-size: 10px; }

/* ── 耗时 / TTFT ── */
.up-timing { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; }
.up-ttft { font-size: 10px; color: var(--up-cache); }

/* ── 成本格 ── */
.up-cost-cell { display: flex; align-items: center; gap: 6px; justify-content: flex-end; white-space: nowrap; }

/* ── 圆形信息触发按钮（参考 sub2api） ── */
.up-info {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; border-radius: 999px; flex-shrink: 0;
  background: var(--el-fill-color); color: var(--up-faint);
  cursor: help; transition: background .15s ease, color .15s ease, transform .15s ease;
}
.up-info:hover { background: var(--el-color-primary-light-9); color: var(--el-color-primary); transform: scale(1.08); }
.up-info .el-icon { font-size: 11px; }

/* ── UA / IP 来源格 ── */
.up-source { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.up-ua { display: block; font-size: 11px; color: var(--up-muted); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: help; }
.up-ip { font-family: var(--up-mono); font-size: 10px; color: var(--up-faint); }

/* ── 用户名 ── */
.up-name { font-size: 12px; color: var(--el-text-color-regular); font-weight: 500; }

/* Mobile responsive for very small screens */
@media (max-width: 640px) {
  .up-header {
    flex-direction: column;
    gap: 8px;
  }

  .up-header__right {
    width: 100%;
    justify-content: flex-end;
  }

  .up-title {
    font-size: 16px;
  }

  .up-stats {
    grid-template-columns: 1fr !important;
  }

  .up-filter-main {
    flex-wrap: wrap;
    gap: 6px;
  }

  .up-presets {
    flex-wrap: wrap;
  }

  .up-filter-dims {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }

  .up-filter-group {
    width: 100%;
    flex-wrap: wrap;
  }

  .up-filter__group,
  .up-filter__model,
  .up-filter__provider,
  .up-filter__status,
  .up-filter__key,
  .up-filter__user,
  .up-filter__dept,
  .up-filter__studio,
  .up-filter__endpoint {
    width: 100% !important;
  }

  .up-filter-sep {
    display: none;
  }

  .up-section__header {
    flex-direction: column;
    align-items: flex-start;
  }

  .up-chart-card__header {
    flex-direction: column;
    align-items: flex-start;
  }

  .up-chart-card__body {
    height: 180px;
  }
}
</style>

