<template>
  <div class="key-list-page">
    <div class="page-header">
      <h2 class="page-title">API 密钥</h2>
      <div class="header-actions">
        <el-button type="primary" @click="showCreateDialog = true">创建 Key</el-button>
      </div>
    </div>

    <el-table v-loading="loading" :data="keys" border stripe :default-sort="{ prop: 'createdAt', order: 'descending' }">
      <el-table-column prop="name" label="名称" min-width="120" show-overflow-tooltip />

      <el-table-column label="API 密钥" min-width="180">
        <template #default="{ row }">
          <div class="key-cell">
            <span class="key-masked">{{ row.maskedKey }}</span>
            <el-button size="small" text type="primary" @click="copyFullKey(row)">复制</el-button>
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

      <el-table-column label="创建时间" width="170" sortable>
        <template #default="{ row }">{{ formatDate(row.createdAt) }}</template>
      </el-table-column>

      <el-table-column label="活跃时间" width="170">
        <template #default="{ row }">{{ formatDate(row.lastUsedAt) }}</template>
      </el-table-column>

      <el-table-column label="操作" width="280" fixed="right" align="center">
        <template #default="{ row }">
          <el-button size="small" text type="primary" @click="handleUseKey(row)">使用密钥</el-button>
          <el-button size="small" text type="primary" @click="handleCcsImport(row)">导入 CCSwitch</el-button>
          <el-button size="small" text type="warning" @click="handleReset(row)">重置</el-button>
          <el-button size="small" text type="danger" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>

      <template #empty>
        <el-empty description="暂无 API Key" />
      </template>
    </el-table>

    <div class="pagination-wrapper" v-if="total > pageSize">
      <el-pagination v-model:current-page="currentPage" :page-size="pageSize" :total="total" layout="prev, pager, next" @current-change="loadKeys" />
    </div>

    <KeyCreateDialog v-model="showCreateDialog" @success="onKeyCreated" />

    <UseKeyModal
      v-model="showUsage"
      :api-key-id="usageKey?.id ?? null"
      :key-hint="usageKey?.keyHint ?? ''"
    />

    <el-dialog v-model="showCcsSelect" title="导入 CCSwitch" width="480px" destroy-on-close>
      <p class="ccs-desc">选择要导入的目标客户端：</p>
      <div class="ccs-client-grid">
        <div
          class="ccs-client-card"
          :class="{ active: selectedCcsClient === 'claude' }"
          @click="selectedCcsClient = 'claude'"
        >
          <span class="ccs-client-name">Claude Code</span>
        </div>
        <div
          class="ccs-client-card"
          :class="{ active: selectedCcsClient === 'codex' }"
          @click="selectedCcsClient = 'codex'"
        >
          <span class="ccs-client-name">Codex CLI</span>
        </div>
        <div
          class="ccs-client-card"
          :class="{ active: selectedCcsClient === 'opencode' }"
          @click="selectedCcsClient = 'opencode'"
        >
          <span class="ccs-client-name">OpenCode</span>
        </div>
        <div
          class="ccs-client-card"
          :class="{ active: selectedCcsClient === 'gemini' }"
          @click="selectedCcsClient = 'gemini'"
        >
          <span class="ccs-client-name">Gemini CLI</span>
        </div>
      </div>
      <template #footer>
        <el-button @click="showCcsSelect = false">取消</el-button>
        <el-button type="primary" @click="confirmCcsImport">导入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

function formatDate(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

import { ElMessage, ElMessageBox } from 'element-plus'
import { listKeys, disableKey, enableKey, deleteKey, resetKey, revealKey } from '@/services/control-plane'
import { mapKeysResponse, mapCreateKeyResponse } from '@/types/api-key'
import type { ApiKey } from '@/types/api-key'
import KeyCreateDialog from '@/components/key/KeyCreateDialog.vue'
import UseKeyModal from '@/components/key/UseKeyModal.vue'
import { executeCcsImport } from '@/utils/ccs-import'
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

const showCcsSelect = ref(false)
const selectedCcsClient = ref('claude')
const pendingCcsRow = ref<ApiKey | null>(null)

const keyCache = useKeyCacheStore()

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
    await navigator.clipboard.writeText(result.key)
    ElMessage.success('完整 Key 已复制到剪贴板')
  } catch {
    ElMessage.warning('获取 Key 失败，请重试')
  }
}

function handleUseKey(row: ApiKey) {
  usageKey.value = row
  showUsage.value = true
}

function handleCcsImport(row: ApiKey) {
  pendingCcsRow.value = row
  selectedCcsClient.value = 'claude'
  showCcsSelect.value = true
}

async function confirmCcsImport() {
  const row = pendingCcsRow.value
  if (!row) return
  try {
    const result = await revealKey(row.id)
    executeCcsImport(result.key, selectedCcsClient.value)
    showCcsSelect.value = false
    pendingCcsRow.value = null
  } catch {
    ElMessage.warning('获取 Key 失败，请重试')
  }
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
    try {
      await ElMessageBox.confirm('是否一键导入密钥到 CCSwitch 桌面客户端？', '导入 CCSwitch', { confirmButtonText: '导入', cancelButtonText: '跳过' })
      executeCcsImport(mapped.key)
    } catch { /* user skipped */ }
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

.text-muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.ccs-desc {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0 0 16px;
}

.ccs-client-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 12px;
}

.ccs-client-card {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  border: 2px solid var(--el-border-color-light);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.2s;
}

.ccs-client-card:hover {
  border-color: var(--el-color-primary-light-5);
  background: var(--el-color-primary-light-9);
}

.ccs-client-card.active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.ccs-client-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

@media (max-width: 900px) {
  .page-header { flex-direction: column; align-items: flex-start; gap: 12px; }
  .header-actions { width: 100%; justify-content: flex-start; }
}
</style>