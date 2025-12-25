<template>
  <div class="key-list-page">
    <div class="page-header">
      <h2 class="page-title">API 密钥</h2>
      <div class="header-actions">
        <el-button type="primary" @click="showCreateDialog = true">创建 Key</el-button>
      </div>
    </div>

    <div class="key-table-wrap"><el-table v-loading="loading" :data="keys" border stripe :default-sort="{ prop: 'createdAt', order: 'descending' }">
      <el-table-column prop="name" label="名称" min-width="120" show-overflow-tooltip />

      <el-table-column label="API 密钥" min-width="180">
        <template #default="{ row }">
          <div class="key-cell">
            <span class="key-masked">{{ row.maskedKey }}</span>
            <el-button size="small" text type="primary" @click.stop="copyFullKey(row)">复制</el-button>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="我的模型" min-width="160">
        <template #default="{ row }">
          <div class="group-cell">
            <template v-if="row.accessAllGroups">
              <el-tag size="small" type="success" style="margin-right:4px;margin-bottom:2px">全部模型</el-tag>
            </template>
            <template v-else-if="row.boundGroups && row.boundGroups.length > 0">
              <el-tag
                v-for="g in row.boundGroups"
                :key="g.id"
                size="small"
                type="info"
                style="margin-right:4px;margin-bottom:2px"
              >{{ g.name }}</el-tag>
            </template>
            <span v-else class="text-muted" style="margin-right:4px">未分配</span>
            <el-button size="small" text type="primary" @click.stop="openGroupDialog(row)">管理</el-button>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="过期时间" width="170">
        <template #default="{ row }">
          <span v-if="row.expiresAt">{{ formatDate(row.expiresAt) }}</span>
          <span v-else class="text-muted">永久有效</span>
        </template>
      </el-table-column>

      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }">
          <el-switch
            v-model="row.enabled"
            size="small"
            inline-prompt
            active-text="活跃"
            inactive-text="禁用"
            @change="(val: boolean) => handleToggle(row, val)"
          />
        </template>
      </el-table-column>

      <el-table-column label="日志记录" width="110" align="center">
        <template #header>
          <span>日志记录
            <el-tooltip content="关闭后该 Key 的请求不写入调用日志，但耗时和 Token 仍会计算" placement="top">
              <el-icon style="vertical-align:middle;cursor:help"><QuestionFilled /></el-icon>
            </el-tooltip>
          </span>
        </template>
        <template #default="{ row }">
          <el-switch
            v-model="row.logEnabled"
            size="small"
            inline-prompt
            active-text="记录"
            inactive-text="关闭"
            :active-color="'#67c23a'"
            :inactive-color="'#909399'"
            @change="(val: boolean) => handleToggleLog(row, val)"
          />
        </template>
      </el-table-column>

      <el-table-column label="创建时间" width="170" sortable>
        <template #default="{ row }">{{ formatDate(row.createdAt) }}</template>
      </el-table-column>

      <el-table-column label="活跃时间" width="170">
        <template #default="{ row }">{{ formatDate(row.lastUsedAt) }}</template>
      </el-table-column>

      <el-table-column label="操作" width="340" fixed="right" align="center">
        <template #default="{ row }">
          <el-button size="small" text type="primary" @click.stop="handleUseKey(row)">使用密钥</el-button>
          <el-button size="small" text type="primary" @click.stop="handleOpenCodeConfig(row)">配置 OpenCode</el-button>
          <el-button size="small" text type="warning" @click.stop="handleReset(row)">重置</el-button>
          <el-button size="small" text type="danger" @click.stop="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>

      <template #empty>
        <el-empty description="暂无 API Key" />
      </template>
    </el-table></div>

    <div class="pagination-wrapper" v-if="total > pageSize">
      <el-pagination v-model:current-page="currentPage" :page-size="pageSize" :total="total" layout="prev, pager, next" @current-change="loadKeys" />
    </div>

    <KeyCreateDialog v-model="showCreateDialog" @success="onKeyCreated" />

    <UseKeyModal
      v-model="showUsage"
      :api-key-id="usageKey?.id ?? null"
      :key-hint="usageKey?.keyHint ?? ''"
    />

    <!-- 组绑定管理弹窗 -->
    <el-dialog v-model="showGroupDialog" title="管理我的模型" width="500px" destroy-on-close @opened="loadGroupDialogData">
      <div v-if="editGroupKey">
        <div class="group-dialog-section">
          <h4>当前可用模型</h4>
          <template v-if="editGroupKey.accessAllGroups">
            <el-tag size="large" type="success" closable @close="handleAccessAllToggle(false)">全部模型</el-tag>
          </template>
          <div v-if="!editGroupKey.accessAllGroups && editGroupKey.boundGroups && editGroupKey.boundGroups.length > 0" class="bound-tags">
            <el-tag
              v-for="g in editGroupKey.boundGroups"
              :key="g.id"
              closable
              size="large"
              type="info"
              @close="removeGroup(g)"
            >{{ g.name }}</el-tag>
          </div>
          <span v-if="!editGroupKey.accessAllGroups && (!editGroupKey.boundGroups || editGroupKey.boundGroups.length === 0)" class="text-muted">未分配任何模型</span>
        </div>
        <div class="group-dialog-section">
          <h4>添加模型</h4>
          <el-select
            v-model="addGroupIds"
            multiple
            filterable
            placeholder="选择要添加的模型"
            style="width:100%"
            :loading="groupLoading"
            @change="handleAddGroupChange"
          >
            <el-option
              key="__all__"
              label="全部模型（不限制）"
              value="__all__"
              :disabled="editGroupKey.accessAllGroups"
            />
            <el-option
              v-for="g in availableAddGroups"
              :key="g.id"
              :label="g.name"
              :value="g.id"
              :disabled="editGroupKey.accessAllGroups"
            />
          </el-select>
        </div>
      </div>
      <template #footer>
        <el-button @click="showGroupDialog = false">取消</el-button>
        <el-button type="primary" :loading="groupSaving" @click="saveGroupChanges">保存</el-button>
      </template>
    </el-dialog>

    <OpenCodeConfigDialog
      v-model:visible="showOpenCodeConfig"
      :api-key-id="openCodeConfigKeyId"
      :masked-key="openCodeConfigMaskedKey"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'

