<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { groupsApi } from '@/api/groups'
import { getModels, listKeys, revealKey } from '@/services/control-plane'
import { mapKeysResponse, type ApiKey } from '@/types/api-key'
import type { GroupListItem, GroupModel } from '@/types/group'
import type { Model } from '@/types/model'

// ─── 类型 ──────────────────────────────────────────────────────────────────
type PlaygroundMode = 'model' | 'group' | 'apikey'

interface ChatMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
  reasoning_content?: string
}

interface GatewayModelInfo {
  id: string
  object: string
  created: number
  owned_by: string
}

// ─── 状态 ──────────────────────────────────────────────────────────────────
const mode = ref<PlaygroundMode>('model')
const models = ref<Model[]>([])
const gatewayModelIds = ref<string[]>([])
const loadingModels = ref(false)
const groups = ref<GroupListItem[]>([])
const groupModels = ref<Record<number, GroupModel[]>>({})
const selectedGroupId = ref<number | ''>('')
const myKeys = ref<ApiKey[]>([])
const selectedKeyId = ref('')
const loadingGroups = ref(false)
const loadingKeys = ref(false)

const config = reactive({
  model: '',
  apiBase: '',
  apiKey: '',
  temperature: 0.7,
  topP: 1.0,
  maxTokens: 2048,
  stop: '',
  systemPrompt: '',
  stream: true,
})

const messages = ref<ChatMessage[]>([])
const userInput = ref('')
const isStreaming = ref(false)
const chatContainerRef = ref<HTMLElement>()
const showAdvanced = ref(false)

// 模型元数据缓存（keyed by model name）
const modelMetaMap = ref<Map<string, Model>>(new Map())

// 当前选中模型的元数据（从加载的模型列表中直接获取）
const currentModelMeta = computed(() => {
  if (!config.model) return null
  return modelMetaMap.value.get(config.model) ?? null
})

const effectiveApiBase = computed(() => config.apiBase || '/v1')

// 选中模型的上下文窗口提示
const modelHint = computed(() => {
  if (mode.value !== 'model') return ''
  const meta = currentModelMeta.value
  if (!meta) return ''
  const parts: string[] = []
  if (meta.context_length) parts.push(`上下文 ${(meta.context_length / 1024).toFixed(0)}K`)
  if (meta.max_output_tokens) parts.push(`最大输出 ${(meta.max_output_tokens / 1024).toFixed(0)}K`)
  return parts.join(' / ')
})

const selectedGroup = computed(() => groups.value.find(g => g.id === selectedGroupId.value) || null)

// 模型选项：
// - API Key 模式：来自网关 /v1/models（该 Key 有权限的模型）
// - 模型模式：全部已注册模型；模型组模式走独立的组下拉，不经过此列表
const modelOptions = computed(() => {
  if (mode.value === 'apikey') {
    return gatewayModelIds.value.map(id => ({ id: '', name: id, upstream_model_id: id, model_type: 'chat', enabled: true } as Model))
  }
  return models.value
})

