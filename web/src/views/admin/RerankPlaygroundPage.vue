<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getModels } from '@/services/control-plane'
import type { Model } from '@/types/model'

// ─── 类型 ──────────────────────────────────────────────────────────────────
type PlaygroundMode = 'session' | 'apikey'

interface GatewayModelInfo {
  id: string
  object: string
  created: number
  owned_by: string
}

interface RerankResultItem {
  index: number
  relevance_score: number
  document?: { text: string }
}

// ─── 状态 ──────────────────────────────────────────────────────────────────
const mode = ref<PlaygroundMode>('session')
const models = ref<Model[]>([])
const gatewayModelIds = ref<string[]>([])
const loadingModels = ref(false)

const config = reactive({
  model: '',
  apiBase: '',
  apiKey: '',
  query: '',
  documentText: '',
  topN: 10,
  returnDocuments: true,
})

const results = ref<RerankResultItem[]>([])
const respModel = ref('')
const respUsage = ref<{ total_tokens: number }>({ total_tokens: 0 })
const isRunning = ref(false)
const showAdvanced = ref(false)

const modelMetaMap = ref<Map<string, Model>>(new Map())

const effectiveApiBase = computed(() => config.apiBase || '/v1')

const modelOptions = computed(() => {
  if (mode.value === 'apikey') {
    return gatewayModelIds.value.map(id => ({ id: '', name: id, upstream_model_id: id, model_type: 'rerank', enabled: true } as Model))
  }
  return models.value
})

// 解析文档列表
const documents = computed(() => {
  return config.documentText
    .split('\n')
    .map(s => s.trim())
    .filter(s => s.length > 0)
})

// ─── 加载模型列表 ──────────────────────────────────────────────────────────
async function loadAdminModels() {
  loadingModels.value = true
  try {
    const result = await getModels(undefined, { pageSize: 200 })
    const list = result.data.filter(m => m.model_type === 'rerank' && m.enabled)
    models.value = list
    const map = new Map<string, Model>()
    list.forEach(m => {
      const key = m.upstream_model_id || m.name
      map.set(key, m)
      if (m.name !== key) map.set(m.name, m)
    })
    modelMetaMap.value = map
    if (list.length > 0 && !config.model) {
      config.model = list[0].upstream_model_id || list[0].name
    }
  } catch {
    models.value = []
  } finally {
    loadingModels.value = false
  }
}

async function loadGatewayModels() {
  if (!config.apiKey) {
    gatewayModelIds.value = []
    models.value = []
    return
  }
  loadingModels.value = true
  try {
    const baseUrl = effectiveApiBase.value.replace(/\/$/, '')
    const resp = await fetch(`${baseUrl}/models`, {
      headers: { Authorization: `Bearer ${config.apiKey}` },
    })
    if (!resp.ok) {
      const err = await resp.json().catch(() => null)
      throw new Error(err?.error?.message || `HTTP ${resp.status}`)
    }
    const data = await resp.json()
    gatewayModelIds.value = (data.data as GatewayModelInfo[]).map(m => m.id)
    if (gatewayModelIds.value.length > 0 && !config.model) {
      config.model = gatewayModelIds.value[0]
    }
  } catch (err: any) {
    gatewayModelIds.value = []
    ElMessage.error(`获取模型列表失败: ${err.message}`)
  } finally {
    loadingModels.value = false
  }
}

onMounted(loadAdminModels)

function getAuthToken(): string {
  if (mode.value === 'apikey') return config.apiKey
  return localStorage.getItem('access_token') || ''
}

// ─── 执行 Rerank ───────────────────────────────────────────────────────────
async function runRerank() {
  if (!config.model) {
    ElMessage.warning('请先选择模型')
    return
  }
  if (!config.query.trim()) {
    ElMessage.warning('请输入查询文本')
    return
  }
  if (documents.value.length === 0) {
    ElMessage.warning('请输入待排序文档（每行一个）')
    return
  }

  const token = getAuthToken()
  if (!token) {
    ElMessage.warning(mode.value === 'apikey' ? '请先输入 API Key' : '请先登录获取会话令牌')
    return
  }

  isRunning.value = true
  results.value = []
  respModel.value = ''
  respUsage.value = { total_tokens: 0 }

  try {
    const baseUrl = effectiveApiBase.value.replace(/\/$/, '')
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    }

    const body: any = {
      model: config.model,
      query: config.query,
      documents: documents.value,
      top_n: config.topN,
      return_documents: config.returnDocuments,
    }

    const response = await fetch(`${baseUrl}/rerank`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    })

    if (!response.ok) {
      const errData = await response.json().catch(() => null)
      throw new Error(errData?.error?.message || `HTTP ${response.status}`)
    }

    const data = await response.json()
    results.value = data.results || []
    respModel.value = data.model || ''
    respUsage.value = data.usage || { total_tokens: 0 }
  } catch (err: any) {
    ElMessage.error(`请求失败: ${err.message}`)
  } finally {
    isRunning.value = false
  }
}

