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
            <!-- ≥80% 显示扩容按钮 -->
            <el-button
              v-if="(item.used_ratio ?? 0) >= 0.8"
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

    <div class="mb-footer">
      <el-button type="primary" size="large" @click="router.push('/admin/expansion-requests')">
        申请扩容
      </el-button>
      <el-tooltip placement="top" content="预算按自然月重置，每月1日自动清零">
        <el-icon class="mb-footer__info"><InfoFilled /></el-icon>
      </el-tooltip>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, Wallet, WarningFilled, InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getMyBudget } from '@/services/control-plane'
import { useAuthStore } from '@/stores/auth'
import type { BudgetChainItem, BudgetScopeType } from '@/types/budget'
import { SCOPE_TYPE_LABELS, SCOPE_TYPE_COLORS } from '@/types/budget'

const router = useRouter()
const authStore = useAuthStore()

const chain = ref<BudgetChainItem[]>([])
const loading = ref(false)

const currentPeriod = new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long' })

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

// FE-18 + FE-36: 超支文案区分维度 + 恢复时间
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
  cursor: help;
  color: var(--el-text-color-secondary);
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
