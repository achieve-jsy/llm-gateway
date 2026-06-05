<template>
  <div class="tier-limits-editor" data-test="tier-limits-editor">
    <div class="tier-limits-editor__hero">
      <div>
        <div class="tier-limits-editor__eyebrow">月度预算</div>
        <div class="tier-limits-editor__hero-value">
          <strong>{{ formatBudgetUSD(monthlyUsage) }}</strong>
          <span>/ {{ monthlyLimitLabel }}</span>
        </div>
        <div class="tier-limits-editor__hero-note">
          使用率 {{ monthlyUsagePercent.toFixed(0) }}%
          <template v-if="monthlyConfigured && monthlyUnlimited"> · 当前不限</template>
        </div>
      </div>
      <div
        class="tier-limits-editor__hero-bar"
        role="progressbar"
        :aria-valuenow="monthlyUsagePercent"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-label="`月度使用率 ${monthlyUsagePercent.toFixed(0)}%`"
      >
        <i :style="{ width: `${Math.min(100, monthlyUsagePercent)}%` }" />
      </div>
    </div>

    <section class="tier-limits-editor__section">
      <div class="tier-limits-editor__heading">
        <div>
          <h3>月度上限</h3>
          <p>默认只改月额度；日/周与调整原因在高级设置。</p>
        </div>
      </div>

      <div class="tier-limits-editor__primary">
        <div v-if="monthlyUnlimited" class="tier-limits-editor__unlimited" data-test="monthly-unlimited">
          <strong>不限</strong>
          <span>不限制月度总消耗，仅受上级预算约束。</span>
          <el-button :disabled="disabled" data-test="monthly-set-limit" @click="setMonthlyLimited()">
            设置上限
          </el-button>
        </div>
        <div v-else class="tier-limits-editor__input-row">
          <el-input
            :model-value="displayMonthlyValue"
            inputmode="decimal"
            :disabled="disabled"
            placeholder="输入月度上限（USD）"
            data-test="monthly-limit-input"
            @update:model-value="updateTotalPeriod('monthly', { configured: true, value: String($event) })"
          >
            <template #prefix>$</template>
          </el-input>
          <el-button :disabled="disabled" data-test="monthly-set-unlimited" @click="setMonthlyUnlimited()">
            设为不限
          </el-button>
        </div>

        <div class="tier-limits-editor__effective">
          <span>生效</span>
          <el-radio-group
            :model-value="modelValue.effectiveMode"
            size="small"
            :disabled="disabled"
            @update:model-value="updateRoot('effectiveMode', $event)"
          >
            <el-radio-button value="immediate">立即</el-radio-button>
            <el-radio-button value="next_period" :disabled="firstTimeSetup">下周期</el-radio-button>
          </el-radio-group>
          <span v-if="firstTimeSetup" class="tier-limits-editor__effective-hint">首次配置仅支持立即生效</span>
        </div>
      </div>
    </section>

    <section class="tier-limits-editor__section">
      <div class="tier-limits-editor__heading">
        <div>
          <h3>等级子预算</h3>
          <p>默认按月限制；日/周在高级设置。未配置则只受总预算约束。</p>
        </div>
      </div>

      <div class="tier-limits-editor__tier-list">
        <article v-for="tier in tiers" :key="tier.id" class="tier-limits-editor__tier-row">
          <div class="tier-limits-editor__tier-meta">
            <TierColorDot :color="tier.color" />
            <div>
              <strong>{{ tier.name }}</strong>
              <span>{{ tier.group_count }} 个模型组</span>
            </div>
            <BudgetStatusTag :status="tierStatus(tier)" />
          </div>

          <div class="tier-limits-editor__tier-usage">
            <span>已用 {{ formatBudgetUSD(tierUsage(tier.id)?.used_usd ?? 0) }}</span>
            <span>有效额度 {{ tierLimitLabel(tier.id) }}</span>
          </div>

          <div class="tier-limits-editor__tier-controls">
            <el-input
              :model-value="tierDraft(tier.id).value"
              inputmode="decimal"
              :disabled="disabled || !tier.enabled"
              placeholder="未配置"
              @update:model-value="updateTier(tier.id, 'monthly', {
                configured: true,
                value: String($event),
              })"
            />
            <el-button
              :data-test="`tier-${tier.id}-zero`"
              :disabled="disabled || !tier.enabled"
              @click="updateTier(tier.id, 'monthly', { configured: true, value: '0' })"
            >
              设为 $0
            </el-button>
            <el-button
              :data-test="`tier-${tier.id}-unconfigure`"
              :disabled="disabled || !tier.enabled"
              @click="updateTier(tier.id, 'monthly', { configured: false, value: '' })"
            >
              移除限制
            </el-button>
          </div>
        </article>
      </div>

      <div class="tier-limits-editor__legend">
        <span><i class="is-unconfigured" />未配置：只受总预算约束</span>
        <span><i class="is-zero" />$0 表示当前等级不可使用</span>
        <span><i class="is-disabled" />等级停用：不可用于新关联</span>
      </div>
    </section>

    <section class="tier-limits-editor__advanced">
      <button
        type="button"
        class="tier-limits-editor__advanced-toggle"
        data-test="advanced-toggle"
        :aria-expanded="advancedOpen"
        @click="advancedOpen = !advancedOpen"
      >
        <span>高级设置</span>
        <span class="tier-limits-editor__advanced-hint">总预算日/周 · 等级日/周 · 调整原因</span>
        <span aria-hidden="true">{{ advancedOpen ? '收起' : '展开' }}</span>
      </button>

      <div v-if="advancedOpen" class="tier-limits-editor__advanced-body">
        <div class="tier-limits-editor__advanced-block-head">
          <strong>总预算 · 日/周</strong>
        </div>
        <div class="tier-limits-editor__advanced-grid">
          <div class="tier-limits-editor__limit-card">
            <div class="tier-limits-editor__limit-head">
              <div>
                <strong>每日上限</strong>
                <span>已用 {{ formatBudgetUSD(dailyUsage) }}</span>
              </div>
              <span class="tier-limits-editor__limit-state">{{ limitStateLabel(modelValue.total.daily) }}</span>
            </div>
            <div class="tier-limits-editor__limit-control">
              <template v-if="isUnlimited(modelValue.total.daily)">
                <div class="tier-limits-editor__limit-unlimited" data-test="daily-unlimited">不限</div>
                <el-button
                  plain
                  :disabled="disabled"
                  data-test="daily-set-limit"
                  @click="updateTotalPeriod('daily', { configured: true, value: '' })"
                >
                  设置上限
                </el-button>
              </template>
              <template v-else>
                <el-input
                  :model-value="modelValue.total.daily.configured ? modelValue.total.daily.value : ''"
                  inputmode="decimal"
                  :disabled="disabled"
                  placeholder="未配置"
                  data-test="daily-limit-input"
                  @update:model-value="updateTotalPeriod('daily', { configured: String($event).trim() !== '', value: String($event) })"
                >
                  <template #prefix>$</template>
                </el-input>
                <el-button
                  plain
                  :disabled="disabled"
                  data-test="daily-set-unlimited"
                  @click="updateTotalPeriod('daily', { configured: true, value: '-1' })"
                >
                  设为不限
                </el-button>
              </template>
            </div>
          </div>

          <div class="tier-limits-editor__limit-card">
            <div class="tier-limits-editor__limit-head">
              <div>
                <strong>每周上限</strong>
                <span>已用 {{ formatBudgetUSD(weeklyUsage) }}</span>
              </div>
              <span class="tier-limits-editor__limit-state">{{ limitStateLabel(modelValue.total.weekly) }}</span>
            </div>
            <div class="tier-limits-editor__limit-control">
              <template v-if="isUnlimited(modelValue.total.weekly)">
                <div class="tier-limits-editor__limit-unlimited" data-test="weekly-unlimited">不限</div>
                <el-button
                  plain
                  :disabled="disabled"
                  data-test="weekly-set-limit"
                  @click="updateTotalPeriod('weekly', { configured: true, value: '' })"
                >
                  设置上限
                </el-button>
              </template>
              <template v-else>
                <el-input
                  :model-value="modelValue.total.weekly.configured ? modelValue.total.weekly.value : ''"
                  inputmode="decimal"
                  :disabled="disabled"
                  placeholder="未配置"
                  data-test="weekly-limit-input"
                  @update:model-value="updateTotalPeriod('weekly', { configured: String($event).trim() !== '', value: String($event) })"
                >
                  <template #prefix>$</template>
                </el-input>
                <el-button
                  plain
                  :disabled="disabled"
                  data-test="weekly-set-unlimited"
                  @click="updateTotalPeriod('weekly', { configured: true, value: '-1' })"
                >
                  设为不限
                </el-button>
              </template>
            </div>
          </div>
        </div>

        <div class="tier-limits-editor__advanced-block" data-test="tier-period-advanced">
          <div class="tier-limits-editor__advanced-block-head">
            <div>
              <strong>等级子预算 · 日/周</strong>
              <p>未配置则该周期只受总预算约束；与月度子预算同一套语义。</p>
            </div>
          </div>

          <div class="tier-limits-editor__tier-period-list">
            <article
              v-for="tier in tiers"
              :key="`advanced-${tier.id}`"
              class="tier-limits-editor__tier-period-row"
            >
              <div class="tier-limits-editor__tier-meta">
                <TierColorDot :color="tier.color" />
                <div>
                  <strong>{{ tier.name }}</strong>
                  <span>{{ tier.enabled ? '可配置' : '已停用' }}</span>
                </div>
              </div>

              <div class="tier-limits-editor__tier-period-controls">
                <div class="tier-limits-editor__tier-period-field">
                  <label>日</label>
                  <span class="tier-limits-editor__period-usage" :data-test="`tier-${tier.id}-daily-used`">
                    已用 {{ formatBudgetUSD(tierUsage(tier.id, 'daily')?.used_usd ?? 0) }}
                  </span>
                  <el-input
                    :model-value="tierPeriodDraft(tier.id, 'daily').value"
                    inputmode="decimal"
                    :disabled="disabled || !tier.enabled"
                    placeholder="未配置"
                    :data-test="`tier-${tier.id}-daily-input`"
                    @update:model-value="updateTier(tier.id, 'daily', {
                      configured: true,
                      value: String($event),
                    })"
                  >
                    <template #prefix>$</template>
                  </el-input>
                  <el-button
                    plain
                    :data-test="`tier-${tier.id}-daily-zero`"
                    :disabled="disabled || !tier.enabled"
                    @click="updateTier(tier.id, 'daily', { configured: true, value: '0' })"
                  >
                    $0
                  </el-button>
                  <el-button
                    plain
                    :data-test="`tier-${tier.id}-daily-unconfigure`"
                    :disabled="disabled || !tier.enabled"
                    @click="updateTier(tier.id, 'daily', { configured: false, value: '' })"
                  >
                    移除
                  </el-button>
                </div>

                <div class="tier-limits-editor__tier-period-field">
                  <label>周</label>
                  <span class="tier-limits-editor__period-usage" :data-test="`tier-${tier.id}-weekly-used`">
                    已用 {{ formatBudgetUSD(tierUsage(tier.id, 'weekly')?.used_usd ?? 0) }}
                  </span>
                  <el-input
                    :model-value="tierPeriodDraft(tier.id, 'weekly').value"
                    inputmode="decimal"
                    :disabled="disabled || !tier.enabled"
                    placeholder="未配置"
                    :data-test="`tier-${tier.id}-weekly-input`"
                    @update:model-value="updateTier(tier.id, 'weekly', {
                      configured: true,
                      value: String($event),
                    })"
                  >
                    <template #prefix>$</template>
                  </el-input>
                  <el-button
                    plain
                    :data-test="`tier-${tier.id}-weekly-zero`"
                    :disabled="disabled || !tier.enabled"
                    @click="updateTier(tier.id, 'weekly', { configured: true, value: '0' })"
                  >
                    $0
                  </el-button>
                  <el-button
                    plain
                    :data-test="`tier-${tier.id}-weekly-unconfigure`"
                    :disabled="disabled || !tier.enabled"
                    @click="updateTier(tier.id, 'weekly', { configured: false, value: '' })"
                  >
                    移除
                  </el-button>
                </div>
              </div>
            </article>
          </div>
        </div>

        <el-form-item class="tier-limits-editor__reason" label="调整原因" required>
          <el-input
            :model-value="modelValue.reason"
            type="textarea"
            :rows="2"
            maxlength="200"
            show-word-limit
            :disabled="disabled"
            placeholder="例如：补充项目集中交付额度"
            data-test="adjust-reason"
            @update:model-value="updateRoot('reason', String($event))"
          />
        </el-form-item>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, toRaw } from 'vue'
