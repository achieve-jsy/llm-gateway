<template>
  <div class="key-request-page">
    <header class="page-header">
      <div>
        <h1>Key 申请审批</h1>
        <p class="page-desc">审批 Key 创建申请。</p>
      </div>
      <div class="header-actions">
        <el-button @click="load" :loading="loading">刷新</el-button>
      </div>
    </header>

    <div class="approval-summary approval-summary--three">
      <div class="approval-stat approval-stat--warn">
        <span class="approval-stat__value">{{ pendingCount }}</span>
        <span class="approval-stat__label">待审批</span>
      </div>
      <div
        class="approval-stat approval-stat--budget"
        :class="{ 'is-active': pendingBudgetOnly, 'is-disabled': pendingBudgetCount <= 0 }"
        :role="pendingBudgetCount > 0 ? 'button' : undefined"
        :tabindex="pendingBudgetCount > 0 ? 0 : undefined"
        :title="pendingBudgetCount > 0 ? '点击筛选含预算申请' : undefined"
        @click="togglePendingBudgetOnly"
        @keyup.enter="togglePendingBudgetOnly"
      >
        <span class="approval-stat__value">{{ pendingBudgetCount }}</span>
        <span class="approval-stat__label">含预算待办</span>
      </div>
      <div class="approval-stat">
        <span class="approval-stat__value">{{ historyDisplayTotal }}</span>
        <span class="approval-stat__label">历史记录</span>
      </div>
    </div>

    <el-tabs v-model="tab">
      <!-- 待审批（仅负责人可见） -->
      <el-tab-pane name="pending">
        <template #label>
          待审批
          <el-badge v-if="pendingCount > 0" :value="pendingCount" :max="99" class="tab-badge" />
        </template>

        <div v-if="pendingCount > 0" class="key-request-filter-bar">
          <el-check-tag
            v-if="pendingBudgetOnly"
            class="pending-budget-tag"
            checked
            @change="(v: boolean) => { if (!v) { pendingBudgetOnly = false; triggerPendingSearch() } }"
          >
            仅含预算
          </el-check-tag>
          <el-input
            v-model="pendingNameFilter"
            placeholder="搜索申请人或 Key 名称"
            clearable
            style="width: 260px"
            @keyup.enter="triggerPendingSearch"
            @clear="triggerPendingSearch"
          >
            <template #append>
              <el-button :icon="Search" @click="triggerPendingSearch" />
            </template>
          </el-input>
        </div>

        <el-empty v-if="!loading && pendingItems.length === 0" description="暂无待审批申请" />

        <div v-else v-loading="loading" class="pending-list">
          <div v-for="item in pendingItems" :key="item.id" class="req-card">
            <div class="req-head">
              <div>
                <div class="req-title">{{ item.key_name }}</div>
                <div class="req-meta">
                  申请人：
                  <strong>{{ item.requester_name || item.requester_id }}</strong>
                  <span v-if="item.dept_name" class="req-dept">· {{ item.dept_name }}</span>
                </div>
              </div>
              <el-tag type="warning" size="small">审批中</el-tag>
            </div>

            <div class="req-body">
              <div class="req-info-row" v-if="item.purpose">
                <span class="req-label">用途说明：</span>
                <span>{{ item.purpose }}</span>
              </div>
              <div class="req-info-row">
                <span class="req-label">预算申请：</span>
                <KeyRequestBudgetTag :budget-monthly="item.budget_monthly" />
              </div>
              <div class="req-info-row" v-if="item.expires_at">
                <span class="req-label">Key 有效期：</span>
                <span>{{ formatDate(item.expires_at) }}</span>
              </div>
              <div class="req-info-row text-muted">{{ formatDate(item.created_at) }} 提交</div>
            </div>

            <div class="req-actions">
              <el-popconfirm
                :title="approveConfirmTitle(item)"
                confirm-button-text="确认通过"
                width="280"
                @confirm="handleDecide(item.id, 'approve')"
              >
                <template #reference>
                  <el-button type="primary" size="small">通过</el-button>
                </template>
              </el-popconfirm>

              <el-button size="small" type="danger" plain @click="openRejectDialog(item)">驳回</el-button>
            </div>
          </div>
        </div>
        <div v-if="pendingTotal > 0" class="pagination-wrapper approval-pagination">
          <el-pagination
            v-model:current-page="pendingPage"
            :page-size="pendingPageSize"
            :total="pendingTotal"
            :hide-on-single-page="false"
            layout="total, prev, pager, next"
            @current-change="load"
          />
        </div>
      </el-tab-pane>

      <!-- 审批历史 -->
      <el-tab-pane name="history" label="审批历史">
        <div v-if="historyAllTotal > 0" class="key-request-filter-bar">
          <el-input
            v-model="historyNameFilter"
            placeholder="搜索申请人或 Key 名称"
            clearable
            style="width: 260px"
            @keyup.enter="triggerHistorySearch"
            @clear="triggerHistorySearch"
          >
            <template #append>
              <el-button :icon="Search" @click="triggerHistorySearch" />
            </template>
          </el-input>
          <el-select
            v-model="historyStatusFilter"
            placeholder="审批结果"
            clearable
            style="width: 140px"
          >
            <el-option label="已通过" value="approved" />
            <el-option label="已拒绝" value="rejected" />
          </el-select>
        </div>

        <el-empty v-if="!loadingHistory && historyItems.length === 0 && historyTotal === 0" description="暂无审批记录" />

        <div v-else v-loading="loadingHistory" class="history-list">
          <div
            v-for="item in historyItems"
            :key="item.id"
            class="req-card"
            :class="`req-card--${item.status}`"
          >
            <div class="req-head">
              <div>
                <div class="req-title">{{ item.key_name }}</div>
                <div class="req-meta">
                  申请人：
                  <strong>{{ item.requester_name || item.requester_id }}</strong>
                  <span v-if="item.dept_name" class="req-dept">· {{ item.dept_name }}</span>
                </div>
              </div>
              <el-tag :type="STATUS_COLORS[item.status]" size="small">
                {{ STATUS_LABELS[item.status] }}
              </el-tag>
            </div>
            <div class="req-body">
              <div class="req-info-row" v-if="item.purpose">
                <span class="req-label">用途：</span>
                <span>{{ item.purpose }}</span>
              </div>
              <div class="req-info-row">
                <span class="req-label">预算申请：</span>
                <KeyRequestBudgetTag :budget-monthly="item.budget_monthly" />
              </div>
              <div class="req-info-row" v-if="item.approve_comment">
                <span class="req-label">审批备注：</span>
                <span>{{ item.approve_comment }}</span>
              </div>
              <div class="req-info-row">
                <span class="req-label">审批人：</span>
                <KeyRequestApproverCell :status="item.status" :approver-name="item.approver_name" />
              </div>
              <div class="req-info-row text-muted">{{ formatDate(item.updated_at) }}</div>
            </div>
          </div>
        </div>
        <div v-if="historyDisplayTotal > 0" class="pagination-wrapper approval-pagination">
          <el-pagination
            v-model:current-page="historyPage"
            :page-size="historyPageSize"
            :total="historyDisplayTotal"
            :hide-on-single-page="false"
            layout="total, prev, pager, next"
            @current-change="loadHistory"
          />
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 部门策略配置（与 Tabs 同级的独立区块，仅管理角色可见） -->
    <div v-if="authStore.isManager" class="policy-section">
      <h3 class="section-title">部门 Key 创建策略</h3>
      <el-alert
        type="info"
        :closable="false"
        style="margin-bottom:16px"
      >
        控制本部门成员新建 Key 时是否需要审批。「自由创建」= 默认行为，可直接创建；「需要审批」= 创建时弹窗填写用途并提交审批，由部门负责人通过后才能生成 Key。
      </el-alert>
      <el-form label-width="120px" style="max-width: 480px">
        <el-form-item label="当前部门">
          <span class="current-dept">{{ authStore.user?.departmentName || '未分配' }}</span>
        </el-form-item>
        <el-form-item label="Key 创建策略">
          <el-radio-group
            :model-value="deptPolicy"
            :disabled="policySaving"
            @change="handlePolicyChange"
          >
            <el-radio value="free">自由创建（不需要审批）</el-radio>
            <el-radio value="approval">需要审批（部门负责人批准后才创建）</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
    </div>

    <!-- 驳回弹窗 -->
    <KeyAutoApproveDialog
      v-model="showAutoApproveDialog"
      :loading="autoApproveLoading"
      :pending-count="pendingCount"
      :result="autoApproveResult"
      @closed="autoApproveResult = null"
    />
    <el-dialog v-model="showRejectDialog" title="驳回申请" width="400px" @closed="rejectComment = ''">
      <el-input v-model="rejectComment" type="textarea" :rows="3" placeholder="填写驳回理由（必填）" maxlength="500" show-word-limit />
      <template #footer>
        <el-button @click="showRejectDialog = false">取消</el-button>
        <el-button type="danger" :disabled="!rejectComment.trim()" :loading="deciding" @click="confirmReject">确认驳回</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import {
  getPendingKeyRequests,
  getPendingKeyRequestCounts,
  getKeyRequestHistory,
  getDeptKeyCreationPolicy,
  setDeptKeyCreationPolicy,
  decideKeyRequest,
} from '@/services/control-plane'
import type { KeyRequest, KeyRequestAutoApproveResult } from '@/types/key-request'
import { KEY_REQUEST_STATUS_LABELS as STATUS_LABELS, KEY_REQUEST_STATUS_COLORS as STATUS_COLORS, formatKeyRequestBudget, keyRequestHasBudget } from '@/types/key-request'
import KeyAutoApproveDialog from '@/components/key/KeyAutoApproveDialog.vue'
import KeyRequestBudgetTag from '@/components/key/KeyRequestBudgetTag.vue'
import KeyRequestApproverCell from '@/components/key/KeyRequestApproverCell.vue'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const tab = ref('pending')
const loading = ref(false)
const loadingHistory = ref(false)
const pendingItems = ref<KeyRequest[]>([])
const pendingCount = ref(0)
const pendingBudgetCount = ref(0)
const pendingBudgetOnly = ref(false)
const pendingPage = ref(1)
const pendingTotal = ref(0)
const pendingPageSize = 10
const pendingNameFilter = ref('')
const historyItems = ref<KeyRequest[]>([])
const historyPage = ref(1)
const historyTotal = ref(0)
const historyAllTotal = ref(0)
const historyPageSize = 10
const historyNameFilter = ref('')
const historyStatusFilter = ref<'approved' | 'rejected' | ''>('')
let historyLoadSeq = 0
let pendingLoadSeq = 0

