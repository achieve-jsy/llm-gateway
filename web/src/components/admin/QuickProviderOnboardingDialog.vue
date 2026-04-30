<template>
  <el-dialog
    :model-value="visible"
    title="快速接入供应商"
    width="860px"
    destroy-on-close
    @close="emit('close')"
  >
    <div class="quick-provider">
      <div class="summary-band">
        <div>
          <div class="summary-title">{{ previewRows.length }} 个渠道 · {{ totalModelCreates }} 个模型</div>
          <div class="summary-subtitle">每个 Key 都会创建一个渠道，且每个渠道都会拥有输入的每个模型</div>
        </div>
        <el-tag type="info" effect="plain">{{ selectedDef?.display_name || '选择供应商' }}</el-tag>
      </div>

      <el-form
        ref="formRef"
        :model="formData"
        :rules="rules"
        label-width="112px"
        label-position="left"
      >
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="供应商" prop="definition_id">
              <el-select
                v-model="formData.definition_id"
                filterable
                placeholder="选择渠道类型"
                style="width: 100%"
                @change="handleDefinitionChange"
              >
                <el-option
                  v-for="def in definitions"
                  :key="def.id"
                  :label="def.display_name"
                  :value="def.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="继承配置">
              <el-select
                v-model="templateProviderId"
                clearable
                filterable
                :disabled="!formData.definition_id"
                placeholder="可选，从已有渠道带入配置"
                style="width: 100%"
                @change="handleTemplateChange"
              >
                <el-option
                  v-for="provider in templateProviders"
                  :key="provider.id"
                  :label="provider.name"
                  :value="provider.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="Endpoint" prop="endpoint">
          <el-input v-model="formData.endpoint" placeholder="https://..." />
          <div v-if="selectedDef?.base_url" class="form-hint">默认地址：{{ selectedDef.base_url }}</div>
        </el-form-item>

        <el-form-item label="渠道标签">
          <el-input v-model="formData.tag" placeholder="可选，多个标签用逗号分隔" />
        </el-form-item>

        <el-form-item label="用途">
          <el-input v-model="formData.purpose" placeholder="可选，例如：坐席、备用、研发；用于自动生成渠道名" />
          <div class="form-hint">不需要逐个填写渠道名称，后端会按供应商和用途自动生成并避让重名</div>
        </el-form-item>

        <el-form-item label="Key 字段" prop="credential_key">
          <el-select
            v-model="formData.credential_key"
            :disabled="credentialKeys.length <= 1"
            placeholder="选择批量填充的凭证字段"
            style="width: 100%"
          >
            <el-option
              v-for="item in credentialKeys"
              :key="item.key"
              :label="item.label"
              :value="item.key"
            />
          </el-select>
          <div class="form-hint">其他凭证字段会使用供应商默认值；如需复杂认证，请先创建一个渠道再从它继承配置</div>
        </el-form-item>

        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="API Keys" prop="keys_text">
              <el-input
                v-model="formData.keys_text"
                type="textarea"
                :rows="8"
                resize="none"
                placeholder="每行一个 Key，也支持用逗号分隔"
              />
              <div class="form-hint">已识别 {{ parsedKeys.length }} 个 Key，自动去重空行</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="模型名" prop="models_text">
              <el-input
                v-model="formData.models_text"
                type="textarea"
                :rows="8"
                resize="none"
                placeholder="每行一个模型名，如 gpt-4o 或 qwen-plus"
              />
              <div class="form-hint">已识别 {{ parsedModels.length }} 个模型；每个渠道都会创建这些模型</div>
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="模型类型">
          <el-select v-model="formData.model_type" style="width: 240px">
            <el-option label="Chat（对话）" value="chat" />
            <el-option label="Embedding（向量）" value="embedding" />
            <el-option label="Embedding-VL（多模态向量）" value="embedding_vl" />
            <el-option label="Rerank（重排序）" value="rerank" />
            <el-option label="Rerank-VL（多模态重排序）" value="rerank_vl" />
            <el-option label="Video（视频生成）" value="video" />
          </el-select>
        </el-form-item>
      </el-form>

      <div class="preview-panel">
        <div class="preview-header">
          <div>
            <span>创建预览</span>
            <span class="preview-meta">{{ previewMeta }}</span>
          </div>
          <el-button size="small" :loading="previewLoading" :disabled="parsedKeys.length === 0 || parsedModels.length === 0" @click="refreshPreview">
            刷新预览
          </el-button>
        </div>
        <el-alert
          v-if="previewWarnings.length > 0"
          type="warning"
          :closable="false"
          class="preview-warning"
        >
          <template #title>{{ previewWarnings.join('；') }}</template>
        </el-alert>
        <el-table :data="previewRows" border max-height="260" empty-text="输入 Key 和模型后生成预览">
          <el-table-column label="渠道名称" min-width="190">
            <template #default="{ row }">
              <span>{{ row.name }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Key" min-width="180">
            <template #default="{ row }">
              <span class="mono-text">{{ row.credential }}</span>
            </template>
          </el-table-column>
          <el-table-column label="将创建模型" min-width="280">
            <template #default="{ row }">
              <div class="model-tags">
                <el-tag v-for="model in row.models" :key="model" size="small" effect="plain">{{ model }}</el-tag>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="submitting" :disabled="previewRows.length === 0" @click="handleSubmit">
        创建 {{ previewRows.length }} 个渠道
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createQuickOnboardProviders, previewQuickOnboardProviders } from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'
import type { ModelPayload } from '@/types/model'
import type { Provider, ProviderDefinition, QuickOnboardPayload, QuickOnboardProviderPlan } from '@/types/provider'

