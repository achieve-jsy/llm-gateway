<template>
  <section class="proto-overview" data-test="budget-manage-overview">
    <!-- 原型：独立 KPI 卡片，首卡 primary 绿边 -->
    <div class="proto-kpis">
      <div class="proto-kpi primary">
        <div class="proto-kpi-label">
          {{ poolLabel }}
          <span class="proto-tag gray">下发率 {{ allocationRate }}%</span>
        </div>
        <div class="proto-kpi-value">{{ formatBudgetUSD(poolAmount, { unlimited: true }) }}</div>
        <div class="proto-kpi-note">
          已下发 {{ formatBudgetUSD(allocated) }} · 本月已用 {{ formatBudgetUSD(poolUsed) }}
        </div>
        <div
          class="proto-progress"
          role="progressbar"
          :aria-valuenow="allocationRate"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-label="`下发率 ${allocationRate}%`"
        >
          <i :style="{ width: `${allocationRate}%` }" />
        </div>
      </div>

      <div class="proto-kpi">
        <div class="proto-kpi-label">未下发余量</div>
        <div class="proto-kpi-value">{{ formatBudgetUSD(remaining) }}</div>
        <div class="proto-kpi-note">
          {{ remaining > 0 ? `可继续分配给${childUnit}` : poolAmount > 0 ? '已全部下发' : '—' }}
        </div>
      </div>

      <button type="button" class="proto-kpi is-clickable" data-test="kpi-risk" @click="emit('kpiSelect', 'risk')">
        <div class="proto-kpi-label">
          等级预算风险
          <span class="proto-kpi-link">查看 →</span>
        </div>
        <div class="proto-kpi-value" :class="{ warn: riskCount > 0 }">{{ riskCount }}</div>
        <div class="proto-kpi-note">{{ riskNoteText }}</div>
      </button>

      <div class="proto-kpi">
        <div class="proto-kpi-label">管辖{{ childUnit }}</div>
        <div class="proto-kpi-value">{{ rows.length }}</div>
        <div class="proto-kpi-note">个{{ childUnit }}</div>
      </div>

      <button type="button" class="proto-kpi is-clickable" data-test="kpi-expansion" @click="emit('kpiSelect', 'expansion')">
        <div class="proto-kpi-label">扩容申请</div>
        <div class="proto-kpi-value action">查看</div>
        <div class="proto-kpi-note">审批{{ role === 'admin' ? '平台' : '' }}扩容申请 →</div>
      </button>
    </div>

    <div class="proto-budget-grid">
      <div class="proto-panel">
        <div class="proto-panel-head">
          <div>
            <h3>{{ tableTitle }}</h3>
            <p>{{ tableHint }}</p>
          </div>
        </div>

        <div v-if="rows.length === 0" class="proto-table-empty">
          <BudgetEmptyState
            :level="emptyLevel"
            :action-label="primaryEmptyAction.label"
            :action-test="primaryEmptyAction.test"
            @action="emit('emptyAction')"
          />
        </div>
        <div v-else class="proto-table-wrap">
          <table class="proto-native-table">
            <thead>
              <tr>
                <th v-if="canSelect" class="center" style="width:42px">
                  <input
                    class="proto-check"
                    type="checkbox"
                    :checked="allSelected"
                    :indeterminate.prop="someSelected && !allSelected"
                    aria-label="全选"
                    @change="toggleAll(($event.target as HTMLInputElement).checked)"
                  >
                </th>
                <th>{{ childColumnLabel }}</th>
                <th class="right">已分配额度</th>
                <th>已使用</th>
                <th>等级预算</th>
                <th class="center">状态</th>
                <th class="center">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in rows" :key="rowKey(row)">
                <td v-if="canSelect" class="center">
                  <input
                    class="proto-check"
                    type="checkbox"
                    :checked="isSelected(row)"
                    :aria-label="`选择 ${row.label || row.scope_id}`"
                    @change="toggleRow(row, ($event.target as HTMLInputElement).checked)"
                  >
                </td>
                <td>
                  <strong class="proto-scope-name">{{ row.label || row.scope_id }}</strong>
                </td>
                <td class="right mono">{{ formatBudgetUSD(row.amount_usd, { unlimited: true }) }}</td>
                <td>
                  <div class="proto-usage">
                    <span class="mono">
                      {{ formatBudgetUSD(row.used_usd) }} / {{ formatBudgetUSD(row.amount_usd, { unlimited: true }) }}
                    </span>
                    <div class="proto-usage-meta">
                      <div
                        class="proto-progress"
                        role="progressbar"
                        :aria-valuenow="usagePercent(row)"
                        aria-valuemin="0"
                        aria-valuemax="100"
                        :aria-label="`使用率 ${usagePercent(row)}%，${statusLabel(row.used_ratio)}`"
                      >
                        <i :style="{ width: `${usagePercent(row)}%`, background: progressColor(row.used_ratio) }" />
                      </div>
                      <span class="proto-usage-status" :class="statusTone(row.used_ratio)">
                        {{ statusLabel(row.used_ratio) }}
                      </span>
                    </div>
                  </div>
                </td>
                <td>
                  <template v-if="tierTags(row).length">
                    <span
                      v-for="tag in tierTags(row)"
                      :key="tag.id"
                      class="proto-tier-tag"
                      :class="getTierTagTone(tag.name, tag.state)"
                    >
                      {{ tag.label }}
                    </span>
                  </template>
                  <span v-else class="muted">未配置子预算</span>
                </td>
                <td class="center">
                  <span class="proto-tag" :class="statusTone(row.used_ratio)">{{ statusLabel(row.used_ratio) }}</span>
                </td>
                <td class="center">
                  <button type="button" class="proto-btn sm plain-primary" @click="emit('drill', row)">
                    {{ drillLabel }}
                  </button>
                  <button type="button" class="proto-btn sm" @click="emit('edit', row)">配置</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="selectedRows.length" class="proto-selection-bar" data-test="selection-bar">
          <strong>已选择 {{ selectedRows.length }} 项</strong>
          <span>当前未下发余量 {{ formatBudgetUSD(remaining) }}</span>
          <span class="grow" />
          <button type="button" class="proto-btn sm" data-test="batch-absolute" @click="emitBatch('absolute')">设置固定额度</button>
          <button type="button" class="proto-btn sm" data-test="batch-percentage" @click="emitBatch('percentage')">按比例增减</button>
          <button type="button" class="proto-btn sm" data-test="batch-tier" @click="emitBatch('tier')">等级子预算</button>
          <button type="button" class="proto-link" @click="clearSelection">取消选择</button>
        </div>
      </div>

      <aside class="proto-panel proto-side">
        <h4>治理链路</h4>
        <div class="proto-chain current">
          <div class="proto-chain-row">
            <span>● {{ chainRootLabel }}</span>
            <span class="proto-tag green">当前</span>
          </div>
          <strong>{{ formatBudgetUSD(poolAmount, { unlimited: true }) }}</strong>
        </div>
        <div class="proto-chain-arrow">↓</div>
        <div class="proto-chain dashed">
          <div class="proto-chain-row">
            <span>下发至 {{ rows.length }} 个{{ childUnit }}</span>
            <button type="button" class="proto-link" data-test="chain-detail" @click="emit('kpiSelect', 'scopes')">明细 →</button>
          </div>
          <strong>
            {{ formatBudgetUSD(allocated) }}
            <small>占 {{ allocationRate }}%</small>
          </strong>
          <div class="proto-progress">
            <i :style="{ width: `${allocationRate}%` }" />
          </div>
        </div>
        <div class="proto-side-divider" />
        <h4>快速操作</h4>
        <button type="button" class="proto-quick" @click="emit('kpiSelect', 'risk')">
          查看预算风险事件 <span>→</span>
        </button>
      </aside>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { BudgetItem } from '@/types/budget'
