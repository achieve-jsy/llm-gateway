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
          <span class="quota-hint">0 = 不限制，留空 = 继承默认 ({{ effective?.default_rpm ?? defaultRpm }})</span>
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
          <span class="quota-hint">0 = 不限制，留空 = 继承默认 ({{ effective?.default_max_concurrency ?? defaultConcurrency }})</span>
        </el-form-item>
      </el-form>
      <div v-if="effective" class="quota-effective">
        <div class="quota-effective-title">当前生效配额</div>
        <div class="quota-effective-grid">
          <div class="quota-effective-item">
            <span class="label">RPM：</span>
            <span class="value">{{ effective.rpm === 0 ? '不限制' : effective.rpm }}</span>
            <el-tag
              size="small"
              :type="sourceTagInfo.type"
              style="margin-left: 6px"
            >
              {{ sourceTagInfo.label }}
            </el-tag>
          </div>
          <div class="quota-effective-item">
            <span class="label">并发：</span>
            <span class="value">{{ effective.max_concurrency === 0 ? '不限制' : effective.max_concurrency }}</span>
          </div>
        </div>
      </div>
      <div class="quota-defaults-hint">
        默认 {{ defaultRpm }} RPM / {{ defaultConcurrency }} 并发
      </div>
    </div>
    <template #footer>
      <el-button @click="$emit('update:visible', false)">取消</el-button>
      <el-popconfirm title="清除配额设置？将恢复为继承默认值" @confirm="clearQuota">
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

const props = withDefaults(defineProps<Props>(), {
  defaultRpm: QUOTA_DEFAULTS.user.rpm,
  defaultConcurrency: QUOTA_DEFAULTS.user.max_concurrency,
})

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

const sourceTagInfo = computed(() => {
  const source = effective.value?.source
  switch (source) {
    case 'override': return { label: '自定义', type: 'success' as const }
    case 'default_user': return { label: '用户默认', type: 'info' as const }
    case 'default_service_key': return { label: 'SK默认', type: 'info' as const }
    case 'unset': return { label: '未设置', type: 'info' as const }
    default: return { label: '未设置', type: 'info' as const }
  }
})

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
          effective.value = data
          if (data.source === 'override') {
            form.value.rpm = data.rpm
            form.value.max_concurrency = data.max_concurrency
          }
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
      effective.value = data
      if (data.source === 'override') {
        form.value.rpm = data.rpm
        form.value.max_concurrency = data.max_concurrency
      }
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
    ElMessage.success('已清除，恢复为继承默认')
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