// ─── 加载模型列表 ──────────────────────────────────────────────────────────
async function loadAdminModels() {
  loadingModels.value = true
  try {
    const result = await getModels(undefined, { pageSize: 200 })
    const list = result.data.filter(m => m.model_type === 'chat' && m.enabled)
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

// ─── 模型组 / API Key 选项 ────────────────────────────────────────────────
async function loadGroups() {
  loadingGroups.value = true
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    groups.value = res.data.data ?? []
  } catch {
    groups.value = []
  } finally {
    loadingGroups.value = false
  }
}

async function loadMyKeys() {
  loadingKeys.value = true
  try {
    const res = await listKeys(1, 100)
    myKeys.value = mapKeysResponse(res)
  } catch {
    myKeys.value = []
  } finally {
    loadingKeys.value = false
  }
}

async function onGroupChange() {
  config.model = ''
  if (selectedGroupId.value === '') return
  const gid = selectedGroupId.value
  // 选组后直接用组名作为发送的 model（gateway 按组调度）
  config.model = selectedGroup.value?.name || ''
  // 懒加载组内成员（仅用于展示该组包含哪些模型）
  if (!groupModels.value[gid]) {
    try {
      const res = await groupsApi.listGroupModels(gid)
      groupModels.value = { ...groupModels.value, [gid]: res.data.data ?? [] }
    } catch {
      groupModels.value = { ...groupModels.value, [gid]: [] }
    }
  }
}

async function onKeySelect(keyId: string) {
  const key = myKeys.value.find(k => k.id === keyId)
  if (!key) return
  try {
    const r = await revealKey(keyId)
    config.apiKey = r.key
    selectedKeyId.value = keyId
    config.model = ''
    await loadGatewayModels()
  } catch {
    ElMessage.error('获取 API Key 失败')
  }
}

async function onManualApiKeyChange() {
  config.model = ''
  selectedKeyId.value = ''
  await loadGatewayModels()
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
    } else if (gatewayModelIds.value.length === 0) {
      ElMessage.warning('网关未返回可用模型')
    }
  } catch (err: any) {
    gatewayModelIds.value = []
    ElMessage.error(`获取模型列表失败: ${err.message}`)
  } finally {
    loadingModels.value = false
  }
}

async function loadModels() {
  if (mode.value === 'apikey') {
    await loadGatewayModels()
  } else if (mode.value === 'model') {
    await loadAdminModels()
  }
  // 模型组模式不需要模型列表（走组下拉）
}

watch(mode, () => {
  config.model = ''
  selectedGroupId.value = ''
  selectedKeyId.value = ''
  if (mode.value === 'apikey' && myKeys.value.length === 0) loadMyKeys()
  loadModels()
})

onMounted(() => {
  loadAdminModels()
  loadGroups()
  loadMyKeys()
})

// ─── 主动获取鉴权 token ───────────────────────────────────────────────────
function getAuthToken(): string {
  return config.apiKey
}

// ─── 模型/模型组模式：自动用当前账号的第一个可用 Key（不再回退会话令牌） ───
async function ensureApiKeyForNonApikeyMode(): Promise<boolean> {
  if (config.apiKey.trim()) return true
  if (myKeys.value.length === 0) await loadMyKeys()
  const key = myKeys.value.find(k => k.enabled)
  if (!key) {
    ElMessage.warning('当前账号没有可用的 API Key，请先在「Key 管理」创建一个')
    return false
  }
  try {
    const r = await revealKey(key.id)
    config.apiKey = r.key
    selectedKeyId.value = key.id
    return true
  } catch {
    ElMessage.error('获取 API Key 失败')
    return false
  }
}

// ─── 滚动到底部 ──────────────────────────────────────────────────────────
function scrollToBottom() {
  nextTick(() => {
    if (chatContainerRef.value) {
      chatContainerRef.value.scrollTop = chatContainerRef.value.scrollHeight
    }
  })
}

