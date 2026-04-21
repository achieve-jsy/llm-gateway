<template>
  <el-dialog v-model="visible" title="批量配置等级额度" width="min(760px, 94vw)">
    <el-steps :active="step" finish-status="success"><el-step title="填写额度" /><el-step title="影响预览" /><el-step title="确认生效" /></el-steps>
    <div class="wizard-body">
      <el-form v-if="step === 0" label-position="top">
        <el-form-item label="调整类型">
          <el-radio-group v-model="form.mode"><el-radio-button value="total_fixed">总预算固定值</el-radio-button><el-radio-button value="total_percent">总预算百分比</el-radio-button><el-radio-button value="tier">等级额度</el-radio-button></el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.mode === 'tier'" label="调整等级" required>
          <el-select v-model="form.modelTierId"><el-option v-for="tier in tiers" :key="tier.id" :label="tier.name" :value="tier.id" /></el-select>
        </el-form-item>
        <el-form-item v-if="form.mode === 'tier'" label="等级调整模式">
          <el-radio-group v-model="form.operation"><el-radio-button value="set_fixed">固定额度</el-radio-button><el-radio-button value="adjust_percent">百分比</el-radio-button></el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.mode === 'total_percent' || form.operation === 'adjust_percent'" label="方向">
          <el-radio-group v-model="form.direction"><el-radio value="increase">上调</el-radio><el-radio value="decrease">下调</el-radio></el-radio-group>
        </el-form-item>
        <el-form-item v-if="isPercentMode" label="百分比（%）">
          <div style="display:flex;align-items:center;gap:12px;width:100%">
            <el-slider v-model="percentSlider" :min="0" :max="100" :step="1" style="flex:1" />
            <el-input-number v-model="percentSlider" :min="0" :max="100" :step="1" :controls="false" style="width:80px" />
            <span>%</span>
          </div>
        </el-form-item>
        <el-form-item v-else label="额度（USD）"><el-input v-model="form.value" inputmode="decimal" /></el-form-item>
        <el-form-item label="周期"><el-select v-model="form.periodType"><el-option label="日" value="daily" /><el-option label="周" value="weekly" /><el-option label="月" value="monthly" /></el-select></el-form-item>
        <el-form-item label="生效方式"><el-radio-group v-model="form.effectiveMode"><el-radio value="immediate">立即</el-radio><el-radio value="next_period">下周期</el-radio></el-radio-group></el-form-item>
        <el-form-item label="原因" required><el-input v-model="form.reason" type="textarea" maxlength="200" /></el-form-item>
      </el-form>
      <template v-else>
        <DataFreshnessNotice :stale="previewStale" :updated-at="previewUpdatedAt" />
        <el-table :data="previewRows">
          <el-table-column prop="display_label" label="对象" />
          <el-table-column prop="old_limit_usd" label="原额度" />
          <el-table-column prop="new_limit_usd" label="新额度" />
          <el-table-column prop="current_used_usd" label="已用" />
        </el-table>
      </template>
    </div>
    <template #footer>
      <el-button v-if="step > 0" @click="step--">上一步</el-button>
      <el-button v-if="step === 0" type="primary" :loading="working" :disabled="!canPreview" @click="runPreview">生成预览</el-button>
      <el-button v-else-if="step === 1" type="primary" :disabled="previewStale" @click="step = 2">下一步</el-button>
      <el-button v-else type="primary" :loading="working" :disabled="previewRows.length === 0 || previewStale" @click="apply">确认应用</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import DataFreshnessNotice from '@/components/common/DataFreshnessNotice.vue'
import { applyBudgetTierBatch, applyTotalBudgetBatch, previewBudgetTierBatch, previewTotalBudgetBatch } from '@/services/control-plane'
import type { BudgetAdjustmentImpact, BudgetItem } from '@/types/budget'
import type { ModelTier, TierAdjustmentPreview } from '@/types/model-tier'
import {
  batchEntryModeToWizardMode,
  type BudgetBatchEntryMode,
} from '@/composables/budgetScopeFilters'

