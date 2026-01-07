<template>
  <div class="expansion-center" :class="{ 'expansion-center--admin': isAdmin }">
    <header class="page-head">
      <div class="page-head-title">
        <h1>{{ pageTitle }}</h1>
      </div>
      <div class="page-actions">
        <el-button v-if="isAdmin" plain @click="router.push('/admin/org')">组织预算</el-button>
        <el-button v-if="canCreateRequest" type="primary" @click="openCreateDialog()">新建申请</el-button>
      </div>
    </header>

    <section class="kpi-strip">
      <div class="kpi is-primary">
        <span>{{ primaryKpiLabel }}</span>
        <strong>{{ pendingList.length }}</strong>
      </div>
      <div class="kpi">
        <span>我发起的审批中</span>
        <strong>{{ isAdmin ? platformPendingCount : pendingMineCount }}</strong>
      </div>
      <div class="kpi">
        <span>已批准</span>
        <strong>{{ isAdmin ? approvedPendingCount : approvedMineCount }}</strong>
      </div>
      <div class="kpi">
        <span>已驳回/取消</span>
        <strong>{{ isAdmin ? closedPendingCount : closedMineCount }}</strong>
      </div>
    </section>

    <section class="workspace-grid" :class="{ 'admin-workspace': isAdmin }">

      <!-- ── 左：待审批卡片 ── -->
      <div v-if="canApprove" class="panel task-panel">
        <div class="panel-head">
          <div>
            <h2>{{ pendingTitle }} <span v-if="pendingList.length" class="panel-count">{{ pendingList.length }}</span></h2>
            <p v-if="pendingDesc">{{ pendingDesc }}</p>
          </div>
          <div style="display:flex;gap:8px;align-items:center;flex-shrink:0">
            <span v-if="selectedPending.length > 0" class="batch-selected-hint">已选 {{ selectedPending.length }}</span>
            <el-button v-if="selectedPending.length > 0" size="small" type="primary" @click="batchApprove">批量批准</el-button>
            <el-button v-if="selectedPending.length > 0" size="small" plain @click="batchRejectVisible = true">批量驳回</el-button>
            <el-button v-if="selectedPending.length > 0" size="small" link @click="selectedPending = []">取消</el-button>
            <el-button :loading="pendingLoading" size="small" plain @click="loadPending">刷新</el-button>
          </div>
        </div>
        <!-- 搜索栏 -->
        <div class="panel-search">
          <el-input
            v-model="pendingSearch"
            placeholder="搜索申请人姓名…"
            clearable size="small"
            prefix-icon="Search"
          />
        </div>

        <div ref="pendingScrollRef" v-loading="pendingLoading" class="pending-list">
          <article v-for="item in visiblePending" :key="item.id" class="approval-card" :class="{ selected: selectedPending.includes(item.id) }">
            <el-checkbox v-model="item._checked" class="card-check" @change="toggleSelect(item)" />
            <div class="card-body">
              <!-- 行1：类型标签 + 申请人 + 申请额度 -->
              <div class="card-row card-row--head">
                <div class="card-left">
                  <el-tag :type="scopeTagType(item.scope_type)" size="small">{{ scopeLabel(item.scope_type) }}</el-tag>
                  <span class="card-period-label">{{ PERIOD_LABELS[(item.period_type as ExpansionPeriod) || 'monthly'] }}</span>
                  <!-- 申请来源：scope 可读名称 + 申请人（member 类型时 scope 本身就是申请人，不重复） -->
                  <span class="card-scope-name">{{ resolveScopeName(item) }}</span>
                  <span v-if="item.requester_name && item.scope_type !== 'member'" class="card-requester">· {{ item.requester_name }}</span>
                </div>
                <div class="card-amount">
                  <span v-if="item.current_amount" class="card-amount-from">{{ formatMoney(item.current_amount) }} →</span>
                  <strong>{{ formatMoney(item.requested_amount) }}{{ PERIOD_SUFFIX[(item.period_type as ExpansionPeriod) || 'monthly'] }}</strong>
                </div>
              </div>
              <!-- 行2：申请理由 -->
              <div class="card-reason">{{ item.reason || '未填写申请理由' }}</div>
              <!-- 行2.5：工作室预算不足提示（studio_admin 专用） -->
              <div v-if="isStudioAdmin && exceedsBudget(item)" class="card-budget-warn">
                <el-icon style="color:#e6a23c;font-size:13px;vertical-align:-2px;margin-right:4px;"><WarningFilled /></el-icon>
                申请额度（{{ formatMoney(item.requested_amount) }}）超出工作室剩余可分配额度（{{ formatMoney(studioBudgetRemaining) }}），直接批准可能导致超支，建议上报平台审批。
              </div>
              <!-- 行3：使用率进度 + 时间 + 操作 -->
              <div class="card-row card-row--foot">
                <div class="card-meta">
                  <div v-if="item.current_usage_ratio != null" class="card-usage">
                    <div class="usage-track">
                      <div class="usage-fill" :class="item.current_usage_ratio >= 0.8 ? 'is-danger' : ''" :style="{ width: Math.min(Math.round(item.current_usage_ratio * 100), 100) + '%' }" />
                    </div>
                    <span :class="item.current_usage_ratio >= 0.8 ? 'usage-num--danger' : 'usage-num'">{{ Math.round(item.current_usage_ratio * 100) }}%</span>
                  </div>
                  <span class="card-date">{{ formatDateTime(item.created_at) }}</span>
                </div>
                <div class="card-actions">
                  <!-- 批准：直接 popconfirm，无需打开 dialog -->
                  <el-popconfirm
                    title="确认批准该申请？"
                    confirm-button-text="批准"
                    cancel-button-text="取消"
                    @confirm="quickApprove(item)"
                  >
                    <template #reference>
                      <el-button size="small" :type="exceedsBudget(item) ? 'default' : 'primary'" :plain="exceedsBudget(item)">批准</el-button>
                    </template>
                  </el-popconfirm>
                  <el-button size="small" plain @click="openActionDialog(item, 'reject')">驳回</el-button>
                  <el-button
                    v-if="item.current_level === 'studio'"
                    size="small"
                    :type="exceedsBudget(item) ? 'warning' : 'default'"
                    :plain="!exceedsBudget(item)"
                    @click="openActionDialog(item, 'escalate')"
                  >上报平台</el-button>
                </div>
              </div>
            </div>
          </article>

          <div ref="pendingSentinelRef" class="list-sentinel" />
          <div v-if="!pendingLoading && pendingList.length === 0" class="empty-approval">
            <div class="empty-inbox">✓</div>
            <strong>暂无待处理申请</strong>
            <span>{{ emptyDesc }}</span>
          </div>
        </div>
      </div>

      <!-- ── 右：我发起的申请（紧凑列表）+ 快捷导航 ── -->
      <aside v-if="!isAdmin" class="panel side-panel">
        <div class="panel-head">
          <div>
            <h2>我发起的申请</h2>
            <p>审批通过后由负责人完成调额。</p>
          </div>
          <el-button :loading="mineLoading" size="small" plain @click="loadMine">刷新</el-button>
        </div>

        <div ref="mineScrollRef" v-loading="mineLoading" class="mine-list">
          <div v-if="mineList.length === 0 && !mineLoading" class="mine-empty">暂无申请记录</div>
          <div v-for="item in visibleMine" :key="item.id" class="mine-item">
            <div class="mine-item-main">
              <el-tag :type="scopeTagType(item.scope_type)" size="small" style="flex-shrink:0">{{ scopeLabel(item.scope_type) }}</el-tag>
              <span class="mine-amount">{{ formatMoney(item.requested_amount) }}{{ PERIOD_SUFFIX[(item.period_type as ExpansionPeriod) || 'monthly'] }}</span>
              <el-tag size="small" :type="item.status === 'approved' ? 'success' : item.status === 'rejected' ? 'danger' : item.status === 'cancelled' ? 'info' : 'warning'">{{ STATUS_LABELS[item.status] }}</el-tag>
            </div>
            <div class="mine-item-foot">
              <span class="muted">{{ formatDateTime(item.created_at) }}</span>
              <div class="mine-actions">
                <el-button v-if="item.status === 'approved' && item.scope_type !== 'member'" link type="primary" size="small" @click="goBudget(item)">去调额</el-button>
                <el-popconfirm v-else-if="item.status === 'pending'" title="确定取消？" confirm-button-text="确定" cancel-button-text="取消" @confirm="handleCancel(item)">
                  <template #reference><el-button link type="danger" size="small">取消</el-button></template>
                </el-popconfirm>
                <el-button link size="small" @click="showSteps(item)">详情</el-button>
              </div>
            </div>
          </div>
          <div ref="mineSentinelRef" class="list-sentinel" />
        </div>

        <!-- 快捷导航 -->
        <div class="side-nav">
          <el-button v-if="isStudioAdmin" type="primary" plain size="small" style="width:100%" @click="router.push('/admin/org')">组织预算管理</el-button>
          <el-button plain size="small" style="width:100%" @click="router.push('/admin/budget')">查看预算总览</el-button>
        </div>
      </aside>
    </section>

    <el-dialog v-model="createVisible" title="新建扩容申请" width="520px">
      <el-form :model="createForm" label-width="96px">
        <!-- studio_admin：固定工作室范围，只读展示 -->
        <el-form-item v-if="isStudioAdmin" label="申请工作室">
          <el-input
            :value="orgStore.studios.find(s => s.id === createForm.scope_id)?.name || createForm.scope_id"
            disabled
            style="width: 100%"
          />
        </el-form-item>
        <!-- 多部门 dept_admin：显示部门选择器 -->
        <el-form-item v-else-if="isMultiDeptAdmin" label="申请部门">
          <el-select v-model="createForm.scope_id" style="width: 100%">
            <el-option
              v-for="dept in myManagedDepts"
              :key="dept.dept_id"
              :label="`${dept.dept_name}（${dept.dept_id}）`"
              :value="dept.dept_id"
            />
          </el-select>
        </el-form-item>
        <!-- 单部门 dept_admin：只读展示 -->
        <el-form-item v-else-if="isDeptAdmin && myManagedDepts.length === 1" label="申请部门">
          <el-input :value="myManagedDepts[0].dept_name" disabled style="width: 100%" />
        </el-form-item>
        <el-form-item label="预算类型">
          <el-select v-model="createForm.scope_type" style="width: 100%">
            <!-- studio_admin 只允许发起工作室级别扩容；部门级由 dept_admin 发起 -->
            <template v-if="isStudioAdmin">
              <el-option label="工作室月预算" value="studio" />
            </template>
            <template v-else>
              <el-option label="部门日常预算（个人 Key）" value="dept_daily" />
              <el-option label="部门服务预算（Service Key）" value="dept_service" />
            </template>
          </el-select>
        </el-form-item>
        <el-form-item label="调整维度">
          <el-segmented v-model="createForm.period_type" :options="periodOptions" block style="width:100%" />
        </el-form-item>
        <el-form-item label="申请额度（USD）">
          <el-input-number v-model="createForm.requested_amount" :min="0" :precision="2" controls-position="right" style="width: 100%" />
          <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px;">填写期望的{{ PERIOD_LABELS[(createForm.period_type as ExpansionPeriod) || 'monthly'] }}总额（USD），审批通过后由负责人调整。</div>
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
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { useOrgStore } from '@/stores/org'
import {
  createExpansionRequest,
  decideExpansion,
  batchDecideExpansion,
  getMyExpansionRequests,
  getPendingExpansions,
  getExpansionSteps,
  getBudgetList,
} from '@/services/control-plane'
import type { CreateExpansionPayload, ExpansionAction, ExpansionPeriod, ExpansionRequest } from '@/types/expansion'
import { LEVEL_LABELS, PERIOD_LABELS, PERIOD_SUFFIX, STATUS_COLORS, STATUS_LABELS } from '@/types/expansion'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const orgStore = useOrgStore()


