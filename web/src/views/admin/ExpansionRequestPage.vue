<template>
  <div class="expansion-center" :class="{ 'expansion-center--admin': isAdmin }">
    <!-- ── 页头 ── -->
    <header class="page-head">
      <div class="page-head-title">
        <h1>{{ pageTitle }}</h1>
        <p v-if="pageDesc" class="page-head-desc">{{ pageDesc }}</p>
      </div>
      <div class="page-actions">
        <el-button v-if="isAdmin" plain @click="router.push('/admin/org')">组织预算</el-button>
        <el-button v-if="canCreateRequest" type="primary" @click="openCreateDialog()">
          <el-icon style="margin-right:4px"><Plus /></el-icon>新建申请
        </el-button>
      </div>
    </header>

    <!-- ── KPI 数据条 ── -->
    <section class="kpi-strip">
      <div class="kpi is-primary">
        <div class="kpi-icon kpi-icon--primary">
          <el-icon><Clock /></el-icon>
        </div>
        <div class="kpi-content">
          <span>{{ primaryKpiLabel }}</span>
          <strong>{{ pendingList.length }}</strong>
        </div>
      </div>
      <div class="kpi">
        <div class="kpi-icon kpi-icon--warning">
          <el-icon><User /></el-icon>
        </div>
        <div class="kpi-content">
          <span>我发起的审批中</span>
          <strong>{{ isAdmin ? platformPendingCount : pendingMineCount }}</strong>
        </div>
      </div>
      <div class="kpi">
        <div class="kpi-icon kpi-icon--success">
          <el-icon><CircleCheck /></el-icon>
        </div>
        <div class="kpi-content">
          <span>已批准</span>
          <strong>{{ isAdmin ? approvedPendingCount : approvedMineCount }}</strong>
        </div>
      </div>
      <div class="kpi">
        <div class="kpi-icon kpi-icon--danger">
          <el-icon><CircleClose /></el-icon>
        </div>
        <div class="kpi-content">
          <span>已驳回/取消</span>
          <strong>{{ isAdmin ? closedPendingCount : closedMineCount }}</strong>
        </div>
      </div>
    </section>

    <!-- ── 主工作区 ── -->
    <section class="workspace-grid" :class="{ 'admin-workspace': isAdmin }">

      <!-- ── 左：待审批卡片 ── -->
      <div v-if="canApprove" class="panel task-panel">
        <div class="panel-head">
          <div>
            <h2>{{ pendingTitle }}
              <span v-if="pendingList.length" class="panel-count">{{ pendingList.length }}</span>
            </h2>
            <p v-if="pendingDesc">{{ pendingDesc }}</p>
          </div>
          <div class="panel-head-actions">
            <template v-if="selectedPending.length > 0">
              <span class="batch-selected-hint">已选 {{ selectedPending.length }}</span>
              <el-button size="small" type="primary" @click="batchApprove">批量批准</el-button>
              <el-button size="small" plain @click="batchRejectVisible = true">批量驳回</el-button>
              <el-button size="small" link @click="selectedPending = []">取消</el-button>
            </template>
            <el-button :loading="pendingLoading" size="small" plain @click="loadPending">
              <el-icon><Refresh /></el-icon>
            </el-button>
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
          <article
            v-for="item in visiblePending"
            :key="item.id"
            class="approval-card"
            :class="{ selected: selectedPending.includes(item.id) }"
          >
            <el-checkbox v-model="item._checked" class="card-check" @change="toggleSelect(item)" />

            <!-- 左侧金额区 -->
            <div class="card-amount-block">
              <div class="card-amount-value">
                <span v-if="item.current_amount" class="card-amount-from">
                  {{ formatMoney(item.current_amount) }}
                </span>
                <el-icon v-if="item.current_amount" class="card-amount-arrow"><ArrowRight /></el-icon>
                <strong>{{ formatMoney(item.requested_amount) }}</strong>
              </div>
              <span class="card-period-badge">{{ PERIOD_LABELS[(item.period_type as ExpansionPeriod) || 'monthly'] }}</span>
            </div>

            <!-- 右侧信息区 -->
            <div class="card-body">
              <!-- 行1：标签 + 申请人 -->
              <div class="card-row card-row--head">
                <div class="card-left">
                  <el-tag :type="scopeTagType(item.scope_type)" size="small" round>{{ scopeLabel(item.scope_type) }}</el-tag>
                  <span class="card-scope-name">{{ resolveScopeName(item) }}</span>
                  <span v-if="item.requester_name && item.scope_type !== 'member'" class="card-requester">· {{ item.requester_name }}</span>
                </div>
                <span class="card-date">{{ formatDateTime(item.created_at) }}</span>
              </div>

              <!-- 使用率 -->
              <div v-if="item.current_usage_ratio != null" class="card-usage-row">
                <span class="usage-label">当前使用率</span>
                <div class="usage-track">
                  <div
                    class="usage-fill"
                    :class="item.current_usage_ratio >= 0.8 ? 'is-danger' : item.current_usage_ratio >= 0.6 ? 'is-warning' : ''"
                    :style="{ width: Math.min(Math.round(item.current_usage_ratio * 100), 100) + '%' }"
                  />
                </div>
                <span :class="item.current_usage_ratio >= 0.8 ? 'usage-num--danger' : 'usage-num'">
                  {{ Math.round(item.current_usage_ratio * 100) }}%
                </span>
              </div>

              <!-- 申请理由 -->
              <div class="card-reason">{{ item.reason || '未填写申请理由' }}</div>

              <!-- 预算超出提示 -->
              <div v-if="isStudioAdmin && exceedsBudget(item)" class="card-budget-warn">
                <el-icon style="color:#e6a23c;vertical-align:-2px;margin-right:4px"><WarningFilled /></el-icon>
                申请额度超出工作室剩余可分配额度（{{ formatMoney(studioBudgetRemaining) }}），建议上报平台审批。
              </div>

              <!-- 操作区 -->
              <div class="card-actions">
                <el-popconfirm
                  title="确认批准该申请？"
                  confirm-button-text="批准"
                  cancel-button-text="取消"
                  @confirm="quickApprove(item)"
                >
                  <template #reference>
                    <el-button
                      size="small"
                      :type="exceedsBudget(item) ? 'default' : 'primary'"
                      :plain="exceedsBudget(item)"
                    >
                      <el-icon><Check /></el-icon> 批准
                    </el-button>
                  </template>
                </el-popconfirm>
                <el-button size="small" plain @click="openActionDialog(item, 'reject')">
                  <el-icon><Close /></el-icon> 驳回
                </el-button>
                <el-button
                  v-if="item.current_level === 'studio'"
                  size="small"
                  :type="exceedsBudget(item) ? 'warning' : 'default'"
                  :plain="!exceedsBudget(item)"
                  @click="openActionDialog(item, 'escalate')"
                >上报平台</el-button>
                <el-button size="small" link @click="showSteps(item)">审批流水</el-button>
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

      <!-- ── 右：我发起的申请 + 快捷导航 ── -->
      <aside v-if="!isAdmin" class="panel side-panel">
        <div class="panel-head">
          <div>
            <h2>我发起的申请</h2>
            <p>审批通过后由负责人完成调额。</p>
          </div>
          <el-button :loading="mineLoading" size="small" plain @click="loadMine">
            <el-icon><Refresh /></el-icon>
          </el-button>
        </div>

        <div ref="mineScrollRef" v-loading="mineLoading" class="mine-list">
          <div v-if="mineList.length === 0 && !mineLoading" class="mine-empty">
            <el-icon style="font-size:28px;color:var(--el-text-color-placeholder);margin-bottom:8px"><DocumentAdd /></el-icon>
            <p>暂无申请记录</p>
          </div>

          <div v-for="item in visibleMine" :key="item.id" class="mine-item">
            <!-- 状态色条 -->
            <div class="mine-status-bar" :class="`mine-status-bar--${item.status}`" />
            <div class="mine-item-content">
              <div class="mine-item-main">
                <el-tag :type="scopeTagType(item.scope_type)" size="small" round style="flex-shrink:0">
                  {{ scopeLabel(item.scope_type) }}
                </el-tag>
                <span class="mine-amount">
                  {{ formatMoney(item.requested_amount) }}
                  <small>{{ PERIOD_SUFFIX[(item.period_type as ExpansionPeriod) || 'monthly'] }}</small>
                </span>
                <el-tag
                  size="small"
                  :type="item.status === 'approved' ? 'success' : item.status === 'rejected' ? 'danger' : item.status === 'cancelled' ? 'info' : 'warning'"
                  style="flex-shrink:0"
                >{{ STATUS_LABELS[item.status] }}</el-tag>
              </div>

              <!-- 审批进度步骤 -->
              <div class="mine-steps">
                <div class="mine-step" :class="{ active: true, done: true }">
                  <div class="step-dot done" />
                  <span>提交</span>
                </div>
                <div class="mine-step-line" :class="{ done: item.status !== 'pending' }" />
                <div class="mine-step" :class="{ active: item.status !== 'pending', done: item.status === 'approved' || item.status === 'rejected' }">
                  <div class="step-dot" :class="{ done: item.status === 'approved' || item.status === 'rejected', active: item.status === 'pending', cancelled: item.status === 'cancelled' }" />
                  <span>审批</span>
                </div>
                <div class="mine-step-line" :class="{ done: item.status === 'approved' }" />
                <div class="mine-step" :class="{ done: item.status === 'approved' }">
                  <div class="step-dot" :class="{ done: item.status === 'approved' }" />
                  <span>调额</span>
                </div>
              </div>

              <div class="mine-item-foot">
                <span class="muted">{{ formatDateTime(item.created_at) }}</span>
                <div class="mine-actions">
                  <el-button v-if="item.status === 'approved' && item.scope_type !== 'member'" link type="primary" size="small" @click="goBudget(item)">去调额 →</el-button>
                  <el-popconfirm v-else-if="item.status === 'pending'" title="确定取消？" confirm-button-text="确定" cancel-button-text="取消" @confirm="handleCancel(item)">
                    <template #reference><el-button link type="danger" size="small">取消</el-button></template>
                  </el-popconfirm>
                  <el-button link size="small" @click="showSteps(item)">详情</el-button>
                </div>
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

    <!-- ── 新建申请弹窗（重新设计） ── -->
    <el-dialog v-model="createVisible" title="新建扩容申请" width="540px" class="create-dialog">
      <div class="create-dialog-body">
        <el-form :model="createForm" label-width="96px">
          <!-- studio_admin：固定工作室范围 -->
          <el-form-item v-if="isStudioAdmin" label="申请工作室">
            <el-input
              :value="orgStore.studios.find(s => s.id === createForm.scope_id)?.name || createForm.scope_id"
              disabled style="width: 100%"
            />
          </el-form-item>
          <!-- 多部门 dept_admin -->
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
          <!-- 单部门 dept_admin -->
          <el-form-item v-else-if="isDeptAdmin && myManagedDepts.length === 1" label="申请部门">
            <el-input :value="myManagedDepts[0].dept_name" disabled style="width: 100%" />
          </el-form-item>

          <!-- 预算类型 -->
          <el-form-item v-if="!isStudioAdmin" label="预算类型">
            <el-select v-model="createForm.scope_type" style="width: 100%">
              <el-option label="部门日常预算（个人 Key）" value="dept_daily" />
              <el-option label="部门服务预算（Service Key）" value="dept_service" />
            </el-select>
          </el-form-item>

          <!-- 调整维度 -->
          <el-form-item label="调整维度">
            <el-segmented v-model="createForm.period_type" :options="periodOptions" block style="width:100%" />
          </el-form-item>

          <!-- 申请额度 + 参考提示 -->
          <el-form-item :label="`申请额度`">
            <div style="width:100%">
              <el-input-number
                v-model="createForm.requested_amount"
                :min="0" :precision="2"
                controls-position="right"
                style="width: 100%"
              />
              <div class="amount-hint">
                <span>填写期望的{{ PERIOD_LABELS[(createForm.period_type as ExpansionPeriod) || 'monthly'] }}总额（USD），审批通过后由负责人调整。</span>
                <div v-if="createForm.requested_amount > 0" class="amount-suggestions">
                  <span class="amount-suggestion-label">快速选择：</span>
                  <el-button
                    v-for="preset in amountPresets"
                    :key="preset"
                    size="small"
                    :type="createForm.requested_amount === preset ? 'primary' : 'default'"
                    plain
                    @click="createForm.requested_amount = preset"
                  >${{ preset }}</el-button>
                </div>
              </div>
            </div>
          </el-form-item>

          <!-- 申请理由 -->
          <el-form-item label="申请理由">
            <div style="width:100%">
              <el-input
                v-model="createForm.reason"
                type="textarea"
                :rows="3"
                placeholder="说明业务背景、预计消耗和需要扩容的原因"
              />
              <div class="reason-tips">
                <span>💡 建议说明：使用场景、预计消耗量、为何需要提升</span>
              </div>
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button @click="createVisible = false">取消</el-button>
          <el-button type="primary" :loading="createLoading" @click="handleCreate">
            提交申请
          </el-button>
        </div>
      </template>
    </el-dialog>

    <!-- ── 审批操作弹窗 ── -->
    <el-dialog v-model="actionVisible" :title="actionTitle" width="440px">
      <div v-if="actionTarget" class="action-context">
        <div class="action-context-left">
          <el-tag :type="scopeTagType(actionTarget.scope_type)" size="small" round>{{ scopeLabel(actionTarget.scope_type) }}</el-tag>
          <span class="action-context-name">{{ resolveScopeName(actionTarget) }}</span>
        </div>
        <strong class="action-context-amount">{{ formatMoney(actionTarget.requested_amount) }}</strong>
      </div>
      <el-input v-model="actionComment" type="textarea" :rows="3" :placeholder="actionType === 'reject' ? '请填写驳回理由（建议填写）' : '审批意见（可选）'" />
      <template #footer>
        <el-button @click="actionVisible = false">取消</el-button>
        <el-button
          :type="actionType === 'reject' ? 'danger' : actionType === 'escalate' ? 'warning' : 'primary'"
          :loading="actionLoading"
          @click="handleAction"
        >确认{{ actionTitle }}</el-button>
      </template>
    </el-dialog>

    <!-- ── 批量驳回弹窗 ── -->
    <el-dialog v-model="batchRejectVisible" title="批量驳回" width="420px">
      <p style="margin:0 0 12px; color: var(--el-text-color-secondary); font-size: 13px;">
        驳回 <strong>{{ selectedPending.length }}</strong> 项申请，请填写统一的驳回理由：
      </p>
      <el-input v-model="batchRejectComment" type="textarea" :rows="3" placeholder="驳回理由（必填）" />
      <template #footer>
        <el-button @click="batchRejectVisible = false">取消</el-button>
        <el-button type="danger" @click="batchReject">确认驳回</el-button>
      </template>
    </el-dialog>

    <!-- ── 审批流水弹窗 ── -->
    <el-dialog v-model="stepsVisible" title="审批流水" width="500px">
      <div v-loading="stepsLoading">
        <el-timeline v-if="stepsData.length > 0">
          <el-timeline-item
            v-for="step in stepsData"
            :key="step.id"
            :timestamp="formatDateTime(step.created_at)"
            :type="stepColor(step.action)"
            placement="top"
          >
            <strong>{{ stepActionLabel(step.action) }}</strong>
            <span style="margin-left: 8px; color: var(--el-text-color-secondary); font-size: 12px;">
              {{ step.level === 'platform' ? '平台级' : '工作室级' }}
            </span>
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
import {
  WarningFilled,
  Plus,
  Clock,
  User,
  CircleCheck,
  CircleClose,
  Refresh,
  Check,
  Close,
  ArrowRight,
  DocumentAdd,
} from '@element-plus/icons-vue'
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

