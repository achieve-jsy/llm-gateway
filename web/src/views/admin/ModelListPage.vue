<template>
  <div class="model-page">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <h2 class="page-title">模型管理</h2>
      <div class="header-actions">
        <el-input
          v-model="filterName"
          placeholder="搜索模型名称"
          clearable
          :prefix-icon="Search"
          style="width: 200px; margin-right: 12px"
          @input="handleFilterChange"
        />
        <el-select
          v-model="filterModelType"
          placeholder="按类型筛选"
          clearable
          style="width: 130px; margin-right: 12px"
          @change="handleFilterChange"
        >
          <el-option label="chat" value="chat" />
          <el-option label="embedding" value="embedding" />
        </el-select>
        <el-select
          v-model="filterProviderId"
          placeholder="按渠道筛选"
          clearable
          style="width: 180px; margin-right: 12px"
          @change="handleFilterChange"
        >
          <el-option
            v-for="p in providerOptions"
            :key="p.id"
            :label="p.name"
            :value="p.id"
          />
        </el-select>
        <el-select
          v-model="filterTagIds"
          placeholder="按标签筛选"
          multiple
          clearable
          collapse-tags
          style="width: 200px; margin-right: 12px"
          @change="handleFilterChange"
        >
          <el-option
            v-for="tag in allTags"
            :key="tag.id"
            :label="tag.name"
            :value="tag.id"
          />
        </el-select>
        <el-button
          type="success"
          :disabled="selectedModels.length === 0"
          @click="handleBatchEnable"
        >
          批量启用{{ selectedModels.length > 0 ? `(${selectedModels.length})` : '' }}
        </el-button>
        <el-button
          type="warning"
          :disabled="selectedModels.length === 0"
          @click="handleBatchDisable"
        >
          批量禁用{{ selectedModels.length > 0 ? `(${selectedModels.length})` : '' }}
        </el-button>
        <el-button
          type="warning"
          :disabled="selectedModels.length === 0"
          @click="batchTagDialogVisible = true"
        >
          批量设置标签{{ selectedModels.length > 0 ? `(${selectedModels.length})` : '' }}
        </el-button>
        <el-button
          type="danger"
          :disabled="selectedModels.length === 0"
          @click="handleBatchDelete"
        >
          批量删除{{ selectedModels.length > 0 ? `(${selectedModels.length})` : '' }}
        </el-button>
        <el-tooltip content="检测设置" placement="top">
          <el-button text type="info" @click="modelCheckSettingsVisible = true">
            <el-icon :size="18"><Setting /></el-icon>
          </el-button>
        </el-tooltip>
        <el-button type="primary" @click="openCreateDialog">注册模型</el-button>
      </div>
    </div>

    <!-- 模型列表表格 -->
    <el-table :data="filteredModels" border stripe @selection-change="handleSelectionChange">
      <el-table-column type="selection" width="45" />
      <el-table-column prop="name" label="模型名称" min-width="180" show-overflow-tooltip />

      <el-table-column label="上游模型ID" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <span v-if="row.upstream_model_id" class="mono-text">{{ row.upstream_model_id }}</span>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column label="归属渠道" min-width="120">
        <template #default="{ row }">
          {{ row.provider?.name || getProviderName(row.provider_id) }}
        </template>
      </el-table-column>

      <el-table-column label="类型" width="100">
        <template #default="{ row }">
          <el-tag :type="row.model_type === 'embedding' ? 'warning' : 'success'" size="small">
            {{ row.model_type || 'chat' }}
          </el-tag>
        </template>
      </el-table-column>

      <el-table-column label="能力" width="140">
        <template #default="{ row }">
          <span v-if="row.catalog" class="capability-row">
            <span v-if="row.catalog.supports_function_calling" title="function calling">🛠</span>
            <span v-if="row.catalog.supports_vision" title="vision">🖼</span>
            <span v-if="row.catalog.supports_prompt_caching" title="prompt caching">💾</span>
            <span v-if="row.catalog.supports_response_schema" title="response schema">📋</span>
          </span>
          <span v-else class="text-muted">-</span>
          <el-tag
            v-if="row.catalog?.deprecation_date"
            type="danger"
            size="small"
            class="dep-badge"
          >
            ⏰ {{ row.catalog.deprecation_date.slice(0, 10) }} 废弃
          </el-tag>
        </template>
      </el-table-column>


      <el-table-column label="标签" min-width="160">
        <template #default="{ row }">
          <template v-if="modelTagsMap[row.id]?.length">
            <el-tag
              v-for="tag in modelTagsMap[row.id]"
              :key="tag.id"
              size="small"
              :style="{
                backgroundColor: tag.color + '26',
                color: tag.color,
                borderColor: tag.color + '40',
                marginRight: '4px',
                marginBottom: '2px'
              }"
            >
              {{ tag.name }}
            </el-tag>
          </template>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column label="上下文" width="180" align="center">
        <template #default="{ row }">
          <span v-if="row.context_length">{{ formatTokenCount(row.context_length) }}</span>
          <span v-else class="text-muted">-</span>
          <template v-if="row.ctx_overridden">
            <el-tag type="warning" size="small" class="override-badge">已脱离目录</el-tag>
            <el-button
              text
              type="primary"
              size="small"
              class="restore-btn"
              @click="handleResetOverride(row, 'context_length')"
            >🔗 恢复跟随目录</el-button>
          </template>
        </template>
      </el-table-column>

      <el-table-column label="价格" width="130" align="center">
        <template #default="{ row }">
          <span v-if="row.input_price || row.output_price">
            ${{ row.input_price ?? 0 }}/${{ row.output_price ?? 0 }}
          </span>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column label="状态" width="80" align="center">
        <template #default="{ row }">
          <el-switch
            :model-value="row.enabled"
            @change="(val: boolean) => handleToggleModel(row, val)"
          />
        </template>
      </el-table-column>

      <el-table-column label="健康" width="160" align="center">
        <template #default="{ row }">
          <div class="health-cell">
            <template v-if="!row.enabled && row.consecutive_failures >= 3">
              <el-tooltip :content="row.last_check_error || '连续失败达阈值'" placement="top">
                <div class="health-inner">
                  <span class="health-dot health-error"></span>
                  <span class="health-text health-error-text">{{ shortenError(row.last_check_error) || '模型不可用' }} {{ row.consecutive_failures }}/3</span>
                </div>
              </el-tooltip>
            </template>
            <template v-else-if="row.consecutive_failures > 0">
              <el-tooltip :content="row.last_check_error || '检测失败'" placement="top">
                <div class="health-inner">
                  <span class="health-dot health-warn"></span>
                  <span class="health-text health-warn-text">{{ shortenError(row.last_check_error) || '检测失败' }} {{ row.consecutive_failures }}/3</span>
                </div>
              </el-tooltip>
            </template>
            <template v-else-if="row.response_time > 0">
              <div class="health-inner">
                <span class="health-dot health-ok"></span>
                <span class="health-text health-ok-text">{{ row.response_time }}ms</span>
              </div>
            </template>
            <template v-else>
              <div class="health-inner">
                <span class="health-dot health-unknown"></span>
                <span class="health-text text-muted">未检测</span>
              </div>
            </template>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="180" align="center">
        <template #default="{ row }">
          <el-button text type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
          <el-button
            text
            type="warning"
            size="small"
            :loading="checkingModelId === row.id"
            @click="handleCheckModel(row)"
          >检测</el-button>
          <el-popconfirm
            title="确定删除该模型？"
            confirm-button-text="确定"
            cancel-button-text="取消"
            @confirm="handleDelete(row)"
          >
            <template #reference>
              <el-button text type="danger" size="small">删除</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100, 200]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="applyFilter"
        @size-change="handleSizeChange"
      />
    </div>

    <!-- 创建/编辑对话框 -->
    <ModelDialog
      v-model:visible="dialogVisible"
      :model="editingModel"
      :providers="providerOptions"
      @saved="onDialogSaved"
      @close="dialogVisible = false"
    />

    <!-- 批量设置标签对话框 -->
    <BatchTagDialog
      :visible="batchTagDialogVisible"
      :model-ids="selectedModelIds"
      @saved="onBatchTagSaved"
      @close="batchTagDialogVisible = false"
    />

    <!-- 模型检测设置对话框 -->
    <ModelCheckSettingsDialog
      :visible="modelCheckSettingsVisible"
      @close="modelCheckSettingsVisible = false"
      @saved="onModelCheckSettingsSaved"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Setting } from '@element-plus/icons-vue'