// ─── 发送消息 ──────────────────────────────────────────────────────────────
async function sendMessage() {
  const content = userInput.value.trim()
  if (!content || isStreaming.value) return

  if (!config.model) {
    ElMessage.warning('请先选择或输入模型')
    return
  }

  if (!config.apiKey.trim()) {
    if (mode.value === 'apikey' || !(await ensureApiKeyForNonApikeyMode())) {
      ElMessage.warning('请先输入 API Key')
      return
    }
  }

  if (config.temperature < 0 || config.temperature > 2) {
    ElMessage.warning('温度值应在 0 到 2 之间')
    return
  }

  if (config.topP < 0 || config.topP > 1) {
    ElMessage.warning('Top P 值应在 0 到 1 之间')
    return
  }

  if (config.maxTokens < 1 || config.maxTokens > 128000) {
    ElMessage.warning('最大令牌数应在 1 到 128000 之间')
    return
  }

  if (config.apiBase && !/^https?:\/\/.+/.test(config.apiBase) && config.apiBase !== '/v1') {
    ElMessage.warning('接口地址格式不正确，请输入以 http(s):// 开头的地址或 /v1 路径')
    return
  }

  const token = getAuthToken()
  if (!token) {
    ElMessage.warning('请先输入 API Key')
    return
  }

  messages.value.push({ role: 'user', content })
  userInput.value = ''
  scrollToBottom()

  const reqMessages: ChatMessage[] = []
  if (config.systemPrompt) {
    reqMessages.push({ role: 'system', content: config.systemPrompt })
  }
  reqMessages.push(...messages.value.filter(m => m.role !== 'system' && m.content))

  const assistantMsg: ChatMessage = { role: 'assistant', content: '', reasoning_content: '' }
  messages.value.push(assistantMsg)
  scrollToBottom()

  isStreaming.value = true

  try {
    const baseUrl = effectiveApiBase.value.replace(/\/$/, '')
    const url = `${baseUrl}/chat/completions`

    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    const token = getAuthToken()
    headers['Authorization'] = `Bearer ${token}`

    const stopSeqs = config.stop.trim()
      ? config.stop.split(',').map(s => s.trim()).filter(Boolean)
      : undefined

    const body = JSON.stringify({
      model: config.model,
      messages: reqMessages.map(m => ({ role: m.role, content: m.content })),
      stream: config.stream,
      temperature: config.temperature,
      top_p: config.topP,
      max_tokens: config.maxTokens,
      ...(stopSeqs && { stop: stopSeqs }),
    })

    const response = await fetch(url, { method: 'POST', headers, body })

    if (!response.ok) {
      const errData = await response.json().catch(() => null)
      const errMsg = errData?.error?.message || `HTTP ${response.status}`
      throw new Error(errMsg)
    }

    if (!config.stream) {
      // Non-streaming
      const data = await response.json()
      messages.value[messages.value.length - 1].content = data.choices?.[0]?.message?.content || ''
      scrollToBottom()
      return
    }

    // Streaming SSE
    const reader = response.body?.getReader()
    if (!reader) throw new Error('No response body')

    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const data = line.slice(6).trim()
        if (data === '[DONE]') continue

        try {
          const chunk = JSON.parse(data)
          const delta = chunk.choices?.[0]?.delta
          if (delta?.content) {
            messages.value[messages.value.length - 1].content += delta.content
            scrollToBottom()
          }
          if (delta?.reasoning_content) {
            const last = messages.value[messages.value.length - 1]
            last.reasoning_content = (last.reasoning_content || '') + delta.reasoning_content
            scrollToBottom()
          }
        } catch {
          // ignore
        }
      }
    }
  } catch (err: any) {
    messages.value[messages.value.length - 1].content = `[Error] ${err.message}`
    ElMessage.error(`请求失败: ${err.message}`)
  } finally {
    isStreaming.value = false
    scrollToBottom()
  }
}

// ─── 工具方法 ──────────────────────────────────────────────────────────────
function clearChat() {
  messages.value = []
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendMessage()
  }
}

function renderContent(content: string): string {
  if (!content) return ''
  return content
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\n/g, '<br>')
}
</script>