import type { TierBudgetTagItem } from '@/composables/useBudgetDisplay'
import { formatBudgetUSD, getTierTagTone } from '@/composables/useBudgetDisplay'
import type { BudgetBatchEntryMode } from '@/composables/budgetScopeFilters'
import BudgetEmptyState from './BudgetEmptyState.vue'

interface TierUsageItem {
  scope_id: string
  tier_name: string
  used_usd?: number
  used_ratio?: number
  color?: string
  enabled?: boolean
  state?: TierBudgetTagItem['state']
}

const props = withDefaults(defineProps<{
  items: BudgetItem[]
  tierItems?: TierUsageItem[]
  role: string
  riskCount?: number
  riskNote?: string
}>(), {
  tierItems: () => [],
  riskCount: 0,
  riskNote: '',
})

const emit = defineEmits<{
  drill: [item: BudgetItem]
  edit: [item: BudgetItem]
  batch: [items: BudgetItem[], mode: BudgetBatchEntryMode]
  emptyAction: []
  kpiSelect: [key: string]
}>()

function emitBatch(mode: BudgetBatchEntryMode) {
  emit('batch', selectedRows.value, mode)
}

const selectedRows = ref<BudgetItem[]>([])

const company = computed(() => props.items.find(item => item.scope_type === 'company'))
const studio = computed(() => props.items.find(item => item.scope_type === 'studio'))

