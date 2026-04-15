<template>
  <el-dialog v-model="visible" title="新建扩容申请" width="min(620px, 94vw)" :close-on-click-modal="false">
    <el-form label-position="top">
      <div class="form-grid">
        <el-form-item label="对象类型" required><el-select v-model="form.scope_type" :disabled="ordinaryUser"><el-option v-for="item in scopeOptions" :key="item" :label="SCOPE_TYPE_LABELS[item]" :value="item" /></el-select></el-form-item>
        <el-form-item label="对象 ID" required><el-input v-model="form.scope_id" :disabled="ordinaryUser" /></el-form-item>
        <el-form-item label="预算维度"><el-radio-group v-model="form.dimension_type"><el-radio-button value="total">总预算</el-radio-button><el-radio-button value="tier">模型等级</el-radio-button></el-radio-group></el-form-item>
        <el-form-item v-if="form.dimension_type === 'tier'" label="模型等级" required><el-select v-model="form.model_tier_id"><el-option v-for="tier in tiers" :key="tier.id" :label="tier.name" :value="tier.id" /></el-select></el-form-item>
        <el-form-item label="周期"><el-select v-model="form.period_type"><el-option label="日" value="daily" /><el-option label="周" value="weekly" /><el-option label="月" value="monthly" /></el-select></el-form-item>
        <el-form-item label="持续时间"><el-radio-group v-model="form.duration_type"><el-radio value="permanent">长期</el-radio><el-radio value="current_period">仅当前周期</el-radio></el-radio-group></el-form-item>
        <el-form-item label="目标额度（USD）" required><el-input v-model="form.target_amount_usd" inputmode="decimal" /></el-form-item>
        <el-form-item label="优先级"><el-radio-group v-model="form.priority"><el-radio value="normal">普通</el-radio><el-radio value="urgent">紧急</el-radio></el-radio-group></el-form-item>
      </div>
      <el-form-item label="申请原因" required><el-input v-model="form.reason" type="textarea" maxlength="300" show-word-limit /></el-form-item>
      <el-form-item v-if="form.priority === 'urgent'" label="紧急原因" required><el-input v-model="form.urgent_reason" type="textarea" /></el-form-item>
    </el-form>
    <ExpansionPrecheckPanel :value="precheck" />
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button v-if="precheck?.can_notify_owner && !precheck.can_submit" :loading="submitting" @click="notify">通知负责人</el-button>
      <el-button type="primary" :loading="submitting || checking" :disabled="!canSubmit" @click="submit">提交申请</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { createExpansionRequest, listModelTiers, notifyBudgetOwner, precheckExpansion } from '@/services/control-plane'
import { SCOPE_TYPE_LABELS, type BudgetScopeType } from '@/types/budget'
import type { ExpansionDimension, ExpansionPeriod, ExpansionPrecheck } from '@/types/expansion'
import type { ModelTier } from '@/types/model-tier'
import ExpansionPrecheckPanel from './ExpansionPrecheckPanel.vue'

const props = withDefaults(defineProps<{
  ordinaryUser?: boolean
  initialScopeType?: BudgetScopeType
  initialScopeId?: string
  initialDimensionType?: ExpansionDimension
  initialModelTierId?: string
  initialPeriodType?: ExpansionPeriod
  initialTargetAmountUsd?: string
}>(), {
  ordinaryUser: false,
  initialScopeType: 'member',
  initialScopeId: '',
  initialDimensionType: 'total',
  initialModelTierId: '',
  initialPeriodType: 'monthly',
  initialTargetAmountUsd: '',
})
const visible = defineModel<boolean>({ default: false })
const emit = defineEmits<{ created: [] }>()
const scopeOptions: BudgetScopeType[] = ['company', 'studio', 'dept_daily', 'dept_service', 'member']
const tiers = ref<ModelTier[]>([])
const precheck = ref<ExpansionPrecheck | null>(null)
const checking = ref(false)
const submitting = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0
const form = reactive({
  scope_type: props.initialScopeType, scope_id: props.initialScopeId, period_type: 'monthly' as 'daily' | 'weekly' | 'monthly',
  dimension_type: 'total' as 'total' | 'tier', model_tier_id: '', duration_type: 'permanent' as 'permanent' | 'current_period',
  priority: 'normal' as 'normal' | 'urgent', target_amount_usd: '', reason: '', urgent_reason: '',
})
const valid = computed(() => form.scope_id && /^\d+(\.\d{1,6})?$/.test(form.target_amount_usd) && form.reason.trim() && (form.priority !== 'urgent' || form.urgent_reason.trim()) && (form.dimension_type !== 'tier' || form.model_tier_id))
const canSubmit = computed(() => !!valid.value && !!precheck.value?.can_submit && !precheck.value.stale)
async function check() {
  if (!form.scope_id || !/^\d+(\.\d{1,6})?$/.test(form.target_amount_usd)) { precheck.value = null; return }
  const current = ++generation
  checking.value = true
  try {
    const result = await precheckExpansion({
      scope_type: form.scope_type, scope_id: form.scope_id, period_type: form.period_type,
      dimension_type: form.dimension_type, model_tier_id: form.dimension_type === 'tier' ? form.model_tier_id : null,
      duration_type: form.duration_type, priority: form.priority, target_amount_usd: Number(form.target_amount_usd).toFixed(6),
    })
    if (current === generation) precheck.value = result
  } finally { if (current === generation) checking.value = false }
}
watch(() => [form.scope_type, form.scope_id, form.period_type, form.dimension_type, form.model_tier_id, form.duration_type, form.priority, form.target_amount_usd], () => {
  clearTimeout(timer)
  timer = setTimeout(check, 350)
})
watch(visible, async value => {
  if (!value) return
  generation++
  clearTimeout(timer)
  Object.assign(form, {
    scope_type: props.initialScopeType,
    scope_id: props.initialScopeId,
    dimension_type: props.initialDimensionType,
    model_tier_id: props.initialModelTierId,
    period_type: props.initialPeriodType,
    duration_type: 'permanent',
    priority: 'normal',
    target_amount_usd: props.initialTargetAmountUsd,
    reason: '',
    urgent_reason: '',
  })
  precheck.value = null
  if (!tiers.value.length) tiers.value = await listModelTiers()
})
async function submit() {
  if (!canSubmit.value || !precheck.value) return
  submitting.value = true
  try {
    await createExpansionRequest({
      ...form, requested_amount: Number(form.target_amount_usd), target_amount_usd: Number(form.target_amount_usd).toFixed(6),
      model_tier_id: form.dimension_type === 'tier' ? form.model_tier_id : null,
    })
    visible.value = false
    ElMessage.success('扩容申请已提交')
    emit('created')
  } finally { submitting.value = false }
}
async function notify() {
  submitting.value = true
  try {
    await notifyBudgetOwner({
      scope_type: form.scope_type, scope_id: form.scope_id, period_type: form.period_type,
      dimension_type: form.dimension_type, model_tier_id: form.dimension_type === 'tier' ? form.model_tier_id : null,
      message: form.reason,
    })
    ElMessage.success('已通知上级预算负责人')
  } finally { submitting.value = false }
}
onBeforeUnmount(() => clearTimeout(timer))
</script>
<style scoped>.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 16px; } .el-select { width: 100%; } @media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } }</style>
