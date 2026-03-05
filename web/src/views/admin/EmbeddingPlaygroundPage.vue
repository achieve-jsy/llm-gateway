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

// ─── 状态 ──────────────────────────────────────────────────────────────────
const mode = ref<PlaygroundMode>('session')
const models = ref<Model[]>([])
const gatewayModelIds = ref<string[]>([])
const loadingModels = ref(false)

const config = reactive({
  model: '',
  apiBase: '',
  apiKey: '',
  inputMode: 'single' as 'single' | 'array',
  inputText: '',
  inputLines: [] as string[],
  encodingFormat: 'float',
  dimensions: undefined as number | undefined,
})

const result = ref<{
  object: string
  model: string
  data: { index: number; embedding_preview: string }[]
  usage: { prompt_tokens: number; total_tokens: number }
} | null>(null)
const isRunning = ref(false)
const showAdvanced = ref(false)

const modelMetaMap = ref<Map<string, Model>>(new Map())

const effectiveApiBase = computed(() => config.apiBase || '/v1')

const modelOptions = computed(() => {
  if (mode.value === 'apikey') {
    return gatewayModelIds.value.map(id => ({ id: '', name: id, upstream_model_id: id, model_type: 'embedding', enabled: true } as Model))
  }
  return models.value
})

// ─── 加载模型列表 ──────────────────────────────────────────────────────────
async function loadAdminModels() {
  loadingModels.value = true
  try {
    const result = await getModels(undefined, { pageSize: 200 })
    const list = result.data.filter(m => m.model_type === 'embedding' && m.enabled)
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

// ─── 执行 Embedding ────────────────────────────────────────────────────────
async function runEmbedding() {
  if (!config.model) {
    ElMessage.warning('请先选择模型')
    return
  }

  const input = config.inputMode === 'single'
    ? config.inputText
    : config.inputLines.filter(s => s.trim())

  if (!input || (Array.isArray(input) && input.length === 0)) {
    ElMessage.warning('请输入文本')
    return
  }

  const token = getAuthToken()
  if (!token) {
    ElMessage.warning(mode.value === 'apikey' ? '请先输入 API Key' : '请先登录获取会话令牌')
    return
  }

  isRunning.value = true
  result.value = null

  try {
    const baseUrl = effectiveApiBase.value.replace(/\/$/, '')
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    }

    const body: any = {
      model: config.model,
      input,
      encoding_format: config.encodingFormat,
    }
    if (config.dimensions && config.dimensions > 0) {
      body.dimensions = config.dimensions
    }

    const response = await fetch(`${baseUrl}/embeddings`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    })

    if (!response.ok) {
      const errData = await response.json().catch(() => null)
      throw new Error(errData?.error?.message || `HTTP ${response.status}`)
    }

    const data = await response.json()
    result.value = {
      object: data.object,
      model: data.model,
      data: (data.data || []).map((d: any) => ({
        index: d.index,
        embedding_preview: formatEmbeddingPreview(d.embedding),
      })),
      usage: data.usage || { prompt_tokens: 0, total_tokens: 0 },
    }
  } catch (err: any) {
    ElMessage.error(`请求失败: ${err.message}`)
  } finally {
    isRunning.value = false
  }
}

function formatEmbeddingPreview(embedding: any): string {
  if (!embedding) return 'null'
  if (typeof embedding === 'string') {
    // base64
    return `[base64] (${embedding.length} chars)`
  }
  if (Array.isArray(embedding)) {
    const preview = embedding.slice(0, 10).map((v: number) => v.toFixed(6)).join(', ')
    return `[${embedding.length} floats] ${preview}${embedding.length > 10 ? ', ...' : ''}`
  }
  return String(embedding)
}

function addInputLine() {
  config.inputLines.push('')
}

function removeInputLine(index: number) {
  config.inputLines.splice(index, 1)
}
</script>

