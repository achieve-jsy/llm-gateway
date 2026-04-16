<template>
  <el-dialog v-model="visible" :title="dialogTitle" width="680px" destroy-on-close>
    <div v-if="item" class="budget-detail-dialog">
      <section class="detail-section">
        <h3>周期限额</h3>
        <div class="detail-grid">
          <div class="detail-card" :class="periodClass(monthlyRatio)">
            <span>月额度</span>
            <strong>{{ formatBudgetUSD(item.amount_usd, { unlimited: true }) }}</strong>
            <small>已用 {{ formatBudgetUSD(item.used_usd) }}</small>
            <el-progress
              :percentage="Math.min(100, Math.round(monthlyRatio * 100))"
              :stroke-width="4"
              :color="progressColor(monthlyRatio)"
              :show-text="false"
            />
          </div>
          <div class="detail-card" :class="periodClass(weeklyRatio)">
            <span>周额度</span>
            <strong>{{ formatBudgetUSD(item.weekly_amount_usd, { unlimited: true }) }}</strong>
            <small>已用 {{ formatBudgetUSD(item.weekly_used_usd ?? 0) }}</small>
            <el-progress
              :percentage="Math.min(100, Math.round(weeklyRatio * 100))"
              :stroke-width="4"
              :color="progressColor(weeklyRatio)"
              :show-text="false"
            />
          </div>
          <div class="detail-card" :class="periodClass(dailyRatio)">
            <span>日额度</span>
            <strong>{{ formatBudgetUSD(item.daily_amount_usd, { unlimited: true }) }}</strong>
            <small>已用 {{ formatBudgetUSD(item.daily_used_usd ?? 0) }}</small>
            <el-progress
              :percentage="Math.min(100, Math.round(dailyRatio * 100))"
              :stroke-width="4"
              :color="progressColor(dailyRatio)"
              :show-text="false"
            />
          </div>
        </div>
      </section>

      <section v-if="tierRows.length" class="detail-section">
        <h3>等级子预算</h3>
        <el-table :data="tierRows" size="small" stripe>
          <el-table-column label="等级" min-width="120">
            <template #default="{ row }">
              <span class="tier-name">
                <span class="tier-dot" :style="{ background: row.color }" />
                {{ row.name }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="月额度" width="120" align="right">
            <template #default="{ row }">
              <span v-if="!row.enabled" class="muted">已停用</span>
              <span v-else-if="row.limit === null" class="muted">未配置</span>
              <span v-else>{{ formatBudgetUSD(row.limit, { compact: true }) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="已使用" width="120" align="right">
            <template #default="{ row }">
              <span>{{ formatBudgetUSD(row.used, { compact: true }) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="使用率" min-width="140">
            <template #default="{ row }">
              <template v-if="row.limit !== null && row.limit > 0">
                <el-progress
                  :percentage="Math.min(100, Math.round(row.ratio * 100))"
                  :stroke-width="5"
                  :color="progressColor(row.ratio)"
                />
              </template>
              <span v-else-if="row.limit === null" class="muted">无限制</span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90" align="center">
            <template #default="{ row }">
              <el-tag v-if="!row.enabled" type="info" size="small">已停用</el-tag>
              <el-tag v-else-if="row.limit === null" type="info" size="small">无限制</el-tag>
              <el-tag v-else-if="row.limit === 0" type="danger" size="small">已禁用</el-tag>
              <el-tag v-else :type="row.ratio >= 1 ? 'danger' : row.ratio >= 0.8 ? 'warning' : 'success'" size="small">
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

interface TierRow {
  name: string
  color?: string
  enabled: boolean
  limit: number | null
  used: number
  ratio: number
}

const props = defineProps<{
  item: BudgetItem | null
  tierLimits?: BudgetTierLimit[]
}>()

const visible = defineModel<boolean>({ default: false })

const dialogTitle = computed(() => {
  if (!props.item) return '预算详情'
  return `${props.item.label || props.item.scope_id} · 预算详情`
})

const monthlyRatio = computed(() => {
  if (!props.item) return 0
  const limit = Number(props.item.amount_usd ?? 0)
  const used = Number(props.item.used_usd ?? 0)
  return limit > 0 ? used / limit : 0
})

const weeklyRatio = computed(() => {
  if (!props.item) return 0
  const limit = Number(props.item.weekly_amount_usd ?? 0)
  const used = Number(props.item.weekly_used_usd ?? 0)
  return limit > 0 ? used / limit : 0
})

const dailyRatio = computed(() => {
  if (!props.item) return 0
  const limit = Number(props.item.daily_amount_usd ?? 0)
  const used = Number(props.item.daily_used_usd ?? 0)
  return limit > 0 ? used / limit : 0
})

const tierRows = computed<TierRow[]>(() => {
  if (!props.tierLimits?.length) return []
  return props.tierLimits.map(limit => {
    const limitValue = limit.base_limit_usd ?? limit.limit_usd
    const safeLimit = Number(limitValue ?? 0)
    const used = Number(limit.used_usd ?? 0)
    const ratio = safeLimit > 0 ? used / safeLimit : 0
    return {
      name: limit.model_tier_name ?? limit.model_tier_id,
      color: undefined,
      enabled: limit.enabled ?? true,
      limit: Number.isFinite(safeLimit) ? safeLimit : null,
      used,
      ratio,
    }
  })
})

function periodClass(ratio: number): string {
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return ''
}

function progressColor(ratio: number): string {
  if (ratio >= 1) return '#ef4444'
  if (ratio >= 0.8) return '#e6a23c'
  return '#36b34e'
}
</script>

<style scoped>
.budget-detail-dialog {
  display: grid;
  gap: 24px;
}

.detail-section h3 {
  margin: 0 0 12px;
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 600;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.detail-card {
  display: grid;
  gap: 8px;
  padding: 14px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fff;
}

.detail-card span {
  color: var(--text-secondary);
  font-size: 12px;
}

.detail-card strong {
  color: var(--text-primary);
  font-size: 18px;
  font-variant-numeric: tabular-nums;
}

.detail-card small {
  color: var(--text-secondary);
  font-size: 12px;
}

.detail-card.warning {
  border-color: var(--el-color-warning);
  background: #fef3e7;
}

.detail-card.warning strong {
  color: var(--el-color-warning);
}

.detail-card.danger {
  border-color: var(--el-color-danger);
  background: #fef0f0;
}

.detail-card.danger strong {
  color: var(--el-color-danger);
}

.tier-name {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tier-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.empty-state {
  padding: 24px;
  text-align: center;
}

.muted {
  color: var(--text-secondary);
}
</style>
