<template>
  <el-dialog
    :model-value="visible"
    :title="isEdit ? '编辑渠道' : '新建渠道'"
    width="640px"
    destroy-on-close
    @close="emit('close')"
  >
    <el-tabs v-model="activeTab">
      <!-- 基本信息 Tab -->
      <el-tab-pane label="基本信息" name="basic">
        <el-form
          ref="basicFormRef"
          :model="formData"
          :rules="basicRules"
          label-width="100px"
          label-position="left"
        >
          <el-form-item label="名称" prop="name">
            <el-input v-model="formData.name" placeholder="请输入渠道名称（全局唯一）" />
          </el-form-item>

          <el-form-item label="渠道类型" prop="definition_id">
            <el-select
              v-model="formData.definition_id"
              placeholder="请选择渠道类型"
              style="width: 100%"
              @change="handleDefinitionChange"
            >
              <el-option
                v-for="def in definitions"
                :key="def.id"
                :label="def.display_name"
                :value="def.id"
              >
                <span>{{ def.display_name }}</span>
                <span style="color: var(--el-text-color-secondary); font-size: 12px; margin-left: 8px">
                  {{ def.description }}
                </span>
              </el-option>
            </el-select>
          </el-form-item>

          <el-form-item label="Endpoint" prop="endpoint">
            <el-input
              v-model="formData.endpoint"
              :disabled="formData.definition_id === 'bedrock'"
              placeholder="https://..."
            />
            <div v-if="formData.definition_id === 'bedrock'" class="form-hint">
              由 Region 自动生成，格式：https://bedrock-runtime.{region}.amazonaws.com
            </div>
            <div v-else-if="selectedDef?.base_url" class="form-hint">
              默认地址：{{ selectedDef.base_url }}
            </div>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <!-- 凭证 Tab -->
      <el-tab-pane label="凭证" name="credentials">
        <el-form
          ref="credFormRef"
          :model="formData"
          label-width="100px"
          label-position="left"
        >
          <template v-if="selectedDef">
            <el-form-item
              v-for="(field, key) in selectedDef.credential_schema"
              :key="key"
              :label="field.label"
              :prop="'credentials_map.' + key"
              :rules="getCredentialFieldRules(field)"
            >
              <el-input
                v-model="formData.credentials_map[key]"
                :type="field.secret && !isEdit ? 'password' : 'text'"
                :show-password="field.secret && !isEdit"
                :placeholder="field.placeholder || (isEdit ? '已配置，清空后输入新值可更新' : `请输入${field.label}`)"
              />
              <div v-if="field.default && !formData.credentials_map[key]" class="form-hint">
                默认值：{{ field.default }}
              </div>
            </el-form-item>
          </template>
          <el-empty v-else description="请先选择渠道类型" :image-size="60" />
        </el-form>
      </el-tab-pane>

      <!-- 高级设置 Tab -->
      <el-tab-pane label="高级设置" name="advanced">
        <el-form
          :model="formData"
          label-width="100px"
          label-position="left"
        >
          <el-form-item label="优先级">
            <el-input-number v-model="formData.priority" :min="0" :max="9999" />
            <span class="form-hint inline">数值越大优先级越高</span>
          </el-form-item>

          <el-form-item label="权重">
            <el-input-number v-model="formData.weight" :min="0" :max="9999" />
            <span class="form-hint inline">负载均衡权重</span>
          </el-form-item>

          <!-- AWS Bedrock: Region 设置 -->
          <el-form-item v-if="selectedDef?.id === 'bedrock'" label="Region">
            <el-input v-model="formData.region" placeholder="us-east-1" />
            <div class="form-hint">AWS 区域，用于构造 Bedrock 端点地址</div>
          </el-form-item>

          <!-- Azure OpenAI: API Version 设置 -->
          <el-form-item v-if="selectedDef?.id === 'azure-openai'" label="API Version">
            <el-input v-model="formData.api_version" placeholder="2024-02-01" />
            <div class="form-hint">Azure OpenAI API 版本号</div>
          </el-form-item>

          <el-form-item label="超时(秒)">
            <el-input-number v-model="formData.timeout" :min="1" :max="300" />
          </el-form-item>

          <el-form-item label="最大重试">
            <el-input-number v-model="formData.max_retries" :min="0" :max="10" />
          </el-form-item>

          <el-form-item label="测试模型">
            <el-input v-model="formData.test_model" placeholder="用于连通性测试的模型名" />
          </el-form-item>

          <el-form-item label="标签">
            <!-- 多标签输入：回车确认，点 × 删除 -->
            <div class="tag-input-wrap">
              <el-tag
                v-for="(t, i) in formData.tags"
                :key="t"
                closable
                size="small"
                class="tag-item"
                @close="formData.tags.splice(i, 1)"
              >
                {{ t }}
              </el-tag>
              <el-input
                v-if="tagInputVisible"
                ref="tagInputRef"
                v-model="tagInputVal"
                size="small"
                class="tag-input"
                @keyup.enter="confirmTag"
                @blur="confirmTag"
              />
              <el-button
                v-else
                size="small"
                class="tag-add-btn"
                @click="showTagInput"
              >
                + 添加标签
              </el-button>
            </div>
            <div class="form-hint">多个标签用于分组筛选，回车确认每个标签</div>
          </el-form-item>

          <el-form-item label="备注">
            <el-input v-model="formData.remark" type="textarea" :rows="3" placeholder="备注信息" />
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <!-- 模型预填 Tab（仅创建时） -->
      <el-tab-pane v-if="!isEdit" label="预填模型" name="models">
        <div v-if="selectedDef && selectedDef.default_models?.length" class="models-prefill">
          <p class="form-hint" style="margin-bottom: 12px">
            选择要自动注册的模型（来自 {{ selectedDef.display_name }} 默认模型列表）
          </p>
          <el-checkbox-group v-model="formData.selected_models">
            <el-checkbox
              v-for="model in selectedDef.default_models"
              :key="model"
              :label="model"
              :value="model"
              class="prefill-model-item"
            >
              {{ model }}
            </el-checkbox>
          </el-checkbox-group>

          <el-divider content-position="left">手动添加</el-divider>
          <div class="extra-model-input">
            <el-input
              v-model="extraModelName"
              placeholder="输入额外模型名称"
              class="extra-model-field"
              @keyup.enter="addExtraModel"
            />
            <el-button size="small" @click="addExtraModel">添加</el-button>
          </div>
          <div v-if="formData.extra_models.length" class="extra-model-tags">
            <el-tag
              v-for="(m, idx) in formData.extra_models"
              :key="m"
              closable
              class="extra-model-tag"
              @close="formData.extra_models.splice(idx, 1)"
            >
              {{ m }}
            </el-tag>
          </div>
        </div>
        <el-empty v-else description="请先选择渠道类型，或该类型无默认模型" :image-size="60" />
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        确定
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import type { FormInstance, FormRules, InputInstance } from 'element-plus'
import { ElMessage } from 'element-plus'
import { createProvider, updateProvider, getProviderDefinitions, createModel } from '@/services/control-plane'
import type { Provider, ProviderPayload, ProviderDefinition, CredentialFieldSchema } from '@/types/provider'