type ModelType = ModelPayload['model_type']

interface FormData {
  definition_id: string
  endpoint: string
  tag: string
  purpose: string
  credential_key: string
  keys_text: string
  models_text: string
  model_type: ModelType
}

const props = defineProps<{
  visible: boolean
  definitions: ProviderDefinition[]
  providers: Provider[]
}>()

const emit = defineEmits<{
  close: []
  saved: []
  'update:visible': [val: boolean]
}>()

const formRef = ref<FormInstance>()
const submitting = ref(false)
const previewLoading = ref(false)
const templateProviderId = ref('')
const serverPreviewRows = ref<QuickOnboardProviderPlan[]>([])
const previewWarnings = ref<string[]>([])
const previewMatrixCount = ref(0)

const formData = ref<FormData>({
  definition_id: '',
  endpoint: '',
  tag: '',
  purpose: '',
  credential_key: '',
  keys_text: '',
  models_text: '',
  model_type: 'chat'
})

const selectedDef = computed(() =>
  props.definitions.find(def => def.id === formData.value.definition_id) || null
)

const templateProviders = computed(() =>
  props.providers.filter(provider => provider.definition_id === formData.value.definition_id)
)

const credentialKeys = computed(() => {
  return getCredentialSchemaEntries(selectedDef.value || undefined).map(([key, field]) => ({ key, label: field.label || key }))
})

const parsedKeys = computed(() => parseMultiline(formData.value.keys_text))
const parsedModels = computed(() => parseMultiline(formData.value.models_text))
const totalModelCreates = computed(() => parsedKeys.value.length * parsedModels.value.length)
const previewMeta = computed(() => {
  if (previewMatrixCount.value > 0) return `${previewRows.value.length} Keys × ${parsedModels.value.length} 模型 = ${previewMatrixCount.value} 项`
  return `${parsedKeys.value.length} Keys × ${parsedModels.value.length} 模型`
})

const previewRows = computed(() => {
  if (serverPreviewRows.value.length > 0) return serverPreviewRows.value
  return buildLocalPreviewRows()
})

