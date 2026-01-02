<template>
  <div class="mb">
    <div class="mb-header">
      <div class="mb-header__left">
        <h2 class="mb-title">我的预算</h2>
        <span class="mb-subtitle">{{ currentPeriod }}</span>
      </div>
      <el-button :loading="loading" :icon="Refresh" circle size="small" title="刷新" @click="load" />
    </div>

    <div v-if="loading && !chain.length" v-loading="true" class="mb-loading" />

    <div v-else-if="!chain.length" class="mb-empty">
      <el-icon size="64" color="#c0c4cc"><Wallet /></el-icon>
      <p class="mb-empty__text">暂无预算配置信息，请联系管理员</p>
    </div>

    <div v-else class="mb-chain">
      <div
        v-for="(item, idx) in chain"
        :key="idx"
        class="mb-node"
        :style="{ animationDelay: `${idx * 80}ms` }"
      >
        <div class="mb-node__dot" :style="{ background: getDotColor(item) }" />
        <div v-if="idx < chain.length - 1" class="mb-node__line" :style="{ borderColor: getLineColor(idx) }" />
        
        <div class="mb-card" :style="getCardStyle(item)">
          <div class="mb-card__left">
            <div class="mb-card__scope-dot" :style="{ background: SCOPE_TYPE_COLORS[item.scope_type] }" />
            <div class="mb-card__labels">
              <span class="mb-card__scope-label" :style="{ color: SCOPE_TYPE_COLORS[item.scope_type] }">
                {{ SCOPE_TYPE_LABELS[item.scope_type] }}
              </span>
              <span v-if="item.label" class="mb-card__custom-label">{{ item.label }}</span>
            </div>
          </div>

          <div class="mb-card__center">
            <span v-if="item.amount_usd >= 0" class="mb-card__amount">${{ formatAmount(item.amount_usd) }}</span>
            <span v-else class="mb-card__amount mb-card__amount--unset">未设置</span>
            <!-- 日/周限额标签 -->
            <div v-if="item.amount_usd >= 0" class="mb-card__limits">
              <span v-if="item.weekly_amount_usd != null && item.weekly_amount_usd >= 0" class="limit-tag">周 ${{ formatAmount(item.weekly_amount_usd) }}</span>
              <span v-if="item.daily_amount_usd != null && item.daily_amount_usd >= 0" class="limit-tag">日 ${{ formatAmount(item.daily_amount_usd) }}</span>
            </div>
            <div v-if="item.amount_usd >= 0" class="mb-card__reset">
              {{ resetSummary(item) }}
            </div>
          </div>

          <div v-if="item.amount_usd >= 0" class="mb-card__right">
            <el-progress
              type="circle"
              :percentage="Math.round((item.used_ratio ?? 0) * 100)"
              :width="52"
              :stroke-width="6"
              :color="getProgressColor(item.used_ratio ?? 0)"
              :format="() => `${Math.round((item.used_ratio ?? 0) * 100)}%`"
            />
            <!-- 超支文案 + 恢复时间 -->
            <div v-if="(item.used_ratio ?? 0) >= 1" class="mb-card__exhaust-hint danger">
              {{ exhaustMessage(item) }}
            </div>
            <!-- ≥80% 且有申请权限才显示扩容按钮 -->
            <el-button
              v-if="(item.used_ratio ?? 0) >= 0.8 && canRequestExpansion(item)"
              type="danger"
              size="small"
              link
              class="mb-card__expand-btn"
              @click="goExpansion(item)"
            >申请扩容</el-button>
          </div>
          <div v-else class="mb-card__right">
            <div class="mb-card__unset-indicator">
              <el-icon color="#c0c4cc" size="28"><WarningFilled /></el-icon>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 底部操作区：按角色显示不同入口 -->
    <div class="mb-footer">
      <template v-if="authStore.effectiveRole === 'admin'">
        <el-button type="primary" size="large" @click="router.push('/admin/org')">
          去调整额度
        </el-button>
      </template>
      <template v-else-if="authStore.effectiveRole === 'studio_admin'">
        <el-button type="primary" size="large" @click="router.push('/admin/org')">
          去调整额度
        </el-button>
        <el-button size="large" @click="router.push('/admin/expansion-requests')">
          申请扩容
        </el-button>
      </template>
      <template v-else-if="authStore.effectiveRole === 'dept_admin'">
        <el-button type="primary" size="large" @click="router.push('/admin/expansion-requests')">
          申请扩容
        </el-button>
      </template>
      <template v-else>
        <!-- 普通员工：申请提升自己的成员额度 -->
        <el-button type="primary" size="large" @click="openUserExpansionDialog">
          申请扩容
        </el-button>
      </template>
      <el-tooltip placement="top" content="预算按自然月重置，每月1日自动清零">
        <el-icon class="mb-footer__info"><InfoFilled /></el-icon>
      </el-tooltip>
    </div>

    <el-dialog v-model="expansionVisible" title="申请提升个人额度" width="520px">
      <div class="expansion-summary">
        <div>
          <span>当前个人额度</span>
          <strong>{{ personalBudget ? `$${formatAmount(personalBudget.amount_usd)}` : '未单独设置' }}</strong>
        </div>
        <div>
          <span>当前已用</span>
          <strong>{{ personalBudget ? `$${formatAmount(personalBudget.used_usd || 0)}` : '-' }}</strong>
        </div>
      </div>
      <el-form label-position="top">
        <el-form-item label="申请后的月额度（USD）">
          <el-input-number v-model="expansionForm.requestedAmount" :min="1" :precision="2" controls-position="right" style="width:100%" />
        </el-form-item>
        <el-form-item label="申请理由">
          <el-input v-model="expansionForm.reason" type="textarea" :rows="3" placeholder="说明业务场景、预计消耗和需要提升个人额度的原因" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="expansionVisible = false">取消</el-button>
        <el-button type="primary" :loading="expansionSubmitting" @click="submitUserExpansion">提交申请</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, Wallet, WarningFilled, InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { createExpansionRequest, getMyBudget } from '@/services/control-plane'
