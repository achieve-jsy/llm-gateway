<template>
  <div class="ud">

    <!-- ══ 页头 ══ -->
    <div class="ud-header">
      <div class="ud-header__left">
        <h2 class="ud-title">用量看板</h2>
        <span class="ud-badge">{{ scopeLabel }}</span>
      </div>
      <div class="ud-header__right">
        <el-radio-group v-model="currency" size="small">
          <el-radio-button value="USD">$ USD</el-radio-button>
          <el-radio-button value="CNY">¥ CNY</el-radio-button>
        </el-radio-group>
        <el-button :loading="summaryLoading || logsLoading" :icon="Refresh"
          circle size="small" title="刷新" @click="loadAll" />
      </div>
    </div>

    <!-- ══ 筛选栏 ══ -->
    <div class="ud-filter">
      <div class="ud-filter-row ud-filter-row--date">
        <div class="ud-presets">
          <button
            v-for="p in datePresets" :key="p.label"
            :class="['ud-preset-btn', { 'ud-preset-btn--active': activePreset === p.label }]"
            @click="applyPreset(p)"
          >{{ p.label }}</button>
        </div>
        <div class="ud-filter-divider" />
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          range-separator="至"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          value-format="YYYY-MM-DD"
          format="MM/DD"
          class="ud-filter__date"
          size="default"
          @change="onDateChange"
        />
        <!-- 移动端筛选折叠按钮 -->
        <button class="ud-filter-toggle" @click="showFilters = !showFilters">
          <el-icon size="14"><component :is="showFilters ? 'ArrowUp' : 'ArrowDown'" /></el-icon>
          筛选
          <span v-if="activeFilterCount > 0" class="ud-filter-badge">{{ activeFilterCount }}</span>
        </button>
      </div>
      <Transition name="ud-filter-collapse">
        <div v-show="showFilters" class="ud-filter-row ud-filter-row--dims">
          <!-- 成员姓名：非普通用户可见 -->
          <div v-if="authStore.effectiveRole !== 'user'" class="ud-filter-chip">
            <span class="ud-chip-label">成员</span>
            <el-input v-model="filterUserName" placeholder="姓名搜索" clearable size="small"
              class="ud-chip-input" @change="onUserNameChange" @clear="onUserNameClear">
              <template #prefix><el-icon><User /></el-icon></template>
            </el-input>
          </div>
          <!-- 模型 -->
          <div class="ud-filter-chip">
            <span class="ud-chip-label">模型</span>
            <el-input v-model="filterModel" placeholder="搜索模型名" clearable size="small"
              class="ud-chip-input" @change="loadAll" @clear="loadAll">
              <template #prefix><el-icon><Search /></el-icon></template>
            </el-input>
          </div>
          <!-- 状态 -->
          <div class="ud-filter-chip">
            <span class="ud-chip-label">状态</span>
            <el-select v-model="filterStatus" placeholder="全部" clearable size="small"
              class="ud-chip-select" @change="loadAll" @clear="loadAll">
              <el-option label="全部" value="" />
              <el-option value="success">
                <div style="display:flex;align-items:center;gap:6px"><span class="ud-status-dot ud-status-dot--ok" />成功</div>
              </el-option>
              <el-option value="error">
                <div style="display:flex;align-items:center;gap:6px"><span class="ud-status-dot ud-status-dot--err" />失败</div>
              </el-option>
            </el-select>
          </div>
          <el-button v-if="activeFilterCount > 0" link size="small" type="warning" @click="clearFilters">
            清空 {{ activeFilterCount }} 个筛选
          </el-button>
        </div>
      </Transition>
    </div>

    <!-- ══ KPI 卡片 ══ -->
    <div class="ud-kpi-row" v-loading="summaryLoading">
      <!-- 总成本 -->
      <div class="ud-kpi-card ud-kpi-card--green">
        <div class="ud-kpi__icon"><el-icon size="20"><Money /></el-icon></div>
        <div class="ud-kpi__body">
          <p class="ud-kpi__label">总成本</p>
          <p class="ud-kpi__value">{{ fmtCost(summary?.cost_estimate) }}</p>
          <div class="ud-kpi__change-row">
            <template v-if="costChange !== null">
              <el-tooltip :content="prevPeriodLabel" placement="bottom" :show-after="300">
                <span :class="['ud-change-badge', changeClass(costChange, true)]">
                  <el-icon size="15"><component :is="costChange >= 0 ? ArrowUp : ArrowDown" /></el-icon>
                  <b>{{ fmtChange(costChange) }}</b>
                  <span class="ud-change-badge__vs">vs 上期</span>
                </span>
              </el-tooltip>
              <span class="ud-kpi__prev-val">上期 {{ fmtCost(summary?.prev_cost_estimate) }}</span>
            </template>
            <span v-else class="ud-kpi__sub">暂无上期数据</span>
          </div>
          <!-- 预算进度 -->
          <template v-if="currentBudget">
            <div :class="['ud-kpi__budget', `ud-kpi__budget--${budgetLevel}`]">
              <div class="ud-kpi__budget-hdr">
                <span>预算</span>
                <span>{{ fmtCost(currentBudget.used_usd) }} / {{ fmtCost(currentBudget.amount_usd) }}
                  ({{ Math.round((currentBudget.used_ratio || 0) * 100) }}%)</span>
              </div>
              <div class="ud-kpi__budget-track">
                <div class="ud-kpi__budget-fill"
                  :style="{ width: Math.min((currentBudget.used_ratio || 0) * 100, 100) + '%' }" />
              </div>
            </div>
          </template>
        </div>
      </div>
      <!-- 总调用 -->
      <div class="ud-kpi-card ud-kpi-card--blue">
        <div class="ud-kpi__icon"><el-icon size="20"><DataLine /></el-icon></div>
        <div class="ud-kpi__body">
          <p class="ud-kpi__label">总调用次数</p>
          <p class="ud-kpi__value">{{ fmtNum(summary?.call_count) }}</p>
          <div class="ud-kpi__change-row">
            <template v-if="callsChange !== null">
              <el-tooltip :content="prevPeriodLabel" placement="bottom" :show-after="300">
                <span :class="['ud-change-badge', changeClass(callsChange)]">
                  <el-icon size="15"><component :is="callsChange >= 0 ? ArrowUp : ArrowDown" /></el-icon>
                  <b>{{ fmtChange(callsChange) }}</b>
                  <span class="ud-change-badge__vs">vs 上期</span>
                </span>
              </el-tooltip>
              <span class="ud-kpi__prev-val">上期 {{ fmtNum(summary?.prev_call_count) }}</span>
            </template>
            <span v-else class="ud-kpi__sub">暂无上期数据</span>
          </div>
        </div>
      </div>
      <!-- 成功率 -->
      <div class="ud-kpi-card" :class="successRate >= 99 ? 'ud-kpi-card--green' : successRate >= 95 ? 'ud-kpi-card--amber' : 'ud-kpi-card--red'">
        <div class="ud-kpi__icon"><el-icon size="20"><CircleCheck /></el-icon></div>
        <div class="ud-kpi__body">
          <p class="ud-kpi__label">成功率</p>
          <p class="ud-kpi__value">{{ successRate.toFixed(1) }}%</p>
          <div class="ud-kpi__change-row">
            <span class="ud-kpi__sub">失败 {{ fmtNum(summary?.error_count) }} 次</span>
            <template v-if="successRateChange !== null">
              <span :class="['ud-change-badge ud-change-badge--sm', successRateChange >= 0 ? 'ud-change--good' : 'ud-change--bad']">
                <el-icon size="11"><component :is="successRateChange >= 0 ? ArrowUp : ArrowDown" /></el-icon>
                {{ successRateChange >= 0 ? '+' : '' }}{{ successRateChange.toFixed(2) }}pp
              </span>
            </template>
          </div>
        </div>
      </div>
      <!-- 活跃成员 / 个人视角下换成 Token 消耗 -->
      <div class="ud-kpi-card ud-kpi-card--purple">
        <div class="ud-kpi__icon"><el-icon size="20"><component :is="currentScope === 'user' ? List : User" /></el-icon></div>
        <template v-if="currentScope !== 'user'">
          <div class="ud-kpi__body">
            <p class="ud-kpi__label">活跃成员</p>
            <p class="ud-kpi__value">{{ summary?.user_count ?? '—' }}</p>
            <div class="ud-kpi__change-row">
              <span class="ud-kpi__sub">均次成本 {{ avgCostPerCall }}</span>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="ud-kpi__body">
            <p class="ud-kpi__label">Token 消耗</p>
            <p class="ud-kpi__value">{{ summary?.total_tokens != null ? (summary.total_tokens >= 1000000 ? (summary.total_tokens/1000000).toFixed(2)+'M' : (summary.total_tokens/1000).toFixed(1)+'K') : '—' }}</p>
            <div class="ud-kpi__change-row">
              <span class="ud-kpi__sub">均次成本 {{ avgCostPerCall }}</span>
            </div>
          </div>
        </template>
      </div>
      <!-- 异常概览 -->
      <div
        class="ud-kpi-card ud-kpi-card--anomaly"
        :class="(summary?.error_count || 0) > 0 ? 'ud-kpi-card--red' : 'ud-kpi-card--amber'"
        style="cursor:pointer"
        title="点击筛选失败调用"
        @click="filterToErrors"
      >
        <div class="ud-kpi__icon"><el-icon size="20"><WarningFilled /></el-icon></div>
        <div class="ud-kpi__body">
          <p class="ud-kpi__label">失败调用</p>
          <p class="ud-kpi__value">{{ fmtNum(summary?.error_count) }}</p>
          <div class="ud-kpi__change-row">
            <el-tooltip v-if="anomalyStats.failover > 0" content="故障转移次数（本页）" placement="bottom" :show-after="300">
              <span class="ud-anomaly-chip ud-anomaly-chip--warn">切换 {{ anomalyStats.failover }}</span>
            </el-tooltip>
            <el-tooltip v-if="anomalyStats.highLatency > 0" content="耗时 >10s 的高延迟调用（本页）" placement="bottom" :show-after="300">
              <span class="ud-anomaly-chip ud-anomaly-chip--orange">慢 {{ anomalyStats.highLatency }}</span>
            </el-tooltip>
            <span v-if="anomalyStats.failover === 0 && anomalyStats.highLatency === 0" class="ud-kpi__sub">
              {{ (summary?.error_count || 0) === 0 ? '运行正常' : '点击查看' }}
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- ══ 趋势图 ══ -->
    <div class="ud-section">
      <div class="ud-section__header">
        <span class="ud-section__title">调用趋势</span>
        <div class="ud-trend-header-ctrls">
          <div class="ud-trend-tabs">
            <button
              v-for="m in trendMetrics" :key="m.value"
              :class="['ud-trend-tab', { 'ud-trend-tab--active': trendMetric === m.value }]"
              @click="switchTrendMetric(m.value)"
            >{{ m.label }}</button>
          </div>
          <div class="ud-trend-tabs">
            <button :class="['ud-trend-tab', { 'ud-trend-tab--active': trendGranularity === 'day' }]" @click="switchTrendGranularity('day')">日</button>
            <button :class="['ud-trend-tab', { 'ud-trend-tab--active': trendGranularity === 'week' }]" @click="switchTrendGranularity('week')">周</button>
            <button :class="['ud-trend-tab', { 'ud-trend-tab--active': trendGranularity === 'month' }]" @click="switchTrendGranularity('month')">月</button>
          </div>
        </div>
      </div>
      <div class="ud-chart-wrap" v-loading="trendLoading">
        <canvas ref="trendCanvasRef" class="ud-trend-canvas" />
        <div v-if="!trendLoading && trendData.length === 0" class="ud-chart-empty">暂无趋势数据</div>
      </div>
    </div>

    <!-- ══ 花销构成 ══ -->
    <div v-if="showCostBreakdown" class="ud-section">
      <div class="ud-section__header">
        <span class="ud-section__title">花销构成</span>
        <span class="ud-section__hint">{{ currentScope === 'dept' ? '成员' : '子部门' }} · {{ dateRange[0] }} ~ {{ dateRange[1] }}</span>
      </div>
      <div class="ud-breakdown-body">
        <!-- 预算池 -->
        <div v-if="budgetPoolItems.length > 0" class="ud-breakdown-budgets">
          <div v-for="item in budgetPoolItems" :key="item.scope_type + item.scope_id" class="ud-budget-pool">
            <div class="ud-budget-pool__hdr">
              <span class="ud-budget-pool__label">{{ item.label }}</span>
              <span :class="['ud-budget-pool__ratio', item.used_ratio >= 1 ? 'c-red fw6' : item.used_ratio >= 0.8 ? 'c-orange fw6' : 'c-green fw6']">
                {{ Math.round((item.used_ratio || 0) * 100) }}%
              </span>
            </div>
            <el-progress
              :percentage="Math.min((item.used_ratio || 0) * 100, 100)"
              :status="item.used_ratio >= 1 ? 'exception' : item.used_ratio >= 0.8 ? 'warning' : 'success'"
              :stroke-width="6"
              :show-text="false"
              class="ud-budget-progress"
            />
            <div class="ud-budget-pool__amts">
              <span>已用 {{ fmtCost(item.used_usd) }}</span>
              <span>共 {{ fmtCost(item.amount_usd) }}</span>
            </div>
          </div>
        </div>
        <!-- Top 5 成员/部门 -->
        <div class="ud-breakdown-top5">
          <div class="ud-breakdown-heading">{{ currentScope === 'dept' ? '成员' : '子部门' }}花销 Top 5</div>
          <div v-if="top5Breakdown.length > 0" class="ud-top5-list">
            <div v-for="(row, i) in top5Breakdown" :key="row.id ?? i" class="ud-top5-item">
              <span :class="['ud-rank-num', i < 3 ? `ud-rank-num--top${i+1}` : '']">{{ i + 1 }}</span>
              <span class="ud-top5-name">{{ row.name || row.id || '—' }}</span>
              <div class="ud-top5-bar"><div class="ud-top5-fill" :style="{ width: barPct(row.cost_estimate, top5Max) + '%' }" /></div>
              <span class="ud-top5-cost c-green fw6">{{ fmtCost(row.cost_estimate) }}</span>
              <span class="ud-top5-pct">{{ barPct(row.cost_estimate, totalCost) }}%</span>
            </div>
          </div>
          <div v-else class="ud-chart-empty" style="position:static;padding:16px 0">暂无数据</div>
        </div>
      </div>
    </div>

    <!-- ══ 成本归因 + 模型分布 ══ -->
    <div class="ud-main-row">

      <!-- 成本归因排行 -->
      <div class="ud-section">

        <!-- 个人视角提示条 -->
        <div v-if="currentScope === 'user'" class="ud-user-scope-banner">
          <el-icon><User /></el-icon>
          <span>当前仅展示 <strong>{{ breadcrumbs[breadcrumbs.length - 1]?.name }}</strong> 的个人用量数据</span>
          <el-button link size="small" type="primary" style="margin-left:auto" @click="resetToTop">
            退出个人视角
          </el-button>
        </div>

        <div class="ud-section__header">
          <div class="ud-section__title-row">
            <span class="ud-section__title">{{ rankTitle }}</span>
            <el-input
              v-model="rankSearch"
              placeholder="搜索名称"
              clearable
              size="small"
              class="ud-rank-search"
              @input="rankPage = 1"
            >
              <template #prefix><el-icon><Search /></el-icon></template>
            </el-input>
          </div>
          <div class="ud-breadcrumb">
            <span class="ud-bc-home" @click="resetToTop">{{ topLabel }}</span>
            <template v-for="(crumb, idx) in breadcrumbs" :key="crumb.id">
              <el-icon class="ud-bc-sep"><ArrowRight /></el-icon>
              <span
                :class="['ud-bc-item', { 'ud-bc-current': idx === breadcrumbs.length - 1 }]"
                @click="idx < breadcrumbs.length - 1 && popToLevel(idx)"
              >{{ crumb.name }}</span>
            </template>
          </div>
        </div>

        <div class="ud-table-wrap" v-loading="summaryLoading">
          <table class="ud-rank-table">
            <thead>
              <tr>
                <th class="col-rank">#</th>
                <th class="col-name">名称</th>
                <th>
                  <el-tooltip content="该成员成本占本期团队总成本的比例" placement="top">
                    <span style="cursor:help;border-bottom:1px dashed currentColor">成本占比</span>
                  </el-tooltip>
                </th>
                <th class="col-num col-sortable" @click="toggleSort('cost')">
                  成本 ({{ currency }})
                  <span class="ud-sort-arrows">
                    <span :class="['ud-sort-arrow', rankSort.field === 'cost' && rankSort.order === 'asc' ? 'ud-sort-arrow--active' : '']">▲</span>
                    <span :class="['ud-sort-arrow', rankSort.field === 'cost' && rankSort.order === 'desc' ? 'ud-sort-arrow--active' : '']">▼</span>
                  </span>
                </th>
                <th class="col-num col-sortable" @click="toggleSort('calls')">
                  调用次数
                  <span class="ud-sort-arrows">
                    <span :class="['ud-sort-arrow', rankSort.field === 'calls' && rankSort.order === 'asc' ? 'ud-sort-arrow--active' : '']">▲</span>
                    <span :class="['ud-sort-arrow', rankSort.field === 'calls' && rankSort.order === 'desc' ? 'ud-sort-arrow--active' : '']">▼</span>
                  </span>
                </th>
                <th class="col-num col-sortable" @click="toggleSort('tokens')">
                  {{ currentScope === 'user' ? 'Token 消耗' : '活跃用户' }}
                  <span class="ud-sort-arrows">
                    <span :class="['ud-sort-arrow', rankSort.field === 'tokens' && rankSort.order === 'asc' ? 'ud-sort-arrow--active' : '']">▲</span>
                    <span :class="['ud-sort-arrow', rankSort.field === 'tokens' && rankSort.order === 'desc' ? 'ud-sort-arrow--active' : '']">▼</span>
                  </span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, i) in displayedBreakdown"
                :key="row.id ?? i"
                :class="{ 'ud-rank-drillable': canDrill }"
              >
                <td class="col-rank">
                  <span :class="['ud-rank-num', i < 3 ? `ud-rank-num--top${i+1}` : '']">{{ row.rank ?? i + 1 }}</span>
                </td>
                <td class="col-name">
                  <span
                    :class="['ud-row-name', { drillable: canDrill }]"
                    @click="canDrill && drillDown(row)"
                  >{{ row.name || row.id || '(未归属)' }}</span>
                </td>
                <td>
                  <div class="ud-bar-wrap">
                    <div class="ud-bar-bg">
                      <div class="ud-bar-fill ud-bar-fill--cost" :style="{ width: barPct(row.cost_estimate, totalCost) + '%' }" />
                    </div>
                    <span class="ud-bar-pct">{{ barPct(row.cost_estimate, totalCost) }}%</span>
                  </div>
                </td>
                <td class="col-num c-green fw6">{{ fmtCost(row.cost_estimate) }}</td>
                <td class="col-num">{{ fmtNum(row.call_count) }}</td>
                <!-- user scope: 展示 token，否则展示活跃用户数 -->
                <td class="col-num">
                  <template v-if="currentScope === 'user'">
                    {{ row.total_tokens != null ? (row.total_tokens >= 1000000 ? (row.total_tokens/1000000).toFixed(1)+'M' : (row.total_tokens/1000).toFixed(1)+'K') : '—' }}
                  </template>
                  <template v-else>{{ row.user_count ?? '—' }}</template>
                </td>
              </tr>
              <tr v-if="!summaryLoading && displayedBreakdown.length === 0">
                <td colspan="6" class="ud-empty">暂无数据</td>
              </tr>
            </tbody>
          </table>
          <div v-if="rankTotal > rankPageSize" class="ud-pagination">
            <el-pagination
              v-model:current-page="rankPage"
              :page-size="rankPageSize"
              :total="rankTotal"
              layout="total, prev, pager, next"
              small
              @current-change="loadSummary"
            />
          </div>
        </div>
      </div>

      <!-- 模型分布 -->
      <div class="ud-section">
        <div class="ud-section__header">
          <span class="ud-section__title">模型成本分布</span>
          <div class="ud-model-ctrl">
            <div class="ud-trend-tabs">
              <button :class="['ud-trend-tab', { 'ud-trend-tab--active': modelMetric === 'cost' }]" @click="switchModelMetric('cost')">成本</button>
              <button :class="['ud-trend-tab', { 'ud-trend-tab--active': modelMetric === 'calls' }]" @click="switchModelMetric('calls')">调用量</button>
              <button :class="['ud-trend-tab', { 'ud-trend-tab--active': modelMetric === 'tokens' }]" @click="switchModelMetric('tokens')">Token</button>
            </div>
            <div class="ud-trend-tabs">
              <button :class="['ud-trend-tab', { 'ud-trend-tab--active': modelChartType === 'bar' }]" @click="switchModelChartType('bar')">柱状图</button>
              <button :class="['ud-trend-tab', { 'ud-trend-tab--active': modelChartType === 'doughnut' }]" @click="switchModelChartType('doughnut')">饼图</button>
            </div>
            <span class="ud-section__hint">Top {{ Math.min(modelsData.length, 6) }}{{ modelsData.length > 6 ? ` / 共 ${modelsData.length} 种` : '' }}</span>
          </div>
        </div>
        <div class="ud-chart-wrap ud-chart-wrap--model" v-loading="modelsLoading">
          <canvas ref="modelCanvasRef" class="ud-model-canvas" />
          <div v-if="!modelsLoading && modelsData.length === 0" class="ud-chart-empty">暂无数据</div>
        </div>
      </div>

    </div>

    <!-- ══ 调用明细 ══ -->
    <UsageRecordTable
      :rows="logRows"
      :loading="logsLoading"
      :total="logsTotal"
      :page="logsPage"
      :page-size="logsPageSize"
      :show-key="true"
      :show-user="true"
      user-label="成员"
      :show-studio="authStore.effectiveRole === 'admin'"
      :show-dept="authStore.effectiveRole === 'admin' || authStore.effectiveRole === 'studio_admin'"
      show-source="ip-only"
      :sortable-columns="['duration_ms', 'est_cost_usd', 'ts']"
      :page-sizes="[20, 50, 100]"
      :currency="currency"
      :trace-endpoint="fetchTrace"
      trace-meta-mode="full"
      @update:page="(p) => { logsPage = p; loadLogs() }"
      @update:page-size="(s) => { logsPageSize = s; logsPage = 1; loadLogs() }"
    />

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { Chart, registerables } from 'chart.js'
import {
  Refresh, DataLine, Money, User, ArrowRight, ArrowUp, ArrowDown,
  List, Search, WarningFilled,
} from '@element-plus/icons-vue'
import UsageRecordTable from '@/components/usage/UsageRecordTable.vue'
import type { TraceData } from '@/components/usage/UsageTraceDrawer.vue'
import { useAuthStore } from '@/stores/auth'
import { useOrgStore } from '@/stores/org'
import { useRoute } from 'vue-router'
import http from '@/services/http'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'
import { fmtDateTime, fmtDuration } from '@/utils/time'

