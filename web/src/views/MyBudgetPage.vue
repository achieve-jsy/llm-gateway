<template>
  <div class="mb">
    <header class="mb-head">
      <div class="mb-head__left">
        <h2 class="mb-title">我的预算</h2>
        <span class="mb-badge">{{ authStore.userName || '个人' }}</span>
      </div>
      <el-button :loading="loading" :icon="Refresh" circle size="small" @click="loadAll" />
    </header>

    <div v-if="loading && !chain.length" v-loading="true" class="mb-loading" />
    <div v-else-if="!chain.length" class="mb-no-budget">
      <template v-if="authStore.isAdmin">
        <el-icon class="mb-no-budget__icon"><Setting /></el-icon>
        <p>平台管理员无个人预算配置</p>
        <span>管理员可在预算管理中为组织配置预算，或在组织架构中为成员分配额度。</span>
        <el-button type="primary" plain size="small" style="margin-top:12px" @click="router.push('/admin/budget')">前往预算管理</el-button>
      </template>
      <template v-else>
        <el-empty description="暂无预算配置，请联系管理员" />
      </template>
    </div>

    <template v-else>
      <!-- KPI strip -->
      <section class="mb-kpi-strip">
        <div class="mb-kpi">
          <span>可用余额</span>
          <strong>{{ formatUSD(remainingAmount) }}</strong>
        </div>
        <div class="mb-kpi">
          <span>本月已用</span>
          <strong>{{ formatUSD(primaryBudget?.used_usd || 0) }}</strong>
        </div>
        <div class="mb-kpi">
          <span>月度额度<template v-if="isUsingSharedPool"> <em class="mb-kpi-pool-tag">共享池</em></template></span>
          <strong>{{ primaryBudget ? displayAmount(primaryBudget.amount_usd) : '未设置' }}</strong>
        </div>
        <div class="mb-kpi">
          <span>使用率</span>
          <strong :class="{ 'is-danger': primaryRatioPercent >= 80 }">{{ primaryRatioPercent }}%</strong>
        </div>
      </section>

      <!-- 主工作区 -->
      <section class="mb-grid">
        <!-- 左：额度详情 -->
        <div class="mb-panel">
          <div class="mb-panel-head">
            <div>
              <h2>{{ primaryBudgetLabel }}</h2>
              <p>{{ isUsingSharedPool ? '当前所用额度与使用情况' : '您的专属配额与使用情况' }}</p>
            </div>
            <div class="mb-panel-head-actions">
              <span :class="`mb-tag mb-tag--${statusTagType}`">{{ statusText }}</span>
            </div>
          </div>

          <!-- 共享池提示 -->
          <div v-if="isUsingSharedPool" class="mb-shared-pool-notice">
            <span class="mb-shared-pool-icon">🔗</span>
            <div class="mb-shared-pool-text">
              <span class="mb-shared-pool-title">您正在使用部门共享预算池</span>
              <span class="mb-shared-pool-desc">
                您尚未分配个人独立配额，当前消耗计入「{{ primaryBudgetLabel }}」共享池。
                如需独立额度管理，可向部门负责人申请。
              </span>
            </div>
          </div>

          <div class="mb-balance-block">
            <div>
              <span class="mb-balance-label">可用余额</span>
              <div class="mb-balance-amount">{{ formatUSD(remainingAmount) }}</div>
              <div class="mb-balance-sub">已用 {{ formatUSD(primaryBudget?.used_usd || 0) }} / 总额 {{ primaryBudget ? displayAmount(primaryBudget.amount_usd) : '未设置' }}</div>
            </div>
            <div class="mb-balance-ring">
              <svg viewBox="0 0 64 64">
                <circle cx="32" cy="32" r="26" fill="none" stroke="var(--el-fill-color)" stroke-width="5"/>
                <circle cx="32" cy="32" r="26" fill="none"
                  :stroke="progressColor(primaryBudget?.used_ratio || 0)"
                  stroke-width="5"
                  stroke-linecap="round"
                  :stroke-dasharray="`${primaryRatioPercent * 1.634} 163.4`"
                  transform="rotate(-90 32 32)"
                />
              </svg>
              <span>{{ primaryRatioPercent }}%</span>
            </div>
          </div>

          <div class="mb-period-rows">
            <div class="mb-period-item">
              <span class="mb-period-name">今日额度</span>
              <span class="mb-period-value">{{ displayAmount(primaryBudget?.daily_amount_usd ?? -1) }}</span>
              <span class="mb-period-reset">明天 00:00 重置</span>
            </div>
            <div class="mb-period-item">
              <span class="mb-period-name">本周额度</span>
              <span class="mb-period-value">{{ displayAmount(primaryBudget?.weekly_amount_usd ?? -1) }}</span>
              <span class="mb-period-reset">下周一 00:00 重置</span>
            </div>
            <div class="mb-period-item">
              <span class="mb-period-name">本月额度</span>
              <span class="mb-period-value">{{ primaryBudget ? displayAmount(primaryBudget.amount_usd) : '未设置' }}</span>
              <span class="mb-period-reset">下月 1 日 00:00 重置</span>
            </div>
          </div>

          <!-- 月度分析 -->
          <div class="mb-monthly">
            <div class="mb-monthly-title">月度分析</div>

            <div class="mb-monthly-row">
              <span class="mb-monthly-label">月内进度</span>
              <span class="mb-monthly-sub">第 {{ monthDay }} 天 / 共 {{ daysInMonth }} 天</span>
            </div>
            <div class="mb-monthly-track">
              <div class="mb-monthly-fill mb-monthly-fill--day" :style="{ width: monthDayPercent + '%' }" />
            </div>

            <div class="mb-monthly-stats">
              <div class="mb-monthly-stat">
                <span>日均消耗</span>
                <strong>{{ dailyAvg > 0 ? formatUSD(dailyAvg) : '—' }}</strong>
              </div>
              <div class="mb-monthly-stat">
                <span>预计月末</span>
                <strong v-if="dailyAvg > 0" :class="projectedOver ? 'is-over' : ''">{{ formatUSD(projectedTotal) }}</strong>
                <strong v-else class="mb-monthly-na">暂无数据</strong>
              </div>
              <div class="mb-monthly-stat">
                <span>剩余天数</span>
                <strong>{{ daysInMonth - monthDay }} 天</strong>
              </div>
            </div>

            <div v-if="projectedOver" class="mb-monthly-warn">
              ⚠ 按当前速率月末预计超出额度，建议申请提升
            </div>
            <div v-else-if="primaryRatioPercent === 0" class="mb-monthly-hint">
              本月暂无消耗，额度充足
            </div>
          </div>
        </div>

        <!-- 右：扩容申请 -->
        <div class="mb-panel">
          <div class="mb-panel-head">
            <div>
              <h2>扩容申请</h2>
              <p v-if="!authStore.isManager">个人额度提升申请</p>
              <p v-else>额度直接调整</p>
            </div>
            <!-- 普通用户才显示新建申请按钮 -->
            <template v-if="!authStore.isManager">
              <el-tooltip v-if="hasPendingRequest" content="已有审批中的申请，请等待结果或取消后重新提交" placement="top">
                <el-button size="small" disabled>新建申请</el-button>
              </el-tooltip>
              <el-button v-else size="small" type="primary" plain @click="openExpansion">新建申请</el-button>
            </template>
          </div>

          <!-- 管理员：直接调额提示 -->
          <div v-if="authStore.isManager" class="mb-manager-hint">
            <div class="mb-manager-hint-icon">⚡</div>
            <div>
              <p class="mb-manager-hint-title">您可直接调整额度</p>
              <p class="mb-manager-hint-desc">
                <template v-if="authStore.effectiveRole === 'dept_admin'">作为部门负责人，可在「组织管理」中直接修改成员配额，无需提交申请。如需提升部门整体预算，请联系工作室负责人。</template>
                <template v-else-if="authStore.effectiveRole === 'studio_admin'">作为工作室负责人，可在「组织管理」中直接修改部门预算，无需提交申请。如需提升工作室整体预算，请联系平台管理员。</template>
                <template v-else>拥有全局额度管理权限，可在「预算总览」中直接调整任意层级的额度。</template>
              </p>
              <el-button
                size="small" type="primary" plain style="margin-top: 12px"
                @click="goManagerAdjust"
              >前往调整 →</el-button>
            </div>
          </div>

          <template v-else>
          <div v-if="requestLoading" v-loading="true" class="mb-inner-loading" />

          <!-- 空状态 -->
          <div v-else-if="myRequests.length === 0" class="mb-empty">
            <div class="mb-empty-icon">↑</div>
            <p>尚未发起过扩容申请</p>
            <span>额度不足时，点击右上角「新建申请」可提升个人配额，由部门负责人审批。</span>
          </div>

          <template v-else>
            <!-- 最新申请：审批中 → 突出显示 + 取消入口 -->
            <div v-if="latestRequest" class="mb-active-request" :class="`is-${latestRequest.status}`">
              <div class="mb-active-header">
                <div class="mb-active-meta">
                  <span :class="`mb-tag mb-tag--${latestRequest.status}`">{{ statusLabel(latestRequest.status) }}</span>
                  <strong class="mb-active-amount">{{ formatUSD(latestRequest.requested_amount) }} {{ PERIOD_SUFFIX[latestRequest.period_type || 'monthly'] }}</strong>
                </div>
                <div class="mb-active-actions">
                  <span class="mb-muted">{{ formatDateTime(latestRequest.created_at) }}</span>
                  <el-button
                    v-if="latestRequest.status === 'pending'"
                    size="small" type="danger" plain
                    :loading="cancellingId === latestRequest.id"
                    @click="cancelRequest(latestRequest.id)"
                  >取消</el-button>
                  <el-button
                    v-else-if="latestRequest.status !== 'pending'"
                    size="small" @click="openExpansion"
                  >重新申请</el-button>
                </div>
              </div>

              <p v-if="latestRequest.reason" class="mb-active-reason">{{ latestRequest.reason }}</p>

              <!-- 步进器 -->
              <div class="mb-steps">
                <div class="mb-step is-done">
                  <div class="mb-step-node">✓</div>
                  <span>提交</span>
                </div>
                <div class="mb-step-line" :class="{ 'is-done': latestRequest.status !== 'pending' }" />
                <div class="mb-step" :class="{
                  'is-active': latestRequest.status === 'pending',
                  'is-done': latestRequest.status === 'approved',
                  'is-rejected': latestRequest.status === 'rejected'
                }">
                  <div class="mb-step-node">{{ latestRequest.status === 'approved' ? '✓' : latestRequest.status === 'rejected' ? '✕' : '' }}</div>
                  <span>审批</span>
                </div>
                <div class="mb-step-line" :class="{ 'is-done': latestRequest.status === 'approved' }" />
                <div class="mb-step" :class="{
                  'is-done': latestRequest.status === 'approved',
                  'is-rejected': latestRequest.status === 'rejected'
                }">
                  <div class="mb-step-node">{{ latestRequest.status === 'approved' ? '✓' : latestRequest.status === 'rejected' ? '✕' : '' }}</div>
                  <span>{{ latestRequest.status === 'rejected' ? '驳回' : '生效' }}</span>
                </div>
              </div>

              <div class="mb-active-note">{{ requestProgressText(latestRequest) }}</div>
            </div>

            <!-- 历史记录 -->
            <div v-if="historyRequests.length > 0" class="mb-history">
              <div class="mb-history-header">
                <span>历史记录</span>
                <span class="mb-history-total">共 {{ historyRequests.length }} 条</span>
              </div>
              <div ref="historyScrollRef" class="mb-history-scroll">
                <div v-for="item in visibleHistory" :key="item.id" class="mb-history-item">
                  <span :class="`mb-tag mb-tag--${item.status} mb-tag--sm`">{{ statusLabel(item.status) }}</span>
                  <span class="mb-history-amount">{{ formatUSD(item.requested_amount) }} {{ PERIOD_SUFFIX[(item.period_type as ExpansionPeriod) || 'monthly'] }}</span>
                  <span class="mb-muted">{{ formatDateTime(item.created_at) }}</span>
                </div>
                <!-- 滚动到底部触发加载更多 -->
                <div ref="historySentinelRef" class="mb-history-sentinel" />
              </div>
            </div>
          </template>
          </template>
        </div>
      </section>

    </template>

    <el-dialog v-model="expansionVisible" title="申请提升个人额度" width="520px">
      <el-form label-position="top">
        <el-form-item label="调整维度">
          <el-segmented v-model="expansionForm.periodType" :options="periodOptions" block style="width:100%" />
        </el-form-item>
        <el-form-item>
          <div class="mb-dialog-summary">
            <div>
              <span>当前{{ PERIOD_LABELS[expansionForm.periodType] }}</span>
              <strong>{{ dialogCurrentAmount }}</strong>
            </div>
            <div>
              <span>已用金额</span>
              <strong>{{ formatUSD(primaryBudget?.used_usd || 0) }}</strong>
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="`申请后的${PERIOD_LABELS[expansionForm.periodType]}（USD）`">
          <el-input-number v-model="expansionForm.requestedAmount" :min="1" :precision="2" controls-position="right" style="width:100%" />
        </el-form-item>
        <el-form-item label="申请理由">
          <el-input v-model="expansionForm.reason" type="textarea" :rows="3" placeholder="说明业务场景、预计消耗和需要提升额度的原因" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="expansionVisible = false">取消</el-button>
        <el-button type="primary" :loading="expansionSubmitting" @click="submitExpansion">提交申请</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, Setting } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { createExpansionRequest, decideExpansion, getMyBudget, getMyExpansionRequests } from '@/services/control-plane'