<template>
  <div class="pg">
    <!-- ─── 左侧配置面板 ──────────────────────────────────── -->
    <aside class="pg-sidebar">
      <div class="pg-sidebar__header">
        <span class="pg-sidebar__title">对话测试</span>
        <el-tag size="small" type="info" effect="plain">Beta</el-tag>
      </div>

      <div class="pg-sidebar__body">
        <!-- 模式切换：模型 / 模型组 / API Key 三种调用方式 -->
        <div class="field">
          <label class="field__label">调用方式</label>
          <el-radio-group v-model="mode" size="small" class="field__control" :disabled="isStreaming">
            <el-radio-button value="model">模型</el-radio-button>
            <el-radio-button value="group">模型组</el-radio-button>
            <el-radio-button value="apikey">API Key</el-radio-button>
          </el-radio-group>
        </div>

        <!-- API Key 模式：选择我的 Key / 手动输入 -->
        <div v-if="mode === 'apikey'" class="field">
          <label class="field__label">我的 API Key</label>
          <el-select
            v-model="selectedKeyId"
            filterable
            clearable
            placeholder="选择我的 Key"
            :loading="loadingKeys"
            class="field__control"
            @change="onKeySelect"
          >
            <el-option v-for="k in myKeys" :key="k.id" :label="k.name" :value="k.id">
              <div class="model-option">
                <span class="model-option__name">{{ k.name }}</span>
                <span class="model-option__owner">{{ k.maskedKey }}</span>
              </div>
            </el-option>
          </el-select>
        </div>
        <div v-if="mode === 'apikey'" class="field">
          <label class="field__label">API Key</label>
          <el-input
            v-model="config.apiKey"
            placeholder="sk-..."
            type="password"
            show-password
            clearable
            class="field__control"
            @change="onManualApiKeyChange"
            @clear="selectedKeyId = ''"
          />
        </div>

        <!-- 模型组模式：选择模型组 -->
        <div v-if="mode === 'group'" class="field">
          <label class="field__label">模型组</label>
          <el-select
            v-model="selectedGroupId"
            filterable
            clearable
            placeholder="选择模型组"
            :loading="loadingGroups"
            class="field__control"
            @change="onGroupChange"
          >
            <el-option
              v-for="g in groups"
              :key="g.id"
              :label="g.name"
              :value="g.id"
            />
          </el-select>
        </div>

        <!-- 模型选择（模型 / API Key 模式） -->
        <div v-if="mode !== 'group'" class="field">
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
                <span v-if="mode === 'model'" class="model-option__owner">{{ m.provider?.name || m.provider_id }}</span>
              </div>
            </el-option>
          </el-select>
          <div v-if="modelHint" class="field__hint">{{ modelHint }}</div>
        </div>

        <!-- System Prompt -->
        <div class="field">
          <label class="field__label">系统提示词</label>
          <el-input
            v-model="config.systemPrompt"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 5 }"
            placeholder="你是一个有帮助的助手..."
            class="field__control"
          />
        </div>

        <!-- 参数 -->
        <div class="field">
          <label class="field__label">温度</label>
          <div class="field__row">
            <el-slider
              v-model="config.temperature"
              :min="0" :max="2" :step="0.05"
              class="field__slider"
            />
            <span class="field__value">{{ config.temperature.toFixed(2) }}</span>
          </div>
        </div>

        <div class="field">
          <label class="field__label">Top P</label>
          <div class="field__row">
            <el-slider
              v-model="config.topP"
              :min="0" :max="1" :step="0.05"
              class="field__slider"
            />
            <span class="field__value">{{ config.topP.toFixed(2) }}</span>
          </div>
        </div>

        <div class="field">
          <label class="field__label">最大令牌数</label>
          <el-input-number
            v-model="config.maxTokens"
            :min="1" :max="128000" :step="256"
            controls-position="right"
            class="field__control"
          />
        </div>

        <div class="field">
          <label class="field__label">停止序列</label>
          <el-input
            v-model="config.stop"
            placeholder="多个用逗号分隔"
            clearable
            class="field__control"
          />
        </div>

        <div class="field">
          <label class="field__label">流式输出</label>
          <el-switch v-model="config.stream" />
        </div>

        <!-- 高级配置折叠 -->
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

          <div v-if="mode !== 'apikey'" class="field">
            <label class="field__label">密钥</label>
            <el-input
              v-model="config.apiKey"
              placeholder="留空自动使用我的 Key"
              type="password"
              show-password
              clearable
              class="field__control"
            />
          </div>

          </template>
      </div>

      <!-- 底部操作 -->
      <div class="pg-sidebar__footer">
        <el-button @click="clearChat" :disabled="isStreaming || messages.length === 0" plain class="pg-sidebar__btn">
          清空对话
        </el-button>
      </div>
    </aside>

    <!-- ─── 右侧聊天区 ──────────────────────────────────────── -->
    <main class="pg-chat">
      <!-- 消息列表 -->
      <div ref="chatContainerRef" class="pg-chat__messages">
        <!-- 空态 -->
        <div v-if="messages.length === 0" class="pg-chat__empty">
          <div class="empty-graphic">
            <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
            </svg>
          </div>
          <h3 class="empty-title">对话测试</h3>
          <p class="empty-desc">选择模型，输入消息开始测试对话效果</p>
        </div>

        <!-- 消息列表 -->
        <template v-for="(msg, idx) in messages" :key="idx">
          <div :class="['msg', `msg--${msg.role}`]">
            <div class="msg__icon">
              <template v-if="msg.role === 'user'">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.7 0 5-2.3 5-5s-2.3-5-5-5-5 2.3-5 5 2.3 5 5 5zm0 2c-3.3 0-10 1.7-10 5v3h20v-3c0-3.3-6.7-5-10-5z"/></svg>
              </template>
              <template v-else>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M20 9V7c0-1.1-.9-2-2-2h-3c0-1.66-1.34-3-3-3S9 3.34 9 5H6c-1.1 0-2 .9-2 2v2c-1.66 0-3 1.34-3 3s1.34 3 3 3v4c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2v-4c1.66 0 3-1.34 3-3s-1.34-3-3-3zM7.5 11.5c0-.83.67-1.5 1.5-1.5s1.5.67 1.5 1.5S9.83 13 9 13s-1.5-.67-1.5-1.5zM16 17H8v-2h8v2zm-1-4c-.83 0-1.5-.67-1.5-1.5S14.17 10 15 10s1.5.67 1.5 1.5S15.83 13 15 13z"/></svg>
              </template>
            </div>
            <div class="msg__body">
              <div class="msg__role">{{ msg.role === 'user' ? '我' : '助手' }}</div>
              <!-- Reasoning -->
              <div v-if="msg.reasoning_content" class="msg__reasoning">
                <details>
                  <summary>思考中...</summary>
                  <pre>{{ msg.reasoning_content }}</pre>
                </details>
              </div>
              <!-- Content -->
              <div class="msg__content" v-html="renderContent(msg.content)"></div>
              <!-- Typing indicator -->
              <div v-if="isStreaming && idx === messages.length - 1 && msg.role === 'assistant' && !msg.content" class="msg__typing">
                <span></span><span></span><span></span>
              </div>
            </div>
          </div>
        </template>
      </div>

      <!-- 输入区 -->
      <div class="pg-chat__input">
        <div class="input-box">
          <el-input
            v-model="userInput"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 6 }"
            placeholder="输入消息...（Enter 发送，Shift+Enter 换行）"
            :disabled="isStreaming"
            @keydown="onKeydown"
            resize="none"
            class="input-box__textarea"
          />
          <button
            class="input-box__send"
            :class="{ 'input-box__send--active': userInput.trim() && !isStreaming }"
            :disabled="!userInput.trim() || isStreaming"
            @click="sendMessage"
          >
            <svg v-if="!isStreaming" width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
              <path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/>
            </svg>
            <span v-else class="send-loading"></span>
          </button>
        </div>
        <div class="input-hint">
          <span>{{ config.model || '未选择模型' }}</span>
          <span v-if="isStreaming" class="streaming-badge">生成中...</span>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
