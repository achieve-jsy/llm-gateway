<template>
  <el-dialog
    :model-value="visible"
    title="选择模型"
    width="640px"
    destroy-on-close
    @close="emit('close')"
  >
    <div class="selector-toolbar">
      <el-input
        v-model="filterName"
        placeholder="搜索渠道或模型名称"
        clearable
        class="selector-search"
      />
      <span class="selector-count">已选 {{ checkedIds.length }} 个</span>
    </div>

    <div class="selector-actions">
      <el-button size="small" @click="selectAllVisible" :disabled="visibleModels.length === 0">全选可见</el-button>
      <el-button size="small" @click="deselectAllVisible" :disabled="checkedVisibleCount === 0">取消全选</el-button>
    </div>

    <div v-if="loading" class="selector-loading">
      <el-skeleton :rows="5" animated />
    </div>

    <div v-else-if="allModels.length === 0" class="selector-empty">
      <el-empty description="暂无可用模型" />
    </div>

    <div v-else class="selector-list" ref="listRef" @scroll="handleScroll">
      <div
        v-for="model in visibleModels"
        :key="model.id"
        class="selector-item"
      >
        <el-checkbox
          :model-value="checkedIds.includes(model.id)"
          :disabled="props.existingModelIds.includes(model.id)"
          @change="(checked: boolean) => onCheckChange(model, checked)"
        >
          <span class="model-label">{{ model.provider?.name ? model.provider.name + ': ' : '' }}{{ model.name }}</span>
          <el-tag size="small" type="info" class="model-type-tag">{{ model.model_type }}</el-tag>
        </el-checkbox>
        <div v-if="checkedIds.includes(model.id) && props.strategy !== 'round_robin' && props.strategy !== 'auto'" class="model-param">
          <template v-if="props.strategy === 'priority'">
            <span class="param-label">优先级</span>
            <el-input-number
              :model-value="priorities[model.id] ?? 0"
              :min="0"
              :max="9999"
              size="small"
              controls-position="right"
              style="width: 120px"
              @update:model-value="(val: number | undefined) => onPriorityChange(model.id, val ?? 0)"
            />
          </template>
          <template v-else>
            <span class="param-label">权重</span>
            <el-input-number
              :model-value="weights[model.id] ?? 1"
              :min="1"
              :max="100"
              size="small"
              controls-position="right"
              style="width: 120px"
              @update:model-value="(val: number | undefined) => onWeightChange(model.id, val ?? 1)"
            />
          </template>
        </div>
      </div>
      <div v-if="loadingMore" class="loading-more">
        <el-skeleton :rows="2" animated />
      </div>
      <div v-else-if="!hasMore && allModels.length > 0" class="no-more">
        <el-divider>已加载全部模型</el-divider>
      </div>
    </div>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :disabled="checkedIds.length === 0" @click="onConfirm">
        确定 ({{ checkedIds.length }})
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { groupsApi } from '@/api/groups'
import type { ModelOption, Strategy } from '@/types/group'

interface ModelItem {
  model_id: string
  model_name: string
  priority: number | null
  weight: number | null
}

const props = defineProps<{
  visible: boolean
  strategy: Strategy
  existingModelIds: string[]
  /** 按模型组类型筛选可选模型（chat/embedding/…） */
  modelType?: string
}>()

const emit = defineEmits<{
  close: []
  confirm: [models: ModelItem[]]
  'update:visible': [val: boolean]
}>()

const allModels = ref<ModelOption[]>([])
const loading = ref(false)
const loadingMore = ref(false)
const filterName = ref('')
const checkedIds = ref<string[]>([])
const priorities = ref<Record<string, number>>({})
const weights = ref<Record<string, number>>({})
const listRef = ref<HTMLDivElement>()

// 分页状态
const currentPage = ref(1)
const pageSize = 50
const hasMore = ref(true)

const visibleModels = computed(() => {
  let list = allModels.value
  if (filterName.value) {
    const kw = filterName.value.toLowerCase()
    list = list.filter(m =>
      m.name.toLowerCase().includes(kw) ||
      (m.provider?.name ?? '').toLowerCase().includes(kw)
    )
  }
  return list
})

const checkedVisibleCount = computed(() =>
  checkedIds.value.filter(id => visibleModels.value.some(m => m.id === id)).length
)

// 搜索防抖
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(filterName, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    resetAndLoad()
  }, 300)
})