Chart.register(...registerables)

const settingsStore = useSettingsStore()
const currency = ref<Currency>('CNY')
const cc = computed(() => ({
  currency: currency.value,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const authStore = useAuthStore()
const orgStore  = useOrgStore()
const route     = useRoute()


// ── 日期 ──────────────────────────────────────────────────────
const fmt = (d: Date) => d.toISOString().split('T')[0]
const today = new Date()
const daysAgo = (n: number) => { const d = new Date(today); d.setDate(d.getDate() - n); return d }

const dateRange = ref<[string, string]>([fmt(daysAgo(29)), fmt(today)])
const activePreset = ref('近30天')

const datePresets = [
  { label: '今天',   days: 0 },
  { label: '近7天',  days: 6 },
  { label: '近30天', days: 29 },
  { label: '近90天', days: 89 },
]

function applyPreset(p: { label: string; days: number }) {
  dateRange.value = [fmt(daysAgo(p.days)), fmt(today)]
  activePreset.value = p.label
  granularityManuallySet = false
  autoSetGranularity()
  loadAll()
}

function onDateChange() {
  activePreset.value = ''
  granularityManuallySet = false
  autoSetGranularity()
  loadAll()
}

// ── 筛选 ──────────────────────────────────────────────────────
const filterUserName = ref('')
const filterModel    = ref('')
const filterStatus   = ref('')
const showFilters    = ref(true)  // 移动端可折叠筛选栏

const activeFilterCount = computed(() =>
  [filterUserName.value, filterModel.value, filterStatus.value].filter(Boolean).length
)

function clearFilters() {
  filterUserName.value = ''
  filterModel.value    = ''
  filterStatus.value   = ''
  // 如果当前处于个人视角（从成员详情跳入），同步退出
  if (breadcrumbs.value[breadcrumbs.value.length - 1]?.scope === 'user') {
    breadcrumbs.value = []
  }
  reloadAll()
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

// 根据当前面包屑最末层 scope 返回对应 API 参数（dept_id / studio_id / user_id）
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

const maxCost = computed(() =>
  Math.max(1, ...breakdown.value.map(r => r.cost_estimate || 0))
)

const totalCost = computed(() =>
  breakdown.value.reduce((sum, r) => sum + (r.cost_estimate || 0), 0)
)

const avgCostPerCall = computed(() => {
  const s = summary.value
  if (!s?.call_count) return '0.00'
  return fmtCost((s.cost_estimate || 0) / s.call_count)
})

const successRate = computed(() => {
  const s = summary.value
  if (!s?.call_count) return 100
  return ((s.call_count - (s.error_count ?? 0)) / s.call_count) * 100
})

// ── 成功率环比 ────────────────────────────────────────────────
const successRateChange = computed(() => {
  const s = summary.value
  if (!s?.prev_call_count) return null
  const prevErrors = s.prev_error_count ?? 0
  const prevRate = s.prev_call_count > 0
    ? ((s.prev_call_count - prevErrors) / s.prev_call_count) * 100
    : 100
  return successRate.value - prevRate
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
  } catch {
    budgetContext.value = []
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

// ── 排行多维度排序 & 搜索 ─────────────────────────────────────
const rankSort = ref<{ field: 'cost' | 'calls' | 'tokens'; order: 'asc' | 'desc' }>({ field: 'cost', order: 'desc' })
const rankSearch = ref('')

const displayedBreakdown = computed(() => {
  let list = [...breakdown.value]
  if (rankSearch.value) {
    const q = rankSearch.value.toLowerCase()
    list = list.filter((r: any) => (r.name || r.id || '').toLowerCase().includes(q))
  }
  const { field, order } = rankSort.value
  const key = field === 'cost' ? 'cost_estimate'
    : field === 'calls' ? 'call_count'
    : currentScope.value === 'user' ? 'total_tokens' : 'user_count'
  list.sort((a: any, b: any) => {
    const av = a[key] ?? 0, bv = b[key] ?? 0
    return order === 'desc' ? bv - av : av - bv
  })
  return list
})

function toggleSort(field: 'cost' | 'calls' | 'tokens') {
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

function fmtChange(pct: number | null): string {
  if (pct === null) return '—'
  return `${pct >= 0 ? '+' : ''}${pct.toFixed(1)}%`
}

// 上期日期范围标签（用于 tooltip）
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

function changeClass(pct: number | null, costMode = false): string {
  if (pct === null) return 'ud-change--neutral'
  if (pct === 0) return 'ud-change--neutral'
  const isUp = pct > 0
  if (costMode) return isUp ? 'ud-change--bad' : 'ud-change--good'
  return isUp ? 'ud-change--up' : 'ud-change--down'
}

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
    const res = await http.get('/api/v1/usage/dashboard', { params })
    summary.value = res.data?.summary ?? null
    breakdown.value = res.data?.breakdown ?? []
    rankTotal.value = res.data?.total ?? 0
  } catch {
    summary.value = null
    breakdown.value = []
  } finally {
    summaryLoading.value = false
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
const trendCanvasRef = ref<HTMLCanvasElement | null>(null)
let trendChartInst: Chart | null = null

const trendMetric = ref<'overview' | 'cost' | 'calls' | 'tokens'>('overview')
const trendMetrics = [
  { value: 'overview', label: '综合' },
  { value: 'cost',     label: '成本' },
  { value: 'calls',    label: '调用量' },
  { value: 'tokens',   label: 'Token' },
] as const

function switchTrendMetric(m: typeof trendMetric.value) {
  trendMetric.value = m
  renderTrendChart()
}

// ── 趋势图粒度 ─────────────────────────────────────────────────
const trendGranularity = ref<'day' | 'week' | 'month'>('day')
let granularityManuallySet = false

function autoSetGranularity() {
  const [start, end] = dateRange.value
  if (!start || !end) return
  const days = Math.round((new Date(end).getTime() - new Date(start).getTime()) / 86400000) + 1
  if (days > 90) trendGranularity.value = 'month'
  else if (days > 30) trendGranularity.value = 'week'
  else trendGranularity.value = 'day'
}

function switchTrendGranularity(g: 'day' | 'week' | 'month') {
  granularityManuallySet = true
  trendGranularity.value = g
  renderTrendChart()
}

function getISOWeekLabel(date: Date): string {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() + 3 - (d.getDay() + 6) % 7)
  const week1 = new Date(d.getFullYear(), 0, 4)
  const weekNum = 1 + Math.round(((d.getTime() - week1.getTime()) / 86400000 - 3 + (week1.getDay() + 6) % 7) / 7)
  return `W${String(weekNum).padStart(2, '0')}`
}

const MONTH_ABBR = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']

function getMonthLabel(date: Date): string {
  return `${MONTH_ABBR[date.getMonth()]} ${date.getFullYear()}`
}

function aggregateTrendData(data: any[], granularity: 'day' | 'week' | 'month'): any[] {
  if (granularity === 'day' || !data.length) return data
  const groups = new Map<string, any>()
  for (const d of data) {
    const dateStr = d.date
    if (!dateStr) continue
    const date = new Date(dateStr)
    const key = granularity === 'week' ? getISOWeekLabel(date) : getMonthLabel(date)
    if (!groups.has(key)) {
      groups.set(key, { date: key, cost: 0, calls: 0, errors: 0, total_tokens: 0 })
    }
    const g = groups.get(key)!
    g.cost         += d.cost         ?? 0
    g.calls        += d.calls        ?? 0
    g.errors       += d.errors       ?? 0
    g.total_tokens += d.total_tokens ?? 0
  }
  return Array.from(groups.values())
}

async function loadTrend() {
  trendLoading.value = true
  try {
    const trendParams: Record<string, any> = { start: dateRange.value[0], end: dateRange.value[1] }
    Object.assign(trendParams, scopeParams())
    if (filterModel.value) trendParams.model_name = filterModel.value
    if (filterStatus.value) trendParams.status = filterStatus.value

    // 计算上期日期范围
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
  } catch {
    trendData.value     = []
    prevTrendData.value = []
  } finally {
    trendLoading.value = false
    await nextTick()
    renderTrendChart()
  }
}

function renderTrendChart() {
  if (!trendCanvasRef.value) return
  trendChartInst?.destroy()
  const g = trendGranularity.value
  const data = aggregateTrendData(trendData.value, g)
  if (!data.length) return

  const isDayView = g === 'day'
  const labels     = data.map(d => isDayView ? (d.date?.slice(5) ?? '') : (d.date ?? ''))
  const costs      = data.map(d => d.cost ?? 0)
  const calls      = data.map(d => d.calls ?? 0)
  const errors     = data.map(d => d.errors ?? 0)
  const tokens     = data.map(d => d.total_tokens ?? 0)
  const showPoints = data.length <= 31
  const metric     = trendMetric.value

  const prevAggData = aggregateTrendData(prevTrendData.value, g)
  const hasPrev     = prevAggData.length > 0
  const alignPrev   = (arr: number[]) => labels.map((_, i) => i < arr.length ? arr[i] : null)

  // 综合模式：成本（折线，左轴）+ 调用（柱，右轴）
  if (metric === 'overview') {
    trendChartInst = new Chart(trendCanvasRef.value, {
      data: {
        labels,
        datasets: [
          {
            type: 'line' as const, label: '成本 (' + currency.value + ')', data: costs,
            yAxisID: 'yCost', borderColor: '#67c23a', backgroundColor: 'rgba(103,194,58,0.10)',
            fill: true, tension: 0.35, pointRadius: showPoints ? 3 : 0, pointHoverRadius: 5, borderWidth: 2, order: 1,
          },
          {
            type: 'bar' as const, label: '调用次数', data: calls,
            yAxisID: 'yCalls', backgroundColor: 'rgba(64,158,255,0.45)', borderRadius: 3, order: 2,
          },
          {
            type: 'bar' as const, label: '失败次数', data: errors,
            yAxisID: 'yCalls', backgroundColor: 'rgba(245,108,108,0.7)', borderRadius: 3, order: 3,
          },
        ],
      },
      options: {
        responsive: true, maintainAspectRatio: false,
        interaction: { intersect: false, mode: 'index' },
        scales: {
          x: { grid: { display: false }, ticks: { maxTicksLimit: 15, font: { size: 11 } } },
          yCost: {
            type: 'linear', position: 'left',
            grid: { color: 'rgba(0,0,0,0.05)' },
            ticks: { font: { size: 11 }, callback: (v: any) => `${currencySymbol(cc.value.currency)}${convertCost(Number(v), cc.value).toFixed(2)}` },
          },
          yCalls: {
            type: 'linear', position: 'right',
            grid: { drawOnChartArea: false },
            ticks: { font: { size: 11 }, callback: (v: any) => Number(v).toLocaleString() },
          },
        },
        plugins: {
          legend: { position: 'bottom', labels: { boxWidth: 12, padding: 16, font: { size: 12 } } },
          tooltip: {
            callbacks: {
              label: (ctx: any) => ctx.dataset.yAxisID === 'yCost'
                ? ` 成本: ${fmtCost(ctx.raw as number)}`
                : ` ${ctx.dataset.label}: ${Number(ctx.raw).toLocaleString()}`,
            },
          },
        },
      },
    } as any)
    return
  }

  // 单指标模式（带上期虚线对比）
  const metricCfg = {
    cost:   { curr: costs,  prev: prevAggData.map(d => d.cost ?? 0),         color: '#67c23a', bgColor: 'rgba(103,194,58,0.12)', prevColor: 'rgba(103,194,58,0.5)', label: '成本 (' + currency.value + ')',  fmtFn: (v: any) => fmtCost(v) },
    calls:  { curr: calls,  prev: prevAggData.map(d => d.calls ?? 0),         color: '#409eff', bgColor: 'rgba(64,158,255,0.15)', prevColor: 'rgba(64,158,255,0.5)', label: '调用次数',    fmtFn: (v: any) => Number(v).toLocaleString() },
    tokens: { curr: tokens, prev: prevAggData.map(d => d.total_tokens ?? 0), color: '#8b5cf6', bgColor: 'rgba(139,92,246,0.12)', prevColor: 'rgba(139,92,246,0.5)', label: 'Token 消耗',  fmtFn: (v: any) => fmtTokens(v) },
  }[metric]!

  const datasets: any[] = [
    {
      label: metricCfg.label, data: metricCfg.curr,
      borderColor: metricCfg.color, backgroundColor: metricCfg.bgColor,
      fill: true, tension: 0.35,
      pointRadius: showPoints ? 3 : 0, pointHoverRadius: 5, borderWidth: 2, order: 1,
    },
  ]

  if (hasPrev) {
    datasets.push({
      label: '上期 ' + metricCfg.label,
      data: alignPrev(metricCfg.prev),
      borderColor: metricCfg.prevColor,
      backgroundColor: 'transparent',
      borderDash: [5, 5],
      fill: false, tension: 0.35,
      pointRadius: 0, pointHoverRadius: 4, borderWidth: 1.5, order: 2,
    })
  }

  trendChartInst = new Chart(trendCanvasRef.value, {
    type: 'line',
    data: { labels, datasets },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      scales: {
        x: { grid: { display: false }, ticks: { maxTicksLimit: 15, font: { size: 11 } } },
        y: {
          grid: { color: 'rgba(0,0,0,0.05)' },
          ticks: { font: { size: 11 }, callback: metricCfg.fmtFn },
        },
      },
      plugins: {
        legend: { display: hasPrev, position: 'bottom', labels: { boxWidth: 12, padding: 16, font: { size: 12 } } },
        tooltip: { callbacks: { label: (ctx: any) => ` ${ctx.dataset.label}: ${metricCfg.fmtFn(ctx.raw)}` } },
      },
    },
  } as any)
}

// ── 模型分布图 ────────────────────────────────────────────────
const modelsLoading = ref(false)
const modelsData    = ref<any[]>([])
const modelCanvasRef = ref<HTMLCanvasElement | null>(null)
let modelChartInst: Chart | null = null

// 模型图类型 & 维度
const modelChartType = ref<'bar' | 'doughnut'>('bar')
const modelMetric    = ref<'cost' | 'calls' | 'tokens'>('cost')

function switchModelChartType(t: 'bar' | 'doughnut') {
  modelChartType.value = t
  renderModelChart()
}

function switchModelMetric(m: 'cost' | 'calls' | 'tokens') {
  modelMetric.value = m
  renderModelChart()
}

const MODEL_COLORS = [
  '#409eff', '#67c23a', '#e6a23c', '#f56c6c',
  '#909399', '#8B5CF6', '#06B6D4', '#F59E0B',
  '#10B981', '#EF4444',
]

async function loadModels() {
  modelsLoading.value = true
  try {
    const modelParams: Record<string, any> = { start: dateRange.value[0], end: dateRange.value[1] }
    Object.assign(modelParams, scopeParams())
    if (filterStatus.value) modelParams.status = filterStatus.value
    const res = await http.get('/api/v1/usage/dashboard/models', { params: modelParams })
    modelsData.value = res.data?.rows ?? []
  } catch {
    modelsData.value = []
  } finally {
    modelsLoading.value = false
    await nextTick()
    renderModelChart()
  }
}

function renderModelChart() {
  if (!modelCanvasRef.value) return
  modelChartInst?.destroy()
  const raw = modelsData.value
  if (!raw.length) return

  const TOP_N = 6
  let data: any[]
  if (raw.length > TOP_N) {
    const top  = raw.slice(0, TOP_N)
    const rest = raw.slice(TOP_N)
    const restCost   = rest.reduce((s: number, d: any) => s + (d.cost ?? 0), 0)
    const restCalls  = rest.reduce((s: number, d: any) => s + (d.calls ?? 0), 0)
    const restTokens = rest.reduce((s: number, d: any) => s + (d.total_tokens ?? 0), 0)
    data = [...top, { model: `其他 (${rest.length} 种)`, cost: restCost, calls: restCalls, total_tokens: restTokens }]
  } else {
    data = raw
  }

  const labels = data.map(d => {
    const m = d.model ?? ''
    return m.length > 28 ? m.slice(0, 26) + '…' : m
  })
  const colors = data.map((_: any, i: number) => MODEL_COLORS[i % MODEL_COLORS.length])

  const metric = modelMetric.value
  const metricData = data.map(d =>
    metric === 'cost' ? (d.cost ?? 0)
    : metric === 'calls' ? (d.calls ?? 0)
    : (d.total_tokens ?? 0)
  )
  const metricLabel = metric === 'cost' ? `成本 (${currency.value})` : metric === 'calls' ? '调用次数' : 'Token 消耗'
  const total = metricData.reduce((s: number, v: number) => s + v, 0)

  const fmtMetric = (v: number) =>
    metric === 'cost' ? fmtCost(v)
    : metric === 'calls' ? v.toLocaleString() + ' 次'
    : fmtTokens(v)

  if (modelChartType.value === 'doughnut') {
    modelChartInst = new Chart(modelCanvasRef.value, {
      type: 'doughnut',
      data: {
        labels,
        datasets: [{
          data: metricData,
          backgroundColor: colors,
          borderColor: 'rgba(255,255,255,0.8)',
          borderWidth: 2,
          hoverOffset: 8,
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            position: 'right',
            labels: {
              boxWidth: 12, padding: 10, font: { size: 11 },
              generateLabels: (chart: any) => {
                const meta = chart.getDatasetMeta(0)
                return (chart.data.labels as string[]).map((label: string, i: number) => ({
                  text: label,
                  fillStyle: colors[i],
                  strokeStyle: colors[i],
                  hidden: meta.data[i]?.hidden,
                  index: i,
                }))
              },
            },
          },
          tooltip: {
            callbacks: {
              label: (ctx: any) => {
                const val = ctx.raw as number
                const pct = total > 0 ? ((val / total) * 100).toFixed(1) : '0.0'
                return ` ${fmtMetric(val)} (${pct}%)`
              },
            },
          },
        },
      },
    } as any)
    return
  }

  // 横向柱状图模式
  modelChartInst = new Chart(modelCanvasRef.value, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: metricLabel,
        data: metricData,
        backgroundColor: colors,
        borderRadius: 4,
        borderSkipped: false,
      }],
    },
    options: {
      indexAxis: 'y' as const,
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (ctx: any) => {
              const val = ctx.raw as number
              const pct = total > 0 ? ((val / total) * 100).toFixed(1) : '0.0'
              return ` ${fmtMetric(val)} (${pct}%)`
            },
          },
        },
      },
      scales: {
        x: {
          grid: { color: 'rgba(0,0,0,0.05)' },
          ticks: {
            font: { size: 11 },
            callback: (v: any) =>
              metric === 'cost' ? `${currencySymbol(cc.value.currency)}${convertCost(Number(v), cc.value).toFixed(2)}`
              : metric === 'calls' ? Number(v).toLocaleString()
              : fmtTokens(Number(v)),
          },
        },
        y: {
          grid: { display: false },
          ticks: { font: { size: 11 } },
        },
      },
    },
  })
}

