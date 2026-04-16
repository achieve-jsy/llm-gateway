<template>
  <el-drawer v-model="visible" title="扩容审批详情" size="min(720px, 100%)">
    <template v-if="detail">
      <section class="decision-hero" :class="{ frozen: requiresRecheck, blocked: isBlocked }">
        <div>
          <span class="eyebrow">{{ statusTitle }}</span>
          <h3>{{ decisionTitle }}</h3>
          <p>{{ decisionCopy }}</p>
        </div>
        <el-button v-if="detail.capabilities.can_reprecheck" type="warning" :loading="working" @click="recheck">
          刷新数据
        </el-button>
      </section>

      <section class="request-summary">
        <div class="summary-main">
          <span>{{ scopeLabel }}</span>
          <strong>{{ scopeName }}</strong>
          <small>申请人 {{ detail.request.requester_name || '—' }} · {{ LEVEL_LABELS[detail.request.current_level] }}</small>
        </div>
        <div class="summary-tags">
          <el-tag :type="detail.request.dimension_type === 'tier' ? 'primary' : 'success'">{{ limitItemLabel }}</el-tag>
          <el-tag v-if="detail.request.priority === 'urgent'" type="danger">紧急</el-tag>
          <el-tag :type="detail.request.duration_type === 'current_period' ? 'warning' : 'success'">{{ durationLabel }}</el-tag>
        </div>
      </section>

      <section class="detail-section metric-section">
        <header class="section-head">
          <h3>额度变化</h3>
          <span v-if="dataAsOf">数据更新于 {{ formatDateTime(dataAsOf) }}</span>
        </header>
        <div class="metric-list">
          <div class="metric-card">
            <span>提交时上限</span>
            <strong>{{ money(detail.submitted_snapshot.current_limit_usd) }}</strong>
          </div>
          <div class="metric-card">
            <span>实时上限</span>
            <strong>{{ money(detail.live_snapshot?.current_limit_usd) }}</strong>
          </div>
          <div class="metric-card">
            <span>提交时已用</span>
            <strong>{{ money(detail.submitted_snapshot.used_usd) }}</strong>
          </div>
          <div class="metric-card">
            <span>当前已用</span>
            <strong>{{ money(currentUsed) }}</strong>
          </div>
          <div class="metric-card">
            <span>申请目标</span>
            <strong class="target">{{ money(targetAmount) }}</strong>
          </div>
          <div class="metric-card">
            <span>提交时增加</span>
            <strong>{{ money(submittedIncrease) }}</strong>
          </div>
          <div class="metric-card">
            <span>按实时增加</span>
            <strong>{{ money(liveIncrease) }}</strong>
          </div>
        </div>
      </section>

      <section class="detail-section effect-panel" :class="{ frozen: requiresRecheck, blocked: isBlocked }">
        <h3>{{ effectTitle }}</h3>
        <p>{{ effectCopy }}</p>
      </section>

      <section class="detail-section impact-section">
        <header class="section-head">
          <h3>影响范围</h3>
          <span>{{ detail.affected_groups.length }} 个模型组</span>
        </header>
        <div v-if="visibleGroups.length" class="group-list">
          <el-tag v-for="group in visibleGroups" :key="group.id" size="small">{{ group.name }}</el-tag>
        </div>
        <el-empty v-else description="暂无受影响模型组" :image-size="48" />
        <el-button
          v-if="hiddenGroupCount > 0 || showAllGroups"
          class="group-toggle"
          link
          type="primary"
          @click="showAllGroups = !showAllGroups"
        >
          {{ showAllGroups ? '收起' : `查看全部 ${detail.affected_groups.length} 个` }}
        </el-button>
      </section>

      <section class="detail-section">
        <header class="section-head">
          <h3>审批信息</h3>
          <span>{{ detail.steps.length }} 条流水</span>
        </header>
        <div class="approval-pool">
          <span>当前审批池</span>
          <strong>{{ approvalPoolSummary }}</strong>
        </div>
        <el-timeline class="timeline">
          <el-timeline-item v-for="step in detail.steps" :key="step.id" :timestamp="formatDateTime(step.created_at)">
            {{ LEVEL_LABELS[step.level as keyof typeof LEVEL_LABELS] || step.level }} · {{ actionLabel(step.action) }}<span v-if="step.comment">：{{ step.comment }}</span>
          </el-timeline-item>
        </el-timeline>
      </section>

      <div class="actions">
        <template v-if="detail.capabilities.can_decide">
          <el-input v-model="comment" placeholder="审批意见（驳回和上报必填）" />
          <el-button type="danger" plain :loading="working" @click="decide('reject')">驳回</el-button>
          <el-button type="primary" :loading="working" :disabled="approveDisabled" @click="decide('approve')">批准并生效</el-button>
        </template>
      </div>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { decideExpansionV2, reprecheckExpansion } from '@/services/control-plane'
