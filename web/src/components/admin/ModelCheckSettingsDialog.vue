<template>
  <el-dialog
    :model-value="visible"
    title="⚙️ 模型检测设置"
    width="480px"
    :close-on-click-modal="false"
    @close="emit('close')"
  >
    <el-form :model="form" label-width="120px">
      <el-form-item label="启用自动检测">
        <el-switch v-model="form.model_check_enabled" />
      </el-form-item>
      <el-form-item>
        <span style="color: var(--el-text-color-secondary); font-size: 12px">
          开启后，系统按照设定间隔自动检测所有启用的模型
        </span>
      </el-form-item>

      <el-form-item label="检测间隔">
        <el-select v-model="form.model_check_interval_minutes" style="width: 100%">
          <el-option
            v-for="opt in intervalOptions"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="失败阈值">
        <el-input-number v-model="form.model_check_failure_threshold" :min="1" :max="100" />
        <span style="margin-left: 8px; color: var(--el-text-color-secondary); font-size: 12px">次</span>
      </el-form-item>
      <el-form-item>
        <span style="color: var(--el-text-color-secondary); font-size: 12px">
          连续失败达到阈值后自动禁用该模型
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
          上次结果：成功 {{ checkResult.success }} / 失败 {{ checkResult.failed }} / 自动禁用 {{ autoDisabledCount }}
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
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
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

const form = ref({
  model_check_enabled: true,
  model_check_interval_minutes: 5,
  model_check_failure_threshold: 3,
})

const saving = ref(false)
const checking = ref(false)
const checkResult = ref<ModelCheckAllResult | null>(null)

const autoDisabledCount = computed(() => {
  if (!checkResult.value) return 0
  return checkResult.value.results.filter(r => r.auto_disabled).length
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
      } catch {
        ElMessage.error('加载检测配置失败')
      }
    }
  }
)

/** 保存设置 */
async function handleSave() {
  saving.value = true
  try {
    await updateHealthCheckConfig({
      model_check_enabled: form.value.model_check_enabled,
      model_check_interval_minutes: form.value.model_check_interval_minutes,
      model_check_failure_threshold: form.value.model_check_failure_threshold,
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
    if (result.failed > 0) {
      ElMessage.warning(`检测完成：成功 ${result.success}，失败 ${result.failed}`)
    } else {
      ElMessage.success(`检测完成：全部 ${result.success} 个模型正常`)
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
</style>
