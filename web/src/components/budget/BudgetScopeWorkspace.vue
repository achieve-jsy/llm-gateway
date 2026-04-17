<template>
  <section class="budget-scope-workspace scope-stage-single">
    <div class="summary budget-panel kpi-strip">
      <div>
        <div class="summary-eye">{{ summaryEyebrow }}</div>
        <div class="summary-name">{{ scope.label || scope.id }}</div>
        <div class="summary-meta">
          {{ summaryDescription }}
        </div>
      </div>
      <div class="scope-metrics" aria-label="预算概览">
        <div v-if="showTotalMetric" class="scope-metric primary" :class="{ 'metric-with-action': scope.type === 'studio' && summaryItem }">
          <span>{{ scope.type === 'studio' ? '工作室总额' : '部门总额' }}</span>
          <strong>{{ formatBudgetUSD(summaryLimit, { unlimited: true }) }}</strong>
          <div v-if="scope.type === 'studio' && summaryItem" class="metric-actions">
            <button
              type="button"
              class="metric-action-btn secondary"
              @click="emit('detail', summaryItem)"
            >
              查看详情
            </button>
          </div>
        </div>
        <div v-if="showPersonalPoolMetric" class="scope-metric metric-with-action">
          <span>个人 Key 池</span>
          <strong>{{ formatBudgetUSD(departmentDailyPool?.amount_usd, { unlimited: true }) }}</strong>
          <div class="metric-tier-line" :title="tierSummary(departmentDailyPool)">{{ tierSummary(departmentDailyPool) }}</div>
          <div v-if="departmentDailyPool" class="metric-actions">
            <button
              type="button"
              class="metric-action-btn secondary"
              @click="emit('detail', departmentDailyPool)"
            >
              查看详情
            </button>
            <button
              v-if="!fixedPool"
              type="button"
              class="metric-action-btn primary"
              @click="handlePoolAction(departmentDailyPool)"
            >
              {{ poolActionLabel }}
            </button>
          </div>
        </div>
        <div v-if="showServicePoolMetric" class="scope-metric metric-with-action">
          <span>Service Key 池</span>
          <strong>{{ formatBudgetUSD(departmentServicePool?.amount_usd, { unlimited: true }) }}</strong>
          <div class="metric-tier-line" :title="tierSummary(departmentServicePool)">{{ tierSummary(departmentServicePool) }}</div>
          <div v-if="departmentServicePool" class="metric-actions">
            <button
              type="button"
              class="metric-action-btn secondary"
              @click="emit('detail', departmentServicePool)"
            >
              查看详情
            </button>
            <button
              v-if="!fixedPool"
              type="button"
              class="metric-action-btn primary"
              @click="handlePoolAction(departmentServicePool)"
            >
              {{ poolActionLabel }}
            </button>
          </div>
        </div>
        <div v-if="scope.type === 'studio'" class="scope-metric">
          <span>已下发</span>
          <strong>{{ formatBudgetUSD(allocatedAmount) }}</strong>
        </div>
        <div v-if="scope.type === 'studio'" class="scope-metric" :class="{ warning: overAllocatedAmount > 0 }">
          <span>{{ overAllocatedAmount > 0 ? '超分配' : '未下发' }}</span>
          <strong>{{ formatBudgetUSD(overAllocatedAmount > 0 ? overAllocatedAmount : unallocatedAmount) }}</strong>
        </div>
        <div class="scope-metric usage">
          <span>已用</span>
          <strong>{{ formatBudgetUSD(summaryUsed) }}</strong>
          <el-progress
            :percentage="summaryPercent"
            :stroke-width="4"
            :status="summaryPercent >= 100 ? 'exception' : summaryPercent >= 80 ? 'warning' : 'success'"
            :show-text="false"
          />
        </div>
        <div v-if="scope.type === 'studio'" class="scope-metric compact">
          <span>下属部门</span>
          <strong>{{ visibleRows.length }}</strong>
        </div>
      </div>
    </div>

    <template v-if="scope.type === 'studio'">
      <div class="scope-toolbar">
        <div>
          <strong>部门预算分配</strong>
        </div>
      </div>

      <div class="budget-panel">
        <div class="pool-tabs" role="tablist" aria-label="部门预算池">
          <button
            type="button"
            class="pool-tab"
            :class="{ active: activePool === 'daily' }"
            data-test="pool-tab-daily"
            role="tab"
            :aria-selected="activePool === 'daily'"
            @click="activePool = 'daily'"
          >
            成员日常预算
          </button>
          <button
            type="button"
            class="pool-tab"
            :class="{ active: activePool === 'service' }"
            data-test="pool-tab-service"
            role="tab"
            :aria-selected="activePool === 'service'"
            @click="activePool = 'service'"
          >
            程序服务预算
          </button>
        </div>

        <div v-if="visibleRows.length" class="proto-table-wrap">
          <table class="proto-native-table">
            <thead>
              <tr>
                <th class="center" style="width:42px">
                  <input
                    class="proto-check"
                    type="checkbox"
                    :checked="allSelected"
                    :indeterminate.prop="someSelected && !allSelected"
                    aria-label="全选部门"
                    @change="toggleAll(($event.target as HTMLInputElement).checked)"
                  >
                </th>
                <th>部门</th>
                <th class="right">月额度</th>
                <th>周期限制</th>
                <th>使用率</th>
                <th>等级预算</th>
                <th class="center">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in visibleRows" :key="row.allocation_id || row.scope_id">
                <td class="center">
                  <input
                    class="proto-check"
                    type="checkbox"
                    :checked="isSelected(row)"
                    :aria-label="`选择 ${row.label || row.scope_id}`"
                    @change="toggleRow(row, ($event.target as HTMLInputElement).checked)"
                  >
                </td>
                <td>
                  <strong class="proto-scope-name" :data-test="`pool-row-${row.allocation_id || row.scope_id}`">{{ row.label || row.scope_id }}</strong>
                </td>
                <td class="right mono">{{ formatBudgetUSD(row.amount_usd, { unlimited: true }) }}</td>
                <td>
                  <span class="period-limit">周 {{ formatBudgetUSD(row.weekly_amount_usd, { unlimited: true }) }}</span>
                  <span class="period-limit">日 {{ formatBudgetUSD(row.daily_amount_usd, { unlimited: true }) }}</span>
                </td>
                <td>
                  <div class="proto-usage">
                    <span class="mono">{{ formatBudgetUSD(row.used_usd) }} / {{ formatBudgetUSD(row.amount_usd, { unlimited: true }) }}</span>
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
                      <span class="proto-usage-status" :class="statusTone(row.used_ratio)">{{ statusLabel(row.used_ratio) }}</span>
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
                  <button type="button" class="proto-btn sm plain-primary" @click="emit('drill', row)">查看预算</button>
                  <button type="button" class="proto-btn sm" @click="emit('edit', row)">配置</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="proto-table-empty">
            <BudgetEmptyState
              level="studio"
            />
        </div>
      </div>
    </template>

    <template v-else>
      <div class="budget-panel">
        <div v-if="!fixedPool" class="pool-tabs" role="tablist" aria-label="部门预算明细类型">
          <button
            type="button"
            class="pool-tab"
            :class="{ active: activePool === 'daily' }"
            data-test="dept-detail-tab-personal"
            role="tab"
            :aria-selected="activePool === 'daily'"
            @click="activePool = 'daily'"
          >
            个人 Key 使用明细
          </button>
          <button
            type="button"
            class="pool-tab"
            :class="{ active: activePool === 'service' }"
            data-test="dept-detail-tab-service"
            role="tab"
            :aria-selected="activePool === 'service'"
            @click="activePool = 'service'"
          >
            Service Key 使用明细
          </button>
        </div>

        <template v-if="detailPool === 'daily'">
          <div class="budget-panel__header">
            <div>
              <strong>成员额度</strong>
              <p>默认走部门个人 Key 共享池；需要单独限制时再设独立额度。</p>
            </div>
            <span v-if="memberRows.length" class="member-count">{{ memberRows.length }} 人</span>
          </div>
          <div v-if="membersLoading" class="member-empty-state" data-test="dept-members-loading">
            <p>正在加载部门成员…</p>
          </div>
          <div v-else-if="memberRows.length === 0" class="member-empty-state" data-test="dept-members-empty">
            <strong>暂无成员</strong>
            <p>该部门下还没有成员；成员加入后会出现在此列表，并可单独配置额度。</p>
          </div>
          <div v-else class="proto-table-wrap">
            <table class="proto-native-table member-table" data-test="dept-members-table">
              <thead>
                <tr>
                  <th>成员</th>
                  <th>预算来源</th>
                  <th>等级预算</th>
                  <th class="center">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="row in memberRows"
                  :key="row.allocation_id || row.scope_id"
                  :class="{ 'is-shared-pool': row.isDefaultPool }"
                >
                  <td><strong class="proto-scope-name">{{ row.label || row.scope_id }}</strong></td>
                  <td>
                    <span v-if="row.isDefaultPool" class="proto-tag gray">共享预算池</span>
                    <span v-else class="proto-tag">个人独立额度</span>
                  </td>
                  <td>
                    <template v-if="!row.isDefaultPool && tierTags(row).length">
                      <span
                        v-for="tag in tierTags(row)"
                        :key="tag.id"
                        class="proto-tier-tag"
                        :class="getTierTagTone(tag.name, tag.state)"
                      >
                        {{ tag.label }}
                      </span>
                    </template>
                    <span v-else-if="row.isDefaultPool" class="muted">—</span>
                    <span v-else class="muted">未配置子预算</span>
                  </td>
                  <td class="center">
                    <button
                      type="button"
                      class="proto-btn sm"
                      :data-test="row.isDefaultPool ? 'member-set-quota' : 'member-edit-quota'"
                      @click="emit('edit', row)"
                    >
                      {{ row.isDefaultPool ? '设置额度' : '配置配额' }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>

        <template v-else>
          <div class="budget-panel__header">
            <div>
              <strong>Service Key 独立额度</strong>
              <p>服务、自动化任务默认使用 Service Key 预算池；独立额度适合高风险或高频服务。</p>
            </div>
          </div>
          <div class="member-empty-state">
            <strong>暂无 Service Key 独立额度</strong>
            <p>当前服务调用默认使用 Service Key 预算池；需要单独限制某个 Service Key 时再配置独立额度。</p>
          </div>
        </template>
        </div>
    </template>

    <div v-if="selectedRows.length" class="selection-bar show">
      <BudgetSelectionBar
        :count="selectedRows.length"
        :remaining-text="`当前剩余可分配 ${formatBudgetUSD(remaining)}`"
        @clear="clearSelection"
      >
        <template #actions>
          <el-button size="small" plain data-test="batch-absolute" @click="emit('batch', selectedRows, 'absolute')">固定额度</el-button>
          <el-button size="small" plain data-test="batch-percentage" @click="emit('batch', selectedRows, 'percentage')">按比例增减</el-button>
          <el-button size="small" plain data-test="batch-tier" @click="emit('batch', selectedRows, 'tier')">等级子预算</el-button>
        </template>
      </BudgetSelectionBar>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { BudgetItem } from '@/types/budget'
import type { TierBudgetTagItem } from '@/composables/useBudgetDisplay'
import { formatBudgetUSD, getTierTagTone } from '@/composables/useBudgetDisplay'
import type { BudgetBatchEntryMode } from '@/composables/budgetScopeFilters'
import { useOrgStore, type DeptMember } from '@/stores/org'
import BudgetEmptyState from './BudgetEmptyState.vue'
import BudgetSelectionBar from './BudgetSelectionBar.vue'
import TierBudgetTags from './TierBudgetTags.vue'

interface TierUsageItem {
  scope_id: string
  scope_type?: BudgetItem['scope_type']
  tier_name: string
  used_ratio?: number
  color?: string
  enabled?: boolean
  state?: TierBudgetTagItem['state']
}

/** 部门成员行：已配额合并预算；未配额标 isDefaultPool，走共享池 */
export interface MemberBudgetRow extends BudgetItem {
  isDefaultPool?: boolean
}

const props = withDefaults(defineProps<{
  scope: { type: 'studio' | 'dept'; id: string; label?: string }
  pool?: 'daily' | 'service'
  items: BudgetItem[]
  tierItems?: TierUsageItem[]
  role: string
  remaining?: number
}>(), {
  tierItems: () => [],
  remaining: 0,
})

const emit = defineEmits<{
  drill: [item: BudgetItem]
  edit: [item: BudgetItem | null]
  detail: [item: BudgetItem]
  requestExpansion: [item: BudgetItem]
  batch: [items: BudgetItem[], mode: BudgetBatchEntryMode]
}>()

const orgStore = useOrgStore()
const activePool = ref<'daily' | 'service'>('daily')
const selectedRows = ref<BudgetItem[]>([])
const deptMembers = ref<DeptMember[]>([])
const membersLoading = ref(false)
const fixedPool = computed(() => props.scope.type === 'dept' && !!props.pool)
const detailPool = computed<'daily' | 'service'>(() => props.pool ?? activePool.value)
const summaryEyebrow = computed(() => {
  if (props.scope.type === 'studio') return '工作室预算'
  if (props.pool === 'daily') return '个人 Key 预算详情'
  if (props.pool === 'service') return 'Service Key 预算详情'
  return '部门预算'
})
const summaryDescription = computed(() => {
  if (props.scope.type === 'studio') return '下级部门额度总和不可超过工作室月额度。'
  if (props.pool === 'daily') return '成员通过个人 Key 调用时共享该预算池。'
  if (props.pool === 'service') return '程序、自动化和服务账号调用时使用该预算池。'
  return '个人 Key 与 Service Key 分池管理，互不占用。'
})
const showTotalMetric = computed(() => props.scope.type !== 'dept' || !fixedPool.value)
const showPersonalPoolMetric = computed(() => props.scope.type === 'dept' && (!props.pool || props.pool === 'daily'))
const showServicePoolMetric = computed(() => props.scope.type === 'dept' && (!props.pool || props.pool === 'service'))
const canConfigureDepartmentPools = computed(() => !(props.role === 'dept_admin' && props.scope.type === 'dept'))
const poolActionLabel = computed(() => canConfigureDepartmentPools.value ? '配置' : '申请扩容')

const visibleRows = computed(() => props.items.filter(item =>
  item.scope_type === (activePool.value === 'daily' ? 'dept_daily' : 'dept_service')
  && (!item.parent_scope_id || item.parent_scope_id === props.scope.id),
))
const budgetMemberMap = computed(() => {
  const map = new Map<string, BudgetItem>()
  for (const item of props.items) {
    if (item.scope_type !== 'member') continue
    if (item.parent_scope_id && item.parent_scope_id !== props.scope.id) continue
    map.set(item.scope_id, item)
  }
  return map
})

const memberRows = computed<MemberBudgetRow[]>(() => {
  if (props.scope.type !== 'dept') return []

  if (deptMembers.value.length === 0) {
    // 成员列表未加载到时，回退为仅展示已有独立额度
    return [...budgetMemberMap.value.values()].map(item => ({ ...item, isDefaultPool: false }))
  }

  return deptMembers.value.map<MemberBudgetRow>((member) => {
    const existing = budgetMemberMap.value.get(member.id)
    if (existing) {
      return { ...existing, label: existing.label || member.name, isDefaultPool: false }
    }
    return {
      scope_type: 'member',
      scope_id: member.id,
      parent_scope_id: props.scope.id,
      label: member.name,
      amount_usd: 0,
      weekly_amount_usd: -1,
      daily_amount_usd: -1,
      used_usd: 0,
      used_ratio: 0,
      period: '',
      enabled: true,
      isDefaultPool: true,
    }
  })
})

async function loadAllDeptMembers(deptId: string) {
  membersLoading.value = true
  try {
    const pageSize = 100
    let page = 1
    const all: DeptMember[] = []
    while (true) {
      const { members, total } = await orgStore.fetchDeptMembers(deptId, page, pageSize)
      all.push(...members)
      if (all.length >= total || members.length === 0) break
      page++
    }
    deptMembers.value = all
  } catch {
    deptMembers.value = []
  } finally {
    membersLoading.value = false
  }
}

watch(
  () => [props.scope.type, props.scope.id] as const,
  ([type, id]) => {
    if (type === 'dept' && id) {
      void loadAllDeptMembers(id)
    } else {
      deptMembers.value = []
    }
  },
  { immediate: true },
)
const summaryItem = computed(() => props.items.find(item =>
  item.scope_type === 'studio' && item.scope_id === props.scope.id,
) ?? null)
const departmentDailyPool = computed(() => props.items.find(item => item.scope_type === 'dept_daily' && item.scope_id === props.scope.id) ?? null)
const departmentServicePool = computed(() => props.items.find(item => item.scope_type === 'dept_service' && item.scope_id === props.scope.id) ?? null)
const departmentPools = computed(() => [
  {
    key: 'daily',
    title: '个人 Key 预算池',
    note: '成员通过个人 Key 调用时共享',
    item: departmentDailyPool.value,
  },
  {
    key: 'service',
    title: 'Service Key 预算池',
    note: '程序、自动化和服务账号调用时使用',
    item: departmentServicePool.value,
  },
])
const summaryUsed = computed(() => {
  if (props.scope.type === 'studio') return Number(summaryItem.value?.used_usd ?? 0)
  if (props.pool === 'daily') return Number(departmentDailyPool.value?.used_usd ?? 0)
  if (props.pool === 'service') return Number(departmentServicePool.value?.used_usd ?? 0)
  return departmentPools.value.reduce((sum, pool) => sum + Number(pool.item?.used_usd ?? 0), 0)
})
const summaryLimit = computed(() => props.scope.type === 'studio'
  ? Number(summaryItem.value?.amount_usd ?? 0)
  : departmentPools.value.reduce((sum, pool) => sum + Math.max(Number(pool.item?.amount_usd ?? 0), 0), 0))
const allocatedAmount = computed(() => visibleRows.value.reduce((sum, item) => (
  sum + Math.max(Number(item.amount_usd ?? 0), 0)
), 0))
const unallocatedAmount = computed(() => Math.max(Number(summaryLimit.value || 0) - allocatedAmount.value, 0))
const summaryUsageLimit = computed(() => {
  if (props.pool === 'daily') return Number(departmentDailyPool.value?.amount_usd ?? 0)
  if (props.pool === 'service') return Number(departmentServicePool.value?.amount_usd ?? 0)
  return summaryLimit.value
})
const summaryPercent = computed(() => summaryUsageLimit.value > 0
  ? Math.min(100, Math.round(summaryUsed.value / summaryUsageLimit.value * 100))
  : 0)
const overAllocatedAmount = computed(() => Math.max(allocatedAmount.value - Number(summaryLimit.value || 0), 0))
const allSelected = computed(() => visibleRows.value.length > 0 && selectedRows.value.length === visibleRows.value.length)
const someSelected = computed(() => selectedRows.value.length > 0 && selectedRows.value.length < visibleRows.value.length)

function tierTags(row: BudgetItem): TierBudgetTagItem[] {
  return props.tierItems
    .filter(item => item.scope_id === row.scope_id && (!item.scope_type || item.scope_type === row.scope_type))
    .map((item, index) => {
      const ratio = Number(item.used_ratio ?? 0)
      const state = item.state ?? (item.enabled === false ? 'disabled' : ratio >= 1 ? 'blocked' : ratio >= 0.8 ? 'warning' : 'normal')
      return {
        id: `${row.scope_type}-${row.scope_id}-${item.tier_name}-${index}`,
        name: item.tier_name,
        color: item.color,
        usageRatio: ratio,
        state,
        label: state === 'disabled' ? `${item.tier_name} 已停用` : `${item.tier_name} ${Math.round(ratio * 100)}%`,
      }
    })
}

function tierSummary(row?: BudgetItem | null): string {
  if (!row) return '等级子预算未配置'
  const tags = tierTags(row)
  if (!tags.length) return '等级子预算未配置'
  return tags.slice(0, 3).map(tag => tag.label).join(' · ')
}

function handlePoolAction(row: BudgetItem): void {
  if (canConfigureDepartmentPools.value) {
    emit('edit', row)
    return
  }
  emit('requestExpansion', row)
}

function clearSelection(): void {
  selectedRows.value = []
}

function rowKey(row: BudgetItem): string {
  return row.allocation_id || `${row.scope_type}:${row.scope_id}`
}

function isSelected(row: BudgetItem): boolean {
  const key = rowKey(row)
  return selectedRows.value.some(item => rowKey(item) === key)
}

function toggleRow(row: BudgetItem, checked: boolean): void {
  const key = rowKey(row)
  selectedRows.value = checked
    ? [...selectedRows.value.filter(item => rowKey(item) !== key), row]
    : selectedRows.value.filter(item => rowKey(item) !== key)
}

function toggleAll(checked: boolean): void {
  selectedRows.value = checked ? [...visibleRows.value] : []
}

function usagePercent(row: BudgetItem): number {
  return Math.min(100, Math.round((row.used_ratio || 0) * 100))
}

function statusTone(ratio = 0): 'green' | 'orange' | 'red' {
  if (ratio >= 1) return 'red'
  if (ratio >= 0.8) return 'orange'
  return 'green'
}

function statusLabel(ratio = 0): string {
  if (ratio >= 1) return '已超额'
  if (ratio >= 0.8) return '预警'
  return '正常'
}

function progressColor(ratio = 0): string {
  if (ratio >= 1) return '#ef4444'
  if (ratio >= 0.8) return '#e6a23c'
  return '#36b34e'
}
</script>

<style scoped>
.budget-scope-workspace {
  display: grid;
  gap: var(--space-4);
}

.summary,
.scope-toolbar,
.pool-tabs,
.pool-grid {
  display: flex;
}

.summary {
  padding: var(--space-4);
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
}

.summary-eye,
.page-sub,
.pool-sub {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.summary-name {
  margin: var(--space-1) 0;
  color: var(--text-primary);
  font-size: var(--font-size-title);
  font-weight: var(--font-weight-bold);
}

.summary-meta {
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.summary-amount {
  min-width: 220px;
  text-align: right;
}

.summary-amount strong {
  color: var(--text-primary);
  font-size: var(--font-size-title);
}

.scope-metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(132px, 1fr));
  gap: 1px;
  min-width: min(680px, 58%);
  overflow: hidden;
  border: 1px solid var(--budget-row-border);
  border-radius: 10px;
  background: var(--budget-row-border);
}

.scope-metric {
  display: grid;
  gap: 5px;
  min-width: 0;
  padding: 12px 14px;
  background: #fff;
}

.scope-metric span {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.scope-metric strong {
  color: var(--text-primary);
  font-size: 17px;
  font-weight: var(--font-weight-bold);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.metric-tier-line {
  overflow: hidden;
  max-width: 100%;
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.scope-metric.primary {
  background: #f5fcf6;
}

.scope-metric.primary.metric-with-action {
  align-content: start;
}

.scope-metric.warning strong {
  color: var(--el-color-warning);
}

.scope-metric.usage {
  min-width: 150px;
}

.scope-metric.compact {
  min-width: 86px;
}

.scope-metric.metric-with-action {
  align-content: start;
}

.metric-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
}

.metric-action-btn {
  min-height: 26px;
  padding: 0 9px;
  border-radius: 6px;
  cursor: pointer;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  line-height: 24px;
  white-space: nowrap;
}

.metric-action-btn.primary {
  color: #fff;
  background: var(--brand-green);
  border: 1px solid var(--brand-green);
}

.metric-action-btn.secondary {
  color: var(--brand-green-text);
  background: #fff;
  border: 1px solid var(--brand-green-light);
}

.metric-action-btn:hover {
  filter: brightness(0.97);
}

.scope-toolbar {
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.actions,
.pool-tabs {
  gap: var(--space-2);
}

.pool-tabs {
  padding: var(--space-2) var(--space-3) 0;
  border-bottom: 1px solid var(--budget-row-border);
}

.pool-tab {
  padding: var(--space-2) var(--space-3);
  color: var(--text-secondary);
  background: transparent;
  border: 0;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  font: inherit;
}

.pool-tab.active {
  color: var(--brand-green-text);
  border-bottom-color: var(--brand-green);
  font-weight: var(--font-weight-semibold);
}

.proto-table-wrap {
  overflow-x: auto;
}

.proto-table-empty {
  padding: 8px 0 20px;
}

.proto-native-table {
  width: 100%;
  min-width: 840px;
  border-collapse: collapse;
}

.proto-native-table.member-table {
  min-width: 640px;
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

.proto-progress {
  height: 5px;
  min-width: 80px;
  overflow: hidden;
  border-radius: 5px;
  background: #edf1f5;
  flex: 1;
}

.proto-progress > i {
  display: block;
  height: 100%;
  border-radius: 5px;
  background: #36b34e;
}

.proto-usage-status {
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 600;
}

.proto-usage-status.green { color: #16a34a; }
.proto-usage-status.orange { color: #d97706; }
.proto-usage-status.red { color: #dc2626; }

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

.proto-tier-tag.purple { color: #7e22ce; background: #faf5ff; border-color: #e9d5ff; }
.proto-tier-tag.blue { color: #2563eb; background: #eff6ff; border-color: #bfdbfe; }
.proto-tier-tag.green { color: #047857; background: #ecfdf5; border-color: #a7f3d0; }
.proto-tier-tag.orange { color: #b45309; background: #fffbeb; border-color: #fde68a; }
.proto-tier-tag.red { color: #dc2626; background: #fef2f2; border-color: #fecaca; }
.proto-tier-tag.gray { color: #64748b; background: #f8fafc; border-color: #e2e8f0; }

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

.proto-btn + .proto-btn {
  margin-left: 6px;
}

.usage-cell {
  display: grid;
  gap: var(--space-1);
}

.period-limit {
  display: inline-flex;
  margin-right: 10px;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  white-space: nowrap;
}

.pool-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-3);
}

.pool {
  padding: var(--space-4);
}

.pool-title {
  color: var(--text-primary);
  font-weight: var(--font-weight-semibold);
}

.pool-amount-big {
  margin: var(--space-3) 0;
  color: var(--text-primary);
  font-size: var(--font-size-title);
  font-weight: var(--font-weight-bold);
}

.member-empty-state {
  display: grid;
  min-height: 128px;
  place-items: center;
  align-content: center;
  gap: 6px;
  color: var(--text-regular);
  text-align: center;
}

.member-empty-state strong {
  color: var(--text-primary);
}

.member-empty-state p {
  max-width: 520px;
  margin: 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.budget-panel__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-3);
}

.budget-panel__header .member-count {
  flex-shrink: 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.budget-panel__header p {
  margin: 4px 0 0;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.link {
  padding: 0;
  color: var(--brand-green-text);
  background: transparent;
  border: 0;
  cursor: pointer;
}

@media (max-width: 760px) {
  .summary,
  .scope-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .summary-amount,
  .scope-metrics {
    min-width: 0;
    width: 100%;
  }

  .scope-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .pool-grid {
    grid-template-columns: 1fr;
  }
}
</style>