const props = defineProps<{
  visible: boolean
  provider: Provider | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
  'update:visible': [val: boolean]
}>()

const basicFormRef = ref<FormInstance>()
const credFormRef = ref<FormInstance>()
const submitting = ref(false)
const definitions = ref<ProviderDefinition[]>([])
const activeTab = ref('basic')
const extraModelName = ref('')
const maskedCredentials = ref('') // 编辑模式下记住脱敏值，用于判断是否修改

const isEdit = computed(() => !!props.provider)

/** 当前选中的渠道定义 */
const selectedDef = computed(() =>
  definitions.value.find(d => d.id === formData.value.definition_id) || null
)

interface FormData {
  name: string
  definition_id: string
  endpoint: string
  credentials_map: Record<string, string>
  priority: number
  weight: number
  timeout: number
  max_retries: number
  test_model: string
  tags: string[]       // 多标签，提交时用逗号拼接存入 tag 字段
  remark: string
  region: string
  api_version: string
  selected_models: string[]
  extra_models: string[]
}

const formData = ref<FormData>({
  name: '',
  definition_id: '',
  endpoint: '',
  credentials_map: {},
  priority: 0,
  weight: 1,
  timeout: 30,
  max_retries: 2,
  test_model: '',
  tags: [],
  remark: '',
  region: '',
  api_version: '',
  selected_models: [],
  extra_models: []
})

// ─── 多标签输入逻辑 ───────────────────────────────────────────────────────────
const tagInputVisible = ref(false)
const tagInputVal = ref('')
const tagInputRef = ref<InputInstance>()

function showTagInput() {
  tagInputVisible.value = true
  nextTick(() => tagInputRef.value?.focus())
}

function confirmTag() {
  const val = tagInputVal.value.trim()
  if (val && !formData.value.tags.includes(val)) {
    formData.value.tags.push(val)
  }
  tagInputVal.value = ''
  tagInputVisible.value = false
}
// ─────────────────────────────────────────────────────────────────────────────

/** 加载渠道定义列表 */
onMounted(async () => {
  try {
    definitions.value = await getProviderDefinitions()
  } catch {
    ElMessage.error('加载渠道类型列表失败')
  }
})