const mineList = ref<ExpansionRequest[]>([])
const mineLoading = ref(false)
const pendingList = ref<ExpansionRequest[]>([])
const pendingLoading = ref(false)

// ── 搜索 & 滚动分页 ──
const pendingSearch = ref('')
const PENDING_PAGE_SIZE = 6
const pendingPage = ref(1)
const pendingScrollRef = ref<HTMLElement | null>(null)
const pendingSentinelRef = ref<HTMLElement | null>(null)
let pendingObserver: IntersectionObserver | null = null
let pendingSearchTimer: ReturnType<typeof setTimeout> | null = null

const visiblePending = computed(() => pendingList.value.slice(0, pendingPage.value * PENDING_PAGE_SIZE))
const hasMorePending = computed(() => visiblePending.value.length < pendingList.value.length)

// 搜索框输入防抖 300ms 后调接口
watch(pendingSearch, () => {
  if (pendingSearchTimer) clearTimeout(pendingSearchTimer)
  pendingSearchTimer = setTimeout(() => {
    pendingPage.value = 1
    loadPending()
  }, 300)
})

function setupPendingObserver() {
  pendingObserver?.disconnect()
  if (!pendingSentinelRef.value) return
  pendingObserver = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && hasMorePending.value) pendingPage.value++
  }, { root: pendingScrollRef.value, threshold: 0.1 })
  pendingObserver.observe(pendingSentinelRef.value)
}
watch(pendingSentinelRef, el => { if (el) setupPendingObserver() }, { flush: 'post' })
watch(pendingList, () => { pendingPage.value = 1 }, { flush: 'post' })

