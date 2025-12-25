<template>
  <el-dialog
    :model-value="visible"
    title="⚙️ 模型检测设置"
    width="480px"
    :close-on-click-modal="false"
    @close="emit('close')"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
      <el-form-item label="启用自动检测">
        <el-switch v-model="form.model_check_enabled" />
      </el-form-item>
      <el-form-item>
        <span style="color: var(--el-text-color-secondary); font-size: 12px">
          开启后，系统按照设定间隔自动检测所有启用的模型
        </span>
      </el-form-item>

      <el-form-item label="检测间隔" prop="model_check_interval_minutes">
        <el-select v-model="form.model_check_interval_minutes" style="width: 100%">
          <el-option
            v-for="opt in intervalOptions"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="失败阈值" prop="model_check_failure_threshold">
        <el-input-number v-model="form.model_check_failure_threshold" :min="1" :max="100" />
        <span style="margin-left: 8px; color: var(--el-text-color-secondary); font-size: 12px">次</span>
      </el-form-item>
      <el-form-item>
        <span style="color: var(--el-text-color-secondary); font-size: 12px">
          连续失败达到阈值后自动禁用该模型
        </span>
      </el-form-item>

      <el-divider style="margin: 16px 0"><span style="font-size: 12px; color: var(--el-text-color-secondary)">模型恢复</span></el-divider>

      <el-form-item label="启用自动恢复">
        <el-switch v-model="form.model_recovery_enabled" />
      </el-form-item>
      <el-form-item label="恢复阈值(次)" prop="model_recovery_success_threshold">
        <el-input-number
          v-model="form.model_recovery_success_threshold"
          :min="1"
          :max="10"
          :disabled="!form.model_recovery_enabled"
        />
      </el-form-item>
      <el-form-item>
        <span style="color: var(--el-text-color-secondary); font-size: 12px">
          被自动禁用的模型会继续接受健康检测，连续成功达到阈值后自动恢复。手动禁用的模型不会自动恢复。
        </span>
      </el-form-item>

      <el-divider style="margin: 16px 0" />

      <el-form-item label=" ">
        <el-button plain :loading="checking" @click="handleCheckNow">
          ▶ 立即检测所有模型
        </el-button>
      </el-form-item>

      <el-form-item v-if="checkResult" label=" ">
        <span class="check-result-text">
          共 {{ checkResult.total }} 个 &nbsp;|&nbsp;
          成功 {{ checkResult.success }}
          <template v-if="autoReenabledCount > 0">（含 {{ autoReenabledCount }} 个自动恢复）</template>
          &nbsp;/&nbsp;失败 {{ checkResult.failed }}
          <template v-if="autoDisabledCount > 0">（含 {{ autoDisabledCount }} 个自动禁用）</template>
        </span>
      </el-form-item>

      <el-form-item v-if="failureSummaryText" label="失败分类">
        <span class="check-result-text">{{ failureSummaryText }}</span>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="saving" @click="handleSave">保存设置</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import {
  getHealthCheckConfig,
  updateHealthCheckConfig,
  checkAllModels,
} from '@/services/control-plane'
import type { ModelCheckAllResult } from '@/services/control-plane'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const intervalOptions = [
  { label: '每 5 分钟', value: 5 },
  { label: '每 10 分钟', value: 10 },
  { label: '每 30 分钟', value: 30 },
  { label: '每 60 分钟', value: 60 },
]

const formRef = ref<FormInstance>()

const rules = reactive<FormRules>({
  model_check_interval_minutes: [{ required: true, message: '请选择检测间隔', trigger: 'change' }],
  model_check_failure_threshold: [{ required: true, message: '请设置失败阈值', trigger: 'blur' }],
  model_recovery_success_threshold: [{ required: true, message: '请设置恢复阈值', trigger: 'blur' }],
})

const form = ref({
  model_check_enabled: true,
  model_check_interval_minutes: 5,
  model_check_failure_threshold: 3,
  model_recovery_enabled: false,
  model_recovery_success_threshold: 3,
})

const saving = ref(false)
const checking = ref(false)
const checkResult = ref<ModelCheckAllResult | null>(null)

const autoDisabledCount = computed(() => {
  if (!checkResult.value) return 0
  return checkResult.value.results.filter(r => r.auto_disabled).length
})

const autoReenabledCount = computed(() => {
  if (!checkResult.value) return 0
  return checkResult.value.results.filter(r => r.auto_reenabled).length
})

const failureSummaryText = computed(() => {
  const summary = checkResult.value?.failure_summary
  if (!summary) return ''
  const labelMap: Record<string, string> = {
    invalid_api_key: 'Key无效',
    access_denied: '无权限',
    model_not_found: '模型不存在',
    rate_limited: '限流',
    product_not_activated: '未开通',
    timeout: '超时',
    other: '其他',
  }
  const parts = Object.entries(summary)
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1])
    .map(([key, count]) => `${labelMap[key] || key} ${count}`)
  return parts.join(' / ')
})

/** 弹窗打开时加载配置 */
watch(
  () => props.visible,
  async (val) => {
    if (val) {
      checkResult.value = null
      try {
        const config = await getHealthCheckConfig()
        form.value.model_check_enabled = config.model_check_enabled ?? true
        form.value.model_check_interval_minutes = config.model_check_interval_minutes || 5
        form.value.model_check_failure_threshold = config.model_check_failure_threshold || 3
        form.value.model_recovery_enabled = config.model_recovery_enabled ?? false
        form.value.model_recovery_success_threshold = config.model_recovery_success_threshold || 3
      } catch {
        ElMessage.error('加载检测配置失败')
      }
    }
  }
)

/** 保存设置 */
async function handleSave() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    await updateHealthCheckConfig({
      model_check_enabled: form.value.model_check_enabled,
      model_check_interval_minutes: form.value.model_check_interval_minutes,
      model_check_failure_threshold: form.value.model_check_failure_threshold,
      model_recovery_enabled: form.value.model_recovery_enabled,
      model_recovery_success_threshold: form.value.model_recovery_success_threshold,
    })
    ElMessage.success('模型检测设置已保存')
    emit('saved')
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

/** 立即检测所有模型 */
async function handleCheckNow() {
  checking.value = true
  try {
    const result = await checkAllModels()
    checkResult.value = result
    const reenabledCount = result.results.filter(r => r.auto_reenabled).length
    const disabledCount = result.results.filter(r => r.auto_disabled).length
    if (result.failed > 0) {
      ElMessage.warning(`检测完成：成功 ${result.success}，失败 ${result.failed}${disabledCount > 0 ? `（${disabledCount} 个已自动禁用）` : ''}`)
    } else {
      ElMessage.success(`检测完成：全部 ${result.success} 个模型正常${reenabledCount > 0 ? `，${reenabledCount} 个已自动恢复` : ''}`)
    }
    // 有模型状态变更（自动恢复/禁用），通知父页面刷新列表
    if (reenabledCount > 0 || disabledCount > 0) {
      emit('saved')
    }
  } catch {
    ElMessage.error('检测请求异常')
  } finally {
    checking.value = false
  }
}
</script>

<style scoped>
.check-result-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95% !important;
    max-width: 95vw !important;
  }
}
</style>