import type { BudgetItem } from '@/types/budget'
import type { ExpansionPeriod } from '@/types/expansion'
import type {
  BudgetTierLimit,
  BudgetTierUsage,
  ModelTier,
  TierLimitsDraft,
} from '@/types/model-tier'
import {
  formatBudgetUSD,
  getBudgetStatus,
  toUsagePercent,
} from '@/composables/useBudgetDisplay'
import BudgetStatusTag from './BudgetStatusTag.vue'
import TierColorDot from './TierColorDot.vue'

type TotalEntry = TierLimitsDraft['total'][ExpansionPeriod]

const props = defineProps<{
  allocation: BudgetItem
  tiers: ModelTier[]
  limits: BudgetTierLimit[]
  usages: BudgetTierUsage[]
  modelValue: TierLimitsDraft
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: TierLimitsDraft]
  'dirty-change': [value: boolean]
}>()

const advancedOpen = ref(false)
const monthlyEntry = computed(() => props.modelValue.total.monthly)
const monthlyConfigured = computed(() => monthlyEntry.value.configured)
const monthlyUnlimited = computed(() => isUnlimited(monthlyEntry.value))
const displayMonthlyValue = computed(() => (
  monthlyUnlimited.value ? '' : monthlyEntry.value.value
))
const firstTimeSetup = computed(() => !props.allocation.allocation_id)
const monthlyUsage = computed(() => props.allocation.used_usd ?? 0)
const dailyUsage = computed(() => props.allocation.daily_used_usd ?? 0)
const weeklyUsage = computed(() => props.allocation.weekly_used_usd ?? 0)
const monthlyLimitLabel = computed(() => {
  if (!monthlyEntry.value.configured) return '未配置'
  return formatBudgetUSD(monthlyEntry.value.value, { unlimited: true })
})
const monthlyUsagePercent = computed(() => (
  monthlyEntry.value.configured && !monthlyUnlimited.value
    ? toUsagePercent(monthlyUsage.value, monthlyEntry.value.value)
    : 0
))