import { getModels, deleteModel, updateModel, getProviders, getModelTags, getModelTagsForModel, batchDeleteModels, batchUpdateModelStatus, resetModelOverride, checkModel } from '@/services/control-plane'
import type { Model } from '@/types/model'
import type { Provider } from '@/types/provider'
import type { ModelTag } from '@/types/model-tag'
import type { ResettableOverrideField } from '@/types/catalog'
import ModelDialog from '@/components/admin/ModelDialog.vue'
import BatchTagDialog from '@/components/admin/BatchTagDialog.vue'
import ModelCheckSettingsDialog from '@/components/admin/ModelCheckSettingsDialog.vue'

// const allModels = ref<Model[]>([]) // 已改为后端分页，不再使用
const models = ref<Model[]>([])
const providerOptions = ref<Provider[]>([])
const allTags = ref<ModelTag[]>([])
const filterProviderId = ref('')
const filterName = ref('')
const filterModelType = ref('')
const filterTagIds = ref<string[]>([])
const modelTagsMap = ref<Record<string, ModelTag[]>>({})
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const dialogVisible = ref(false)
const editingModel = ref<Model | null>(null)

const selectedModels = ref<Model[]>([])
const selectedModelIds = computed(() => selectedModels.value.map(m => m.id))
const batchTagDialogVisible = ref(false)
const modelCheckSettingsVisible = ref(false)
const checkingModelId = ref('')