// 快速选择金额预设
const amountPresets = computed(() => {
  const period = createForm.value.period_type
  if (period === 'daily') return [10, 20, 50, 100]
  if (period === 'weekly') return [50, 100, 200, 500]
  return [100, 200, 500, 1000]
})

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

const myManagedDepts = computed(() => {
  const myDeptIds = new Set(
    authStore.orgRoles.filter(r => r.role === 'dept_admin').map(r => r.scope_id)
  )
  if (myDeptIds.size === 0) return []
  return orgStore.departments.filter(d => myDeptIds.has(d.dept_id))
})
const isMultiDeptAdmin = computed(() => isDeptAdmin.value && myManagedDepts.value.length > 1)
const canCreateRequest = computed(() => isDeptAdmin.value || isStudioAdmin.value)

// 工作室预算上下文
const studioBudgetTotal = ref(0)
const studioBudgetAllocated = ref(0)
const studioBudgetRemaining = computed(() => Math.max(studioBudgetTotal.value - studioBudgetAllocated.value, 0))

async function loadStudioBudget() {
  if (!isStudioAdmin.value) return
  try {
    const items = await getBudgetList(undefined, undefined, 'studio')
    const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin')
    const studioScopeId = studioRole?.scope_id || ''
    const studioItem = items.find(i => i.scope_type === 'studio' && i.scope_id === studioScopeId)
    studioBudgetTotal.value = studioItem?.amount_usd ?? 0
    studioBudgetAllocated.value = items
      .filter(i => i.scope_type === 'dept_daily' || i.scope_type === 'dept_service')
      .reduce((sum, i) => sum + (i.amount_usd ?? 0), 0)
  } catch {
    studioBudgetTotal.value = 0
    studioBudgetAllocated.value = 0
  }
}

