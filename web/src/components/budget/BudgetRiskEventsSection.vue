<template>
  <section aria-labelledby="risk-heading">
    <header class="section-header">
      <div><h2 id="risk-heading">实时风险事件</h2><p>事件由实时用量和实际阻断投影，风险消失后会自动移除。</p></div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </header>
    <BudgetKpiStrip :items="kpiItems" />
    <div class="filters budget-filter-bar" aria-label="风险筛选">
      <el-select v-model="filter.period_type" placeholder="全部周期" clearable><el-option label="日" value="daily" /><el-option label="周" value="weekly" /><el-option label="月" value="monthly" /></el-select>
      <el-select v-model="filter.dimension_type" placeholder="全部维度" clearable><el-option label="总预算" value="total" /><el-option label="模型等级" value="tier" /></el-select>
      <el-select v-model="filter.risk_type" placeholder="全部类型" clearable><el-option label="达到阈值" value="threshold_reached" /><el-option label="预算耗尽" value="budget_exhausted" /><el-option label="请求被阻断" value="request_blocked" /></el-select>
      <el-select v-model="filter.severity" placeholder="全部严重度" clearable><el-option label="警告" value="warning" /><el-option label="严重" value="critical" /></el-select>
      <el-button type="primary" plain @click="load">筛选</el-button>
    </div>
    <DataFreshnessNotice :stale="page.stale || summary.stale" :updated-at="page.data_as_of || summary.data_as_of" />
    <el-table v-loading="loading" :data="page.items" empty-text="当前没有预算风险" row-class-name="risk-row" @row-click="open">
      <el-table-column label="严重度" width="90"><template #default="{ row }"><el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'">{{ row.severity === 'critical' ? '严重' : '警告' }}</el-tag></template></el-table-column>
      <el-table-column prop="scope_name" label="影响对象" min-width="150" />
      <el-table-column label="维度" min-width="130"><template #default="{ row }">{{ row.dimension_type === 'tier' ? (row.model_tier_name || '模型等级') : '总预算' }} / {{ periodLabel(row.period_type) }}</template></el-table-column>
      <el-table-column label="额度 / 已用" min-width="180"><template #default="{ row }">{{ formatBudgetUSD(row.effective_limit_usd) }} / {{ formatBudgetUSD(row.used_usd) }}</template></el-table-column>
      <el-table-column prop="usage_ratio" label="使用率" width="100" />
      <el-table-column prop="limit_source" label="限制来源" min-width="140" />
      <el-table-column prop="blocked_requests" label="阻断次数" width="100" />
      <el-table-column prop="detected_at" label="发现时间" min-width="170"><template #default="{ row }">{{ formatTime(row.detected_at) }}</template></el-table-column>
    </el-table>

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
              @click="adjustBudget"
            >
              <strong>调整额度</strong>
              <span>打开当前对象的预算配置</span>
            </button>
            <button type="button" class="disposition-action" :disabled="notifying" @click="notifyOwner">
              <strong>通知负责人</strong>
              <span>发送风险与预算维度提醒</span>
            </button>
            <button type="button" class="disposition-action" @click="viewUsage">
              <strong>查看用量</strong>
              <span>跳转到该对象的用量明细</span>
            </button>
          </div>
        </section>

        <div class="drawer-footer">
          <el-button @click="drawer = false">关闭</el-button>
          <el-button type="primary" data-test="risk-adjust-footer" :disabled="selected.stale" @click="adjustBudget">
            调整额度
          </el-button>
        </div>
      </template>
    </el-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import BudgetKpiStrip from '@/components/budget/BudgetKpiStrip.vue'
import DataFreshnessNotice from '@/components/common/DataFreshnessNotice.vue'
import { formatBudgetUSD } from '@/composables/useBudgetDisplay'
import { getBudgetRiskSummary, listBudgetRisks, notifyBudgetOwner } from '@/services/control-plane'
import type { BudgetRiskEvent, BudgetRiskFilter, BudgetRiskPage, BudgetRiskSummary } from '@/types/budget-risk'