const MINE_PAGE_SIZE = 8
const minePage = ref(1)
const mineScrollRef = ref<HTMLElement | null>(null)
const mineSentinelRef = ref<HTMLElement | null>(null)
let mineObserver: IntersectionObserver | null = null

const visibleMine = computed(() => mineList.value.slice(0, minePage.value * MINE_PAGE_SIZE))
const hasMoreMine = computed(() => visibleMine.value.length < mineList.value.length)

function setupMineObserver() {
  mineObserver?.disconnect()
  if (!mineSentinelRef.value) return
  mineObserver = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && hasMoreMine.value) minePage.value++
  }, { root: mineScrollRef.value, threshold: 0.1 })
  mineObserver.observe(mineSentinelRef.value)
}
watch(mineSentinelRef, el => { if (el) setupMineObserver() }, { flush: 'post' })
watch(mineList, () => { minePage.value = 1 }, { flush: 'post' })

const createVisible = ref(false)
const createLoading = ref(false)
const createForm = ref<CreateExpansionPayload>({
  scope_type: 'dept_daily',
  scope_id: '',
  period_type: 'monthly',
  requested_amount: 100,
  reason: '',
})

const periodOptions = [
  { label: '日额度', value: 'daily' },
  { label: '周额度', value: 'weekly' },
  { label: '月额度', value: 'monthly' },
]

