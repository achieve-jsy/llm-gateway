<template>
  <el-dialog
    v-model="visible"
    title="申请创建 Key"
    width="560px"
    :close-on-click-modal="false"
    destroy-on-close
    @closed="resetForm"
  >
    <template v-if="!submitted">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px" label-position="left" @submit.prevent>
        <el-form-item label="Key 名称" prop="key_name">
          <el-input v-model="form.key_name" placeholder="如：my-dev-key" maxlength="128" />
          <div class="form-hint">仅允许字母、数字、下划线和连字符</div>
        </el-form-item>

        <el-form-item label="用途说明" prop="purpose">
          <el-input
            v-model="form.purpose"
            type="textarea"
            :rows="2"
            placeholder="简要说明 Key 用于什么项目或场景（帮助审批人判断合理性）"
            maxlength="500"
            show-word-limit
          />
        </el-form-item>

        <el-form-item label="过期时间" prop="expires_at">
          <el-date-picker
            v-model="form.expires_at"
            type="datetime"
            placeholder="不填则永久有效"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
            :disabled-date="disabledDate"
            style="width:100%"
          />
        </el-form-item>

        <el-form-item label="模型组">
          <el-select
            v-model="form.group_ids"
            multiple
            filterable
            placeholder="选择可用的模型组"
            style="width:100%"
            :loading="groupsLoading"
            clearable
          >
            <el-option label="全部模型（不限制）" :value="ALL_GROUPS_SENTINEL" />
            <el-option v-for="g in availableGroups" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
          <div class="form-hint">选定后 Key 只能调用这些模型组；不选则创建后自行管理</div>
        </el-form-item>

        <el-form-item label="预算申请">
          <div class="budget-toggle-row">
            <el-switch
              v-model="applyBudget"
              inline-prompt
              active-text="申请"
              inactive-text="不申请"
              @change="onApplyBudgetChange"
            />
            <span class="budget-toggle-hint">
              {{ applyBudget ? '将随 Key 申请一并提交个人月预算额度' : '本次仅申请 Key，不包含预算' }}
            </span>
          </div>
        </el-form-item>

        <el-form-item v-if="applyBudget" label="月预算额度">
          <div class="budget-input-row">
            <el-input-number
              v-model="form.budget_monthly"
              :min="1"
              :max="99999"
              :step="10"
              :precision="2"
              controls-position="right"
              style="width:200px"
            />
            <span class="budget-unit">USD / 月</span>
          </div>
          <div class="form-hint">审批通过后将自动为你创建对应个人月预算额度</div>
          <div class="budget-presets">
            <span class="preset-label">快速选择：</span>
            <el-button v-for="v in [50, 100, 200, 500]" :key="v" size="small" @click="form.budget_monthly = v">
              ${{ v }}
            </el-button>
          </div>
        </el-form-item>
      </el-form>
    </template>

    <div v-else class="submit-result">
      <el-result icon="success" title="申请已提交" sub-title="等待部门负责人审批，可在「我的申请」中查看进度">
        <template #extra>
          <div class="submit-result-summary">
            <div class="submit-result-row">
              <span class="submit-result-label">Key 名称</span>
              <span class="submit-result-value">{{ submittedKeyName }}</span>
            </div>
            <div class="submit-result-row">
              <span class="submit-result-label">预算申请</span>
              <KeyRequestBudgetTag :budget-monthly="submittedBudget" :show-icon="true" />
            </div>
          </div>
        </template>
      </el-result>
    </div>

    <template #footer>
      <div class="dialog-footer">
        <el-button v-if="!submitted" @click="visible = false">取消</el-button>
        <el-button
          v-if="!submitted"
          type="primary"
          :loading="loading"
          :disabled="!form.key_name.trim() || (applyBudget && form.budget_monthly <= 0)"
          @click="handleSubmit"
        >
          {{ submitButtonLabel }}
        </el-button>
        <el-button v-else type="primary" @click="visible = false">知道了</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createKeyRequest, getAvailableGroups } from '@/services/control-plane'
import type { AvailableGroup } from '@/types/api-key'
import KeyRequestBudgetTag from '@/components/key/KeyRequestBudgetTag.vue'