const rows = computed(() => {
  if (props.role === 'admin') return props.items.filter(item => item.scope_type === 'studio')
  if (props.role === 'studio_admin') {
    return props.items.filter(item => item.scope_type === 'dept_daily')
  }
  return props.items.filter(item => item.scope_type === 'member')
})

const canSelect = computed(() => props.role === 'admin' || props.role === 'studio_admin')
const allSelected = computed(() => rows.value.length > 0 && selectedRows.value.length === rows.value.length)
const someSelected = computed(() => selectedRows.value.length > 0)

const poolItem = computed(() => (props.role === 'admin' ? company.value : studio.value))
const poolAmount = computed(() => Number(poolItem.value?.amount_usd ?? 0))
const poolUsed = computed(() => Number(poolItem.value?.used_usd ?? 0))
const allocated = computed(() => rows.value.reduce((sum, item) => sum + Math.max(Number(item.amount_usd || 0), 0), 0))
const remaining = computed(() => Math.max(poolAmount.value - allocated.value, 0))
const allocationRate = computed(() => (
  poolAmount.value > 0 ? Math.min(100, Math.round((allocated.value / poolAmount.value) * 100)) : 0
))

const childUnit = computed(() => (props.role === 'admin' ? '工作室' : props.role === 'studio_admin' ? '部门' : '成员'))
const childColumnLabel = computed(() => (props.role === 'admin' ? '工作室' : props.role === 'studio_admin' ? '部门' : '成员'))
const poolLabel = computed(() => (props.role === 'admin' ? '公司预算池' : '工作室预算池'))
const chainRootLabel = computed(() => (props.role === 'admin' ? '全公司预算' : '工作室预算'))
const tableTitle = computed(() => (props.role === 'admin' ? '工作室预算' : '部门预算'))
const tableHint = computed(() => (
  props.role === 'admin'
    ? '全部工作室的预算分配与消耗概览，可逐行点击「配置」。'
    : '日常预算与程序服务预算互不占用。'
))
const drillLabel = computed(() => (props.role === 'admin' ? '查看部门' : '查看预算'))
const emptyLevel = computed<'company' | 'studio' | 'dept'>(() => (
  props.role === 'admin' ? 'company' : props.role === 'studio_admin' ? 'studio' : 'dept'
))
const primaryEmptyAction = computed(() => {
  if (props.role === 'admin') return { label: '开始分配工作室预算', test: 'create-studio-budget' }
  if (props.role === 'studio_admin') return { label: '新增部门预算', test: 'create-department-budget' }
  return { label: undefined, test: undefined }
})
const riskNoteText = computed(() => props.riskNote || (props.riskCount > 0 ? `${props.riskCount} 个风险事件` : '暂无风险'))

function rowKey(row: BudgetItem): string {
  return `${row.scope_type}:${row.scope_id}:${row.allocation_id || ''}`
}

function isSelected(row: BudgetItem): boolean {
  const key = rowKey(row)
  return selectedRows.value.some(item => rowKey(item) === key)
}

function toggleRow(row: BudgetItem, checked: boolean): void {
  if (checked) {
    if (!isSelected(row)) selectedRows.value = [...selectedRows.value, row]
    return
  }
  selectedRows.value = selectedRows.value.filter(item => rowKey(item) !== rowKey(row))
}

function toggleAll(checked: boolean): void {
  selectedRows.value = checked ? [...rows.value] : []
}

function usagePercent(row: BudgetItem): number {
  const ratio = Number(row.used_ratio ?? 0)
  if (Number.isFinite(ratio) && ratio > 0) return Math.min(100, Math.round(ratio * 100))
  const amount = Number(row.amount_usd ?? 0)
  const used = Number(row.used_usd ?? 0)
  if (amount <= 0) return 0
  return Math.min(100, Math.round((used / amount) * 100))
}

function progressColor(ratio?: number): string {
  const value = Number(ratio ?? 0)
  if (value >= 1) return '#ef4444'
  if (value >= 0.8) return '#e6a23c'
  return '#36b34e'
}