/** 监听 provider 变化，回填表单（编辑模式） */
watch(
  () => props.provider,
  (val) => {
    activeTab.value = 'basic'
    if (val) {
      formData.value = {
        name: val.name,
        definition_id: val.definition_id || val.type,
        endpoint: val.endpoint,
        credentials_map: {},
        priority: val.priority || 0,
        weight: val.weight || 1,
        timeout: val.timeout || 30,
        max_retries: val.max_retries || 2,
        test_model: val.test_model || '',
        tags: val.tag ? val.tag.split(',').map(t => t.trim()).filter(Boolean) : [],
        remark: val.remark || '',
        region: '',
        api_version: '',
        selected_models: [],
        extra_models: []
      }
      // 回填脱敏凭证值（让管理员看到当前配的是哪个 Key）
      if (val.credentials && selectedDef.value?.credential_schema) {
        try {
          const parsed = JSON.parse(val.credentials)
          for (const key of Object.keys(selectedDef.value.credential_schema)) {
            formData.value.credentials_map[key] = parsed[key] || ''
          }
        } catch {
          // 非 JSON 格式（兼容旧数据），整体填入第一个字段
          const firstKey = Object.keys(selectedDef.value.credential_schema)[0]
          if (firstKey) {
            formData.value.credentials_map[firstKey] = val.credentials
          }
        }
      }
      // 记住脱敏值，提交时用于判断是否被修改
      maskedCredentials.value = val.credentials || ''
      // 从 endpoint 中提取 region（Bedrock: https://bedrock-runtime.{region}.amazonaws.com）
      if (val.definition_id === 'bedrock' && val.endpoint) {
        const match = val.endpoint.match(/bedrock-runtime\.(.+?)\.amazonaws/)
        if (match) formData.value.region = match[1]
      }
    } else {
      formData.value = {
        name: '',
        definition_id: '',
        endpoint: '',
        credentials_map: {},
        priority: 0,
        weight: 1,
        timeout: 30,
        max_retries: 2,
        test_model: '',
        tags: [],
        remark: '',
        region: '',
        api_version: '',
        selected_models: [],
        extra_models: []
      }
    }
  },
  { immediate: true }
)

/** 切换渠道定义时，自动填充默认 Endpoint 和凭证默认值 */
function handleDefinitionChange(defId: string) {
  const def = definitions.value.find(d => d.id === defId)
  if (!def) return

  if (def.base_url && !formData.value.endpoint) {
    formData.value.endpoint = def.base_url
  }

  const newMap: Record<string, string> = {}
  for (const [key, field] of Object.entries(def.credential_schema)) {
    newMap[key] = field.default || ''
  }
  formData.value.credentials_map = newMap

  // 设置厂商专属默认值
  if (defId === 'bedrock') {
    formData.value.region = def.vendor_options?.default_region || 'us-east-1'
    // 自动用 region 构造 endpoint
    formData.value.endpoint = `https://bedrock-runtime.${formData.value.region}.amazonaws.com`
  }
  if (defId === 'azure-openai') {
    formData.value.api_version = def.vendor_options?.default_api_version || '2024-02-01'
  }

  // 预选所有默认模型
  if (def.default_models?.length) {
    formData.value.selected_models = [...def.default_models]
  }
}

/** 监听 region 变化，自动更新 Bedrock endpoint */
watch(
  () => formData.value.region,
  (newRegion) => {
    if (formData.value.definition_id === 'bedrock' && newRegion) {
      formData.value.endpoint = `https://bedrock-runtime.${newRegion}.amazonaws.com`
    }
  }
)

/** 添加额外模型 */
function addExtraModel() {
  const name = extraModelName.value.trim()
  if (!name) return
  if (formData.value.extra_models.includes(name) || formData.value.selected_models.includes(name)) {
    ElMessage.warning('模型已存在列表中')
    return
  }
  formData.value.extra_models.push(name)
  extraModelName.value = ''
}

/** 根据 schema 生成凭证字段校验规则（新建模式必填，编辑模式不必填） */
function getCredentialFieldRules(field: CredentialFieldSchema) {
  const rules: any[] = []
  if (field.required) {
    rules.push({ required: !isEdit.value, message: `请输入${field.label}`, trigger: 'blur' })
  }
  return rules
}

const endpointValidator = (_rule: any, value: string, callback: any) => {
  if (!value) return callback(new Error('请输入 Endpoint'))
  if (!/^https?:\/\/.+/.test(value)) return callback(new Error('Endpoint 必须以 http:// 或 https:// 开头'))
  callback()
}

const basicRules: FormRules = {
  name: [{ required: true, message: '请输入渠道名称', trigger: 'blur' }],
  definition_id: [{ required: true, message: '请选择渠道类型', trigger: 'change' }],
  endpoint: [{ required: true, validator: endpointValidator, trigger: 'blur' }]
}

