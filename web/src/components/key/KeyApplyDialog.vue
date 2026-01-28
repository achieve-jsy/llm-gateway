<template>
  <el-dialog
    v-model="visible"
    title="申请创建 Key"
    width="560px"
    :close-on-click-modal="false"
    destroy-on-close
    @closed="resetForm"
  >
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

      <el-divider>同时申请个人预算（可选）</el-divider>

      <el-form-item label="月预算额度">
        <div class="budget-input-row">
          <el-input-number
            v-model="form.budget_monthly"
            :min="0"
            :max="99999"
            :step="10"
            :precision="2"
            controls-position="right"
            style="width:200px"
            placeholder="0 表示不申请"
          />
          <span class="budget-unit">USD / 月</span>
        </div>
        <div class="form-hint">
          留 0 表示本次不申请预算；审批通过后系统会自动创建对应个人月预算额度。
        </div>
        <div class="budget-presets">
          <span class="preset-label">快速选择：</span>
          <el-button v-for="v in [50, 100, 200, 500]" :key="v" size="small" @click="form.budget_monthly = v">
            ${{ v }}
          </el-button>
        </div>
      </el-form-item>
    </el-form>

    <el-alert
      v-if="submitted"
      title="申请已提交，等待部门负责人审批。"
      type="success"
      :closable="false"
      show-icon
    >
      可在「我的 Key」页面「我的申请」Tab 中查看进度。
    </el-alert>

    <template #footer>
      <div class="dialog-footer">
        <el-button v-if="!submitted" @click="visible = false">取消</el-button>
        <el-button v-if="!submitted" type="primary" :loading="loading" :disabled="!form.key_name.trim()" @click="handleSubmit">
          提交申请
        </el-button>
        <el-button v-else type="primary" @click="visible = false">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createKeyRequest, getAvailableGroups, toggleAccessAllGroups } from '@/services/control-plane'
import type { AvailableGroup } from '@/types/api-key'

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
const availableGroups = ref<AvailableGroup[]>([])
const accessAll = ref(false)

const form = ref({
  key_name: '',
  purpose: '',
  group_ids: [] as number[],
  expires_at: '',
  budget_monthly: 0,
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

async function loadGroups() {
  groupsLoading.value = true
  try { const r = await getAvailableGroups(); availableGroups.value = r.data.groups || [] } catch {} finally { groupsLoading.value = false }
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return; loading.value = true
  try {
    await createKeyRequest({
      key_name: form.value.key_name.trim(),
      purpose: form.value.purpose.trim() || undefined,
      group_ids: accessAll.value ? [] : form.value.group_ids,
      expires_at: form.value.expires_at ? new Date(form.value.expires_at).toISOString() : undefined,
      budget_monthly: form.value.budget_monthly > 0 ? form.value.budget_monthly : undefined,
    })
    submitted.value = true; emit('submitted'); ElMessage.success('申请已提交')
  } catch (e: any) { ElMessage.error(e?.response?.data?.error?.message || e?.response?.data?.error || '提交失败') }
  finally { loading.value = false }
}

function resetForm() { form.value = { key_name: '', purpose: '', group_ids: [], expires_at: '', budget_monthly: 0 }; accessAll.value = false; submitted.value = false; formRef.value?.resetFields() }

watch(visible, (v) => { if (v) loadGroups() })
</script>

<style scoped>
.dialog-footer { display: flex; justify-content: flex-end; gap: 8px; }
.form-hint { font-size: 12px; color: #98989D; margin-top: 4px; }
.budget-input-row { display: flex; align-items: center; gap: 8px; }
.budget-unit { color: var(--el-text-color-secondary); font-size: 13px; }
.budget-presets { display: flex; align-items: center; gap: 4px; margin-top: 6px; }
.preset-label { font-size: 12px; color: var(--el-text-color-secondary); }
@media (max-width: 768px) {
  :deep(.el-dialog) { width: 95% !important; max-width: 95vw !important; }
}
</style>