const emit = defineEmits<{ viewBudget: [event: BudgetRiskEvent] }>()
const router = useRouter()
const loading = ref(false)
const notifying = ref(false)
const drawer = ref(false)
const selected = ref<BudgetRiskEvent | null>(null)
const filter = reactive<BudgetRiskFilter>({})
const page = reactive<BudgetRiskPage>({ items: [], total: 0, data_as_of: '', stale: false })
const summary = reactive<BudgetRiskSummary>({ total: 0, warning: 0, critical: 0, data_as_of: '', stale: false })

const blockedRequests = computed(() => page.items.reduce((total, item) => total + item.blocked_requests, 0))
const kpiItems = computed(() => [
  { key: 'total', label: '当前风险', value: summary.total, tone: 'neutral' as const },
  { key: 'critical', label: '严重风险', value: summary.critical, tone: 'danger' as const },
  { key: 'warning', label: '即将耗尽', value: summary.warning, tone: 'warning' as const },
  { key: 'blocked-events', label: '实际阻断', value: summary.by_type?.request_blocked ?? 0, tone: 'danger' as const },
  { key: 'blocked-requests', label: '阻断请求', value: blockedRequests.value, tone: 'danger' as const, note: '当前页累计' },
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
      detail: selected.value.limit_source || '—',
      limiting: true,
    },
  ]
})

const suggestedLimit = computed(() =>
  Math.ceil(Number(selected.value?.used_usd ?? 0) * 1.15 * 100) / 100,
)

async function load() {
  loading.value = true
  try {
    const [nextPage, nextSummary] = await Promise.all([
      listBudgetRisks({ page: 1, page_size: 50, ...filter }),
      getBudgetRiskSummary({ ...filter }),
    ])
    Object.assign(page, nextPage)
    Object.assign(summary, nextSummary)
  } finally {
    loading.value = false
  }
}
function open(event: BudgetRiskEvent) { selected.value = event; drawer.value = true }
function adjustBudget() {
  if (!selected.value || selected.value.stale) return
  emit('viewBudget', selected.value)
  drawer.value = false
}
function viewUsage() {
  if (!selected.value) return
  router.push({
    path: '/admin/usage/dashboard',
    query: {
      scope_type: selected.value.scope_type,
      scope_id: selected.value.scope_id,
      model_tier_id: selected.value.model_tier_id || undefined,
    },
  })
}
async function notifyOwner() {
  if (!selected.value) return
  notifying.value = true
  try {
    await notifyBudgetOwner({
      scope_type: selected.value.scope_type, scope_id: selected.value.scope_id,
      period_type: selected.value.period_type, dimension_type: selected.value.dimension_type,
      model_tier_id: selected.value.model_tier_id,
    })
    ElMessage.success('已通知预算负责人')
  } finally {
    notifying.value = false
  }
}
const periodLabel = (value: string) => ({ daily: '日', weekly: '周', monthly: '月' }[value] || value)
const dimensionLabel = (event: BudgetRiskEvent) => event.dimension_type === 'tier'
  ? event.model_tier_name || '模型等级'
  : '总预算'
const riskStatusLabel = (event: BudgetRiskEvent) => {
  if (event.risk_type === 'request_blocked') return '已阻断'
  if (event.risk_type === 'budget_exhausted') return '预算耗尽'
  return '即将耗尽'
}
const formatTime = (value: string) => new Date(value).toLocaleString()
onMounted(load)
</script>

<style scoped>
section { display: grid; gap: 16px; }
.section-header { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
h2 { margin: 0 0 6px; font-size: 18px; }
p { margin: 0; color: var(--el-text-color-secondary); }
.filters { display: flex; flex-wrap: wrap; gap: 10px; }
.filters .el-select { width: 150px; }
.risk-row { cursor: pointer; }
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
  .filters .el-select { width: calc(50% - 5px); }
  .disposition-actions { grid-template-columns: 1fr; }
}
</style>