/** 提交表单 */
async function handleSubmit() {
  // 校验基本信息
  if (basicFormRef.value) {
    const valid = await basicFormRef.value.validate().catch(() => false)
    if (!valid) {
      activeTab.value = 'basic'
      return
    }
  }

  // 新建模式：校验凭证必填
  if (!isEdit.value && credFormRef.value) {
    const valid = await credFormRef.value.validate().catch(() => false)
    if (!valid) {
      activeTab.value = 'credentials'
      return
    }
  }

  submitting.value = true
  try {
    const credentialsJson = JSON.stringify(formData.value.credentials_map)
    const hasCredentials = Object.values(formData.value.credentials_map).some(v => v !== '')

    // Bedrock: 根据 region 自动构造 endpoint
    let endpoint = formData.value.endpoint
    if (formData.value.definition_id === 'bedrock' && formData.value.region) {
      endpoint = `https://bedrock-runtime.${formData.value.region}.amazonaws.com`
    }

    const payload: ProviderPayload = {
      name: formData.value.name,
      definition_id: formData.value.definition_id,
      endpoint: endpoint,
      credentials: hasCredentials ? credentialsJson : '',
      priority: formData.value.priority,
      weight: formData.value.weight,
      timeout: formData.value.timeout,
      max_retries: formData.value.max_retries,
      test_model: formData.value.test_model || undefined,
      tag: formData.value.tags.join(','),  // 空数组时传 '' 让后端清空标签
      remark: formData.value.remark || undefined
    }

    if (isEdit.value && props.provider) {
      // 判断凭证是否被修改：对比每个字段当前值与原始脱敏值
      let isCredentialModified = false
      try {
        const originalMasked = JSON.parse(maskedCredentials.value || '{}')
        for (const [key, val] of Object.entries(formData.value.credentials_map)) {
          if (val !== '' && val !== (originalMasked[key] || '')) {
            isCredentialModified = true
            break
          }
        }
      } catch {
        // 非 JSON 旧格式，任何非空输入都视为修改
        isCredentialModified = Object.values(formData.value.credentials_map).some(v => v !== '')
      }
      if (!isCredentialModified) {
        delete (payload as any).credentials
      }
      await updateProvider(props.provider.id, payload)
      ElMessage.success('更新成功')
    } else {
      const created = await createProvider(payload)
      // 创建后自动注册预填模型
      const modelsToCreate = [
        ...formData.value.selected_models,
        ...formData.value.extra_models
      ]
      if (modelsToCreate.length > 0) {
        const results = await Promise.allSettled(
          modelsToCreate.map(name =>
            createModel({
              name,
              provider_id: created.id,
              model_type: 'chat'
            })
          )
        )
        const failed = results.filter(r => r.status === 'rejected').length
        if (failed > 0) {
          ElMessage.warning(`渠道创建成功，但有 ${failed} 个模型注册失败`)
        } else {
          ElMessage.success(`创建成功，已注册 ${modelsToCreate.length} 个模型`)
        }
      } else {
        ElMessage.success('创建成功')
      }
    }
    emit('saved')
  } catch {
    // 错误已在全局拦截器统一处理
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.form-hint {
  font-size: 12px;
  color: #98989D;
  margin-top: 4px;
}

.tag-input-wrap {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  min-height: 32px;
  padding: 4px 6px;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  width: 100%;
  box-sizing: border-box;
  cursor: text;
  transition: border-color 0.2s;
}

.tag-input-wrap:focus-within {
  border-color: var(--el-color-primary);
}

.tag-item {
  flex-shrink: 0;
}

.tag-input {
  width: 120px;
  flex-shrink: 0;
}

.tag-input :deep(.el-input__wrapper) {
  box-shadow: none !important;
  padding: 0 4px;
}

.tag-add-btn {
  border: 1px dashed var(--el-border-color);
  background: transparent;
  color: var(--el-text-color-secondary);
}

.tag-add-btn:hover {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}

.form-hint.inline {
  margin-top: 0;
  margin-left: 12px;
}

.models-prefill {
  padding: 8px 0;
}

.extra-model-input {
  display: flex;
  align-items: center;
  gap: 8px;
}

.extra-model-field {
  width: 280px;
}

.extra-model-tags {
  margin-top: 8px;
}

.extra-model-tag {
  margin-right: 6px;
  margin-bottom: 4px;
}

.prefill-model-item {
  display: block;
  margin-bottom: 6px;
}

.el-divider {
  margin: 16px 0;
  border-color: #2C2C2E;
}

.el-divider__text {
  color: #98989D;
  font-size: 13px;
}
</style>
