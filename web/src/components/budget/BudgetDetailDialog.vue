<template>
  <el-dialog v-model="visible" :title="dialogTitle" width="760px" destroy-on-close class="budget-detail-dialog-host">
    <div v-if="item" class="budget-detail-dialog" data-test="budget-detail-dialog">
      <section class="detail-section">
        <h3>周期限额</h3>
        <div class="detail-grid">
          <div class="detail-card" :class="periodClass(monthlyRatio)">
            <span class="detail-card__label">月额度</span>
            <strong class="detail-card__value">{{ formatBudgetUSD(item.amount_usd, { unlimited: true }) }}</strong>
            <div class="detail-card__meta">
              <span>已用 {{ formatBudgetUSD(item.used_usd) }}</span>
              <span>{{ usageLabel(monthlyRatio, item.amount_usd) }}</span>
            </div>
          </div>
          <div class="detail-card" :class="periodClass(weeklyRatio)">
            <span class="detail-card__label">周额度</span>
            <strong class="detail-card__value">{{ formatBudgetUSD(item.weekly_amount_usd, { unlimited: true }) }}</strong>
            <div class="detail-card__meta">
              <span>已用 {{ formatBudgetUSD(item.weekly_used_usd ?? 0) }}</span>
              <span>{{ usageLabel(weeklyRatio, item.weekly_amount_usd) }}</span>
            </div>
          </div>
          <div class="detail-card" :class="periodClass(dailyRatio)">
            <span class="detail-card__label">日额度</span>
            <strong class="detail-card__value">{{ formatBudgetUSD(item.daily_amount_usd, { unlimited: true }) }}</strong>
            <div class="detail-card__meta">
              <span>已用 {{ formatBudgetUSD(item.daily_used_usd ?? 0) }}</span>
              <span>{{ usageLabel(dailyRatio, item.daily_amount_usd) }}</span>
            </div>
          </div>
        </div>
      </section>

      <section v-if="tierRows.length" class="detail-section">
        <div class="detail-section__head">
          <h3>等级子预算</h3>
          <span class="detail-section__hint">用量与状态按月额度计算</span>
        </div>
        <el-table
          :data="tierRows"
          size="small"
          class="tier-detail-table"
          data-test="tier-detail-table"
        >
          <el-table-column label="等级" min-width="108">
            <template #default="{ row }">
              <span class="tier-name">
                <span class="tier-dot" :style="{ background: row.color || 'var(--el-color-info)' }" />
                <span class="tier-name__text">{{ row.name }}</span>
              </span>
            </template>
          </el-table-column>
          <el-table-column label="月额度" min-width="112" align="right">
            <template #default="{ row }">
              <span class="money-cell" :class="{ muted: !row.enabled || row.monthly === null }">
                {{ formatTierCell(row, row.monthly) }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="周额度" min-width="112" align="right">
            <template #default="{ row }">
              <span class="money-cell" :class="{ muted: !row.enabled || row.weekly === null }">
                {{ formatTierCell(row, row.weekly) }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="日额度" min-width="112" align="right">
            <template #default="{ row }">
              <span class="money-cell" :class="{ muted: !row.enabled || row.daily === null }">
                {{ formatTierCell(row, row.daily) }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="月用量" min-width="128" align="right">
            <template #default="{ row }">
              <span v-if="!row.enabled" class="muted">—</span>
              <span v-else class="usage-cell">
                <span class="money-cell">{{ formatBudgetUSD(row.used) }}</span>
                <span class="usage-cell__pct">{{ monthlyUsageText(row) }}</span>
              </span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="88" align="center">
            <template #default="{ row }">
              <el-tag v-if="!row.enabled" type="info" size="small" effect="plain">已停用</el-tag>
              <el-tag
                v-else-if="row.monthly === null && row.weekly === null && row.daily === null"
                type="info"
                size="small"
                effect="plain"
              >
                无限制
              </el-tag>
              <el-tag v-else-if="row.monthly === 0" type="info" size="small" effect="plain">未设置限额</el-tag>
              <el-tag
                v-else
                :type="row.ratio >= 1 ? 'danger' : row.ratio >= 0.8 ? 'warning' : 'success'"
                size="small"
                effect="plain"
              >
                {{ row.ratio >= 1 ? '已超额' : row.ratio >= 0.8 ? '预警' : '正常' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </section>
      <section v-else class="detail-section">
        <h3>等级子预算</h3>
        <div class="empty-state">
          <span class="muted">未配置等级子预算</span>
        </div>
      </section>
    </div>
    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { BudgetItem } from '@/types/budget'
import type { BudgetTierLimit } from '@/types/model-tier'
import { formatBudgetUSD } from '@/composables/useBudgetDisplay'
import { buildTierDetailRows, type TierDetailRow } from './budgetDetailRows'

const props = defineProps<{
  item: BudgetItem | null
  tierLimits?: BudgetTierLimit[]
}>()

const visible = defineModel<boolean>({ default: false })

const dialogTitle = computed(() => {
  if (!props.item) return '预算详情'
  return `${props.item.label || props.item.scope_id} · 预算详情`
})

const monthlyRatio = computed(() => ratioOf(props.item?.amount_usd, props.item?.used_usd))
const weeklyRatio = computed(() => ratioOf(props.item?.weekly_amount_usd, props.item?.weekly_used_usd))
const dailyRatio = computed(() => ratioOf(props.item?.daily_amount_usd, props.item?.daily_used_usd))
const tierRows = computed(() => buildTierDetailRows(props.tierLimits))

function ratioOf(limit?: number | null, used?: number | null): number {
  const safeLimit = Number(limit ?? 0)
  const safeUsed = Number(used ?? 0)
  return safeLimit > 0 ? safeUsed / safeLimit : 0
}

function usageLabel(ratio: number, limit?: number | null): string {
  const amount = Number(limit ?? NaN)
  if (!Number.isFinite(amount) || amount < 0) return '不限'
  if (amount === 0) return '已阻断'
  return `${Math.min(999, Math.round(ratio * 100))}%`
}

function formatTierCell(row: TierDetailRow, value: number | null): string {
  if (!row.enabled) return '—'
  if (value === null) return '未配置'
  return formatBudgetUSD(value)
}

function monthlyUsageText(row: TierDetailRow): string {
  if (row.monthly === null) return '无月限'
  if (row.monthly <= 0) return '—'
  return `${Math.min(999, Math.round(row.ratio * 100))}%`
}

function periodClass(ratio: number): string {
  if (ratio >= 1) return 'is-danger'
  if (ratio >= 0.8) return 'is-warning'
  return ''
}
</script>

<style scoped>
.budget-detail-dialog {
  display: grid;
  gap: 20px;
}

.detail-section h3 {
  margin: 0;
  color: var(--el-text-color-primary);
  font-size: 14px;
  font-weight: 600;
}

.detail-section__head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin-bottom: 10px;
}

.detail-section__hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.detail-section > h3 {
  margin-bottom: 10px;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.detail-card {
  display: grid;
  gap: 6px;
  min-width: 0;
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-blank);
}

.detail-card__label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.detail-card__value {
  color: var(--el-text-color-primary);
  font-size: 20px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.detail-card__meta {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.detail-card.is-warning {
  border-color: var(--el-color-warning-light-5);
  background: var(--el-color-warning-light-9);
}

.detail-card.is-warning .detail-card__value {
  color: var(--el-color-warning);
}

.detail-card.is-danger {
  border-color: var(--el-color-danger-light-5);
  background: var(--el-color-danger-light-9);
}

.detail-card.is-danger .detail-card__value {
  color: var(--el-color-danger);
}

.tier-detail-table {
  --el-table-border-color: var(--el-border-color-lighter);
}

.tier-detail-table :deep(.el-table__header th) {
  color: var(--el-text-color-secondary);
  font-weight: 600;
  background: var(--el-fill-color-light);
}

.tier-detail-table :deep(.el-table__cell) {
  padding: 10px 0;
}

.tier-name {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.tier-name__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tier-dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.money-cell {
  display: inline-block;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.usage-cell {
  display: inline-flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
  line-height: 1.2;
}

.usage-cell__pct {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.empty-state {
  padding: 20px;
  border: 1px dashed var(--el-border-color);
  border-radius: 8px;
  text-align: center;
  background: var(--el-fill-color-lighter);
}

.muted {
  color: var(--el-text-color-secondary);
}

@media (max-width: 720px) {
  .detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