import { useAuthStore } from '@/stores/auth'
import type { BudgetChainItem } from '@/types/budget'
import type { ExpansionPeriod, ExpansionRequest, ExpansionStatus } from '@/types/expansion'
import { PERIOD_LABELS, PERIOD_SUFFIX, STATUS_COLORS, STATUS_LABELS } from '@/types/expansion'

const router = useRouter()
const authStore = useAuthStore()

const chain = ref<BudgetChainItem[]>([])
const myRequests = ref<ExpansionRequest[]>([])
const loading = ref(false)
const requestLoading = ref(false)
const expansionVisible = ref(false)
const expansionSubmitting = ref(false)
const expansionForm = ref<{ periodType: ExpansionPeriod; requestedAmount: number; reason: string }>({
  periodType: 'monthly',
  requestedAmount: 100,
  reason: '',
})

const periodOptions = [
  { label: '日额度', value: 'daily' },
  { label: '周额度', value: 'weekly' },
  { label: '月额度', value: 'monthly' },
]

const dialogCurrentAmount = computed(() => {
  const b = primaryBudget.value
  if (!b) return '未设置'
  const pt = expansionForm.value.periodType
  if (pt === 'daily') return displayAmount(b.daily_amount_usd ?? -1)
  if (pt === 'weekly') return displayAmount(b.weekly_amount_usd ?? -1)
  return displayAmount(b.amount_usd)
})

