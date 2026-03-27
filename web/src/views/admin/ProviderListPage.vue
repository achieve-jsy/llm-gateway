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
          class="toolbar-field toolbar-field-sm"
          @input="handleFilterChange"
        />
        <el-select
          v-model="filterDefinition"
          placeholder="按类型筛选"
          clearable
          class="toolbar-field toolbar-field-sm"
          @change="handleFilterChange"
        >
          <!-- 选项由后端 ProviderDefinition 动态加载，Seedance 类型（如 seedance-text2video）会自动出现 -->
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
          class="toolbar-field toolbar-field-sm"
          @change="handleFilterChange"
        >
          <el-option
            v-for="tag in tagOptions"
            :key="tag"
            :label="tag"
            :value="tag"
          />
        </el-select>
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
        </template>
      </el-table-column>

      <el-table-column label="Endpoint" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          <span class="mono-text">{{ row.endpoint }}</span>
        </template>
      </el-table-column>

      <el-table-column label="凭证" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          <template v-if="parseCredentials(row.credentials)">
            <div class="cred-tags">
              <div
                v-for="(val, key) in parseCredentials(row.credentials)"
                :key="key"
                class="cred-item"
              >
                <el-icon :size="13" class="cred-icon"><Key /></el-icon>
                <span class="cred-key">{{ key }}</span>
                <span class="cred-sep">:</span>
                <span v-if="val" class="cred-masked">{{ val }}</span>
                <span v-else class="cred-empty">未配置</span>
              </div>
            </div>
          </template>
          <template v-else>
            <div class="cred-plain">
              <el-icon :size="13" class="cred-icon"><Lock /></el-icon>
              <span v-if="row.credentials" class="mono-text cred-masked">{{ row.credentials }}</span>
              <span v-else class="cred-empty">未配置</span>
            </div>
          </template>
        </template>
      </el-table-column>

      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }">
          <el-tag v-if="row.status === ProviderStatus.ENABLED" type="success" size="small">启用</el-tag>
          <el-tag v-else-if="row.status === ProviderStatus.MANUALLY_DISABLED" type="info" size="small">已禁用</el-tag>
          <el-tag v-else type="info" size="small">未知</el-tag>
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
          {{ row.model_count ?? 0 }}
        </template>
      </el-table-column>

      <el-table-column label="标签" min-width="120">
        <template #default="{ row }">
          <template v-if="row.tag">
            <el-tag
              v-for="t in row.tag.split(',').map((s: string) => s.trim()).filter(Boolean)"
              :key="t"
              size="small"
              style="margin-right: 4px; margin-bottom: 2px"
            >{{ t }}</el-tag>
          </template>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column label="创建时间" width="170" align="center">
        <template #default="{ row }">
          <span>{{ formatDateTime(row.created_at) }}</span>
        </template>
      </el-table-column>

      <el-table-column label="更新时间" width="170" align="center">
        <template #default="{ row }">
          <span>{{ formatDateTime(row.updated_at) }}</span>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="260" align="center">
        <template #default="{ row }">
          <el-button text type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
          <el-button
            text
            type="primary"
            size="small"
            :loading="testingId === row.id"
            @click="handleTest(row)"
          >
            测试
          </el-button>
          <el-button
            text
            type="primary"
            size="small"
            :loading="syncingId === row.id"
            @click="handleSync(row)"
          >
            同步
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

  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Lock, Key } from '@element-plus/icons-vue'
import {
  getProviders,
  getProviderDefinitions,
  deleteProvider,
  updateProvider,
  syncModels,
  testProvider,
} from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'
import type { Provider, ProviderDefinition } from '@/types/provider'
import { ProviderStatus } from '@/types/provider'
import type { SyncResult } from '@/types/sync'
import ProviderDialog from '@/components/admin/ProviderDialog.vue'
import SyncModelsDialog from '@/components/admin/SyncModelsDialog.vue'

const providers = ref<Provider[]>([])
const definitions = ref<ProviderDefinition[]>([])
const tagOptions = ref<string[]>([])
const filterTag = ref('')
const filterName = ref('')
const filterDefinition = ref('')
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const dialogVisible = ref(false)
const editingProvider = ref<Provider | null>(null)

const syncDialogVisible = ref(false)
const syncProviderId = ref('')
const syncProviderName = ref('')
const syncResult = ref<SyncResult | null>(null)
const syncingId = ref('')
const testingId = ref('')

/** 从分页结果中提取标签选项（消除重复 fetch 全量渠道的 N+1） */
function extractTagOptionsFromProviders(list: Provider[]) {
  const tags = new Set<string>()
  list.forEach(p => {
    if (p.tag) {
      p.tag.split(',').map(t => t.trim()).filter(Boolean).forEach(t => tags.add(t))
    }
  })
  tagOptions.value = Array.from(tags)
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
    extractTagOptionsFromProviders(result.data)
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

/** 获取渠道定义显示名 */
function getDefinitionName(definitionId: string): string {
  const def = definitions.value.find(d => d.id === definitionId)
  return def?.display_name || definitionId
}

/** 解析脱敏凭证 JSON 为 key-value map，失败返回 null */
function parseCredentials(credentials: string): Record<string, string> | null {
  if (!credentials || credentials === '****') return null
  try {
    const parsed = JSON.parse(credentials)
    if (typeof parsed === 'object' && parsed !== null) {
      return parsed as Record<string, string>
    }
  } catch {}
  return null
}

function formatDateTime(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}


/** 筛选变化 */
function handleFilterChange() {
  currentPage.value = 1
  applyFilter()
}


/** 测试渠道连通性（与定时健康检查同源探针） */
async function handleTest(row: Provider) {
  testingId.value = row.id
  try {
    const result = await testProvider(row.id, false)
    if (result.success) {
      const scope = result.message ? `（${result.message}）` : ''
      ElMessage.success(`连通成功 ${result.response_time}ms${scope}`)
    } else {
      ElMessage.error(result.error || result.message || '连通失败')
    }
  } catch (e: any) {
    ElMessage.error(resolveErrorMessage(e) || '测试失败')
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
  } catch (e: any) {
    // 已知错误码由全局拦截器处理，只处理未注册的特殊码
    if (!resolveErrorMessage(e) && e?.response?.status === 409) {
      ElMessage.error('请先删除关联模型')
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
}

onMounted(() => {
  Promise.all([loadDefinitions(), loadProviders()])
})
</script>

<style scoped>
.provider-page {
  padding: 0;
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
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.toolbar-field {
  width: 180px;
}

.toolbar-field-sm {
  width: 160px;
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


.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

@media (max-width: 900px) {
  .page-header {
    align-items: flex-start;
    flex-direction: column;
    gap: 12px;
  }

  .header-actions {
    width: 100%;
    justify-content: flex-start;
  }
}


.cred-tags {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cred-item {
  display: flex;
  align-items: center;
  gap: 4px;
  line-height: 1;
}

.cred-icon {
  color: var(--el-color-info);
  flex-shrink: 0;
}

.cred-key {
  font-size: 12px;
  color: var(--el-text-color-primary);
}

.cred-sep {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.cred-masked {
  color: var(--el-text-color-secondary);
  font-family: monospace;
  font-size: 12px;
}

.cred-empty {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
}

.cred-plain {
  display: flex;
  align-items: center;
  gap: 4px;
}

.cred-lock {
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
}
</style>
