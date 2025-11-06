<template>
  <div class="provider-page">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <h2 class="page-title">渠道管理</h2>
      <div class="header-actions">
        <el-input
          v-model="filterName"
          placeholder="搜索名称"
          clearable
          style="width: 160px; margin-right: 12px"
          @input="handleFilterChange"
        />
        <el-select
          v-model="filterDefinition"
          placeholder="按类型筛选"
          clearable
          style="width: 160px; margin-right: 12px"
          @change="handleFilterChange"
        >
          <el-option
            v-for="def in definitions"
            :key="def.id"
            :label="def.display_name"
            :value="def.id"
          />
        </el-select>
        <el-select
          v-model="filterTag"
          placeholder="按标签筛选"
          clearable
          style="width: 160px; margin-right: 12px"
          @change="handleFilterChange"
        >
          <el-option
            v-for="tag in tagOptions"
            :key="tag"
            :label="tag"
            :value="tag"
          />
        </el-select>
        <el-tooltip content="检测设置" placement="top">
          <el-button text type="info" @click="healthCheckSettingsVisible = true">
            <el-icon :size="18"><Setting /></el-icon>
          </el-button>
        </el-tooltip>
        <el-button type="primary" @click="openCreateDialog">新建渠道</el-button>
      </div>
    </div>

    <!-- 渠道列表表格 -->
    <el-table :data="providers" border stripe>
      <el-table-column label="名称" min-width="140">
        <template #default="{ row }">
          <el-tooltip v-if="row.remark" :content="row.remark" placement="top">
            <span class="name-cell">{{ row.name }}</span>
          </el-tooltip>
          <span v-else>{{ row.name }}</span>
        </template>
      </el-table-column>

      <el-table-column label="类型" min-width="120">
        <template #default="{ row }">
          <span>{{ getDefinitionName(row.definition_id) }}</span>
          <el-tag v-if="row.tag" size="small" style="margin-left: 6px">{{ row.tag }}</el-tag>
        </template>
      </el-table-column>

      <el-table-column label="Endpoint" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          <span class="mono-text">{{ row.endpoint }}</span>
        </template>
      </el-table-column>

      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }">
          <el-tag v-if="row.status === ProviderStatus.ENABLED" type="success" size="small">启用</el-tag>
          <el-tag v-else-if="row.status === ProviderStatus.MANUALLY_DISABLED" type="info" size="small">已禁用</el-tag>
          <el-tag v-else-if="row.status === ProviderStatus.AUTO_DISABLED" type="danger" size="small">自动禁用</el-tag>
          <el-tag v-else type="info" size="small">未知</el-tag>
        </template>
      </el-table-column>

      <el-table-column label="健康" width="160" align="center">
        <template #default="{ row }">
          <div class="health-cell">
            <template v-if="row.status === 3">
              <el-tooltip :content="row.last_check_error || '连续失败达阈值'" placement="top">
                <div class="health-inner">
                  <span class="health-dot health-error"></span>
                  <span class="health-text health-error-text">{{ shortenError(row.last_check_error) || '异常' }} {{ row.consecutive_failures }}/3</span>
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

      <el-table-column label="优先级" width="90" align="center">
        <template #default="{ row }">
          <el-input-number
            v-model="row.priority"
            :min="0"
            :max="9999"
            size="small"
            controls-position="right"
            style="width: 70px"
            @change="handleInlineUpdate(row)"
          />
        </template>
      </el-table-column>

      <el-table-column label="权重" width="90" align="center">
        <template #default="{ row }">
          <el-input-number
            v-model="row.weight"
            :min="0"
            :max="9999"
            size="small"
            controls-position="right"
            style="width: 70px"
            @change="handleInlineUpdate(row)"
          />
        </template>
      </el-table-column>

      <el-table-column label="模型数" width="80" align="center">
        <template #default="{ row }">
          {{ getModelCount(row.id) }}
        </template>
      </el-table-column>

      <el-table-column label="标签" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.tag" size="small">{{ row.tag }}</el-tag>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="260" align="center">
        <template #default="{ row }">
          <el-button text type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
          <el-button text type="warning" size="small" :loading="testingId === row.id" @click="handleTest(row)">测试</el-button>
          <el-button
            text
            type="primary"
            size="small"
            :loading="syncingId === row.id"
            @click="handleSync(row)"
          >
            同步
          </el-button>
          <el-button
            text
            :type="row.status === ProviderStatus.ENABLED ? 'info' : 'success'"
            size="small"
            @click="handleToggle(row)"
          >
            {{ row.status === ProviderStatus.ENABLED ? '禁用' : '启用' }}
          </el-button>
          <el-popconfirm
            title="确定删除该渠道？"
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
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="applyFilter"
        @size-change="handleSizeChange"
      />
    </div>

    <!-- 创建/编辑对话框 -->
    <ProviderDialog
      v-model:visible="dialogVisible"
      :provider="editingProvider"
      @saved="onDialogSaved"
      @close="dialogVisible = false"
    />

    <!-- 同步模型对话框 -->
    <SyncModelsDialog
      :visible="syncDialogVisible"
      :provider-id="syncProviderId"
      :provider-name="syncProviderName"
      :sync-result="syncResult"
      @close="syncDialogVisible = false"
      @saved="onSyncSaved"
    />

    <!-- 健康检测设置对话框 -->
    <HealthCheckSettingsDialog
      :visible="healthCheckSettingsVisible"
      @close="healthCheckSettingsVisible = false"
      @saved="onHealthCheckSettingsSaved"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Setting } from '@element-plus/icons-vue'