function statusLabel(ratio?: number): string {
  const value = Number(ratio ?? 0)
  if (value >= 1) return '已超额'
  if (value >= 0.8) return '预警'
  return '正常'
}

function statusTone(ratio?: number): string {
  const value = Number(ratio ?? 0)
  if (value >= 1) return 'red'
  if (value >= 0.8) return 'orange'
  return 'green'
}

function tierTags(row: BudgetItem): TierBudgetTagItem[] {
  return props.tierItems
    .filter(item => item.scope_id === row.scope_id && (!item.scope_type || item.scope_type === row.scope_type))
    .map((item, index) => {
      const ratio = Number(item.used_ratio ?? 0)
      const used = Number(item.used_usd ?? 0)
      const state = item.state ?? (item.enabled === false ? 'disabled' : ratio >= 1 ? 'blocked' : 'normal')
      return {
        id: `${row.scope_type}-${row.scope_id}-${item.tier_name}-${index}`,
        name: item.tier_name,
        color: item.color,
        usageRatio: ratio,
        // 原型等级色按名称；仅超额覆盖为红（预警仍保留等级色）
        state,
        label: state === 'disabled' ? `${item.tier_name} 已停用` : `${item.tier_name} ${formatBudgetUSD(Number.isFinite(used) ? used : 0)}`,
      }
    })
}

function clearSelection(): void {
  selectedRows.value = []
}
</script>

<style scoped>
.proto-overview {
  display: grid;
  width: 100%;
  gap: 16px;
}

.proto-kpis {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}

.proto-kpi {
  padding: 15px 17px;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  text-align: left;
  color: inherit;
  font: inherit;
}

.proto-kpi.primary {
  border-color: #ace1b6;
  background: #f5fcf6;
}

.proto-kpi.is-clickable {
  cursor: pointer;
}

.proto-kpi.is-clickable:hover {
  border-color: #84d293;
}

.proto-kpi-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
  color: #64748b;
}

.proto-kpi-link {
  color: #36b34e;
  white-space: nowrap;
}

.proto-kpi-value {
  margin: 7px 0 4px;
  color: #0f172a;
  font-size: 23px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.proto-kpi-value.warn {
  color: #d97706;
}

.proto-kpi-value.action {
  color: #36b34e;
  font-size: 20px;
}

.proto-kpi-note {
  font-size: 11px;
  color: #94a3b8;
}

.proto-progress {
  height: 5px;
  margin-top: 8px;
  overflow: hidden;
  background: #edf1f5;
  border-radius: 5px;
}

.proto-progress > i {
  display: block;
  height: 100%;
  background: #36b34e;
  border-radius: 5px;
}

.proto-budget-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 310px;
  gap: 16px;
}

.proto-panel {
  overflow: hidden;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 14px;
}

.proto-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 17px 20px;
  border-bottom: 1px solid #f1f5f9;
}

.proto-panel-head h3 {
  margin: 0;
  color: #0f172a;
  font-size: 16px;
}

.proto-panel-head p {
  margin: 4px 0 0;
  color: #64748b;
  font-size: 12px;
}

.proto-table-wrap {
  overflow-x: auto;
}

.proto-table-empty {
  padding: 8px 0 20px;
}

.proto-native-table {
  width: 100%;
  border-collapse: collapse;
}

.proto-native-table th {
  height: 42px;
  padding: 0 12px;
  color: #64748b;
  background: #f8fafc;
  border-bottom: 1px solid #e2e8f0;
  font-size: 12px;
  font-weight: 600;
  text-align: left;
  white-space: nowrap;
}

.proto-native-table td {
  height: 52px;
  padding: 8px 12px;
  color: #334155;
  border-bottom: 1px solid #f1f5f9;
  vertical-align: middle;
}

.proto-native-table tbody tr:nth-child(even) td {
  background: #fbfcfd;
}

.proto-native-table tbody tr:last-child td {
  border-bottom: 0;
}

.proto-native-table .center {
  text-align: center;
}

.proto-native-table .right {
  text-align: right;
}

.proto-check {
  width: 15px;
  height: 15px;
  accent-color: #36b34e;
  cursor: pointer;
}

.proto-scope-name {
  color: #0f172a;
}

.proto-usage {
  display: grid;
  gap: 6px;
  min-width: 140px;
}

.proto-usage-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.proto-usage-status {
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 600;
}