const showRejectDialog = ref(false)
const rejectingItem = ref<KeyRequest | null>(null)
const rejectComment = ref('')
const deciding = ref(false)

const deptPolicy = ref<'free' | 'approval'>('free')
const policySaving = ref(false)
const currentDeptId = ref('')

const showAutoApproveDialog = ref(false)
const autoApproveLoading = ref(false)
const autoApproveResult = ref<KeyRequestAutoApproveResult | null>(null)

const pendingDisplayTotal = computed(() => Math.max(pendingTotal.value, pendingCount.value))
const historyDisplayTotal = computed(() => historyTotal.value)

function formatDate(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function resolvePageArg(page: number | undefined, pageRef: { value: number }): number {
  if (typeof page === 'number' && Number.isFinite(page) && page >= 1) {
    pageRef.value = page
  }
  return pageRef.value
}

function triggerPendingSearch() {
  pendingPage.value = 1
  load()
}

function togglePendingBudgetOnly() {
  if (pendingBudgetCount.value <= 0) return
  pendingBudgetOnly.value = !pendingBudgetOnly.value
  pendingPage.value = 1
  load()
}

function triggerHistorySearch() {
  historyPage.value = 1
  loadHistory()
}

watch(historyStatusFilter, () => {
  if (tab.value !== 'history') return
  historyPage.value = 1
  loadHistory()
})

let pendingNameDebounce: ReturnType<typeof setTimeout> | null = null
watch(pendingNameFilter, () => {
  if (tab.value !== 'pending') return
  if (pendingNameDebounce) clearTimeout(pendingNameDebounce)
  pendingNameDebounce = setTimeout(() => triggerPendingSearch(), 350)
})

let historyNameDebounce: ReturnType<typeof setTimeout> | null = null
watch(historyNameFilter, () => {
  if (tab.value !== 'history') return
  if (historyNameDebounce) clearTimeout(historyNameDebounce)
  historyNameDebounce = setTimeout(() => triggerHistorySearch(), 350)
})

function applyPendingCounts(counts: { count: number; budgetCount: number }) {
  pendingCount.value = Number(counts.count) || 0
  pendingBudgetCount.value = Number(counts.budgetCount) || 0
  if (pendingBudgetOnly.value && pendingBudgetCount.value === 0) {
    pendingBudgetOnly.value = false
  }
}

async function refreshPendingCounts() {
  try {
    applyPendingCounts(await getPendingKeyRequestCounts())
  } catch {
    pendingCount.value = 0
    pendingBudgetCount.value = 0
  }
}

async function load(page?: number) {
  const p = resolvePageArg(page, pendingPage)
  const seq = ++pendingLoadSeq
  loading.value = true
  try {
    const [r, counts] = await Promise.all([
      getPendingKeyRequests({
        page: p,
        pageSize: pendingPageSize,
        name: pendingNameFilter.value.trim() || undefined,
        hasBudget: pendingBudgetOnly.value || undefined,
      }),
      getPendingKeyRequestCounts(),
    ])
    if (seq !== pendingLoadSeq) return
    pendingItems.value = r.items
    pendingTotal.value = Number(r.total) || 0
    applyPendingCounts(counts)
  } catch {
    if (seq !== pendingLoadSeq) return
    pendingItems.value = []
    pendingTotal.value = 0
    pendingCount.value = 0
    pendingBudgetCount.value = 0
  } finally {
    if (seq === pendingLoadSeq) {
      loading.value = false
    }
  }
}

// 查询审批人已处理的申请（approved / rejected）
async function loadHistory(page?: number) {
  const p = resolvePageArg(page, historyPage)
  const seq = ++historyLoadSeq
  loadingHistory.value = true
  try {
    const r = await getKeyRequestHistory({
      page: p,
      pageSize: historyPageSize,
      name: historyNameFilter.value.trim() || undefined,
      status: historyStatusFilter.value || undefined,
    })
    if (seq !== historyLoadSeq) return
    historyItems.value = r.items
    historyTotal.value = Number(r.total) || 0
    if (!historyNameFilter.value.trim() && !historyStatusFilter.value) {
      historyAllTotal.value = historyTotal.value
    }
  } catch {
    if (seq !== historyLoadSeq) return
    historyItems.value = []
    historyTotal.value = 0
    if (!historyNameFilter.value.trim() && !historyStatusFilter.value) {
      historyAllTotal.value = 0
    }
  } finally {
    if (seq === historyLoadSeq) {
      loadingHistory.value = false
    }
  }
}

function approveConfirmTitle(item: KeyRequest): string {
  if (keyRequestHasBudget(item.budget_monthly)) {
    return `确认通过该申请？将自动创建 Key，并配置个人月预算 ${formatKeyRequestBudget(item.budget_monthly)}。`
  }
  return '确认通过该申请？将自动创建 Key（本次未申请预算）。'
}

async function handleDecide(id: string, action: 'approve') {
  deciding.value = true
  try {
    await decideKeyRequest(id, action)
    ElMessage.success('已通过')
    await Promise.all([load(), loadHistory()])
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '操作失败')
  } finally {
    deciding.value = false
  }
}

