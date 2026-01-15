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
            <el-input-number
              v-model="form.amount"
              :min="0"
              :precision="2"
              controls-position="right"
              style="width: 100%"
            />
          </el-form-item>
          <el-form-item label="周额度（USD，-1 表示不限）">
            <el-input-number
              v-model="form.weekly"
              :min="-1"
              :precision="2"
              controls-position="right"
              style="width: 100%"
            />
          </el-form-item>
          <el-form-item label="日额度（USD，-1 表示不限）">
            <el-input-number
              v-model="form.daily"
              :min="-1"
              :precision="2"
              controls-position="right"
              style="width: 100%"
            />
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
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import { setBudgetBatch } from '@/services/control-plane'
import type { BudgetItem } from '@/types/budget'
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
const form = ref({ amount: 0, weekly: -1 as number, daily: -1 as number })

watch(
  () => props.member,
  (m) => {
    if (m) {
      form.value = {
        amount: m.amount_usd >= 0 ? m.amount_usd : 0,
        weekly: m.weekly_amount_usd,
        daily: m.daily_amount_usd,
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
  saving.value = true
  try {
    await setBudgetBatch([{
      scope_type: 'member',
      scope_id: props.member.scope_id,
      amount_usd: form.value.amount,
      weekly_amount_usd: form.value.weekly,
      daily_amount_usd: form.value.daily,
    }])
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
