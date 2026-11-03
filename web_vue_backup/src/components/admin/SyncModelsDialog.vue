<template>
  <el-dialog
    :model-value="visible"
    :title="`同步模型 — ${providerName}`"
    width="860px"
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

      <!-- 可用性筛选（仅新增 Tab） -->
      <div v-if="activeTab === 'added'" class="availability-panel">
        <div class="availability-row">
          <el-button size="small" :loading="checkingAvailability" @click="checkAvailability">
            <el-icon v-if="!checkingAvailability"><Connection /></el-icon>
            {{ checkingBtnText }}
          </el-button>
          <el-checkbox
            v-if="availabilityChecked"
            v-model="showAvailableOnly"
            class="avail-filter-checkbox"
          >
            仅显示可用
          </el-checkbox>
          <span v-if="availabilityChecked && !showAvailableOnly" class="avail-summary">
            <span class="avail-dot avail-dot-ok"></span> 可用 {{ availableCount }}
            <span class="avail-dot avail-dot-bad"></span> 不可用 {{ unavailableCount }}
            <span class="avail-dot avail-dot-skipped"></span> 跳过 {{ skippedCount }}
          </span>
          <el-button
            v-if="availabilityChecked && availableCount > 0"
            size="small"
            type="success"
            plain
            @click="handleSelectAvailable"
            class="avail-select-btn"
          >
            选中可用 ({{ availableCount }})
          </el-button>
        </div>
        <div v-if="checkingAvailability" class="check-progress">
          <el-progress
            :percentage="checkProgress"
            :status="checkProgress === 100 ? 'success' : undefined"
            :stroke-width="6"
          />
          <span class="progress-text">已检测 {{ checkedCount }} / {{ checkableTotal }}</span>
        </div>
      </div>

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
            :selectable="checkSelectable"
          />
          <el-table-column label="模型名称" prop="id" min-width="240">
            <template #default="{ row }">
              <span class="model-name">{{ row.id }}</span>
            </template>
          </el-table-column>
          <el-table-column label="类型" width="100" align="center">
            <template #default="{ row }">
              <el-tag v-if="getModelType(row.id) !== 'chat'" :type="tagType(row.id)" size="small">
                {{ typeLabel(row.id) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="可用性" width="110" align="center" v-if="activeTab === 'added'">
            <template #default="{ row }">
              <template v-if="getAvailability(row.id)">
                <el-tooltip placement="top">
                  <template #content>
                    <template v-if="getAvailability(row.id)!.available">
                      可用 · {{ getAvailability(row.id)!.response_time }}ms
                    </template>
                    <template v-else-if="getAvailability(row.id)!.skipped">
                      已跳过（非 chat 模型）
                    </template>
                    <template v-else>
                      {{ getAvailability(row.id)!.error || '检测失败' }}
                    </template>
                  </template>
                  <span v-if="getAvailability(row.id)!.available" class="avail-dot avail-dot-ok"></span>
                  <span v-else-if="getAvailability(row.id)!.skipped" class="avail-dot avail-dot-skipped"></span>
                  <span v-else class="avail-dot avail-dot-bad"></span>
                </el-tooltip>
                <span class="avail-text" :class="availTextClass(row.id)">
                  {{ availLabel(row.id) }}
                </span>
              </template>
              <span v-else class="avail-text avail-unknown">-</span>
            </template>
          </el-table-column>
          <el-table-column label="归属" prop="owned_by" width="120">
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
          <el-button size="small" :disabled="availableIds.length === 0" @click="handleSelectAvailable">仅选可用</el-button>
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
import { Search, Connection } from '@element-plus/icons-vue'
import { confirmSync, checkModels } from '@/services/control-plane'
import type { SyncResult, SyncModelItem, ModelAvailabilityItem } from '@/types/sync'

type ModelType = 'chat' | 'embedding' | 'vision' | 'tts' | 'asr' | 'image' | 'speech' | 'ocr' | 'omni' | 'live-translate' | 'coder' | 'math' | 'think'

interface ModelListItem extends SyncModelItem {
  status: 'added' | 'existing' | 'removed'
}

const TYPE_LABELS: Record<ModelType, string> = {
  chat: '',
  embedding: 'Embedding',
  vision: 'Vision',
  tts: 'TTS',
  asr: 'ASR',
  image: 'Image',
  speech: 'Speech',
  ocr: 'OCR',
  omni: 'Omni',
  'live-translate': '翻译',
  coder: 'Coder',
  math: 'Math',
  think: 'Think',
}

const TYPE_TAG_MAP: Record<ModelType, string> = {
  chat: '',
  embedding: 'info',
  vision: 'warning',
  tts: '',
  asr: 'info',
  image: 'danger',
  speech: '',
  ocr: 'info',
  omni: 'success',
  'live-translate': 'info',
  coder: '',
  math: '',
  think: 'warning',
}

const BATCH_SIZE = 30

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
const availabilityMap = ref<Record<string, ModelAvailabilityItem>>({})
const checkingAvailability = ref(false)
const showAvailableOnly = ref(false)
const checkedCount = ref(0)
const checkableTotal = ref(0)

const availabilityChecked = computed(() => Object.keys(availabilityMap.value).length > 0)

const availableCount = computed(() =>
  Object.values(availabilityMap.value).filter(a => a.available).length
)

const unavailableCount = computed(() =>
  Object.values(availabilityMap.value).filter(a => !a.available && !a.skipped).length
)

const skippedCount = computed(() =>
  Object.values(availabilityMap.value).filter(a => a.skipped).length
)

const availableIds = computed(() =>
  addedList.value.filter(m => getAvailability(m.id)?.available).map(m => m.id)
)

const checkProgress = computed(() => {
  if (checkableTotal.value === 0) return 0
  return Math.round((checkedCount.value / checkableTotal.value) * 100)
})

const checkingBtnText = computed(() => {
  if (!checkingAvailability.value) return '检测可用性'
  return `检测中 ${checkedCount.value}/${checkableTotal.value}`
})

function getModelType(modelId: string): ModelType {
  const lower = modelId.toLowerCase()
  if (lower.includes('embed')) return 'embedding'
  if (lower.includes('tts')) return 'tts'
  if (lower.includes('asr')) return 'asr'
  if (lower.includes('image-') || lower.startsWith('wan') || lower.startsWith('z-image') || lower.includes('/image')) return 'image'
  if (lower.includes('speech-') || lower.includes('/speech-')) return 'speech'
  if (lower.includes('ocr')) return 'ocr'
  if (lower.includes('omni')) return 'omni'
  if (lower.includes('vl-') || (lower.includes('vl') && !lower.includes('livetranslate') && !lower.includes('siliconflow'))) return 'vision'
  if (lower.includes('live') && lower.includes('translate')) return 'live-translate'
  if (lower.includes('coder') || (lower.includes('code') && !lower.includes('decode'))) return 'coder'
  if (lower.includes('math')) return 'math'
  if (lower.includes('think') || lower.includes('r1') || lower.includes('qwq') || lower.includes('qvq')) return 'think'
  return 'chat'
}

function typeLabel(modelId: string): string {
  return TYPE_LABELS[getModelType(modelId)] || ''
}

function tagType(modelId: string): string {
  return TYPE_TAG_MAP[getModelType(modelId)] || ''
}

function getAvailability(modelId: string): ModelAvailabilityItem | undefined {
  return availabilityMap.value[modelId]
}

function availLabel(modelId: string): string {
  const a = getAvailability(modelId)
  if (!a) return '-'
  if (a.available) return `${a.response_time}ms`
  if (a.skipped) return '跳过'
  return '不可用'
}

function availTextClass(modelId: string): string {
  const a = getAvailability(modelId)
  if (!a) return 'avail-unknown'
  if (a.available) return 'avail-ok'
  if (a.skipped) return 'avail-skipped'
  return 'avail-bad'
}

function checkSelectable(row: ModelListItem): boolean {
  if (!showAvailableOnly.value) return true
  const a = getAvailability(row.id)
  if (!a) return true
  return a.available
}

const addedList = computed<ModelListItem[]>(() => {
  if (!props.syncResult) return []
  return (props.syncResult.added || [])
    .map((item): ModelListItem => ({ ...item, status: 'added' }))
    .sort((a, b) => a.id.localeCompare(b.id))
})

const existingList = computed<ModelListItem[]>(() => {
  if (!props.syncResult) return []
  return (props.syncResult.existing || [])
    .map((item): ModelListItem => ({ ...item, status: 'existing' }))
    .sort((a, b) => a.id.localeCompare(b.id))
})

const removedList = computed<ModelListItem[]>(() => {
  if (!props.syncResult) return []
  return (props.syncResult.removed || [])
    .map((item): ModelListItem => ({ ...item, status: 'removed' }))
    .sort((a, b) => a.id.localeCompare(b.id))
})

const currentTabList = computed<ModelListItem[]>(() => {
  if (activeTab.value === 'added') return addedList.value
  if (activeTab.value === 'existing') return existingList.value
  return removedList.value
})

const currentTabFiltered = computed<ModelListItem[]>(() => {
  let list = currentTabList.value
  const keyword = searchText.value.toLowerCase()
  if (keyword) {
    list = list.filter(item => item.id.toLowerCase().includes(keyword))
  }
  if (activeTab.value === 'added' && showAvailableOnly.value && availabilityChecked.value) {
    list = list.filter(item => {
      const a = getAvailability(item.id)
      return !a || a.available
    })
  }
  return list
})

const chatOnlyIds = computed(() =>
  addedList.value
    .filter(m => getModelType(m.id) === 'chat')
    .map(m => m.id)
)

function handleSelectionChange(rows: ModelListItem[]) {
  selectedModels.value = rows.map(r => r.id)
}

function handleSelectAllAdded() {
  selectedModels.value = addedList.value.map(m => m.id)
  syncTableSelection()
}

function handleSelectChatOnly() {
  selectedModels.value = [...chatOnlyIds.value]
  syncTableSelection()
}

function handleSelectAvailable() {
  selectedModels.value = [...availableIds.value]
  syncTableSelection()
}

function handleClearSelection() {
  selectedModels.value = []
  tableRef.value?.clearSelection()
}

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

async function checkAvailability() {
  const names = (props.syncResult?.added || []).map(m => m.id)
  if (names.length === 0) return

  checkingAvailability.value = true
  checkableTotal.value = names.length
  checkedCount.value = 0
  availabilityMap.value = {}

  try {
    for (let i = 0; i < names.length; i += BATCH_SIZE) {
      const batch = names.slice(i, i + BATCH_SIZE)
      const result = await checkModels(props.providerId, { model_names: batch })
      for (const m of result.models) {
        availabilityMap.value[m.name] = m
      }
      checkedCount.value = Math.min(i + BATCH_SIZE, names.length)
    }
  } catch {
    ElMessage.warning('可用性检测失败，仍可正常选择模型')
  } finally {
    checkingAvailability.value = false
  }
}

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

watch(() => props.visible, (val) => {
  if (val && props.syncResult) {
    selectedModels.value = []
    searchText.value = ''
    activeTab.value = 'added'
    availabilityMap.value = {}
    showAvailableOnly.value = false
    checkedCount.value = 0
    checkAvailability()
  }
})

watch(showAvailableOnly, (val) => {
  if (val) {
    handleSelectAvailable()
  }
})

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

.availability-panel {
  margin-bottom: 10px;
  padding: 8px 12px;
  background: rgba(0, 128, 128, 0.03);
  border: 1px solid rgba(0, 128, 128, 0.1);
  border-radius: 6px;
}

.availability-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.avail-filter-checkbox {
  margin-left: 4px;
}

.avail-summary {
  font-size: 13px;
  color: #555;
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: 4px;
}

.avail-select-btn {
  margin-left: auto;
}

.check-progress {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
}

.check-progress :deep(.el-progress) {
  flex: 1;
}

.check-progress :deep(.el-progress-bar__outer) {
  background-color: rgba(0, 128, 128, 0.1);
}

.check-progress :deep(.el-progress-bar__inner) {
  background-color: #008080;
}

.progress-text {
  font-size: 12px;
  color: #888;
  white-space: nowrap;
  min-width: 100px;
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

.avail-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 4px;
  vertical-align: middle;
}

.avail-dot-ok {
  background: #27ae60;
}

.avail-dot-bad {
  background: #e74c3c;
}

.avail-dot-skipped {
  background: #bdc3c7;
}

.avail-text {
  font-size: 12px;
  vertical-align: middle;
}

.avail-ok {
  color: #27ae60;
  font-weight: 500;
}

.avail-bad {
  color: #e74c3c;
  font-weight: 500;
}

.avail-skipped {
  color: #95a5a6;
}

.avail-unknown {
  color: #bdc3c7;
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