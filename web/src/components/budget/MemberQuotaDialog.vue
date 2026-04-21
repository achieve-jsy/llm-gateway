<template>
  <el-dialog
    :model-value="modelValue"
    title="调整成员配额"
    width="440px"
    :close-on-click-modal="false"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <template v-if="member">
      <!-- 成员信息 & 当前用量 -->
      <div class="mq-scope">
        <div class="mq-scope-name">{{ member.label || member.scope_id }}</div>
        <div class="mq-scope-meta">成员配额 · {{ member.scope_id }}</div>
        <template v-if="(member.amount_usd || 0) > 0">
          <div class="mq-usage-row">
            <span>本月已用 {{ formatUSD(member.used_usd || 0, { compact: true }) }}</span>
            <span class="mq-usage-sep">/</span>
            <span>{{ formatUSD(member.amount_usd, { compact: true }) }}</span>
            <el-tag :type="statusType(member.used_ratio || 0)" size="small" style="margin-left: 6px">
              {{ statusText(member.used_ratio || 0) }}
            </el-tag>
          </div>
          <el-progress
            :percentage="Math.min(Math.round((member.used_ratio || 0) * 100), 100)"
            :stroke-width="5"
            :color="progressColor(member.used_ratio || 0)"
            :show-text="false"
            style="margin-top: 6px"
          />
        </template>
      </div>

      <!-- 编辑表单（有权限时） -->
      <template v-if="canEdit">
        <el-form label-position="top" style="margin-top: 16px">
          <el-form-item label="月额度（USD）">
            <div v-if="form.amountUnlimited" class="mq-unlimited">
              <strong>不限</strong>
              <el-button size="small" @click="form.amountUnlimited = false">设置上限</el-button>
            </div>
            <template v-else>
              <el-input-number
                v-model="form.amount"
                :min="0"
                :precision="2"
                controls-position="right"
                style="width: 100%"
              />
              <el-button link type="primary" style="margin-top:4px" @click="form.amountUnlimited = true">设为不限</el-button>
            </template>
          </el-form-item>
          <el-form-item label="周额度（USD）">
            <div v-if="form.weeklyUnlimited" class="mq-unlimited">
              <strong>不限</strong>
              <el-button size="small" @click="form.weeklyUnlimited = false">设置上限</el-button>
            </div>
            <template v-else>
              <el-input-number
                v-model="form.weekly"
                :min="0"
                :precision="2"
                controls-position="right"
                style="width: 100%"
              />
              <el-button link type="primary" style="margin-top:4px" @click="form.weeklyUnlimited = true">设为不限</el-button>
            </template>
          </el-form-item>
          <el-form-item label="日额度（USD）">
            <div v-if="form.dailyUnlimited" class="mq-unlimited">
              <strong>不限</strong>
              <el-button size="small" @click="form.dailyUnlimited = false">设置上限</el-button>
            </div>
            <template v-else>
              <el-input-number
                v-model="form.daily"
                :min="0"
                :precision="2"
                controls-position="right"
                style="width: 100%"
              />
              <el-button link type="primary" style="margin-top:4px" @click="form.dailyUnlimited = true">设为不限</el-button>
            </template>
          </el-form-item>
          <el-form-item label="生效方式">
            <el-radio-group v-model="effectiveMode">
              <el-radio value="immediate">立即生效</el-radio>
              <el-radio
                value="next_period"
                :disabled="form.amountUnlimited || form.weeklyUnlimited || form.dailyUnlimited"
              >
                下周期生效
              </el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="调整原因" required>
            <el-input v-model="reason" type="textarea" :rows="2" maxlength="200" />
          </el-form-item>
        </el-form>
        <div class="mq-hint">保存后预算配置立即生效；已有告警去重会随改额重置。</div>
      </template>

      <!-- 无权限时提示 -->
      <div v-else class="mq-no-edit">
        <p>您没有直接调整该成员配额的权限，可申请扩容。</p>
      </div>
    </template>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button v-if="canEdit" type="primary" :loading="saving" @click="save">保存</el-button>
      <el-button v-else type="primary" @click="goExpansion">申请扩容</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { adjustBudget, previewBudgetAdjustment } from '@/services/control-plane'
import type { BudgetEffectiveMode, BudgetItem } from '@/types/budget'
import { formatUSD } from '@/utils/format'

const props = defineProps<{
  modelValue: boolean
  member: BudgetItem | null
  canEdit?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'refresh': []
}>()

const router = useRouter()
const saving = ref(false)
const form = ref({
  amount: 0,
  weekly: 0,
  daily: 0,
  amountUnlimited: false,
  weeklyUnlimited: true,
  dailyUnlimited: true,
})
const reason = ref('')
const effectiveMode = ref<BudgetEffectiveMode>('immediate')

function toPeriodState(value: number | undefined | null) {
  const n = Number(value)
  if (!Number.isFinite(n) || n < 0) return { unlimited: true, value: 0 }
  return { unlimited: false, value: n }
}