const primaryBudget = computed(() =>
  chain.value.find(item => item.scope_type === 'member') ||
  chain.value.find(item => item.scope_type === 'dept_daily') ||
  chain.value[0]
)
// 是否正在消费共享部门池（未配置个人独立配额）
const isUsingSharedPool = computed(() =>
  !!primaryBudget.value && primaryBudget.value.scope_type !== 'member',
)
const primaryBudgetLabel = computed(() => primaryBudget.value?.label || '我的额度')
const primaryRatioPercent = computed(() => Math.min(Math.round((primaryBudget.value?.used_ratio || 0) * 100), 100))
const remainingAmount = computed(() => {
  const budget = primaryBudget.value
  if (!budget || budget.amount_usd < 0) return 0
  return Math.max((budget.amount_usd || 0) - (budget.used_usd || 0), 0)
})
const statusText = computed(() => {
  const ratio = primaryBudget.value?.used_ratio || 0
  if (ratio >= 1) return '已用尽'
  if (ratio >= 0.8) return '预警'
  return '正常'
})
const statusTagType = computed(() => {
  const ratio = primaryBudget.value?.used_ratio || 0
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return 'success'
})
const statusClass = computed(() => `is-${statusTagType.value}`)
const canOpenExpansion = computed(() => authStore.effectiveRole !== 'admin')