function isUnlimited(entry: TotalEntry): boolean {
  return entry.configured && Number(entry.value) < 0
}

function limitStateLabel(entry: TotalEntry): string {
  if (!entry.configured) return '未配置'
  if (isUnlimited(entry)) return '不限'
  if (entry.value === '0') return '$0 阻断'
  return '已设置'
}

function cloneDraft(): TierLimitsDraft {
  return structuredClone(toRaw(props.modelValue))
}

function publish(next: TierLimitsDraft): void {
  emit('update:modelValue', next)
  emit('dirty-change', true)
}

function updateRoot<Key extends 'effectiveMode' | 'reason'>(
  key: Key,
  value: TierLimitsDraft[Key],
): void {
  const next = cloneDraft()
  next[key] = value
  publish(next)
}

function updateTotalPeriod(period: ExpansionPeriod, patch: Partial<TotalEntry>): void {
  const next = cloneDraft()
  next.total[period] = {
    ...next.total[period],
    ...patch,
  }
  publish(next)
}

function setMonthlyUnlimited(): void {
  updateTotalPeriod('monthly', { configured: true, value: '-1' })
}

function setMonthlyLimited(): void {
  updateTotalPeriod('monthly', { configured: true, value: '' })
}

function updateTier(
  tierId: string,
  period: ExpansionPeriod,
  patch: Partial<TierLimitsDraft['tiers'][string][ExpansionPeriod]>,
): void {
  const next = cloneDraft()
  next.tiers[tierId][period] = {
    ...next.tiers[tierId][period],
    ...patch,
  }
  publish(next)
}