/** 客户端标签筛选（在后端分页结果上进一步过滤） */
const filteredModels = computed(() => {
  let result = models.value
  // 标签筛选（客户端过滤，因为标签来自独立API）
  if (filterTagIds.value.length) {
    result = result.filter(m => {
      const tags = modelTagsMap.value[m.id] ?? []
      return filterTagIds.value.every(tid => tags.some(t => t.id === tid))
    })
  }
  return result
})

/** 加载模型列表（后端分页+筛选） */
async function loadModels() {
  try {
    const result = await getModels(filterProviderId.value || undefined, {
      page: currentPage.value,
      pageSize: pageSize.value,
      name: filterName.value,
      model_type: filterModelType.value,
    })
    models.value = result.data
    total.value = result.total
    await loadModelTags()
  } catch {
    ElMessage.error('加载模型列表失败')
  }
}

/** 加载当前页模型的标签 */
async function loadModelTags() {
  const map: Record<string, ModelTag[]> = {}
  await Promise.all(
    models.value.map(async (model) => {
      try {
        map[model.id] = await getModelTagsForModel(model.id)
      } catch {
        map[model.id] = []
      }
    })
  )
  modelTagsMap.value = map
}

/** 翻页或切换 size 时重新加载 */
function applyFilter() {
  loadModels()
}

/** 加载渠道列表 */
async function loadProviders() {
  try {
    const result = await getProviders({ page: 1, pageSize: 1000 })
    providerOptions.value = result.data
  } catch {
    ElMessage.error('加载渠道列表失败')
  }
}

/** 加载全部标签（用于筛选下拉） */
async function loadAllTags() {
  try {
    allTags.value = await getModelTags()
  } catch {
    allTags.value = []
  }
}

/** 渠道筛选/能力筛选变化 */
function handleFilterChange() {
  currentPage.value = 1
  loadModels()
}

/** 每页条数变化 */
function handleSizeChange() {
  currentPage.value = 1
  applyFilter()
}

/** 获取渠道名称 */
function getProviderName(providerId: string): string {
  const found = providerOptions.value.find(p => p.id === providerId)
  return found ? found.name : providerId
}

/** 获取能力标签文本 */

/** 格式化 token 数 */
function formatTokenCount(count: number): string {
  if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
  if (count >= 1000) return `${Math.round(count / 1000)}K`
  return String(count)
}

/** 恢复某字段跟随目录（取消人工覆盖） */
async function handleResetOverride(row: Model, field: ResettableOverrideField) {
  try {
    const updated = await resetModelOverride(row.id, field)
    Object.assign(row, updated)
    ElMessage.success('已恢复跟随目录')
    loadModels()
  } catch {
    ElMessage.error('恢复跟随目录失败')
  }
}

/** 切换模型启用状态 */
async function handleToggleModel(row: Model, enabled: boolean) {
  try {
    await updateModel(row.id, {
      name: row.name,
      provider_id: row.provider_id,
      model_type: row.model_type,
      upstream_model_id: row.upstream_model_id,
      description: row.description,
      context_length: row.context_length,
      max_output_tokens: row.max_output_tokens,
      input_price: row.input_price,
      output_price: row.output_price,
      weight: row.weight
    })
    row.enabled = enabled
  } catch {
    ElMessage.error('状态切换失败')
  }
}

/** 删除模型 */
async function handleDelete(row: Model) {
  try {
    await deleteModel(row.id)
    ElMessage.success('删除成功')
    loadModels()
  } catch {
    ElMessage.error('删除失败')
  }
}

/** 打开新建对话框 */
function openCreateDialog() {
  editingModel.value = null
  dialogVisible.value = true
}

/** 打开编辑对话框 */
function openEditDialog(row: Model) {
  editingModel.value = row
  dialogVisible.value = true
}

/** 对话框保存成功回调 */
function onDialogSaved() {
  dialogVisible.value = false
  loadModels()
}