const actionVisible = ref(false)
const actionLoading = ref(false)
const actionType = ref<ExpansionAction>('approve')
const actionTarget = ref<ExpansionRequest | null>(null)
const actionComment = ref('')

const pendingMineCount = computed(() => mineList.value.filter(i => i?.status === 'pending').length)
const approvedMineCount = computed(() => mineList.value.filter(i => i?.status === 'approved').length)
const closedMineCount = computed(() => mineList.value.filter(i => i?.status === 'rejected' || i?.status === 'cancelled').length)
const isAdmin = computed(() => authStore.effectiveRole === 'admin')
const isStudioAdmin = computed(() => authStore.effectiveRole === 'studio_admin')
const isDeptAdmin = computed(() => authStore.effectiveRole === 'dept_admin')
const canApprove = computed(() => isAdmin.value || isStudioAdmin.value || isDeptAdmin.value)

// 当前用户管辖的部门列表（dept_admin 可能管多个部门）
const myManagedDepts = computed(() => {
  const myDeptIds = new Set(
    authStore.orgRoles.filter(r => r.role === 'dept_admin').map(r => r.scope_id)
  )
  if (myDeptIds.size === 0) return []
  return orgStore.departments.filter(d => myDeptIds.has(d.dept_id))
})
const isMultiDeptAdmin = computed(() => isDeptAdmin.value && myManagedDepts.value.length > 1)
const canCreateRequest = computed(() => isDeptAdmin.value || isStudioAdmin.value)