// 月度分析相关
const now = new Date()
const monthDay = now.getDate()
const daysInMonth = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()
const monthDayPercent = Math.round((monthDay / daysInMonth) * 100)
const dailyAvg = computed(() => {
  const used = primaryBudget.value?.used_usd || 0
  return monthDay > 0 ? used / monthDay : 0
})
const projectedTotal = computed(() => dailyAvg.value * daysInMonth)
const projectedOver = computed(() => {
  const limit = primaryBudget.value?.amount_usd
  if (!limit || limit < 0) return false
  return projectedTotal.value > limit
})
const latestRequest = computed(() => myRequests.value[0])
const HISTORY_PAGE_SIZE = 5
const historyPage = ref(1)
const historyScrollRef = ref<HTMLElement | null>(null)
const historySentinelRef = ref<HTMLElement | null>(null)
let historyObserver: IntersectionObserver | null = null

const historyRequests = computed(() => myRequests.value.slice(1))
const visibleHistory = computed(() => historyRequests.value.slice(0, historyPage.value * HISTORY_PAGE_SIZE))
const hasMoreHistory = computed(() => visibleHistory.value.length < historyRequests.value.length)

function setupHistoryObserver() {
  if (historyObserver) {
    historyObserver.disconnect()
    historyObserver = null
  }
  if (!historySentinelRef.value) return
  historyObserver = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting && hasMoreHistory.value) {
        historyPage.value++
      }
    },
    { root: historyScrollRef.value, threshold: 0.1 }
  )
  historyObserver.observe(historySentinelRef.value)
}

// 数据刷新后重置分页并重启 observer
watch(historyRequests, () => {
  historyPage.value = 1
}, { flush: 'post' })