import {
  getProviders,
  getProviderDefinitions,
  getModels,
  deleteProvider,
  toggleProviderStatus,
  updateProvider,
  syncModels,
  checkProvider,
} from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'
import type { Provider, ProviderDefinition } from '@/types/provider'
import { ProviderStatus } from '@/types/provider'
import type { Model } from '@/types/model'
import type { SyncResult } from '@/types/sync'
import ProviderDialog from '@/components/admin/ProviderDialog.vue'
import SyncModelsDialog from '@/components/admin/SyncModelsDialog.vue'
import HealthCheckSettingsDialog from '@/components/admin/HealthCheckSettingsDialog.vue'

const providers = ref<Provider[]>([])
const definitions = ref<ProviderDefinition[]>([])
const allModels = ref<Model[]>([])
const tagOptions = ref<string[]>([])
const filterTag = ref('')
const filterName = ref('')
const filterDefinition = ref('')
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const testingId = ref('')

const dialogVisible = ref(false)
const editingProvider = ref<Provider | null>(null)

const syncDialogVisible = ref(false)
const syncProviderId = ref('')
const syncProviderName = ref('')
const syncResult = ref<SyncResult | null>(null)
const syncingId = ref('')

const healthCheckSettingsVisible = ref(false)

/** 加载所有标签选项（从全量渠道列表） */
async function loadTagOptions() {
  try {
    const result = await getProviders({ page: 1, pageSize: 1000 })
    const tags = new Set<string>()
    result.data.forEach(p => { if (p.tag) tags.add(p.tag) })
    tagOptions.value = Array.from(tags)
  } catch {
    // 静默失败
  }
}

/** 加载渠道列表 */
async function loadProviders() {
  try {
    const result = await getProviders({
      page: currentPage.value,
      pageSize: pageSize.value,
      name: filterName.value,
      definition_id: filterDefinition.value,
      tag: filterTag.value,
    })
    providers.value = result.data
    total.value = result.total
  } catch {
    ElMessage.error('加载渠道列表失败')
  }
}

/** 处理分页变化 */
function applyFilter() {
  loadProviders()
}

/** 每页条数变化 */
function handleSizeChange() {
  currentPage.value = 1
  applyFilter()
}

/** 加载定义列表 */
async function loadDefinitions() {
  try {
    definitions.value = await getProviderDefinitions()
  } catch {
    // 静默失败
  }
}

/** 加载模型列表（用于显示模型数量） */
async function loadModels() {
  try {
    const result = await getModels(undefined, { page: 1, pageSize: 200 })
    allModels.value = result.data
  } catch {
    // 静默失败
  }
}

/** 获取渠道定义显示名 */
function getDefinitionName(definitionId: string): string {
  const def = definitions.value.find(d => d.id === definitionId)
  return def?.display_name || definitionId
}

/** 获取模型数量 */
function getModelCount(providerId: string): number {
  return allModels.value.filter(m => m.provider_id === providerId).length
}