import { useAuthStore } from '@/stores/auth'
import type { BudgetChainItem, BudgetScopeType } from '@/types/budget'
import { SCOPE_TYPE_LABELS, SCOPE_TYPE_COLORS } from '@/types/budget'

const router = useRouter()
const authStore = useAuthStore()

const chain = ref<BudgetChainItem[]>([])
const loading = ref(false)
const expansionVisible = ref(false)
const expansionSubmitting = ref(false)
const expansionForm = ref({ requestedAmount: 100, reason: '' })

const currentPeriod = new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long' })
const personalBudget = computed(() => chain.value.find(item => item.scope_type === 'member'))

async function load() {
  loading.value = true
  try {
    chain.value = await getMyBudget()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '预算加载失败')
  } finally {
    loading.value = false
  }
}

function formatAmount(v: number) {
  return v.toFixed(2)
}

function getProgressColor(ratio: number) {
  if (ratio > 0.9) return '#ef4444'
  if (ratio >= 0.7) return '#f59e0b'
  return '#10b981'
}

function getDotColor(item: BudgetChainItem) {
  if (item.amount_usd < 0) return '#c0c4cc'
  const r = item.used_ratio ?? 0
  if (r > 0.9) return '#ef4444'
  if (r >= 0.7) return '#f59e0b'
  return '#10b981'
}

function getLineColor(idx: number) {
  return getDotColor(chain.value[idx + 1])
}

function getCardStyle(item: BudgetChainItem) {
  const c = SCOPE_TYPE_COLORS[item.scope_type]
  return {
    background: `linear-gradient(135deg, ${c}12 0%, var(--el-bg-color) 60%)`,
    borderColor: `${c}40`,
  }
}

function goExpansion(item: BudgetChainItem) {
  router.push({
    path: '/admin/expansion-requests',
    query: {
      scope_type: item.scope_type,
      scope_id: item.scope_id || '',
      requested_amount: String(Math.ceil((item.amount_usd || 0) * 1.5)),
    },
  })
}

function openUserExpansionDialog() {
  const userId = authStore.user?.id || ''
  if (!userId) {
    ElMessage.warning('无法确定当前用户，请重新登录')
    return
  }
  const base = personalBudget.value?.amount_usd || 50
  expansionForm.value = {
    requestedAmount: Math.max(Math.ceil(base * 1.5), base + 50),
    reason: '',
  }
  expansionVisible.value = true
}

async function submitUserExpansion() {
  const userId = authStore.user?.id || ''
  if (!userId) {
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
      scope_id: userId,
      requested_amount: expansionForm.value.requestedAmount,
      reason: expansionForm.value.reason,
    })
    ElMessage.success('申请已提交给部门负责人')
    expansionVisible.value = false
  } finally {
    expansionSubmitting.value = false
  }
}

// 判断某层预算是否可以发起扩容申请
// - admin：直接改，不申请
// - studio_admin：工作室层可申请（向 admin 申请）
// - dept_admin：部门层可申请
// - user：不可申请（联系部门负责人处理）
function canRequestExpansion(item: BudgetChainItem): boolean {
  const role = authStore.effectiveRole
  if (role === 'admin') return false
  if (role === 'studio_admin') return item.scope_type === 'studio'
  if (role === 'dept_admin') return item.scope_type === 'dept_daily' || item.scope_type === 'dept_service'
  return item.scope_type === 'member'
}

