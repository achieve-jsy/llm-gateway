<template>
  <el-dialog
    :model-value="visible"
    :title="isEdit ? '编辑模型' : '注册模型'"
    width="580px"
    destroy-on-close
    @close="emit('close')"
  >
    <el-form
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-width="110px"
      label-position="left"
    >
      <el-form-item label="模型名称" prop="name">
        <el-input
          v-model="formData.name"
          placeholder="如 gpt-4o, qwen-turbo"
          :disabled="isEdit"
          @input="debouncedCheckName"
        />
      </el-form-item>

      <el-form-item label="上游模型ID" prop="upstream_model_id">
        <el-input
          v-model="formData.upstream_model_id"
          placeholder="留空则使用模型名原样转发"
        />
      </el-form-item>

      <el-form-item label="归属渠道" prop="provider_id">
        <el-select
          v-model="formData.provider_id"
          placeholder="请选择渠道"
          style="width: 100%"
        >
          <el-option
            v-for="p in providers"
            :key="p.id"
            :label="p.name"
            :value="p.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="模型类型" prop="model_type">
        <el-select
          v-model="formData.model_type"
          placeholder="请选择模型类型"
          style="width: 100%"
        >
          <el-option label="Chat（对话）" value="chat" />
          <el-option label="Embedding（向量）" value="embedding" />
        </el-select>
      </el-form-item>

      <el-form-item label="模型标签">
        <el-select
          v-model="selectedTagIds"
          multiple
          placeholder="选择标签（可多选）"
          style="width: 100%"
        >
          <el-option
            v-for="tag in availableTags"
            :key="tag.id"
            :label="tag.name"
            :value="tag.id"
          >
            <span class="tag-option">
              <span class="tag-option-dot" :style="{ backgroundColor: tag.color }"></span>
              {{ tag.name }}
            </span>
          </el-option>
        </el-select>
      </el-form-item>

      <el-form-item label="描述">
        <el-input
          v-model="formData.description"
          type="textarea"
          :rows="2"
          placeholder="模型描述（可选）"
        />
      </el-form-item>

      <el-divider content-position="left">参数配置</el-divider>

      <el-form-item label="上下文长度">
        <el-input-number
          v-model="formData.context_length"
          :min="0"
          :max="10000000"
          :step="1024"
          style="width: 200px"
        />
        <span class="form-hint inline">tokens</span>
      </el-form-item>

      <el-form-item label="最大输出">
        <el-input-number
          v-model="formData.max_output_tokens"
          :min="0"
          :max="1000000"
          :step="256"
          style="width: 200px"
        />
        <span class="form-hint inline">tokens</span>
      </el-form-item>

      <el-form-item label="输入价格">
        <el-input-number
          v-model="formData.input_price"
          :min="0"
          :precision="6"
          :step="0.1"
          style="width: 200px"
        />
        <span class="form-hint inline">USD / 百万 tokens</span>
      </el-form-item>

      <el-form-item label="输出价格">
        <el-input-number
          v-model="formData.output_price"
          :min="0"
          :precision="6"
          :step="0.1"
          style="width: 200px"
        />
        <span class="form-hint inline">USD / 百万 tokens</span>
      </el-form-item>

      <el-form-item label="权重">
        <el-input-number
          v-model="formData.weight"
          :min="0"
          :max="9999"
          style="width: 200px"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        确定
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { createModel, updateModel, checkModelNameUnique, getModelTags, getModelTagsForModel, setModelTags } from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'
import type { Model, ModelPayload } from '@/types/model'
import type { Provider } from '@/types/provider'
import type { ModelTag } from '@/types/model-tag'

interface FormState {
  name: string
  upstream_model_id: string
  provider_id: string
  model_type: 'chat' | 'embedding'
  description: string
  context_length: number
  max_output_tokens: number
  input_price: number
  output_price: number
  weight: number
}

const props = defineProps<{
  visible: boolean
  model: Model | null
  providers: Provider[]
}>()

const emit = defineEmits<{
  close: []
  saved: []
  'update:visible': [val: boolean]
}>()

const formRef = ref<FormInstance>()
const submitting = ref(false)
const nameConflict = ref(false)
const availableTags = ref<ModelTag[]>([])
const selectedTagIds = ref<string[]>([])

