<template>
  <el-dialog
    :model-value="visible"
    :title="isEdit ? '编辑模型组' : '创建模型组'"
    width="640px"
    destroy-on-close
    @close="emit('close')"
  >
    <el-form
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-width="100px"
      label-position="left"
    >
      <el-form-item label="名称" prop="name">
        <el-input
          v-model="formData.name"
          placeholder="请输入模型组名称"
          maxlength="64"
          show-word-limit
        />
      </el-form-item>

      <el-form-item label="描述">
        <el-input
          v-model="formData.description"
          type="textarea"
          :rows="3"
          placeholder="模型组描述（可选）"
        />
      </el-form-item>

      <el-form-item label="类型" prop="model_type">
        <el-select v-model="formData.model_type" placeholder="请选择模型组类型" @change="handleTypeChange">
          <el-option label="Chat（对话）" value="chat" />
          <el-option label="Embedding（向量）" value="embedding" />
          <el-option label="Embedding VL（多模态向量）" value="embedding_vl" />
          <el-option label="Rerank（重排序）" value="rerank" />
          <el-option label="Rerank VL（多模态重排序）" value="rerank_vl" />
          <el-option label="Video（视频生成）" value="video" />
        </el-select>
        <div class="form-item-tip" style="margin-left:0;margin-top:4px;display:block">
          关联模型将按此类型筛选；Video 仅支持单模型且无负载策略
        </div>
      </el-form-item>

      <template v-if="formData.model_type !== 'video'">
        <el-form-item label="负载策略" prop="strategy">
          <el-radio-group v-model="formData.strategy" @change="handleStrategyChange">
            <el-radio value="round_robin">轮询</el-radio>
            <el-radio value="priority">优先级</el-radio>
            <el-radio value="weighted">加权</el-radio>
            <el-radio value="auto">自动（难度分档）</el-radio>
          </el-radio-group>
        </el-form-item>

        <div v-if="formData.strategy === 'auto'" class="strategy-hint">
          按请求难度自动将任务路由到不同档位的模型。请在每个模型后指定它属于哪个复杂度档位。
        </div>

        <el-divider content-position="left">故障切换配置</el-divider>

        <div v-if="globalDefaults" class="global-defaults-hint">
          全局默认：最大切换 <strong>{{ globalDefaults.max_switches }}</strong> 次，同模型重试 <strong>{{ globalDefaults.same_model_retries }}</strong> 次，冷却 <strong>{{ globalDefaults.failover_cooldown_seconds }}</strong> 秒
        </div>

        <el-form-item label="最大切换次数">
          <el-input-number
            v-model="formData.max_switches"
            :min="1"
            :max="10"
            size="small"
            controls-position="right"
          />
          <span class="form-item-tip">同次请求最多切换几个模型后再报错</span>
        </el-form-item>

        <el-form-item label="同模型重试">
          <el-input-number
            v-model="formData.same_model_retries"
            :min="0"
            :max="5"
            size="small"
            controls-position="right"
          />
          <span class="form-item-tip">同一模型失败后重试几次再换下一个</span>
        </el-form-item>

        <el-form-item label="冷却时间(秒)">
          <el-input-number
            v-model="formData.failover_cooldown_seconds"
            :min="10"
            :max="600"
            size="small"
            controls-position="right"
          />
          <span class="form-item-tip">模型被切换后多久不再参与路由</span>
        </el-form-item>

        <el-form-item label="请求超时(秒)">
          <el-input-number
            v-model="formData.timeout_seconds"
            :min="0"
            :max="600"
            size="small"
            controls-position="right"
          />
          <span class="form-item-tip">0 = 使用系统默认（非流式 30s / 流式 300s）；每轮上游调用独立计时，failover 不受上一轮影响</span>
        </el-form-item>
      </template>

      <el-form-item label="关联模型">
        <div class="model-list-section">
          <div v-if="modelList.length === 0" class="model-empty-hint">
            尚未添加模型，请点击下方按钮添加
          </div>
          <div v-else class="model-items">
            <div v-for="(item, index) in modelList" :key="item.model_id" class="model-item">
              <span class="model-item-name">{{ formatModelLabel(item) }}</span>
              <template v-if="formData.model_type !== 'video' && formData.strategy === 'priority'">
                <el-input-number
                  :model-value="item.priority ?? 0"
                  :min="0"
                  :max="9999"
                  size="small"
                  controls-position="right"
                  style="width: 140px"
                  placeholder="优先级"
                  @update:model-value="(val: number | undefined) => updateModelPriority(index, val ?? 0)"
                />
              </template>
              <template v-if="formData.model_type !== 'video' && formData.strategy === 'weighted'">
                <el-input-number
                  :model-value="item.weight ?? 1"
                  :min="1"
                  :max="100"
                  size="small"
                  controls-position="right"
                  style="width: 140px"
                  placeholder="权重"
                  @update:model-value="(val: number | undefined) => updateModelWeight(index, val ?? 1)"
                />
              </template>
              <template v-if="formData.model_type !== 'video' && formData.strategy === 'auto'">
                <el-select
                  :model-value="item.complexity_tier ?? 'MEDIUM'"
                  size="small"
                  style="width: 160px"
                  placeholder="复杂度档位"
                  @update:model-value="(val: string) => updateModelTier(index, val)"
                >
                  <el-option label="简单 (SIMPLE)" value="SIMPLE" />
                  <el-option label="中等 (MEDIUM)" value="MEDIUM" />
                  <el-option label="复杂 (COMPLEX)" value="COMPLEX" />
                  <el-option label="推理 (REASONING)" value="REASONING" />
                </el-select>
              </template>
              <el-button
                text
                type="danger"
                size="small"
                @click="removeModel(index)"
              >
                移除
              </el-button>
            </div>
          </div>
          <el-button
            style="margin-top: 8px"
            size="small"
            :disabled="formData.model_type === 'video' && modelList.length >= 1"
            @click="selectorVisible = true"
          >
            添加模型
          </el-button>
          <span v-if="formData.model_type === 'video' && modelList.length >= 1" class="form-item-tip">Video 类型仅支持关联一个模型</span>
        </div>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        确定
      </el-button>
    </template>

    <ModelGroupSelector
      v-model:visible="selectorVisible"
      :strategy="formData.strategy"
      :model-type="formData.model_type"
      :existing-model-ids="modelList.map(m => m.model_id)"
      @close="selectorVisible = false"
      @confirm="handleSelectorConfirm"
    />
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage, ElMessageBox } from 'element-plus'
import { groupsApi, type FailoverDefaults } from '@/api/groups'
import type { GroupListItem, Strategy, ComplexityTier, CreateGroupRequest } from '@/types/group'
import ModelGroupSelector from '@/components/ModelGroupSelector.vue'

