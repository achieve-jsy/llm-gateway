<template>
  <div class="mtb my-tier-panel" v-loading="loading">
    <div class="mtb-page-head">
      <div>
        <h3>模型等级额度</h3>
        <p>查看不同模型等级当前是否可用，以及本周期剩余额度。</p>
      </div>
      <el-button
        type="primary"
        data-test="tier-create-expansion"
        @click="openExpansion(temporaryTier?.model_tier_id || tierBudget?.tier_limits[0]?.model_tier_id)"
      >
        申请提升等级额度
      </el-button>
    </div>

    <el-result v-if="error" icon="error" title="等级额度加载失败" :sub-title="error">
      <template #extra><el-button type="primary" @click="loadTierBudget">重试</el-button></template>
    </el-result>

    <template v-else-if="tierBudget">
      <section class="tier-toolbar" aria-label="等级预算摘要">
        <div class="tier-summary-pills">
          <span class="tier-pill is-success" data-test="tier-summary-available">可用 {{ availableTierCount }}</span>
          <span class="tier-pill" :class="restrictedTierCount > 0 ? 'is-warning' : 'is-neutral'" data-test="tier-summary-restricted">受限 {{ restrictedTierCount }}</span>
          <span class="tier-pill is-neutral">当前周期 {{ periodLabel }}</span>
          <span class="tier-pill is-neutral">{{ currentSourceLabel }}</span>
        </div>
        <PeriodSegmented v-model="selectedPeriod" />
      </section>

      <section v-if="temporaryPeriod && temporaryTier" class="temporary-inline">
        <span class="temporary-inline__icon">◷</span>
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

      <section class="tier-table mtb-panel" data-test="tier-budget-list">
        <div class="tier-table-head" aria-hidden="true">
          <span>模型等级</span>
          <span>本期额度</span>
          <span>关联模型组</span>
          <span>操作</span>
        </div>
        <div v-if="tierBudget.tier_limits.length" class="tier-table-body">
          <article
            v-for="tier in tierBudget.tier_limits"
            :key="tier.model_tier_id"
            class="tier-row"
            :class="{ 'is-disabled': !canApply(tier) }"
          >
            <div class="tier-row__identity">
              <div class="tier-row__title">
                  <TierColorDot :color="tier.model_tier_color" />
                  <strong>{{ tier.model_tier_name }}</strong>
                <BudgetStatusTag :status="tierStatus(tier)" />
              </div>
              <span class="source-mini" data-test="tier-source-pill">{{ sourceLabel(tier) }}</span>
            </div>

            <div class="tier-row__amount">
              <template v-if="periodInfo(tier)">
                <strong>{{ remainingDisplay(periodInfo(tier)!) }}</strong>
                <span>{{ usageSummary(periodInfo(tier)!) }}</span>
                <small v-if="periodHint(periodInfo(tier)!)">{{ periodHint(periodInfo(tier)!) }}</small>
              </template>
              <span v-else class="tier-row__muted">当前周期未配置</span>
            </div>

            <div class="tier-row__groups" data-test="tier-groups">
              <template v-if="tierGroups(tier).length">
                <strong>{{ tierGroups(tier).length }} 个模型组</strong>
                <span>{{ groupSummary(tier) }}</span>
                <button class="tier-groups__link" type="button" @click="openGroupsDialog(tier)">查看全部</button>
              </template>
              <span v-else class="tier-groups__empty">暂无可见模型组</span>
            </div>

            <div class="tier-row__action">
              <el-button
                size="small"
                type="primary"
                plain
                :disabled="!canApply(tier)"
                @click="openExpansion(tier.model_tier_id)"
              >
                {{ canApply(tier) ? '申请提升' : '不可申请' }}
              </el-button>
            </div>
          </article>
        </div>
        <el-empty v-else description="暂无模型等级额度配置" />
      </section>

      <el-dialog v-model="groupsDialogVisible" :title="`${groupsDialogTierName}关联模型组`" width="min(460px, 92vw)" class="tier-groups-dialog">
        <div class="groups-dialog-list">
          <div v-for="group in groupsDialogItems" :key="group.id" class="groups-dialog-item">
            <strong>{{ displayGroupName(group) }}</strong>
            <span v-if="group.description">{{ group.description }}</span>
          </div>
        </div>
      </el-dialog>
    </template>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { getAvailableGroups, getMyBudgetTiers } from '@/services/control-plane'
