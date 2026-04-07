<template>
  <el-drawer v-model="visible" title="扩容审批详情" size="min(600px, 100%)">
    <template v-if="detail">
      <el-alert v-if="requiresRecheck" title="数据已变化，审批已冻结，请先重新预检并再次确认。" type="warning" :closable="false" show-icon />
      <el-descriptions title="提交快照与实时变化" :column="2" border class="snapshot-compare">
        <el-descriptions-item label="提交时额度">{{ detail.submitted_snapshot.current_limit_usd }}</el-descriptions-item>
        <el-descriptions-item label="实时额度">{{ detail.live_snapshot?.current_limit_usd || '不可用' }}</el-descriptions-item>
        <el-descriptions-item label="提交时已用">{{ detail.submitted_snapshot.used_usd }}</el-descriptions-item>
        <el-descriptions-item label="实时已用">{{ detail.live_snapshot?.current_used_usd || '不可用' }}</el-descriptions-item>
        <el-descriptions-item label="申请目标">{{ detail.submitted_snapshot.target_amount_usd }}</el-descriptions-item>
        <el-descriptions-item label="额度变化">{{ liveDelta }}</el-descriptions-item>
      </el-descriptions>
      <ExpansionPrecheckPanel :value="detail.live_snapshot" />
      <section class="detail-section">
        <h3>父级约束</h3>
        <el-empty v-if="detail.parent_constraints.length === 0" description="无父级瓶颈" :image-size="48" />
        <el-tag v-for="item in detail.parent_constraints" :key="`${item.source_level}:${item.scope_id}`" :type="item.status === 'blocking' ? 'danger' : 'info'">
          {{ item.source_level }} · {{ item.status }}<template v-if="item.limit_usd"> · {{ item.limit_usd }}</template>
        </el-tag>
      </section>
      <section class="detail-section">
        <h3>审批池与影响范围</h3>
        <p>审批人：{{ detail.approval_pool.join('、') || '暂无可用审批人' }}</p>
        <p>影响模型组：{{ detail.affected_groups.map(item => item.name).join('、') || '无' }}</p>
      </section>
      <el-timeline class="timeline">
        <el-timeline-item v-for="step in detail.steps" :key="step.id" :timestamp="new Date(step.created_at).toLocaleString()">
          {{ step.level }} · {{ step.action }}<span v-if="step.comment">：{{ step.comment }}</span>
        </el-timeline-item>
      </el-timeline>
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
<style scoped>.snapshot-compare,.timeline,.detail-section { margin-top: 20px; } .detail-section h3 { margin: 0 0 10px; } .detail-section .el-tag { margin: 0 6px 6px 0; } .actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 24px; } .actions .el-input { flex: 1 1 100%; }</style>