const rules: FormRules = {
  definition_id: [{ required: true, message: '请选择供应商', trigger: 'change' }],
  endpoint: [{ required: true, validator: validateEndpoint, trigger: 'blur' }],
  credential_key: [{ required: true, message: '请选择 Key 写入字段', trigger: 'change' }],
  keys_text: [{ required: true, validator: validateKeys, trigger: 'blur' }],
  models_text: [{ required: true, validator: validateModels, trigger: 'blur' }]
}

watch(
  () => props.visible,
  (visible) => {
    if (visible) resetForm()
  }
)

watch(
  () => [formData.value.definition_id, formData.value.endpoint, formData.value.keys_text, formData.value.models_text, formData.value.credential_key, formData.value.tag, formData.value.purpose, formData.value.model_type],
  () => {
    serverPreviewRows.value = []
    previewWarnings.value = []
    previewMatrixCount.value = 0
  }
)

function resetForm() {
  templateProviderId.value = ''
  serverPreviewRows.value = []
  previewWarnings.value = []
  previewMatrixCount.value = 0
  formData.value = {
    definition_id: '',
    endpoint: '',
    tag: '',
    purpose: '',
    credential_key: '',
    keys_text: '',
    models_text: '',
    model_type: 'chat'
  }
}

function parseMultiline(value: string): string[] {
  const seen = new Set<string>()
  const result: string[] = []
  value
    .split(/[\n,，;；]+/)
    .map(item => item.trim())
    .filter(Boolean)
    .forEach(item => {
      if (!seen.has(item)) {
        seen.add(item)
        result.push(item)
      }
    })
  return result
}

function validateEndpoint(_rule: unknown, value: string, callback: (error?: Error) => void) {
  if (!value) return callback(new Error('请输入 Endpoint'))
  if (!/^https?:\/\/.+/.test(value)) return callback(new Error('Endpoint 必须以 http:// 或 https:// 开头'))
  callback()
}

function validateKeys(_rule: unknown, _value: string, callback: (error?: Error) => void) {
  if (parsedKeys.value.length === 0) return callback(new Error('请输入至少一个 Key'))
  callback()
}

function validateModels(_rule: unknown, _value: string, callback: (error?: Error) => void) {
  if (parsedModels.value.length === 0) return callback(new Error('请输入至少一个模型'))
  callback()
}

function handleDefinitionChange(defId: string) {
  const def = props.definitions.find(item => item.id === defId)
  templateProviderId.value = ''
  formData.value.endpoint = def?.base_url || ''
  formData.value.credential_key = findDefaultCredentialKey(def)
}

function handleTemplateChange(providerId: string) {
  const provider = props.providers.find(item => item.id === providerId)
  if (!provider) return
  formData.value.endpoint = provider.endpoint || selectedDef.value?.base_url || ''
  formData.value.tag = provider.tag || ''
}

function findDefaultCredentialKey(def?: ProviderDefinition): string {
  const entries = getCredentialSchemaEntries(def)
  const apiKey = entries.find(([key]) => ['api_key', 'key', 'token'].includes(key))
  if (apiKey) return apiKey[0]
  const secret = entries.find(([, field]) => field.secret)
  return secret?.[0] || entries[0]?.[0] || ''
}

function getCredentialSchemaEntries(def?: ProviderDefinition): Array<[string, { label?: string; default?: string; secret?: boolean }]> {
  const entries = Object.entries(def?.credential_schema || {}) as Array<[string, { label?: string; default?: string; secret?: boolean }]>
  if (entries.length > 0) return entries
  return [['api_key', { label: 'API Key', secret: true }]]
}

function buildCredentials(key: string): string {
  const credentials: Record<string, string> = {}
  for (const [fieldKey, field] of getCredentialSchemaEntries(selectedDef.value || undefined)) {
    credentials[fieldKey] = field.default || ''
  }
  credentials[formData.value.credential_key] = key
  return JSON.stringify(credentials)
}