// ── 调用明细 ──────────────────────────────────────────────────
const logsLoading  = ref(false)
const logRows      = ref<any[]>([])
const logsTotal    = ref(0)
const logsPage     = ref(1)
const logsPageSize = ref(50)


async function loadLogs() {
  logsLoading.value = true
  try {
    const params: Record<string, any> = {
      start: dateRange.value[0],
      end:   dateRange.value[1],
      page:  logsPage.value,
      page_size: logsPageSize.value,
    }
    // 当面包屑下钻时，按 scope 类型传递对应过滤参数
    const sp = scopeParams()
    if (sp.user_id) {
      params.user_id = sp.user_id
    } else if (sp.dept_id) {
      params.dept_id = sp.dept_id
    } else if (sp.studio_id) {
      params.studio_id = sp.studio_id
    } else {
      if (filterUserName.value) params.user_name = filterUserName.value
    }
    if (filterModel.value)    params.model_name = filterModel.value
    if (filterStatus.value)   params.status     = filterStatus.value
    const res = await http.get('/api/v1/usage/scoped/logs', { params })
    logRows.value  = res.data?.rows  ?? []
    logsTotal.value = res.data?.total ?? 0
  } catch {
    logRows.value = []
    logsTotal.value = 0
  } finally {
    logsLoading.value = false
  }
}

