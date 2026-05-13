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
      <el-form-item label="上游模型ID" prop="name">
        <el-input
          v-model="formData.name"
          placeholder="如 gpt-4o, qwen-turbo"
          @input="onNameInput"
        />
        <!-- Catalog 联想填充建议下拉 -->
        <div
          v-if="!isEdit && catalogSuggestions.length"
          class="catalog-suggestions"
          data-testid="catalog-suggestions"
        >
          <div
            v-for="item in catalogSuggestions"
            :key="item.id"
            class="catalog-suggestion-item"
            @click="applyCatalogSelection(item)"
          >
            <span class="cs-id">{{ item.source_model_id }}</span>
            <span class="cs-meta">
              · {{ formatCtxK(item.context_length) }}K
              · ${{ item.input_price_per_million }}/${{ item.output_price_per_million }}
              <template v-if="item.cache_read_price != null || item.cache_write_price != null">
                · cache ${{ item.cache_read_price ?? '-' }}/${{ item.cache_write_price ?? '-' }}
              </template>
            </span>
            <span class="cs-icons">
              <span v-if="item.supports_function_calling" title="function calling">🛠</span>
              <span v-if="item.supports_vision" title="vision">🖼</span>
              <span v-if="item.supports_prompt_caching" title="prompt caching">💾</span>
              <span v-if="item.supports_response_schema" title="response schema">📋</span>
            </span>
          </div>
        </div>
        <div v-if="catalogLinked" class="catalog-linked-hint">
          🔗 已关联 catalog · 修改字段将自动脱离目录
        </div>
      </el-form-item>

      <el-form-item label="模型名称" prop="upstream_model_id">
        <el-input
          v-model="formData.upstream_model_id"
          placeholder="留空则使用上游模型ID原样转发"
        />
        <div
          v-if="selectedProviderDefinitionId === 'azure'"
          class="form-hint"
          style="margin-top: 4px; color: var(--el-text-color-secondary); font-size: 12px;"
        >
          Azure 渠道：此处必须填 Azure <b>部署名</b>（如 gpt-5.5）。填写 azure/us/gpt-5.5 等前缀会导致 DeploymentNotFound。
        </div>
      </el-form-item>

      <el-form-item v-if="isEdit" label="归属渠道" prop="provider_id">
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
      <el-form-item v-else label="归属渠道" prop="provider_ids">
        <el-select
          v-model="formData.provider_ids"
          multiple
          filterable
          collapse-tags
          collapse-tags-tooltip
          placeholder="请选择一个或多个渠道"
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
          <el-option label="Embedding-VL（多模态向量）" value="embedding_vl" />
          <el-option label="Rerank（重排序）" value="rerank" />
          <el-option label="Rerank-VL（多模态重排序）" value="rerank_vl" />
          <el-option label="🎬 Video（视频生成）" value="video" />
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

      <!-- CAT-2: 目录绑定区块（仅编辑模式显示；新建模式由顶部联想完成绑定） -->
      <template v-if="isEdit">
        <el-divider content-position="left">目录绑定</el-divider>
        <el-form-item label="目录条目">
          <div v-if="boundCatalogLabel" class="catalog-binding-row" data-testid="catalog-binding">
            <span class="catalog-binding-name">
              <span class="catalog-binding-icon">🔗</span>
              {{ boundCatalogLabel }}
            </span>
            <el-button
              size="small"
              data-testid="btn-switch-catalog"
              @click="openCatalogPicker"
            >
              切换
            </el-button>
            <el-button
              size="small"
              type="danger"
              plain
              data-testid="btn-unbind-catalog"
              :loading="unbinding"
              @click="handleUnbind"
            >
              解绑
            </el-button>
          </div>
          <div v-else class="catalog-binding-row" data-testid="catalog-binding">
            <span class="catalog-binding-empty">未关联任何目录条目</span>
            <el-button
              size="small"
              data-testid="btn-pick-catalog"
              @click="openCatalogPicker"
            >
              选择目录条目
            </el-button>
          </div>
        </el-form-item>
      </template>

      <el-divider content-position="left">参数配置</el-divider>

      <el-form-item label="上下文长度" v-if="formData.model_type === 'chat'">
        <el-input-number
          v-model="formData.context_length"
          :min="0"
          :max="10000000"
          :step="1024"
          class="metric-input"
        />
        <span class="form-hint inline">tokens</span>
      </el-form-item>

      <el-form-item label="最大输出" v-if="formData.model_type === 'chat'">
        <el-input-number
          v-model="formData.max_output_tokens"
          :min="0"
          :max="1000000"
          :step="256"
          class="metric-input"
        />
        <span class="form-hint inline">tokens</span>
      </el-form-item>

      <template v-if="formData.model_type === 'video'">
        <el-form-item label="分辨率">
          <el-select v-model="formData.video_supported_resolutions" multiple placeholder="多选" style="width: 100%">
            <el-option v-for="r in VIDEO_RESOLUTIONS" :key="r" :label="r" :value="r" />
          </el-select>
        </el-form-item>
        <el-form-item label="宽高比">
          <el-select v-model="formData.video_supported_aspect_ratios" multiple placeholder="多选" style="width: 100%">
            <el-option v-for="r in VIDEO_ASPECT_RATIOS" :key="r" :label="r" :value="r" />
          </el-select>
        </el-form-item>
        <el-form-item label="时长范围（秒）">
          <div class="duration-range">
            <el-input-number v-model="formData.video_min_duration_seconds" :min="1" :max="600" />
            <span class="form-hint inline">~</span>
            <el-input-number v-model="formData.video_max_duration_seconds" :min="1" :max="600" />
          </div>
          <div class="form-hint">常见 Seedance 范围为 4～15 秒</div>
        </el-form-item>
        <el-form-item label="支持场景">
          <el-select v-model="formData.video_supported_actions" multiple placeholder="多选" style="width: 100%">
            <el-option
              v-for="a in VIDEO_ACTIONS"
              :key="a.value"
              :label="a.label"
              :value="a.value"
            />
          </el-select>
        </el-form-item>
      </template>

      <!-- Video：按任务计费且单渠道固定路由，不展示 Token 价格 / 负载权重（对齐原型） -->
      <template v-if="formData.model_type !== 'video'">
        <el-form-item label="价格模式">
          <el-radio-group v-model="formData.price_mode">
            <el-radio value="catalog">跟随目录价格</el-radio>
            <el-radio value="manual">手动指定</el-radio>
          </el-radio-group>
          <span v-if="formData.price_mode === 'catalog'" class="form-hint inline" style="margin-left:8px">
            实时读取 Catalog 价格，无需手动维护
          </span>
        </el-form-item>

        <el-form-item label="输入价格">
          <el-input-number
            v-model="formData.input_price"
            :min="0"
            :precision="6"
            :step="0.1"
            :disabled="formData.price_mode === 'catalog'"
            class="metric-input"
          />
          <span class="form-hint inline">USD / 百万 tokens</span>
        </el-form-item>

        <el-form-item label="输出价格">
          <el-input-number
            v-model="formData.output_price"
            :min="0"
            :precision="6"
            :step="0.1"
            :disabled="formData.price_mode === 'catalog'"
            class="metric-input"
          />
          <span class="form-hint inline">USD / 百万 tokens</span>
        </el-form-item>

        <el-form-item label="缓存读价格">
          <el-input-number
            v-model="formData.cache_read_price"
            :min="0"
            :precision="6"
            :step="0.1"
            :disabled="formData.price_mode === 'catalog'"
            class="metric-input"
          />
          <span class="form-hint inline">可选；为空按现有逻辑</span>
        </el-form-item>

        <el-form-item label="缓存写价格">
          <el-input-number
            v-model="formData.cache_write_price"
            :min="0"
            :precision="6"
            :step="0.1"
            :disabled="formData.price_mode === 'catalog'"
            class="metric-input"
          />
          <span class="form-hint inline">可选；为空按现有逻辑</span>
        </el-form-item>

        <el-form-item label="权重">
          <el-input-number
            v-model="formData.weight"
            :min="0"
            :max="9999"
            class="metric-input"
          />
        </el-form-item>
      </template>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        确定
      </el-button>
    </template>

    <!-- CAT-2: catalog picker 子 dialog -->
    <el-dialog
      v-model="catalogPickerVisible"
      title="选择目录条目"
      width="540px"
      append-to-body
      :destroy-on-close="true"
      data-testid="catalog-picker"
    >
      <el-input
        v-model="pickerKeyword"
        placeholder="输入关键字搜索目录条目（≥2 字符）"
        clearable
        data-testid="picker-input"
        @input="debouncedPickerSearch"
      />
      <div class="picker-results">
        <div v-if="pickerLoading" class="picker-empty">搜索中...</div>
        <div v-else-if="!pickerResults.length && pickerKeyword.length >= 2" class="picker-empty">
          没有匹配的目录条目
        </div>
        <div v-else-if="!pickerResults.length" class="picker-empty">
          请输入关键字开始搜索
        </div>
        <div
          v-for="item in pickerResults"
          :key="item.id"
          class="picker-item"
          :class="{ active: pickerSelectedId === item.id }"
          data-testid="picker-item"
          @click="pickerSelectedId = item.id"
        >
          <div class="picker-id">{{ item.source_model_id }}</div>
          <div class="picker-meta">
            {{ item.provider || '-' }} · {{ formatCtxK(item.context_length) }}K ·
            ${{ item.input_price_per_million }}/${{ item.output_price_per_million }}
            <template v-if="item.cache_read_price != null || item.cache_write_price != null">
              · cache ${{ item.cache_read_price ?? '-' }}/${{ item.cache_write_price ?? '-' }}
            </template>
          </div>
        </div>
      </div>
      <el-checkbox
        v-model="pickerResetFields"
        data-testid="picker-reset-fields"
        style="margin-top: 12px"
      >
        同时把价格、上下文长度、最大输出、上游模型 ID 重置为新目录的当前值
      </el-checkbox>
      <div class="picker-hint">
        <span v-if="pickerResetFields">⚠ 当前 4 个字段将被覆盖；本地任何手工调整都会丢失</span>
        <span v-else>仅切换关联，不动本地价格 / 容量字段（diff 列表会立刻把这条标记为已 override）</span>
      </div>
      <template #footer>
        <el-button data-testid="picker-cancel" @click="catalogPickerVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :disabled="!pickerSelectedId"
          :loading="binding"
          data-testid="picker-confirm"
          @click="handleBindConfirm"
        >
          确认绑定
        </el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  updateModel,
  checkModelNameUnique,
  getModelTags,
  getModelTagsForModel,
  setModelTags,
  batchSetModelTags,
  searchCatalog,
  batchCreateModels,
  batchCreateModelsFromCatalog,
  bindModelCatalog,
  unbindModelCatalog
} from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'
import type { Model, ModelPayload, BatchCreateModelsPayload } from '@/types/model'
import type { Provider } from '@/types/provider'
import type { ModelTag } from '@/types/model-tag'
import type { BatchCreateModelsResult, ModelCatalog } from '@/types/catalog'