interface ModelItem {
  model_id: string
  model_name: string
  provider_name?: string
  priority: number | null
  weight: number | null
  complexity_tier?: ComplexityTier
}

function formatModelLabel(item: Pick<ModelItem, 'model_name' | 'provider_name'>) {
  return item.provider_name ? `${item.provider_name}: ${item.model_name}` : item.model_name
}

interface FormState {
  name: string
  description: string
  model_type: string
  strategy: Strategy
  max_switches: number
  same_model_retries: number
  failover_cooldown_seconds: number
  timeout_seconds: number
}

const props = defineProps<{
  visible: boolean
  group: GroupListItem | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
  'update:visible': [val: boolean]
}>()

const formRef = ref<FormInstance>()
const submitting = ref(false)
const selectorVisible = ref(false)
const globalDefaults = ref<FailoverDefaults | null>(null)

const isEdit = computed(() => !!props.group)

const formData = ref<FormState>({
  name: '',
  description: '',
  model_type: 'chat',
  strategy: 'round_robin',
  max_switches: 2,
  same_model_retries: 1,
  failover_cooldown_seconds: 60,
  timeout_seconds: 0,
})

const modelList = ref<ModelItem[]>([])

const originalStrategy = ref<Strategy>('round_robin')
/** 切换类型确认用：取消时回退 */
const prevModelType = ref('chat')

const rules: FormRules = {
  name: [
    { required: true, message: '请输入模型组名称', trigger: 'blur' },
    { max: 64, message: '名称不能超过 64 个字符', trigger: 'blur' },
  ],
  model_type: [{ required: true, message: '请选择模型组类型', trigger: 'change' }],
  strategy: [{ required: true, message: '请选择负载策略', trigger: 'change' }],
}