function reloadLogs() {
  logsPage.value = 1
  loadLogs()
}

// ── 异常聚合（本页统计） ─────────────────────────────────────
const anomalyStats = computed(() => ({
  failover:    logRows.value.filter(r => r.failover_used).length,
  highLatency: logRows.value.filter(r => (r.duration_ms ?? 0) > 10000).length,
}))

function filterToErrors() {
  filterStatus.value = 'error'
  showFilters.value = true
  loadAll()
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
        // 未匹配到用户 → 设一个不存在的 user_id，让图表返回空
        breadcrumbs.value = [{ scope: 'user' as const, id: '__no_match__', name: filterUserName.value }]
      }
    } catch {
      breadcrumbs.value = [{ scope: 'user' as const, id: '__no_match__', name: filterUserName.value }]
    }
  }
  loadAll()
}

function onUserNameClear() {
  // 清空成员姓名时，同样退出 user scope
  const last = breadcrumbs.value[breadcrumbs.value.length - 1]
  if (last?.scope === 'user') {
    breadcrumbs.value = []
    loadAll()  // 退出 user scope 后刷新全部数据
    return
  }
  reloadLogs()
}


function loadAll() {
  logsPage.value = 1
  loadSummary()
  loadTrend()
  loadModels()
  loadLogs()
  loadBudget()
}

