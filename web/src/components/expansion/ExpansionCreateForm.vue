<template>
  <el-dialog v-model="visible" title="新建扩容申请" width="min(620px, 94vw)" :close-on-click-modal="false">
    <el-form label-position="top">
      <div class="form-grid">
        <el-form-item v-if="hideScopeFields" label="申请对象">
          <div class="scope-summary">{{ scopeSummaryText }}</div>
        </el-form-item>
        <template v-else>
          <el-form-item label="对象类型" required><el-select v-model="form.scope_type"><el-option v-for="item in scopeOptions" :key="item" :label="SCOPE_TYPE_LABELS[item]" :value="item" /></el-select></el-form-item>
          <el-form-item label="对象 ID" required><el-input v-model="form.scope_id" /></el-form-item>
        </template>
        <el-form-item v-if="showManagerPoolSelector" label="预算池">
          <el-select v-model="form.scope_type">
            <el-option label="成员日常预算池" value="dept_daily" />
            <el-option label="程序服务预算池" value="dept_service" />
          </el-select>
        </el-form-item>
        <el-form-item label="预算维度"><el-radio-group v-model="form.dimension_type"><el-radio-button value="total">总预算</el-radio-button><el-radio-button value="tier">模型等级</el-radio-button></el-radio-group></el-form-item>
        <el-form-item v-if="form.dimension_type === 'tier'" label="模型等级" required><el-select v-model="form.model_tier_id"><el-option v-for="tier in tiers" :key="tier.id" :label="tier.name" :value="tier.id" /></el-select></el-form-item>
        <el-form-item label="周期"><el-select v-model="form.period_type"><el-option label="日" value="daily" /><el-option label="周" value="weekly" /><el-option label="月" value="monthly" /></el-select></el-form-item>
        <el-form-item label="调整类型">
          <el-radio-group v-model="form.duration_type">
            <el-radio-button value="current_period">临时扩容</el-radio-button>
            <el-radio-button value="permanent">长期调额</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="目标额度（USD）" required><el-input v-model="form.target_amount_usd" inputmode="decimal" /></el-form-item>
        <el-form-item label="优先级"><el-radio-group v-model="form.priority"><el-radio value="normal">普通</el-radio><el-radio value="urgent">紧急</el-radio></el-radio-group></el-form-item>
      </div>
      <el-form-item label="申请原因" required><el-input v-model="form.reason" type="textarea" maxlength="300" show-word-limit /></el-form-item>
      <el-form-item v-if="form.priority === 'urgent'" label="紧急原因" required><el-input v-model="form.urgent_reason" type="textarea" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="submit">提交申请</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { createExpansionRequest, getMyBudgetTiers, listModelTiers } from '@/services/control-plane'
import { SCOPE_TYPE_LABELS, type BudgetScopeType } from '@/types/budget'
import type { ExpansionDimension, ExpansionPeriod } from '@/types/expansion'
import type { ModelTier } from '@/types/model-tier'

interface TierOption {
  id: string
  name: string
}