function tierDraft(tierId: string) {
  return props.modelValue.tiers[tierId].monthly
}

function tierPeriodDraft(tierId: string, period: ExpansionPeriod) {
  return props.modelValue.tiers[tierId][period]
}

function tierLimit(tierId: string, period: ExpansionPeriod = 'monthly'): BudgetTierLimit | undefined {
  return props.limits.find(limit => (
    limit.model_tier_id === tierId && limit.period_type === period
  ))
}

function tierUsage(tierId: string, period: ExpansionPeriod = 'monthly'): BudgetTierUsage | undefined {
  return props.usages.find(usage => (
    usage.model_tier_id === tierId && usage.period_type === period
  ))
}

function tierLimitLabel(tierId: string): string {
  const entry = tierDraft(tierId)
  if (!entry.configured) return '未配置'
  return formatBudgetUSD(entry.value)
}

function tierStatus(tier: ModelTier) {
  const entry = tierDraft(tier.id)
  const usage = tierUsage(tier.id)
  const configuredLimit = tierLimit(tier.id)
  const limit = entry.configured
    ? entry.value
    : configuredLimit?.effective_limit_usd ?? configuredLimit?.limit_usd ?? configuredLimit?.base_limit_usd
  const usedAmount = Number(usage?.used_usd ?? 0)
  const limitAmount = Number(limit)
  const usageRatio = Number.isFinite(usedAmount) && Number.isFinite(limitAmount) && limitAmount > 0
    ? usedAmount / limitAmount
    : 0
  return getBudgetStatus({
    configured: entry.configured,
    enabled: tier.enabled && entry.enabled,
    limit,
    usageRatio,
    stale: usage?.stale,
  })
}