function formatDate(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

import { ElMessage, ElMessageBox } from 'element-plus'
import { listKeys, disableKey, enableKey, deleteKey, resetKey, revealKey, updateKeyGroups, getAvailableGroups, toggleAccessAllGroups, toggleLogEnabled } from '@/services/control-plane'
import { mapKeysResponse, mapCreateKeyResponse, type BoundGroupInfo } from '@/types/api-key'
import type { ApiKey, AvailableGroup } from '@/types/api-key'
import KeyCreateDialog from '@/components/key/KeyCreateDialog.vue'
import UseKeyModal from '@/components/key/UseKeyModal.vue'
import OpenCodeConfigDialog from '@/components/key/OpenCodeConfigDialog.vue'
import { copyToClipboard } from '@/utils/clipboard'
import { useSettingsStore } from '@/stores/settings'
import { useKeyCacheStore } from '@/stores/key-cache'

const loading = ref(false)
const keys = ref<ApiKey[]>([])
const currentPage = ref(1)
const total = ref(0)
const pageSize = 20
const showCreateDialog = ref(false)
const showUsage = ref(false)
const usageKey = ref<ApiKey | null>(null)

const showOpenCodeConfig = ref(false)
const openCodeConfigKeyId = ref('')
const openCodeConfigMaskedKey = ref('')

// Group binding management
const showGroupDialog = ref(false)
const editGroupKey = ref<ApiKey | null>(null)
const addGroupIds = ref<(string | number)[]>([])
const removedGroupIds = ref<number[]>([])
const availableAddGroups = ref<AvailableGroup[]>([])
const groupLoading = ref(false)
const groupSaving = ref(false)
// 万能模型组待保存状态（null = 未改动，true/false = 待提交）
const pendingAccessAll = ref<boolean | null>(null)

const keyCache = useKeyCacheStore()

function strategyLabel(strategy: string): string {
  const map: Record<string, string> = { round_robin: '轮询', priority: '优先级', weighted: '加权' }
  return map[strategy] || strategy
}

async function loadKeys() {
  loading.value = true
  try {
    const response = await listKeys(currentPage.value, pageSize)
    keys.value = mapKeysResponse(response)
    total.value = response.data.total
  } catch {
    ElMessage.error('加载 Key 列表失败')
  } finally {
    loading.value = false
  }
}

async function copyFullKey(row: ApiKey) {
  try {
    const result = await revealKey(row.id)
    await copyToClipboard(result.key)
    ElMessage.success('完整 Key 已复制到剪贴板')
  } catch {
    ElMessage.warning('获取 Key 失败，请重试')
  }
}

function handleUseKey(row: ApiKey) {
  usageKey.value = row
  showUsage.value = true
}

function handleOpenCodeConfig(row: ApiKey) {
  openCodeConfigKeyId.value = row.id
  openCodeConfigMaskedKey.value = row.maskedKey
  showOpenCodeConfig.value = true
}

async function handleToggle(row: ApiKey, enabled: boolean) {
  try {
    if (enabled) {
      await enableKey(row.id)
      ElMessage.success('已启用')
    } else {
      await ElMessageBox.confirm(`确定要禁用 Key「${row.name}」？禁用后该 Key 将立即无法用于 API 调用。`, '确认禁用', { type: 'warning' })
      await disableKey(row.id)
      ElMessage.success('已禁用')
    }
    await loadKeys()
  } catch {
    row.enabled = !enabled
  }
}

async function handleToggleLog(row: ApiKey, logEnabled: boolean) {
  try {
    await toggleLogEnabled(row.id, logEnabled)
    ElMessage.success(logEnabled ? '已开启日志记录' : '已关闭日志记录')
  } catch {
    row.logEnabled = !logEnabled
    ElMessage.error('操作失败')
  }
}

async function handleReset(row: ApiKey) {
  try {
    await ElMessageBox.confirm(`重置后 Key「${row.name}」的旧密钥将立即失效，新密钥仅显示一次。确定重置？`, '确认重置', { type: 'warning' })
    const response = await resetKey(row.id)
    const mapped = mapCreateKeyResponse(response)
    keyCache.cacheKey(mapped.id, mapped.key)
    await ElMessageBox.alert(
      `<pre style="background:var(--el-fill-color-light);padding:10px;border-radius:6px;font-size:13px;margin:0;white-space:pre-wrap;word-break:break-all;max-width:100%;overflow:hidden">${mapped.key}</pre>`,
      '密钥已重置', { dangerouslyUseHTMLString: true, confirmButtonText: '已复制，关闭' }
    )
    await loadKeys()
  } catch { /* cancelled */ }
}

async function handleDelete(row: ApiKey) {
  try {
    await ElMessageBox.confirm(`确定删除 Key「${row.name}」？此操作不可恢复。`, '确认删除', { type: 'error' })
    await deleteKey(row.id)
    keyCache.clearKey(row.id)
    ElMessage.success('已删除')
    await loadKeys()
  } catch { /* cancelled */ }
}

function onKeyCreated(_keyId: string, _rawKey: string) {
  showCreateDialog.value = false
  currentPage.value = 1
  loadKeys()
}

// Group binding management
function openGroupDialog(row: ApiKey) {
  editGroupKey.value = { ...row, boundGroups: [...(row.boundGroups || [])] }
  addGroupIds.value = []
  removedGroupIds.value = []
  pendingAccessAll.value = null
  showGroupDialog.value = true
}

async function loadGroupDialogData() {
  groupLoading.value = true
  try {
    const resp = await getAvailableGroups()
    const all = resp.data.groups || []
    const boundIds = new Set((editGroupKey.value?.boundGroups || []).map((g: BoundGroupInfo) => g.id))
    availableAddGroups.value = all.filter((g: AvailableGroup) => !boundIds.has(g.id))
  } catch {
    ElMessage.error('加载可用模型列表失败')
  } finally {
    groupLoading.value = false
  }
}

function removeGroup(group: BoundGroupInfo) {
  if (!editGroupKey.value) return
  editGroupKey.value.boundGroups = (editGroupKey.value.boundGroups || []).filter((g) => g.id !== group.id)
  removedGroupIds.value.push(group.id)
  addGroupIds.value = addGroupIds.value.filter((id) => id !== group.id)
  const alreadyInList = availableAddGroups.value.some(g => g.id === group.id)
  if (!alreadyInList) {
    availableAddGroups.value = [...availableAddGroups.value, { id: group.id, name: group.name, description: '', strategy: '' }]
  }
}

function handleAddGroupChange(values: (string | number)[]) {
  if (values.includes('__all__')) {
    addGroupIds.value = []
    pendingAccessAll.value = true
    if (editGroupKey.value) {
      const boundGroups = editGroupKey.value.boundGroups || []
      for (const g of boundGroups) {
        if (!removedGroupIds.value.includes(g.id)) {
          removedGroupIds.value.push(g.id)
        }
        if (!availableAddGroups.value.some(a => a.id === g.id)) {
          availableAddGroups.value = [...availableAddGroups.value, { id: g.id, name: g.name, description: '', strategy: '' }]
        }
      }
      editGroupKey.value.boundGroups = []
      editGroupKey.value.accessAllGroups = true
    }
  }
}

function handleAccessAllToggle(value: boolean) {
  if (!editGroupKey.value) return
  pendingAccessAll.value = value
  editGroupKey.value.accessAllGroups = value
}

async function saveGroupChanges() {
  if (!editGroupKey.value) return
  groupSaving.value = true
  try {
    if (pendingAccessAll.value !== null) {
      await toggleAccessAllGroups(editGroupKey.value.id, pendingAccessAll.value)
    }
    const add = addGroupIds.value.filter((v): v is number => typeof v === 'number')
    const remove = removedGroupIds.value
    if (add.length > 0 || remove.length > 0) {
      await updateKeyGroups(editGroupKey.value.id, add, remove)
    }
    ElMessage.success('我的模型已更新')
    showGroupDialog.value = false
    await loadKeys()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '更新失败')
  } finally {
    groupSaving.value = false
  }
}

