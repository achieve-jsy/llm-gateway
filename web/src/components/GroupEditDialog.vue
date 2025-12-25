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

      <el-form-item label="负载策略" prop="strategy">
        <el-radio-group v-model="formData.strategy" @change="handleStrategyChange">
          <el-radio value="round_robin">轮询</el-radio>
          <el-radio value="priority">优先级</el-radio>
          <el-radio value="weighted">加权</el-radio>
        </el-radio-group>
      </el-form-item>

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

      <el-form-item label="关联模型">
        <div class="model-list-section">
          <div v-if="modelList.length === 0" class="model-empty-hint">
            尚未添加模型，请点击下方按钮添加
          </div>
          <div v-else class="model-items">
            <div v-for="(item, index) in modelList" :key="item.model_id" class="model-item">
              <span class="model-item-name">{{ item.model_name }}</span>
              <template v-if="formData.strategy === 'priority'">
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
              <template v-if="formData.strategy === 'weighted'">
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
          <el-button style="margin-top: 8px" size="small" @click="selectorVisible = true">
            添加模型
          </el-button>
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
import type { GroupListItem, Strategy, CreateGroupRequest } from '@/types/group'
import ModelGroupSelector from '@/components/ModelGroupSelector.vue'

interface ModelItem {
  model_id: string
  model_name: string
  priority: number | null
  weight: number | null
}

interface FormState {
  name: string
  description: string
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
  strategy: 'round_robin',
  max_switches: 2,
  same_model_retries: 1,
  failover_cooldown_seconds: 60,
  timeout_seconds: 0,
})

const modelList = ref<ModelItem[]>([])

const originalStrategy = ref<Strategy>('round_robin')

const rules: FormRules = {
  name: [
    { required: true, message: '请输入模型组名称', trigger: 'blur' },
    { max: 64, message: '名称不能超过 64 个字符', trigger: 'blur' },
  ],
  strategy: [{ required: true, message: '请选择负载策略', trigger: 'change' }],
}

watch(
  () => props.group,
  async (val) => {
    if (val) {
      formData.value = {
        name: val.name,
        description: val.description || '',
        strategy: val.strategy,
        max_switches: val.max_switches ?? 2,
        same_model_retries: val.same_model_retries ?? 1,
        failover_cooldown_seconds: val.failover_cooldown_seconds ?? 60,
        timeout_seconds: val.timeout_seconds ?? 0,
      }
      originalStrategy.value = val.strategy
      await loadGroupModels(val.id)
    } else {
      formData.value = {
        name: '',
        description: '',
        strategy: 'round_robin',
        max_switches: globalDefaults.value?.max_switches ?? 2,
        same_model_retries: globalDefaults.value?.same_model_retries ?? 1,
        failover_cooldown_seconds: globalDefaults.value?.failover_cooldown_seconds ?? 60,
        timeout_seconds: 0,
      }
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
      priority: m.priority,
      weight: m.weight,
    }))
  } catch {
    modelList.value = []
  }
}

function handleStrategyChange() {
  if (!isEdit.value) return
  if (formData.value.strategy !== originalStrategy.value && modelList.value.length > 0) {
    ElMessageBox.confirm(
      `切换负载策略后，已关联模型的${originalStrategy.value === 'priority' ? '优先级' : originalStrategy.value === 'weighted' ? '权重' : '轮询顺序'}配置将丢失，确定切换吗？`,
      '切换策略确认',
      { confirmButtonText: '确定切换', cancelButtonText: '取消', type: 'warning' }
    ).then(() => {
      modelList.value = modelList.value.map(m => ({
        ...m,
        priority: null,
        weight: null,
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
  const newItems = selected.filter(s => !existing.has(s.model_id))
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
      max_switches: formData.value.max_switches,
      same_model_retries: formData.value.same_model_retries,
      failover_cooldown_seconds: formData.value.failover_cooldown_seconds,
      timeout_seconds: formData.value.timeout_seconds,
    }

if (isEdit.value && props.group) {
      await groupsApi.updateGroup(props.group.id, payload)

      const res = await groupsApi.listGroupModels(props.group.id)
      const existingModelIds = (res.data.data || []).map(m => m.model_id)

      for (const modelId of existingModelIds) {
        await groupsApi.removeModelFromGroup(props.group.id, modelId)
      }

      if (modelList.value.length > 0) {
        const priorities: Record<string, number> = {}
        const weights: Record<string, number> = {}
        for (const m of modelList.value) {
          if (m.priority != null) priorities[m.model_id] = m.priority
          if (m.weight != null) weights[m.model_id] = m.weight
        }
        await groupsApi.addModelsToGroup(props.group.id, {
          model_ids: modelList.value.map(m => m.model_id),
          priorities: Object.keys(priorities).length > 0 ? priorities : undefined,
          weights: Object.keys(weights).length > 0 ? weights : undefined,
        })
      }
      ElMessage.success('更新成功')
    } else {
      const res = await groupsApi.createGroup(payload)
      const groupId = res.data.data.id

      if (modelList.value.length > 0) {
        const priorities: Record<string, number> = {}
        const weights: Record<string, number> = {}
        for (const m of modelList.value) {
          if (m.priority != null) priorities[m.model_id] = m.priority
          if (m.weight != null) weights[m.model_id] = m.weight
        }
        await groupsApi.addModelsToGroup(groupId, {
          model_ids: modelList.value.map(m => m.model_id),
          priorities: Object.keys(priorities).length > 0 ? priorities : undefined,
          weights: Object.keys(weights).length > 0 ? weights : undefined,
        })
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
</style>
