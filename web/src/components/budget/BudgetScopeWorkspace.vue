<template>
  <section class="budget-scope-workspace scope-stage-single">
    <div class="summary budget-panel kpi-strip">
      <div>
        <div class="summary-eye">{{ scope.type === 'studio' ? '工作室预算' : '部门预算' }}</div>
        <div class="summary-name">{{ scope.label || scope.id }}</div>
        <div class="summary-meta">
          {{ scope.type === 'studio'
            ? '下级部门额度总和不可超过工作室月额度。'
            : '成员默认共享日常预算池；仅特殊成员设置独立额度。' }}
        </div>
      </div>
      <div class="summary-amount">
        <strong>{{ formatBudgetUSD(summaryUsed) }}</strong>
        <span> / {{ formatBudgetUSD(summaryLimit, { unlimited: true }) }}</span>
        <el-progress
          :percentage="summaryPercent"
          :stroke-width="5"
          :status="summaryPercent >= 100 ? 'exception' : summaryPercent >= 80 ? 'warning' : 'success'"
          :show-text="false"
        />
      </div>
    </div>

    <BudgetPermissionBanner :role="role" :scope-type="scope.type">
      <template #actions>
        <el-button size="small" @click="emit('edit', summaryItem)">配置当前预算</el-button>
      </template>
    </BudgetPermissionBanner>

    <template v-if="scope.type === 'studio'">
      <div class="scope-toolbar">
        <div>
          <strong>部门预算分配</strong>
          <div class="page-sub">日常预算与程序服务预算互不占用。</div>
        </div>
        <div class="actions">
          <el-button size="small" :disabled="selectedRows.length === 0" @click="emit('batch', selectedRows, 'absolute')">
            批量调整
          </el-button>
          <el-button
            v-if="canCreateDepartment"
            size="small"
            type="primary"
            data-test="create-department-budget"
            @click="emitCreateDepartment"
          >
            新增部门预算
          </el-button>
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

        <el-table
          ref="tableRef"
          :data="visibleRows"
          stripe
          size="small"
          @selection-change="selectedRows = $event"
        >
          <el-table-column type="selection" width="42" />
          <el-table-column label="部门" min-width="130">
            <template #default="{ row }">
              <strong :data-test="`pool-row-${row.allocation_id || row.scope_id}`">{{ row.label || row.scope_id }}</strong>
            </template>
          </el-table-column>
          <el-table-column label="月额度" min-width="100" align="right">
            <template #default="{ row }"><span class="mono">{{ formatBudgetUSD(row.amount_usd, { unlimited: true }) }}</span></template>
          </el-table-column>
          <el-table-column label="周额度" min-width="90" align="right">
            <template #default="{ row }"><span class="mono">{{ formatBudgetUSD(row.weekly_amount_usd, { unlimited: true }) }}</span></template>
          </el-table-column>
          <el-table-column label="日额度" min-width="90" align="right">
            <template #default="{ row }"><span class="mono">{{ formatBudgetUSD(row.daily_amount_usd, { unlimited: true }) }}</span></template>
          </el-table-column>
          <el-table-column label="使用率" min-width="150">
            <template #default="{ row }">
              <div class="usage-cell">
                <span>{{ Math.round((row.used_ratio || 0) * 100) }}%</span>
                <el-progress :percentage="Math.min(100, Math.round((row.used_ratio || 0) * 100))" :stroke-width="4" :show-text="false" />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="等级预算" min-width="160">
            <template #default="{ row }">
              <TierBudgetTags :items="tierTags(row.scope_id)" />
            </template>
          </el-table-column>
          <el-table-column label="操作" width="180" align="center">
            <template #default="{ row }">
              <el-button size="small" type="primary" plain @click="emit('drill', row)">查看预算</el-button>
              <el-button size="small" @click="emit('edit', row)">配置</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <BudgetEmptyState
              level="studio"
              :action-label="canCreateDepartment ? '新增部门预算' : undefined"
              :action-test="canCreateDepartment ? 'create-department-budget-empty' : undefined"
              @action="emitCreateDepartment"
            />
          </template>
        </el-table>
      </div>
    </template>

    <template v-else>
      <div class="pool-grid">
        <article v-for="pool in departmentPools" :key="pool.key" class="pool budget-panel">
          <div class="pool-head">
            <div>
              <div class="pool-title">{{ pool.title }}</div>
              <div class="pool-sub">{{ pool.note }}</div>
            </div>
          </div>
          <div class="pool-amount-big">{{ formatBudgetUSD(pool.item?.amount_usd, { unlimited: true }) }}</div>
          <button v-if="pool.item" type="button" class="link" @click="emit('edit', pool.item)">调整额度与等级限制</button>
        </article>
      </div>
      <div class="budget-panel">
        <div class="budget-panel__header"><strong>成员独立额度</strong></div>
        <BudgetEmptyState v-if="memberRows.length === 0" level="dept" />
        <el-table v-else :data="memberRows" stripe size="small">
          <el-table-column label="成员" prop="label" min-width="140" />
          <el-table-column label="预算来源" min-width="130"><template #default>个人独立额度</template></el-table-column>
          <el-table-column label="等级预算" min-width="160"><template #default="{ row }"><TierBudgetTags :items="tierTags(row.scope_id)" /></template></el-table-column>
          <el-table-column label="操作" width="150"><template #default="{ row }"><el-button size="small" @click="emit('edit', row)">配置配额</el-button></template></el-table-column>
        </el-table>
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
import { computed, ref } from 'vue'
import type { BudgetItem, BudgetScopeType } from '@/types/budget'
import type { TierBudgetTagItem } from '@/composables/useBudgetDisplay'
import { formatBudgetUSD } from '@/composables/useBudgetDisplay'
import type { BudgetBatchEntryMode } from '@/composables/budgetScopeFilters'
import BudgetEmptyState from './BudgetEmptyState.vue'
import BudgetPermissionBanner from './BudgetPermissionBanner.vue'
import BudgetSelectionBar from './BudgetSelectionBar.vue'
import TierBudgetTags from './TierBudgetTags.vue'