// ---- 工作室预算上下文（studio_admin 专用） ----
// studioBudgetTotal: 工作室被分配的总月额度
// studioBudgetAllocated: 已向下分配给所有部门的月额度之和
const studioBudgetTotal = ref(0)
const studioBudgetAllocated = ref(0)
const studioBudgetRemaining = computed(() => Math.max(studioBudgetTotal.value - studioBudgetAllocated.value, 0))

async function loadStudioBudget() {
  if (!isStudioAdmin.value) return
  try {
    const items = await getBudgetList(undefined, undefined, 'studio')
    const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin')
    const studioScopeId = studioRole?.scope_id || ''
    // 工作室自身的总额度
    const studioItem = items.find(i => i.scope_type === 'studio' && i.scope_id === studioScopeId)
    studioBudgetTotal.value = studioItem?.amount_usd ?? 0
    // 已分配给所有部门的总额（dept_daily + dept_service 都计入）
    studioBudgetAllocated.value = items
      .filter(i => i.scope_type === 'dept_daily' || i.scope_type === 'dept_service')
      .reduce((sum, i) => sum + (i.amount_usd ?? 0), 0)
  } catch {
    studioBudgetTotal.value = 0
    studioBudgetAllocated.value = 0
  }
}

/** 判断某条申请是否超过工作室剩余可用月额度 */
function exceedsBudget(item: ExpansionRequest): boolean {
  if (!isStudioAdmin.value) return false
  if (item.period_type && item.period_type !== 'monthly') return false
  return (item.requested_amount ?? 0) > studioBudgetRemaining.value
}
const platformPendingCount = computed(() => pendingList.value.filter(i => i.status === 'pending').length)
const approvedPendingCount = computed(() => pendingList.value.filter(i => i.status === 'approved').length)
const closedPendingCount = computed(() => pendingList.value.filter(i => i.status === 'rejected' || i.status === 'cancelled').length)
const roleKicker = computed(() => {
  if (isAdmin.value) return '审批工作台'
  if (isStudioAdmin.value) return '工作室治理'
  if (isDeptAdmin.value) return '部门治理'
  return '扩容申请'
})
const pageTitle = computed(() => {
  if (isAdmin.value) return '平台扩容审批'
  if (isStudioAdmin.value) return '工作室扩容审批'
  if (isDeptAdmin.value) return '部门额度审批'
  return '扩容申请中心'
})
const pageDesc = computed(() => {
  if (isAdmin.value) return ''
  if (isStudioAdmin.value) return '审批下属部门申请，并在工作室预算不足时上报平台。'
  if (isDeptAdmin.value) return '审批成员个人额度申请，并跟踪部门预算扩容进度。'
  return '预算不足时发起申请；负责人在这里审批并回到组织预算页完成调额。'
})
const primaryKpiLabel = computed(() => {
  if (isAdmin.value) return '平台待审批'
  if (isStudioAdmin.value) return '部门待审批'
  if (isDeptAdmin.value) return '成员待审批'
  return '待我审批'
})
const pendingTitle = computed(() => primaryKpiLabel.value)
const pendingDesc = computed(() => {
  if (isAdmin.value) return ''
  if (isStudioAdmin.value) return '处理部门预算池扩容，必要时上报平台。'
  if (isDeptAdmin.value) return '处理成员个人额度申请，驳回时请填写明确理由。'
  return '处理下级扩容诉求。'
})
const emptyDesc = computed(() => {
  if (isAdmin.value) return '平台级申请会在工作室上报后出现在这里。'
  if (isStudioAdmin.value) return '部门负责人提交的申请会出现在这里。'
  if (isDeptAdmin.value) return '成员在“我的预算”提交的个人额度申请会出现在这里。'
  return '暂无需要处理的申请。'
})
const contextTitle = computed(() => '下一步')
const contextDesc = computed(() => '审批和申请的处理边界。')
const contextItems = computed(() => {
  if (isStudioAdmin.value) {
    return [
      { title: '审批部门申请', desc: '只显示自己工作室下属部门提交的扩容。' },
      { title: '可上报平台', desc: '当工作室预算不足时，将申请升级到平台。' },
      { title: '可发起工作室扩容', desc: '工作室预算不足时由平台管理员审批。' },
    ]
  }
  return [
    { title: '审批成员申请', desc: '只显示自己部门成员提交的个人额度申请。' },
    { title: '发起部门扩容', desc: '部门预算池不足时向工作室负责人申请。' },
    { title: '关注审批流水', desc: '申请状态变化会保留在详情中。' },
  ]
})

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
    pendingList.value = await getPendingExpansions(pendingSearch.value.trim() || undefined)
  } finally {
    pendingLoading.value = false
  }
}