const VIDEO_RESOLUTIONS = ['720p', '480p', '1080p', '4K'] as const
const VIDEO_ASPECT_RATIOS = ['16:9', '9:16', '1:1', '4:3', '3:4', '21:9', 'adaptive'] as const
const VIDEO_ACTIONS = [
  { value: 'text_to_video', label: '文生视频' },
  { value: 'image_to_video', label: '图生视频' },
  { value: 'multimodal_reference', label: '多模态参考' },
  { value: 'web_search', label: '联网搜索' },
] as const

interface FormState {
  name: string
  upstream_model_id: string
  provider_id: string
  /** 注册态多选渠道；编辑态忽略 */
  provider_ids: string[]
  model_type: 'chat' | 'embedding' | 'embedding_vl' | 'rerank' | 'rerank_vl' | 'video'
  description: string
  context_length: number
  max_output_tokens: number
  price_mode: 'catalog' | 'manual'
  input_price: number
  output_price: number
  cache_read_price: number | null
  cache_write_price: number | null
  weight: number
  video_supported_resolutions: string[]
  video_supported_aspect_ratios: string[]
  video_min_duration_seconds: number
  video_max_duration_seconds: number
  video_supported_actions: string[]
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

/** 已选 catalog 项；非 null 表示提交时走 createModelFromCatalog 路径 */
const selectedCatalogId = ref<string | null>(null)
/** 选中 catalog 时缓存的原始数值，用于检测"用户改了字段 → 脱离目录" */
const catalogBaseline = ref<{ context_length: number; max_output_tokens: number; input_price: number; output_price: number; upstream_model_id: string } | null>(null)
const catalogSuggestions = ref<ModelCatalog[]>([])
/** 已脱离目录的字段集合（防止重复弹提示） */
const detachedFields = ref<Set<string>>(new Set())
/** 内部标记：true 时 watcher 跳过脱离检测（用于程序化填充） */
const applyingCatalog = ref(false)

const isEdit = computed(() => !!props.model)
const catalogLinked = computed(() => !!selectedCatalogId.value)
/** 注册态取首个已选渠道，用于 Azure 提示 / catalog 联想过滤 */
const primaryProviderId = computed(() => {
  if (isEdit.value) return formData.value.provider_id
  return formData.value.provider_ids[0] || ''
})

const selectedProviderDefinitionId = computed(() => {
  const p = props.providers.find((pr) => pr.id === primaryProviderId.value)
  return (p as { definition_id?: string } | undefined)?.definition_id || ''
})

function providerName(id: string): string {
  return props.providers.find((p) => p.id === id)?.name || id
}

function formatBatchCreateSummary(res: BatchCreateModelsResult): string {
  const lines = [`注册完成：成功 ${res.success_count}，跳过 ${res.skip_count}，失败 ${res.fail_count}`]
  for (const item of res.results) {
    if (item.status === 'created') continue
    const label = providerName(item.provider_id)
    lines.push(`${label}：${item.status}${item.reason ? `（${item.reason}）` : ''}`)
  }
  return lines.join('\n')
}

function formatCtxK(n: number): number {
  return Math.round(n / 1000)
}

const formData = ref<FormState>({
  name: '',
  upstream_model_id: '',
  provider_id: '',
  provider_ids: [],
  model_type: 'chat',
  description: '',
  context_length: 0,
  max_output_tokens: 0,
  price_mode: 'catalog',
  input_price: 0,
  output_price: 0,
  cache_read_price: null,
  cache_write_price: null,
  weight: 1,
  video_supported_resolutions: ['720p', '480p'],
  video_supported_aspect_ratios: ['16:9', '9:16', '1:1'],
  video_min_duration_seconds: 4,
  video_max_duration_seconds: 15,
  video_supported_actions: ['text_to_video', 'image_to_video', 'multimodal_reference'],
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
        provider_ids: val.provider_id ? [val.provider_id] : [],
        model_type: (val.model_type as FormState['model_type']) || 'chat',
        description: val.description || '',
        context_length: val.context_length || 0,
        max_output_tokens: val.max_output_tokens || 0,
        price_mode: val.price_mode || 'catalog',
        input_price: val.input_price || 0,
        output_price: val.output_price || 0,
        cache_read_price: val.cache_read_price ?? null,
        cache_write_price: val.cache_write_price ?? null,
        weight: val.weight || 1,
        video_supported_resolutions: val.video_supported_resolutions || ['720p', '480p'],
        video_supported_aspect_ratios: val.video_supported_aspect_ratios || ['16:9', '9:16', '1:1'],
        video_min_duration_seconds: val.video_min_duration_seconds || 4,
        video_max_duration_seconds: val.video_max_duration_seconds || 15,
        video_supported_actions: val.video_supported_actions || ['text_to_video', 'image_to_video', 'multimodal_reference'],
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
        provider_ids: [],
        model_type: 'chat',
        description: '',
        context_length: 0,
        max_output_tokens: 0,
        price_mode: 'catalog',
        input_price: 0,
        output_price: 0,
        cache_read_price: null,
        cache_write_price: null,
        weight: 1,
        video_supported_resolutions: ['720p', '480p'],
        video_supported_aspect_ratios: ['16:9', '9:16', '1:1'],
        video_min_duration_seconds: 4,
        video_max_duration_seconds: 15,
        video_supported_actions: ['text_to_video', 'image_to_video', 'multimodal_reference'],
      }
      selectedTagIds.value = []
    }
    nameConflict.value = false
    selectedCatalogId.value = null
    catalogBaseline.value = null
    catalogSuggestions.value = []
    detachedFields.value = new Set()
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
let catalogSearchTimer: ReturnType<typeof setTimeout> | null = null

/** 防抖检查模型名称唯一性（仅单渠道时乐观提示；多渠道由批量接口按条 skip） */
function debouncedCheckName() {
  nameConflict.value = false
  if (checkTimer) clearTimeout(checkTimer)
  if (!formData.value.name || isEdit.value) return
  if (formData.value.provider_ids.length !== 1) return

  checkTimer = setTimeout(async () => {
    try {
      const available = await checkModelNameUnique(
        formData.value.name,
        formData.value.provider_ids[0]
      )
      nameConflict.value = !available
      formRef.value?.validateField('name')
    } catch {
      // 检查失败不阻塞
    }
  }, 300)
}

/** 防抖触发 catalog 联想搜索（≥2 字符） */
function debouncedSearchCatalog() {
  if (catalogSearchTimer) clearTimeout(catalogSearchTimer)
  if (isEdit.value) return
  const q = formData.value.name.trim()
  if (q.length < 2) {
    catalogSuggestions.value = []
    return
  }
  catalogSearchTimer = setTimeout(async () => {
    try {
      const provider = primaryProviderId.value
        ? findProviderCatalogKey(primaryProviderId.value)
        : undefined
      const result = await searchCatalog({
        q,
        ...(provider ? { provider } : {}),
        limit: 10
      })
      catalogSuggestions.value = result.data
    } catch {
      catalogSuggestions.value = []
    }
  }, 300)
}

/** 把 ProviderId 映射到 catalog 中的 provider 字段（如 definition_id），找不到时返回 undefined */
function findProviderCatalogKey(providerId: string): string | undefined {
  const p = props.providers.find((pr) => pr.id === providerId)
  // 使用渠道的 definition_id（如 'openai' / 'anthropic'）作为 catalog 过滤条件
  return (p as any)?.definition_id || undefined
}

/** 名称输入：同时触发唯一性检查 + catalog 联想 */
function onNameInput() {
  debouncedCheckName()
  debouncedSearchCatalog()
}

/**
 * 选中一条 catalog 建议：自动填充 5 个字段并记录 catalog_id。
 * 使用 `nextTick` 在 Vue 把同步赋值触发的 watcher 都消化掉之后，再
 * 把 `applyingCatalog` 改回 false——这样手工后续的字段修改才会被
 * 检测为"脱离目录"。
 */
async function applyCatalogSelection(item: ModelCatalog) {
  applyingCatalog.value = true
  selectedCatalogId.value = item.id
  formData.value.upstream_model_id = item.source_model_id
  formData.value.context_length = item.context_length
  formData.value.max_output_tokens = item.max_output_tokens
  formData.value.input_price = item.input_price_per_million
  formData.value.output_price = item.output_price_per_million
  catalogBaseline.value = {
    context_length: item.context_length,
    max_output_tokens: item.max_output_tokens,
    input_price: item.input_price_per_million,
    output_price: item.output_price_per_million,
    upstream_model_id: item.source_model_id
  }
  detachedFields.value = new Set()
  catalogSuggestions.value = []
  await nextTick()
  applyingCatalog.value = false
}

/** 当 catalog 已关联且某字段被改动时，弹一次"脱离目录"提示 */
function notifyDetach(field: string) {
  if (!selectedCatalogId.value) return
  if (applyingCatalog.value) return
  if (detachedFields.value.has(field)) return
  detachedFields.value.add(field)
  ElMessage.warning('此字段将不再随目录自动更新（可在详情页恢复）')
}

watch(() => formData.value.context_length, (val) => {
  if (catalogBaseline.value && val !== catalogBaseline.value.context_length) {
    notifyDetach('context_length')
  }
})

watch(() => formData.value.max_output_tokens, (val) => {
  if (catalogBaseline.value && val !== catalogBaseline.value.max_output_tokens) {
    notifyDetach('max_output_tokens')
  }
})

watch(() => formData.value.input_price, (val) => {
  if (catalogBaseline.value && val !== catalogBaseline.value.input_price) {
    notifyDetach('input_price')
  }
})

watch(() => formData.value.output_price, (val) => {
  if (catalogBaseline.value && val !== catalogBaseline.value.output_price) {
    notifyDetach('output_price')
  }
})

// ===================== CAT-2: 编辑模式的目录绑定/换绑/解绑 =====================
//
// 设计要点（与方案 D1=C / D2=A / D3=B 对齐）：
//   - 仅在 isEdit 模式显示「目录绑定」区块；新建模式由顶部联想已经处理过；
//   - boundCatalogLabel 优先取嵌套展开的 catalog snapshot，回退到 catalog_id 短串；
//   - picker 用 searchCatalog 联想（与新建联想共享后端接口），admin 选中条目并勾选 reset_fields；
//   - 「解绑」走 ElMessageBox 二次确认，避免误操作（解绑后 diff 列表里这条会立刻消失）。

const catalogPickerVisible = ref(false)
const pickerKeyword = ref('')
const pickerResults = ref<ModelCatalog[]>([])
const pickerSelectedId = ref<string | null>(null)
const pickerResetFields = ref(false)
const pickerLoading = ref(false)
const binding = ref(false)
const unbinding = ref(false)
let pickerSearchTimer: ReturnType<typeof setTimeout> | null = null

/** 当前已绑定的 catalog 显示名（优先 source_model_id，回退到截短的 catalog_id） */
const boundCatalogLabel = computed<string>(() => {
  const m = props.model
  if (!m) return ''
  if (m.catalog?.source_model_id) {
    return `${m.catalog.source || 'litellm'} / ${m.catalog.source_model_id}`
  }
  if (m.catalog_id) {
    return `catalog_id: ${m.catalog_id.slice(0, 8)}…`
  }
  return ''
})

function openCatalogPicker() {
  catalogPickerVisible.value = true
  pickerKeyword.value = ''
  pickerResults.value = []
  pickerSelectedId.value = null
  pickerResetFields.value = false
  // 默认建议值：用模型当前 name 作为起手关键字（admin 大概率想绑同名）
  if (props.model?.name) {
    pickerKeyword.value = props.model.name
    debouncedPickerSearch()
  }
}

function debouncedPickerSearch() {
  if (pickerSearchTimer) clearTimeout(pickerSearchTimer)
  pickerSelectedId.value = null
  const q = pickerKeyword.value.trim()
  if (q.length < 2) {
    pickerResults.value = []
    return
  }
  pickerLoading.value = true
  pickerSearchTimer = setTimeout(async () => {
    try {
      const result = await searchCatalog({ q, limit: 20 })
      pickerResults.value = result.data
    } catch {
      pickerResults.value = []
    } finally {
      pickerLoading.value = false
    }
  }, 300)
}

async function handleBindConfirm() {
  if (!props.model || !pickerSelectedId.value) return
  binding.value = true
  try {
    await bindModelCatalog(props.model.id, {
      catalog_id: pickerSelectedId.value,
      reset_fields: pickerResetFields.value
    })
    ElMessage.success(pickerResetFields.value ? '已切换并按目录重置字段' : '已切换目录关联')
    catalogPickerVisible.value = false
    emit('saved')
  } catch (e: unknown) {
    const friendly = resolveErrorMessage(e)
    ElMessage.error(friendly ?? '绑定失败')
  } finally {
    binding.value = false
  }
}

async function handleUnbind() {
  if (!props.model) return
  try {
    await ElMessageBox.confirm(
      '解绑后该模型不再随目录自动更新；当前价格/容量将作为 snapshot 保留。是否继续？',
      '解除目录关联',
      { type: 'warning', confirmButtonText: '解除关联', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  unbinding.value = true
  try {
    await unbindModelCatalog(props.model.id)
    ElMessage.success('已解除目录关联')
    emit('saved')
  } catch (e: unknown) {
    const friendly = resolveErrorMessage(e)
    ElMessage.error(friendly ?? '解绑失败')
  } finally {
    unbinding.value = false
  }
}

defineExpose({ applyCatalogSelection, openCatalogPicker, handleUnbind })

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

function validateProviderIds(_rule: unknown, value: string[], callback: (err?: Error) => void) {
  if (!value || value.length === 0) {
    callback(new Error('请至少选择一个归属渠道'))
    return
  }
  callback()
}

const rules: FormRules = {
  name: [{ required: true, validator: validateName, trigger: 'blur' }],
  provider_id: [{ required: true, message: '请选择归属渠道', trigger: 'change' }],
  provider_ids: [{ required: true, validator: validateProviderIds, trigger: 'change' }],
  model_type: [{ required: true, message: '请选择模型类型', trigger: 'change' }]
}

/** 提交表单 */
async function handleSubmit() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const isVideo = formData.value.model_type === 'video'
    const sharedFields = {
      name: formData.value.name,
      model_type: formData.value.model_type,
      upstream_model_id: formData.value.upstream_model_id || undefined,
      description: formData.value.description || undefined,
      context_length: isVideo ? undefined : (formData.value.context_length || undefined),
      max_output_tokens: isVideo ? undefined : (formData.value.max_output_tokens || undefined),
      // Video 本期不计费、无负载权重；保留默认 weight=1 满足后端字段，不提交 Token 价
      price_mode: isVideo ? 'manual' as const : formData.value.price_mode,
      input_price: isVideo ? undefined : (formData.value.price_mode === 'manual' ? (formData.value.input_price || undefined) : undefined),
      output_price: isVideo ? undefined : (formData.value.price_mode === 'manual' ? (formData.value.output_price || undefined) : undefined),
      cache_read_price: isVideo ? undefined : (formData.value.price_mode === 'manual' ? formData.value.cache_read_price : null),
      cache_write_price: isVideo ? undefined : (formData.value.price_mode === 'manual' ? formData.value.cache_write_price : null),
      weight: isVideo ? 1 : formData.value.weight,
      ...(isVideo ? {
        video_supported_resolutions: formData.value.video_supported_resolutions,
        video_supported_aspect_ratios: formData.value.video_supported_aspect_ratios,
        video_min_duration_seconds: formData.value.video_min_duration_seconds,
        video_max_duration_seconds: formData.value.video_max_duration_seconds,
        video_supported_actions: formData.value.video_supported_actions,
      } : {}),
    }

    if (isEdit.value && props.model) {
      const payload: ModelPayload = {
        ...sharedFields,
        provider_id: formData.value.provider_id,
      }
      await updateModel(props.model.id, payload)
      await setModelTags(props.model.id, selectedTagIds.value)
      ElMessage.success('更新成功')
      emit('saved')
      return
    }

    let res: BatchCreateModelsResult
    if (selectedCatalogId.value) {
      res = await batchCreateModelsFromCatalog({
        catalog_id: selectedCatalogId.value,
        provider_ids: formData.value.provider_ids,
        name: formData.value.name,
      })
    } else {
      const payload: BatchCreateModelsPayload = {
        ...sharedFields,
        provider_ids: formData.value.provider_ids,
      }
      res = await batchCreateModels(payload)
    }

    const createdIds = res.results
      .filter((r) => r.status === 'created' && r.model_id)
      .map((r) => r.model_id!)
    if (createdIds.length > 0 && selectedTagIds.value.length > 0) {
      await batchSetModelTags(createdIds, selectedTagIds.value)
    }

    if (res.fail_count === 0 && res.skip_count === 0) {
      ElMessage.success(res.success_count > 1 ? `已注册到 ${res.success_count} 个渠道` : '注册成功')
    } else {
      ElMessage.warning(formatBatchCreateSummary(res))
    }
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

.duration-range {
  display: flex;
  align-items: center;
  gap: 8px;
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

.catalog-suggestions {
  margin-top: 4px;
  border: 1px solid #2C2C2E;
  border-radius: 6px;
  max-height: 220px;
  overflow-y: auto;
  background: #1C1C1E;
}

.metric-input {
  width: 156px;
}

.catalog-suggestion-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  cursor: pointer;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.catalog-suggestion-item:hover {
  background: #2C2C2E;
}

.cs-id {
  font-family: monospace;
  font-weight: 500;
}

.cs-meta {
  color: #98989D;
}

.cs-icons {
  margin-left: auto;
  display: inline-flex;
  gap: 4px;
}

.catalog-linked-hint {
  margin-top: 4px;
  font-size: 12px;
  color: #98989D;
}

.catalog-binding-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
}

.catalog-binding-name {
  flex: 1;
  font-family: monospace;
  font-size: 13px;
  color: var(--el-text-color-primary);
  word-break: break-all;
}

.catalog-binding-icon {
  margin-right: 4px;
}

.catalog-binding-empty {
  flex: 1;
  font-size: 13px;
  color: #98989D;
}

.picker-results {
  margin-top: 8px;
  max-height: 280px;
  overflow-y: auto;
  border: 1px solid #2C2C2E;
  border-radius: 6px;
  background: #1C1C1E;
}

.picker-empty {
  padding: 16px;
  text-align: center;
  color: #98989D;
  font-size: 13px;
}

.picker-item {
  padding: 8px 12px;
  cursor: pointer;
  border-bottom: 1px solid #2C2C2E;
  transition: background 0.1s;
}

.picker-item:last-child {
  border-bottom: none;
}

.picker-item:hover {
  background: #2C2C2E;
}

.picker-item.active {
  background: #0a3d6c;
}

.picker-id {
  font-family: monospace;
  font-weight: 500;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.picker-meta {
  margin-top: 2px;
  font-size: 12px;
  color: #98989D;
}

.picker-hint {
  margin-top: 8px;
  font-size: 12px;
  color: #98989D;
}

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95% !important;
    max-width: 95vw !important;
  }
  :deep(.el-dialog .el-dialog__body) {
    padding: 16px !important;
  }
  .catalog-suggestion-item {
    flex-wrap: wrap;
    gap: 4px;
  }
  .cs-meta {
    width: 100%;
  }
}
</style>
