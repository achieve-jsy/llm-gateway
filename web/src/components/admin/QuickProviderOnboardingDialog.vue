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
          <div class="summary-title">批量创建 {{ parsedKeys.length }} 个渠道</div>
          <div class="summary-subtitle">每个 Key 生成一个渠道，并绑定当前输入的全部模型</div>
        </div>
        <div class="summary-metrics">
          <span>{{ parsedKeys.length }} Keys</span>
          <span>{{ parsedModels.length }} 模型</span>
          <span>{{ totalModelCreates }} 绑定</span>
        </div>
      </div>

      <el-form
        ref="formRef"
        :model="formData"
        :rules="rules"
        label-position="top"
      >
        <section class="section-block">
          <div class="section-heading">选择供应商</div>
          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="供应商" prop="definition_id">
                <el-select
                  v-model="formData.definition_id"
                  filterable
                  placeholder="选择供应商"
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
                  placeholder="可选，复用已有渠道配置"
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
        </section>

        <section class="batch-grid">
          <div class="input-panel">
            <div class="panel-heading">
              <div>
                <div class="panel-title">API Keys</div>
                <div class="panel-subtitle">每个 Key 会创建一个独立渠道</div>
              </div>
              <el-tag size="small" effect="plain">{{ parsedKeys.length }} 个</el-tag>
            </div>
            <el-form-item prop="keys_text" class="textarea-item">
              <el-input
                v-model="formData.keys_text"
                type="textarea"
                :rows="8"
                resize="none"
                placeholder="每行一个 Key，也支持逗号分隔"
              />
            </el-form-item>
          </div>

          <div class="input-panel">
            <div class="panel-heading">
              <div>
                <div class="panel-title">模型</div>
                <div class="panel-subtitle">每个渠道都会创建这些模型</div>
              </div>
              <el-tag size="small" effect="plain">{{ parsedModels.length }} 个</el-tag>
            </div>
            <el-form-item prop="models_text" class="textarea-item">
              <el-input
                v-model="formData.models_text"
                type="textarea"
                :rows="6"
                resize="none"
                placeholder="每行一个模型名，如 gpt-4o 或 qwen-plus"
              />
            </el-form-item>
            <el-form-item label="模型类型" class="model-type-item">
              <el-select v-model="formData.model_type" style="width: 100%">
                <el-option label="Chat（对话）" value="chat" />
                <el-option label="Embedding（向量）" value="embedding" />
                <el-option label="Embedding-VL（多模态向量）" value="embedding_vl" />
                <el-option label="Rerank（重排序）" value="rerank" />
                <el-option label="Rerank-VL（多模态重排序）" value="rerank_vl" />
                <el-option label="Video（视频生成）" value="video" />
              </el-select>
            </el-form-item>
          </div>
        </section>

        <el-collapse v-model="pricingPanels" class="advanced-collapse">
          <el-collapse-item title="价格设置（可选）" name="pricing">
            <el-form-item label="价格来源">
              <el-radio-group v-model="pricingMode">
                <el-radio-button label="catalog">Catalog 自动匹配</el-radio-button>
                <el-radio-button label="uniform">统一手动价格</el-radio-button>
                <el-radio-button label="per_model">按模型分别设置</el-radio-button>
              </el-radio-group>
              <div class="form-hint">默认使用 Catalog；手动价格单位为 USD / 1M tokens。</div>
            </el-form-item>

            <el-row v-if="pricingMode === 'uniform'" :gutter="16">
              <el-col :span="12">
                <el-form-item label="输入价格">
                  <el-input-number v-model="uniformPricing.input" :min="0" :precision="6" :step="0.1" style="width: 100%" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="输出价格">
                  <el-input-number v-model="uniformPricing.output" :min="0" :precision="6" :step="0.1" style="width: 100%" />
                </el-form-item>
              </el-col>
            </el-row>

            <div v-else-if="pricingMode === 'per_model'" class="pricing-table">
              <div class="pricing-row pricing-head">
                <span>模型名</span>
                <span>输入价格</span>
                <span>输出价格</span>
              </div>
              <div v-for="model in parsedModels" :key="model" class="pricing-row">
                <span class="mono-text">{{ model }}</span>
                <el-input-number v-model="perModelPricing[model].input" :min="0" :precision="6" :step="0.1" />
                <el-input-number v-model="perModelPricing[model].output" :min="0" :precision="6" :step="0.1" />
              </div>
              <div v-if="parsedModels.length === 0" class="pricing-empty">输入模型名后可分别设置价格</div>
            </div>
          </el-collapse-item>
        </el-collapse>

        <el-collapse v-model="advancedPanels" class="advanced-collapse">
          <el-collapse-item title="高级设置" name="advanced">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item label="Endpoint" prop="endpoint">
                  <el-input v-model="formData.endpoint" placeholder="https://..." />
                  <div v-if="selectedDef?.base_url" class="form-hint">默认地址：{{ selectedDef.base_url }}</div>
                </el-form-item>
              </el-col>
              <el-col :span="12">
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
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item label="渠道标签">
                  <el-input v-model="formData.tag" placeholder="可选，多个标签用逗号分隔" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="渠道名前缀">
                  <el-input v-model="formData.name_prefix" placeholder="可选，如：qwen-agent" />
                  <div class="form-hint">后端会自动追加 001、002，并避让已有重名。</div>
                </el-form-item>
              </el-col>
            </el-row>
          </el-collapse-item>
        </el-collapse>
      </el-form>

      <div class="preview-panel" :class="{ 'is-empty': previewRows.length === 0 }">
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
        <div v-if="previewRows.length === 0" class="preview-empty">
          输入 Key 和模型后生成渠道预览
        </div>
        <el-table v-else :data="previewRows" border max-height="220">
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
          <el-table-column label="价格来源" width="140">
            <template #default>
              <el-tag size="small" effect="plain">{{ pricingModeLabel }}</el-tag>
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
type PricingMode = 'catalog' | 'uniform' | 'per_model'
type PricePair = { input: number; output: number }

