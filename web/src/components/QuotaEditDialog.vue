<template>
  <el-dialog
    :model-value="visible"
    :title="dialogTitle"
    width="500px"
    :close-on-click-modal="false"
    destroy-on-close
    @update:model-value="$emit('update:visible', $event)"
  >
    <div class="quota-edit-body">
      <el-alert
        :title="subjectLabel"
        type="info"
        show-icon
        :closable="false"
        style="margin-bottom: 16px"
      />
      <el-form label-position="top">
        <el-form-item label="每分钟请求数 (RPM)">
          <el-input-number
            v-model="form.rpm"
            :min="0"
            :max="10000"
            :step="10"
            controls-position="right"
            style="width: 100%"
          />
          <span class="quota-hint">
            0 = 不限制，<template v-if="inheritsSystemDefault">留空 = 继承系统默认 ({{ displayDefaultRpm }})</template>
            <template v-else>留空 = 不额外限制</template>
          </span>
        </el-form-item>
        <el-form-item label="最大并发数">
          <el-input-number
            v-model="form.max_concurrency"
            :min="0"
            :max="1000"
            :step="1"
            controls-position="right"
            style="width: 100%"
          />
          <span class="quota-hint">
            0 = 不限制，<template v-if="inheritsSystemDefault">留空 = 继承系统默认 ({{ displayDefaultConcurrency }})</template>
            <template v-else>留空 = 不额外限制</template>
          </span>
        </el-form-item>
      </el-form>
      <div v-if="effective" class="quota-effective">
        <div class="quota-effective-title">当前生效配额</div>
        <div class="quota-effective-grid">
          <div class="quota-effective-item">
            <span class="label">RPM：</span>
            <span class="value">{{ formatEffectiveValue(effective.rpm) }}</span>
            <el-tag
              size="small"
              :type="sourceTagInfo(effective.rpm_source).type"
              style="margin-left: 6px"
            >
              {{ sourceTagInfo(effective.rpm_source).label }}
            </el-tag>
          </div>
          <div class="quota-effective-item">
            <span class="label">并发：</span>
            <span class="value">{{ formatEffectiveValue(effective.max_concurrency) }}</span>
            <el-tag
              size="small"
              :type="sourceTagInfo(effective.max_concurrency_source).type"
              style="margin-left: 6px"
            >
              {{ sourceTagInfo(effective.max_concurrency_source).label }}
            </el-tag>
          </div>
        </div>
      </div>
      <div class="quota-defaults-hint">
        <template v-if="inheritsSystemDefault">
          系统默认 {{ displayDefaultRpm }} RPM / {{ displayDefaultConcurrency }} 并发
        </template>
        <template v-else>
          {{ subjectType === 'group' ? '模型组' : '模型' }}未设置配额时不额外限制，仍受用户或 Service Key 配额约束
        </template>
      </div>
    </div>
    <template #footer>
      <el-button @click="$emit('update:visible', false)">取消</el-button>
      <el-popconfirm :title="clearConfirmTitle" @confirm="clearQuota">
        <template #reference>
          <el-button>清除设置</el-button>
        </template>
      </el-popconfirm>
      <el-button type="primary" :loading="saving" @click="saveQuota">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import {
  getUserQuota, setUserQuota,
  getServiceKeyQuota, setServiceKeyQuota,
  getModelQuota, setModelQuota,
  getGroupQuota, setGroupQuota,
  QUOTA_DEFAULTS,
} from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'

interface Props {
  visible: boolean
  subjectType: 'user' | 'service_key' | 'model' | 'group'
  subjectId: string
  subjectLabel: string
  defaultRpm?: number
  defaultConcurrency?: number
}

const props = defineProps<Props>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  'saved': []
}>()

function getQuotaApi(subjectType: string) {
  switch (subjectType) {
    case 'user': return { get: getUserQuota, set: setUserQuota }
    case 'service_key': return { get: getServiceKeyQuota, set: setServiceKeyQuota }
    case 'model': return { get: getModelQuota, set: setModelQuota }
    case 'group': return { get: getGroupQuota, set: setGroupQuota }
    default: return { get: getUserQuota, set: setUserQuota }
  }
}

const form = ref<{ rpm: number | null; max_concurrency: number | null }>({
  rpm: null,
  max_concurrency: null,
})
const saving = ref(false)
const effective = ref<QuotaEffective | null>(null)

const inheritsSystemDefault = computed(
  () => props.subjectType === 'user' || props.subjectType === 'service_key',
)

