<template>
  <div class="mtb" v-loading="loading">
    <!-- 临时扩容横幅 -->
    <el-alert
      v-if="tierBudget?.source_summary.has_temporary_override"
      title="临时扩容生效"
      type="warning"
      show-icon
      :closable="false"
      class="mtb-alert"
    >
      <template #default>
        当前存在临时额度调整，覆盖了部分等级的默认预算。
      </template>
    </el-alert>

    <!-- 日/周/月切换 -->
    <div class="mtb-period-toggle">
      <el-radio-group v-model="selectedPeriod" size="small">
        <el-radio-button value="daily">日额度</el-radio-button>
        <el-radio-button value="weekly">周额度</el-radio-button>
        <el-radio-button value="monthly">月额度</el-radio-button>
      </el-radio-group>
    </div>

    <!-- 空状态 -->
    <el-empty v-if="!loading && (!tierBudget || !tierBudget.tier_limits.length)" description="暂无模型等级额度配置" />

    <template v-else-if="tierBudget">
      <!-- 各等级额度列表 -->
      <div class="mtb-list">
        <div
          v-for="tier in tierBudget.tier_limits"
          :key="tier.model_tier_id"
          class="mtb-card"
        >
          <!-- 等级名称 + 颜色圆点 -->
          <div class="mtb-card-head">
            <div class="mtb-card-title">
              <span class="mtb-color-dot" :style="{ background: tierColor(tier.model_tier_id) }" />
              <strong>{{ tier.model_tier_name }}</strong>
            </div>
            <span
              v-if="periodInfo(tier)"
              :class="`mtb-tag mtb-tag--${statusTagType(periodInfo(tier)!)}`"
            >
              {{ statusLabel(periodInfo(tier)!) }}
            </span>
          </div>

          <!-- 来源说明 -->
          <div class="mtb-card-source">
            {{ sourceLabel(tier) }}
          </div>

          <template v-if="periodInfo(tier)">
            <!-- 使用量进度条 -->
            <div class="mtb-progress-section">
              <el-progress
                :percentage="usagePercent(periodInfo(tier)!)"
                :stroke-width="10"
                :color="progressColor(periodInfo(tier)!)"
                :show-text="false"
              />
            </div>

            <!-- 可用金额 + 已用/总额 -->
            <div class="mtb-card-stats">
              <div class="mtb-stat">
                <span class="mtb-stat-label">可用</span>
                <strong class="mtb-stat-value">{{ formatUSD(periodInfo(tier)!.remaining_usd) }}</strong>
              </div>
              <div class="mtb-stat">
                <span class="mtb-stat-label">已用</span>
                <span>{{ formatUSD(periodInfo(tier)!.used_usd) }}</span>
              </div>
              <div class="mtb-stat">
                <span class="mtb-stat-label">总额</span>
                <span>{{ periodInfo(tier)!.configured ? formatUSD(periodInfo(tier)!.effective_limit_usd) : '未配置' }}</span>
              </div>
            </div>

            <!-- 临时额度标记 -->
            <el-tag
              v-if="periodInfo(tier)!.temporary_target_usd"
              size="small"
              type="warning"
              effect="plain"
              class="mtb-temp-tag"
            >
              临时额度 ${{ periodInfo(tier)!.temporary_target_usd }}
            </el-tag>
          </template>
          <div v-else class="mtb-no-period">
            该等级在当前周期无额度配置
          </div>
        </div>
      </div>
    </template>

    <!-- 额度如何生效说明 -->
    <el-collapse class="mtb-help">
      <el-collapse-item title="额度如何生效">
        <ol class="mtb-help-list">
          <li><strong>个人临时额度</strong>：管理员通过扩容审批临时调整的额度，优先级最高。</li>
          <li><strong>个人等级子预算</strong>：在个人总预算内为各等级分配的子预算额度。</li>
          <li><strong>个人未配置</strong>：未为等级单独分配预算时，共享总预算池。</li>
          <li><strong>部门共享子预算</strong>：部门预算中为各等级分配的额度，个人无独立配额时生效。</li>
        </ol>
      </el-collapse-item>
    </el-collapse>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { getMyBudgetTiers } from '@/services/control-plane'
import type { MyBudgetTiersResponse, MyBudgetTierLimit, TierPeriodUsage } from '@/types/model-tier'
import type { ExpansionPeriod } from '@/types/expansion'

const loading = ref(false)
const tierBudget = ref<MyBudgetTiersResponse | null>(null)
const selectedPeriod = ref<ExpansionPeriod>('monthly')

