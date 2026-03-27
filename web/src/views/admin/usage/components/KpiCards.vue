<template>
  <div :class="['ud-kpi-row', { 'ud-kpi-row--compact': isCompactMode }]" v-loading="loading">
    <!-- 总成本（Chat 模式） -->
    <div v-if="!isCompactMode" class="ud-kpi-card ud-kpi-card--green">
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

    <!-- Token 消耗 -->
    <div class="ud-kpi-card ud-kpi-card--cyan">
      <div class="ud-kpi__icon"><el-icon size="20"><Coin /></el-icon></div>
      <div class="ud-kpi__body">
        <p class="ud-kpi__label">{{ isCompactMode && endpointType === 'embedding' ? '输入 Token' : 'Token 消耗' }}</p>
        <p class="ud-kpi__value">{{ tokenDisplayValue }}</p>
        <div class="ud-kpi__change-row">
          <span v-if="isCompactMode && endpointType === 'embedding'" class="ud-kpi__sub">
            合计 {{ fmtTokensShort(summary?.total_tokens) }}
          </span>
          <span v-else-if="!isCompactMode" class="ud-kpi__sub">
            输入 {{ fmtTokensShort(summary?.prompt_tokens) }} · 输出 {{ fmtTokensShort(summary?.completion_tokens) }}
          </span>
          <span v-else class="ud-kpi__sub">Rerank 按文档计费</span>
        </div>
      </div>
    </div>

    <!-- 成功率（Embedding / Rerank） -->
    <div v-if="isCompactMode" class="ud-kpi-card ud-kpi-card--amber">
      <div class="ud-kpi__icon"><el-icon size="20"><CircleCheck /></el-icon></div>
      <div class="ud-kpi__body">
        <p class="ud-kpi__label">成功率</p>
        <p class="ud-kpi__value">{{ successRateDisplay }}</p>
        <div class="ud-kpi__change-row">
          <span class="ud-kpi__sub">失败 {{ fmtNum(summary?.error_count) }} 次</span>
        </div>
      </div>
    </div>

    <!-- 活跃成员（Chat 模式） -->
    <div v-if="!isCompactMode && currentScope !== 'user'" class="ud-kpi-card ud-kpi-card--purple">
      <div class="ud-kpi__icon"><el-icon size="20"><User /></el-icon></div>
      <div class="ud-kpi__body">
        <p class="ud-kpi__label">活跃成员</p>
        <p class="ud-kpi__value">{{ summary?.user_count ?? '—' }}</p>
        <div class="ud-kpi__change-row">
          <span class="ud-kpi__sub">均次成本 {{ avgCostPerCall }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Money, DataLine, Coin, User, ArrowUp, ArrowDown, CircleCheck } from '@element-plus/icons-vue'
import { type Currency, convertCost, currencySymbol } from '@/utils/currency'
import { useSettingsStore } from '@/stores/settings'
import type { UsageEndpointType } from '@/utils/usage-endpoint'

defineOptions({ name: 'KpiCards' })

const props = defineProps<{
  summary: any
  loading: boolean
  currency: Currency
  costChange: number | null
  callsChange: number | null
  prevPeriodLabel: string
  currentBudget: any
  budgetLevel: string
  currentScope: string
  avgCostPerCall: string
  endpointType?: UsageEndpointType
}>()

const isCompactMode = computed(() =>
  props.endpointType === 'embedding' || props.endpointType === 'rerank',
)

const tokenDisplayValue = computed(() => {
  const s = props.summary
  if (!s) return '—'
  if (isCompactMode.value && props.endpointType === 'embedding') {
    const n = s.prompt_tokens ?? s.total_tokens ?? 0
    return fmtTokensShort(n)
  }
  if (s.total_tokens == null) return '—'
  if (s.total_tokens >= 1000000) return (s.total_tokens / 1000000).toFixed(2) + 'M'
  return (s.total_tokens / 1000).toFixed(1) + 'K'
})

const successRateDisplay = computed(() => {
  const rate = props.summary?.success_rate
  if (rate == null || Number.isNaN(rate)) return '—'
  return `${rate.toFixed(1)}%`
})

const settingsStore = useSettingsStore()

const cc = computed(() => ({
  currency: props.currency,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

// ── Helper functions ──────────────────────────────────────────

function fmtCost(v?: number | null): string {
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

function fmtNum(n?: number | null): string {
  return (n ?? 0).toLocaleString()
}

function fmtTokensShort(n?: number | null): string {
  if (!n) return '0'
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(0) + 'K'
  return String(n)
}

function fmtChange(pct: number | null): string {
  if (pct === null) return '—'
  return `${pct >= 0 ? '+' : ''}${pct.toFixed(1)}%`
}

function changeClass(pct: number | null, costMode = false): string {
  if (pct === null) return 'ud-change--neutral'
  if (pct === 0) return 'ud-change--neutral'
  const isUp = pct > 0
  if (costMode) return isUp ? 'ud-change--bad' : 'ud-change--good'
  return isUp ? 'ud-change--up' : 'ud-change--down'
}
</script>

<style scoped>
/* ── KPI 卡片 ── */
.ud-kpi-row {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 10px;
}
.ud-kpi-row--compact {
  grid-template-columns: repeat(3, 1fr);
}
@media (max-width: 1200px) { .ud-kpi-row { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 900px)  { .ud-kpi-row { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 540px)  { .ud-kpi-row { grid-template-columns: 1fr; } }

.ud-kpi-card {
  background: var(--ud-surface);
  border: 1px solid var(--ud-border);
  border-radius: 12px;
  padding: 10px 14px 8px;
  display: flex; align-items: flex-start; gap: 10px;
  position: relative; overflow: hidden;
}
.ud-kpi-card::before {
  content: ''; position: absolute; top: 0; left: 0;
  width: 4px; height: 100%; border-radius: 12px 0 0 12px;
}
.ud-kpi-card--green::before { background: #67c23a; }
.ud-kpi-card--blue::before  { background: #409eff; }
.ud-kpi-card--amber::before { background: #e6a23c; }
.ud-kpi-card--red::before   { background: #f56c6c; }
.ud-kpi-card--purple::before { background: #8B5CF6; }
.ud-kpi-card--cyan::before { background: #06B6D4; }

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
.ud-kpi-card--cyan .ud-kpi__icon { background: rgba(6,182,212,0.12); color: #06B6D4; }

.ud-kpi__body { flex: 1; min-width: 0; }
.ud-kpi__label { font-size: 12px; color: var(--ud-muted); margin: 0 0 4px; }
.ud-kpi__value { font-size: 22px; font-weight: 700; margin: 0 0 4px; color: var(--el-text-color-primary); line-height: 1.2; }
.ud-kpi__sub   { color: var(--ud-muted); }

/* 环比变化 */
.ud-kpi__change-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 2px; }
.ud-kpi__prev-val   { font-size: 11px; color: var(--ud-faint); }

.ud-change-badge {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 3px 9px; border-radius: 6px;
  font-size: 13px; cursor: default; white-space: nowrap;
}
.ud-change-badge b { font-size: 14px; font-weight: 700; }
.ud-change-badge__vs { font-size: 11px; opacity: 0.75; }
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

/* Mobile responsive */
@media (max-width: 640px) {
  .ud-kpi-row {
    grid-template-columns: 1fr;
  }
}
</style>