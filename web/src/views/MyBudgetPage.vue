<template>
  <div class="budget-wallet">
    <header class="wallet-head">
      <div>
        <span class="eyebrow">Personal budget</span>
        <h1>我的预算</h1>
        <p>查看个人额度、部门预算链路和周期重置时间。</p>
      </div>
      <el-button :loading="loading" :icon="Refresh" circle @click="load" />
    </header>

    <div v-if="loading && !chain.length" v-loading="true" class="loading-block" />

    <el-empty v-else-if="!chain.length" description="暂无预算配置信息，请联系管理员" />

    <template v-else>
      <section class="hero-panel">
        <div class="hero-main">
          <span class="hero-label">{{ primaryBudgetLabel }}</span>
          <div class="hero-amount">
            <strong>{{ formatUSD(remainingAmount) }}</strong>
            <span>可用余额</span>
          </div>
          <div class="hero-meta">
            已用 {{ formatUSD(primaryBudget?.used_usd || 0) }} / 总额 {{ primaryBudget ? displayAmount(primaryBudget.amount_usd) : '未设置' }}
          </div>
          <el-progress
            :percentage="primaryRatioPercent"
            :stroke-width="10"
            :color="progressColor(primaryBudget?.used_ratio || 0)"
            class="hero-progress"
          />
        </div>

        <div class="hero-side">
          <div class="reset-card">
            <span>日额度</span>
            <strong>{{ displayAmount(primaryBudget?.daily_amount_usd ?? -1) }}</strong>
            <small>明天 00:00 重置</small>
          </div>
          <div class="reset-card">
            <span>周额度</span>
            <strong>{{ displayAmount(primaryBudget?.weekly_amount_usd ?? -1) }}</strong>
            <small>下周一 00:00 重置</small>
          </div>
          <div class="reset-card">
            <span>月额度</span>
            <strong>{{ primaryBudget ? displayAmount(primaryBudget.amount_usd) : '未设置' }}</strong>
            <small>下月 1 日 00:00 重置</small>
          </div>
        </div>

        <div class="hero-actions">
          <el-button v-if="canAdjustBudget" type="primary" @click="router.push('/admin/org')">去调整额度</el-button>
          <el-button v-if="canOpenExpansion" type="primary" plain @click="openExpansion">申请扩容</el-button>
          <el-button v-if="authStore.isManager" plain @click="router.push('/admin/expansion-requests')">查看申请</el-button>
        </div>
      </section>

      <section class="content-grid">
        <div class="section-block">
          <div class="section-head">
            <div>
              <h2>预算链路</h2>
              <p>请求会按成员、部门、工作室和公司预算逐层校验。</p>
            </div>
          </div>

          <div class="chain-list">
            <div v-for="item in chain" :key="`${item.scope_type}:${item.scope_id || item.label}`" class="chain-row">
              <div class="chain-left">
                <span class="scope-dot" :style="{ background: SCOPE_TYPE_COLORS[item.scope_type] }" />
                <div>
                  <strong>{{ item.label || SCOPE_TYPE_LABELS[item.scope_type] }}</strong>
                  <span>{{ SCOPE_TYPE_LABELS[item.scope_type] }}</span>
                </div>
              </div>
              <div class="chain-usage">
                <span>{{ formatUSD(item.used_usd || 0) }} / {{ displayAmount(item.amount_usd) }}</span>
                <el-progress
                  :percentage="Math.min(Math.round((item.used_ratio || 0) * 100), 100)"
                  :stroke-width="6"
                  :color="progressColor(item.used_ratio || 0)"
                />
              </div>
            </div>
          </div>
        </div>

        <aside class="section-block side-block">
          <h2>使用提示</h2>
          <div class="tip-list">
            <div>
              <strong>日/周额度</strong>
              <span>用于控制短周期消耗节奏，恢复后可继续使用。</span>
            </div>
            <div>
              <strong>月额度</strong>
              <span>作为硬限制，超出后需要等待重置或申请扩容。</span>
            </div>
            <div>
              <strong>扩容申请</strong>
              <span>普通成员申请个人额度，部门负责人审批。</span>
            </div>
          </div>
        </aside>
      </section>
    </template>

    <el-dialog v-model="expansionVisible" title="申请提升个人额度" width="520px">
      <div class="dialog-summary">
        <div>
          <span>当前额度</span>
          <strong>{{ primaryBudget ? displayAmount(primaryBudget.amount_usd) : '未设置' }}</strong>
        </div>
        <div>
          <span>已用金额</span>
          <strong>{{ formatUSD(primaryBudget?.used_usd || 0) }}</strong>
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
        <el-button type="primary" :loading="expansionSubmitting" @click="submitExpansion">提交申请</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { createExpansionRequest, getMyBudget } from '@/services/control-plane'
import { useAuthStore } from '@/stores/auth'
import type { BudgetChainItem } from '@/types/budget'
import { SCOPE_TYPE_COLORS, SCOPE_TYPE_LABELS } from '@/types/budget'

