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
            <el-button size="small" text type="primary" @click="copyHint(row)">复制</el-button>
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

      <el-table-column label="操作" width="200" fixed="right" align="center">
        <template #default="{ row }">
          <el-button size="small" text type="primary" @click="handleUseKey(row)">使用密钥</el-button>
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

    <el-dialog v-model="showUsage" title="使用密钥" width="680px" destroy-on-close>
      <template v-if="usageKey">
        <el-tabs v-model="usageTab">
          <el-tab-pane label="cURL" name="curl">
            <pre class="code-block"><code>curl https://your-gateway/v1/chat/completions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hi"}]}'</code></pre>
          </el-tab-pane>
          <el-tab-pane label="Python SDK" name="python">
            <pre class="code-block"><code>from openai import OpenAI
client = OpenAI(
    base_url="https://your-gateway/v1",
    api_key="YOUR_API_KEY"
)
response = client.chat.completions.create(
    model="gpt-4",
    messages=[{"role":"user","content":"Hi"}]
)</code></pre>
          </el-tab-pane>
          <el-tab-pane label="Codex CLI" name="codex">
            <p class="usage-tip">将以下内容写入 <code>~/.codex/config.toml</code>：</p>
            <pre class="code-block"><code>model_provider = "OpenAI"
model = "gpt-4"

[model_providers.OpenAI]
name = "OpenAI"
base_url = "https://your-gateway/v1"
wire_api = "responses"
requires_openai_auth = true</code></pre>
            <p class="usage-tip">将以下内容写入 <code>~/.codex/auth.json</code>：</p>
            <pre class="code-block"><code>{
  "OPENAI_API_KEY": "YOUR_API_KEY"
}</code></pre>
          </el-tab-pane>
          <el-tab-pane label="OpenCode" name="opencode">
            <p class="usage-tip">设置环境变量：</p>
            <pre class="code-block"><code>export OPENAI_API_KEY="YOUR_API_KEY"
export OPENAI_BASE_URL="https://your-gateway/v1"</code></pre>
          </el-tab-pane>
          <el-tab-pane label="CCSwitch" name="ccswitch">
            <p class="usage-tip">CCSwitch 桌面客户端支持一键导入密钥。创建或重置 Key 时会自动弹出导入选项。</p>
            <p class="usage-tip">如需手动配置，在 CCSwitch 中设置以下端点：</p>
            <pre class="code-block"><code>端点: {{ getBaseUrl() }}
API Key: 创建或重置 Key 时获取的完整密钥</code></pre>
          </el-tab-pane>
        </el-tabs>
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
import { listKeys, disableKey, enableKey, deleteKey, resetKey } from '@/services/control-plane'
import { mapKeysResponse, mapCreateKeyResponse } from '@/types/api-key'
import type { ApiKey } from '@/types/api-key'
import KeyCreateDialog from '@/components/key/KeyCreateDialog.vue'
import { executeCcsImport, getBaseUrl } from '@/utils/ccs-import'
import { useSettingsStore } from '@/stores/settings'

const loading = ref(false)
const keys = ref<ApiKey[]>([])
const currentPage = ref(1)
const total = ref(0)
const pageSize = 20
const showCreateDialog = ref(false)
const showUsage = ref(false)
const usageKey = ref<ApiKey | null>(null)
const usageTab = ref('curl')

async function loadKeys() {
  loading.value = true
  try {
    const response = await listKeys(currentPage.value, pageSize)
    keys.value = mapKeysResponse(response)
    total.value = response.data.total
  } catch (e: any) {
    ElMessage.error('加载 Key 列表失败')
  } finally {
    loading.value = false
  }
}

function copyHint(row: ApiKey) {
  navigator.clipboard.writeText(`sk-...${row.keyHint}`).then(() => ElMessage.success('已复制')).catch(() => ElMessage.warning('复制失败'))
}

function handleUseKey(row: ApiKey) {
  usageKey.value = row
  showUsage.value = true
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
    /* cancelled or failed, revert */
  }
}

async function handleReset(row: ApiKey) {
  try {
    await ElMessageBox.confirm(`重置后 Key「${row.name}」的旧密钥将立即失效，新密钥仅显示一次。确定重置？`, '确认重置', { type: 'warning' })
    const response = await resetKey(row.id)
    const mapped = mapCreateKeyResponse(response)
    await ElMessageBox.alert(
      `<p style="margin:0 0 8px;font-weight:600">新密钥（仅显示一次，请复制保存）</p><pre style="background:var(--el-fill-color-light);padding:10px;border-radius:6px;font-size:13px;margin:0">${mapped.key}</pre>`,
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
    ElMessage.success('已删除')
    await loadKeys()
  } catch { /* cancelled */ }
}

function onKeyCreated() {
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

.code-block {
  background: var(--el-fill-color-light);
  border-radius: 6px;
  padding: 12px 14px;
  font-size: 12.5px;
  line-height: 1.7;
  margin: 0;
  overflow-x: auto;
}

.usage-tip {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 12px 0 6px;
}

.usage-tip code {
  background: var(--el-fill-color);
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 12px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

@media (max-width: 900px) {
  .page-header { flex-direction: column; align-items: flex-start; gap: 12px; }
  .header-actions { width: 100%; justify-content: flex-start; }
}
</style>