function formatScore(score: number): string {
  return (score * 100).toFixed(2) + '%'
}

function scoreColor(score: number): string {
  if (score >= 0.8) return '#0EA371'
  if (score >= 0.5) return '#EAB308'
  return '#E74C3C'
}
</script>

<template>
  <div class="pg">
    <!-- ─── 左侧配置面板 ──────────────────────────────────── -->
    <aside class="pg-sidebar">
      <div class="pg-sidebar__header">
        <span class="pg-sidebar__title">Rerank 测试</span>
        <el-tag size="small" type="warning" effect="plain">Beta</el-tag>
      </div>

      <div class="pg-sidebar__body">
        <!-- 模式切换 -->
        <div class="field">
          <label class="field__label">模式</label>
          <el-radio-group v-model="mode" size="small" class="field__control" :disabled="isRunning">
            <el-radio-button value="session">会话</el-radio-button>
            <el-radio-button value="apikey">API Key</el-radio-button>
          </el-radio-group>
        </div>

        <!-- API Key -->
        <div v-if="mode === 'apikey'" class="field">
          <label class="field__label">API Key</label>
          <el-input
            v-model="config.apiKey"
            placeholder="sk-..."
            type="password"
            show-password
            clearable
            class="field__control"
            @change="loadGatewayModels"
          />
        </div>

        <!-- 模型选择 -->
        <div class="field">
          <label class="field__label">模型</label>
          <el-select
            v-model="config.model"
            filterable
            allow-create
            default-first-option
            placeholder="选择或输入模型"
            :loading="loadingModels"
            class="field__control"
          >
            <el-option
              v-for="m in modelOptions"
              :key="mode === 'apikey' ? m.name : m.id"
              :label="m.name"
              :value="m.upstream_model_id || m.name"
            >
              <div class="model-option">
                <span class="model-option__name">{{ m.name }}</span>
                <span v-if="mode === 'session'" class="model-option__owner">{{ m.provider?.name || m.provider_id }}</span>
              </div>
            </el-option>
          </el-select>
        </div>

        <!-- 查询文本 -->
        <div class="field">
          <label class="field__label">查询文本</label>
          <el-input
            v-model="config.query"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="输入查询文本..."
          />
        </div>

        <!-- 待排序文档 -->
        <div class="field">
          <label class="field__label">
            待排序文档
            <span class="field__hint-inline">{{ documents.length }} 条</span>
          </label>
          <el-input
            v-model="config.documentText"
            type="textarea"
            :autosize="{ minRows: 6, maxRows: 16 }"
            placeholder="每行一个文档..."
          />
        </div>

        <!-- 参数 -->
        <div class="field">
          <label class="field__label">返回数量 (Top N)</label>
          <el-input-number
            v-model="config.topN"
            :min="1"
            :max="100"
            controls-position="right"
            class="field__control"
          />
        </div>

        <div class="field">
          <label class="field__label">返回原文</label>
          <el-switch v-model="config.returnDocuments" />
        </div>

        <!-- 高级设置 -->
        <el-divider content-position="center">
          <span class="advanced-toggle" @click="showAdvanced = !showAdvanced">
            {{ showAdvanced ? '收起' : '高级设置' }}
          </span>
        </el-divider>

        <template v-if="showAdvanced">
          <div class="field">
            <label class="field__label">接口地址</label>
            <el-input
              v-model="config.apiBase"
              placeholder="/v1（默认网关）"
              clearable
              class="field__control"
            />
          </div>
        </template>
      </div>

      <!-- 底部操作 -->
      <div class="pg-sidebar__footer">
        <el-button
          type="primary"
          :loading="isRunning"
          class="pg-sidebar__btn"
          @click="runRerank"
        >
          {{ isRunning ? '重排序中...' : '执行重排序' }}
        </el-button>
      </div>
    </aside>

    <!-- ─── 右侧结果区 ──────────────────────────────────────── -->
    <main class="pg-main">
      <div class="pg-main__header">
        <span class="pg-main__title">排序结果</span>
        <span v-if="respModel" class="pg-main__model">{{ respModel }}</span>
        <span v-if="respUsage.total_tokens" class="pg-main__tokens">{{ respUsage.total_tokens }} tokens</span>
      </div>

      <div v-if="results.length === 0 && !isRunning" class="pg-empty">
        <div class="empty-graphic">
          <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 7h3l1-2h10l1 2h3v12H3z"/>
            <path d="M12 11v6M9 14h6"/>
          </svg>
        </div>
        <h3 class="empty-title">Rerank 测试</h3>
        <p class="empty-desc">选择 Rerank 模型，输入查询和文档列表进行重排序</p>
      </div>

      <div v-else class="pg-result">
        <div
          v-for="(item, idx) in results"
          :key="idx"
          class="result-item"
        >
          <div class="result-item__rank">
            <span class="result-item__pos">#{{ idx + 1 }}</span>
            <span class="result-item__score" :style="{ color: scoreColor(item.relevance_score) }">
              {{ formatScore(item.relevance_score) }}
            </span>
          </div>
          <div class="result-item__bar">
            <div
              class="result-item__bar-fill"
              :style="{ width: (item.relevance_score * 100) + '%', background: scoreColor(item.relevance_score) }"
            ></div>
          </div>
          <div class="result-item__meta">
            <span class="result-item__index">原始索引: {{ item.index }}</span>
          </div>
          <div v-if="item.document?.text" class="result-item__doc">
            {{ item.document.text }}
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
/* ─── 整体布局 ─────────────────────────────────────────────── */
.pg {
  display: flex;
  height: calc(100vh - 97px);
  margin: -20px -24px;
  background: var(--el-bg-color);
  border-radius: 0;
  overflow: hidden;
}