function openRejectDialog(item: KeyRequest) {
  rejectingItem.value = item
  showRejectDialog.value = true
}

async function confirmReject() {
  if (!rejectingItem.value) return
  deciding.value = true
  try {
    await decideKeyRequest(rejectingItem.value.id, 'reject', rejectComment.value.trim())
    ElMessage.success('已驳回')
    showRejectDialog.value = false
    await Promise.all([load(), loadHistory()])
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '操作失败')
  } finally {
    deciding.value = false
  }
}

async function loadPolicy() {
  const deptId = (authStore.user as any)?.departmentId || ''
  currentDeptId.value = deptId
  if (!deptId) return
  try {
    const res = await getDeptKeyCreationPolicy(deptId)
    deptPolicy.value = (res.key_creation_policy as 'free' | 'approval') || 'free'
  } catch {
    deptPolicy.value = 'free'
  }
}

async function confirmDeptKeyPolicyChange(enabling: boolean, pending: number): Promise<boolean> {
  const title = enabling ? '开启部门 Key 审批' : '关闭部门 Key 审批'
  let message = enabling
    ? '开启后，部门成员创建 Key 需先提交申请，由部门负责人审批通过后方可生成 Key。'
    : '关闭后，部门成员可直接创建 Key，无需审批。'
  if (!enabling && pending > 0) {
    message += ` 当前有 ${pending} 条待审批申请，关闭后将自动通过并生成 Key。`
  }
  try {
    await ElMessageBox.confirm(message, title, {
      type: enabling ? 'info' : 'warning',
      confirmButtonText: enabling ? '确认开启' : '确认关闭',
      cancelButtonText: '取消',
    })
    return true
  } catch {
    return false
  }
}