import {
  LEVEL_LABELS,
  type ExpansionAction,
  type ExpansionDetail,
} from '@/types/expansion'

const detail = defineModel<ExpansionDetail | null>('detail', { default: null })
const visible = defineModel<boolean>({ default: false })
const emit = defineEmits<{ changed: [] }>()
const comment = ref('')
const working = ref(false)
const showAllGroups = ref(false)

const requiresRecheck = computed(() => detail.value?.request.status === 'precheck_required' ||
  detail.value?.request.status === 'pending_reconfirmation' ||
  !!detail.value?.live_snapshot?.stale ||
  (!!detail.value?.live_snapshot && detail.value.live_snapshot.precheck_version !== detail.value.request.precheck_version))

const isBlocked = computed(() => false)
const targetAmount = computed(() => detail.value?.submitted_snapshot.target_amount_usd)
const submittedLimit = computed(() => detail.value?.submitted_snapshot.current_limit_usd)
const liveLimit = computed(() => detail.value?.live_snapshot?.current_limit_usd)
const currentUsed = computed(() => detail.value?.live_snapshot?.current_used_usd || detail.value?.submitted_snapshot.used_usd)
const dataAsOf = computed(() => detail.value?.live_snapshot?.data_as_of || detail.value?.data_as_of)
const submittedIncrease = computed(() => delta(targetAmount.value, submittedLimit.value))
const liveIncrease = computed(() => delta(targetAmount.value, liveLimit.value))
const visibleGroups = computed(() => {
  const groups = detail.value?.affected_groups || []
  return showAllGroups.value ? groups : groups.slice(0, 8)
})
const hiddenGroupCount = computed(() => Math.max((detail.value?.affected_groups.length || 0) - visibleGroups.value.length, 0))
const approveDisabled = computed(() => isBlocked.value || !detail.value?.live_snapshot)

const scopeLabelMap: Record<string, string> = {
  company: '公司预算',
  studio: '工作室预算',
  dept_daily: '成员日常预算池',
  dept_service: '程序服务预算池',
  member: '个人额度',
}
const scopeName = computed(() => {
  const request = detail.value?.request
  if (!request) return '—'
  if (request.scope_type === 'member') return request.scope_name || request.requester_name || '个人额度'
  return request.scope_name || request.scope_id || '—'
})
const scopeLabel = computed(() => scopeLabelMap[detail.value?.request.scope_type || ''] || detail.value?.request.scope_type || '预算主体')
const limitItemLabel = computed(() => {
  if (!detail.value) return '—'
  return detail.value.request.dimension_type === 'tier'
    ? `${detail.value.request.model_tier_name || '模型等级'}额度`
    : '总预算'
})
const statusTitle = computed(() => {
  if (isBlocked.value) return '需谨慎审批'
  return '可审批'
})
const decisionTitle = computed(() => {
  if (isBlocked.value) return '批准后仍可能受上级限制'
  return '最新状态可批准，批准后将自动生效'
})
const decisionCopy = computed(() => {
  if (isBlocked.value) return '当前申请需要进一步处理后才能批准。'
  return '当前目标高于已用金额，可进入审批确认。'
})
const effectTitle = computed(() => '批准后效果')
const effectCopy = computed(() => {
  if (isBlocked.value) return '批准只会修改当前申请项。'
  if (detail.value?.request.duration_type === 'current_period') {
    return `${limitItemLabel.value} 将在当前周期临时调整为 ${money(targetAmount.value)}，周期结束后恢复长期基础额度，本周期已用金额不会清零。`
  }
  return `${limitItemLabel.value} 将调整为 ${money(targetAmount.value)}，本周期已用金额不会清零。`
})
const durationLabel = computed(() => detail.value?.request.duration_type === 'current_period' ? '临时调整' : '长期调额')
const approvalPoolSummary = computed(() => {
  const pool = detail.value?.approval_pool || []
  if (!pool.length) return '暂无可用审批人'
  if (pool.length <= 2) return `${pool.length} 人可处理`
  return `${pool.length} 人可处理`
})

