<template>
  <div class="mtb my-tier-panel" v-loading="loading">
    <div class="mtb-page-head">
      <div>
        <h3>模型等级额度</h3>
        <p>查看当前真正生效的等级子预算；总预算与等级额度会同时检查。</p>
      </div>
      <el-button
        type="primary"
        data-test="tier-create-expansion"
        @click="openExpansion(temporaryTier?.model_tier_id || tierBudget?.tier_limits[0]?.model_tier_id)"
      >
        申请调整额度
      </el-button>
    </div>

    <DataFreshnessNotice
      v-if="tierBudget"
      :stale="tierBudget.stale"
      :updated-at="tierBudget.data_updated_at"
    />

    <el-result v-if="error" icon="error" title="等级额度加载失败" :sub-title="error">
      <template #extra><el-button type="primary" @click="loadTierBudget">重试</el-button></template>
    </el-result>

    <template v-else-if="tierBudget">
      <section class="tier-effective-summary" aria-label="等级预算摘要">
        <div v-for="item in sourceSummaryItems" :key="item.label" data-test="tier-source-summary-item">
          <span>{{ item.label }}</span>
          <strong :class="item.tone ? `is-${item.tone}` : ''">{{ item.value }}</strong>
        </div>
      </section>

      <section v-if="temporaryPeriod && temporaryTier" class="temporary-banner">
        <span class="temporary-banner__icon">◷</span>
        <div>
          <strong>{{ temporaryTier.model_tier_name }}额度包含临时扩容</strong>
          <small>
            长期基础额度 {{ formatBudgetUSD(temporaryPeriod.base_limit_usd) }}
            + 临时增量 {{ formatBudgetUSD(temporaryIncrement) }}；本周期已用金额不会在到期时清零。
          </small>
        </div>
        <div class="temporary-values">
          <b>当前有效 {{ formatBudgetUSD(temporaryPeriod.effective_limit_usd) }}</b>
          <small>{{ formatExpiry(temporaryPeriod.reset_at) }} 到期</small>
        </div>
        <el-button size="small" @click="openExpansion(temporaryTier.model_tier_id)">查看申请</el-button>
      </section>

      <div class="my-tier-grid">
        <section class="mtb-panel">
          <div class="mtb-panel-head">
            <div>
              <h3>各等级有效额度</h3>
              <p>额度来源可能因等级和周期不同，优先展示当前实际限制来源。</p>
            </div>
            <div class="mtb-panel-actions">
              <PeriodSegmented v-model="selectedPeriod" />
              <el-tag size="small">按等级显示生效来源</el-tag>
            </div>
          </div>

          <div v-if="tierBudget.tier_limits.length" class="mtb-list" data-test="tier-budget-list">
            <article
              v-for="tier in tierBudget.tier_limits"
              :key="tier.model_tier_id"
              class="mtb-card"
            >
              <div class="mtb-card-head">
                <div class="mtb-card-title">
                  <TierColorDot :color="tierColor(tier.model_tier_id)" />
                  <strong>{{ tier.model_tier_name }}</strong>
                </div>
                <BudgetStatusTag :status="tierStatus(tier)" />
              </div>

              <span class="source-mini" data-test="tier-source-pill">{{ sourceLabel(tier) }}</span>

              <template v-if="periodInfo(tier)">
                <el-progress
                  class="mtb-progress-section"
                  :percentage="usagePercent(periodInfo(tier)!)"
                  :stroke-width="8"
                  :color="progressColor(periodInfo(tier)!)"
                  :show-text="false"
                />
                <div class="mtb-card-stats">
                  <div class="mtb-stat">
                    <span class="mtb-stat-label">可用</span>
                    <strong class="mtb-stat-value">{{ formatBudgetUSD(periodInfo(tier)!.remaining_usd) }}</strong>
                  </div>
                  <div class="mtb-stat">
                    <span class="mtb-stat-label">已用</span>
                    <span>{{ formatBudgetUSD(periodInfo(tier)!.used_usd) }}</span>
                  </div>
                  <div class="mtb-stat">
                    <span class="mtb-stat-label">总额</span>
                    <span>{{ periodInfo(tier)!.configured ? formatBudgetUSD(periodInfo(tier)!.effective_limit_usd) : '未配置' }}</span>
                  </div>
                </div>
              </template>
              <div v-else class="mtb-no-period">该等级在当前周期无额度配置</div>
            </article>
          </div>
          <el-empty v-else description="暂无模型等级额度配置" />
        </section>

        <aside class="mtb-panel tier-help">
          <h4>额度如何生效</h4>
          <div v-for="item in helpItems" :key="item.title" class="tier-help-item" data-test="tier-help-item">
            <strong>{{ item.title }}</strong>
            <span>{{ item.detail }}</span>
          </div>
        </aside>
      </div>
    </template>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { getMyBudgetTiers, listModelTiers } from '@/services/control-plane'