defineExpose({ openAdvanced: () => { advancedOpen.value = true } })
</script>

<style scoped>
.tier-limits-editor {
  display: grid;
  gap: 14px;
}

.tier-limits-editor__hero {
  display: grid;
  gap: 10px;
  padding: 14px 16px;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  background: #f8fafc;
}

.tier-limits-editor__eyebrow {
  font-size: 12px;
  color: #64748b;
}

.tier-limits-editor__hero-value {
  display: flex;
  align-items: baseline;
  gap: 6px;
  margin-top: 4px;
}

.tier-limits-editor__hero-value strong {
  font-size: 24px;
  color: #0f172a;
}

.tier-limits-editor__hero-value span,
.tier-limits-editor__hero-note {
  color: #64748b;
  font-size: 13px;
}

.tier-limits-editor__hero-bar {
  height: 6px;
  overflow: hidden;
  border-radius: 999px;
  background: #e2e8f0;
}

.tier-limits-editor__hero-bar > i {
  display: block;
  height: 100%;
  background: #36b34e;
}

.tier-limits-editor__section {
  padding: 16px;
  border: 1px solid #e2e8f0;
  background: #fff;
  border-radius: 12px;
}

.tier-limits-editor__heading {
  margin-bottom: 14px;
}

h3,
p {
  margin: 0;
}

h3 {
  color: #0f172a;
  font-size: 15px;
}

p,
.tier-limits-editor__tier-meta span,
.tier-limits-editor__tier-usage {
  color: #64748b;
  font-size: 12px;
}

.tier-limits-editor__heading p {
  margin-top: 4px;
}

.tier-limits-editor__primary {
  display: grid;
  gap: 12px;
}

.tier-limits-editor__input-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.tier-limits-editor__unlimited,
.tier-limits-editor__mini-unlimited {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 10px 12px;
  border: 1px dashed #cbd5e1;
  border-radius: 10px;
  background: #f8fafc;
}

.tier-limits-editor__unlimited strong,
.tier-limits-editor__mini-unlimited strong {
  color: #0f172a;
}

.tier-limits-editor__unlimited span {
  color: #64748b;
  font-size: 12px;
  margin-right: auto;
}

.tier-limits-editor__effective {
  display: flex;
  align-items: center;
  gap: 10px;
  color: #64748b;
  font-size: 13px;
}

.tier-limits-editor__effective-hint {
  font-size: 12px;
  color: #b45309;
  background: #fef3c7;
  padding: 2px 8px;
  border-radius: 6px;
}

.tier-limits-editor__tier-list {
  display: grid;
}

.tier-limits-editor__tier-row {
  display: grid;
  grid-template-columns: minmax(160px, 0.8fr) minmax(140px, 0.6fr) minmax(280px, 1.2fr);
  align-items: center;
  gap: 12px;
  min-height: 56px;
  padding: 10px 0;
  border-top: 1px solid #f1f5f9;
}

.tier-limits-editor__tier-row:first-child {
  border-top: 0;
}

.tier-limits-editor__tier-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tier-limits-editor__tier-meta > div {
  display: grid;
  min-width: 0;
  margin-right: auto;
}

.tier-limits-editor__tier-usage {
  display: grid;
  gap: 2px;
}

.tier-limits-editor__tier-controls {
  display: flex;
  gap: 8px;
  width: 100%;
}

.tier-limits-editor__legend {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid #f1f5f9;
  color: #64748b;
  font-size: 12px;
}