function openCreateDialog(prefill?: Partial<CreateExpansionPayload>) {
  // studio_admin 只发工作室级别申请，scope_type 固定为 studio
  const scopeType = isStudioAdmin.value
    ? 'studio'
    : (prefill?.scope_type || String(route.query.scope_type || 'dept_daily'))
  // 多部门 dept_admin：默认选第一个管辖部门
  const defaultDeptId = isMultiDeptAdmin.value
    ? (myManagedDepts.value[0]?.dept_id || '')
    : defaultScopeID(scopeType)
  createForm.value = {
    scope_type: scopeType,
    scope_id: prefill?.scope_id || String(route.query.scope_id || '') || defaultDeptId,
    period_type: (prefill?.period_type as ExpansionPeriod) || 'monthly',
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
    ElMessage.warning('无法确定申请范围，请刷新页面后重试，或联系管理员确认角色配置')
    return
  }
  createLoading.value = true
  try {
    const created = await createExpansionRequest(createForm.value)
    if (created) mineList.value.unshift(created)
    ElMessage.success('申请已提交')
    createVisible.value = false
  } finally {
    createLoading.value = false
  }
}

async function quickApprove(item: ExpansionRequest) {
  try {
    await decideExpansion(item.id, 'approve')
    ElMessage.success('已批准')
    await reloadLists()
  } catch (e: any) {
    ElMessage.error(e?.message || '操作失败')
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

// 将 scope_id（UUID/dept_id）解析为可读名称
function resolveScopeName(item: ExpansionRequest): string {
  if (item.scope_type === 'member') {
    // 成员请求自己的扩容，scope_id 就是该用户 ID，直接用 requester_name
    return item.requester_name || item.scope_id
  }
  if (item.scope_type === 'studio') {
    const studio = orgStore.studios.find(s => s.id === item.scope_id)
    if (studio) return studio.name
  }
  if (item.scope_type === 'dept_daily' || item.scope_type === 'dept_service') {
    const dept = orgStore.departments.find(d => d.dept_id === item.scope_id)
    if (dept) return dept.dept_name
  }
  return item.scope_id
}

function scopeTagType(type: string): string {
  const map: Record<string, string> = {
    dept_daily: 'primary',
    dept_service: 'info',
    member: 'success',
    studio: 'warning',
  }
  return map[type] ?? 'info'
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
    const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin')
    return studioRole?.scope_id || ''
  }
  // dept_daily / dept_service：都用部门 ID
  if (authStore.effectiveRole === 'dept_admin') {
    // 多部门时使用当前已选的 scope_id（不自动覆盖），单部门时直接填入
    if (myManagedDepts.value.length === 1) return myManagedDepts.value[0].dept_id
    return createForm.value.scope_id || myManagedDepts.value[0]?.dept_id || ''
  }
  return authStore.user?.departmentId || ''
}

onMounted(() => {
  reloadLists()
  loadStudioBudget()
  // dept_admin 管多部门时需要部门名称
  if (isDeptAdmin.value) orgStore.fetchDepartments().catch(() => {})
  if (route.query.scope_type && route.query.scope_id) {
    openCreateDialog()
  }
})
onUnmounted(() => {
  pendingObserver?.disconnect()
  mineObserver?.disconnect()
})

watch(() => createForm.value.scope_type, (scopeType) => {
  if (!createVisible.value) return
  if (route.query.scope_id) return
  // 多部门 dept_admin 切换类型时 scope_id 保持当前选择的部门不变
  if (isMultiDeptAdmin.value) return
  createForm.value.scope_id = defaultScopeID(scopeType)
})
</script>

<style scoped>
.expansion-center {
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 24px 28px;
}
.expansion-center--admin {
  padding: 24px 28px;
}
.page-head {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  align-items: center;
  padding-bottom: 4px;
}
.page-head-title {
  border-left: 3px solid var(--el-color-primary);
  padding-left: 12px;
}
.page-head h1 { margin: 0; font-size: 22px; font-weight: 700; color: var(--el-text-color-primary); letter-spacing: -.02em; }
.page-actions {
  display: flex;
  gap: 10px;
  flex-shrink: 0;
}
.kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1px;
  overflow: hidden;
  border: 1px solid var(--el-border-color-light);
  border-radius: 16px;
  background: var(--el-border-color-lighter);
}
.kpi {
  padding: 14px 18px;
  border: none;
  border-radius: 0;
  background: var(--el-bg-color);
}
.kpi.is-primary {
  background: var(--el-bg-color);
}
.kpi span { display: block; color: var(--el-text-color-secondary); font-size: 12px; margin-bottom: 8px; }
.kpi strong { font-size: 24px; color: var(--el-text-color-primary); letter-spacing: -.03em; }
.kpi.is-primary strong { color: var(--el-color-primary); }
/* ── Grid ── */
.workspace-grid {
  display: grid;
  grid-template-columns: 1fr 340px;
  gap: 16px;
  align-items: start;
}
.workspace-grid.admin-workspace { grid-template-columns: minmax(0, 1fr); }

