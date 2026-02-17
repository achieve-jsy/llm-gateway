<template>
  <el-dialog
    v-model="visible"
    title="创建 API Key"
    width="520px"
    :close-on-click-modal="false"
    destroy-on-close
    @closed="resetForm"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-width="80px" label-position="left" @submit.prevent>
      <el-form-item label="名称" prop="name">
        <el-input v-model="form.name" placeholder="输入 Key 名称，便于识别" maxlength="64" />
        <div class="form-hint">仅允许字母、数字、下划线和连字符</div>
      </el-form-item>
      <el-form-item label="过期时间" prop="expiresAt">
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
      <el-form-item label="模型组">
        <el-select
          v-model="form.groupIds"
          multiple
          filterable
          placeholder="选择可用的模型组（可选，不选则Key无可用模型）"
          style="width:100%"
          :loading="groupsLoading"
        >
          <el-option :key="'__all__'" label="全部模型（不限制）" :value="ALL_GROUPS_SENTINEL" />
          <el-option
            v-for="g in availableGroups"
            :key="g.id"
            :label="g.name"
            :value="g.id"
            :disabled="form.accessAllGroups"
          />
        </el-select>
        <div class="form-hint">{{ form.accessAllGroups ? '该 Key 可访问全部模型，不限制' : '选择后，该 Key 只能调用选中的模型组' }}</div>
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
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createKey, getAvailableGroups, toggleAccessAllGroups } from '@/services/control-plane'
import { mapCreateKeyResponse, type AvailableGroup } from '@/types/api-key'
import { executeCcsImport } from '@/utils/ccs-import'
import { copyToClipboard } from '@/utils/clipboard'
import { useKeyCacheStore } from '@/stores/key-cache'

const ALL_GROUPS_SENTINEL = -1

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void; (e: 'success', keyId: string, rawKey: string): void }>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const formRef = ref<FormInstance>()

const rules = reactive<FormRules>({
  name: [
    { required: true, message: '请输入 Key 名称', trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_-]+$/, message: '仅允许字母、数字、下划线和连字符', trigger: 'blur' },
  ],
})

const loading = ref(false)
const groupsLoading = ref(false)
const createdKey = ref('')
const availableGroups = ref<AvailableGroup[]>([])
const form = ref({ name: '', expiresAt: '', groupIds: [] as number[], accessAllGroups: false })

// 监听 groupIds 变化：选中"全部模型组"时自动清除其他选项并禁用；取消时恢复
watch(() => form.value.groupIds, (val) => {
  if (val.includes(ALL_GROUPS_SENTINEL)) {
    if (val.length > 1) {
      // 同时选了其他选项，保留"全部模型组"，清掉其余
      form.value.groupIds = [ALL_GROUPS_SENTINEL]
    }
    form.value.accessAllGroups = true
  } else {
    form.value.accessAllGroups = false
  }
})

function disabledDate(time: Date) {
  return time.getTime() < Date.now() - 86400000
}

async function loadGroups() {
  groupsLoading.value = true
  try {
    const resp = await getAvailableGroups()
    availableGroups.value = resp.data.groups || []
  } catch {
    // silently fail, group list is optional
  } finally {
    groupsLoading.value = false
  }
}

async function handleCreate() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  try {
    const expiresAt = form.value.expiresAt ? new Date(form.value.expiresAt).toISOString() : undefined
    const groupIds = (!form.value.accessAllGroups && form.value.groupIds.length > 0) ? form.value.groupIds : undefined
    const response = await createKey(form.value.name.trim(), expiresAt, groupIds)
    const mapped = mapCreateKeyResponse(response)
    // 如果选了全部模型组，创建后立即设置 access_all_groups
    if (form.value.accessAllGroups) {
      await toggleAccessAllGroups(mapped.id, true)
    }
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
    await copyToClipboard(createdKey.value)
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
  form.value.groupIds = []
  form.value.accessAllGroups = false
  createdKey.value = ''
  formRef.value?.resetFields()
}

onMounted(() => {
  loadGroups()
})
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

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95%;
    max-width: 95vw;
  }
  .key-display {
    flex-direction: column;
    align-items: stretch;
  }
  .ccs-import-row {
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }
}
</style>