// ── Trace Endpoint ────────────────────────────────────────────
async function fetchTrace(requestId: string): Promise<TraceData> {
  const res = await http.get(`/api/v1/usage/logs/${requestId}/trace`, { _silent: true } as any)
  return res.data
}

// ── 格式化 ────────────────────────────────────────────────────
const fmtNum = (n?: number | null) => (n ?? 0).toLocaleString()
const fmtTokens = (n?: number | null) => {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}
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
const barPct = (v: number, max: number) => !max || !v ? 0 : Math.round(v / max * 100)
const fmtJSON = (v: any): string => {
  if (v == null) return '(无数据)'
  try {
    const obj = typeof v === 'string' ? JSON.parse(v) : v
    return JSON.stringify(obj, null, 2)
  } catch { return String(v) }
}

onMounted(() => {
  // 从组织架构页跳转时，预填成员筛选
  const { user_id, user_name, start, end } = route.query
  if (user_id && user_name) {
    // 将 breadcrumbs 设置到 user 层级，让看板直接展示该成员视角
    breadcrumbs.value = [{ scope: 'user', id: String(user_id), name: String(user_name) }]
    // 同步回显到筛选栏，让用户看到当前按谁过滤
    filterUserName.value = String(user_name)
  }
  if (start && end) {
    dateRange.value = [String(start), String(end)]
  }
  loadAll()
})
onUnmounted(() => {
  trendChartInst?.destroy()
  modelChartInst?.destroy()
})