function exceedsBudget(item: ExpansionRequest): boolean {
  if (!isStudioAdmin.value) return false
  if (item.period_type && item.period_type !== 'monthly') return false
  return (item.requested_amount ?? 0) > studioBudgetRemaining.value
}

const platformPendingCount = computed(() => pendingList.value.filter(i => i.status === 'pending').length)
const approvedPendingCount = computed(() => pendingList.value.filter(i => i.status === 'approved').length)
const closedPendingCount = computed(() => pendingList.value.filter(i => i.status === 'rejected' || i.status === 'cancelled').length)

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
  if (isDeptAdmin.value) return '成员在"我的预算"提交的个人额度申请会出现在这里。'
  return '暂无需要处理的申请。'
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
  const scopeType = isStudioAdmin.value
    ? 'studio'
    : (prefill?.scope_type || String(route.query.scope_type || 'dept_daily'))
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
  if (!createForm.value.scope_id) {
    createForm.value.scope_id = defaultScopeID(createForm.value.scope_type)
  }
  if (!createForm.value.scope_id) {
    ElMessage.warning('无法确定申请范围，请刷新页面后重试，或联系管理员确认角色配置')
    return
  }
  if (!createForm.value.reason?.trim()) {
    ElMessage.warning('请填写申请理由')
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

function resolveScopeName(item: ExpansionRequest): string {
  if (item.scope_type === 'member') return item.requester_name || item.scope_id
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
  if (authStore.effectiveRole === 'dept_admin') {
    if (myManagedDepts.value.length === 1) return myManagedDepts.value[0].dept_id
    return createForm.value.scope_id || myManagedDepts.value[0]?.dept_id || ''
  }
  return authStore.user?.departmentId || ''
}

onMounted(() => {
  reloadLists()
  loadStudioBudget()
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

/* ── 页头 ── */
.page-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
}
.page-head-title {
  border-left: 3px solid var(--el-color-primary);
  padding-left: 12px;
}
.page-head h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  letter-spacing: -.02em;
}
.page-head-desc {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.page-actions {
  display: flex;
  gap: 10px;
  flex-shrink: 0;
}

/* ── KPI 数据条 ── */
.kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.kpi {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 20px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  background: var(--el-bg-color);
  transition: box-shadow .15s;
}
.kpi:hover { box-shadow: 0 2px 12px rgba(0,0,0,.06); }
.kpi-icon {
  width: 40px; height: 40px;
  border-radius: 10px;
  display: grid; place-items: center;
  font-size: 18px;
  flex-shrink: 0;
}
.kpi-icon--primary { background: var(--el-color-primary-light-9); color: var(--el-color-primary); }
.kpi-icon--warning { background: #fff7e6; color: #f59e0b; }
.kpi-icon--success { background: var(--el-color-success-light-9); color: var(--el-color-success); }
.kpi-icon--danger  { background: var(--el-color-danger-light-9);  color: var(--el-color-danger);  }
.kpi-content { display: flex; flex-direction: column; gap: 2px; }
.kpi-content span { font-size: 12px; color: var(--el-text-color-secondary); }
.kpi-content strong { font-size: 24px; font-weight: 700; color: var(--el-text-color-primary); letter-spacing: -.03em; line-height: 1; }
.kpi.is-primary .kpi-content strong { color: var(--el-color-primary); }

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
.side-panel { position: sticky; top: 24px; }
.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.panel-head h2 { margin: 0; font-size: 15px; color: var(--el-text-color-primary); display: flex; align-items: center; gap: 6px; }
.panel-head p { margin: 3px 0 0; font-size: 12px; color: var(--el-text-color-secondary); }
.panel-head-actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.batch-selected-hint { font-size: 12px; color: var(--el-text-color-secondary); }
.panel-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px; height: 18px;
  padding: 0 5px;
  border-radius: 9px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  font-size: 11px;
  font-weight: 600;
}

/* ── Search ── */
.panel-search {
  padding: 10px 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

/* ── Pending list ── */
.pending-list {
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 580px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: var(--el-border-color) transparent;
}
.pending-list::-webkit-scrollbar { width: 4px; }
.pending-list::-webkit-scrollbar-thumb { background: var(--el-border-color); border-radius: 2px; }
.list-sentinel { height: 1px; flex-shrink: 0; }

/* ── Approval card ── */
.approval-card {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 12px;
  padding: 12px;
  background: var(--el-fill-color-blank);
  display: flex;
  gap: 10px;
  transition: border-color .15s, box-shadow .15s;
}
.approval-card:hover {
  border-color: var(--el-border-color);
  box-shadow: 0 2px 10px rgba(0,0,0,.06);
}
.approval-card.selected {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}
.card-check { padding-top: 2px; flex-shrink: 0; }

/* 左侧金额区 */
.card-amount-block {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  min-width: 88px;
  padding: 8px 10px;
  background: var(--el-color-primary-light-9);
  border-radius: 8px;
  flex-shrink: 0;
}
.card-amount-value {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  justify-content: center;
}
.card-amount-from {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  text-decoration: line-through;
}
.card-amount-arrow { font-size: 11px; color: var(--el-text-color-placeholder); }
.card-amount-value strong {
  font-size: 15px;
  font-weight: 700;
  color: var(--el-color-primary);
}
.card-period-badge {
  font-size: 11px;
  color: var(--el-color-primary);
  opacity: .75;
}

/* 右侧信息区 */
.card-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 8px; }
.card-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.card-left { display: flex; align-items: center; gap: 6px; min-width: 0; flex: 1; flex-wrap: wrap; }
.card-scope-name { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
.card-requester { font-size: 12px; color: var(--el-text-color-secondary); }
.card-date { font-size: 11px; color: var(--el-text-color-placeholder); flex-shrink: 0; }

/* 使用率 */
.card-usage-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.usage-label { font-size: 11px; color: var(--el-text-color-placeholder); flex-shrink: 0; }
.usage-track { flex: 1; max-width: 80px; height: 5px; background: var(--el-border-color-lighter); border-radius: 3px; overflow: hidden; }
.usage-fill { height: 100%; background: var(--el-color-primary); border-radius: 3px; transition: width .3s; }
.usage-fill.is-warning { background: #f59e0b; }
.usage-fill.is-danger  { background: var(--el-color-danger); }
.usage-num { font-size: 11px; color: var(--el-text-color-secondary); }
.usage-num--danger { font-size: 11px; color: var(--el-color-danger); font-weight: 600; }

.card-reason {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.card-budget-warn {
  font-size: 12px;
  color: #e6a23c;
  background: #fdf6ec;
  border: 1px solid #faecd8;
  border-radius: 6px;
  padding: 6px 10px;
  line-height: 1.5;
}
.card-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  align-items: center;
}

/* 空状态 */
.empty-approval {
  min-height: 240px;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  text-align: center; gap: 6px;
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
.empty-approval span { font-size: 12px; color: var(--el-text-color-secondary); max-width: 240px; line-height: 1.6; }

/* ── Side panel ── */
.mine-list {
  height: 380px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: var(--el-border-color) transparent;
}
.mine-list::-webkit-scrollbar { width: 4px; }
.mine-list::-webkit-scrollbar-thumb { background: var(--el-border-color); border-radius: 2px; }
.mine-empty {
  height: 100%;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  color: var(--el-text-color-placeholder); font-size: 13px;
}
.mine-empty p { margin: 0; }

.mine-item {
  display: flex;
  gap: 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
  transition: background .1s;
}
.mine-item:last-child { border-bottom: none; }
.mine-item:hover { background: var(--el-fill-color-light); }

/* 左侧状态色条 */
.mine-status-bar {
  width: 3px;
  flex-shrink: 0;
  border-radius: 2px 0 0 2px;
}
.mine-status-bar--pending  { background: #f59e0b; }
.mine-status-bar--approved { background: var(--el-color-success); }
.mine-status-bar--rejected { background: var(--el-color-danger); }
.mine-status-bar--cancelled { background: var(--el-border-color); }

.mine-item-content {
  flex: 1;
  padding: 10px 14px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.mine-item-main { display: flex; align-items: center; gap: 8px; }
.mine-amount {
  flex: 1;
  font-size: 13px; font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.mine-amount small { font-weight: 400; color: var(--el-text-color-secondary); font-size: 11px; }

/* 审批进度步骤 */
.mine-steps {
  display: flex;
  align-items: center;
  gap: 0;
  padding: 2px 0;
}
.mine-step {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
}
.mine-step span { font-size: 10px; color: var(--el-text-color-placeholder); }
.mine-step.done span { color: var(--el-color-success); }
.mine-step.active span { color: var(--el-color-primary); }
.step-dot {
  width: 8px; height: 8px;
  border-radius: 50%;
  background: var(--el-border-color);
  transition: background .2s;
}
.step-dot.done   { background: var(--el-color-success); }
.step-dot.active { background: var(--el-color-primary); box-shadow: 0 0 0 3px var(--el-color-primary-light-7); }
.step-dot.cancelled { background: var(--el-border-color); }
.mine-step-line {
  flex: 1;
  height: 2px;
  background: var(--el-border-color-lighter);
  min-width: 24px;
  margin-bottom: 13px;
  transition: background .2s;
}
.mine-step-line.done { background: var(--el-color-success); }

.mine-item-foot { display: flex; align-items: center; justify-content: space-between; }
.mine-actions { display: flex; gap: 4px; }
.side-nav { padding: 12px 16px; border-top: 1px solid var(--el-border-color-lighter); display: flex; flex-direction: column; gap: 8px; }

/* ── 新建申请弹窗 ── */
.create-dialog-body { padding: 0 4px; }
.amount-hint {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}
.amount-suggestions {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  flex-wrap: wrap;
}
.amount-suggestion-label { font-size: 12px; color: var(--el-text-color-placeholder); }
.reason-tips {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

/* ── 审批操作弹窗 ── */
.action-context {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 14px;
  padding: 12px 14px;
  border-radius: 10px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
}
.action-context-left { display: flex; align-items: center; gap: 8px; }
.action-context-name { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
.action-context-amount { font-size: 16px; font-weight: 700; color: var(--el-color-primary); }

/* ── Misc ── */
.muted { color: var(--el-text-color-placeholder); font-size: 12px; }

/* ── 响应式 ── */
@media (max-width: 1100px) {
  .workspace-grid,
  .workspace-grid.admin-workspace { grid-template-columns: 1fr; }
  .side-panel { position: static; }
  .mine-list { height: 280px; }
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .pending-list { max-height: 420px; }
}
@media (max-width: 720px) {
  .expansion-center { padding: 16px; }
  .page-head { flex-direction: column; align-items: flex-start; gap: 12px; }
  .page-actions { width: 100%; }
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .kpi { padding: 12px 14px; }
  .kpi-content strong { font-size: 20px; }
  .approval-card { flex-wrap: wrap; }
  .card-amount-block { min-width: unset; width: 100%; flex-direction: row; justify-content: space-between; }
}
@media (max-width: 480px) {
  .expansion-center { padding: 12px; }
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
  .kpi-content strong { font-size: 18px; }
  .page-head h1 { font-size: 18px; }
}
</style>
