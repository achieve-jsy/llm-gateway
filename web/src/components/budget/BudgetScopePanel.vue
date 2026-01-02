<template>
  <div class="budget-scope-panel">
    <div class="budget-summary">
      <div class="summary-main">
        <div class="summary-eyebrow">{{ scopeType === 'studio' ? '工作室预算' : '部门预算' }}</div>
        <div class="summary-title">{{ title }}</div>
        <div class="summary-meta">预算按 USD 计费，月额度为硬限制；周/日额度用于节奏控制。</div>
      </div>
      <div class="summary-numbers">
        <div class="summary-amount">
          <strong>{{ formatUSD(totalUsed, { compact: true }) }}</strong>
          <span>/ {{ formatUSD(totalAmount, { compact: true }) }}</span>
        </div>
        <el-progress
          :percentage="Math.min(Math.round(totalRatio * 100), 100)"
          :stroke-width="8"
          :color="progressColor(totalRatio)"
        />
      </div>
    </div>

    <div class="budget-toolbar">
      <div>
        <h3>{{ tableTitle }}</h3>
        <p>{{ tableDesc }}</p>
      </div>
      <el-button :loading="loading" size="small" @click="load">刷新</el-button>
    </div>

    <div v-if="scopeType === 'dept'" class="pool-grid">
      <div v-for="pool in deptPools" :key="pool.scope_type" class="pool-card">
        <div class="pool-head">
          <div>
            <div class="pool-title">{{ SCOPE_TYPE_LABELS[pool.scope_type] }}</div>
            <div class="pool-sub">{{ pool.scope_type === 'dept_service' ? 'Service Key 走程序服务预算池' : '个人 Key 默认走成员日常预算池' }}</div>
          </div>
          <el-tag :type="statusType(pool.used_ratio || 0)" size="small">{{ statusText(pool.used_ratio || 0) }}</el-tag>
        </div>
        <div class="pool-amount">
          <strong>{{ formatUSD(pool.used_usd || 0, { compact: true }) }}</strong>
          <span>/ {{ displayAmount(pool.amount_usd) }}</span>
        </div>
        <el-progress
          :percentage="Math.min(Math.round((pool.used_ratio || 0) * 100), 100)"
          :stroke-width="7"
          :color="progressColor(pool.used_ratio || 0)"
        />
        <div class="pool-limits">
          <span>周 {{ displayAmount(pool.weekly_amount_usd) }}</span>
          <span>日 {{ displayAmount(pool.daily_amount_usd) }}</span>
        </div>
        <el-button v-if="canEdit" link type="primary" size="small" class="pool-edit" @click="openDrawer(pool)">调整额度</el-button>
        <el-button v-else link type="primary" size="small" class="pool-edit" @click="goExpansion(pool)">申请扩容</el-button>
      </div>
    </div>

    <el-table
      v-if="scopeType === 'dept'"
      v-loading="loading"
      :data="allocationRows"
      stripe
      size="small"
      class="budget-table"
      empty-text="暂无成员单独额度"
    >
      <el-table-column label="对象" min-width="180">
        <template #default="{ row }">
          <div class="scope-cell">
            <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS[row.scope_type] }" />
            <div>
              <div class="scope-name">{{ row.label || row.scope_id }}</div>
              <div class="scope-id">{{ SCOPE_TYPE_LABELS[row.scope_type] }}</div>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="月额度" width="130" align="right">
        <template #default="{ row }">{{ displayAmount(row.amount_usd) }}</template>
      </el-table-column>
      <el-table-column label="周额度" width="120" align="right">
        <template #default="{ row }">{{ displayAmount(row.weekly_amount_usd) }}</template>
      </el-table-column>
      <el-table-column label="日额度" width="120" align="right">
        <template #default="{ row }">{{ displayAmount(row.daily_amount_usd) }}</template>
      </el-table-column>
      <el-table-column label="已用 / 使用率" min-width="180">
        <template #default="{ row }">
          <div class="usage-cell">
            <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }}</span>
            <el-progress
              :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)"
              :stroke-width="6"
              :color="progressColor(row.used_ratio || 0)"
            />
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }">
          <el-tag :type="statusType(row.used_ratio || 0)" size="small">{{ statusText(row.used_ratio || 0) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100" fixed="right" align="center">
        <template #default="{ row }">
          <el-button v-if="canEdit" link type="primary" size="small" @click="openDrawer(row)">调额</el-button>
          <el-button v-else link type="primary" size="small" @click="goExpansion(row)">申请</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 工作室视图：Tab 切换（日常预算 / 服务预算） -->
    <template v-else>
      <div class="dual-table-section" v-loading="loading">
        <el-tabs v-model="poolTab" class="pool-tabs">
          <el-tab-pane name="daily">
            <template #label>
              成员日常预算
              <el-badge v-if="dailyRows.length" :value="dailyRows.length" type="info" class="tab-badge" />
            </template>
            <p class="pool-desc">个人 Key 默认走此预算池，各部门共享。</p>
            <el-table :data="dailyRows" stripe size="small" empty-text="暂无配置">
              <el-table-column label="部门" min-width="160">
                <template #default="{ row }">
                  <span class="dept-name-text">{{ row.label || row.scope_id }}</span>
                </template>
              </el-table-column>
              <el-table-column label="月额度" width="120" align="right">
                <template #default="{ row }">{{ displayAmount(row.amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="周额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.weekly_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="日额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.daily_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="使用率" min-width="150">
                <template #default="{ row }">
                  <div class="usage-cell">
                    <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }}</span>
                    <el-progress :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)" :stroke-width="6" :color="progressColor(row.used_ratio || 0)" />
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="80" align="center">
                <template #default="{ row }">
                  <el-tag :type="statusType(row.used_ratio || 0)" size="small">{{ statusText(row.used_ratio || 0) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="" width="70" align="center">
                <template #default="{ row }">
                  <el-button v-if="canEdit" link type="primary" size="small" @click="openDrawer(row)">调额</el-button>
                  <el-button v-else link type="primary" size="small" @click="goExpansion(row)">申请</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane name="service">
            <template #label>
              程序服务预算
              <el-badge v-if="serviceRows.length" :value="serviceRows.length" type="info" class="tab-badge" />
            </template>
            <p class="pool-desc">Service Key 走此预算池，与成员日常互不干扰。</p>
            <el-table :data="serviceRows" stripe size="small" empty-text="暂无配置">
              <el-table-column label="部门" min-width="160">
                <template #default="{ row }">
                  <span class="dept-name-text">{{ row.label || row.scope_id }}</span>
                </template>
              </el-table-column>
              <el-table-column label="月额度" width="120" align="right">
                <template #default="{ row }">{{ displayAmount(row.amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="周额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.weekly_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="日额度" width="100" align="right">
                <template #default="{ row }">{{ displayAmount(row.daily_amount_usd) }}</template>
              </el-table-column>
              <el-table-column label="使用率" min-width="150">
                <template #default="{ row }">
                  <div class="usage-cell">
                    <span>{{ formatUSD(row.used_usd || 0, { compact: true }) }}</span>
                    <el-progress :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)" :stroke-width="6" :color="progressColor(row.used_ratio || 0)" />
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="80" align="center">
                <template #default="{ row }">
                  <el-tag :type="statusType(row.used_ratio || 0)" size="small">{{ statusText(row.used_ratio || 0) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="" width="70" align="center">
                <template #default="{ row }">
                  <el-button v-if="canEdit" link type="primary" size="small" @click="openDrawer(row)">调额</el-button>
                  <el-button v-else link type="primary" size="small" @click="goExpansion(row)">申请</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </div>
    </template>

    <el-drawer v-model="drawerVisible" title="调整预算额度" size="380px">
      <div v-if="editingItem" class="budget-drawer">
        <div class="drawer-scope">
          <div class="scope-name">{{ editingItem.label || editingItem.scope_id }}</div>
          <div class="scope-id">{{ SCOPE_TYPE_LABELS[editingItem.scope_type] }} · {{ editingItem.scope_id }}</div>
        </div>
        <el-form label-position="top">
          <el-form-item label="月额度（USD）">
            <el-input-number v-model="editForm.amount" :min="0" :precision="2" controls-position="right" style="width:100%" />
          </el-form-item>
          <el-form-item label="周额度（USD，-1 表示不限）">
            <el-input-number v-model="editForm.weekly" :min="-1" :precision="2" controls-position="right" style="width:100%" />
          </el-form-item>
          <el-form-item label="日额度（USD，-1 表示不限）">
            <el-input-number v-model="editForm.daily" :min="-1" :precision="2" controls-position="right" style="width:100%" />
          </el-form-item>
        </el-form>
        <div class="drawer-hint">保存后预算配置立即生效；已有告警去重会随改额重置。</div>
      </div>
      <template #footer>
        <el-button @click="drawerVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getBudgetList, setBudgetBatch } from '@/services/control-plane'
import type { BudgetItem, BudgetSetPayload, BudgetView } from '@/types/budget'
import { SCOPE_TYPE_COLORS, SCOPE_TYPE_LABELS } from '@/types/budget'
import { formatUSD } from '@/utils/format'

const props = defineProps<{
  scopeType: 'studio' | 'dept'
  scopeId: string
  title: string
  childScopeIds?: string[]
  canEdit?: boolean
}>()

const router = useRouter()
const loading = ref(false)
const items = ref<BudgetItem[]>([])
const drawerVisible = ref(false)
const editingItem = ref<BudgetItem | null>(null)
const saving = ref(false)
const editForm = ref({ amount: 0, weekly: -1, daily: -1 })
const poolTab = ref('daily')

const view = computed<BudgetView>(() => props.scopeType === 'studio' ? 'studio' : 'department')

const currentItems = computed(() => {
  if (props.scopeType === 'studio') {
    return items.value.filter(i => i.scope_type === 'studio' && i.scope_id === props.scopeId)
  }
  return items.value.filter(i =>
    (i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && i.scope_id === props.scopeId
  )
})

const displayItems = computed(() => {
  if (props.scopeType === 'studio') {
    const ids = new Set(props.childScopeIds || [])
    return items.value.filter(i =>
      (i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && ids.has(i.scope_id)
    )
  }
  return items.value.filter(i =>
    ((i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && i.scope_id === props.scopeId) ||
    i.scope_type === 'member'
  )
})

const deptPools = computed(() => {
  if (props.scopeType !== 'dept') return []
  return items.value.filter(i =>
    (i.scope_type === 'dept_daily' || i.scope_type === 'dept_service') && i.scope_id === props.scopeId
  )
})

const allocationRows = computed(() => {
  if (props.scopeType === 'dept') {
    return displayItems.value.filter(i => i.scope_type === 'member')
  }
  return displayItems.value
})

// 工作室视图：按部门分组（同一 scope_id 的 dept_daily + dept_service 归为一组）
interface DeptGroup {
  deptId: string
  deptName: string
  pools: BudgetItem[]
}
const deptGroups = computed<DeptGroup[]>(() => {
  if (props.scopeType !== 'studio') return []
  const map = new Map<string, DeptGroup>()
  for (const item of displayItems.value) {
    if (item.scope_type !== 'dept_daily' && item.scope_type !== 'dept_service') continue
    const existing = map.get(item.scope_id)
    if (existing) {
      existing.pools.push(item)
    } else {
      // 从 label 中提取部门名（label 格式通常为"部门名"或"部门名·xxx"）
      const deptName = (item.label || item.scope_id).replace(/[·.].*$/, '')
      map.set(item.scope_id, { deptId: item.scope_id, deptName, pools: [item] })
    }
  }
  // 每组内按 dept_daily 在前、dept_service 在后排序
  for (const group of map.values()) {
    group.pools.sort((a, b) => (a.scope_type === 'dept_daily' ? -1 : 1))
  }
  return Array.from(map.values())
})

// 工作室视图分表：日常预算 / 服务预算
const dailyRows = computed(() => displayItems.value.filter(i => i.scope_type === 'dept_daily'))
const serviceRows = computed(() => displayItems.value.filter(i => i.scope_type === 'dept_service'))

const totalAmount = computed(() => {
  const base = currentItems.value.length ? currentItems.value : displayItems.value
  return base.reduce((sum, item) => sum + Math.max(item.amount_usd || 0, 0), 0)
})
const totalUsed = computed(() => {
  const base = currentItems.value.length ? currentItems.value : displayItems.value
  return base.reduce((sum, item) => sum + (item.used_usd || 0), 0)
})
const totalRatio = computed(() => totalAmount.value > 0 ? totalUsed.value / totalAmount.value : 0)

const tableTitle = computed(() => props.scopeType === 'studio' ? '部门预算分配' : '成员单独额度')
const tableDesc = computed(() => props.scopeType === 'studio'
  ? '在当前工作室下给各部门配置日常预算和程序服务预算。'
  : '成员默认共享部门日常预算池；只有需要单独控制时才在这里设置成员额度。'
)

async function load() {
  loading.value = true
  try {
    items.value = await getBudgetList(undefined, props.scopeType === 'dept' ? props.scopeId : undefined, view.value)
  } finally {
    loading.value = false
  }
}

function openDrawer(item: BudgetItem) {
  editingItem.value = item
  editForm.value = {
    amount: item.amount_usd,
    weekly: item.weekly_amount_usd,
    daily: item.daily_amount_usd,
  }
  drawerVisible.value = true
}

async function save() {
  if (!editingItem.value) return
  saving.value = true
  try {
    const payload: BudgetSetPayload = {
      scope_type: editingItem.value.scope_type,
      scope_id: editingItem.value.scope_id,
      amount_usd: editForm.value.amount,
      weekly_amount_usd: editForm.value.weekly,
      daily_amount_usd: editForm.value.daily,
    }
    await setBudgetBatch([payload])
    ElMessage.success('预算已更新')
    drawerVisible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

function goExpansion(item: BudgetItem) {
  router.push({
    path: '/admin/expansion-requests',
    query: {
      scope_type: item.scope_type,
      scope_id: item.scope_id,
      requested_amount: Math.max(Math.ceil((item.amount_usd || 0) * 1.2), (item.amount_usd || 0) + 100),
    },
  })
}

function displayAmount(value: number) {
  if (value == null || value < 0) return '不限'
  return formatUSD(value, { compact: true })
}

function progressColor(ratio: number) {
  if (ratio >= 1) return '#f56c6c'
  if (ratio >= 0.8) return '#e6a23c'
  return '#2BA8A2'
}

function statusType(ratio: number) {
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return 'success'
}

function statusText(ratio: number) {
  if (ratio >= 1) return '已超额'
  if (ratio >= 0.8) return '预警'
  return '正常'
}

onMounted(load)
watch(() => [props.scopeType, props.scopeId, props.childScopeIds?.join(',')], load)
</script>

<style scoped>
.budget-scope-panel { display: flex; flex-direction: column; gap: 16px; }
.budget-summary {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  padding: 18px 20px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
}
.summary-main { min-width: 0; }
.summary-eyebrow { font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 4px; }
.summary-title { font-size: 18px; font-weight: 700; color: var(--el-text-color-primary); }
.summary-meta { margin-top: 4px; font-size: 12px; color: var(--el-text-color-secondary); }
.summary-numbers { width: 260px; flex-shrink: 0; }
.summary-amount { display: flex; align-items: baseline; gap: 4px; margin-bottom: 8px; justify-content: flex-end; }
.summary-amount strong { font-size: 22px; color: var(--el-text-color-primary); }
.summary-amount span { color: var(--el-text-color-secondary); font-size: 13px; }
.budget-toolbar { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; }
.budget-toolbar h3 { margin: 0; font-size: 15px; color: var(--el-text-color-primary); }
.budget-toolbar p { margin: 4px 0 0; color: var(--el-text-color-secondary); font-size: 12px; }
.budget-table { border: 1px solid var(--el-border-color-lighter); border-radius: 10px; overflow: hidden; }
.pool-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.pool-card {
  position: relative;
  padding: 16px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-fill-color-blank);
}
.pool-head { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; }
.pool-title { font-weight: 700; color: var(--el-text-color-primary); }
.pool-sub { margin-top: 3px; font-size: 12px; color: var(--el-text-color-secondary); }
.pool-amount { display: flex; align-items: baseline; gap: 5px; margin: 14px 0 8px; }
.pool-amount strong { font-size: 22px; color: var(--el-text-color-primary); }
.pool-amount span { color: var(--el-text-color-secondary); font-size: 13px; }
.pool-limits { display: flex; gap: 12px; margin-top: 8px; color: var(--el-text-color-secondary); font-size: 12px; }
.pool-edit { position: absolute; right: 12px; bottom: 10px; }
.scope-cell { display: flex; align-items: center; gap: 10px; min-width: 0; }
.scope-dot { width: 8px; height: 8px; border-radius: 99px; flex-shrink: 0; }
.scope-name { font-weight: 600; color: var(--el-text-color-primary); }
.scope-id { margin-top: 2px; font-size: 11px; color: var(--el-text-color-secondary); }
.usage-cell { display: grid; grid-template-columns: 70px 1fr; align-items: center; gap: 10px; }
.muted { color: var(--el-text-color-placeholder); }
.budget-drawer { display: flex; flex-direction: column; gap: 18px; }
.drawer-scope { padding-bottom: 12px; border-bottom: 1px solid var(--el-border-color-lighter); }
.drawer-hint { font-size: 12px; line-height: 1.6; color: var(--el-text-color-secondary); background: var(--el-fill-color-light); padding: 10px 12px; border-radius: 8px; }

/* 按部门分组卡片 */
.dept-group-list { display: flex; flex-direction: column; gap: 16px; }
.empty-hint { text-align: center; padding: 32px; color: var(--el-text-color-placeholder); font-size: 13px; }

/* 双表格布局 */
.dual-table-section { }
.pool-tabs { }
.pool-desc { margin: 0 0 12px; font-size: 12px; color: var(--el-text-color-secondary); }
.tab-badge { margin-left: 6px; }
.dept-name-text { font-weight: 500; color: var(--el-text-color-primary); }
@media (max-width: 900px) {
  .budget-summary { flex-direction: column; }
  .summary-numbers { width: 100%; }
  .pool-grid { grid-template-columns: 1fr; }
}
</style>