watch(
  () => props.visible,
  (val) => {
    if (val) {
      filterName.value = ''
      checkedIds.value = []
      priorities.value = {}
      weights.value = {}
      resetAndLoad()
    }
  }
)

watch(
  () => props.modelType,
  () => {
    if (props.visible) resetAndLoad()
  },
)

function resetAndLoad() {
  allModels.value = []
  currentPage.value = 1
  hasMore.value = true
  loadMoreModels()
}

async function loadMoreModels() {
  if (loading.value || loadingMore.value || !hasMore.value) return

  const isFirstLoad = currentPage.value === 1
  if (isFirstLoad) {
    loading.value = true
  } else {
    loadingMore.value = true
  }

  try {
    const res = await groupsApi.listAllModels({
      page: currentPage.value,
      page_size: pageSize,
      model_type: props.modelType || undefined,
    })
    const newModels = res.data.data || []

    // 追加新模型，避免重复
    const existingIds = new Set(allModels.value.map(m => m.id))
    const uniqueNewModels = newModels.filter(m => !existingIds.has(m.id))
    allModels.value = [...allModels.value, ...uniqueNewModels]

    // 判断是否还有更多
    hasMore.value = newModels.length >= pageSize
    currentPage.value++
  } catch {
    ElMessage.error('加载模型列表失败')
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

// 滚动监听
function handleScroll(e: Event) {
  const target = e.target as HTMLDivElement
  const scrollTop = target.scrollTop
  const scrollHeight = target.scrollHeight
  const clientHeight = target.clientHeight

  // 距离底部 100px 时加载更多
  if (scrollHeight - scrollTop - clientHeight < 100 && hasMore.value && !loadingMore.value) {
    loadMoreModels()
  }
}

function onCheckChange(model: ModelOption, checked: boolean) {
  if (checked) {
    checkedIds.value = [...checkedIds.value, model.id]
  } else {
    checkedIds.value = checkedIds.value.filter(id => id !== model.id)
    delete priorities.value[model.id]
    delete weights.value[model.id]
  }
}

function selectAllVisible() {
  const toAdd = visibleModels.value
    .filter(m => !props.existingModelIds.includes(m.id) && !checkedIds.value.includes(m.id))
    .map(m => m.id)
  if (toAdd.length > 0) {
    checkedIds.value = [...checkedIds.value, ...toAdd]
  }
}

function deselectAllVisible() {
  checkedIds.value = checkedIds.value.filter(id =>
    props.existingModelIds.includes(id) || !visibleModels.value.some(m => m.id === id)
  )
  for (const m of visibleModels.value) {
    delete priorities.value[m.id]
    delete weights.value[m.id]
  }
}

function onPriorityChange(id: string, val: number) {
  priorities.value = { ...priorities.value, [id]: val }
}

function onWeightChange(id: string, val: number) {
  weights.value = { ...weights.value, [id]: val }
}

function onConfirm() {
  const result: ModelItem[] = checkedIds.value.map(id => {
    const model = allModels.value.find(m => m.id === id)
    return {
      model_id: id,
      model_name: model?.name ?? id,
      priority: props.strategy === 'priority' ? (priorities.value[id] ?? 0) : null,
      weight: props.strategy === 'weighted' ? (weights.value[id] ?? 1) : null,
    }
  })
  emit('confirm', result)
}
</script>

<style scoped>
.selector-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.selector-search {
  flex: 1;
}

.selector-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.selector-actions {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
}

.selector-loading {
  padding: 20px 0;
}

.selector-empty {
  padding: 20px 0;
}

.selector-list {
  max-height: 380px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.selector-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  transition: background-color 0.15s;
}

.selector-item:hover {
  background: var(--el-fill-color-light);
}

.model-label {
  font-size: 13px;
  color: var(--el-text-color-primary);
  margin-left: 6px;
}

.model-type-tag {
  margin-left: 8px;
}

.model-provider-tag {
  margin-left: 4px;
}

.model-param {
  display: flex;
  align-items: center;
  gap: 6px;
}

.param-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.loading-more {
  padding: 12px 0;
}

.no-more {
  padding: 8px 0;
  :deep(.el-divider__text) {
    font-size: 12px;
    color: var(--el-text-color-placeholder);
  }
}

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95%;
    max-width: 95vw;
  }
  :deep(.el-dialog .el-dialog__body) {
    padding: 12px;
  }
  .selector-item {
    flex-wrap: wrap;
    gap: 6px;
  }
  .model-param {
    width: 100%;
    justify-content: flex-end;
    padding-left: 28px;
  }
}
</style>