function delta(target?: string, base?: string) {
  const targetNumber = Number(target)
  const baseNumber = Number(base)
  return Number.isFinite(targetNumber) && Number.isFinite(baseNumber)
    ? Math.max(targetNumber - baseNumber, 0).toFixed(6)
    : undefined
}

function money(value?: string | number | null) {
  const number = Number(value)
  if (!Number.isFinite(number)) return '—'
  return `$${number.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function formatDateTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

function actionLabel(action: string) {
  return ({ approve: '批准', reject: '驳回', escalate: '上报', cancel: '取消', submitted: '提交' } as Record<string, string>)[action] || action
}

async function recheck() {
  if (!detail.value) return
  working.value = true
  try { detail.value = await reprecheckExpansion(detail.value.request.id) } finally { working.value = false }
}

async function decide(action: ExpansionAction) {
  if (!detail.value || !detail.value.live_snapshot) return
  if (action === 'approve') {
    try {
      await ElMessageBox.confirm(
        `确认批准「${scopeName.value}」的${limitItemLabel.value}${durationLabel.value}到 ${money(targetAmount.value)}？`,
        '确认批准扩容申请',
        { type: 'warning', confirmButtonText: '确认批准', cancelButtonText: '取消' },
      )
    } catch (error) {
      return
    }
  }
  working.value = true
  try {
    detail.value = await decideExpansionV2(detail.value.request.id, { action, comment: comment.value, precheck_version: detail.value.live_snapshot.precheck_version })
    emit('changed')
  } finally { working.value = false }
}
</script>

<style scoped>
.decision-hero,
.request-summary,
.effect-panel,
.approval-pool {
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
}

.decision-hero {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 16px;
  background: var(--el-color-success-light-9);
  border-color: var(--el-color-success-light-7);
}

.decision-hero.frozen { background: var(--el-color-warning-light-9); border-color: var(--el-color-warning-light-7); }
.decision-hero.blocked { background: var(--el-color-danger-light-9); border-color: var(--el-color-danger-light-7); }
.decision-hero h3,
.detail-section h3 { margin: 0; color: var(--el-text-color-primary); }
.decision-hero p,
.detail-section p { margin: 6px 0 0; color: var(--el-text-color-regular); font-size: 13px; line-height: 1.6; }
.eyebrow { color: var(--el-text-color-secondary); font-size: 12px; font-weight: 600; }

.request-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-top: 16px;
  padding: 14px 16px;
}

.summary-main { display: grid; gap: 3px; }
.summary-main span,
.summary-main small,
.section-head span { color: var(--el-text-color-secondary); font-size: 12px; }
.summary-main strong { color: var(--el-text-color-primary); font-size: 18px; }
.summary-tags { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }

.detail-section { margin-top: 20px; }
.section-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.metric-list { display: flex; flex-wrap: wrap; gap: 8px; }
.metric-card { display: grid; flex: 1 1 180px; gap: 6px; min-width: 0; padding: 14px; background: var(--el-bg-color); border: 1px solid var(--el-border-color-light); border-radius: 8px; }
.metric-card span { color: var(--el-text-color-secondary); font-size: 12px; }
.metric-card strong { color: var(--el-text-color-primary); font-size: 18px; font-variant-numeric: tabular-nums; }
.metric-card .target { color: var(--el-color-primary); }

.effect-panel { padding: 14px; background: var(--el-color-success-light-9); border-color: var(--el-color-success-light-7); }
.effect-panel.frozen { background: var(--el-color-warning-light-9); border-color: var(--el-color-warning-light-7); }
.effect-panel.blocked { background: var(--el-color-danger-light-9); border-color: var(--el-color-danger-light-7); }

.group-list { display: flex; flex-wrap: wrap; gap: 6px; }
.group-toggle { margin-top: 8px; }
.approval-pool { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; padding: 10px 12px; background: var(--el-fill-color-lighter); }
.approval-pool span { color: var(--el-text-color-secondary); font-size: 12px; }
.timeline { margin-top: 10px; }
.actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 24px; }
.actions .el-input { flex: 1 1 100%; }

@media (max-width: 640px) {
  .decision-hero,
  .request-summary,
  .section-head { flex-direction: column; align-items: flex-start; }
  .metric-card { flex-basis: calc(50% - 4px); }
}
</style>
