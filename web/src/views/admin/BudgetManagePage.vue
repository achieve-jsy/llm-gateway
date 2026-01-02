<template>
  <div class="budget-overview">
    <header class="overview-header">
      <div>
        <h1>预算总览</h1>
        <p>查看预算风险与消耗分布；具体调额请进入组织架构中的工作室或部门预算页。</p>
      </div>
      <div class="header-actions">
        <el-button :loading="loading" @click="load">刷新</el-button>
        <el-button type="primary" @click="goOrg">进入组织治理</el-button>
      </div>
    </header>

    <section class="kpi-strip">
      <div class="kpi">
        <span class="kpi-label">总额度</span>
        <strong>{{ formatUSD(totalBudget, { compact: true }) }}</strong>
      </div>
      <div class="kpi">
        <span class="kpi-label">已用</span>
        <strong>{{ formatUSD(totalUsed, { compact: true }) }}</strong>
      </div>
      <div class="kpi">
        <span class="kpi-label">剩余</span>
        <strong>{{ formatUSD(Math.max(totalBudget - totalUsed, 0), { compact: true }) }}</strong>
      </div>
      <div class="kpi">
        <span class="kpi-label">风险对象</span>
        <strong :class="{ danger: riskItems.length > 0 }">{{ riskItems.length }}</strong>
      </div>
    </section>

    <section class="overview-grid">
      <div class="panel panel-main">
        <div class="panel-head">
          <div>
            <h2>{{ tableTitle }}</h2>
            <p>{{ tableDesc }}</p>
          </div>
          <el-segmented v-if="riskItems.length > 0" v-model="riskFilter" :options="riskOptions" size="small" />
        </div>

        <div v-if="!loading && riskItems.length === 0" class="healthy-state">
          <div class="healthy-icon">✓</div>
          <div>
            <strong>当前管理范围内暂无预算风险</strong>
            <span>下方展示消耗最高的预算对象，便于提前观察趋势。</span>
          </div>
        </div>

        <el-table
          v-loading="loading"
          :data="tableItems"
          stripe
          size="small"
          :empty-text="riskItems.length === 0 ? '暂无预算对象' : '暂无符合条件的风险预算'"
        >
          <el-table-column label="对象" min-width="210">
            <template #default="{ row }">
              <div class="scope-cell">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS[row.scope_type] }" />
                <div>
                  <el-tooltip :content="row.scope_id" placement="top" :disabled="!row.scope_id">
                    <div class="scope-name">{{ displayScopeName(row) }}</div>
                  </el-tooltip>
                  <div class="scope-id">{{ SCOPE_TYPE_LABELS[row.scope_type] }}</div>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="已用 / 额度" min-width="180">
            <template #default="{ row }">
              <div class="usage-cell">
                <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }} / {{ displayAmount(row.amount_usd) }}</span>
                <el-progress
                  :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)"
                  :stroke-width="6"
                  :color="progressColor(row.used_ratio || 0)"
                />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="周 / 日限制" width="150">
            <template #default="{ row }">
              <span class="limit-text">
                周 {{ displayAmount(row.weekly_amount_usd) }} · 日 {{ displayAmount(row.daily_amount_usd) }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100" align="center">
            <template #default="{ row }">
              <el-tag :type="statusType(row.used_ratio || 0)" size="small">{{ statusText(row.used_ratio || 0) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120" align="center">
            <template #default="{ row }">
              <el-button link type="primary" @click="goOrg(row)">定位处理</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <aside class="panel side-panel">
        <div v-if="contextItems.length" class="context-section">
          <div class="panel-head compact">
            <div>
              <h2>治理链路</h2>
              <p>当前账号关联的预算上下文。</p>
            </div>
          </div>
          <div class="context-chain">
            <div v-for="item in contextItems" :key="`${item.scope_type}:${item.scope_id}`" class="context-node">
              <div class="context-node-main">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS[item.scope_type] }" />
                <span>{{ item.label || SCOPE_TYPE_LABELS[item.scope_type] }}</span>
              </div>
              <div class="context-node-value">
                <strong>{{ formatUSD(item.used_usd || 0, { compact: true }) }}</strong>
                <span>/ {{ displayAmount(item.amount_usd) }}</span>
              </div>
            </div>
          </div>

          <div class="divider" />
        </div>

        <div class="panel-head compact">
          <div>
            <h2>范围分布</h2>
            <p>按预算对象类型汇总。</p>
          </div>
        </div>
        <div class="scope-breakdown">
          <div v-for="row in scopeBreakdown" :key="row.type" class="breakdown-row">
            <div class="breakdown-meta">
              <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS[row.type] }" />
              <span>{{ SCOPE_TYPE_LABELS[row.type] }}</span>
            </div>
            <div class="breakdown-value">
              <strong>{{ formatUSD(row.used, { compact: true }) }}</strong>
              <span>/ {{ formatUSD(row.amount, { compact: true }) }}</span>
            </div>
          </div>
        </div>

        <div class="divider" />

        <div class="quick-actions">
          <h3>推荐操作</h3>
          <button class="quick-action" @click="goOrg()">
            <strong>调整部门/工作室预算</strong>
            <span>进入组织架构，选中节点后在「预算」Tab 中配置。</span>
          </button>
          <button class="quick-action" @click="goExpansion">
            <strong>处理扩容申请</strong>
            <span>查看待审批和我发起的扩容流程。</span>
          </button>
        </div>
      </aside>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { getBudgetContext, getBudgetList } from '@/services/control-plane'
import { useAuthStore } from '@/stores/auth'
import type { BudgetContextItem, BudgetItem, BudgetScopeType, BudgetView } from '@/types/budget'
import { SCOPE_TYPE_COLORS, SCOPE_TYPE_LABELS } from '@/types/budget'
import { formatUSD } from '@/utils/format'

const router = useRouter()
const auth = useAuthStore()
const loading = ref(false)
const items = ref<BudgetItem[]>([])
const contextItems = ref<BudgetContextItem[]>([])
const riskFilter = ref('all')

const riskOptions = [
  { label: '全部风险', value: 'all' },
  { label: '已超额', value: 'danger' },
  { label: '预警', value: 'warning' },
]

const overviewView = computed<BudgetView>(() => {
  if (auth.effectiveRole === 'admin') return 'company'
  if (auth.effectiveRole === 'studio_admin') return 'studio'
  return 'department'
})

const kpiItems = computed(() => {
  const rows = items.value
  if (overviewView.value === 'company') {
    const studios = rows.filter(item => item.scope_type === 'studio')
    return studios.length ? studios : rows.filter(item => item.scope_type === 'company')
  }
  if (overviewView.value === 'studio') {
    return rows.filter(item => item.scope_type === 'dept_daily' || item.scope_type === 'dept_service')
  }
  return rows.filter(item => item.scope_type === 'dept_daily' || item.scope_type === 'dept_service')
})

const totalBudget = computed(() =>
  kpiItems.value.reduce((sum, item) => sum + Math.max(item.amount_usd || 0, 0), 0)
)
const totalUsed = computed(() =>
  kpiItems.value.reduce((sum, item) => sum + (item.used_usd || 0), 0)
)
const riskItems = computed(() =>
  items.value
    .filter(item => (item.amount_usd || 0) > 0 && (item.used_ratio || 0) >= 0.8)
    .sort((a, b) => (b.used_ratio || 0) - (a.used_ratio || 0))
)
const filteredRiskItems = computed(() => {
  if (riskFilter.value === 'danger') return riskItems.value.filter(i => (i.used_ratio || 0) >= 1)
  if (riskFilter.value === 'warning') return riskItems.value.filter(i => (i.used_ratio || 0) >= 0.8 && (i.used_ratio || 0) < 1)
  return riskItems.value
})
const topUsageItems = computed(() =>
  kpiItems.value
    .slice()
    .sort((a, b) => {
      const ratioDiff = (b.used_ratio || 0) - (a.used_ratio || 0)
      return ratioDiff !== 0 ? ratioDiff : (b.used_usd || 0) - (a.used_usd || 0)
    })
    .slice(0, 8)
)
const tableItems = computed(() => riskItems.value.length > 0 ? filteredRiskItems.value : topUsageItems.value)
const tableTitle = computed(() => riskItems.value.length > 0 ? '预算风险' : '预算健康')
const tableDesc = computed(() => riskItems.value.length > 0
  ? '优先处理已超额和预计耗尽的对象。'
  : '没有超过 80% 的预算对象，展示当前消耗最高的对象。'
)

const scopeBreakdown = computed(() => {
  const map = new Map<BudgetScopeType, { type: BudgetScopeType; amount: number; used: number }>()
  for (const item of items.value) {
    const row = map.get(item.scope_type) || { type: item.scope_type, amount: 0, used: 0 }
    row.amount += Math.max(item.amount_usd || 0, 0)
    row.used += item.used_usd || 0
    map.set(item.scope_type, row)
  }
  return Array.from(map.values()).sort((a, b) => b.used - a.used)
})

async function load() {
  loading.value = true
  try {
    const [budgetItems, contexts] = await Promise.all([
      getBudgetList(undefined, undefined, overviewView.value),
      getBudgetContext().catch(() => []),
    ])
    items.value = budgetItems
    contextItems.value = contexts
  } finally {
    loading.value = false
  }
}

function displayAmount(value: number) {
  if (value == null || value < 0) return '不限'
  return formatUSD(value, { compact: true })
}

function displayScopeName(row: BudgetItem) {
  if (row.label) return row.label
  const fallback: Record<BudgetScopeType, string> = {
    company: '全公司预算',
    studio: '未命名工作室',
    dept_daily: '未命名部门日常预算',
    dept_service: '未命名部门服务预算',
    member: '未命名成员配额',
  }
  return fallback[row.scope_type] || SCOPE_TYPE_LABELS[row.scope_type]
}

function progressColor(ratio: number) {
  if (ratio >= 1) return '#f56c6c'
  if (ratio >= 0.8) return '#e6a23c'
  return '#2BA8A2'
}

function statusType(ratio: number) {
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return 'success'
}

function statusText(ratio: number) {
  if (ratio >= 1) return '已超额'
  if (ratio >= 0.8) return '预警'
  return '正常'
}

function goOrg(row?: BudgetItem) {
  if (!row) {
    router.push('/admin/org')
    return
  }
  router.push({
    path: '/admin/org',
    query: {
      scope_type: row.scope_type,
      scope_id: row.scope_id,
      tab: 'budget',
    },
  })
}

function goExpansion() {
  router.push('/admin/expansion-requests')
}

onMounted(load)
watch(overviewView, load)
</script>

<style scoped>
.budget-overview {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 28px 32px;
}
.overview-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
}
.overview-header h1 {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.overview-header p {
  margin: 6px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.header-actions { display: flex; gap: 10px; flex-shrink: 0; }
.kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.kpi {
  padding: 16px 18px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
}
.kpi-label {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-bottom: 8px;
}
.kpi strong {
  font-size: 24px;
  color: var(--el-text-color-primary);
}
.kpi strong.danger { color: var(--el-color-danger); }
.overview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: 16px;
  align-items: start;
}
.panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  background: var(--el-bg-color);
  overflow: hidden;
}
.panel-main { padding: 0; }
.side-panel { padding: 18px; }
.panel-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  padding: 18px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.panel-head.compact {
  padding: 0 0 14px;
  border-bottom: none;
}
.panel-head h2 {
  margin: 0;
  font-size: 16px;
  color: var(--el-text-color-primary);
}
.panel-head p {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.healthy-state {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 16px 20px 0;
  padding: 14px 16px;
  border: 1px solid var(--el-color-success-light-7);
  border-radius: 12px;
  background: var(--el-color-success-light-9);
}
.healthy-icon {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: #fff;
  background: var(--el-color-success);
  font-weight: 700;
}
.healthy-state strong {
  display: block;
  color: var(--el-text-color-primary);
  font-size: 14px;
}
.healthy-state span {
  display: block;
  margin-top: 2px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.scope-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}
.scope-dot {
  width: 8px;
  height: 8px;
  border-radius: 99px;
  flex-shrink: 0;
}
.scope-name { font-weight: 600; color: var(--el-text-color-primary); }
.scope-id { margin-top: 2px; font-size: 11px; color: var(--el-text-color-secondary); }
.usage-cell { display: grid; grid-template-columns: 90px 1fr; align-items: center; gap: 10px; }
.limit-text { font-size: 12px; color: var(--el-text-color-secondary); }
.context-section {
  margin-bottom: 2px;
}
.context-chain {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.context-node {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px dashed var(--el-border-color-lighter);
}
.context-node:last-child {
  border-bottom: none;
}
.context-node-main {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
}
.context-node-main span:last-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.context-node-value {
  display: flex;
  align-items: baseline;
  gap: 4px;
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}
.context-node-value strong {
  color: var(--el-text-color-primary);
  font-size: 13px;
}
.context-node-value span {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.scope-breakdown { display: flex; flex-direction: column; gap: 12px; }
.breakdown-row { display: flex; justify-content: space-between; gap: 12px; align-items: center; }
.breakdown-meta { display: flex; align-items: center; gap: 8px; color: var(--el-text-color-regular); font-size: 13px; }
.breakdown-value { display: flex; align-items: baseline; gap: 4px; font-variant-numeric: tabular-nums; }
.breakdown-value span { color: var(--el-text-color-secondary); font-size: 12px; }
.divider { height: 1px; background: var(--el-border-color-lighter); margin: 18px 0; }
.quick-actions h3 { margin: 0 0 10px; font-size: 14px; color: var(--el-text-color-primary); }
.quick-action {
  width: 100%;
  text-align: left;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-blank);
  border-radius: 10px;
  padding: 12px;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 10px;
}
.quick-action:hover {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}
.quick-action strong { color: var(--el-text-color-primary); }
.quick-action span { color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.5; }
@media (max-width: 1080px) {
  .overview-grid { grid-template-columns: 1fr; }
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 720px) {
  .budget-overview { padding: 18px; }
  .overview-header { flex-direction: column; }
  .kpi-strip { grid-template-columns: 1fr; }
}
</style>