/** 表格多选变化 */
function handleSelectionChange(selection: Model[]) {
  selectedModels.value = selection
}

/** 批量设置标签成功回调 */
function onBatchTagSaved() {
  batchTagDialogVisible.value = false
  loadModels()
}

/** 模型检测设置保存成功 */
function onModelCheckSettingsSaved() {
  modelCheckSettingsVisible.value = false
  loadModels()
}

/** 批量启用模型 */
async function handleBatchEnable() {
  try {
    await ElMessageBox.confirm(
      `确定启用选中的 ${selectedModels.value.length} 个模型？`,
      '批量启用',
      { confirmButtonText: '确定', cancelButtonText: '取消', type: 'info' }
    )
    const { updated } = await batchUpdateModelStatus(selectedModelIds.value, true)
    ElMessage.success(`已启用 ${updated} 个模型`)
    loadModels()
  } catch {
    // 用户取消或请求失败
  }
}

/** 批量禁用模型 */
async function handleBatchDisable() {
  try {
    await ElMessageBox.confirm(
      `确定禁用选中的 ${selectedModels.value.length} 个模型？`,
      '批量禁用',
      { confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning' }
    )
    const { updated } = await batchUpdateModelStatus(selectedModelIds.value, false)
    ElMessage.success(`已禁用 ${updated} 个模型`)
    loadModels()
  } catch {
    // 用户取消或请求失败
  }
}

/** 批量删除模型 */
async function handleBatchDelete() {
  try {
    await ElMessageBox.confirm(
      `确定删除选中的 ${selectedModels.value.length} 个模型？此操作不可恢复`,
      '批量删除',
      { confirmButtonText: '确定', cancelButtonText: '取消', type: 'error' }
    )
    const { deleted } = await batchDeleteModels(selectedModelIds.value)
    ElMessage.success(`已删除 ${deleted} 个模型`)
    loadModels()
  } catch {
    // 用户取消或请求失败
  }
}

/** 检测单个模型 */
async function handleCheckModel(row: Model) {
  checkingModelId.value = row.id
  try {
    const result = await checkModel(row.id)
    if (result.success) {
      ElMessage.success(`${row.name} 检测通过 (${result.response_time}ms)`)
    } else {
      ElMessage.warning(`${row.name} 检测失败: ${result.error}`)
    }
    loadModels()
  } catch {
    ElMessage.error('检测请求失败')
  } finally {
    checkingModelId.value = ''
  }
}

/** 缩短错误信息为关键词 */
function shortenError(error: string): string {
  if (!error) return ''
  const lower = error.toLowerCase()
  if (lower.includes('timeout') || lower.includes('超时')) return '连接超时'
  if (lower.includes('401') || lower.includes('unauthorized') || lower.includes('invalid key')) return 'Key无效'
  if (lower.includes('403') || lower.includes('forbidden')) return '无权限'
  if (lower.includes('404') || lower.includes('not found')) return '地址无效'
  if (lower.includes('connect') || lower.includes('dial') || lower.includes('refused')) return '连接失败'
  if (lower.includes('dns') || lower.includes('no such host')) return 'DNS解析失败'
  if (lower.includes('502') || lower.includes('bad gateway')) return '上游异常'
  if (lower.includes('503') || lower.includes('unavailable')) return '服务不可用'
  if (lower.includes('model')) return '模型不可用'
  return error.length > 10 ? error.slice(0, 10) + '...' : error
}

onMounted(() => {
  loadProviders()
  loadAllTags()
  loadModels()
})
</script>

<style scoped>
.model-page {
  padding: 24px;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0;
}

.header-actions {
  display: flex;
  align-items: center;
}

.mono-text {
  font-family: monospace;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.text-muted {
  color: var(--el-text-color-secondary);
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.capability-row {
  display: inline-flex;
  gap: 4px;
  font-size: 14px;
}

.dep-badge {
  margin-left: 6px;
}

.override-badge {
  margin-left: 6px;
}

.restore-btn {
  margin-left: 4px;
  padding: 0;
  height: auto;
  font-size: 12px;
}

.health-cell {
  display: flex;
  align-items: center;
  justify-content: center;
}

.health-inner {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: default;
}

.health-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.health-ok { background: #27AE60; }
.health-warn { background: #EAB308; }
.health-error { background: #E74C3C; }
.health-unknown { background: #CBD5E1; }

.health-text {
  font-size: 12px;
  font-weight: 500;
}

.health-ok-text { color: #27AE60; }
.health-warn-text { color: #EAB308; }
.health-error-text { color: #E74C3C; }
</style>