const props = withDefaults(defineProps<{
  ordinaryUser?: boolean
  initialScopeType?: BudgetScopeType
  initialScopeId?: string
  initialDimensionType?: ExpansionDimension
  initialModelTierId?: string
  initialPeriodType?: ExpansionPeriod
  initialTargetAmountUsd?: string
  fixedManagerScope?: boolean
  managerScopeLabel?: string
}>(), {
  ordinaryUser: false,
  initialScopeType: 'member',
  initialScopeId: '',
  initialDimensionType: 'total',
  initialModelTierId: '',
  initialPeriodType: 'monthly',
  initialTargetAmountUsd: '',
  fixedManagerScope: false,
  managerScopeLabel: '',
})
const visible = defineModel<boolean>({ default: false })
const emit = defineEmits<{
  created: []
  duplicate: [payload: { scope_type: BudgetScopeType; scope_id: string; dimension_type: ExpansionDimension; model_tier_id: string | null; duration_type: 'permanent' | 'current_period' }]
}>()
const scopeOptions: BudgetScopeType[] = ['company', 'studio', 'dept_daily', 'dept_service', 'member']
const tiers = ref<TierOption[]>([])
const submitting = ref(false)
const form = reactive({
  scope_type: props.initialScopeType, scope_id: props.initialScopeId, period_type: 'monthly' as 'daily' | 'weekly' | 'monthly',
  dimension_type: 'total' as 'total' | 'tier', model_tier_id: '', duration_type: 'permanent' as 'permanent' | 'current_period',
  priority: 'normal' as 'normal' | 'urgent', target_amount_usd: '', reason: '', urgent_reason: '',
})
const valid = computed(() => form.scope_id && /^\d+(\.\d{1,6})?$/.test(form.target_amount_usd) && form.reason.trim() && (form.priority !== 'urgent' || form.urgent_reason.trim()) && (form.dimension_type !== 'tier' || form.model_tier_id))
const canSubmit = computed(() => !!valid.value)
const hideScopeFields = computed(() => props.ordinaryUser || props.fixedManagerScope)
const showManagerPoolSelector = computed(() => props.fixedManagerScope && (props.initialScopeType === 'dept_daily' || props.initialScopeType === 'dept_service'))
const scopeSummaryText = computed(() => props.fixedManagerScope
  ? (props.managerScopeLabel || '我的部门')
  : '我的成员额度')
watch(visible, async value => {
  if (!value) return
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
  if (!tiers.value.length) tiers.value = await loadTierOptions(props.ordinaryUser)
  if (form.dimension_type === 'tier' && form.model_tier_id && !tiers.value.some(tier => tier.id === form.model_tier_id)) {
    form.model_tier_id = ''
  }
})

async function loadTierOptions(ordinaryUser: boolean): Promise<TierOption[]> {
  if (ordinaryUser) {
    const budget = await getMyBudgetTiers()
    return budget.tier_limits
      .filter(tier => tier.enabled && tier.periods.some(period => period.status !== 'tier_disabled'))
      .map(tier => ({ id: tier.model_tier_id, name: tier.model_tier_name }))
  }
  const items: ModelTier[] = await listModelTiers()
  return items.filter(tier => tier.enabled).map(tier => ({ id: tier.id, name: tier.name }))
}
async function submit() {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    await createExpansionRequest({
      ...form, requested_amount: Number(form.target_amount_usd), target_amount_usd: Number(form.target_amount_usd).toFixed(6),
      model_tier_id: form.dimension_type === 'tier' ? form.model_tier_id : null,
    })
    visible.value = false
    ElMessage.success('扩容申请已提交')
    emit('created')
  } catch (error: any) {
    const code = error?.response?.data?.error?.code || error?.response?.data?.code
    const message = error?.response?.data?.error?.message || error?.response?.data?.message
    if (error?.response?.status === 409 || code === 'DUPLICATE_PENDING_EXPANSION') {
      ElMessage.warning('已有审批中的同类申请，请等待审批结果或取消后重新提交')
      visible.value = false
      emit('duplicate', {
        scope_type: form.scope_type,
        scope_id: form.scope_id,
        dimension_type: form.dimension_type,
        model_tier_id: form.dimension_type === 'tier' ? form.model_tier_id : null,
        duration_type: form.duration_type,
      })
    } else {
      ElMessage.error(message || error?.message || '提交失败')
    }
  } finally { submitting.value = false }
}
</script>
<style scoped>.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 16px; } .el-select { width: 100%; } .scope-summary { width: 100%; min-height: 32px; display: inline-flex; align-items: center; padding: 0 12px; border: 1px solid var(--el-border-color); border-radius: 6px; color: var(--el-text-color-primary); background: var(--el-fill-color-light); } @media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } }</style>