.proto-usage-status.green { color: #16a34a; }
.proto-usage-status.orange { color: #d97706; }
.proto-usage-status.red { color: #dc2626; }

.mono {
  font-variant-numeric: tabular-nums;
}

.muted {
  color: #94a3b8;
  font-size: 12px;
}

.proto-tier-tag {
  display: inline-flex;
  align-items: center;
  height: 24px;
  margin: 0 4px 4px 0;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: 5px;
  font-size: 12px;
  white-space: nowrap;
}

.proto-tier-tag.purple {
  color: #7e22ce;
  background: #faf5ff;
  border-color: #e9d5ff;
}

.proto-tier-tag.blue {
  color: #2563eb;
  background: #eff6ff;
  border-color: #bfdbfe;
}

.proto-tier-tag.green {
  color: #047857;
  background: #ecfdf5;
  border-color: #a7f3d0;
}

.proto-tier-tag.orange {
  color: #b45309;
  background: #fffbeb;
  border-color: #fde68a;
}

.proto-tier-tag.red {
  color: #dc2626;
  background: #fef2f2;
  border-color: #fecaca;
}

.proto-tier-tag.gray {
  color: #64748b;
  background: #f8fafc;
  border-color: #e2e8f0;
}

.proto-tag {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: 5px;
  font-size: 12px;
  white-space: nowrap;
}

.proto-tag.green {
  color: #047857;
  background: #ecfdf5;
  border-color: #a7f3d0;
}

.proto-tag.orange {
  color: #b45309;
  background: #fffbeb;
  border-color: #fde68a;
}

.proto-tag.red {
  color: #dc2626;
  background: #fef2f2;
  border-color: #fecaca;
}

.proto-tag.gray {
  color: #64748b;
  background: #f8fafc;
  border-color: #e2e8f0;
}

.proto-btn {
  height: 32px;
  padding: 0 13px;
  color: #334155;
  background: #fff;
  border: 1px solid #d8dee8;
  border-radius: 7px;
  cursor: pointer;
  font: inherit;
}

.proto-btn.sm {
  height: 28px;
  padding: 0 10px;
  font-size: 12px;
}

.proto-btn.plain-primary {
  color: #2a9c4b;
  background: #fff;
  border-color: #84d293;
}

.proto-btn:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.proto-btn + .proto-btn {
  margin-left: 6px;
}

.proto-link {
  padding: 3px 5px;
  color: #2f9e4a;
  background: none;
  border: 0;
  cursor: pointer;
  font-size: 13px;
}

.proto-selection-bar {
  display: flex;
  align-items: center;
  gap: 9px;
  margin: 12px 14px;
  padding: 10px 12px;
  color: #26763a;
  background: #f0f9f2;
  border: 1px solid #b7e7c0;
  border-radius: 8px;
}

.proto-selection-bar .grow {
  flex: 1;
}

.proto-side {
  padding: 18px;
}

.proto-side h4 {
  margin: 0 0 12px;
  color: #64748b;
  font-size: 12px;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}

.proto-chain {
  padding: 11px 12px;
  background: #eaf7ec;
  border: 1px solid #ace1b6;
  border-radius: 10px;
}

.proto-chain.dashed {
  background: #f8fafc;
  border-style: dashed;
  border-color: #e2e8f0;
}

.proto-chain-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 13px;
  color: #334155;
}

.proto-chain strong {
  display: block;
  margin-top: 6px;
  color: #0f172a;
  font-size: 18px;
}

.proto-chain strong small {
  margin-left: 4px;
  color: #64748b;
  font-size: 12px;
  font-weight: 400;
}

.proto-chain-arrow {
  padding: 6px 0;
  color: #94a3b8;
  text-align: center;
}

.proto-side-divider {
  height: 1px;
  margin: 18px 0;
  background: #f1f5f9;
}

.proto-quick {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  height: 38px;
  margin-top: 9px;
  padding: 0 12px;
  color: #475569;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  cursor: pointer;
  font: inherit;
}

.proto-quick:hover {
  border-color: #84d293;
  color: #2a9c4b;
}

@media (max-width: 1180px) {
  .proto-kpis {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .proto-budget-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .proto-kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .proto-panel-head {
    align-items: flex-start;
    flex-direction: column;
  }

  .proto-selection-bar {
    flex-wrap: wrap;
  }

  .proto-native-table {
    min-width: 720px;
  }
}
</style>