import {
  formatBudgetUSD,
  getBudgetSourceLabel,
  getBudgetStatus,
  toUsagePercent,
  type BudgetDisplayStatus,
} from '@/composables/useBudgetDisplay'
import type { MyBudgetTiersResponse, MyBudgetTierLimit, TierPeriodUsage } from '@/types/model-tier'
import type { ExpansionPeriod } from '@/types/expansion'
import PeriodSegmented from './PeriodSegmented.vue'
import BudgetStatusTag from './BudgetStatusTag.vue'
import TierColorDot from './TierColorDot.vue'
import DataFreshnessNotice from '@/components/common/DataFreshnessNotice.vue'

const emit = defineEmits<{
  requestExpansion: [payload: {
    dimension: 'tier'
    tierId: string
    period: ExpansionPeriod
    targetAmount: string
  }]
}>()
const loading = ref(false)
const error = ref('')
const tierBudget = ref<MyBudgetTiersResponse | null>(null)
const selectedPeriod = ref<ExpansionPeriod>('monthly')
const tierColors = ref(new Map<string, string>())

function periodInfo(tier: MyBudgetTierLimit): TierPeriodUsage | undefined {
  return tier.periods.find(p => p.period_type === selectedPeriod.value)
}

function usagePercent(period: TierPeriodUsage): number {
  return Math.round(toUsagePercent(period.used_usd, period.effective_limit_usd))
}

function tierStatus(tier: MyBudgetTierLimit): BudgetDisplayStatus {
  const period = periodInfo(tier)
  return getBudgetStatus({
    configured: Boolean(period?.configured),
    enabled: tier.enabled
      && period?.status !== 'tier_disabled'
      && period?.status !== 'limit_disabled',
    limit: period?.effective_limit_usd,
    usageRatio: period ? usagePercent(period) / 100 : 0,
    stale: tierBudget.value?.stale,
  })
}

function progressColor(period: TierPeriodUsage): string {
  const pct = usagePercent(period)
  if (pct >= 80) return 'var(--el-color-danger)'
  if (pct >= 60) return 'var(--el-color-warning)'
  return 'var(--el-color-success)'
}

function sourceLabel(tier: MyBudgetTierLimit): string {
  const period = periodInfo(tier)
  if (!period?.configured) return '未配置'
  return getBudgetSourceLabel(period.limiting_source?.scope_type, Boolean(period.temporary_target_usd))
}

function tierColor(tierId: string): string | undefined {
  return tierColors.value.get(tierId)
}

const currentSourceLabel = computed(() => ({
  personal: '个人独立预算',
  shared: '部门共享预算',
  mixed: '混合预算来源',
}[tierBudget.value?.source_summary.mode ?? 'shared']))

const configuredTierCount = computed(() =>
  tierBudget.value?.tier_limits.filter(tier => tier.periods.some(period => period.configured)).length ?? 0,
)

const currentRisk = computed(() => {
  for (const tier of tierBudget.value?.tier_limits ?? []) {
    const status = tierStatus(tier)
    if (status.tone === 'danger' || status.tone === 'warning') {
      return { label: `${tier.model_tier_name}${status.label}`, tone: status.tone }
    }
  }
  return { label: '暂无风险', tone: 'success' as const }
})

const sourceSummaryItems = computed(() => [
  { label: '当前预算来源', value: currentSourceLabel.value },
  {
    label: '已配置等级',
    value: `${configuredTierCount.value} / ${tierBudget.value?.tier_limits.length ?? 0}`,
  },
  { label: '当前风险', value: currentRisk.value.label, tone: currentRisk.value.tone },
])

const temporaryTier = computed(() =>
  tierBudget.value?.tier_limits.find(tier =>
    tier.periods.some(period => Boolean(period.temporary_target_usd)),
  ),
)

const temporaryPeriod = computed(() =>
  temporaryTier.value?.periods.find(period => Boolean(period.temporary_target_usd)),
)

const temporaryIncrement = computed(() =>
  Math.max(
    0,
    Number(temporaryPeriod.value?.effective_limit_usd ?? 0)
      - Number(temporaryPeriod.value?.base_limit_usd ?? 0),
  ),
)

const helpItems = [
  {
    title: '同时检查两类额度',
    detail: '调用模型时会同时检查总预算和对应等级子预算，任一用尽都会阻止调用。',
  },
  {
    title: '共享子预算',
    detail: '部门成员共同消费，剩余额度会随其他成员使用实时变化。',
  },
  {
    title: '未配置',
    detail: '该等级不增加额外限制，仍然消耗并受总预算约束。',
  },
  {
    title: '额度为 $0',
    detail: '表示当前等级不可使用，但不影响其他等级。',
  },
]