watch(historySentinelRef, (el) => {
  if (el) setupHistoryObserver()
}, { flush: 'post' })
// 只看最新一条是否 pending：历史里的旧 pending 记录不应永久阻塞新申请
const hasPendingRequest = computed(() => latestRequest.value?.status === 'pending')
const cancellingId = ref<string | null>(null)

async function loadAll() {
  await Promise.all([loadBudget(), loadRequests()])
}

async function loadBudget() {
  loading.value = true
  try {
    chain.value = await getMyBudget()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '预算加载失败')
  } finally {
    loading.value = false
  }
}

async function loadRequests() {
  requestLoading.value = true
  try {
    myRequests.value = await getMyExpansionRequests()
  } catch {
    myRequests.value = []
  } finally {
    requestLoading.value = false
  }
}

function formatUSD(value: number) {
  return `$${(value || 0).toLocaleString('en-US', { maximumFractionDigits: 2 })}`
}

function displayAmount(value: number) {
  if (value == null || value < 0) return '不限'
  return formatUSD(value)
}

function progressColor(ratio: number) {
  if (ratio >= 1) return '#ef4444'
  if (ratio >= 0.8) return '#f59e0b'
  return '#36B34E'
}


function goManagerAdjust() {
  if (authStore.isAdmin) {
    router.push('/admin/budget')
    return
  }
  // dept_admin：找到自己管辖的部门，直接定位到预算 tab
  if (authStore.effectiveRole === 'dept_admin') {
    const deptRole = authStore.orgRoles.find(r => r.role === 'dept_admin')
    if (deptRole?.scope_id) {
      router.push(`/admin/org?scope_type=dept_daily&scope_id=${encodeURIComponent(deptRole.scope_id)}&tab=budget`)
      return
    }
  }
  // studio_admin：找到自己管辖的工作室，直接定位到预算 tab
  if (authStore.effectiveRole === 'studio_admin') {
    const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin')
    if (studioRole?.scope_id) {
      router.push(`/admin/org?scope_type=studio&scope_id=${encodeURIComponent(studioRole.scope_id)}&tab=budget`)
      return
    }
  }
  router.push('/admin/org')
}

function openExpansion() {
  if (authStore.isManager) {
    router.push('/admin/expansion-requests')
    return
  }
  const userID = authStore.user?.id || ''
  if (!userID) {
    ElMessage.warning('无法确定当前用户，请重新登录')
    return
  }
  const base = primaryBudget.value?.amount_usd || 50
  expansionForm.value = {
    periodType: 'monthly',
    requestedAmount: Math.max(Math.ceil(base * 1.5), base + 50),
    reason: '',
  }
  expansionVisible.value = true
}

async function submitExpansion() {
  const userID = authStore.user?.id || ''
  if (!userID) {
    ElMessage.warning('无法确定当前用户，请重新登录')
    return
  }
  if (!expansionForm.value.reason.trim()) {
    ElMessage.warning('请填写申请理由')
    return
  }
  expansionSubmitting.value = true
  try {
    await createExpansionRequest({
      scope_type: 'member',
      scope_id: userID,
      period_type: expansionForm.value.periodType,
      requested_amount: expansionForm.value.requestedAmount,
      reason: expansionForm.value.reason,
    })
    ElMessage.success('申请已提交给部门负责人')
    expansionVisible.value = false
    await loadRequests()
  } finally {
    expansionSubmitting.value = false
  }
}

function statusLabel(status: ExpansionStatus) {
  return STATUS_LABELS[status] || status
}

function statusColor(status: ExpansionStatus) {
  return STATUS_COLORS[status] || '#6b7280'
}

function scopeLabel(type: string) {
  const map: Record<string, string> = {
    member: '个人额度',
    dept_daily: '部门日常',
    dept_service: '部门服务',
    studio: '工作室',
  }
  return map[type] || type
}

