<template>
  <el-dialog
    :model-value="visible"
    title="编辑用户配额"
    width="500px"
    :close-on-click-modal="false"
    destroy-on-close
    @update:model-value="$emit('update:visible', $event)"
  >
    <div v-if="user" class="quota-edit-body">
      <el-alert
        :title="`用户：${user.name}（${user.email}）`"
        type="info"
        show-icon
        :closable="false"
        style="margin-bottom: 16px"
      />
      <el-form label-width="160px" label-position="top">
        <el-form-item label="每分钟请求数 (RPM)">
          <el-input-number
            v-model="form.rpm"
            :min="0"
            :max="10000"
            :step="10"
            controls-position="right"
            style="width: 100%"
          />
          <span class="quota-hint">0 = 不限制，留空 = 继承默认 ({{ effective?.default_rpm ?? 60 }})</span>
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
          <span class="quota-hint">0 = 不限制，留空 = 继承默认 ({{ effective?.default_max_concurrency ?? 10 }})</span>
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
              :type="effective.source === 'override' ? 'success' : 'info'"
              style="margin-left: 6px"
            >
              {{ effective.source === 'override' ? '自定义' : '未设置' }}
            </el-tag>
          </div>
          <div class="quota-effective-item">
            <span class="label">并发：</span>
            <span class="value">{{ effective.max_concurrency === 0 ? '不限制' : effective.max_concurrency }}</span>
          </div>
        </div>
      </div>
      <div class="quota-defaults-hint">
        用户默认 60 RPM / 10 并发
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
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getUserQuota, setUserQuota } from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'

interface UserInfo {
  id: string
  name: string
  email: string
}

const props = defineProps<{
  visible: boolean
  user: UserInfo | null
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  'saved': []
}>()

const form = ref<{ rpm: number | null; max_concurrency: number | null }>({
  rpm: null,
  max_concurrency: null,
})
const saving = ref(false)
const effective = ref<QuotaEffective | null>(null)

watch(
  () => props.visible,
  async (v) => {
    if (v && props.user) {
      form.value = { rpm: null, max_concurrency: null }
      effective.value = null
      try {
        const res = await getUserQuota(props.user.id)
        const data = (res as any)?.data
        if (data) {
          effective.value = data
          if (data.source === 'override') {
            form.value.rpm = data.rpm
            form.value.max_concurrency = data.max_concurrency
          }
        }
      } catch { /* ignore */ }
    }
  },
)

async function saveQuota() {
  if (!props.user) return
  saving.value = true
  try {
    await setUserQuota(props.user.id, {
      rpm: form.value.rpm,
      max_concurrency: form.value.max_concurrency,
    })
    ElMessage.success('配额已保存')
    // reload effective
    const res = await getUserQuota(props.user.id)
    effective.value = (res as any)?.data || null
    if (effective.value?.source === 'override') {
      form.value.rpm = effective.value.rpm
      form.value.max_concurrency = effective.value.max_concurrency
    }
    emit('saved')
  } catch (e: any) {
    ElMessage.error(e?.message || '配额保存失败')
  } finally {
    saving.value = false
  }
}

async function clearQuota() {
  if (!props.user) return
  saving.value = true
  try {
    await setUserQuota(props.user.id, { rpm: null, max_concurrency: null })
    form.value = { rpm: null, max_concurrency: null }
    ElMessage.success('已清除，恢复为继承默认')
    const res = await getUserQuota(props.user.id)
    effective.value = (res as any)?.data || null
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
