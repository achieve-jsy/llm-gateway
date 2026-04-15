<template>
  <main class="budget-page expansion-workbench">
    <header class="workbench-header">
      <div>
        <h1>{{ pageTitle }}</h1>
        <p>{{ pageDescription }}</p>
      </div>
      <div class="workbench-header__actions">
        <el-button :loading="loading" @click="loadRows">刷新</el-button>
        <el-button v-if="canCreateRequest" type="primary" @click="createVisible = true">新建申请</el-button>
      </div>
    </header>

    <nav class="approval-tabs" aria-label="扩容申请视图">
      <button
        v-for="tab in availableTabs"
        :key="tab.value"
        :class="{ active: activeTab === tab.value }"
        type="button"
        @click="activeTab = tab.value"
      >
        <span :data-test="`approval-tab-${tab.value}`">{{ tab.label }}</span>
        <span v-if="tab.value === 'pending' && pendingRows.length" class="tab-count">
          {{ pendingRows.length }}
        </span>
      </button>
    </nav>

    <section class="approval-kpis" aria-label="申请摘要">
      <div>
        <span>{{ activeTabLabel }}</span>
        <strong>{{ sourceRows.length }}</strong>
      </div>
      <div>
        <span>紧急申请</span>
        <strong class="tone-danger">{{ urgentCount }}</strong>
      </div>
      <div>
        <span>待重新确认</span>
        <strong class="tone-warning">{{ staleCount }}</strong>
      </div>
      <div>
        <span>筛选结果</span>
        <strong>{{ filteredRows.length }}</strong>
      </div>
    </section>

    <section class="budget-panel">
      <div class="budget-filter-bar approval-filters">
        <el-select
          v-model="filter.dimension"
          data-test="approval-filter-dimension"
          aria-label="申请维度"
        >
          <el-option label="全部申请维度" value="all" />
          <el-option label="总预算扩容" value="total" />
          <el-option label="模型等级额度" value="tier" />
        </el-select>
        <el-select
          v-model="filter.modelTierId"
          data-test="approval-filter-tier"
          aria-label="模型等级"
        >
          <el-option label="全部模型等级" value="all" />
          <el-option v-for="tier in tiers" :key="tier.id" :label="tier.name" :value="tier.id" />
        </el-select>
        <el-select
          v-model="filter.duration"
          data-test="approval-filter-duration"
          aria-label="调整类型"
        >
          <el-option label="全部类型" value="all" />
          <el-option label="长期调整" value="permanent" />
          <el-option label="仅当前周期" value="current_period" />
        </el-select>
        <el-select
          v-model="filter.priority"
          data-test="approval-filter-priority"
          aria-label="优先级"
        >
          <el-option label="全部优先级" value="all" />
          <el-option label="紧急" value="urgent" />
          <el-option label="普通" value="normal" />
        </el-select>
        <el-select
          v-model="filter.status"
          data-test="approval-filter-status"
          aria-label="当前状态"
        >
          <el-option label="全部状态" value="all" />
          <el-option
            v-for="(label, value) in STATUS_LABELS"
            :key="value"
            :label="label"
            :value="value"
          />
        </el-select>
        <el-input
          v-model="filter.keyword"
          data-test="approval-filter-keyword"
          clearable
          placeholder="搜索申请人或预算主体"
          aria-label="搜索申请"
        />
      </div>

      <el-alert
        v-if="activeTab === 'all'"
        class="visibility-notice"
        title="当前权限可见申请"
        description="暂无全量查询接口，此视图仅合并待我审批与我发起的申请，不代表全公司完整记录。"
        type="info"
        :closable="false"
        show-icon
      />

      <div v-loading="loading" class="budget-desktop-only approval-table-wrap">
        <el-table
          :data="filteredRows"
          row-key="id"
          class="approval-table"
          empty-text="暂无符合条件的申请"
        >
          <el-table-column v-if="showSelection" width="48">
            <template #default="{ row }">
              <input
                :data-test="`request-select-${row.id}`"
                type="checkbox"
                :checked="selectedIds.includes(row.id)"
                :disabled="!isSelectable(row)"
                :aria-label="`选择 ${resolveScopeName(row)}`"
                @change="toggleSelection(row, $event)"
              >
            </template>
          </el-table-column>
          <el-table-column label="申请主体" min-width="180">
            <template #default="{ row }">
              <div
                :data-test="`request-row-${row.id}`"
                class="approval-subject"
                :class="{ 'is-stale': isStaleRequest(row) }"
              >
                <span class="approval-avatar">{{ resolveScopeName(row).slice(0, 1) }}</span>
                <div>
                  <strong>{{ resolveScopeName(row) }}</strong>
                  <small>{{ subjectCaption(row) }}</small>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="申请内容" min-width="230">
            <template #default="{ row }">
              <div class="approval-change">
                <strong>
                  <el-tag v-if="row.model_tier_name" size="small" type="primary">
                    {{ row.model_tier_name }}
                  </el-tag>
                  {{ PERIOD_LABELS[row.period_type] }}
                  {{ formatMoney(row.current_amount_snapshot) }} → {{ formatMoney(row.target_amount_usd) }}
                </strong>
                <small v-if="isStaleRequest(row)" class="stale-copy">提交后数据已变化，需要重新预检</small>
                <small v-else>当前已用 {{ formatMoney(row.used_amount_snapshot) }} · {{ row.reason || '未填写原因' }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="类型" min-width="130">
            <template #default="{ row }">
              <div class="tag-stack">
                <el-tag v-if="row.priority === 'urgent'" type="danger" size="small">紧急</el-tag>
                <el-tag :type="row.duration_type === 'permanent' ? 'success' : 'primary'" size="small">
                  {{ durationLabel(row.duration_type) }}
                </el-tag>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="当前状态" min-width="120">
            <template #default="{ row }">
              <el-tag :type="statusTagType(row.status)" size="small">{{ STATUS_LABELS[row.status] }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="提交时间" min-width="150">
            <template #default="{ row }"><span class="mono">{{ formatDateTime(row.created_at) }}</span></template>
          </el-table-column>
          <el-table-column label="当前审批层级" min-width="130">
            <template #default="{ row }">{{ LEVEL_LABELS[row.current_level] }}</template>
          </el-table-column>
          <el-table-column label="操作" width="132" align="center" fixed="right">
            <template #default="{ row }">
              <el-button
                v-if="isStaleRequest(row)"
                :data-test="`request-action-${row.id}`"
                link
                type="warning"
                @click="recheck(row)"
              >
                重新预检
              </el-button>
              <el-button
                v-else
                :data-test="`request-action-${row.id}`"
                link
                type="primary"
                @click="openDetail(row)"
              >
                {{ activeTab === 'pending' ? '查看并审批' : '查看详情' }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div v-loading="loading" class="budget-mobile-only approval-mobile-list">
        <ExpansionMobileListItem
          v-for="row in filteredRows"
          :key="row.id"
          :item="row"
          :selectable="showSelection && isSelectable(row)"
          :selected="selectedIds.includes(row.id)"
          @select="toggleMobileSelection(row, $event)"
          @open="isStaleRequest(row) ? recheck(row) : openDetail(row)"
        />
        <el-empty v-if="!loading && filteredRows.length === 0" description="暂无符合条件的申请" />
      </div>
    </section>

    <BudgetSelectionBar
      :count="selectedRows.length"
      :remaining-text="hasSelectedStale ? '数据已变化的申请不能批量审批' : '仅处理当前可审批申请'"
      @clear="clearSelection"
    >
      <template #actions>
        <el-button
          data-test="batch-approve"
          type="primary"
          :loading="batchWorking"
          :disabled="hasSelectedStale"
          @click="batchApprove"
        >
          批量批准
        </el-button>
        <el-button
          data-test="batch-reject"
          type="danger"
          plain
          :disabled="hasSelectedStale"
          @click="batchRejectVisible = true"
        >
          批量驳回
        </el-button>
      </template>
    </BudgetSelectionBar>

    <el-dialog v-model="batchRejectVisible" title="批量驳回" width="min(440px, 92vw)">
      <p class="dialog-copy">将驳回 {{ selectedRows.length }} 项申请，请填写统一审批意见。</p>
      <el-input v-model="batchRejectComment" type="textarea" :rows="3" placeholder="驳回理由（必填）" />
      <template #footer>
        <el-button @click="batchRejectVisible = false">取消</el-button>
        <el-button type="danger" :loading="batchWorking" @click="batchReject">确认驳回</el-button>
      </template>
    </el-dialog>

    <ExpansionCreateForm
      v-model="createVisible"
      :ordinary-user="!authStore.isManager"
      :initial-scope-type="initialScopeType"
      :initial-scope-id="initialScopeId"
      @created="loadRows"
    />
    <ExpansionApprovalDetail
      v-model="detailVisible"
      v-model:detail="selectedDetail"
      @changed="loadRows"
    />
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import {
  batchDecideExpansion,
  getExpansionDetail,
  getMyExpansionRequests,
  getPendingExpansions,
  listModelTiers,
  reprecheckExpansion,
} from '@/services/control-plane'
import BudgetSelectionBar from '@/components/budget/BudgetSelectionBar.vue'
import ExpansionApprovalDetail from '@/components/expansion/ExpansionApprovalDetail.vue'
import ExpansionCreateForm from '@/components/expansion/ExpansionCreateForm.vue'
import ExpansionMobileListItem from '@/components/expansion/ExpansionMobileListItem.vue'
import type { BudgetScopeType } from '@/types/budget'
import type { ModelTier } from '@/types/model-tier'
import {
  LEVEL_LABELS,
  PERIOD_LABELS,
  STATUS_LABELS,
  type ExpansionListFilter,
  type ExpansionListTab,
  type ExpansionRequest,
  type ExpansionStatus,
} from '@/types/expansion'

const route = useRoute()
const authStore = useAuthStore()
const activeTab = ref<ExpansionListTab>('pending')
const pendingRows = ref<ExpansionRequest[]>([])
const mineRows = ref<ExpansionRequest[]>([])
const tiers = ref<ModelTier[]>([])
const loading = ref(false)
const batchWorking = ref(false)
const selectedIds = ref<string[]>([])
const createVisible = ref(false)
const detailVisible = ref(false)
const selectedDetail = ref<Awaited<ReturnType<typeof getExpansionDetail>> | null>(null)
const batchRejectVisible = ref(false)
const batchRejectComment = ref('')

const filter = reactive<ExpansionListFilter>({
  dimension: 'all',
  modelTierId: 'all',
  duration: 'all',
  priority: 'all',
  status: 'all',
  keyword: '',
})

const isAdmin = computed(() => authStore.effectiveRole === 'admin')
const canApprove = computed(() => authStore.isManager)
const canCreateRequest = computed(() => ['studio_admin', 'dept_admin'].includes(authStore.effectiveRole || ''))
const initialScopeType = computed(() =>
  (typeof route.query.scope_type === 'string' ? route.query.scope_type : 'member') as BudgetScopeType,
)
const initialScopeId = computed(() =>
  typeof route.query.scope_id === 'string' ? route.query.scope_id : authStore.user?.id || '',
)

const availableTabs = computed(() => [
  ...(canApprove.value ? [{ value: 'pending' as const, label: '待我审批' }] : []),
  { value: 'mine' as const, label: '我发起的' },
  ...(isAdmin.value ? [{ value: 'all' as const, label: '全部申请' }] : []),
])
const activeTabLabel = computed(() => availableTabs.value.find(tab => tab.value === activeTab.value)?.label || '申请')
const allRows = computed(() => {
  const merged = new Map<string, ExpansionRequest>()
  for (const row of [...pendingRows.value, ...mineRows.value]) merged.set(row.id, row)
  return [...merged.values()]
})
const sourceRows = computed(() => {
  if (activeTab.value === 'pending') return pendingRows.value
  if (activeTab.value === 'mine') return mineRows.value
  return allRows.value
})
const filteredRows = computed(() => sourceRows.value.filter(row => {
  if (filter.dimension !== 'all' && row.dimension_type !== filter.dimension) return false
  if (filter.modelTierId !== 'all' && row.model_tier_id !== filter.modelTierId) return false
  if (filter.duration !== 'all' && row.duration_type !== filter.duration) return false
  if (filter.priority !== 'all' && row.priority !== filter.priority) return false
  if (filter.status !== 'all' && row.status !== filter.status) return false
  const needle = filter.keyword.trim().toLowerCase()
  if (!needle) return true
  return `${row.scope_name} ${row.requester_name} ${row.reason}`.toLowerCase().includes(needle)
}))
const urgentCount = computed(() => sourceRows.value.filter(row => row.priority === 'urgent').length)
const staleCount = computed(() => sourceRows.value.filter(isStaleRequest).length)
const showSelection = computed(() => activeTab.value === 'pending' && canApprove.value)
const selectedRows = computed(() =>
  pendingRows.value.filter(row => selectedIds.value.includes(row.id) && isSelectable(row)),
)
const hasSelectedStale = computed(() =>
  pendingRows.value.some(row => selectedIds.value.includes(row.id) && isStaleRequest(row)),
)

const pageTitle = computed(() => ({
  admin: '平台扩容审批',
  studio_admin: '工作室扩容审批',
  dept_admin: '部门额度审批',
  user: '我的扩容申请',
}[authStore.effectiveRole || 'user']))
const pageDescription = computed(() => ({
  admin: '审批全公司总预算与模型等级额度申请。',
  studio_admin: '审批管辖工作室下属部门与成员的额度申请。',
  dept_admin: '审批管辖部门成员的个人额度申请。',
  user: '查看并跟踪个人额度申请。',
}[authStore.effectiveRole || 'user']))

function isStaleRequest(row: ExpansionRequest) {
  return row.status === 'precheck_required' || row.status === 'pending_reconfirmation'
}

function isSelectable(row: ExpansionRequest) {
  return showSelection.value && row.status === 'pending' && !isStaleRequest(row)
}

function toggleSelection(row: ExpansionRequest, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  if (checked && isSelectable(row)) selectedIds.value = [...new Set([...selectedIds.value, row.id])]
  else selectedIds.value = selectedIds.value.filter(id => id !== row.id)
}

function toggleMobileSelection(row: ExpansionRequest, checked: boolean) {
  toggleSelection(row, { target: { checked } } as unknown as Event)
}

function clearSelection() {
  selectedIds.value = []
}

async function loadRows() {
  loading.value = true
  try {
    const [pending, mine, modelTiers] = await Promise.all([
      canApprove.value ? getPendingExpansions() : Promise.resolve([]),
      getMyExpansionRequests(),
      listModelTiers(false),
    ])
    pendingRows.value = pending
    mineRows.value = mine
    tiers.value = modelTiers
    selectedIds.value = []
  } catch (error: any) {
    ElMessage.error(error?.message || '申请列表加载失败')
  } finally {
    loading.value = false
  }
}

async function runBatch(action: 'approve' | 'reject', comment = '') {
  const decisions = selectedRows.value.map(row => ({
    id: row.id,
    action,
    comment,
    precheck_version: row.precheck_version,
  }))
  if (!decisions.length) return
  batchWorking.value = true
  try {
    const { results } = await batchDecideExpansion(decisions)
    const recheckItems = results.filter(result => result.next_action === 'reprecheck')
    await Promise.all(recheckItems.map(result => reprecheckExpansion(result.id)))
    const successCount = results.filter(result => result.ok).length
    ElMessage.success(`${successCount} 项已${action === 'approve' ? '批准' : '驳回'}`)
    if (recheckItems.length) ElMessage.warning(`${recheckItems.length} 项已重新预检，请再次确认`)
    batchRejectVisible.value = false
    batchRejectComment.value = ''
    await loadRows()
  } catch (error: any) {
    ElMessage.error(error?.message || '批量审批失败')
  } finally {
    batchWorking.value = false
  }
}

async function batchApprove() {
  await runBatch('approve')
}

async function batchReject() {
  if (!batchRejectComment.value.trim()) {
    ElMessage.warning('驳回必须填写理由')
    return
  }
  await runBatch('reject', batchRejectComment.value.trim())
}

async function recheck(row: ExpansionRequest) {
  try {
    selectedDetail.value = await reprecheckExpansion(row.id)
    detailVisible.value = true
    await loadRows()
  } catch (error: any) {
    ElMessage.error(error?.message || '重新预检失败')
  }
}

async function openDetail(row: ExpansionRequest) {
  try {
    selectedDetail.value = await getExpansionDetail(row.id)
    detailVisible.value = true
  } catch (error: any) {
    ElMessage.error(error?.message || '申请详情加载失败')
  }
}

function resolveScopeName(row: ExpansionRequest) {
  return row.scope_name || row.requester_name || row.scope_id
}

function subjectCaption(row: ExpansionRequest) {
  const scope = {
    member: '个人额度',
    dept_daily: '成员日常预算池',
    dept_service: '程序服务预算池',
    studio: '工作室预算',
  }[row.scope_type] || row.scope_type
  return row.scope_type === 'member' ? `${row.requester_name || '成员'} · ${scope}` : `${row.requester_name || '申请人'} · ${scope}`
}

function durationLabel(value: ExpansionRequest['duration_type']) {
  return value === 'permanent' ? '长期' : '本周期'
}

function statusTagType(status: ExpansionStatus) {
  if (status === 'approved') return 'success'
  if (status === 'rejected') return 'danger'
  if (status === 'cancelled' || status === 'expired') return 'info'
  return 'warning'
}

function formatMoney(value: string | number | null | undefined) {
  const number = Number(value)
  if (!Number.isFinite(number)) return '—'
  return `$${number.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value || '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}

watch(activeTab, () => clearSelection())

onMounted(() => {
  if (!canApprove.value) activeTab.value = 'mine'
  if (route.query.create === '1' || (route.query.scope_type && route.query.scope_id)) createVisible.value = true
  loadRows()
})
</script>

<style scoped>
.expansion-workbench {
  padding: var(--space-5);
}

.workbench-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
}

.workbench-header h1 {
  margin: 0;
  color: var(--text-primary);
  font-size: var(--font-size-2xl);
}

.workbench-header p {
  margin: var(--space-1) 0 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.workbench-header__actions,
.tag-stack {
  display: flex;
  gap: var(--space-2);
}

.approval-tabs {
  display: flex;
  gap: var(--space-5);
  border-bottom: 1px solid var(--border-default);
}

.approval-tabs button {
  position: relative;
  padding: var(--space-3) 0;
  color: var(--text-secondary);
  background: transparent;
  border: 0;
  cursor: pointer;
  font: inherit;
  font-weight: var(--font-weight-medium);
}

.approval-tabs button.active {
  color: var(--brand-green-text);
}

.approval-tabs button.active::after {
  position: absolute;
  right: 0;
  bottom: -1px;
  left: 0;
  height: 2px;
  background: var(--brand-green);
  content: '';
}

.tab-count {
  display: inline-flex;
  min-width: 20px;
  height: 20px;
  margin-left: var(--space-1);
  padding: 0 var(--space-1);
  align-items: center;
  justify-content: center;
  color: var(--brand-green-text);
  background: var(--el-color-primary-light-9);
  border-radius: var(--radius-full);
  font-size: var(--font-size-xs);
}

.approval-kpis {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  overflow: hidden;
  background: var(--budget-panel-bg);
  border: 1px solid var(--budget-panel-border);
  border-radius: var(--budget-panel-radius);
}

.approval-kpis > div {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-4);
  border-right: 1px solid var(--budget-row-border);
}

.approval-kpis > div:last-child { border-right: 0; }
.approval-kpis span { color: var(--text-secondary); font-size: var(--font-size-xs); }
.approval-kpis strong { color: var(--text-primary); font-size: var(--font-size-xl); }
.approval-kpis .tone-danger { color: var(--color-danger); }
.approval-kpis .tone-warning { color: var(--color-warning); }

.approval-filters {
  display: grid;
  grid-template-columns: repeat(5, minmax(128px, 1fr)) minmax(220px, 1.5fr);
  gap: var(--space-2);
  padding: var(--space-3);
  border-bottom: 1px solid var(--budget-row-border);
}

.visibility-notice {
  border-radius: 0;
}

.approval-table-wrap {
  overflow-x: auto;
}

.approval-subject,
.approval-change {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: var(--space-2);
}

.approval-subject.is-stale {
  margin: calc(var(--space-2) * -1);
  padding: var(--space-2);
  background: var(--budget-stale-bg);
  border-radius: var(--radius-sm);
}

.approval-avatar {
  display: grid;
  width: 32px;
  height: 32px;
  flex: none;
  place-items: center;
  color: var(--brand-green-text);
  background: var(--el-color-primary-light-9);
  border-radius: var(--radius-full);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
}

.approval-subject > div,
.approval-change {
  flex-direction: column;
  align-items: flex-start;
}

.approval-subject small,
.approval-change small {
  overflow: hidden;
  max-width: 100%;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.approval-change strong {
  display: flex;
  align-items: center;
  gap: var(--space-1);
}

.approval-change .stale-copy { color: var(--color-warning); }
.mono { font-variant-numeric: tabular-nums; }
.approval-mobile-list { padding: var(--space-3); }
.dialog-copy { margin: 0 0 var(--space-3); color: var(--text-secondary); font-size: var(--font-size-sm); }

@media (max-width: 1180px) {
  .approval-filters { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}

@media (max-width: 760px) {
  .expansion-workbench { padding: var(--space-3); }
  .workbench-header { flex-direction: column; }
  .workbench-header__actions { width: 100%; }
  .workbench-header__actions :deep(.el-button) { flex: 1; }
  .approval-kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .approval-kpis > div:nth-child(2) { border-right: 0; }
  .approval-kpis > div:nth-child(-n + 2) { border-bottom: 1px solid var(--budget-row-border); }
  .approval-filters { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 480px) {
  .approval-tabs { gap: var(--space-3); }
  .approval-filters { grid-template-columns: 1fr; }
}
</style>