const isEdit = computed(() => !!props.model)

const formData = ref<FormState>({
  name: '',
  upstream_model_id: '',
  provider_id: '',
  model_type: 'chat',
  description: '',
  context_length: 0,
  max_output_tokens: 0,
  input_price: 0,
  output_price: 0,
  weight: 1
})

/** 监听 model 变化，回填表单 */
watch(
  () => props.model,
  async (val) => {
    loadAvailableTags()
    if (val) {
      formData.value = {
        name: val.name,
        upstream_model_id: val.upstream_model_id || '',
        provider_id: val.provider_id,
        model_type: (val.model_type as 'chat' | 'embedding') || 'chat',
        description: val.description || '',
        context_length: val.context_length || 0,
        max_output_tokens: val.max_output_tokens || 0,
        input_price: val.input_price || 0,
        output_price: val.output_price || 0,
        weight: val.weight || 1
      }
      try {
        const tags = await getModelTagsForModel(val.id)
        selectedTagIds.value = tags.map(t => t.id)
      } catch {
        selectedTagIds.value = []
      }
    } else {
      formData.value = {
        name: '',
        upstream_model_id: '',
        provider_id: '',
        model_type: 'chat',
        description: '',
        context_length: 0,
        max_output_tokens: 0,
        input_price: 0,
        output_price: 0,
        weight: 1
      }
      selectedTagIds.value = []
    }
    nameConflict.value = false
  },
  { immediate: true }
)

/** 加载可选标签 */
async function loadAvailableTags() {
  try {
    availableTags.value = await getModelTags()
  } catch {
    availableTags.value = []
  }
}

/** 防抖定时器 */
let checkTimer: ReturnType<typeof setTimeout> | null = null

/** 防抖检查模型名称唯一性 */
function debouncedCheckName() {
  nameConflict.value = false
  if (checkTimer) clearTimeout(checkTimer)
  if (!formData.value.name || isEdit.value) return

  checkTimer = setTimeout(async () => {
    try {
      const available = await checkModelNameUnique(formData.value.name)
      nameConflict.value = !available
      formRef.value?.validateField('name')
    } catch {
      // 检查失败不阻塞
    }
  }, 300)
}

/** 名称唯一性校验 */
function validateName(_rule: any, value: string, callback: (err?: Error) => void) {
  if (!value) {
    callback(new Error('请输入模型名称'))
    return
  }
  if (nameConflict.value) {
    callback(new Error('模型名称已存在'))
    return
  }
  callback()
}

const rules: FormRules = {
  name: [{ required: true, validator: validateName, trigger: 'blur' }],
  provider_id: [{ required: true, message: '请选择归属渠道', trigger: 'change' }],
  model_type: [{ required: true, message: '请选择模型类型', trigger: 'change' }]
}

/** 提交表单 */
async function handleSubmit() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload: ModelPayload = {
      name: formData.value.name,
      provider_id: formData.value.provider_id,
      model_type: formData.value.model_type,
      upstream_model_id: formData.value.upstream_model_id || undefined,
      description: formData.value.description || undefined,
      context_length: formData.value.context_length || undefined,
      max_output_tokens: formData.value.max_output_tokens || undefined,
      input_price: formData.value.input_price || undefined,
      output_price: formData.value.output_price || undefined,
      weight: formData.value.weight
    }

    let modelId: string
    if (isEdit.value && props.model) {
      await updateModel(props.model.id, payload)
      modelId = props.model.id
    } else {
      const created = await createModel(payload)
      modelId = created.id
    }

    // 保存标签关联
    await setModelTags(modelId, selectedTagIds.value)

    ElMessage.success(isEdit.value ? '更新成功' : '注册成功')
    emit('saved')
  } catch (e: unknown) {
    const friendly = resolveErrorMessage(e)
    ElMessage.error(friendly ?? (isEdit.value ? '更新失败' : '注册失败'))
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

.form-hint.inline {
  margin-top: 0;
  margin-left: 8px;
}

.el-divider {
  margin: 20px 0 16px;
  border-color: #2C2C2E;
}

.el-divider__text {
  color: #98989D;
  font-size: 13px;
}

.tag-option {
  display: flex;
  align-items: center;
}

.tag-option-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  margin-right: 8px;
  flex-shrink: 0;
}
</style>