watch(currency, async () => {
  await nextTick()
  renderTrendChart()
  renderModelChart()
})
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
  display: flex; flex-direction: column; gap: 16px;
}

/* ── 页头 ── */
.ud-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.ud-header__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.ud-header__right { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.ud-title {
  font-size: 20px; font-weight: 700; margin: 0; letter-spacing: .2px;
  color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 10px;
}
.ud-title::before {
  content: ''; width: 4px; height: 18px; border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1F8A3B));
}
.ud-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}

/* ── 卡片通用 ── */
.ud-filter, .ud-section {
  background: var(--ud-surface);
  border: 1px solid var(--ud-border);
  border-radius: 14px;
}

/* ── 筛选栏 ── */
.ud-filter { display: flex; flex-direction: column; overflow: hidden; }

.ud-filter-row {
  display: flex; align-items: center; gap: 10px; padding: 12px 20px;
  flex-wrap: wrap;
}
.ud-filter-row--date { border-bottom: 1px solid var(--ud-border); }
.ud-filter-row--dims { padding-top: 10px; padding-bottom: 10px; gap: 14px; }

.ud-presets { display: flex; align-items: center; gap: 4px; }
.ud-preset-btn {
  padding: 3px 10px; border-radius: 20px;
  border: 1px solid #dcdfe6; background: #f5f7fa;
  color: #606266; font-size: 13px; cursor: pointer;
  transition: all .15s; white-space: nowrap; line-height: 1.6;
}
.ud-preset-btn:hover { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.ud-preset-btn--active { border-color: #409eff; color: #fff; background: #409eff; font-weight: 500; }
.ud-filter__date { width: 220px; flex-shrink: 0; }
.ud-filter-divider { width: 1px; height: 22px; background: #e4e7ed; margin: 0 4px; flex-shrink: 0; }

.ud-filter-chip { display: flex; align-items: center; gap: 6px; }
.ud-chip-label { font-size: 12px; color: var(--ud-muted); white-space: nowrap; }
.ud-chip-input { width: 140px; }
.ud-chip-select { width: 100px; }

/* 移动端筛选折叠按钮 - 桌面端隐藏 */
.ud-filter-toggle {
  display: none;
  align-items: center;
  gap: 4px;
  padding: 6px 12px;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background: #f5f7fa;
  color: #606266;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
  margin-left: auto;
  white-space: nowrap;
}
.ud-filter-toggle:hover { border-color: #409eff; color: #409eff; }
.ud-filter-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 8px;
  background: #409eff;
  color: #fff;
  font-size: 11px;
  font-weight: 600;
}

/* 折叠动画 */
.ud-filter-collapse-enter-active,
.ud-filter-collapse-leave-active {
  transition: all 0.25s ease;
  overflow: hidden;
}
.ud-filter-collapse-enter-from,
.ud-filter-collapse-leave-to {
  opacity: 0;
  max-height: 0;
  padding-top: 0;
  padding-bottom: 0;
}
.ud-filter-collapse-enter-to,
.ud-filter-collapse-leave-from {
  opacity: 1;
  max-height: 200px;
}

/* ── KPI 卡片 ── */
.ud-kpi-row {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 14px;
}
@media (max-width: 1200px) { .ud-kpi-row { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 900px)  { .ud-kpi-row { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 540px)  { .ud-kpi-row { grid-template-columns: 1fr; } }

.ud-kpi-card {
  background: var(--ud-surface);
  border: 1px solid var(--ud-border);
  border-radius: 14px;
  padding: 18px 20px 16px;
  display: flex; align-items: flex-start; gap: 14px;
  position: relative; overflow: hidden;
}
.ud-kpi-card::before {
  content: ''; position: absolute; top: 0; left: 0;
  width: 4px; height: 100%; border-radius: 14px 0 0 14px;
}
.ud-kpi-card--green::before { background: #67c23a; }
.ud-kpi-card--blue::before  { background: #409eff; }
.ud-kpi-card--amber::before { background: #e6a23c; }
.ud-kpi-card--red::before   { background: #f56c6c; }
.ud-kpi-card--purple::before { background: #8B5CF6; }

.ud-kpi__icon {
  width: 40px; height: 40px; border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.ud-kpi-card--green  .ud-kpi__icon { background: rgba(103,194,58,0.12); color: #67c23a; }
.ud-kpi-card--blue   .ud-kpi__icon { background: rgba(64,158,255,0.12); color: #409eff; }
.ud-kpi-card--amber  .ud-kpi__icon { background: rgba(230,162,60,0.12); color: #e6a23c; }
.ud-kpi-card--red    .ud-kpi__icon { background: rgba(245,108,108,0.12); color: #f56c6c; }
.ud-kpi-card--purple .ud-kpi__icon { background: rgba(139,92,246,0.12); color: #8B5CF6; }

.ud-kpi__body { flex: 1; min-width: 0; }
.ud-kpi__label { font-size: 12px; color: var(--ud-muted); margin: 0 0 4px; }
.ud-kpi__value { font-size: 24px; font-weight: 700; margin: 0 0 6px; color: var(--el-text-color-primary); line-height: 1.2; }
.ud-kpi__foot  { display: flex; align-items: center; gap: 6px; font-size: 12px; margin: 0; }
.ud-kpi__vs    { color: var(--ud-faint); }
.ud-kpi__sub   { color: var(--ud-muted); }

/* 环比变化 — 新的 badge 样式（更醒目） */
.ud-kpi__change-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 2px; }
.ud-kpi__prev-val   { font-size: 11px; color: var(--ud-faint); }

.ud-change-badge {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 3px 9px; border-radius: 6px;
  font-size: 13px; cursor: default; white-space: nowrap;
}
.ud-change-badge b { font-size: 14px; font-weight: 700; }
.ud-change-badge__vs { font-size: 11px; opacity: 0.75; }
.ud-change-badge--sm { padding: 2px 7px; font-size: 11px; }
.ud-change-badge--sm b { font-size: 11px; }
.ud-change-badge.ud-change--good { background: rgba(103,194,58,0.14); color: #48a014; }
.ud-change-badge.ud-change--bad  { background: rgba(245,108,108,0.14); color: #d63b3b; }
.ud-change-badge.ud-change--up   { background: rgba(64,158,255,0.14);  color: #1872cc; }
.ud-change-badge.ud-change--down { background: rgba(230,162,60,0.14);  color: #b87112; }
.ud-change-badge.ud-change--neutral { background: var(--el-fill-color); color: var(--ud-muted); }

/* 预算进度条（KPI卡片内） */
.ud-kpi__budget {
  margin-top: 8px; padding: 7px 10px; border-radius: 8px;
  background: var(--el-fill-color-light);
}
.ud-kpi__budget--ok     { border-left: 3px solid #67c23a; }
.ud-kpi__budget--warn   { border-left: 3px solid #e6a23c; }
.ud-kpi__budget--danger { border-left: 3px solid #f56c6c; }
.ud-kpi__budget-hdr {
  display: flex; justify-content: space-between; align-items: center;
  font-size: 11px; color: var(--ud-muted); margin-bottom: 5px;
}
.ud-kpi__budget-track {
  height: 5px; border-radius: 3px;
  background: var(--el-fill-color);
  overflow: hidden;
}
.ud-kpi__budget-fill {
  height: 100%; border-radius: 3px; transition: width .4s ease;
}
.ud-kpi__budget--ok     .ud-kpi__budget-fill { background: linear-gradient(90deg, #67c23a, #95d475); }
.ud-kpi__budget--warn   .ud-kpi__budget-fill { background: linear-gradient(90deg, #e6a23c, #f0bb6e); }
.ud-kpi__budget--danger .ud-kpi__budget-fill { background: linear-gradient(90deg, #f56c6c, #f89898); }

/* ── Section 通用 ── */
.ud-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 20px 12px; border-bottom: 1px solid var(--ud-border);
  flex-wrap: wrap; gap: 8px;
}
.ud-section__title { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.ud-section__count { font-size: 12px; color: var(--ud-muted); }
.ud-section__hint  { font-size: 12px; color: var(--ud-faint); }

/* 异常聚合卡片 chips */
.ud-anomaly-chip {
  display: inline-flex; align-items: center;
  padding: 2px 8px; border-radius: 20px;
  font-size: 11px; font-weight: 600; white-space: nowrap; cursor: default;
}
.ud-anomaly-chip--warn   { background: rgba(230,162,60,0.18); color: #b87112; }
.ud-anomaly-chip--orange { background: rgba(245,108,108,0.18); color: #c0392b; }

/* 趋势图头部双控件行 */
.ud-trend-header-ctrls { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

/* 趋势图指标切换 */
.ud-trend-tabs { display: flex; gap: 2px; background: var(--el-fill-color); border-radius: 6px; padding: 2px; }
.ud-trend-tab {
  padding: 3px 10px; font-size: 12px; border-radius: 4px; border: none; cursor: pointer;
  background: transparent; color: var(--el-text-color-secondary); transition: all .15s;
}
.ud-trend-tab:hover { color: var(--el-text-color-primary); }
.ud-trend-tab--active {
  background: var(--el-bg-color); color: var(--el-color-primary);
  font-weight: 600; box-shadow: 0 1px 3px rgba(0,0,0,.1);
}

/* ── 趋势图 ── */
.ud-chart-wrap {
  padding: 16px 20px;
  height: 260px;
  position: relative;
}
.ud-chart-wrap--model { height: 280px; }
.ud-trend-canvas { width: 100% !important; height: 100% !important; }
.ud-model-canvas { width: 100% !important; height: 100% !important; }
.ud-chart-empty {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--ud-faint); font-size: 13px;
}
.ud-chart-empty-inner {
  display: flex; flex-direction: column; align-items: center; gap: 8px;
  color: var(--ud-faint); font-size: 13px;
}
.ud-chart-empty-inner p { margin: 0; }

/* ── 两列布局 ── */
.ud-main-row {
  display: grid;
  grid-template-columns: 3fr 2fr;
  gap: 16px;
}
@media (max-width: 960px) { .ud-main-row { grid-template-columns: 1fr; } }

/* Mobile responsive for very small screens */
@media (max-width: 640px) {
  .ud {
    overflow-x: auto;
  }

  .ud-header {
    flex-direction: column;
    gap: 8px;
  }

  .ud-header__right {
    width: 100%;
    justify-content: flex-end;
  }

  .ud-title {
    font-size: 16px;
  }

  .ud-filter-row--date {
    flex-wrap: wrap;
    gap: 6px;
  }

  /* 移动端显示筛选折叠按钮 */
  .ud-filter-toggle {
    display: flex;
  }

  .ud-filter-divider {
    display: none;
  }

  .ud-presets {
    flex-wrap: wrap;
  }

  .ud-filter-row--dims {
    flex-wrap: wrap;
    gap: 8px;
  }

  .ud-filter-chip {
    width: 100%;
  }

  .ud-chip-input,
  .ud-chip-select {
    width: 100% !important;
  }

  .ud-kpi-row {
    grid-template-columns: 1fr;
  }

  .ud-section__header {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }

  .ud-trend-tabs {
    width: 100%;
  }

  .ud-trend-tab {
    flex: 1;
    text-align: center;
  }

  :deep(.el-table) {
    min-width: 700px;
  }

  .ud-pagination {
    justify-content: center;
  }

  .ud-chart-wrap {
    height: 200px;
    padding: 12px 14px;
  }

  .ud-chart-wrap--model {
    height: 220px;
  }

  .ud-rank-table {
    font-size: 12px;
  }

  .ud-rank-table th,
  .ud-rank-table td {
    padding: 8px 10px;
  }

  .ud-breadcrumb {
    font-size: 12px;
  }
}

/* ── 排行表格 ── */
.ud-table-wrap { padding: 0; overflow: auto; }
.ud-rank-table {
  width: 100%; border-collapse: collapse;
  font-size: 13px; table-layout: auto;
}
.ud-rank-table thead tr { border-bottom: 1px solid var(--ud-border); }
.ud-rank-table th {
  padding: 10px 14px; text-align: left;
  font-weight: 600; font-size: 12px;
  color: var(--ud-muted); white-space: nowrap;
  background: var(--el-fill-color-light);
}
.ud-rank-table td { padding: 10px 14px; border-bottom: 1px solid var(--ud-border); }
.ud-rank-table tbody tr:last-child td { border-bottom: none; }
.ud-rank-table tbody tr:hover td { background: var(--el-fill-color-extra-light); }
.ud-rank-drillable td { cursor: pointer; }

.col-rank { width: 40px; text-align: center; }
.col-name { min-width: 120px; }
.col-num  { text-align: right; white-space: nowrap; }

.ud-rank-num {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px; border-radius: 6px;
  background: var(--el-fill-color); color: var(--ud-muted);
  font-size: 12px; font-weight: 600;
}
.ud-rank-num--top1 { background: #ffd700; color: #7a5f00; }
.ud-rank-num--top2 { background: #c0c0c0; color: #555; }
.ud-rank-num--top3 { background: #cd7f32; color: #fff; }

.ud-row-name { color: var(--el-text-color-primary); }
.ud-row-name.drillable { color: var(--el-color-primary); cursor: pointer; font-weight: 500; }
.ud-row-name.drillable:hover { text-decoration: underline; }

.ud-bar-wrap { display: flex; align-items: center; gap: 8px; min-width: 120px; }
.ud-bar-bg   { flex: 1; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; }
.ud-bar-fill { height: 100%; border-radius: 3px; transition: width .3s; background: var(--el-color-primary); }
.ud-bar-fill--cost { background: linear-gradient(90deg, #67c23a, #95d475); }
.ud-bar-pct  { font-size: 12px; color: var(--ud-muted); white-space: nowrap; min-width: 36px; }
.ud-empty    { padding: 32px; text-align: center; color: var(--ud-faint); }

/* ── 面包屑 ── */
/* 个人视角提示条 */
.ud-user-scope-banner {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 14px; margin-bottom: 12px;
  background: #ecf5ff; border: 1px solid #b3d8ff;
  border-radius: 6px; font-size: 13px; color: #409eff;
}
.ud-user-scope-banner strong { color: #1a6fd4; }

.ud-breadcrumb { display: flex; align-items: center; gap: 4px; font-size: 13px; flex-wrap: wrap; }
.ud-bc-home { color: var(--el-color-primary); cursor: pointer; font-weight: 500; }
.ud-bc-home:hover { text-decoration: underline; }
.ud-bc-sep  { color: var(--ud-faint); font-size: 11px; }
.ud-bc-item { color: var(--ud-muted); cursor: pointer; }
.ud-bc-item:hover { color: var(--el-color-primary); }
.ud-bc-current { color: var(--el-text-color-primary); font-weight: 500; cursor: default; }

/* ── 排行表格排序 & 搜索 ── */
.ud-section__title-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ud-rank-search { width: 140px; }

.col-sortable { cursor: pointer; user-select: none; }
.col-sortable:hover { color: var(--el-color-primary); }
.ud-sort-arrows { display: inline-flex; flex-direction: column; margin-left: 4px; vertical-align: middle; line-height: 1; gap: 0; }
.ud-sort-arrow  { font-size: 8px; color: var(--el-border-color); line-height: 1; display: block; }
.ud-sort-arrow--active { color: var(--el-color-primary); }

/* ── 模型图控制栏 ── */
.ud-model-ctrl { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

/* ── 花销构成区块 ── */
.ud-breakdown-body {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 0;
}
@media (max-width: 768px) { .ud-breakdown-body { grid-template-columns: 1fr; } }

.ud-breakdown-budgets {
  border-right: 1px solid var(--ud-border);
  padding: 16px 20px;
  display: flex; flex-direction: column; gap: 14px;
}
.ud-breakdown-top5 {
  padding: 16px 20px;
}
.ud-breakdown-heading {
  font-size: 12px; font-weight: 600; color: var(--ud-muted);
  text-transform: uppercase; letter-spacing: .04em;
  margin-bottom: 12px;
}

.ud-budget-pool { display: flex; flex-direction: column; gap: 5px; }
.ud-budget-pool__hdr { display: flex; justify-content: space-between; align-items: center; font-size: 12px; }
.ud-budget-pool__label { color: var(--el-text-color-regular); font-weight: 500; }
.ud-budget-pool__ratio { font-size: 12px; }
.ud-budget-pool__amts  { display: flex; justify-content: space-between; font-size: 11px; color: var(--ud-faint); }
.ud-budget-progress :deep(.el-progress-bar__outer) { border-radius: 3px; }
.ud-budget-progress :deep(.el-progress-bar__inner) { border-radius: 3px; }

.ud-top5-list { display: flex; flex-direction: column; gap: 8px; }
.ud-top5-item { display: flex; align-items: center; gap: 8px; }
.ud-top5-name { flex: 1; font-size: 13px; color: var(--el-text-color-primary); min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ud-top5-bar  { width: 80px; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; flex-shrink: 0; }
.ud-top5-fill { height: 100%; border-radius: 3px; background: linear-gradient(90deg, #67c23a, #95d475); }
.ud-top5-cost { font-size: 12px; white-space: nowrap; min-width: 60px; text-align: right; }
.ud-top5-pct  { font-size: 11px; color: var(--ud-muted); min-width: 36px; text-align: right; }

/* ── 分页 ── */
.ud-pagination { display: flex; justify-content: flex-end; padding: 12px 16px; }

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

/* tooltip CSS 已移至文件末尾非 scoped <style> 块（因 el-tooltip 内容 teleport 到 body）*/

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
.ud-tl-measured   { display: block; font-size: 10px; color: var(--el-text-color-placeholder); font-style: italic; }
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
.ud-tt__row b.c-green  { color: #4ade80; }
.ud-tt__sep { border-top: 1px solid #374151; margin: 6px 0; }

/* ── Trace Drawer 移动端适配（非 scoped，因为 el-drawer teleport 到 body）── */
@media (max-width: 768px) {
  .ud-trace-drawer.el-drawer { width: 100% !important; }

  .ud-trace-drawer .ud-msg-content {
    font-size: 11px !important; padding: 8px !important;
    overflow-x: auto; -webkit-overflow-scrolling: touch;
  }
  .ud-trace-drawer .ud-json-view { padding: 10px; }
  .ud-trace-drawer .ud-json-view pre {
    font-size: 11px !important; overflow-x: auto; -webkit-overflow-scrolling: touch;
    white-space: pre;
  }
  .ud-trace-drawer .ud-error-msg { font-size: 11px; padding: 10px; }

  .ud-trace-drawer .ud-header-row { flex-direction: column; gap: 2px; padding: 6px 8px; }
  .ud-trace-drawer .ud-header-key { min-width: auto; font-size: 11px; font-weight: 700; }
  .ud-trace-drawer .ud-header-val { font-size: 11px; }

  .ud-trace-drawer .ud-meta-grid { grid-template-columns: 1fr !important; }

  .ud-trace-drawer .el-tabs__nav-wrap {
    overflow-x: auto; -webkit-overflow-scrolling: touch;
  }
  .ud-trace-drawer .el-tabs__nav {
    flex-wrap: nowrap; display: inline-flex;
  }
}
</style>