watch(
  () => props.member,
  (m) => {
    if (m) {
      reason.value = ''
      effectiveMode.value = 'immediate'
      const monthly = toPeriodState(m.amount_usd)
      const weekly = toPeriodState(m.weekly_amount_usd)
      const daily = toPeriodState(m.daily_amount_usd)
      form.value = {
        amount: monthly.value,
        weekly: weekly.value,
        daily: daily.value,
        amountUnlimited: monthly.unlimited,
        weeklyUnlimited: weekly.unlimited,
        dailyUnlimited: daily.unlimited,
      }
    }
  },
  { immediate: true },
)

function progressColor(ratio: number) {
  if (ratio >= 1) return '#f56c6c'
  if (ratio >= 0.8) return '#e6a23c'
  return '#36B34E'
}

function statusType(ratio: number) {
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return 'success'
}

function statusText(ratio: number) {
  if (ratio >= 1) return '已超额'
  if (ratio >= 0.8) return '预警'
  return '正常'
}

async function save() {
  if (!props.member) return
  if (!reason.value.trim()) { ElMessage.warning('请填写调整原因'); return }
  const hasUnlimited = form.value.amountUnlimited || form.value.weeklyUnlimited || form.value.dailyUnlimited
  if (hasUnlimited && effectiveMode.value === 'next_period') {
    ElMessage.warning('含「不限」时仅支持立即生效')
    return
  }
  const targets = [
    { period_type: 'monthly' as const, value: form.value.amountUnlimited ? -1 : form.value.amount },
    { period_type: 'weekly' as const, value: form.value.weeklyUnlimited ? -1 : form.value.weekly },
    { period_type: 'daily' as const, value: form.value.dailyUnlimited ? -1 : form.value.daily },
  ]
  if (targets.some(item => !(item.value === -1 || (Number.isFinite(item.value) && item.value >= 0)))) {
    ElMessage.warning('额度必须 ≥ 0，或不限')
    return
  }
  saving.value = true
  try {
    const previews = await Promise.all(targets.map(item => previewBudgetAdjustment({
      scope_type: 'member',
      scope_id: props.member!.scope_id,
      period_type: item.period_type,
      target_limit_usd: Number(item.value).toFixed(6),
      effective_mode: effectiveMode.value,
      reason: reason.value.trim(),
    })))
    if (previews.some(item => item.stale)) {
      ElMessage.warning('用量数据已过期，请刷新后重新预览')
      return
    }
    if (previews.some(item => item.zero_block)) {
      await ElMessageBox.confirm('$0 将阻断后续请求，是否继续？', '确认 $0 风险', { type: 'warning' })
    }
    if (previews.some(item => item.below_used)) {
      await ElMessageBox.confirm('新额度低于当前已用量，是否继续？', '确认低于已用风险', { type: 'warning' })
    }
    if (previews.some(item => item.replaces_pending_schedule)) {
      await ElMessageBox.confirm('本次调整将替换已有排期，是否继续？', '确认替换排期', { type: 'warning' })
    }
    if (!previews.some(item => item.zero_block || item.below_used || item.replaces_pending_schedule)) {
      await ElMessageBox.confirm('预览完成，确认应用预算调整？', '确认预算调整', { type: 'info' })
    }
    await Promise.all(targets.map((item, index) => adjustBudget({
      scope_type: 'member',
      scope_id: props.member!.scope_id,
      period_type: item.period_type,
      target_limit_usd: Number(item.value).toFixed(6),
      effective_mode: effectiveMode.value,
      reason: reason.value.trim(),
      confirm_zero_block: previews[index].zero_block,
      confirm_below_used: previews[index].below_used,
      confirm_replace_schedule: previews[index].replaces_pending_schedule,
      preview_version: previews[index].preview_version,
    })))
    ElMessage.success('成员配额已更新')
    emit('update:modelValue', false)
    emit('refresh')
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

function goExpansion() {
  if (!props.member) return
  router.push({
    path: '/admin/expansion-requests',
    query: {
      scope_type: props.member.scope_type,
      scope_id: props.member.scope_id,
      requested_amount: Math.max(
        Math.ceil((props.member.amount_usd || 0) * 1.2),
        (props.member.amount_usd || 0) + 100,
      ),
    },
  })
  emit('update:modelValue', false)
}
</script>

<style scoped>
.mq-scope {
  padding: 14px 16px;
  background: var(--el-fill-color-light);
  border-radius: 10px;
}
.mq-scope-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.mq-scope-meta {
  margin-top: 3px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.mq-usage-row {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.mq-usage-sep {
  color: var(--el-text-color-placeholder);
}
.mq-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-light);
  padding: 10px 12px;
  border-radius: 8px;
  line-height: 1.6;
  margin-top: 8px;
}
.mq-no-edit {
  padding: 16px;
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
</style>
