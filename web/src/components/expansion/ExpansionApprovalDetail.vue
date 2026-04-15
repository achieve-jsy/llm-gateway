<template>
  <el-drawer v-model="visible" title="扩容审批详情" size="min(600px, 100%)">
    <template v-if="detail">
      <el-alert
        v-if="requiresRecheck"
        title="数据已变化，审批已冻结"
        description="预算或用量已不同于提交快照，请先重新预检并基于最新结果再次确认。"
        type="warning"
        :closable="false"
        show-icon
      />

      <section class="detail-section">
        <h3>提交快照与实时变化</h3>
        <el-descriptions :column="2" border class="snapshot-compare">
        <el-descriptions-item label="提交时上限">{{ detail.submitted_snapshot.current_limit_usd }}</el-descriptions-item>
        <el-descriptions-item label="实时额度">{{ detail.live_snapshot?.current_limit_usd || '不可用' }}</el-descriptions-item>
        <el-descriptions-item label="提交时已用">{{ detail.submitted_snapshot.used_usd }}</el-descriptions-item>
        <el-descriptions-item label="当前已用">{{ detail.live_snapshot?.current_used_usd || detail.submitted_snapshot.used_usd }}</el-descriptions-item>
        <el-descriptions-item label="申请目标">{{ detail.submitted_snapshot.target_amount_usd }}</el-descriptions-item>
        <el-descriptions-item label="调整类型">{{ adjustmentType }}</el-descriptions-item>
        <el-descriptions-item label="额度变化">{{ liveDelta }}</el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="detail-section effect-panel" :class="{ frozen: requiresRecheck }">
        <h3>{{ requiresRecheck ? '批准后效果（已冻结）' : '批准后效果' }}</h3>
        <p v-if="requiresRecheck">重新预检完成前不展示可能已经失效的生效额度。</p>
        <p v-else>
          当前上限将调整为
          <strong>{{ detail.live_snapshot?.effective_after_usd || detail.submitted_snapshot.target_amount_usd }}</strong>
          ，影响 {{ detail.affected_groups.length }} 个模型组。
        </p>
      </section>

      <section class="detail-section">
        <h3>申请信息与上级限制预检</h3>
        <ExpansionPrecheckPanel :value="detail.live_snapshot" />
        <h4>父级约束</h4>
        <el-empty v-if="detail.parent_constraints.length === 0" description="无父级瓶颈" :image-size="48" />
        <el-tag v-for="item in detail.parent_constraints" :key="`${item.source_level}:${item.scope_id}`" :type="item.status === 'blocking' ? 'danger' : 'info'">
          {{ item.source_level }} · {{ item.status }}<template v-if="item.limit_usd"> · {{ item.limit_usd }}</template>
        </el-tag>
        <p>审批人：{{ detail.approval_pool.join('、') || '暂无可用审批人' }}</p>
        <p>影响模型组：{{ detail.affected_groups.map(item => item.name).join('、') || '无' }}</p>
      </section>

      <section class="detail-section">
        <h3>审批流水与审批意见</h3>
        <el-timeline class="timeline">
          <el-timeline-item v-for="step in detail.steps" :key="step.id" :timestamp="new Date(step.created_at).toLocaleString()">
            {{ step.level }} · {{ step.action }}<span v-if="step.comment">：{{ step.comment }}</span>
          </el-timeline-item>
        </el-timeline>
      </section>

      <div class="actions">
        <el-button v-if="detail.capabilities.can_reprecheck" :loading="working" @click="recheck">重新预检</el-button>
        <template v-if="detail.capabilities.can_decide">
          <el-input v-model="comment" placeholder="审批意见" />
          <el-button type="danger" plain :loading="working" @click="decide('reject')">驳回</el-button>
          <el-button type="primary" :loading="working" :disabled="!detail.live_snapshot || detail.live_snapshot.stale" @click="decide('approve')">批准</el-button>
        </template>
      </div>
    </template>
  </el-drawer>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { decideExpansionV2, reprecheckExpansion } from '@/services/control-plane'
import type { ExpansionAction, ExpansionDetail } from '@/types/expansion'
import ExpansionPrecheckPanel from './ExpansionPrecheckPanel.vue'
const detail = defineModel<ExpansionDetail | null>('detail', { default: null })
const visible = defineModel<boolean>({ default: false })
const emit = defineEmits<{ changed: [] }>()
const comment = ref('')
const working = ref(false)
const requiresRecheck = computed(() => detail.value?.request.status === 'precheck_required' ||
  detail.value?.request.status === 'pending_reconfirmation' ||
  !!detail.value?.live_snapshot?.stale ||
  (!!detail.value?.live_snapshot && detail.value.live_snapshot.precheck_version !== detail.value.request.precheck_version))
const liveDelta = computed(() => {
  if (!detail.value?.live_snapshot) return '不可用'
  const submitted = Number(detail.value.submitted_snapshot.current_limit_usd)
  const live = Number(detail.value.live_snapshot.current_limit_usd)
  return Number.isFinite(submitted) && Number.isFinite(live) ? (live - submitted).toFixed(6) : '不可用'
})
const adjustmentType = computed(() => {
  if (!detail.value) return '—'
  const dimension = detail.value.request.dimension_type === 'tier'
    ? `${detail.value.request.model_tier_name || '模型等级'}额度`
    : '总预算'
  const duration = detail.value.request.duration_type === 'permanent' ? '长期基础额度' : '本周期临时扩容'
  return `${dimension} · ${duration}`
})
async function recheck() {
  if (!detail.value) return
  working.value = true
  try { detail.value = await reprecheckExpansion(detail.value.request.id) } finally { working.value = false }
}
async function decide(action: ExpansionAction) {
  if (!detail.value) return
  working.value = true
  try {
    if (!detail.value.live_snapshot) return
    detail.value = await decideExpansionV2(detail.value.request.id, { action, comment: comment.value, precheck_version: detail.value.live_snapshot.precheck_version })
    emit('changed')
  } finally { working.value = false }
}
</script>
<style scoped>
.snapshot-compare,
.timeline,
.detail-section { margin-top: 20px; }
.detail-section h3 { margin: 0 0 10px; }
.detail-section h4 { margin: 16px 0 8px; color: var(--el-text-color-regular); }
.detail-section p { color: var(--el-text-color-regular); font-size: 13px; line-height: 1.6; }
.detail-section .el-tag { margin: 0 6px 6px 0; }
.effect-panel { padding: 14px; background: var(--el-color-success-light-9); border: 1px solid var(--el-color-success-light-7); border-radius: 8px; }
.effect-panel.frozen { background: var(--budget-stale-bg, var(--el-color-warning-light-9)); border-color: var(--el-color-warning-light-7); }
.effect-panel p { margin: 0; }
.actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 24px; }
.actions .el-input { flex: 1 1 100%; }
</style>