onMounted(() => {
  loadKeys()
  useSettingsStore().loadPublicSettings()
})
</script>

<style scoped>
.key-list-page { padding: 0; }

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size, 22px);
  font-weight: var(--font-title-weight, 800);
  margin: 0;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.key-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}

.key-masked {
  font-family: 'SF Mono', 'JetBrains Mono', monospace;
  font-size: 13px;
  background: var(--el-fill-color-light);
  padding: 2px 10px;
  border-radius: 4px;
  color: var(--el-text-color-secondary);
}

.group-cell {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

.text-muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.group-dialog-section {
  margin-bottom: 20px;
}

.group-dialog-section h4 {
  margin: 0 0 8px 0;
  font-size: 14px;
  color: var(--el-text-color-primary);
}

.bound-tags {
  min-height: 32px;
}

.key-table-wrap { width: 100%; }

@media (max-width: 900px) {
  .page-header { flex-direction: column; align-items: flex-start; gap: 12px; }
  .header-actions { width: 100%; justify-content: flex-start; }
  .key-table-wrap { overflow-x: auto; }
  .key-table-wrap :deep(.el-table) { min-width: 820px; }
}

@media (max-width: 480px) {
  .header-actions { flex-wrap: wrap; gap: 8px; }
  .header-actions .el-button { width: 100%; }
  :deep(.el-dialog) { width: 92% !important; max-width: 92vw !important; }
}
</style>