function formatDateTime(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

async function cancelRequest(id: string) {
  cancellingId.value = id
  // 乐观更新：立刻把本地状态改为 cancelled，hasPendingRequest 即时变 false，"新建申请"立即可点
  const req = myRequests.value.find(r => r.id === id)
  if (req) req.status = 'cancelled'
  try {
    await decideExpansion(id, 'cancel')
    ElMessage.success('申请已取消')
    await loadRequests()
  } catch (e: any) {
    // 失败时回滚本地状态
    if (req) req.status = 'pending'
    ElMessage.error(e?.response?.data?.error || '取消失败')
  } finally {
    cancellingId.value = null
  }
}

function requestProgressText(item: ExpansionRequest) {
  if (item.status === 'pending') return '等待部门负责人审批'
  if (item.status === 'approved') return '已批准，额度将按审批结果生效'
  if (item.status === 'rejected') return '申请已驳回，可调整理由后重新提交'
  if (item.status === 'cancelled') return '申请已取消'
  return statusLabel(item.status)
}

onMounted(loadAll)
onUnmounted(() => { historyObserver?.disconnect() })
</script>

<style scoped>
/* ─────────────── Layout ─────────────── */
.mb {
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 28px 32px;
}

/* ─────────────── Header ─────────────── */
.mb-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
.mb-head__left { display: flex; align-items: center; gap: 12px; }
.mb-title {
  font-size: 20px;
  font-weight: 700;
  margin: 0;
  color: var(--el-text-color-primary);
  display: inline-flex;
  align-items: center;
  gap: 10px;
}
.mb-title::before {
  content: '';
  width: 4px;
  height: 18px;
  border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), #1a7a74);
  flex-shrink: 0;
}
.mb-badge {
  font-size: 11px;
  font-weight: 500;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px;
  padding: 3px 12px;
}
.mb-loading { height: 360px; }
.mb-no-budget {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 80px 24px;
  text-align: center;
  color: var(--el-text-color-secondary);
}
.mb-no-budget__icon { font-size: 48px; color: var(--el-color-primary-light-5); margin-bottom: 8px; }
.mb-no-budget p { margin: 0; font-size: 15px; font-weight: 600; color: var(--el-text-color-primary); }
.mb-no-budget span { font-size: 13px; max-width: 320px; line-height: 1.7; }
.mb-muted { font-size: 12px; color: var(--el-text-color-placeholder); }

/* ─────────────── KPI strip ─────────────── */
.mb-kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1px;
  overflow: hidden;
  border: 1px solid var(--el-border-color-light);
  border-radius: 16px;
  background: var(--el-border-color-lighter);
}
.mb-kpi {
  padding: 14px 18px;
  background: var(--el-bg-color);
}
.mb-kpi span {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-bottom: 8px;
}
.mb-kpi strong {
  font-size: 24px;
  color: var(--el-text-color-primary);
  letter-spacing: -.03em;
}
.mb-kpi strong.is-danger { color: var(--el-color-danger); }
.mb-kpi-pool-tag {
  font-style: normal;
  font-size: 10px;
  font-weight: 500;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--el-color-info-light-8);
  color: var(--el-color-info-dark-2);
  vertical-align: middle;
}

/* ─────────────── Grid ─────────────── */
.mb-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
  gap: 16px;
  align-items: stretch;
}