/* ── Panel ── */
.panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 16px;
  background: var(--el-bg-color);
  overflow: hidden;
}
.task-panel { min-height: 420px; }
.side-panel {
  position: sticky;
  top: 24px;
}
.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.panel-head h2 { margin: 0; font-size: 15px; color: var(--el-text-color-primary); }
.panel-head p { margin: 3px 0 0; font-size: 12px; color: var(--el-text-color-secondary); }
.batch-selected-hint { font-size: 12px; color: var(--el-text-color-secondary); }

/* ── Search bar ── */
.panel-search {
  padding: 10px 16px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding-bottom: 10px;
}
.panel-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px;
  height: 18px;
  padding: 0 5px;
  border-radius: 9px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  font-size: 11px;
  font-weight: 600;
  margin-left: 6px;
  vertical-align: middle;
}

/* ── Pending list ── */
.pending-list {
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 560px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: var(--el-border-color) transparent;
}
.pending-list::-webkit-scrollbar { width: 4px; }
.pending-list::-webkit-scrollbar-thumb { background: var(--el-border-color); border-radius: 2px; }
.list-sentinel { height: 1px; flex-shrink: 0; }

/* ── Approval card (redesigned) ── */
.approval-card {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  padding: 10px 12px;
  background: var(--el-fill-color-blank);
  display: flex;
  gap: 8px;
  transition: border-color .15s, box-shadow .15s;
}
.approval-card:hover { border-color: var(--el-border-color); box-shadow: 0 2px 8px rgba(0,0,0,.06); }
.approval-card.selected { border-color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
.card-check { padding-top: 2px; flex-shrink: 0; }
.card-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.card-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.card-row--foot { }
.card-left { display: flex; align-items: center; gap: 6px; min-width: 0; flex: 1; }
.card-period-label { font-size: 11px; color: var(--el-text-color-placeholder); background: var(--el-fill-color); padding: 1px 6px; border-radius: 4px; flex-shrink: 0; }
.card-scope-name { font-size: 12px; font-weight: 600; color: var(--el-text-color-primary); flex-shrink: 0; }
.card-requester { font-size: 12px; color: var(--el-text-color-secondary); flex-shrink: 0; }
.card-reason { font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.5; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
.card-budget-warn { font-size: 12px; color: #e6a23c; background: #fdf6ec; border: 1px solid #faecd8; border-radius: 4px; padding: 6px 10px; line-height: 1.5; margin-top: 4px; }
.card-amount { font-size: 13px; white-space: nowrap; flex-shrink: 0; }
.card-amount-from { color: var(--el-text-color-placeholder); margin-right: 3px; }
.card-amount strong { color: var(--el-color-primary); font-size: 14px; font-weight: 700; }
.card-meta { display: flex; align-items: center; gap: 10px; }
.card-date { font-size: 11px; color: var(--el-text-color-placeholder); }
.card-actions { display: flex; gap: 6px; flex-shrink: 0; }

/* 使用率小进度条 */
.card-usage { display: flex; align-items: center; gap: 6px; }
.usage-track { width: 48px; height: 4px; background: var(--el-border-color-lighter); border-radius: 2px; overflow: hidden; }
.usage-fill { height: 100%; background: var(--el-color-primary); border-radius: 2px; transition: width .3s; }
.usage-fill.is-danger { background: var(--el-color-danger); }
.usage-num { font-size: 11px; color: var(--el-text-color-secondary); }
.usage-num--danger { font-size: 11px; color: var(--el-color-danger); font-weight: 600; }

/* 空状态 */
.empty-approval {
  min-height: 240px;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  text-align: center; gap: 6px; color: var(--el-text-color-secondary);
}
.empty-inbox {
  width: 52px; height: 52px; border-radius: 50%;
  display: grid; place-items: center;
  margin-bottom: 8px;
  background: var(--el-color-success-light-9);
  color: var(--el-color-success);
  font-size: 22px;
}
.empty-approval strong { color: var(--el-text-color-primary); font-size: 14px; }
.empty-approval span { font-size: 12px; max-width: 240px; line-height: 1.6; }

/* ── Side panel (right) ── */
.mine-list {
  padding: 0;
  height: 340px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: var(--el-border-color) transparent;
}
.mine-list::-webkit-scrollbar { width: 4px; }
.mine-list::-webkit-scrollbar-thumb { background: var(--el-border-color); border-radius: 2px; }
.mine-empty { padding: 32px 16px; text-align: center; color: var(--el-text-color-placeholder); font-size: 13px; }
.mine-item {
  padding: 10px 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  display: flex; flex-direction: column; gap: 6px;
}
.mine-item:last-child { border-bottom: none; }
.mine-item-main { display: flex; align-items: center; gap: 8px; }
.mine-amount { flex: 1; font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mine-item-foot { display: flex; align-items: center; justify-content: space-between; }
.mine-actions { display: flex; gap: 4px; }
.side-nav { padding: 12px 16px; border-top: 1px solid var(--el-border-color-lighter); display: flex; flex-direction: column; gap: 8px; }

/* ── Misc ── */
.muted { color: var(--el-text-color-placeholder); font-size: 12px; }
.scope-line, .scope-cell { display: flex; align-items: center; gap: 8px; min-width: 0; }
.amount-block strong, .amount-text { color: var(--el-color-primary); font-weight: 700; }
.action-context {
  display: flex; align-items: center; gap: 8px;
  margin-bottom: 14px; padding: 10px 12px;
  border-radius: 8px; background: var(--el-fill-color-light);
}
.action-context strong { margin-left: auto; color: var(--el-color-primary); }

/* ≤1100px：双栏 → 单栏 */
@media (max-width: 1100px) {
  .workspace-grid,
  .workspace-grid.admin-workspace { grid-template-columns: 1fr; }
  .side-panel { position: static; }
  .mine-list { height: 280px; }
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .pending-list { max-height: 420px; }
}

/* ≤720px */
@media (max-width: 720px) {
  .expansion-center { padding: 16px; }

  /* header */
  .page-head { flex-direction: column; align-items: flex-start; gap: 12px; }
  .page-actions { width: 100%; justify-content: flex-start; }

  /* KPI */
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .kpi { padding: 10px 12px; }
  .kpi strong { font-size: 20px; }

  /* panel head：批量操作换行 */
  .panel-head { flex-wrap: wrap; row-gap: 8px; }
  .panel-head > div:last-child { width: 100%; justify-content: flex-start; flex-wrap: wrap; }

  /* 卡片：头行允许换行 */
  .card-row--head { flex-wrap: wrap; gap: 6px; }
  .card-amount { width: 100%; text-align: left; }

  /* 卡片底行 */
  .card-row--foot { flex-wrap: wrap; row-gap: 8px; }
  .card-actions { width: 100%; justify-content: flex-end; }
  .approval-card { padding: 10px 12px; }
}

/* ≤480px */
@media (max-width: 480px) {
  .expansion-center { padding: 12px; }
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1px; }
  .kpi strong { font-size: 18px; }
  .page-head h1 { font-size: 18px; }
  .pending-list { max-height: 360px; }
}
</style>