const router = useRouter()
const authStore = useAuthStore()

const chain = ref<BudgetChainItem[]>([])
const loading = ref(false)
const expansionVisible = ref(false)
const expansionSubmitting = ref(false)
const expansionForm = ref({ requestedAmount: 100, reason: '' })

const primaryBudget = computed(() =>
  chain.value.find(item => item.scope_type === 'member') ||
  chain.value.find(item => item.scope_type === 'dept_daily') ||
  chain.value[0]
)
const primaryBudgetLabel = computed(() => primaryBudget.value?.label || '个人可用预算')
const primaryRatioPercent = computed(() => Math.min(Math.round((primaryBudget.value?.used_ratio || 0) * 100), 100))
const remainingAmount = computed(() => {
  const budget = primaryBudget.value
  if (!budget || budget.amount_usd < 0) return 0
  return Math.max((budget.amount_usd || 0) - (budget.used_usd || 0), 0)
})
const canAdjustBudget = computed(() => authStore.effectiveRole === 'admin' || authStore.effectiveRole === 'studio_admin')
const canOpenExpansion = computed(() => authStore.effectiveRole !== 'admin')

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
  return '#2BA8A2'
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
      requested_amount: expansionForm.value.requestedAmount,
      reason: expansionForm.value.reason,
    })
    ElMessage.success('申请已提交给部门负责人')
    expansionVisible.value = false
  } finally {
    expansionSubmitting.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.budget-wallet {
  max-width: 1120px;
  margin: 0 auto;
  padding: 32px;
}
.wallet-head {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  align-items: flex-start;
  margin-bottom: 22px;
}
.eyebrow {
  color: var(--el-color-primary);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
}
.wallet-head h1 {
  margin: 4px 0 6px;
  font-size: 28px;
  color: var(--el-text-color-primary);
}
.wallet-head p {
  margin: 0;
  color: var(--el-text-color-secondary);
}
.loading-block {
  height: 360px;
}
.hero-panel {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  gap: 28px;
  padding: 28px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 22px;
  background:
    radial-gradient(circle at 10% 0%, rgba(43,168,162,.14), transparent 32%),
    linear-gradient(135deg, var(--el-bg-color), var(--el-fill-color-light));
}
.hero-main {
  min-width: 0;
}
.hero-label {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.hero-amount {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin: 16px 0 8px;
}
.hero-amount strong {
  font-size: 48px;
  line-height: 1;
  color: var(--el-text-color-primary);
  letter-spacing: -0.04em;
}
.hero-amount span,
.hero-meta {
  color: var(--el-text-color-secondary);
}
.hero-progress {
  max-width: 560px;
  margin-top: 24px;
}
.hero-side {
  display: grid;
  gap: 10px;
}
.reset-card {
  padding: 14px 16px;
  border-radius: 14px;
  background: rgba(255,255,255,.56);
  border: 1px solid var(--el-border-color-lighter);
}
.reset-card span,
.reset-card small {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.reset-card strong {
  display: block;
  margin: 4px 0;
  color: var(--el-text-color-primary);
  font-size: 18px;
}
.hero-actions {
  grid-column: 1 / -1;
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  padding-top: 4px;
}
.content-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 18px;
  margin-top: 18px;
}
.section-block {
  padding: 20px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 18px;
  background: var(--el-bg-color);
}
.section-head {
  display: flex;
  justify-content: space-between;
  margin-bottom: 14px;
}
.section-head h2,
.side-block h2 {
  margin: 0;
  color: var(--el-text-color-primary);
  font-size: 16px;
}
.section-head p {
  margin: 4px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.chain-list {
  display: flex;
  flex-direction: column;
}
.chain-row {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  gap: 18px;
  align-items: center;
  padding: 14px 0;
  border-top: 1px solid var(--el-border-color-lighter);
}
.chain-row:first-child {
  border-top: none;
}
.chain-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.scope-dot {
  width: 8px;
  height: 8px;
  border-radius: 99px;
  flex-shrink: 0;
}
.chain-left strong {
  display: block;
  color: var(--el-text-color-primary);
}
.chain-left span:not(.scope-dot) {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-top: 2px;
}
.chain-usage {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  gap: 12px;
  align-items: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.tip-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
  margin-top: 16px;
}
.tip-list strong {
  display: block;
  margin-bottom: 4px;
  color: var(--el-text-color-primary);
}
.tip-list span {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}
.dialog-summary {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 18px;
}
.dialog-summary > div {
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-fill-color-light);
}
.dialog-summary span {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-bottom: 6px;
}
.dialog-summary strong {
  color: var(--el-text-color-primary);
  font-size: 18px;
}
@media (max-width: 980px) {
  .hero-panel,
  .content-grid {
    grid-template-columns: 1fr;
  }
  .chain-row,
  .chain-usage {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .budget-wallet {
    padding: 18px;
  }
  .wallet-head {
    align-items: center;
  }
  .hero-panel {
    padding: 20px;
  }
  .hero-amount strong {
    font-size: 38px;
  }
}
</style>