<template>
  <div class="pg">
    <!-- ─── 左侧配置面板 ──────────────────────────────────── -->
    <aside class="pg-sidebar">
      <div class="pg-sidebar__header">
        <span class="pg-sidebar__title">Embedding 测试</span>
        <el-tag size="small" type="success" effect="plain">Beta</el-tag>
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

        <!-- 输入模式 -->
        <div class="field">
          <label class="field__label">输入模式</label>
          <el-radio-group v-model="config.inputMode" size="small">
            <el-radio-button value="single">单条文本</el-radio-button>
            <el-radio-button value="array">多条文本</el-radio-button>
          </el-radio-group>
        </div>

        <!-- 单条输入 -->
        <div v-if="config.inputMode === 'single'" class="field">
          <label class="field__label">文本</label>
          <el-input
            v-model="config.inputText"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 8 }"
            placeholder="输入要向量化的文本..."
          />
        </div>

        <!-- 多条输入 -->
        <div v-else class="field">
          <label class="field__label">
            文本列表
            <el-button size="small" text type="primary" @click="addInputLine">+ 添加</el-button>
          </label>
          <div class="input-lines">
            <div v-for="(_, idx) in config.inputLines" :key="idx" class="input-line-item">
              <el-input
                v-model="config.inputLines[idx]"
                type="textarea"
                :autosize="{ minRows: 1, maxRows: 3 }"
                :placeholder="`文本 ${idx + 1}`"
              />
              <el-button
                v-if="config.inputLines.length > 1"
                text
                type="danger"
                size="small"
                @click="removeInputLine(idx)"
              >
                ✕
              </el-button>
            </div>
          </div>
        </div>

        <!-- 高级设置 -->
        <el-divider content-position="center">
          <span class="advanced-toggle" @click="showAdvanced = !showAdvanced">
            {{ showAdvanced ? '收起' : '高级设置' }}
          </span>
        </el-divider>

        <template v-if="showAdvanced">
          <div class="field">
            <label class="field__label">编码格式</label>
            <el-select v-model="config.encodingFormat" class="field__control">
              <el-option label="float" value="float" />
              <el-option label="base64" value="base64" />
            </el-select>
          </div>

          <div class="field">
            <label class="field__label">输出维度</label>
            <el-input-number
              v-model="config.dimensions"
              :min="0"
              :max="8192"
              :step="128"
              placeholder="默认"
              controls-position="right"
              class="field__control"
            />
            <span class="field__hint">留空使用模型默认维度</span>
          </div>

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
          @click="runEmbedding"
        >
          {{ isRunning ? '向量化中...' : '生成向量' }}
        </el-button>
      </div>
    </aside>

    <!-- ─── 右侧结果区 ──────────────────────────────────────── -->
    <main class="pg-main">
      <div class="pg-main__header">
        <span class="pg-main__title">结果</span>
      </div>

      <div v-if="!result" class="pg-empty">
        <div class="empty-graphic">
          <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 3v18M3 12h18"/>
          </svg>
        </div>
        <h3 class="empty-title">Embedding 测试</h3>
        <p class="empty-desc">选择 Embedding 模型，输入文本生成向量</p>
      </div>

      <div v-else class="pg-result">
        <!-- 元信息 -->
        <div class="result-meta">
          <span class="result-model">{{ result.model }}</span>
          <span class="result-tokens">{{ result.usage.total_tokens }} tokens</span>
        </div>

        <!-- 向量列表 -->
        <div class="result-list">
          <div v-for="item in result.data" :key="item.index" class="result-item">
            <div class="result-item__header">
              <span class="result-item__index">#{{ item.index }}</span>
            </div>
            <div class="result-item__embedding">
              <code>{{ item.embedding_preview }}</code>
            </div>
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

.field__hint {
  margin-top: 4px;
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.model-option {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.model-option__name { font-size: 13px; }
.model-option__owner { font-size: 11px; color: var(--el-text-color-placeholder); }

.input-lines {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.input-line-item {
  display: flex;
  align-items: flex-start;
  gap: 4px;
}

.input-line-item .el-input { flex: 1; }

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
  padding: 16px 24px;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}

.pg-main__title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-regular);
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
}

.result-meta {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
  padding: 10px 14px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
}

.result-model {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}

.result-tokens {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  padding: 2px 8px;
  background: var(--el-fill-color);
  border-radius: 4px;
}

.result-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.result-item {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  overflow: hidden;
}

.result-item__header {
  padding: 8px 14px;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-fill-color-light);
}

.result-item__index {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
}

.result-item__embedding {
  padding: 12px 14px;
  overflow-x: auto;
}

.result-item__embedding code {
  font-size: 12px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  color: var(--el-text-color-regular);
  word-break: break-all;
  line-height: 1.6;
}
</style>