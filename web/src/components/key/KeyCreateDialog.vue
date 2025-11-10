<template>
  <el-dialog
    v-model="visible"
    title="创建 API Key"
    width="520px"
    :close-on-click-modal="false"
    destroy-on-close
    @closed="resetForm"
  >
    <el-form ref="formRef" :model="form" label-width="80px" label-position="left" @submit.prevent>
      <el-form-item label="名称" required>
        <el-input v-model="form.name" placeholder="输入 Key 名称，便于识别" maxlength="64" />
        <div class="form-hint">仅允许字母、数字、下划线和连字符</div>
      </el-form-item>
      <el-form-item label="过期时间">
        <el-date-picker
          v-model="form.expiresAt"
          type="datetime"
          placeholder="不填则永久有效"
          value-format="YYYY-MM-DDTHH:mm:ssZ"
          :disabled-date="disabledDate"
          style="width:100%"
        />
        <div class="form-hint">不设置则 Key 永久有效</div>
      </el-form-item>
    </el-form>

    <el-alert
      v-if="createdKey"
      title="Key 创建成功！请复制并保管好，关闭后将无法再次查看。"
      type="success"
      :closable="false"
      show-icon
    >
      <template #default>
        <div class="key-display">
          <code>{{ createdKey }}</code>
          <el-button size="small" @click="copyKey">复制</el-button>
        </div>
        <div class="ccs-import-row">
          <span class="ccs-tip">一键导入到 CCSwitch 桌面客户端：</span>
          <el-button size="small" type="primary" @click="importToCcs">导入 CCSwitch</el-button>
        </div>
      </template>
    </el-alert>

    <template #footer>
      <div class="dialog-footer">
        <el-button v-if="!createdKey" @click="visible = false">取消</el-button>
        <el-button v-if="!createdKey" type="primary" :loading="loading" :disabled="!form.name.trim()" @click="handleCreate">确认创建</el-button>
        <el-button v-else type="primary" @click="visible = false">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { createKey } from '@/services/control-plane'
import { mapCreateKeyResponse } from '@/types/api-key'
import { executeCcsImport } from '@/utils/ccs-import'
import { useKeyCacheStore } from '@/stores/key-cache'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void; (e: 'success', keyId: string, rawKey: string): void }>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const loading = ref(false)
const createdKey = ref('')
const form = ref({ name: '', expiresAt: '' })

function disabledDate(time: Date) {
  return time.getTime() < Date.now() - 86400000
}

async function handleCreate() {
  if (!form.value.name.trim()) return
  loading.value = true
  try {
    const expiresAt = form.value.expiresAt ? new Date(form.value.expiresAt).toISOString() : undefined
    const response = await createKey(form.value.name.trim(), expiresAt)
    const mapped = mapCreateKeyResponse(response)
    createdKey.value = mapped.key
    useKeyCacheStore().cacheKey(mapped.id, mapped.key)
    emit('success', mapped.id, mapped.key)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '创建失败')
  } finally {
    loading.value = false
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(createdKey.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动复制')
  }
}

function importToCcs() {
  executeCcsImport(createdKey.value)
}

function resetForm() {
  form.value.name = ''
  form.value.expiresAt = ''
  createdKey.value = ''
}
</script>

<style scoped>
.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.key-display {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}

.key-display code {
  background: var(--el-color-success-light-9);
  padding: 6px 14px;
  border-radius: 6px;
  font-size: 13px;
  word-break: break-all;
  flex: 1;
}

.form-hint {
  font-size: 12px;
  color: #98989D;
  margin-top: 4px;
}

.ccs-import-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}

.ccs-tip {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
</style>