const ALL_GROUPS_SENTINEL = -1

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'submitted'): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const formRef = ref<FormInstance>()
const loading = ref(false)
const groupsLoading = ref(false)
const submitted = ref(false)
const submittedKeyName = ref('')
const submittedBudget = ref(0)
const applyBudget = ref(false)
const availableGroups = ref<AvailableGroup[]>([])
const accessAll = ref(false)

const form = ref({
  key_name: '',
  purpose: '',
  group_ids: [] as number[],
  expires_at: '',
  budget_monthly: 100,
})

const submitButtonLabel = computed(() => {
  if (applyBudget.value && form.value.budget_monthly > 0) {
    return `提交申请（含预算 ${form.value.budget_monthly % 1 === 0 ? form.value.budget_monthly : form.value.budget_monthly.toFixed(2)} USD/月）`
  }
  return '提交申请（仅 Key）'
})

watch(() => form.value.group_ids, (ids) => {
  if (ids.includes(ALL_GROUPS_SENTINEL) && ids.length > 1) {
    form.value.group_ids = [ALL_GROUPS_SENTINEL]
  }
  accessAll.value = ids.includes(ALL_GROUPS_SENTINEL)
})

const rules: FormRules = {
  key_name: [
    { required: true, message: '请输入 Key 名称', trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_-]+$/, message: '仅允许字母、数字、下划线和连字符', trigger: 'blur' },
  ],
}

function disabledDate(time: Date) { return time.getTime() < Date.now() - 86400000 }

function onApplyBudgetChange(enabled: boolean) {
  if (enabled && form.value.budget_monthly <= 0) {
    form.value.budget_monthly = 100
  }
  if (!enabled) {
    form.value.budget_monthly = 0
  }
}

async function loadGroups() {
  groupsLoading.value = true
  try { const r = await getAvailableGroups(); availableGroups.value = r.data.groups || [] } catch {} finally { groupsLoading.value = false }
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  if (applyBudget.value && form.value.budget_monthly <= 0) {
    ElMessage.warning('请填写月预算额度，或关闭「预算申请」')
    return
  }
  loading.value = true
  const budget = applyBudget.value && form.value.budget_monthly > 0 ? form.value.budget_monthly : undefined
  try {
    await createKeyRequest({
      key_name: form.value.key_name.trim(),
      purpose: form.value.purpose.trim() || undefined,
      group_ids: accessAll.value ? [] : form.value.group_ids,
      expires_at: form.value.expires_at ? new Date(form.value.expires_at).toISOString() : undefined,
      budget_monthly: budget,
    })
    submittedKeyName.value = form.value.key_name.trim()
    submittedBudget.value = budget ?? 0
    submitted.value = true
    emit('submitted')
    ElMessage.success(budget ? `申请已提交（含预算 $${budget}/月）` : '申请已提交（仅 Key）')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error?.message || e?.response?.data?.error || '提交失败')
  } finally {
    loading.value = false
  }
}

function resetForm() {
  form.value = { key_name: '', purpose: '', group_ids: [], expires_at: '', budget_monthly: 100 }
  applyBudget.value = false
  accessAll.value = false
  submitted.value = false
  submittedKeyName.value = ''
  submittedBudget.value = 0
  formRef.value?.resetFields()
}

watch(visible, (v) => { if (v) loadGroups() })
</script>

<style scoped>
.dialog-footer { display: flex; justify-content: flex-end; gap: 8px; }
.form-hint { font-size: 12px; color: #98989D; margin-top: 4px; }
.budget-toggle-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.budget-toggle-hint { font-size: 13px; color: var(--el-text-color-secondary); }
.budget-input-row { display: flex; align-items: center; gap: 8px; }
.budget-unit { color: var(--el-text-color-secondary); font-size: 13px; }
.budget-presets { display: flex; align-items: center; gap: 4px; margin-top: 6px; flex-wrap: wrap; }
.preset-label { font-size: 12px; color: var(--el-text-color-secondary); }
.submit-result { padding: 0 8px 8px; }
.submit-result-summary {
  display: flex;
  flex-direction: column;
  gap: 10px;
  text-align: left;
  min-width: 260px;
}
.submit-result-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.submit-result-label {
  flex-shrink: 0;
  width: 72px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.submit-result-value {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
@media (max-width: 768px) {
  :deep(.el-dialog) { width: 95% !important; max-width: 95vw !important; }
}
</style>