function applyGroupToForm(g: {
  name: string
  description?: string
  model_type?: string
  strategy: Strategy
  max_switches?: number
  same_model_retries?: number
  failover_cooldown_seconds?: number
  timeout_seconds?: number
}) {
  const mt = g.model_type || 'chat'
  formData.value = {
    name: g.name,
    description: g.description || '',
    model_type: mt,
    strategy: mt === 'video' ? 'round_robin' : g.strategy,
    max_switches: g.max_switches ?? 2,
    same_model_retries: g.same_model_retries ?? 1,
    failover_cooldown_seconds: g.failover_cooldown_seconds ?? 60,
    timeout_seconds: g.timeout_seconds ?? 0,
  }
  prevModelType.value = mt
  originalStrategy.value = formData.value.strategy
}

watch(
  () => props.group,
  async (val) => {
    if (val) {
      // 列表项可能缺 model_type 等字段，编辑时拉详情保证表单与库一致
      applyGroupToForm(val as any)
      try {
        const res = await groupsApi.getGroup(val.id)
        const full = res.data.data
        if (full) applyGroupToForm(full)
      } catch {
        // 详情失败时沿用列表行数据
      }
      await loadGroupModels(val.id)
    } else {
      formData.value = {
        name: '',
        description: '',
        model_type: 'chat',
        strategy: 'round_robin',
        max_switches: globalDefaults.value?.max_switches ?? 2,
        same_model_retries: globalDefaults.value?.same_model_retries ?? 1,
        failover_cooldown_seconds: globalDefaults.value?.failover_cooldown_seconds ?? 60,
        timeout_seconds: 0,
      }
      prevModelType.value = 'chat'
      originalStrategy.value = 'round_robin'
      modelList.value = []
    }
  },
  { immediate: true }
)

watch(
  () => props.visible,
  async (val) => {
    if (val) {
      await loadFailoverDefaults()
    }
  }
)

async function loadFailoverDefaults() {
  try {
    const res = await groupsApi.getGroupDefaults()
    globalDefaults.value = res.data.data
    if (!props.group) {
      formData.value.max_switches = globalDefaults.value?.max_switches ?? 2
      formData.value.same_model_retries = globalDefaults.value?.same_model_retries ?? 1
      formData.value.failover_cooldown_seconds = globalDefaults.value?.failover_cooldown_seconds ?? 60
    }
  } catch {
    globalDefaults.value = null
  }
}

async function loadGroupModels(groupId: number) {
  try {
    const res = await groupsApi.listGroupModels(groupId)
    modelList.value = (res.data.data || []).map(m => ({
      model_id: m.model_id,
      model_name: m.model_name,
      provider_name: m.provider_name,
      priority: m.priority,
      weight: m.weight,
      complexity_tier: m.complexity_tier,
    }))
  } catch {
    modelList.value = []
  }
}

function handleTypeChange(val: string) {
  const applyType = (next: string) => {
    prevModelType.value = next
    if (next === 'video') {
      formData.value.strategy = 'round_robin'
      originalStrategy.value = 'round_robin'
    }
  }

  if (modelList.value.length === 0) {
    applyType(val)
    return
  }

  ElMessageBox.confirm(
    '切换模型组类型后，已关联的模型将被清空（需按新类型重新选择），确定切换吗？',
    '切换类型确认',
    { confirmButtonText: '确定切换', cancelButtonText: '取消', type: 'warning' },
  ).then(() => {
    modelList.value = []
    applyType(val)
  }).catch(() => {
    formData.value.model_type = prevModelType.value
  })
}

function handleStrategyChange() {
  if (!isEdit.value) return
  if (formData.value.strategy !== originalStrategy.value && modelList.value.length > 0) {
    const oldLabel = originalStrategy.value === 'priority' ? '优先级' : originalStrategy.value === 'weighted' ? '权重' : originalStrategy.value === 'auto' ? '复杂度档位' : '轮询顺序'
    ElMessageBox.confirm(
      `切换负载策略后，已关联模型的${oldLabel}配置将丢失，确定切换吗？`,
      '切换策略确认',
      { confirmButtonText: '确定切换', cancelButtonText: '取消', type: 'warning' }
    ).then(() => {
      const toAuto = formData.value.strategy === 'auto'
      modelList.value = modelList.value.map(m => ({
        ...m,
        priority: null,
        weight: null,
        complexity_tier: toAuto ? 'MEDIUM' : undefined,
      }))
      originalStrategy.value = formData.value.strategy
    }).catch(() => {
      formData.value.strategy = originalStrategy.value
    })
  } else {
    originalStrategy.value = formData.value.strategy
  }
}