/* ─── 左侧面板 ─────────────────────────────────────────────── */
.pg-sidebar {
  width: 340px;
  min-width: 340px;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}

.pg-sidebar__header {
  padding: 16px 20px;
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid var(--el-border-color);
}

.pg-sidebar__title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-regular);
}

.pg-sidebar__body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
}

.pg-sidebar__footer {
  padding: 12px 20px;
  border-top: 1px solid var(--el-border-color);
}

.pg-sidebar__btn {
  width: 100%;
}

/* ─── 表单字段 ─────────────────────────────────────────────── */
.field {
  margin-bottom: 16px;
}

.field__label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 6px;
}

.field__control {
  width: 100%;
}

.field__hint-inline {
  font-size: 11px;
  font-weight: 400;
  color: var(--el-text-color-placeholder);
  text-transform: none;
}

.model-option {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.model-option__name { font-size: 13px; }
.model-option__owner { font-size: 11px; color: var(--el-text-color-placeholder); }

.advanced-toggle {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  user-select: none;
}

.advanced-toggle:hover { color: var(--el-color-primary); }

/* ─── 右侧区域 ─────────────────────────────────────────────── */
.pg-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color-page);
  overflow: hidden;
}

.pg-main__header {
  padding: 14px 24px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}

.pg-main__title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-regular);
}

.pg-main__model {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  padding: 2px 8px;
  background: var(--el-fill-color);
  border-radius: 4px;
}

.pg-main__tokens {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin-left: auto;
}

/* 空态 */
.pg-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  flex: 1;
  text-align: center;
}

.empty-graphic {
  color: var(--el-border-color);
  margin-bottom: 16px;
}

.empty-title {
  font-size: 20px;
  font-weight: 600;
  color: var(--el-text-color-regular);
  margin-bottom: 8px;
}

.empty-desc {
  font-size: 14px;
  color: var(--el-text-color-secondary);
}

/* 结果 */
.pg-result {
  flex: 1;
  overflow-y: auto;
  padding: 16px 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.result-item {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  padding: 14px 16px;
}

.result-item__rank {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.result-item__pos {
  font-size: 13px;
  font-weight: 700;
  color: var(--el-text-color-regular);
}

.result-item__score {
  font-size: 14px;
  font-weight: 700;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.result-item__bar {
  height: 4px;
  background: var(--el-fill-color);
  border-radius: 2px;
  margin-bottom: 8px;
  overflow: hidden;
}

.result-item__bar-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.4s ease;
}

.result-item__meta {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  margin-bottom: 6px;
}

.result-item__index {
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.result-item__doc {
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
  padding: 8px 10px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
  word-break: break-word;
}
</style>