.tier-limits-editor__legend span {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.tier-limits-editor__legend i {
  width: 8px;
  height: 8px;
  border-radius: 999px;
}

.tier-limits-editor__legend .is-unconfigured { background: #94a3b8; }
.tier-limits-editor__legend .is-zero { background: #ef4444; }
.tier-limits-editor__legend .is-disabled { background: #64748b; }

.tier-limits-editor__advanced {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  overflow: hidden;
  background: #fff;
}

.tier-limits-editor__advanced-toggle {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 12px 16px;
  border: 0;
  background: #f8fafc;
  cursor: pointer;
  text-align: left;
  color: #0f172a;
  font-size: 14px;
  font-weight: 600;
}

.tier-limits-editor__advanced-hint {
  margin-right: auto;
  color: #94a3b8;
  font-size: 12px;
  font-weight: 400;
}

.tier-limits-editor__advanced-body {
  display: grid;
  gap: 12px;
  padding: 14px 16px 16px;
  border-top: 1px solid #e2e8f0;
}

.tier-limits-editor__advanced-block-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.tier-limits-editor__advanced-block-head strong {
  color: #0f172a;
  font-size: 13px;
}

.tier-limits-editor__advanced-block-head p {
  margin-top: 4px;
  color: #64748b;
  font-size: 12px;
}

.tier-limits-editor__advanced-block {
  display: grid;
  gap: 10px;
  padding-top: 4px;
  border-top: 1px dashed #e2e8f0;
}

.tier-limits-editor__advanced-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.tier-limits-editor__tier-period-list {
  display: grid;
}

.tier-limits-editor__tier-period-row {
  display: grid;
  grid-template-columns: minmax(120px, 0.45fr) minmax(0, 1.55fr);
  align-items: start;
  gap: 12px;
  padding: 10px 0;
  border-top: 1px solid #f1f5f9;
}

.tier-limits-editor__tier-period-row:first-child {
  border-top: 0;
  padding-top: 0;
}

.tier-limits-editor__tier-period-controls {
  display: grid;
  gap: 8px;
}

.tier-limits-editor__tier-period-field {
  display: grid;
  grid-template-columns: 28px minmax(92px, 0.4fr) minmax(0, 1fr) max-content max-content;
  align-items: center;
  gap: 8px;
}

.tier-limits-editor__tier-period-field label {
  color: #64748b;
  font-size: 12px;
  font-weight: 600;
}

.tier-limits-editor__period-usage {
  color: #64748b;
  font-size: 12px;
  white-space: nowrap;
}

.tier-limits-editor__tier-period-field :deep(.el-button) {
  padding: 0 10px;
  margin-left: 0;
}

.tier-limits-editor__limit-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;
}

.tier-limits-editor__limit-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.tier-limits-editor__limit-head > div {
  display: flex;
  align-items: baseline;
  gap: 6px;
}

.tier-limits-editor__limit-head strong {
  color: #0f172a;
  font-size: 13px;
}

.tier-limits-editor__limit-head span {
  color: #94a3b8;
  font-size: 11px;
}

.tier-limits-editor__limit-state {
  flex: none;
  color: #94a3b8 !important;
  font-size: 11px !important;
}

.tier-limits-editor__limit-control {
  display: grid;
  grid-template-columns: minmax(0, 1fr) max-content;
  align-items: center;
  gap: 8px;
}

.tier-limits-editor__limit-control :deep(.el-input__wrapper) {
  box-shadow: 0 0 0 1px #d8dee8 inset;
}

.tier-limits-editor__limit-control :deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px #b7c3d0 inset;
}

.tier-limits-editor__limit-control :deep(.el-button) {
  padding: 0 10px;
  border-color: #e2e8f0;
  background: #fff;
  color: #475569;
}

.tier-limits-editor__limit-control :deep(.el-button:hover) {
  border-color: #84d293;
  color: #2a9c4b;
}

.tier-limits-editor__limit-control :deep(.el-button) {
  margin-left: 0;
}

.tier-limits-editor__limit-unlimited {
  display: flex;
  align-items: center;
  min-height: 32px;
  padding: 0 11px;
  border: 1px dashed #d8dee8;
  border-radius: 7px;
  background: #fff;
  color: #0f172a;
  font-weight: 700;
}

.tier-limits-editor__reason {
  margin-bottom: 0;
  padding-top: 2px;
}

.tier-limits-editor__reason :deep(.el-form-item__label) {
  margin-bottom: 6px;
  color: #334155;
  font-weight: 600;
}

@media (max-width: 760px) {
  .tier-limits-editor__tier-row,
  .tier-limits-editor__advanced-grid,
  .tier-limits-editor__tier-period-row {
    grid-template-columns: 1fr;
  }

  .tier-limits-editor__tier-controls,
  .tier-limits-editor__input-row {
    flex-wrap: wrap;
  }

  .tier-limits-editor__limit-control,
  .tier-limits-editor__tier-period-field {
    grid-template-columns: 1fr;
  }
}
</style>