function periodInfo(tier: MyBudgetTierLimit): TierPeriodUsage | undefined {
  return tier.periods.find(p => p.period_type === selectedPeriod.value)
}

function usagePercent(period: TierPeriodUsage): number {
  if (!period.configured || !period.effective_limit_usd || Number(period.effective_limit_usd) <= 0) return 0
  const used = Number(period.used_usd || 0)
  const limit = Number(period.effective_limit_usd)
  return Math.min(100, Math.round((used / limit) * 100))
}

function formatUSD(value?: string | number): string {
  const n = Number(value || 0)
  return `$${n.toLocaleString('en-US', { maximumFractionDigits: 2 })}`
}

function statusTagType(period: TierPeriodUsage): string {
  if (period.status === 'tier_disabled' || period.status === 'limit_disabled') return 'info'
  if (!period.configured) return 'info'
  if (Number(period.effective_limit_usd) === 0) return 'danger'
  const pct = usagePercent(period)
  if (pct >= 80) return 'danger'
  if (pct >= 60) return 'warning'
  return 'success'
}

function statusLabel(period: TierPeriodUsage): string {
  if (period.status === 'tier_disabled') return '等级已停用'
  if (period.status === 'limit_disabled') return '额度已停用'
  if (!period.configured) return '未配置'
  if (Number(period.effective_limit_usd) === 0) return '不可使用'
  const pct = usagePercent(period)
  if (pct >= 80) return '预警'
  if (pct >= 60) return '偏高'
  return '正常'
}

function progressColor(period: TierPeriodUsage): string {
  const pct = usagePercent(period)
  if (pct >= 80) return '#ef4444'
  if (pct >= 60) return '#f59e0b'
  return '#36B34E'
}

function sourceLabel(tier: MyBudgetTierLimit): string {
  if (!tier.enabled) return '等级已停用'
  // 找第一个有配置的周期的来源
  for (const p of tier.periods) {
    if (p.configured) {
      const scope = p.limiting_source?.scope_type
      if (scope === 'member') {
        if (p.temporary_target_usd) return '个人临时额度'
        return '个人等级子预算'
      }
      if (scope === 'dept_daily' || scope === 'dept_service') return '部门共享子预算'
      return '共享预算'
    }
  }
  return '个人未配置'
}

// 简单的颜色映射，基于 tier index
const tierColors = ['#409EFF', '#67C23A', '#E6A23C', '#F56C6C', '#909399', '#B37FEB', '#36B34E', '#F2C94C']
function tierColor(_tierId: string): string {
  if (!tierBudget.value) return '#409EFF'
  const idx = tierBudget.value.tier_limits.findIndex(t => t.model_tier_id === _tierId)
  return tierColors[idx % tierColors.length]
}

defineExpose({ loadTierBudget })

async function loadTierBudget() {
  loading.value = true
  try {
    tierBudget.value = await getMyBudgetTiers()
  } catch {
    tierBudget.value = null
  } finally {
    loading.value = false
  }
}

onMounted(loadTierBudget)
</script>

<style scoped>
.mtb {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mtb-alert {
  margin: 0;
}

.mtb-period-toggle {
  display: flex;
  justify-content: flex-end;
}

.mtb-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mtb-card {
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
  padding: 16px 18px;
}

.mtb-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.mtb-card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
}

.mtb-color-dot {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}

.mtb-tag {
  display: inline-block;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 999px;
  line-height: 1.6;
  flex-shrink: 0;
}
.mtb-tag--success { background: var(--color-success-bg); color: var(--color-success); }
.mtb-tag--warning { background: var(--color-warning-bg); color: var(--color-warning); }
.mtb-tag--danger  { background: var(--color-danger-bg); color: var(--color-danger); }
.mtb-tag--info    { background: var(--color-info-bg); color: var(--color-info); }

.mtb-card-source {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 12px;
}

.mtb-progress-section {
  margin-bottom: 12px;
}

.mtb-card-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}

.mtb-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.mtb-stat-label {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}
.mtb-stat-value {
  font-size: 18px;
  font-weight: 800;
  color: var(--el-color-primary);
}
.mtb-stat span {
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.mtb-temp-tag {
  margin-top: 8px;
}

.mtb-no-period {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  padding: 8px 0;
}

.mtb-help {
  margin-top: 4px;
}
.mtb-help :deep(.el-collapse-item__header) {
  font-size: 13px;
  font-weight: 600;
}
.mtb-help-list {
  margin: 0;
  padding-left: 20px;
  font-size: 12px;
  line-height: 2;
  color: var(--el-text-color-secondary);
}
.mtb-help-list strong {
  color: var(--el-text-color-primary);
}
</style>