import {
  formatBudgetUSD,
  getBudgetSourceLabel,
  getBudgetStatus,
  toUsagePercent,
  type BudgetDisplayStatus,
} from '@/composables/useBudgetDisplay'
import type { MyBudgetTiersResponse, MyBudgetTierLimit, TierPeriodUsage } from '@/types/model-tier'
import type { ExpansionPeriod } from '@/types/expansion'
import type { AvailableGroup } from '@/types/api-key'
import PeriodSegmented from './PeriodSegmented.vue'
import BudgetStatusTag from './BudgetStatusTag.vue'
import TierColorDot from './TierColorDot.vue'

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
const availableGroups = ref<AvailableGroup[]>([])
const groupsDialogVisible = ref(false)
const groupsDialogTierName = ref('')
const groupsDialogItems = ref<AvailableGroup[]>([])

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
  })
}

function sourceLabel(tier: MyBudgetTierLimit): string {
  const period = periodInfo(tier)
  if (!period?.configured) return '未配置'
  const source = getBudgetSourceLabel(period.limiting_source?.scope_type, Boolean(period.temporary_target_usd))
  if (source === '个人预算') return '个人额度'
  if (source === '部门共享子预算') return '部门共享额度'
  if (source === '个人临时额度') return '个人临时额度'
  return '额度来源已自动匹配'
}

function amountVisible(period: TierPeriodUsage): boolean {
  const scope = period.limiting_source?.scope_type
  return !period.configured || !scope || scope === 'member' || scope === 'dept_daily'
}

function remainingDisplay(period: TierPeriodUsage): string {
  if (!period.configured) return '未配置'
  if (!amountVisible(period)) return '额度不可见'
  return formatBudgetUSD(period.remaining_usd)
}

function usedDisplay(period: TierPeriodUsage): string {
  if (!period.configured) return '—'
  if (!amountVisible(period)) return '额度不可见'
  return formatBudgetUSD(period.used_usd)
}

function totalDisplay(period: TierPeriodUsage): string {
  if (!period.configured) return '未配置'
  if (!amountVisible(period)) return '额度不可见'
  return formatBudgetUSD(period.effective_limit_usd)
}

function periodHint(period: TierPeriodUsage): string {
  if (!period.configured) return '该等级未设置单独额度，仍会消耗总预算。'
  if (!amountVisible(period)) return '该等级受共享预算约束，具体上级额度不在此展示。'
  if (Number(period.effective_limit_usd) === 0) return '该等级当前不可使用。'
  return ''
}

function tierGroups(tier: MyBudgetTierLimit): AvailableGroup[] {
  return availableGroups.value.filter(group => group.budget_tier_id === tier.model_tier_id)
}

function groupSummary(tier: MyBudgetTierLimit): string {
  const groups = tierGroups(tier)
  const names = groups.slice(0, 2).map(displayGroupName).join('、')
  return groups.length > 2 ? `${names} 等` : names
}

function displayGroupName(group: AvailableGroup): string {
  return group.name
    .replace(/\s*(?:→|->|=>|替代).*$/u, '')
    .trim() || group.name
}

function openGroupsDialog(tier: MyBudgetTierLimit) {
  groupsDialogTierName.value = tier.model_tier_name
  groupsDialogItems.value = tierGroups(tier)
  groupsDialogVisible.value = true
}

function canApply(tier: MyBudgetTierLimit): boolean {
  const period = periodInfo(tier)
  const status = tierStatus(tier)
  return Boolean(tier.enabled && period && period.status !== 'tier_disabled' && !status.blocked)
}

function usageSummary(period: TierPeriodUsage): string {
  if (!period.configured) return '仍受总预算约束'
  if (!amountVisible(period)) return '具体额度不在此展示'
  return `已用 ${formatBudgetUSD(period.used_usd)} / 总额 ${formatBudgetUSD(period.effective_limit_usd)}`
}

const currentSourceLabel = computed(() => ({
  personal: '个人额度',
  shared: '部门共享额度',
  mixed: '个人与共享额度',
}[tierBudget.value?.source_summary.mode ?? 'shared']))