const props = withDefaults(defineProps<{
  items: BudgetItem[]
  tiers: ModelTier[]
  /** 打开向导时预填的调整类型（对齐原型三键入口） */
  initialMode?: BudgetBatchEntryMode
}>(), {
  initialMode: 'absolute',
})
const emit = defineEmits<{ applied: [] }>()
const visible = defineModel<boolean>({ default: false })
const step = ref(0)
const working = ref(false)
const preview = ref<TierAdjustmentPreview | null>(null)
const totalPreviews = ref<Array<{ item: BudgetItem; impact: BudgetAdjustmentImpact; target: string }>>([])
const form = reactive({
  mode: 'total_fixed' as 'total_fixed' | 'total_percent' | 'tier',
  modelTierId: '', operation: 'set_fixed' as 'set_fixed' | 'adjust_percent',
  direction: 'increase' as 'increase' | 'decrease', value: '', periodType: 'monthly' as 'daily' | 'weekly' | 'monthly',
  effectiveMode: 'immediate' as 'immediate' | 'next_period', reason: '',
})
const isPercentMode = computed(() => form.mode === 'total_percent' || (form.mode === 'tier' && form.operation === 'adjust_percent'))
const percentSlider = ref(0)
watch(percentSlider, (v) => { form.value = String(v) })
const canPreview = computed(() => props.items.length > 0 && (form.mode !== 'tier' || form.modelTierId) && /^\d+(\.\d{1,6})?$/.test(form.value) && form.reason.trim())
const previewRows = computed(() => {
  if (form.mode === 'tier') {
    return (preview.value?.items || []).map(item => ({
      ...item,
      display_label: resolveLabel(item.item_id),
    }))
  }
  return totalPreviews.value.map(row => ({
    item_id: budgetBatchItemId(row.item),
    display_label: row.item.label || row.item.scope_id,
    old_limit_usd: row.impact.before_limit_usd,
    new_limit_usd: row.impact.after_limit_usd,
    current_used_usd: row.impact.current_used_usd,
  }))
})
function resolveLabel(itemId: string): string {
  const allocationId = itemId.includes(':') ? itemId.split(':')[0] : itemId
  const item = props.items.find(i => i.allocation_id === allocationId)
  return item?.label || item?.scope_id || allocationId
}
function budgetBatchItemId(item: BudgetItem): string {
  return item.allocation_id || `${item.scope_type}:${item.scope_id}`
}
const previewStale = computed(() => form.mode === 'tier' ? !!preview.value?.stale : totalPreviews.value.some(row => row.impact.stale))
const previewUpdatedAt = computed(() => form.mode === 'tier' ? preview.value?.data_as_of : totalPreviews.value[0]?.impact.data_as_of)
watch(visible, (value) => {
  if (!value) return
  step.value = 0
  preview.value = null
  totalPreviews.value = []
  form.mode = batchEntryModeToWizardMode(props.initialMode)
  form.operation = 'set_fixed'
  form.direction = 'increase'
  form.value = ''
  percentSlider.value = 0
  form.reason = ''
})
async function runPreview() {
  working.value = true
  try {
    if (form.mode !== 'tier') {
      const response = await previewTotalBudgetBatch({
        operation: form.mode === 'total_fixed' ? 'set_fixed' : 'adjust_percent',
        direction: form.direction, value: Number(form.value).toFixed(6), period_type: form.periodType,
        effective_mode: form.effectiveMode, reason: form.reason.trim(),
        items: props.items.map(item => ({ item_id: budgetBatchItemId(item), scope_type: item.scope_type, scope_id: item.scope_id })),
      })
      totalPreviews.value = response.items.map(result => ({
        item: props.items.find(item => budgetBatchItemId(item) === result.item_id)!,
        impact: result.impact, target: result.target_limit_usd,
      }))
    } else preview.value = await previewBudgetTierBatch({
      operation: form.operation, effective_mode: form.effectiveMode, reason: form.reason.trim(),
      items: props.items.filter(item => item.allocation_id).map(item => ({
        item_id: item.allocation_id!, allocation_id: item.allocation_id!,
        model_tier_id: form.modelTierId, period_type: form.periodType,
        direction: form.operation === 'adjust_percent' ? form.direction : undefined,
        value: Number(form.value).toFixed(6),
      })),
    })
    step.value = 1
  } finally { working.value = false }
}
async function apply() {
  if (form.mode !== 'tier') {
    if (previewStale.value) return
    if (totalPreviews.value.some(row => row.impact.zero_block)) await ElMessageBox.confirm('$0 将阻断请求，确认继续？', '确认 $0 风险', { type: 'warning' })
    if (totalPreviews.value.some(row => row.impact.below_used)) await ElMessageBox.confirm('部分额度低于已用量，确认继续？', '确认低于已用风险', { type: 'warning' })
    if (totalPreviews.value.some(row => row.impact.replaces_pending_schedule)) await ElMessageBox.confirm('将替换已有下周期计划，确认继续？', '确认替换计划', { type: 'warning' })
    working.value = true
    try {
      await applyTotalBudgetBatch({
        operation: form.mode === 'total_fixed' ? 'set_fixed' : 'adjust_percent',
        direction: form.direction, value: Number(form.value).toFixed(6), period_type: form.periodType,
        effective_mode: form.effectiveMode, reason: form.reason.trim(),
        confirm_zero_block: totalPreviews.value.some(row => row.impact.zero_block),
        confirm_below_used: totalPreviews.value.some(row => row.impact.below_used),
        confirm_replace_schedule: totalPreviews.value.some(row => row.impact.replaces_pending_schedule),
        items: totalPreviews.value.map(row => ({
          item_id: budgetBatchItemId(row.item), scope_type: row.item.scope_type, scope_id: row.item.scope_id,
          preview_version: row.impact.preview_version,
        })),
      })
      ElMessage.success('批量总预算调整已提交'); visible.value = false; emit('applied')
    } finally { working.value = false }
    return
  }
  if (!preview.value || preview.value.stale) return
  const valid = preview.value.items.filter(item => item.status === 'valid')
  if (valid.some(item => item.zero_block)) await ElMessageBox.confirm('$0 将阻断请求，确认继续？', '确认 $0 风险', { type: 'warning' })
  if (valid.some(item => item.below_used)) await ElMessageBox.confirm('部分额度低于已用量，确认继续？', '确认低于已用风险', { type: 'warning' })
  working.value = true
  try {
    await applyBudgetTierBatch({ preview_token: preview.value.preview_token, confirm_item_ids: valid.map(item => item.item_id) })
    ElMessage.success('批量调整已提交')
    visible.value = false
    emit('applied')
  } finally { working.value = false }
}

defineExpose({ form, runPreview })
</script>

<style scoped>.wizard-body { min-height: 240px; padding: 24px 0; }</style>