/** 响应时间样式 */
function responseTimeClass(ms: number): string {
  if (ms < 500) return 'rt-good'
  if (ms < 2000) return 'rt-warn'
  return 'rt-bad'
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
  // 截取前 10 个字符
  return error.length > 10 ? error.slice(0, 10) + '...' : error
}

/** 筛选变化 */
function handleFilterChange() {
  currentPage.value = 1
  applyFilter()
}

/** 切换启用/禁用状态 */
async function handleToggle(row: Provider) {
  const shouldEnable = row.status !== ProviderStatus.ENABLED
  try {
    await toggleProviderStatus(row.id, shouldEnable)
    ElMessage.success(shouldEnable ? '已启用' : '已禁用')
    loadProviders()
  } catch {
    ElMessage.error('状态切换失败')
  }
}

/** 测试渠道（连通性 + 转发） */
async function handleTest(row: Provider) {
  testingId.value = row.id
  try {
    const result = await checkProvider(row.id)
    if (result.success) {
      row.response_time = result.response_time
      ElMessage.success(`检测成功，响应 ${result.response_time}ms`)
    } else {
      const errMsg = result.error || '未知错误'
      if (result.auto_disabled) {
        ElMessage.error(`检测失败：${errMsg}（已自动禁用）`)
        loadProviders()
      } else {
        ElMessage.error(`检测失败：${errMsg}`)
      }
    }
  } catch {
    ElMessage.error('检测请求异常，请检查网络')
  } finally {
    testingId.value = ''
  }
}

/** 同步模型 */
async function handleSync(row: Provider) {
  syncingId.value = row.id
  try {
    const result = await syncModels(row.id)
    if (result.unsupported) {
      ElMessage.warning('该渠道类型不支持自动同步')
      return
    }
    syncProviderId.value = row.id
    syncProviderName.value = row.name
    syncResult.value = result
    syncDialogVisible.value = true
  } catch (e: any) {
    const code = e?.response?.data?.code
    if (code === 'sync_not_supported') {
      ElMessage.warning('该渠道类型不支持自动同步')
    } else {
      ElMessage.error('同步失败')
    }
  } finally {
    syncingId.value = ''
  }
}

/** 同步对话框保存成功 */
function onSyncSaved() {
  syncDialogVisible.value = false
  loadModels()
}

/** 检测设置保存成功 */
function onHealthCheckSettingsSaved() {
  healthCheckSettingsVisible.value = false
  loadProviders()
}

/** 行内编辑优先级/权重 */
async function handleInlineUpdate(row: Provider) {
  try {
    await updateProvider(row.id, {
      name: row.name,
      definition_id: row.definition_id,
      endpoint: row.endpoint,
      credentials: '',
      priority: row.priority,
      weight: row.weight
    })
  } catch {
    ElMessage.error('更新失败')
    loadProviders()
  }
}

/** 删除渠道 */
async function handleDelete(row: Provider) {
  try {
    await deleteProvider(row.id)
    ElMessage.success('删除成功')
    currentPage.value = 1
    loadProviders()
    loadTagOptions()
  } catch (e: any) {
    const friendly = resolveErrorMessage(e)
    if (friendly) {
      ElMessage.error(friendly)
    } else if (e?.response?.status === 409) {
      ElMessage.error('请先删除关联模型')
    } else {
      ElMessage.error('删除失败')
    }
  }
}

/** 打开新建对话框 */
function openCreateDialog() {
  editingProvider.value = null
  dialogVisible.value = true
}

/** 打开编辑对话框 */
function openEditDialog(row: Provider) {
  editingProvider.value = row
  dialogVisible.value = true
}

/** 对话框保存成功回调 */
function onDialogSaved() {
  dialogVisible.value = false
  currentPage.value = 1
  loadProviders()
  loadModels()
  loadTagOptions()
}

onMounted(() => {
  loadDefinitions()
  loadTagOptions()
  loadProviders()
  loadModels()
})
</script>

<style scoped>
.provider-page {
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

.name-cell {
  cursor: pointer;
  border-bottom: 1px dashed var(--el-text-color-secondary);
}

.mono-text {
  font-family: monospace;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.text-muted {
  color: var(--el-text-color-secondary);
}

.rt-good {
  color: #67C23A;
  font-weight: 500;
}

.rt-warn {
  color: #E6A23C;
  font-weight: 500;
}

.rt-bad {
  color: #F56C6C;
  font-weight: 500;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
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