const subjectDefaults = computed(() => QUOTA_DEFAULTS[props.subjectType])
const displayDefaultRpm = computed(
  () => effective.value?.default_rpm ?? props.defaultRpm ?? subjectDefaults.value.rpm,
)
const displayDefaultConcurrency = computed(
  () => effective.value?.default_max_concurrency
    ?? props.defaultConcurrency
    ?? subjectDefaults.value.max_concurrency,
)

const clearConfirmTitle = computed(() => inheritsSystemDefault.value
  ? '清除配额设置？清除后将继承系统默认'
  : '清除配额设置？清除后将不再单独限制')

function formatEffectiveValue(value: number) {
  if (!inheritsSystemDefault.value && effective.value?.source === 'unset') {
    return '未单独限制'
  }
  return value === 0 ? '不限制' : value
}

// 后端响应为 { code:0, data:{...} }，axios 再包一层 → 真实对象在 res.data.data
function unwrap<T = any>(res: any): T | null {
  return (res?.data?.data ?? res?.data ?? null) as T | null
}

const dialogTitle = computed(() => {
  switch (props.subjectType) {
    case 'user': return '编辑用户配额'
    case 'service_key': return '编辑 Service Key 配额'
    case 'model': return '编辑模型配额'
    case 'group': return '编辑模型组配额'
    default: return '编辑配额'
  }
})

function sourceTagInfo(source: QuotaEffective['source']) {
  switch (source) {
    case 'override': return { label: '自定义', type: 'success' as const }
    case 'default_user': return { label: '用户默认', type: 'info' as const }
    case 'default_service_key': return { label: 'SK默认', type: 'info' as const }
    case 'unset': return {
      label: inheritsSystemDefault.value ? '继承系统默认' : '未单独限制',
      type: 'info' as const,
    }
    default: return { label: '未设置', type: 'info' as const }
  }
}

function applyEffective(data: QuotaEffective) {
  effective.value = data
  form.value = {
    rpm: data.configured_rpm,
    max_concurrency: data.configured_max_concurrency,
  }
}

watch(
  () => props.visible,
  async (v) => {
    if (v && props.subjectId) {
      form.value = { rpm: null, max_concurrency: null }
      effective.value = null
      try {
        const api = getQuotaApi(props.subjectType)
        const res = await api.get(props.subjectId)
        const data = unwrap<QuotaEffective>(res)
        if (data) {
          applyEffective(data)
        }
      } catch (e) {
        console.warn('load quota failed:', e)
        ElMessage.warning('加载配额失败，请稍后重试')
      }
    }
  },
)

async function saveQuota() {
  if (!props.subjectId) return
  // Client-side validation
  if (form.value.rpm !== null && form.value.rpm < 0) {
    ElMessage.error('RPM 不能小于 0')
    return
  }
  if (form.value.max_concurrency !== null && form.value.max_concurrency < 0) {
    ElMessage.error('最大并发数不能小于 0')
    return
  }
  saving.value = true
  try {
    const api = getQuotaApi(props.subjectType)
    await api.set(props.subjectId, {
      rpm: form.value.rpm,
      max_concurrency: form.value.max_concurrency,
    })
    ElMessage.success('配额已保存')
    // Reload effective
    const res = await api.get(props.subjectId)
    const data = unwrap<QuotaEffective>(res)
    if (data) {
      applyEffective(data)
    }
    emit('saved')
  } catch (e: any) {
    ElMessage.error(e?.message || '配额保存失败')
  } finally {
    saving.value = false
  }
}

async function clearQuota() {
  if (!props.subjectId) return
  saving.value = true
  try {
    const api = getQuotaApi(props.subjectType)
    await api.set(props.subjectId, { rpm: null, max_concurrency: null })
    form.value = { rpm: null, max_concurrency: null }
    ElMessage.success(inheritsSystemDefault.value
      ? '已清除，恢复为继承系统默认'
      : '已清除，不再单独限制')
    const res = await api.get(props.subjectId)
    effective.value = unwrap<QuotaEffective>(res) || null
    emit('saved')
  } catch (e: any) {
    ElMessage.error(e?.message || '清除失败')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.quota-edit-body { padding: 0 4px; }
.quota-hint { display: block; font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }
.quota-effective { margin-top: 16px; padding: 12px; background: var(--el-fill-color-light); border-radius: 8px; }
.quota-effective-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; color: var(--el-text-color-primary); }
.quota-effective-grid { display: flex; gap: 24px; }
.quota-effective-item { font-size: 13px; }
.quota-effective-item .label { color: var(--el-text-color-secondary); }
.quota-effective-item .value { font-weight: 600; color: var(--el-text-color-primary); }
.quota-defaults-hint { margin-top: 12px; font-size: 12px; color: var(--el-text-color-placeholder); text-align: center; }
</style>