/* ─── 整体布局 ─────────────────────────────────────────────── */
.pg {
  display: flex;
  height: calc(100vh - 97px); /* 减去顶栏+padding */
  margin: -20px -24px; /* 抵消 page-container padding */
  background: var(--el-bg-color);
  border-radius: 0;
  overflow: hidden;
}

/* ─── 左侧面板 ─────────────────────────────────────────────── */
.pg-sidebar {
  width: 300px;
  min-width: 300px;
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
  display: block;
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

.field__row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.field__slider {
  flex: 1;
}

.field__value {
  font-size: 13px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  color: var(--el-text-color-regular);
  min-width: 36px;
  text-align: right;
}

.model-option {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.model-option__name {
  font-size: 13px;
}

.model-option__owner {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.field__hint {
  margin-top: 4px;
  font-size: 11px;
  color: var(--el-color-primary);
  opacity: 0.8;
}

.advanced-toggle {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  user-select: none;
}

.advanced-toggle:hover {
  color: var(--el-color-primary);
}

/* ─── 右侧聊天区 ──────────────────────────────────────────── */
.pg-chat {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color-page);
  overflow: hidden;
}

.pg-chat__messages {
  flex: 1;
  overflow-y: auto;
  padding: 32px 24px;
}

/* 空态 */
.pg-chat__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
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

/* ─── 消息 ─────────────────────────────────────────────────── */
.msg {
  display: flex;
  gap: 12px;
  padding: 16px 20px;
  border-radius: 12px;
  margin-bottom: 8px;
  transition: background-color 0.15s;
}

.msg--user {
  background: transparent;
}

.msg--assistant {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
}

.msg__icon {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  margin-top: 2px;
}

.msg--user .msg__icon {
  background: var(--el-color-primary);
  color: white;
}

.msg--assistant .msg__icon {
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
}

.msg__body {
  flex: 1;
  min-width: 0;
}

.msg__role {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.msg__content {
  font-size: 14px;
  line-height: 1.7;
  color: var(--el-text-color-regular);
  word-break: break-word;
}

.msg__reasoning {
  margin-bottom: 8px;
}

.msg__reasoning details {
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 12px;
}

.msg__reasoning summary {
  cursor: pointer;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.msg__reasoning pre {
  margin-top: 8px;
  white-space: pre-wrap;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  max-height: 200px;
  overflow-y: auto;
}

/* Typing */
.msg__typing {
  display: flex;
  gap: 4px;
  padding: 8px 0 4px;
}

.msg__typing span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-primary);
  opacity: 0.4;
  animation: bounce 1.4s infinite;
}

.msg__typing span:nth-child(2) { animation-delay: 0.2s; }
.msg__typing span:nth-child(3) { animation-delay: 0.4s; }

@keyframes bounce {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.4; }
  30% { transform: translateY(-4px); opacity: 1; }
}

/* ─── 输入区 ──────────────────────────────────────────────── */
.pg-chat__input {
  padding: 16px 24px 20px;
  background: var(--el-bg-color-page);
}

.input-box {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 12px;
  padding: 10px 12px 10px 16px;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.input-box:focus-within {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 3px rgba(54, 179, 78, 0.1);
}

.input-box__textarea {
  flex: 1;
}

.input-box__textarea :deep(.el-textarea__inner) {
  border: none !important;
  box-shadow: none !important;
  padding: 4px 0;
  background: transparent;
  resize: none;
  font-size: 14px;
  line-height: 1.5;
}

.input-box__send {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  border: none;
  background: var(--el-fill-color);
  color: var(--el-text-color-placeholder);
  cursor: not-allowed;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.15s;
}

.input-box__send--active {
  background: var(--el-color-primary);
  color: white;
  cursor: pointer;
}

.input-box__send--active:hover {
  background: var(--el-color-primary-dark-2);
}

.send-loading {
  width: 14px;
  height: 14px;
  border: 2px solid currentColor;
  border-top-color: transparent;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.input-hint {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 4px 0;
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.streaming-badge {
  color: var(--el-color-primary);
  font-weight: 500;
  animation: pulse 1.5s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
</style>