function formatExpiry(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value || '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function openExpansion(tierId = '') {
  const tier = tierBudget.value?.tier_limits.find(item => item.model_tier_id === tierId)
    ?? tierBudget.value?.tier_limits[0]
  emit('requestExpansion', {
    dimension: 'tier',
    tierId: tier?.model_tier_id ?? '',
    period: selectedPeriod.value,
    targetAmount: tier ? periodInfo(tier)?.effective_limit_usd ?? '' : '',
  })
}

async function loadTierBudget() {
  loading.value = true
  error.value = ''
  try {
    const [budget, tiers] = await Promise.all([getMyBudgetTiers(), listModelTiers(false)])
    tierBudget.value = budget
    tierColors.value = new Map(tiers.map(tier => [tier.id, tier.color]))
  } catch (loadError) {
    error.value = loadError instanceof Error ? loadError.message : '请稍后重试'
  } finally {
    loading.value = false
  }
}

defineExpose({ loadTierBudget })
onMounted(loadTierBudget)
</script>

<style scoped>
.mtb {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mtb-page-head,
.mtb-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.mtb-page-head h3,
.mtb-panel-head h3,
.tier-help h4 { margin: 0; color: var(--el-text-color-primary); }
.mtb-page-head p,
.mtb-panel-head p {
  margin: 4px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.tier-effective-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  border: 1px solid var(--el-border-color-light);
  border-radius: var(--budget-panel-radius);
  background: var(--el-bg-color);
}
.tier-effective-summary > div {
  display: grid;
  gap: 5px;
  padding: 14px 18px;
  border-right: 1px solid var(--el-border-color-lighter);
}
.tier-effective-summary > div:last-child { border-right: 0; }
.tier-effective-summary span { font-size: 12px; color: var(--el-text-color-secondary); }
.tier-effective-summary strong { font-size: 17px; }
.tier-effective-summary .is-warning { color: var(--el-color-warning); }
.tier-effective-summary .is-danger { color: var(--el-color-danger); }
.tier-effective-summary .is-success { color: var(--el-color-success); }
.temporary-banner {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 13px 16px;
  border: 1px solid var(--el-color-warning-light-5);
  border-radius: var(--budget-panel-radius);
  background: var(--el-color-warning-light-9);
}
.temporary-banner__icon { font-size: 20px; color: var(--el-color-warning); }
.temporary-banner > div:not(.temporary-values) {
  display: grid;
  flex: 1;
  gap: 3px;
}
.temporary-banner small { color: var(--el-text-color-secondary); }
.temporary-values {
  display: grid;
  gap: 3px;
  text-align: right;
}
.my-tier-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 16px;
  align-items: start;
}
.mtb-panel {
  overflow: hidden;
  border: 1px solid var(--el-border-color-light);
  border-radius: var(--budget-panel-radius);
  background: var(--el-bg-color);
}
.mtb-panel-head { padding: 14px 18px; border-bottom: 1px solid var(--el-border-color-lighter); }
.mtb-panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mtb-list {
  display: flex;
  flex-direction: column;
}

.mtb-card {
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
  padding: 16px 18px;
}
.mtb-card:last-child { border-bottom: 0; }

.mtb-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.mtb-card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
}

.source-mini {
  display: inline-flex;
  width: fit-content;
  margin-bottom: 12px;
  padding: 2px 7px;
  border-radius: var(--radius-full);
  background: var(--el-color-info-light-9);
  color: var(--el-color-info-dark-2);
  font-size: 12px;
}

.mtb-progress-section {
  margin-bottom: 12px;
}

.mtb-card-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}

.mtb-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.mtb-stat-label {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}
.mtb-stat-value {
  font-size: 18px;
  font-weight: 800;
  color: var(--el-color-primary);
}
.mtb-stat span {
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.mtb-no-period {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  padding: 8px 0;
}

.tier-help { padding: 18px; }
.tier-help-item {
  display: grid;
  gap: 4px;
  padding: 14px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.tier-help-item:last-child { border-bottom: 0; padding-bottom: 0; }
.tier-help-item strong { font-size: 13px; color: var(--el-text-color-primary); }
.tier-help-item span { font-size: 12px; line-height: 1.65; color: var(--el-text-color-secondary); }

@media (max-width: 1180px) {
  .my-tier-grid { grid-template-columns: 1fr; }
}
@media (max-width: 760px) {
  .mtb-page-head,
  .mtb-panel-head,
  .temporary-banner { align-items: stretch; flex-direction: column; }
  .mtb-page-head .el-button { width: 100%; }
  .tier-effective-summary { grid-template-columns: 1fr; }
  .tier-effective-summary > div { border-right: 0; border-bottom: 1px solid var(--el-border-color-lighter); }
  .tier-effective-summary > div:last-child { border-bottom: 0; }
  .temporary-values { text-align: left; }
  .mtb-panel-actions { justify-content: space-between; flex-wrap: wrap; }
  .mtb-card-stats { grid-template-columns: 1fr 1fr; }
}
</style>