interface TierUsageItem {
  scope_id: string
  tier_name: string
  used_ratio?: number
  color?: string
}

const props = withDefaults(defineProps<{
  scope: { type: 'studio' | 'dept'; id: string; label?: string }
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
  batch: [items: BudgetItem[], mode: BudgetBatchEntryMode]
  createDepartment: [payload: { scopeType: Extract<BudgetScopeType, 'dept_daily' | 'dept_service'>; studioId: string }]
}>()

const activePool = ref<'daily' | 'service'>('daily')
const selectedRows = ref<BudgetItem[]>([])
const tableRef = ref<{ clearSelection?: () => void } | null>(null)

const visibleRows = computed(() => props.items.filter(item =>
  item.scope_type === (activePool.value === 'daily' ? 'dept_daily' : 'dept_service')
  && (!item.parent_scope_id || item.parent_scope_id === props.scope.id),
))
const memberRows = computed(() => props.items.filter(item =>
  item.scope_type === 'member' && (!item.parent_scope_id || item.parent_scope_id === props.scope.id),
))
const summaryItem = computed(() => props.items.find(item =>
  item.scope_type === 'studio' && item.scope_id === props.scope.id,
) ?? null)
const departmentPools = computed(() => [
  {
    key: 'daily',
    title: '成员日常预算池',
    note: '个人 Key 默认共享',
    item: props.items.find(item => item.scope_type === 'dept_daily' && item.scope_id === props.scope.id),
  },
  {
    key: 'service',
    title: '程序服务预算池',
    note: 'Service Key 使用',
    item: props.items.find(item => item.scope_type === 'dept_service' && item.scope_id === props.scope.id),
  },
])
const summaryUsed = computed(() => props.scope.type === 'studio'
  ? Number(summaryItem.value?.used_usd ?? 0)
  : departmentPools.value.reduce((sum, pool) => sum + Number(pool.item?.used_usd ?? 0), 0))
const summaryLimit = computed(() => props.scope.type === 'studio'
  ? Number(summaryItem.value?.amount_usd ?? 0)
  : departmentPools.value.reduce((sum, pool) => sum + Math.max(Number(pool.item?.amount_usd ?? 0), 0), 0))
const summaryPercent = computed(() => summaryLimit.value > 0
  ? Math.min(100, Math.round(summaryUsed.value / summaryLimit.value * 100))
  : 0)
const canCreateDepartment = computed(() => props.role === 'admin' || props.role === 'studio_admin')

function tierTags(scopeId: string): TierBudgetTagItem[] {
  return props.tierItems
    .filter(item => item.scope_id === scopeId)
    .map((item, index) => {
      const ratio = Number(item.used_ratio ?? 0)
      return {
        id: `${scopeId}-${item.tier_name}-${index}`,
        name: item.tier_name,
        color: item.color,
        usageRatio: ratio,
        state: ratio >= 1 ? 'blocked' : ratio >= 0.8 ? 'warning' : 'normal',
        label: `${item.tier_name} ${Math.round(ratio * 100)}%`,
      }
    })
}

function emitCreateDepartment(): void {
  emit('createDepartment', {
    scopeType: activePool.value === 'daily' ? 'dept_daily' : 'dept_service',
    studioId: props.scope.id,
  })
}

function clearSelection(): void {
  tableRef.value?.clearSelection?.()
  selectedRows.value = []
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

.usage-cell {
  display: grid;
  gap: var(--space-1);
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

  .summary-amount {
    min-width: 0;
    text-align: left;
  }

  .pool-grid {
    grid-template-columns: 1fr;
  }
}
</style>