async function handlePolicyChange(val: string | number | boolean) {
  if (!currentDeptId.value) {
    ElMessage.warning('当前用户未分配部门，无法设置部门策略')
    return
  }
  const policy = val as 'free' | 'approval'
  if (policy === deptPolicy.value) return
  const enabling = policy === 'approval'
  const ok = await confirmDeptKeyPolicyChange(enabling, pendingCount.value)
  if (!ok) return

  const isDisabling = policy === 'free'
  deptPolicy.value = policy
  policySaving.value = true
  if (isDisabling && pendingCount.value > 0) {
    showAutoApproveDialog.value = true
    autoApproveLoading.value = true
    autoApproveResult.value = null
  }
  try {
    const res = await setDeptKeyCreationPolicy(currentDeptId.value, policy)
    if (res.auto_approve && res.auto_approve.total > 0) {
      autoApproveResult.value = res.auto_approve
      await Promise.all([load(), loadHistory()])
    } else {
      showAutoApproveDialog.value = false
      ElMessage.success(isDisabling ? '已关闭部门 Key 审批' : '已开启部门 Key 审批')
    }
  } catch (e: any) {
    deptPolicy.value = policy === 'free' ? 'approval' : 'free'
    showAutoApproveDialog.value = false
    autoApproveResult.value = null
    ElMessage.error(e?.response?.data?.error || '更新失败')
  } finally {
    policySaving.value = false
    autoApproveLoading.value = false
  }
}