/* ─────────────── Panel (same as ExpansionRequestPage) ─────────────── */
.mb-panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 16px;
  background: var(--el-bg-color);
  overflow: hidden;
}
.mb-panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mb-panel-head h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.mb-panel-head p {
  margin: 3px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.mb-panel-head-actions { display: flex; align-items: center; gap: 10px; flex-shrink: 0; }

/* ─────────────── Status tags ─────────────── */
.mb-tag {
  display: inline-block;
  font-size: 11px;
  font-weight: 600;
  padding: 3px 9px;
  border-radius: 999px;
  line-height: 1.6;
}
.mb-tag--success  { background: #e6f9f1; color: #0a6640; }
.mb-tag--warning  { background: #fff8e1; color: #8a5a00; }
.mb-tag--danger   { background: #fff0f0; color: #c0392b; }
.mb-tag--pending  { background: #eef2ff; color: #4338ca; }
.mb-tag--approved { background: #e6f9f1; color: #0a6640; }
.mb-tag--rejected { background: #fff0f0; color: #c0392b; }
.mb-tag--cancelled { background: #f3f4f6; color: #6b7280; }
.mb-tag--sm { font-size: 10px; padding: 2px 7px; }

/* ─────────────── Shared pool notice ─────────────── */
.mb-shared-pool-notice {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin: 0 18px 0;
  padding: 11px 14px;
  background: var(--el-color-info-light-9);
  border: 1px solid var(--el-color-info-light-5);
  border-radius: 10px;
  margin-top: 12px;
}
.mb-shared-pool-icon {
  font-size: 16px;
  flex-shrink: 0;
  margin-top: 1px;
}
.mb-shared-pool-text {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.mb-shared-pool-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-color-info-dark-2);
}
.mb-shared-pool-desc {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}

/* ─────────────── Balance block ─────────────── */
.mb-balance-block {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 20px 20px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mb-balance-label {
  display: block;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}
.mb-balance-amount {
  font-size: 38px;
  font-weight: 800;
  color: var(--el-color-primary);
  letter-spacing: -.04em;
  line-height: 1;
}
.mb-balance-sub {
  margin-top: 8px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
.mb-balance-ring {
  position: relative;
  flex-shrink: 0;
  width: 64px;
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.mb-balance-ring svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}
.mb-balance-ring span {
  font-size: 12px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  position: relative;
  z-index: 1;
}

/* ─────────────── Period rows ─────────────── */
.mb-period-rows { display: flex; flex-direction: column; }
.mb-period-item {
  display: grid;
  grid-template-columns: 1fr auto auto;
  gap: 16px;
  align-items: center;
  padding: 13px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mb-period-item:last-child { border-bottom: 0; }
.mb-period-name { color: var(--el-text-color-regular); font-size: 13px; }
.mb-period-value { font-size: 15px; font-weight: 700; color: var(--el-text-color-primary); }
.mb-period-reset { font-size: 12px; color: var(--el-text-color-placeholder); white-space: nowrap; }

/* ─────────────── Monthly Analysis ─────────────── */
.mb-monthly {
  margin: 0 18px 18px;
  padding: 14px 16px;
  background: var(--el-fill-color-light);
  border-radius: 10px;
}
.mb-monthly-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  margin-bottom: 10px;
  letter-spacing: 0.4px;
  text-transform: uppercase;
}
.mb-monthly-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 5px;
}
.mb-monthly-label { font-size: 12px; color: var(--el-text-color-regular); }
.mb-monthly-sub { font-size: 11px; color: var(--el-text-color-placeholder); }
.mb-monthly-track {
  height: 4px;
  background: var(--el-border-color-lighter);
  border-radius: 2px;
  overflow: hidden;
  margin-bottom: 12px;
}
.mb-monthly-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.4s ease;
}
.mb-monthly-fill--day { background: var(--el-color-primary-light-3); }
.mb-monthly-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
}
.mb-monthly-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 10px;
  background: #fff;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
}
.mb-monthly-stat span { font-size: 11px; color: var(--el-text-color-placeholder); }
.mb-monthly-stat strong { font-size: 13px; font-weight: 700; color: var(--el-text-color-primary); }
.mb-monthly-stat strong.is-over { color: var(--el-color-danger); }
.mb-monthly-stat strong.mb-monthly-na { font-weight: 400; color: var(--el-text-color-placeholder); font-size: 12px; }
.mb-monthly-warn {
  margin-top: 10px;
  padding: 7px 10px;
  background: var(--el-color-warning-light-9);
  border: 1px solid var(--el-color-warning-light-5);
  border-radius: 6px;
  font-size: 12px;
  color: var(--el-color-warning-dark-2);
}
.mb-monthly-hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  text-align: center;
}

/* ─────────────── Manager Hint ─────────────── */
.mb-manager-hint {
  display: flex;
  gap: 14px;
  align-items: flex-start;
  padding: 24px 20px;
  margin: 8px 16px 16px;
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-color-primary-light-7);
  border-radius: 12px;
}
.mb-manager-hint-icon {
  font-size: 22px;
  line-height: 1;
  flex-shrink: 0;
  margin-top: 2px;
}
.mb-manager-hint-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 6px;
}
.mb-manager-hint-desc {
  font-size: 13px;
  color: var(--el-text-color-regular);
  line-height: 1.6;
}

/* ─────────────── Empty / Loading ─────────────── */
.mb-inner-loading { height: 180px; }
.mb-empty {
  padding: 36px 20px 28px;
  text-align: center;
  color: var(--el-text-color-secondary);
  display: flex;
  flex-direction: column;
  align-items: center;
}
.mb-empty-icon {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-size: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 12px;
}
.mb-empty p { margin: 0 0 6px; font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.mb-empty span { font-size: 12px; line-height: 1.7; max-width: 260px; }

/* ─────────────── Active request card ─────────────── */
.mb-active-request {
  padding: 16px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mb-active-request.is-pending {
  background: var(--el-color-primary-light-9);
  border-left: 3px solid var(--el-color-primary);
}
.mb-active-request.is-approved {
  background: #f0faf5;
  border-left: 3px solid #10b981;
}
.mb-active-request.is-rejected {
  background: #fff8f8;
  border-left: 3px solid #ef4444;
}
.mb-active-request.is-cancelled {
  background: var(--el-fill-color-light);
  border-left: 3px solid var(--el-border-color);
}
.mb-active-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
}
.mb-active-meta {
  display: flex;
  align-items: center;
  gap: 10px;
}
.mb-active-amount {
  font-size: 17px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.mb-active-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.mb-active-reason {
  margin: 10px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}
.mb-active-note {
  margin: 10px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  padding: 7px 10px;
  background: rgba(255,255,255,.6);
  border-radius: 6px;
}

/* ─────────────── History ─────────────── */
.mb-history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 18px 6px;
  font-size: 11px;
  font-weight: 600;
  color: var(--el-text-color-placeholder);
  letter-spacing: .04em;
  text-transform: uppercase;
  border-top: 1px solid var(--el-border-color-lighter);
}

/* ─────────────── Step track ─────────────── */
.mb-steps {
  display: flex;
  align-items: flex-start;
  padding: 14px 0 8px;
}
.mb-step {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  flex-shrink: 0;
}
.mb-step-node {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 700;
  background: var(--el-fill-color);
  color: var(--el-text-color-placeholder);
  border: 1.5px solid var(--el-border-color);
}
.mb-step.is-done .mb-step-node {
  background: var(--el-color-primary);
  color: #fff;
  border-color: var(--el-color-primary);
}
.mb-step.is-active .mb-step-node {
  background: var(--el-bg-color);
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 3px var(--el-color-primary-light-8);
}
.mb-step.is-active .mb-step-node::after {
  content: '';
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--el-color-primary);
}
.mb-step.is-rejected .mb-step-node {
  background: #fff0f0;
  color: #c24141;
  border-color: #fcc;
}
.mb-step > span {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}
.mb-step.is-done > span,
.mb-step.is-active > span { color: var(--el-text-color-primary); font-weight: 600; }
.mb-step.is-rejected > span { color: #c24141; }
.mb-step-line {
  flex: 1;
  height: 1.5px;
  background: var(--el-border-color-light);
  margin-top: 12px;
}
.mb-step-line.is-done { background: var(--el-color-primary); }

/* ─────────────── History ─────────────── */
.mb-history { display: flex; flex-direction: column; }
.mb-history-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mb-history-item:last-child { border-bottom: 0; }
.mb-history-amount { flex: 1; font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
.mb-history-total { font-weight: 400; font-size: 11px; color: var(--el-text-color-placeholder); }
.mb-history-scroll {
  max-height: 220px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: var(--el-border-color) transparent;
}
.mb-history-scroll::-webkit-scrollbar { width: 4px; }
.mb-history-scroll::-webkit-scrollbar-thumb { background: var(--el-border-color); border-radius: 2px; }
.mb-history-sentinel { height: 1px; }


/* ─────────────── Dialog ─────────────── */
.mb-dialog-summary {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 18px;
}
.mb-dialog-summary > div {
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-fill-color-light);
}
.mb-dialog-summary span { display: block; font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 6px; }
.mb-dialog-summary strong { font-size: 18px; color: var(--el-text-color-primary); }

/* ─────────────── Responsive ─────────────── */

/* ≤980px：左右双栏 → 上下单栏 */
@media (max-width: 980px) {
  .mb { padding: 20px 20px; }
  .mb-grid { grid-template-columns: 1fr; }
  .mb-kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .mb-panel { min-height: unset; }
  .mb-monthly-stats { grid-template-columns: repeat(3, 1fr); }
}

/* ≤720px */
@media (max-width: 720px) {
  .mb { padding: 14px 16px; }
  .mb-kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .mb-kpi strong { font-size: 22px; }

  /* 额度卡头部：允许换行 */
  .mb-panel-head { flex-wrap: wrap; gap: 8px; }
  .mb-panel-head-actions { width: 100%; justify-content: flex-start; }

  /* 余额块：竖排 */
  .mb-balance-block { flex-direction: column; align-items: flex-start; gap: 14px; }
  .mb-balance-ring { width: 80px; height: 80px; align-self: flex-end; margin-top: -56px; }

  /* 周期行 */
  .mb-period-item { grid-template-columns: 1fr auto; }
  .mb-period-reset { display: none; }

  /* 月度分析：3列→2列 */
  .mb-monthly-stats { grid-template-columns: repeat(2, 1fr); }

  /* 历史区域 */
  .mb-history-scroll { max-height: 160px; }
}

/* ≤480px */
@media (max-width: 480px) {
  .mb { padding: 12px; }
  .mb-kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1px; }
  .mb-kpi { padding: 10px 12px; }
  .mb-kpi strong { font-size: 18px; }

  /* 月度分析：→1列 */
  .mb-monthly-stats { grid-template-columns: 1fr; }

  /* dialog */
  .mb-dialog-summary { grid-template-columns: 1fr; }
}
</style>
