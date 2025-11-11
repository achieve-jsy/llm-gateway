<template>
  <el-dialog
    :model-value="visible"
    :title="`同步模型 — ${providerName}`"
    width="760px"
    @close="emit('close')"
  >
    <template v-if="syncResult">
      <!-- 统计栏 -->
      <div class="sync-stats">
        <div class="stat-item stat-total">
          <span class="stat-label">上游总计</span>
          <span class="stat-value">{{ syncResult.total_upstream }}</span>
        </div>
        <div class="stat-item stat-added">
          <span class="stat-label">新增</span>
          <span class="stat-value">{{ (syncResult.added || []).length }}</span>
        </div>
        <div class="stat-item stat-existing">
          <span class="stat-label">已存在</span>
          <span class="stat-value">{{ (syncResult.existing || []).length }}</span>
        </div>
        <div class="stat-item stat-removed">
          <span class="stat-label">已下架</span>
          <span class="stat-value">{{ (syncResult.removed || []).length }}</span>
        </div>
      </div>

      <!-- Tab 分类 -->
      <el-tabs v-model="activeTab" class="sync-tabs">
        <el-tab-pane :label="`新增 (${(syncResult.added || []).length})`" name="added" />
        <el-tab-pane :label="`已存在 (${(syncResult.existing || []).length})`" name="existing" />
        <el-tab-pane :label="`已下架 (${(syncResult.removed || []).length})`" name="removed" />
      </el-tabs>

      <!-- 搜索框 -->
      <el-input
        v-model="searchText"
        placeholder="搜索模型名称"
        clearable
        class="sync-search"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>

      <!-- 提示信息（新增 Tab） -->
      <div v-if="activeTab === 'added' && selectedModels.length === 0" class="sync-hint">
        共 {{ (syncResult.added || []).length }} 个新模型可注册，请勾选需要的模型
      </div>

      <!-- 模型表格 -->
      <div class="model-table-wrapper">
        <el-table
          :data="currentTabFiltered"
          height="400"
          stripe
          class="sync-model-table"
          row-class-name="model-row"
          @selection-change="handleSelectionChange"
          ref="tableRef"
        >
          <el-table-column
            v-if="activeTab === 'added'"
            type="selection"
            width="45"
            :selectable="() => true"
          />
          <el-table-column label="模型名称" prop="id" min-width="320">
            <template #default="{ row }">
              <span class="model-name">{{ row.id }}</span>
            </template>
          </el-table-column>
          <el-table-column label="类型" width="100" align="center">
            <template #default="{ row }">
              <el-tag v-if="getModelType(row.id) === 'embedding'" type="info" size="small">
                Embedding
              </el-tag>
              <el-tag v-else-if="getModelType(row.id) === 'vision'" type="warning" size="small">
                Vision
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="归属" prop="owned_by" width="140">
            <template #default="{ row }">
              <span class="model-owner">{{ row.owned_by }}</span>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 底部操作栏 -->
      <div class="sync-footer">
        <div class="footer-actions" v-if="activeTab === 'added'">
          <el-button size="small" @click="handleSelectAllAdded">全选新增</el-button>
          <el-button size="small" @click="handleSelectChatOnly">仅选 Chat</el-button>
          <el-button size="small" @click="handleClearSelection">清空选择</el-button>
        </div>
        <div v-else class="footer-actions">
          <span class="footer-info">{{ activeTab === 'existing' ? '已存在模型无需操作' : '下架模型将被禁用' }}</span>
        </div>
        <div class="footer-buttons">
          <el-button @click="emit('close')">取消</el-button>
          <el-button
            type="primary"
            :loading="confirming"
            :disabled="selectedModels.length === 0"
            @click="handleConfirm"
          >
            确认注册 ({{ selectedModels.length }})
          </el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { ElMessage, ElTable } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { confirmSync } from '@/services/control-plane'
import type { SyncResult, SyncModelItem } from '@/types/sync'

type ModelType = 'chat' | 'embedding' | 'vision'

interface ModelListItem extends SyncModelItem {
  status: 'added' | 'existing' | 'removed'
}

