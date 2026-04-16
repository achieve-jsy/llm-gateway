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

    <aside class="approval-permission" :data-test="`approval-permission-${authStore.effectiveRole || 'user'}`">
      <span class="approval-permission__badge">{{ permissionCopy.badge }}</span>
      <div class="approval-permission__body">
        <strong>{{ permissionCopy.title }}</strong>
        <span>{{ permissionCopy.detail }}</span>
      </div>
      <div class="approval-permission__meta">
        <span>{{ permissionCopy.approvalScope }}</span>
        <span>{{ permissionCopy.createScope }}</span>
      </div>
    </aside>

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
        <span>目标低于已用</span>
        <strong class="tone-warning">{{ belowUsedCount }}</strong>
      </div>
      <div>
        <span>需关注申请</span>
        <strong class="tone-warning">{{ attentionCount }}</strong>
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
          <el-option label="临时调整" value="current_period" />
          <el-option label="长期调额" value="permanent" />
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
          v-if="showStatusFilter"
          v-model="filter.status"
          data-test="approval-filter-status"
          aria-label="当前状态"
        >
          <el-option
            v-for="option in STATUS_FILTER_OPTIONS"
            :key="option.value"
            :label="option.label"
            :value="option.value"
          />
        </el-select>
        <el-input
          v-model="filter.keyword"
          data-test="approval-filter-keyword"
          clearable
          :placeholder="isDeptAdmin ? '搜索成员或预算主体' : '搜索申请人或预算主体'"
          aria-label="搜索申请"
        />
        <span class="filter-result-count">筛选结果 {{ filteredRows.length }}</span>
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
          <el-table-column :label="subjectColumnLabel" min-width="190">
            <template #default="{ row }">
              <div
                :data-test="`request-row-${row.id}`"
                class="approval-subject"
                :class="{ 'is-stale': isStaleRequest(row) }"
              >
                <span class="approval-avatar">{{ resolveScopeName(row).slice(0, 1) }}</span>
                <div>
                  <strong>{{ resolveScopeName(row) }}</strong>
                  <small>{{ scopeCaption(row) }}</small>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column v-if="showRequesterColumn" label="申请人" min-width="130">
            <template #default="{ row }">
              <div class="requester-cell">
                <strong>{{ requesterName(row) }}</strong>
                <small>{{ LEVEL_LABELS[row.current_level] }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="限制项" min-width="160">
            <template #default="{ row }">
              <div class="limit-cell">
                <el-tag :type="row.dimension_type === 'tier' ? 'primary' : 'success'" size="small">
                  {{ limitItemLabel(row) }}
                </el-tag>
                <small>{{ PERIOD_LABELS[row.period_type] }} · {{ durationLabel(row.duration_type) }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="额度变化" min-width="210">
            <template #default="{ row }">
              <div class="approval-change">
                <strong>{{ formatMoney(row.current_amount_snapshot) }} → {{ formatMoney(targetAmount(row)) }}</strong>
                <small>增加 {{ formatMoney(increaseAmount(row)) }} · 已用 {{ formatMoney(row.used_amount_snapshot) }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="风险提示" min-width="180">
            <template #default="{ row }">
              <div class="risk-cell">
                <el-tag v-if="row.priority === 'urgent'" type="danger" size="small">紧急</el-tag>
                <el-tag v-if="targetBelowUsed(row)" type="danger" size="small">目标低于已用</el-tag>
                <el-tag v-else type="success" size="small">可批准</el-tag>
                <el-tooltip :content="row.reason || '未填写原因'" placement="top" :show-after="250">
                  <small class="risk-reason">{{ row.reason || '未填写原因' }}</small>
                </el-tooltip>
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
          <el-table-column label="操作" width="190" align="center" fixed="right">
            <template #default="{ row }">
              <div class="row-actions">
                <template v-if="showSelection && row.status === 'pending'">
                  <el-button
                    :data-test="`request-approve-${row.id}`"
                    link
                    type="success"
                    :disabled="!rowCanDirectApprove(row)"
                    :loading="rowWorkingId === row.id"
                    @click="approveRow(row)"
                  >
                    批准
                  </el-button>
                  <el-button
                    :data-test="`request-reject-${row.id}`"
                    link
                    type="danger"
                    :disabled="!isSelectable(row)"
                    @click="rejectRow(row)"
                  >
                    驳回
                  </el-button>
                </template>
                <el-button
                  v-if="isStaleRequest(row)"
                  :data-test="`request-action-${row.id}`"
                  link
                  type="warning"
                  :loading="rowWorkingId === row.id"
                  @click="recheck(row)"
                >
                  刷新数据
                </el-button>
                <el-button
                  v-if="canCancelRow(row)"
                  :data-test="`request-cancel-${row.id}`"
                  link
                  type="danger"
                  :loading="rowWorkingId === row.id"
                  @click="cancelRow(row)"
                >
                  取消
                </el-button>
                <el-button
                  :data-test="`request-detail-${row.id}`"
                  link
                  type="primary"
                  @click="openDetail(row)"
                >
                  详情
                </el-button>
              </div>
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
          :show-requester="showRequesterColumn"
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
      :fixed-manager-scope="fixedCreateScope"
      :manager-scope-label="createScopeLabel"
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
import { ElMessage, ElMessageBox } from 'element-plus'
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
  STATUS_FILTER_OPTIONS,
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
const rowWorkingId = ref('')
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
const canCreateRequest = computed(() => Boolean(authStore.effectiveRole || authStore.user?.id))
const isDeptAdmin = computed(() => authStore.effectiveRole === 'dept_admin')
const hasRouteScope = computed(() => typeof route.query.scope_type === 'string' && typeof route.query.scope_id === 'string')
const subjectColumnLabel = computed(() => isDeptAdmin.value ? '申请成员' : '申请主体')
const showRequesterColumn = computed(() => !isDeptAdmin.value)
const initialScopeType = computed(() =>
  (typeof route.query.scope_type === 'string'
    ? route.query.scope_type
    : isDeptAdmin.value
      ? 'dept_daily'
      : 'member') as BudgetScopeType,
)
const initialScopeId = computed(() =>
  typeof route.query.scope_id === 'string'
    ? route.query.scope_id
    : isDeptAdmin.value
      ? deptAdminScopeId.value
      : authStore.user?.id || '',
)
const deptAdminScopeId = computed(() => {
  const role = authStore.orgRoles.find(role => role.role === 'dept_admin' || role.scope_type === 'dept')
  return role?.scope_id || authStore.user?.departmentId || ''
})
const deptAdminScopeLabel = computed(() => authStore.user?.departmentName || deptAdminScopeId.value || '我的部门')
const fixedCreateScope = computed(() => isDeptAdmin.value || hasRouteScope.value)
const createScopeLabel = computed(() => {
  if (hasRouteScope.value) {
    const routeName = typeof route.query.scope_name === 'string' ? route.query.scope_name : ''
    if (routeName) return routeName
    if (initialScopeType.value === 'studio') return '当前工作室'
    if (initialScopeType.value === 'dept_daily') return '成员日常预算池'
    if (initialScopeType.value === 'dept_service') return '程序服务预算池'
  }
  return deptAdminScopeLabel.value
})

const availableTabs = computed(() => [
  ...(canApprove.value ? [{ value: 'pending' as const, label: '待我审批' }] : []),
  { value: 'mine' as const, label: '我发起的' },
  ...(isAdmin.value ? [{ value: 'all' as const, label: '权限范围内' }] : []),
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
const filteredRows = computed(() => sourceRows.value)
const showStatusFilter = computed(() => activeTab.value !== 'pending')
const urgentCount = computed(() => sourceRows.value.filter(row => row.priority === 'urgent').length)
const belowUsedCount = computed(() => sourceRows.value.filter(targetBelowUsed).length)
const attentionCount = computed(() =>
  sourceRows.value.filter(row => row.priority === 'urgent' || isStaleRequest(row) || targetBelowUsed(row)).length,
)
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

const permissionCopy = computed(() => {
  switch (authStore.effectiveRole) {
    case 'admin':
      return {
        badge: '平台管理员',
        title: '可审批平台级扩容申请',
        detail: '可查看平台待办和本人发起的申请；可为公司、工作室、部门或成员发起扩容。',
        approvalScope: '审批范围：平台级',
        createScope: '发起范围：全局',
      }
    case 'studio_admin':
      return {
        badge: '工作室管理员',
        title: '可审批工作室范围内的扩容申请',
        detail: '可处理管辖工作室、下属部门和成员相关申请；超出工作室额度的事项需上报平台。',
        approvalScope: '审批范围：工作室级',
        createScope: '发起范围：管辖工作室',
      }
    case 'dept_admin':
      return {
        badge: '部门管理员',
        title: '可审批部门成员额度申请',
        detail: '可处理本部门成员个人额度申请；部门预算池扩容由工作室负责人审批。',
        approvalScope: '审批范围：部门级',
        createScope: '发起范围：本部门',
      }
    default:
      return {
        badge: '普通用户',
        title: '仅查看和发起本人的额度申请',
        detail: '可查看自己提交的扩容申请并申请个人额度；无法审批他人或修改组织预算。',
        approvalScope: '审批范围：无',
        createScope: '发起范围：本人',
      }
  }
})

function isStaleRequest(row: ExpansionRequest) {
  return row.status === 'precheck_required' || row.status === 'pending_reconfirmation'
}

function isSelectable(row: ExpansionRequest) {
  return showSelection.value && row.status === 'pending' && !isStaleRequest(row)
}

function rowCanDirectApprove(row: ExpansionRequest) {
  return isSelectable(row) && !targetBelowUsed(row)
}

function canCancelRow(row: ExpansionRequest) {
  return activeTab.value === 'mine' && ['pending', 'precheck_required', 'pending_reconfirmation'].includes(row.status)
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
    const listFilter = requestFilter()
    const [pending, mine, modelTiers] = await Promise.all([
      canApprove.value ? getPendingExpansions({ ...listFilter, status: undefined }) : Promise.resolve([]),
      getMyExpansionRequests(listFilter),
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

function requestFilter(): Partial<ExpansionListFilter> {
  return {
    dimension: filter.dimension,
    modelTierId: filter.modelTierId,
    duration: filter.duration,
    priority: filter.priority,
    status: showStatusFilter.value ? filter.status : 'all',
    keyword: filter.keyword,
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
    if (recheckItems.length) ElMessage.warning(`${recheckItems.length} 项需要刷新数据，请再次确认`)
    batchRejectVisible.value = false
    batchRejectComment.value = ''
    await loadRows()
  } catch (error: any) {
    ElMessage.error(error?.message || '批量审批失败')
  } finally {
    batchWorking.value = false
  }
}

async function runSingleDecision(row: ExpansionRequest, action: 'approve' | 'reject' | 'cancel', comment = '') {
  rowWorkingId.value = row.id
  try {
    const { results } = await batchDecideExpansion([{ id: row.id, action, comment, precheck_version: row.precheck_version }])
    const result = results[0]
    if (!result?.ok) {
      if (result?.next_action === 'reprecheck') {
        ElMessage.warning('数据已变化，已为你刷新最新状态')
        await recheck(row)
        return
      }
      ElMessage.error(result?.error || '审批操作失败')
      return
    }
    ElMessage.success(action === 'approve' ? '申请已批准并生效' : action === 'cancel' ? '申请已取消' : '申请已驳回')
    await loadRows()
  } catch (error: any) {
    ElMessage.error(error?.message || '审批操作失败')
  } finally {
    rowWorkingId.value = ''
  }
}

async function cancelRow(row: ExpansionRequest) {
  if (!canCancelRow(row)) return
  try {
    await ElMessageBox.confirm(
      `确认取消「${resolveScopeName(row)}」的扩容申请？取消后如仍需额度，需要重新提交申请。`,
      '取消扩容申请',
      { type: 'warning', confirmButtonText: '确认取消', cancelButtonText: '保留申请' },
    )
  } catch (error) {
    return
  }
  await runSingleDecision(row, 'cancel')
}

async function approveRow(row: ExpansionRequest) {
  if (!rowCanDirectApprove(row)) return
  try {
    await ElMessageBox.confirm(
      `确认批准「${resolveScopeName(row)}」的${limitItemLabel(row)}${durationLabel(row.duration_type)}到 ${formatMoney(row.target_amount_usd)}？`,
      '确认批准扩容申请',
      { type: 'warning', confirmButtonText: '确认批准', cancelButtonText: '取消' },
    )
  } catch (error) {
    return
  }
  await runSingleDecision(row, 'approve')
}

async function rejectRow(row: ExpansionRequest) {
  if (!isSelectable(row)) return
  try {
    const { value } = await ElMessageBox.prompt('请填写驳回理由', '驳回扩容申请', {
      confirmButtonText: '确认驳回',
      cancelButtonText: '取消',
      inputType: 'textarea',
      inputPattern: /\S/,
      inputErrorMessage: '驳回必须填写理由',
    })
    await runSingleDecision(row, 'reject', String(value || '').trim())
  } catch (error) {
    // 用户取消弹窗时无需提示。
  }
}

async function batchApprove() {
  if (!selectedRows.value.length) return
  try {
    await ElMessageBox.confirm(
      `确认批量批准 ${selectedRows.value.length} 项扩容申请？批准后额度将自动生效。`,
      '确认批量批准',
      { type: 'warning', confirmButtonText: '确认批准', cancelButtonText: '取消' },
    )
  } catch (error) {
    return
  }
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
    ElMessage.error(error?.message || '刷新数据失败')
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
  if (row.scope_name && !isUUIDLike(row.scope_name)) return row.scope_name
  if (row.scope_type === 'member') return requesterName(row)
  const fallback = {
    company: '公司预算',
    studio: '工作室预算',
    dept_daily: '成员日常预算池',
    dept_service: '程序服务预算池',
  }[row.scope_type]
  return fallback || row.scope_id
}

function requesterName(row: ExpansionRequest) {
  if (row.requester_name && !isUUIDLike(row.requester_name)) return row.requester_name
  return row.requester_id ? `用户 ${row.requester_id.slice(0, 8)}` : '未知申请人'
}

function isUUIDLike(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
}

function scopeCaption(row: ExpansionRequest) {
  const scope = {
    member: '个人额度',
    dept_daily: '成员日常预算池',
    dept_service: '程序服务预算池',
    studio: '工作室预算',
    company: '公司预算',
  }[row.scope_type] || row.scope_type
  return scope
}

function limitItemLabel(row: ExpansionRequest) {
  return row.dimension_type === 'tier'
    ? `${row.model_tier_name || '模型等级'}额度`
    : '总预算'
}

function increaseAmount(row: ExpansionRequest) {
  const current = Number(row.current_amount_snapshot)
  const target = targetAmount(row)
  if (!Number.isFinite(current) || !Number.isFinite(target)) return Number.NaN
  return Math.max(target - current, 0)
}

function targetAmount(row: ExpansionRequest) {
  const target = Number(row.target_amount_usd)
  if (Number.isFinite(target) && target > 0) return target
  const requested = Number(row.requested_amount)
  return Number.isFinite(requested) ? requested : Number.NaN
}

function targetBelowUsed(row: ExpansionRequest) {
  const used = Number(row.used_amount_snapshot)
  const target = targetAmount(row)
  return Number.isFinite(used) && Number.isFinite(target) && used > target
}

function durationLabel(value: ExpansionRequest['duration_type']) {
  return value === 'current_period' ? '临时调整' : '长期调额'
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
  const pad = (number: number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

let filterReloadTimer: ReturnType<typeof setTimeout> | null = null
watch(activeTab, () => {
  clearSelection()
  loadRows()
})
watch(
  () => [filter.dimension, filter.modelTierId, filter.duration, filter.priority, filter.status, filter.keyword] as const,
  () => {
    clearSelection()
    if (filterReloadTimer) clearTimeout(filterReloadTimer)
    filterReloadTimer = setTimeout(() => {
      loadRows()
    }, 250)
  },
)

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
.row-actions {
  display: flex;
  gap: var(--space-2);
}

.row-actions {
  justify-content: center;
  flex-wrap: wrap;
  gap: 0 var(--space-1);
}

.approval-tabs {
  display: flex;
  gap: var(--space-5);
  border-bottom: 1px solid var(--border-default);
}

.approval-permission {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--brand-green-light);
  border-radius: var(--budget-panel-radius);
}

.approval-permission__badge {
  flex: none;
  padding: 2px var(--space-2);
  color: var(--brand-green-text);
  background: var(--bg-surface);
  border: 1px solid var(--brand-green-light);
  border-radius: var(--radius-full);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
}

.approval-permission__body {
  display: grid;
  min-width: 0;
  gap: 2px;
  flex: 1;
}

.approval-permission__body span,
.approval-permission__meta span {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.approval-permission__meta {
  display: flex;
  flex: none;
  gap: var(--space-2);
  flex-wrap: wrap;
  justify-content: flex-end;
}

.approval-permission__meta span {
  padding: 2px var(--space-2);
  background: var(--bg-surface);
  border-radius: var(--radius-full);
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
  grid-template-columns: repeat(5, minmax(128px, 1fr)) minmax(220px, 1.5fr) auto;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3);
  border-bottom: 1px solid var(--budget-row-border);
}

.filter-result-count {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  white-space: nowrap;
}

.visibility-notice {
  border-radius: 0;
}

.approval-table-wrap {
  overflow-x: auto;
}

.approval-subject,
.approval-change,
.limit-cell,
.requester-cell,
.risk-cell {
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
.approval-change,
.limit-cell,
.requester-cell,
.risk-cell {
  flex-direction: column;
  align-items: flex-start;
}

.approval-subject small,
.approval-change small,
.limit-cell small,
.requester-cell small,
.risk-cell small {
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

.requester-cell strong { color: var(--text-primary); }

.risk-reason {
  display: inline-block;
  max-width: 160px;
  cursor: help;
}

.risk-cell {
  gap: var(--space-1);
}

.risk-cell :deep(.el-tag) + :deep(.el-tag) {
  margin-left: var(--space-1);
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
  .approval-permission { align-items: flex-start; flex-direction: column; }
  .approval-permission__meta { justify-content: flex-start; }
  .approval-kpis > div:nth-child(2) { border-right: 0; }
  .approval-kpis > div:nth-child(-n + 2) { border-bottom: 1px solid var(--budget-row-border); }
  .approval-filters { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 480px) {
  .approval-tabs { gap: var(--space-3); }
  .approval-filters { grid-template-columns: 1fr; }
}
</style>
