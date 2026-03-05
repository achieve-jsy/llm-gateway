<template>
  <el-dialog
    :model-value="visible"
    title="⚙️ 自动检测设置"
    width="480px"
    :close-on-click-modal="false"
    @close="emit('close')"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item label="启用自动检测">
        <el-switch v-model="form.enabled" />
      </el-form-item>

      <el-form-item label="检测间隔" prop="interval_minutes">
        <el-select
          v-model="form.interval_minutes"
          style="width: 100%"
          :disabled="form.interval_custom"
        >
          <el-option
            v-for="opt in intervalOptions"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="  自定义间隔">
        <el-switch
          v-model="form.interval_custom"
          active-text="自定义"
          inactive-text="预设"
          style="--el-switch-on-color: var(--el-color-warning)"
        />
        <el-input-number
          v-if="form.interval_custom"
          v-model="form.interval_minutes"
          :min="1"
          :max="1440"
          style="margin-left: 8px; width: 120px"
        />
        <span
          v-if="form.interval_custom"
          style="margin-left: 4px; color: var(--el-text-color-secondary); font-size: 12px"
        >分钟</span>
      </el-form-item>

      <el-form-item label="检测模式" prop="check_mode">
        <el-select v-model="form.check_mode" style="width: 100%">
          <el-option
            v-for="opt in checkModeOptions"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="失败阈值" prop="max_failures">
        <el-input-number v-model="form.max_failures" :min="1" :max="100" />
        <span style="margin-left: 8px; color: var(--el-text-color-secondary); font-size: 12px">次</span>
      </el-form-item>
      <el-form-item>
        <span style="color: var(--el-text-color-secondary); font-size: 12px">
          连续失败达到阈值后自动禁用渠道
        </span>
      </el-form-item>

      <el-divider style="margin: 16px 0" />

      <el-form-item label=" ">
        <el-button plain :loading="checking" @click="handleCheckNow">
          ▶ 立即检测一次
        </el-button>
      </el-form-item>

      <el-form-item v-if="checkResult" label=" ">
        <span class="check-result-text">
          上次检测结果：成功 {{ checkResult.success }} / 失败 {{ checkResult.failed }} / 自动禁用 {{ checkResult.auto_disabled }}
        </span>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="saving" @click="handleSave">保存设置</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import {
  getHealthCheckConfig,
  updateHealthCheckConfig,
  checkAllProviders,
} from '@/services/control-plane'
import type { CheckAllResult } from '@/services/control-plane'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const intervalOptions = [
  { label: '每 1 分钟', value: 1 },
  { label: '每 3 分钟', value: 3 },
  { label: '每 5 分钟', value: 5 },
  { label: '每 10 分钟', value: 10 },
  { label: '每 30 分钟', value: 30 },
  { label: '每 60 分钟', value: 60 },
]

const checkModeOptions = [
  { label: '仅连通性（不消耗 Token）', value: 'connectivity' },
  { label: '模型转发（消耗极少量 Token）', value: 'model_forward' },
  { label: '两者都做', value: 'both' },
]

const formRef = ref<FormInstance>()

const rules = reactive<FormRules>({
  interval_minutes: [{ required: true, message: '请选择检测间隔', trigger: 'change' }],
  check_mode: [{ required: true, message: '请选择检测模式', trigger: 'change' }],
  max_failures: [{ required: true, message: '请设置失败阈值', trigger: 'blur' }],
})

const form = ref({
  enabled: true,
  interval_minutes: 5,
  interval_custom: false,
  check_mode: 'connectivity',
  max_failures: 3,
})

const saving = ref(false)
const checking = ref(false)
const checkResult = ref<CheckAllResult | null>(null)

/** 弹窗打开时加载配置 */
watch(
  () => props.visible,
  async (val) => {
    if (val) {
      checkResult.value = null
      try {
        const config = await getHealthCheckConfig()
        form.value.enabled = config.enabled
        form.value.interval_minutes = config.interval_minutes
        form.value.interval_custom = !intervalOptions.some(o => o.value === form.value.interval_minutes)
        form.value.max_failures = config.max_failures
        // 从 check_providers/check_models 映射到 check_mode
        if (config.check_providers && config.check_models) {
          form.value.check_mode = 'both'
        } else if (config.check_models) {
          form.value.check_mode = 'model_forward'
        } else {
          form.value.check_mode = 'connectivity'
        }
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
    // 从 check_mode 映射回后端字段
    const check_providers = form.value.check_mode === 'connectivity' || form.value.check_mode === 'both'
    const check_models = form.value.check_mode === 'model_forward' || form.value.check_mode === 'both'
    await updateHealthCheckConfig({
      enabled: form.value.enabled,
      interval_minutes: form.value.interval_minutes,
      max_failures: form.value.max_failures,
      check_providers,
      check_models,
    } as any)
    ElMessage.success('检测设置已保存')
    emit('saved')
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

/** 立即检测一次 */
async function handleCheckNow() {
  checking.value = true
  try {
    const result = await checkAllProviders()
    checkResult.value = result
    if (result.failed > 0) {
      ElMessage.warning(`检测完成：成功 ${result.success}，失败 ${result.failed}`)
    } else {
      ElMessage.success(`检测完成：全部 ${result.success} 个渠道正常`)
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