function resetSummary(item: BudgetChainItem): string {
  const parts = ['月：下月 1 日 00:00']
  if (item.weekly_amount_usd != null && item.weekly_amount_usd >= 0) {
    parts.unshift('周：下周一 00:00')
  }
  if (item.daily_amount_usd != null && item.daily_amount_usd >= 0) {
    parts.unshift('日：明天 00:00')
  }
  return `重置 ${parts.join(' · ')}`
}

function exhaustMessage(item: BudgetChainItem): string {
  // 判断哪个维度超了（优先级：日 > 周 > 月）
  if (item.daily_amount_usd != null && item.daily_amount_usd >= 0) {
    return '今日限额已用尽，明天 00:00 恢复'
  }
  if (item.weekly_amount_usd != null && item.weekly_amount_usd >= 0) {
    return '本周限额已用尽，下周一恢复'
  }
  return '月度预算已用尽，下月 1 号恢复或申请扩容'
}

onMounted(load)
</script>

<style scoped>
.mb {
  padding: 24px;
  max-width: 640px;
  margin: 0 auto;
}

.mb-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 28px;
}

.mb-header__left {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.mb-title {
  font-size: 22px;
  font-weight: 700;
  margin: 0;
  color: var(--el-text-color-primary);
}

.mb-subtitle {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.mb-loading {
  height: 300px;
}

.mb-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 320px;
  gap: 16px;
}

.mb-empty__text {
  color: var(--el-text-color-secondary);
  font-size: 14px;
}

.mb-chain {
  position: relative;
  padding-left: 20px;
}

.mb-node {
  position: relative;
  padding-left: 28px;
  padding-bottom: 20px;
  animation: mb-fade-in 0.4s ease-out both;
}

.mb-node:last-child {
  padding-bottom: 0;
}

.mb-node__dot {
  position: absolute;
  left: 0;
  top: 28px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  z-index: 2;
  box-shadow: 0 0 0 3px var(--el-bg-color);
}

.mb-node__line {
  position: absolute;
  left: 5px;
  top: 40px;
  bottom: 0;
  width: 2px;
  border-left: 2px solid #c0c4cc;
  z-index: 1;
}

.mb-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 18px 20px;
  border-radius: 12px;
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
  background: var(--el-bg-color);
  transition: transform 0.18s, box-shadow 0.18s;
}

.mb-card:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.08);
}

.mb-card__left {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
  min-width: 120px;
}

.mb-card__scope-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}

.mb-card__labels {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mb-card__scope-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
  text-transform: uppercase;
}

.mb-card__custom-label {
  font-size: 13px;
  color: var(--el-text-color-regular);
  font-weight: 500;
}

.mb-card__center {
  flex: 1;
  text-align: center;
}

.mb-card__amount {
  font-size: 24px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.mb-card__amount--unset {
  color: var(--el-text-color-placeholder);
  font-size: 16px;
  font-weight: 500;
}

.mb-card__limits {
  display: flex;
  gap: 6px;
  justify-content: center;
  margin-top: 4px;
}
.limit-tag {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 4px;
}
.mb-card__reset {
  margin-top: 5px;
  color: var(--el-text-color-secondary);
  font-size: 11px;
  text-align: center;
}
.mb-card__expand-btn {
  margin-top: 6px;
  font-weight: 600;
}
.mb-card__exhaust-hint {
  font-size: 11px;
  margin-top: 4px;
  text-align: center;
  line-height: 1.3;
}
.mb-card__exhaust-hint.danger {
  color: var(--el-color-danger);
  font-weight: 500;
}

.mb-card__right {
  flex-shrink: 0;
  width: 52px;
  height: 52px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.mb-card__unset-indicator {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: var(--el-fill-color-light);
  display: flex;
  align-items: center;
  justify-content: center;
}

.mb-footer {
  margin-top: 36px;
  display: flex;
  align-items: center;
  gap: 10px;
  justify-content: center;
}

.mb-footer__info {
  font-size: 18px;
  color: var(--el-text-color-placeholder);
  cursor: pointer;
}
.mb-footer__tip {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.expansion-summary {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 18px;
}

.expansion-summary > div {
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  background: var(--el-fill-color-light);
}

.expansion-summary span {
  display: block;
  margin-bottom: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.expansion-summary strong {
  color: var(--el-text-color-primary);
  font-size: 18px;
}

@keyframes mb-fade-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (max-width: 640px) {
  .mb {
    padding: 16px;
  }

  .mb-card {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
    padding: 16px;
  }

  .mb-card__left {
    min-width: unset;
  }

  .mb-card__center {
    text-align: left;
  }

  .mb-card__right {
    align-self: flex-end;
  }
}

html.dark .mb-card {
  background: var(--el-bg-color);
}

html.dark .mb-node__dot {
  box-shadow: 0 0 0 3px var(--el-bg-color);
}
</style>