const props = defineProps<{
  visible: boolean
  providerId: string
  providerName: string
  syncResult: SyncResult | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const searchText = ref('')
const selectedModels = ref<string[]>([])
const confirming = ref(false)
const activeTab = ref<'added' | 'existing' | 'removed'>('added')
const tableRef = ref<InstanceType<typeof ElTable> | null>(null)

/** 根据模型名启发式判断类型 */
function getModelType(modelId: string): ModelType {
  const lower = modelId.toLowerCase()
  if (lower.includes('embed')) return 'embedding'
  if (lower.includes('vl') || lower.includes('vision')) return 'vision'
  return 'chat'
}

/** 按 Tab 分类的模型列表（排序） */
const addedList = computed<ModelListItem[]>(() => {
  if (!props.syncResult) return []
  return props.syncResult.added || []
    .map(item => ({ ...item, status: 'added' as const }))
    .sort((a, b) => a.id.localeCompare(b.id))
})

const existingList = computed<ModelListItem[]>(() => {
  if (!props.syncResult) return []
  return props.syncResult.existing || []
    .map(item => ({ ...item, status: 'existing' as const }))
    .sort((a, b) => a.id.localeCompare(b.id))
})

const removedList = computed<ModelListItem[]>(() => {
  if (!props.syncResult) return []
  return props.syncResult.removed || []
    .map(item => ({ ...item, status: 'removed' as const }))
    .sort((a, b) => a.id.localeCompare(b.id))
})

/** 当前 Tab 对应的列表 */
const currentTabList = computed<ModelListItem[]>(() => {
  if (activeTab.value === 'added') return addedList.value
  if (activeTab.value === 'existing') return existingList.value
  return removedList.value
})

/** 搜索过滤 */
const currentTabFiltered = computed<ModelListItem[]>(() => {
  if (!searchText.value) return currentTabList.value
  const keyword = searchText.value.toLowerCase()
  return currentTabList.value.filter(item => item.id.toLowerCase().includes(keyword))
})

/** 可选的 Chat 模型（排除 embedding 和 vision） */
const chatOnlyIds = computed(() =>
  addedList.value
    .filter(m => getModelType(m.id) === 'chat')
    .map(m => m.id)
)

/** 处理表格选中变化 */
function handleSelectionChange(rows: ModelListItem[]) {
  selectedModels.value = rows.map(r => r.id)
}

/** 全选新增 */
function handleSelectAllAdded() {
  selectedModels.value = addedList.value.map(m => m.id)
  syncTableSelection()
}

/** 仅选 Chat 模型 */
function handleSelectChatOnly() {
  selectedModels.value = [...chatOnlyIds.value]
  syncTableSelection()
}

/** 清空选择 */
function handleClearSelection() {
  selectedModels.value = []
  tableRef.value?.clearSelection()
}

/** 同步选中状态到表格 UI */
function syncTableSelection() {
  nextTick(() => {
    if (!tableRef.value) return
    tableRef.value.clearSelection()
    const selectedSet = new Set(selectedModels.value)
    for (const row of currentTabFiltered.value) {
      if (selectedSet.has(row.id)) {
        tableRef.value.toggleRowSelection(row, true)
      }
    }
  })
}

/** 确认注册 */
async function handleConfirm() {
  confirming.value = true
  try {
    const result = await confirmSync(props.providerId, {
      models_to_add: selectedModels.value
    })
    ElMessage.success(`注册完成：新增 ${result.registered} 个，跳过 ${result.skipped} 个`)
    emit('saved')
    emit('close')
  } catch {
    ElMessage.error('注册失败')
  } finally {
    confirming.value = false
  }
}

/** 打开时重置状态（不全选） */
watch(() => props.visible, (val) => {
  if (val && props.syncResult) {
    selectedModels.value = []
    searchText.value = ''
    activeTab.value = 'added'
  }
})

/** Tab 切换时同步表格选中状态 */
watch(activeTab, () => {
  nextTick(() => {
    if (activeTab.value === 'added') {
      syncTableSelection()
    }
  })
})
</script>

<style scoped>
.sync-stats {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border-radius: 8px;
  font-size: 14px;
  flex: 1;
  min-width: 100px;
}

.stat-total {
  background: rgba(0, 128, 128, 0.05);
  color: #0d7377;
  border: 1px solid rgba(0, 128, 128, 0.15);
}

.stat-added {
  background: rgba(0, 128, 128, 0.08);
  color: #008080;
  border: 1px solid rgba(0, 128, 128, 0.2);
}

.stat-existing {
  background: rgba(128, 128, 128, 0.06);
  color: #666;
  border: 1px solid rgba(128, 128, 128, 0.15);
}

.stat-removed {
  background: rgba(255, 111, 97, 0.06);
  color: #d35400;
  border: 1px solid rgba(255, 111, 97, 0.2);
}

.stat-label {
  font-weight: 500;
}

.stat-value {
  font-weight: 700;
  font-size: 18px;
}

.sync-tabs {
  margin-bottom: 12px;
}

.sync-tabs :deep(.el-tabs__nav-wrap::after) {
  border-bottom-style: dashed;
}

.sync-tabs :deep(.el-tabs__active-bar) {
  background-color: #008080;
}

.sync-tabs :deep(.el-tabs__item.is-active) {
  color: #008080;
}

.sync-tabs :deep(.el-tabs__item:hover) {
  color: #0d7377;
}

.sync-search {
  margin-bottom: 12px;
}

.sync-hint {
  padding: 8px 12px;
  margin-bottom: 12px;
  background: rgba(0, 128, 128, 0.05);
  border: 1px dashed rgba(0, 128, 128, 0.3);
  border-radius: 6px;
  color: #0d7377;
  font-size: 13px;
}

.model-table-wrapper {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  overflow: hidden;
}

.sync-model-table :deep(.model-row:hover > td) {
  background-color: rgba(0, 128, 128, 0.04) !important;
}

.sync-model-table :deep(.el-table__header th) {
  background-color: rgba(0, 128, 128, 0.03);
  font-weight: 600;
}

.model-name {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 13px;
  color: #333;
}

.model-owner {
  font-size: 12px;
  color: #888;
}

.sync-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-extra-light);
}

.footer-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.footer-info {
  font-size: 13px;
  color: #999;
}

.footer-buttons {
  display: flex;
  gap: 8px;
}
</style>
