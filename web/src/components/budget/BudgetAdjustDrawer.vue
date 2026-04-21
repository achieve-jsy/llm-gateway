<template>
  <el-drawer v-model="visible" title="调整等级额度" size="min(520px, 100%)">
    <el-form label-position="top">
      <el-form-item label="周期"><el-select v-model="form.period_type"><el-option label="日" value="daily" /><el-option label="周" value="weekly" /><el-option label="月" value="monthly" /></el-select></el-form-item>
      <el-form-item label="最终额度（USD）" required><el-input v-model="form.target_limit_usd" inputmode="decimal" /></el-form-item>
      <el-form-item label="生效方式" required><el-radio-group v-model="form.effective_mode"><el-radio-button value="immediate">立即</el-radio-button><el-radio-button value="next_period">下周期</el-radio-button></el-radio-group></el-form-item>
      <el-form-item label="变更原因" required><el-input v-model="form.reason" type="textarea" maxlength="200" show-word-limit /></el-form-item>
      <el-alert v-if="Number(form.target_limit_usd) === 0" title="$0 会禁止该作用域使用此等级" type="warning" :closable="false" show-icon />
      <el-checkbox v-if="Number(form.target_limit_usd) === 0" v-model="confirmZero">我确认后续请求将被阻断</el-checkbox>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="!canSave" @click="save">保存</el-button>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { applyBudgetTierBatch, previewBudgetTierBatch } from '@/services/control-plane'
import type { BudgetTierLimit } from '@/types/model-tier'

const props = defineProps<{ allocationId: string; modelTierId: string; initial?: BudgetTierLimit | null }>()
const emit = defineEmits<{ saved: [] }>()
const visible = defineModel<boolean>({ default: false })
const saving = ref(false)
const confirmZero = ref(false)
const form = reactive({
  period_type: props.initial?.period_type || 'monthly',
  target_limit_usd: props.initial?.limit_usd || '',
  effective_mode: 'immediate' as 'immediate' | 'next_period',
  reason: '',
})
const canSave = computed(() => form.reason.trim() && /^-?\d+(\.\d{1,6})?$/.test(form.target_limit_usd) && (Number(form.target_limit_usd) !== 0 || confirmZero.value))
async function save() {
  saving.value = true
  try {
    const itemId = `${props.allocationId}:${props.modelTierId}:${form.period_type}`
    const preview = await previewBudgetTierBatch({
      operation: 'set_fixed',
      effective_mode: form.effective_mode,
      items: [{
        item_id: itemId, allocation_id: props.allocationId, model_tier_id: props.modelTierId,
        period_type: form.period_type as 'daily' | 'weekly' | 'monthly',
        value: Number(form.target_limit_usd).toFixed(6),
      }],
      reason: form.reason.trim(),
    })
    if (preview.stale || preview.items.some(item => item.stale)) {
      ElMessage.warning('用量数据已过期，请刷新后重新预览')
      return
    }
    const item = preview.items[0]
    if (!item || item.status !== 'valid') throw new Error(item?.error_code || '预算预览失败')
    if (item.zero_block) {
      await ElMessageBox.confirm('$0 将阻断该等级后续请求，是否继续？', '确认 $0 风险', { type: 'warning' })
    }
    if (item.below_used) {
      await ElMessageBox.confirm('新额度低于当前已用量，是否继续？', '确认低于已用风险', { type: 'warning' })
    }
    await applyBudgetTierBatch({ preview_token: preview.preview_token, confirm_item_ids: [itemId] })
    visible.value = false
    ElMessage.success(form.effective_mode === 'immediate' ? '额度已更新' : '已安排下周期生效')
    emit('saved')
  } finally { saving.value = false }
}
</script>