interface FormData {
  definition_id: string
  endpoint: string
  tag: string
  name_prefix: string
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
const advancedPanels = ref<string[]>([])
const pricingPanels = ref<string[]>([])
const pricingMode = ref<PricingMode>('catalog')
const uniformPricing = ref<PricePair>({ input: 0, output: 0 })
const perModelPricing = ref<Record<string, PricePair>>({})
const serverPreviewRows = ref<QuickOnboardProviderPlan[]>([])
const previewWarnings = ref<string[]>([])
const previewMatrixCount = ref(0)

const formData = ref<FormData>({
  definition_id: '',
  endpoint: '',
  tag: '',
  name_prefix: '',
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

const pricingModeLabel = computed(() => {
  if (pricingMode.value === 'uniform') return '统一手动'
  if (pricingMode.value === 'per_model') return '单独手动'
  return 'Catalog'
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
  () => [formData.value.definition_id, formData.value.endpoint, formData.value.keys_text, formData.value.models_text, formData.value.credential_key, formData.value.tag, formData.value.name_prefix, formData.value.model_type],
  () => {
    serverPreviewRows.value = []
    previewWarnings.value = []
    previewMatrixCount.value = 0
  }
)

function resetForm() {
  templateProviderId.value = ''
  advancedPanels.value = []
  pricingPanels.value = []
  pricingMode.value = 'catalog'
  uniformPricing.value = { input: 0, output: 0 }
  perModelPricing.value = {}
  serverPreviewRows.value = []
  previewWarnings.value = []
  previewMatrixCount.value = 0
  formData.value = {
    definition_id: '',
    endpoint: '',
    tag: '',
    name_prefix: '',
    credential_key: '',
    keys_text: '',
    models_text: '',
    model_type: 'chat'
  }
}

watch(parsedModels, (models) => {
  const next: Record<string, PricePair> = {}
  models.forEach(model => {
    next[model] = perModelPricing.value[model] || { input: 0, output: 0 }
  })
  perModelPricing.value = next
})

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
    name_prefix: formData.value.name_prefix || undefined,
    credentials: parsedKeys.value.map(key => ({ credentials: buildCredentials(key) })),
    models: parsedModels.value.map(buildModelPayload)
  }
}

function buildModelPayload(modelName: string) {
  const payload: QuickOnboardPayload['models'][number] = {
    name: modelName,
    upstream_model_id: modelName,
    model_type: formData.value.model_type,
    price_mode: 'catalog',
    weight: 1
  }
  if (pricingMode.value === 'uniform') {
    return applyManualPricing(payload, uniformPricing.value)
  }
  if (pricingMode.value === 'per_model') {
    return applyManualPricing(payload, perModelPricing.value[modelName] || { input: 0, output: 0 })
  }
  return payload
}

function applyManualPricing(payload: QuickOnboardPayload['models'][number], pair: PricePair) {
  return {
    ...payload,
    price_mode: 'manual' as const,
    input_price: pair.input,
    output_price: pair.output,
  }
}

function maskKey(key: string): string {
  if (key.length <= 8) return '*'.repeat(key.length)
  return `${key.slice(0, 4)}...${key.slice(-4)}`
}

function buildLocalPreviewRows(): QuickOnboardProviderPlan[] {
  if (!selectedDef.value) return []
  const prefix = slugifyNamePrefix(formData.value.name_prefix || selectedDef.value.id)
  return parsedKeys.value.map((key, index) => ({
    index,
    name: `${prefix}-${String(index + 1).padStart(3, '0')}`,
    credential: maskKey(key),
    models: parsedModels.value,
    model_count: parsedModels.value.length,
    will_create: true
  }))
}

function slugifyNamePrefix(value: string): string {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
  return normalized || 'provider'
}

async function refreshPreview(): Promise<boolean> {
  if (!selectedDef.value || parsedKeys.value.length === 0 || parsedModels.value.length === 0) return false
  if (!validatePricing()) return false
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
  if (!validatePricing()) return

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

function validatePricing(): boolean {
  if (pricingMode.value === 'catalog') return true
  if (pricingMode.value === 'uniform') {
    if (uniformPricing.value.input > 0 || uniformPricing.value.output > 0) return true
    ElMessage.error('请输入统一手动价格，或切回 Catalog 自动匹配')
    return false
  }
  const missing = parsedModels.value.find(model => {
    const pair = perModelPricing.value[model]
    return !pair || (pair.input <= 0 && pair.output <= 0)
  })
  if (!missing) return true
  ElMessage.error(`请为模型 ${missing} 输入手动价格，或切回 Catalog 自动匹配`)
  return false
}
</script>

<style scoped>
.quick-provider {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.summary-band {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 18px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  background: linear-gradient(180deg, var(--el-fill-color-extra-light), var(--el-fill-color-lighter));
}

.summary-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.summary-metrics {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.summary-metrics span {
  min-width: 68px;
  padding: 6px 10px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  background: var(--el-bg-color);
  color: var(--el-text-color-regular);
  font-size: 12px;
  text-align: center;
}

.summary-subtitle,
.form-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}

.section-block {
  padding: 14px 16px 0;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  background: var(--el-bg-color);
}

.section-heading {
  margin-bottom: 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.batch-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
}

.input-panel {
  min-width: 0;
  padding: 14px 16px 16px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  background: var(--el-bg-color);
}

.panel-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.panel-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.panel-subtitle {
  margin-top: 2px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.4;
}

.textarea-item,
.model-type-item {
  margin-bottom: 0;
}

.model-type-item {
  margin-top: 12px;
}

.advanced-collapse {
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  overflow: hidden;
}

.advanced-collapse :deep(.el-collapse-item__header) {
  height: 42px;
  padding: 0 16px;
  border-bottom: 0;
  font-weight: 600;
  color: var(--el-text-color-primary);
  background: var(--el-fill-color-extra-light);
}

.advanced-collapse :deep(.el-collapse-item__wrap) {
  border-bottom: 0;
}

.advanced-collapse :deep(.el-collapse-item__content) {
  padding: 14px 16px 0;
}

.pricing-table {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 14px;
}

.pricing-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 160px 160px;
  gap: 10px;
  align-items: center;
}

.pricing-head {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: 600;
}

.pricing-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 48px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  border: 1px dashed var(--el-border-color-light);
  border-radius: 6px;
}

:deep(.el-form-item__label) {
  margin-bottom: 6px;
  color: var(--el-text-color-regular);
  font-weight: 500;
}

:deep(.el-form-item.is-error .el-textarea__inner),
:deep(.el-form-item.is-error .el-input__wrapper),
:deep(.el-form-item.is-error .el-select__wrapper) {
  box-shadow: 0 0 0 1px var(--el-color-danger-light-5) inset;
}

.preview-panel {
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  overflow: hidden;
  background: var(--el-bg-color);
}

.preview-panel.is-empty {
  border-style: dashed;
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
  padding: 10px 14px;
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

.preview-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 64px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
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

  .summary-metrics {
    justify-content: flex-start;
  }

  .batch-grid {
    grid-template-columns: 1fr;
  }

  .pricing-row {
    grid-template-columns: 1fr;
  }
}
</style>