watch(tab, (t) => {
  if (t === 'pending') load(1)
  else if (t === 'history') {
    void Promise.all([refreshPendingCounts(), loadHistory(1)])
  }
})

onMounted(() => {
  void refreshPendingCounts()
  load()
  loadHistory()
  loadPolicy()
})
watch(() => authStore.effectiveRole, (role) => {
  if (!role || !['admin', 'studio_admin', 'dept_admin'].includes(role)) return
  if (tab.value === 'pending') load()
  else void Promise.all([refreshPendingCounts(), loadHistory()])
})
</script>

<style scoped>
.key-request-page { padding: 0; }
.approval-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}
.approval-stat--budget.is-disabled {
  cursor: default;
}
.approval-stat--budget.is-disabled:hover {
  border-color: var(--el-border-color-lighter);
}
.approval-stat--budget.is-disabled .approval-stat__value {
  color: var(--el-text-color-secondary);
}
.approval-stat--budget:not(.is-disabled) .approval-stat__value { color: var(--el-color-primary); }
.approval-stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-blank);
}
.approval-stat__value {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.2;
}
.approval-stat__label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.approval-stat--warn .approval-stat__value { color: var(--el-color-warning); }
.approval-stat--budget {
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}
.approval-stat--budget:hover { border-color: var(--el-color-primary-light-5); }
.approval-stat--budget.is-active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}
.approval-stat--budget .approval-stat__value { color: var(--el-color-primary); }
.pending-budget-tag { margin-right: 4px; }
.key-request-filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
}
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
}
.page-header h1 {
  font-size: 22px;
  font-weight: 700;
  margin: 0 0 4px 0;
  color: var(--el-text-color-primary);
}
.page-desc { color: var(--el-text-color-secondary); font-size: 13px; margin: 0; }
.tab-badge { margin-left: 4px; }
.pagination-wrapper { display: flex; justify-content: flex-end; margin-top: 16px; }

.pending-list,
.mine-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 4px;
}

.req-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 16px 20px;
}
.req-card:hover { border-color: var(--el-border-color); }

.req-card--approved { border-left: 3px solid #10b981; }
.req-card--rejected { border-left: 3px solid #ef4444; }
.req-card--pending  { border-left: 3px solid #f59e0b; }
.req-card--cancelled { border-left: 3px solid #6b7280; }

.req-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; margin-bottom: 10px; }
.req-title { font-weight: 600; font-size: 15px; color: var(--el-text-color-primary); }
.req-meta { font-size: 13px; color: var(--el-text-color-secondary); margin-top: 3px; }
.req-dept { margin-left: 4px; }

.req-body { display: flex; flex-direction: column; gap: 4px; font-size: 13px; color: var(--el-text-color-regular); }
.req-info-row { display: flex; align-items: center; gap: 4px; }
.req-label { flex-shrink: 0; color: var(--el-text-color-secondary); }
.text-muted { color: var(--el-text-color-secondary); }

.req-actions { display: flex; gap: 8px; margin-top: 12px; }

.skeleton { opacity: 0.4; animation: pulse 1.2s infinite; }
.sk-line { background: var(--el-fill-color-light); border-radius: 4px; height: 14px; }
@keyframes pulse { 0%,100%{opacity:.4} 50%{opacity:.7} }

.policy-section {
  margin-top: 32px;
  padding-top: 24px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.section-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0 0 12px 0;
}

.current-dept {
  font-weight: 600;
  color: var(--el-color-primary);
}
</style>
