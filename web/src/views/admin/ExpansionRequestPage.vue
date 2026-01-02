<template>
  <div class="expansion-center">
    <header class="page-head">
      <div>
        <h1>扩容申请中心</h1>
        <p>预算不足时发起申请；负责人在这里审批并回到组织预算页完成调额。</p>
      </div>
      <el-button v-if="canCreateRequest" type="primary" @click="openCreateDialog()">新建申请</el-button>
    </header>

    <section class="kpi-strip">
      <div v-if="canApprove" class="kpi">
        <span>待我审批</span>
        <strong>{{ pendingList.length }}</strong>
      </div>
      <div v-if="!isAdmin" class="kpi">
        <span>我发起的审批中</span>
        <strong>{{ pendingMineCount }}</strong>
      </div>
      <div v-if="!isAdmin" class="kpi">
        <span>已批准</span>
        <strong>{{ approvedMineCount }}</strong>
      </div>
      <div v-if="!isAdmin" class="kpi">
        <span>已驳回/取消</span>
        <strong>{{ closedMineCount }}</strong>
      </div>
    </section>

    <section class="center-grid" :class="{ 'single-panel': isAdmin || !canApprove }">
      <div v-if="canApprove" class="panel">
        <div class="panel-head">
          <div>
            <h2>待我审批</h2>
            <p>处理下级扩容诉求；通过后仍需到预算页手动调额。</p>
          </div>
          <el-button :loading="pendingLoading" size="small" @click="loadPending">刷新</el-button>
        </div>

        <div v-loading="pendingLoading" class="pending-list">
          <!-- 批量操作栏 -->
          <div v-if="selectedPending.length > 0" class="batch-bar">
            <span>已选 {{ selectedPending.length }} 项</span>
            <el-button size="small" type="success" @click="batchApprove">批量批准</el-button>
            <el-button size="small" type="danger" plain @click="batchRejectVisible = true">批量驳回</el-button>
            <el-button size="small" link @click="selectedPending = []">取消选择</el-button>
          </div>
          <article v-for="item in pendingList" :key="item.id" class="approval-card" :class="{ selected: selectedPending.includes(item.id) }">
            <div class="approval-check">
              <el-checkbox v-model="item._checked" @change="toggleSelect(item)" />
            </div>
            <div class="approval-body">
              <div class="approval-top">
                <div class="scope-line">
                  <el-tag :color="scopeColor(item.scope_type)" effect="dark" size="small">{{ scopeLabel(item.scope_type) }}</el-tag>
                  <!-- FE-11: 使用率 -->
                  <el-tag v-if="item.current_usage_ratio != null && item.current_usage_ratio >= 0.8" type="danger" size="small" effect="plain" style="margin-left: 6px;">
                    使用率 {{ Math.round(item.current_usage_ratio * 100) }}%
                  </el-tag>
                  <el-tag v-else-if="item.current_usage_ratio != null" type="info" size="small" effect="plain" style="margin-left: 6px;">
                    使用率 {{ Math.round(item.current_usage_ratio * 100) }}%
                  </el-tag>
                </div>
                <div class="amount-block">
                  <span v-if="item.current_amount !== undefined">{{ formatMoney(item.current_amount) }} → </span>
                  <strong>{{ formatMoney(item.requested_amount) }}</strong>
                </div>
              </div>
              <p class="reason">{{ item.reason || '未填写申请理由' }}</p>
              <div class="approval-meta">
                <span>{{ LEVEL_LABELS[item.current_level] || item.current_level }}</span>
                <span>{{ formatDateTime(item.created_at) }}</span>
              </div>
              <div class="approval-actions">
                <el-button size="small" type="success" @click="openActionDialog(item, 'approve')">批准</el-button>
                <el-button size="small" type="danger" plain @click="openActionDialog(item, 'reject')">驳回</el-button>
                <el-button v-if="item.current_level === 'studio'" size="small" type="primary" plain @click="openActionDialog(item, 'escalate')">上报平台</el-button>
              </div>
            </div>
          </article>
          <el-empty v-if="!pendingLoading && pendingList.length === 0" description="暂无待审批申请" />
        </div>
      </div>

      <div v-if="!isAdmin" class="panel">
        <div class="panel-head">
          <div>
            <h2>我发起的申请</h2>
            <p>跟踪申请状态；通过后可跳转到对应预算节点。</p>
          </div>
          <el-button :loading="mineLoading" size="small" @click="loadMine">刷新</el-button>
        </div>

        <el-table
          v-loading="mineLoading"
          :data="mineList"
          stripe
          size="small"
          empty-text="暂无申请记录"
        >
          <el-table-column label="申请对象" min-width="180">
            <template #default="{ row }">
              <div class="scope-cell">
                <el-tag :color="scopeColor(row.scope_type)" effect="dark" size="small">{{ scopeLabel(row.scope_type) }}</el-tag>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="额度" width="140" align="right">
            <template #default="{ row }">
              <span class="amount-text">{{ formatMoney(row.requested_amount) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="105" align="center">
            <template #default="{ row }">
              <el-tag :color="STATUS_COLORS[row.status]" effect="dark" size="small">{{ STATUS_LABELS[row.status] }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="申请时间" width="140">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="130" fixed="right" align="center">
            <template #default="{ row }">
              <el-button v-if="row.status === 'approved' && row.scope_type !== 'member'" link type="primary" @click="goBudget(row)">去调额</el-button>
              <el-popconfirm
                v-else-if="row.status === 'pending'"
                title="确定取消该申请？"
                confirm-button-text="确定"
                cancel-button-text="取消"
                @confirm="handleCancel(row)"
              >
                <template #reference>
                  <el-button link type="danger">取消</el-button>
                </template>
              </el-popconfirm>
              <span v-else class="muted">—</span>
              <el-button link type="info" size="small" @click="showSteps(row)">详情</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </section>

    <el-dialog v-model="createVisible" title="新建扩容申请" width="520px">
      <el-form :model="createForm" label-width="96px">
        <el-form-item label="预算类型">
          <el-select v-model="createForm.scope_type" style="width: 100%">
            <el-option label="部门日常预算（个人 Key）" value="dept_daily" />
            <el-option label="部门服务预算（Service Key）" value="dept_service" />
            <el-option v-if="isStudioAdmin" label="工作室预算" value="studio" />
          </el-select>
        </el-form-item>
        <el-form-item label="申请额度">
          <el-input-number v-model="createForm.requested_amount" :min="0" :precision="2" controls-position="right" style="width: 100%" />
          <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px;">填写期望的月度总额度（USD），审批通过后由负责人调整。</div>
        </el-form-item>
        <el-form-item label="申请理由">
          <el-input v-model="createForm.reason" type="textarea" :rows="3" placeholder="说明业务背景、预计消耗和需要扩容的原因" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="createLoading" @click="handleCreate">提交申请</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="actionVisible" :title="actionTitle" width="420px">
      <div v-if="actionTarget" class="action-context">
        <span>{{ scopeLabel(actionTarget.scope_type) }}</span>
        <strong>{{ formatMoney(actionTarget.requested_amount) }}</strong>
      </div>
      <el-input v-model="actionComment" type="textarea" :rows="3" placeholder="审批意见（可选）" />
      <template #footer>
        <el-button @click="actionVisible = false">取消</el-button>
        <el-button type="primary" :loading="actionLoading" @click="handleAction">确认</el-button>
      </template>
    </el-dialog>

    <!-- 批量驳回弹窗 -->
    <el-dialog v-model="batchRejectVisible" title="批量驳回" width="420px">
      <p style="margin:0 0 12px; color: var(--el-text-color-secondary); font-size: 13px;">驳回 {{ selectedPending.length }} 项申请，请填写统一的驳回理由：</p>
      <el-input v-model="batchRejectComment" type="textarea" :rows="3" placeholder="驳回理由（必填）" />
      <template #footer>
        <el-button @click="batchRejectVisible = false">取消</el-button>
        <el-button type="danger" @click="batchReject">确认驳回</el-button>
      </template>
    </el-dialog>

    <!-- 审批流水弹窗 -->
    <el-dialog v-model="stepsVisible" title="审批流水" width="500px">
      <div v-loading="stepsLoading">
        <el-timeline v-if="stepsData.length > 0">
          <el-timeline-item
            v-for="step in stepsData"
            :key="step.id"
            :timestamp="step.created_at"
            :type="stepColor(step.action)"
            placement="top"
          >
            <strong>{{ stepActionLabel(step.action) }}</strong>
            <span style="margin-left: 8px; color: var(--el-text-color-secondary); font-size: 12px;">{{ step.level === 'platform' ? '平台级' : '工作室级' }}</span>
            <p v-if="step.comment" style="margin: 4px 0 0; color: var(--el-text-color-regular); font-size: 13px;">{{ step.comment }}</p>
          </el-timeline-item>
        </el-timeline>
        <el-empty v-else description="暂无审批记录" />
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import {
  createExpansionRequest,
  decideExpansion,
  batchDecideExpansion,
  getMyExpansionRequests,
  getPendingExpansions,
  getExpansionSteps,
} from '@/services/control-plane'
import type { CreateExpansionPayload, ExpansionAction, ExpansionRequest } from '@/types/expansion'
import { LEVEL_LABELS, STATUS_COLORS, STATUS_LABELS } from '@/types/expansion'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const mineList = ref<ExpansionRequest[]>([])
const mineLoading = ref(false)
const pendingList = ref<ExpansionRequest[]>([])
const pendingLoading = ref(false)

const createVisible = ref(false)
const createLoading = ref(false)
const createForm = ref<CreateExpansionPayload>({
  scope_type: 'dept_daily',
  scope_id: '',
  requested_amount: 100,
  reason: '',
})

const actionVisible = ref(false)
const actionLoading = ref(false)
const actionType = ref<ExpansionAction>('approve')
const actionTarget = ref<ExpansionRequest | null>(null)
const actionComment = ref('')

const pendingMineCount = computed(() => mineList.value.filter(i => i.status === 'pending').length)
const approvedMineCount = computed(() => mineList.value.filter(i => i.status === 'approved').length)
const closedMineCount = computed(() => mineList.value.filter(i => i.status === 'rejected' || i.status === 'cancelled').length)
const isAdmin = computed(() => authStore.effectiveRole === 'admin')
const isStudioAdmin = computed(() => authStore.effectiveRole === 'studio_admin')
const isDeptAdmin = computed(() => authStore.effectiveRole === 'dept_admin')
const canApprove = computed(() => isAdmin.value || isStudioAdmin.value || isDeptAdmin.value)
const canCreateRequest = computed(() => isDeptAdmin.value || isStudioAdmin.value)

// 批量审批
const selectedPending = ref<string[]>([])
const batchRejectVisible = ref(false)
const batchRejectComment = ref('')

function toggleSelect(item: any) {
  if (item._checked) {
    if (!selectedPending.value.includes(item.id)) selectedPending.value.push(item.id)
  } else {
    selectedPending.value = selectedPending.value.filter(id => id !== item.id)
  }
}

async function batchApprove() {
  if (selectedPending.value.length === 0) return
  try {
    const decisions = selectedPending.value.map(id => ({ id, action: 'approve', comment: '' }))
    await batchDecideExpansion(decisions)
    ElMessage.success(`${decisions.length} 项已批准`)
    selectedPending.value = []
    await reloadLists()
  } catch (e: any) {
    ElMessage.error(e?.message || '批量批准失败')
  }
}

async function batchReject() {
  if (selectedPending.value.length === 0 || !batchRejectComment.value.trim()) {
    ElMessage.warning('驳回必须填写理由')
    return
  }
  try {
    const decisions = selectedPending.value.map(id => ({ id, action: 'reject', comment: batchRejectComment.value }))
    await batchDecideExpansion(decisions)
    ElMessage.success(`${decisions.length} 项已驳回`)
    selectedPending.value = []
    batchRejectVisible.value = false
    batchRejectComment.value = ''
    await reloadLists()
  } catch (e: any) {
    ElMessage.error(e?.message || '批量驳回失败')
  }
}

// 审批流水
const stepsVisible = ref(false)
const stepsLoading = ref(false)
const stepsData = ref<Array<{ id: string; level: string; approver_id?: string; action: string; comment: string; created_at: string }>>([])

async function showSteps(row: ExpansionRequest) {
  stepsVisible.value = true
  stepsLoading.value = true
  try {
    stepsData.value = await getExpansionSteps(row.id)
  } catch { stepsData.value = [] }
  finally { stepsLoading.value = false }
}

function stepActionLabel(action: string): string {
  const map: Record<string, string> = { approve: '批准', reject: '驳回', escalate: '上报平台', cancel: '已取消' }
  return map[action] || action
}
function stepColor(action: string): string {
  const map: Record<string, string> = { approve: 'success', reject: 'danger', escalate: 'warning', cancel: 'info' }
  return map[action] || 'info'
}

const actionTitle = computed(() => {
  const map: Record<ExpansionAction, string> = {
    approve: '批准申请',
    reject: '驳回申请',
    escalate: '上报平台',
    cancel: '取消申请',
  }
  return map[actionType.value]
})

async function loadMine() {
  mineLoading.value = true
  try {
    mineList.value = await getMyExpansionRequests()
  } finally {
    mineLoading.value = false
  }
}

async function loadPending() {
  if (!canApprove.value) return
  pendingLoading.value = true
  try {
    pendingList.value = await getPendingExpansions()
  } finally {
    pendingLoading.value = false
  }
}

function openCreateDialog(prefill?: Partial<CreateExpansionPayload>) {
  createForm.value = {
    scope_type: prefill?.scope_type || String(route.query.scope_type || 'dept_daily'),
    scope_id: prefill?.scope_id || String(route.query.scope_id || '') || defaultScopeID(prefill?.scope_type || String(route.query.scope_type || 'dept_daily')),
    requested_amount: prefill?.requested_amount || Number(route.query.requested_amount || 100),
    reason: prefill?.reason || '',
  }
  createVisible.value = true
}

async function handleCreate() {
  // 自动填充 scope_id
  if (!createForm.value.scope_id) {
    createForm.value.scope_id = defaultScopeID(createForm.value.scope_type)
  }
  if (!createForm.value.scope_id) {
    ElMessage.warning('无法确定所属部门，请联系管理员')
    return
  }
  createLoading.value = true
  try {
    const created = await createExpansionRequest(createForm.value)
    mineList.value.unshift(created)
    ElMessage.success('申请已提交')
    createVisible.value = false
  } finally {
    createLoading.value = false
  }
}

function openActionDialog(item: ExpansionRequest, action: ExpansionAction) {
  actionTarget.value = item
  actionType.value = action
  actionComment.value = ''
  actionVisible.value = true
}

async function handleAction() {
  if (!actionTarget.value) return
  actionLoading.value = true
  try {
    await decideExpansion(actionTarget.value.id, actionType.value, actionComment.value || undefined)
    ElMessage.success(actionTitle.value + '成功')
    actionVisible.value = false
    await reloadLists()
  } finally {
    actionLoading.value = false
  }
}

async function handleCancel(row: ExpansionRequest) {
  await decideExpansion(row.id, 'cancel')
  ElMessage.success('已取消')
  await loadMine()
}

function goBudget(row: ExpansionRequest) {
  if (row.scope_type === 'member') return
  router.push({
    path: '/admin/org',
    query: {
      scope_type: row.scope_type,
      scope_id: row.scope_id,
      tab: 'budget',
    },
  })
}

function scopeLabel(type: string): string {
  const map: Record<string, string> = {
    dept_daily: '部门日常',
    dept_service: '部门服务',
    member: '成员额度',
    studio: '工作室',
  }
  return map[type] || type
}

function scopeColor(type: string): string {
  const map: Record<string, string> = {
    dept_daily: '#3b82f6',
    dept_service: '#8b5cf6',
    member: '#10b981',
    studio: '#f59e0b',
  }
  return map[type] || '#6b7280'
}

function formatMoney(value?: number) {
  return `$${(value || 0).toLocaleString('en-US', { maximumFractionDigits: 2 })}`
}

function formatDateTime(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

async function reloadLists() {
  await Promise.all([
    canApprove.value ? loadPending() : Promise.resolve(),
    !isAdmin.value ? loadMine() : Promise.resolve(),
  ])
}

function defaultScopeID(scopeType: string): string {
  if (scopeType === 'studio') {
    const studioRole = authStore.orgRoles.find(role => role.role === 'studio_admin')
    return studioRole?.scope_id || ''
  }
  return authStore.user?.departmentId || ''
}

onMounted(() => {
  reloadLists()
  if (route.query.scope_type && route.query.scope_id) {
    openCreateDialog()
  }
})

watch(() => createForm.value.scope_type, (scopeType) => {
  if (!createVisible.value) return
  if (route.query.scope_id) return
  createForm.value.scope_id = defaultScopeID(scopeType)
})
</script>

<style scoped>
.expansion-center {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 28px 32px;
}
.page-head {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  align-items: flex-start;
}
.page-head h1 { margin: 0; font-size: 24px; color: var(--el-text-color-primary); }
.page-head p { margin: 6px 0 0; color: var(--el-text-color-secondary); font-size: 13px; }
.kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.kpi {
  padding: 16px 18px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
}
.kpi span { display: block; color: var(--el-text-color-secondary); font-size: 12px; margin-bottom: 8px; }
.kpi strong { font-size: 24px; color: var(--el-text-color-primary); }
.center-grid {
  display: grid;
  grid-template-columns: minmax(360px, 0.9fr) minmax(0, 1.1fr);
  gap: 16px;
  align-items: start;
}
.center-grid.single-panel {
  grid-template-columns: minmax(0, 1fr);
}
.panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  background: var(--el-bg-color);
  overflow: hidden;
}
.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.panel-head h2 { margin: 0; font-size: 16px; color: var(--el-text-color-primary); }
.panel-head p { margin: 4px 0 0; font-size: 12px; color: var(--el-text-color-secondary); }
.pending-list { min-height: 260px; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.approval-card {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 12px;
  padding: 14px;
  background: var(--el-fill-color-blank);
}
.approval-top { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; }
.scope-line, .scope-cell { display: flex; align-items: center; gap: 8px; min-width: 0; }
.scope-id { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; color: var(--el-text-color-secondary); overflow: hidden; text-overflow: ellipsis; }
.amount-block { font-size: 13px; color: var(--el-text-color-secondary); white-space: nowrap; }
.amount-block strong, .amount-text { color: var(--el-color-primary); font-weight: 700; }
.reason { margin: 12px 0; color: var(--el-text-color-regular); line-height: 1.6; font-size: 13px; }
.approval-meta { display: flex; gap: 12px; color: var(--el-text-color-secondary); font-size: 12px; margin-bottom: 12px; }
.approval-actions { display: flex; gap: 8px; justify-content: flex-end; }
.muted { color: var(--el-text-color-placeholder); }
.action-context {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
}
.action-context code { color: var(--el-text-color-secondary); font-size: 12px; }
.action-context strong { margin-left: auto; color: var(--el-color-primary); }
@media (max-width: 1100px) {
  .center-grid { grid-template-columns: 1fr; }
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 720px) {
  .expansion-center { padding: 18px; }
  .page-head { flex-direction: column; }
  .kpi-strip { grid-template-columns: 1fr; }
}
</style>