const availableTierCount = computed(() =>
  tierBudget.value?.tier_limits.filter(tier => canApply(tier)).length ?? 0,
)
const restrictedTierCount = computed(() => Math.max((tierBudget.value?.tier_limits.length ?? 0) - availableTierCount.value, 0))
const periodLabel = computed(() => ({ daily: '日', weekly: '周', monthly: '月' }[selectedPeriod.value]))

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
    const [budget, groups] = await Promise.all([
      getMyBudgetTiers(),
      getAvailableGroups().catch(() => ({ data: { groups: [] } })),
    ])
    tierBudget.value = budget
    availableGroups.value = groups.data.groups || []
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
.mtb { display: flex; flex-direction: column; gap: 16px; }
.mtb-page-head,
.tier-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.mtb-page-head h3 { margin: 0; color: var(--el-text-color-primary); }
.mtb-page-head p { margin: 4px 0 0; color: var(--el-text-color-secondary); font-size: 12px; }
.tier-summary-pills { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.tier-pill { display: inline-flex; align-items: center; min-height: 26px; padding: 0 10px; border-radius: 999px; font-size: 12px; font-weight: 600; background: var(--el-fill-color-light); color: var(--el-text-color-regular); }
.tier-pill.is-success { background: var(--el-color-success-light-9); color: var(--el-color-success); }
.tier-pill.is-warning { background: var(--el-color-warning-light-9); color: var(--el-color-warning-dark-2); }
.tier-pill.is-neutral { background: var(--el-fill-color-light); color: var(--el-text-color-secondary); }
.temporary-inline { display: flex; align-items: center; gap: 12px; padding: 10px 12px; border: 1px solid var(--el-color-warning-light-7); border-radius: 8px; background: var(--el-color-warning-light-9); }
.temporary-inline__icon { color: var(--el-color-warning); font-size: 18px; }
.temporary-inline > div:not(.temporary-values) { display: grid; flex: 1; gap: 2px; }
.temporary-inline small { color: var(--el-text-color-secondary); }
.temporary-values { display: grid; gap: 2px; text-align: right; }
.mtb-panel { overflow: hidden; border: 1px solid var(--el-border-color-light); border-radius: 10px; background: var(--el-bg-color); box-shadow: 0 1px 2px rgb(15 23 42 / 3%); }
.tier-table-head,
.tier-row { display: grid; grid-template-columns: minmax(180px, 1.05fr) minmax(180px, .95fr) minmax(240px, 1.35fr) 108px; gap: 18px; align-items: center; }
.tier-table-head { min-height: 40px; padding: 0 20px; color: var(--el-text-color-secondary); font-size: 12px; border-bottom: 1px solid var(--el-border-color-lighter); background: transparent; }
.tier-row { min-height: 88px; margin: 0 10px; padding: 14px 10px; border-bottom: 1px solid var(--el-border-color-lighter); }
.tier-row:last-child { border-bottom: 0; }
.tier-row.is-disabled { opacity: .82; }
.tier-row__identity,
.tier-row__amount { display: grid; gap: 6px; min-width: 0; }
.tier-row__title { display: flex; align-items: center; gap: 8px; min-width: 0; }
.tier-row__title strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tier-row__amount strong { color: var(--el-color-primary); font-size: 18px; font-weight: 800; }
.tier-row__amount span,
.tier-row__amount small,
.tier-row__muted { color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.5; }
.source-mini { display: inline-flex; width: fit-content; padding: 2px 7px; border-radius: 999px; background: var(--el-color-info-light-9); color: var(--el-color-info-dark-2); font-size: 12px; }
.tier-row__groups { display: grid; gap: 4px; min-width: 0; }
.tier-row__groups strong { font-size: 13px; color: var(--el-text-color-primary); }
.tier-row__groups span { overflow: hidden; color: var(--el-text-color-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.tier-groups__link { width: fit-content; margin: 0; padding: 0; border: 0; background: transparent; color: var(--el-color-primary); font-size: 12px; line-height: 1.4; cursor: pointer; }
.tier-groups__link:hover { text-decoration: underline; }
.tier-groups__empty { font-size: 12px; color: var(--el-text-color-secondary); }
.tier-row__action { display: flex; justify-content: flex-end; }
.groups-dialog-list { display: grid; gap: 6px; }
.groups-dialog-item { display: grid; gap: 3px; padding: 10px 2px; border-bottom: 1px solid var(--el-border-color-lighter); }
.groups-dialog-item:last-child { border-bottom: 0; }
.groups-dialog-item span { color: var(--el-text-color-secondary); font-size: 12px; }
@media (max-width: 980px) {
  .tier-table-head { display: none; }
  .tier-row { grid-template-columns: 1fr; gap: 10px; align-items: stretch; }
  .tier-row__action { justify-content: flex-start; }
}
@media (max-width: 760px) {
  .mtb-page-head,
  .tier-toolbar,
  .temporary-inline { align-items: stretch; flex-direction: column; }
  .mtb-page-head .el-button { width: 100%; }
  .temporary-values { text-align: left; }
}
</style>