function removeModel(index: number) {
  modelList.value.splice(index, 1)
}

function handleSelectorConfirm(selected: ModelItem[]) {
  const existing = new Set(modelList.value.map(m => m.model_id))
  const newItems = selected.filter(s => !existing.has(s.model_id)).map(s => ({
    ...s,
    complexity_tier: formData.value.strategy === 'auto'
      ? (s.complexity_tier ?? 'MEDIUM')
      : undefined,
  }))
  if (newItems.length > 0) {
    modelList.value = [...modelList.value, ...newItems]
  }
  selectorVisible.value = false
}

function updateModelPriority(index: number, val: number) {
  modelList.value = modelList.value.map((m, i) =>
    i === index ? { ...m, priority: val } : m
  )
}

function updateModelWeight(index: number, val: number) {
  modelList.value = modelList.value.map((m, i) =>
    i === index ? { ...m, weight: val } : m
  )
}

function updateModelTier(index: number, val: string) {
  modelList.value = modelList.value.map((m, i) =>
    i === index ? { ...m, complexity_tier: val as ComplexityTier } : m
  )
}

async function handleSubmit() {
  if (!formRef.value) return

  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload: CreateGroupRequest = {
      name: formData.value.name,
      description: formData.value.description,
      strategy: formData.value.strategy,
      model_type: formData.value.model_type,
      max_switches: formData.value.max_switches,
      same_model_retries: formData.value.same_model_retries,
      failover_cooldown_seconds: formData.value.failover_cooldown_seconds,
      timeout_seconds: formData.value.timeout_seconds,
    }

    const buildModelParams = () => {
      const priorities: Record<string, number> = {}
      const weights: Record<string, number> = {}
      const tiers: Record<string, ComplexityTier> = {}
      for (const m of modelList.value) {
        if (m.priority != null) priorities[m.model_id] = m.priority
        if (m.weight != null) weights[m.model_id] = m.weight
        if (m.complexity_tier) tiers[m.model_id] = m.complexity_tier
      }
      return {
        model_ids: modelList.value.map(m => m.model_id),
        priorities: Object.keys(priorities).length > 0 ? priorities : undefined,
        weights: Object.keys(weights).length > 0 ? weights : undefined,
        complexity_tiers: Object.keys(tiers).length > 0 ? tiers : undefined,
      }
    }

    if (isEdit.value && props.group) {
      await groupsApi.updateGroup(props.group.id, payload)

      const res = await groupsApi.listGroupModels(props.group.id)
      const existingModelIds = (res.data.data || []).map(m => m.model_id)

      for (const modelId of existingModelIds) {
        await groupsApi.removeModelFromGroup(props.group.id, modelId)
      }

      if (modelList.value.length > 0) {
        await groupsApi.addModelsToGroup(props.group.id, buildModelParams())
      }
      ElMessage.success('更新成功')
    } else {
      const res = await groupsApi.createGroup(payload)
      const groupId = res.data.data.id

      if (modelList.value.length > 0) {
        await groupsApi.addModelsToGroup(groupId, buildModelParams())
      }
      ElMessage.success('创建成功')
    }
    emit('saved')
  } catch {
    ElMessage.error(isEdit.value ? '更新失败' : '创建失败')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.model-list-section {
  width: 100%;
}

.model-empty-hint {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  padding: 12px 0;
}

.model-items {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.model-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 10px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
}

.model-item-name {
  flex: 1;
  font-size: 13px;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.form-item-tip {
  margin-left: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.strategy-hint {
  margin: -8px 0 16px 100px;
  font-size: 12px;
  color: var(--el-color-primary);
  line-height: 1.4;
}

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95%;
    max-width: 95vw;
  }
  :deep(.el-dialog .el-dialog__body) {
    padding: 16px;
  }
  .model-item {
    flex-wrap: wrap;
    gap: 6px;
  }
  .form-item-tip {
    display: block;
    margin-left: 0;
    margin-top: 4px;
  }
}
</style>