function buildPayload(): QuickOnboardPayload {
  return {
    provider: {
      definition_id: formData.value.definition_id,
      type: selectedDef.value?.adapter || formData.value.definition_id,
      endpoint: formData.value.endpoint,
      test_model: parsedModels.value[0] || undefined,
      tag: formData.value.tag,
      remark: `快速接入创建：${selectedDef.value?.display_name || formData.value.definition_id}`
    },
    provider_slug: selectedDef.value?.id || formData.value.definition_id,
    purpose: formData.value.purpose || '',
    credentials: parsedKeys.value.map(key => ({ credentials: buildCredentials(key) })),
    models: parsedModels.value.map(modelName => ({
      name: modelName,
      upstream_model_id: modelName,
      model_type: formData.value.model_type,
      price_mode: 'catalog',
      weight: 1
    }))
  }
}

function maskKey(key: string): string {
  if (key.length <= 8) return '*'.repeat(key.length)
  return `${key.slice(0, 4)}...${key.slice(-4)}`
}

function buildLocalPreviewRows(): QuickOnboardProviderPlan[] {
  if (!selectedDef.value) return []
  const prefix = [selectedDef.value.id, formData.value.tag].filter(Boolean).join('-') || selectedDef.value.id
  return parsedKeys.value.map((key, index) => ({
    index,
    name: `${prefix}-${String(index + 1).padStart(3, '0')}`,
    credential: maskKey(key),
    models: parsedModels.value,
    model_count: parsedModels.value.length,
    will_create: true
  }))
}

async function refreshPreview(): Promise<boolean> {
  if (!selectedDef.value || parsedKeys.value.length === 0 || parsedModels.value.length === 0) return false
  previewLoading.value = true
  try {
    const preview = await previewQuickOnboardProviders(buildPayload())
    serverPreviewRows.value = preview.providers
    previewWarnings.value = preview.warnings || []
    previewMatrixCount.value = preview.matrix_count
    return true
  } catch (error: any) {
    ElMessage.error(resolveErrorMessage(error) || '生成预览失败')
    return false
  } finally {
    previewLoading.value = false
  }
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid || !selectedDef.value) return

  const previewReady = serverPreviewRows.value.length > 0 || await refreshPreview()
  if (!previewReady) return

  if (totalModelCreates.value > 200) {
    try {
      await ElMessageBox.confirm(
        `本次将创建 ${previewRows.value.length} 个渠道和 ${totalModelCreates.value} 个模型，确认继续？`,
        '批量创建确认',
        { type: 'warning', confirmButtonText: '继续创建', cancelButtonText: '取消' }
      )
    } catch {
      return
    }
  }

  submitting.value = true
  try {
    const result = await createQuickOnboardProviders(buildPayload())
    const createdModels = result.succeeded_count * result.model_count_per_provider
    if (result.failed_count === 0) {
      ElMessage.success(`创建完成：${result.succeeded_count} 个渠道，${createdModels} 个模型`)
    } else {
      ElMessage.warning(`部分完成：${result.succeeded_count} 个渠道成功，${result.failed_count} 个渠道失败`)
      console.warn('[QuickProviderOnboarding] failures', result.results.filter(item => item.status === 'failed'))
    }
    if (result.succeeded_count > 0) emit('saved')
  } catch (error: any) {
    ElMessage.error(resolveErrorMessage(error) || '快速接入失败')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.quick-provider {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.summary-band {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  background: var(--el-fill-color-lighter);
}

.summary-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.summary-subtitle,
.form-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}

.form-hint.inline {
  margin-left: 10px;
}

.preview-panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  overflow: hidden;
}

.preview-warning {
  border-radius: 0;
  border-left: 0;
  border-right: 0;
}

.preview-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  background: var(--el-fill-color-lighter);
  border-bottom: 1px solid var(--el-border-color-light);
}

.preview-meta {
  font-size: 12px;
  font-weight: 400;
  color: var(--el-text-color-secondary);
}

.mono-text {
  font-family: monospace;
  color: var(--el-text-color-secondary);
}

.model-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95% !important;
    max-width: 95vw !important;
  }

  :deep(.el-col) {
    max-width: 100%;
    flex: 0 0 100%;
  }

  .summary-band {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
