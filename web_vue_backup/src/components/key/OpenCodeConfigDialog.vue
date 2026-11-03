<template>
  <el-dialog
    v-model="dialogVisible"
    title="配置 OpenCode"
    width="540px"
    destroy-on-close
    @opened="onOpened"
  >

    <!-- ── Step 1: 介绍 & 系统选择 ── -->
    <template v-if="step === 'intro'">
      <div v-loading="loadingKey">
        <p class="intro-desc">
          选择你的操作系统，复制下方命令并在终端执行，将自动把以下配置写入本地 OpenCode。
        </p>

        <!-- 配置预览 -->
        <div class="config-preview">
          <div class="config-row">
            <span class="config-key">写入位置</span>
            <code class="config-val path">{{ configPath }}</code>
          </div>
          <div class="config-row">
            <span class="config-key">Provider</span>
            <code class="config-val">{{ PROVIDER_ID }}</code>
          </div>
          <div class="config-row">
            <span class="config-key">Base URL</span>
            <code class="config-val">{{ previewBaseURL }}</code>
          </div>
          <div class="config-row">
            <span class="config-key">API Key</span>
            <code class="config-val secret">{{ props.maskedKey }}</code>
          </div>
        </div>

        <!-- OS 选择 -->
        <div class="os-tabs">
          <button
            v-for="o in osList"
            :key="o.value"
            :class="['os-tab', { active: selectedOS === o.value }]"
            @click="selectedOS = o.value"
          >
            {{ o.label }}
          </button>
        </div>

        <!-- 命令展示 -->
        <div class="cmd-block">
          <pre class="cmd-text">{{ runCommand }}</pre>
          <el-button
            class="cmd-copy"
            type="primary"
            :icon="CopyDocument"
            @click="copyCommand"
          >复制命令</el-button>
        </div>

        <el-alert type="info" :closable="false" show-icon class="mt-3">
          <template #title>
            复制后粘贴到终端执行即可。执行完成后此对话框会自动更新结果。
          </template>
        </el-alert>
      </div>
    </template>

    <!-- ── Step 2: 等待结果 ── -->
    <template v-else-if="step === 'waiting'">
      <div class="status-panel">
        <el-icon class="spin-icon" :size="44"><Loading /></el-icon>
        <p class="status-title">等待脚本执行...</p>
        <p class="status-sub">脚本执行完成后将自动显示结果</p>
        <el-progress
          :percentage="pollProgress"
          :show-text="false"
          striped striped-flow :duration="8"
          class="mt-3 poll-bar"
        />
        <p class="status-hint mt-2">还没执行？
          <el-button text type="primary" size="small" @click="step = 'intro'">返回重新复制</el-button>
        </p>
      </div>
    </template>

    <!-- ── Step 3: 成功 ── -->
    <template v-else-if="step === 'success'">
      <div class="status-panel">
        <el-icon class="success-icon" :size="52"><CircleCheckFilled /></el-icon>
        <p class="status-title">配置成功！</p>
        <p class="status-sub">
          OpenCode 配置已写入 <code>{{ configPath }}</code>。<br/>
          重启 opencode 即可使用。
        </p>
      </div>
    </template>

    <!-- ── Step 4: 失败 ── -->
    <template v-else-if="step === 'failed'">
      <div class="status-panel">
        <el-icon class="error-icon" :size="48"><CircleCloseFilled /></el-icon>
        <p class="status-title">执行失败</p>
        <p class="status-sub error-msg">{{ failMsg || '未知错误，请检查终端输出' }}</p>
        <el-button class="mt-3" @click="retry">重新生成命令</el-button>
      </div>
    </template>

    <template #footer>
      <template v-if="step === 'intro'">
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="loadingKey || generating"
          :disabled="!apiKey"
          @click="confirm"
        >
          已复制，等待结果
        </el-button>
      </template>
      <template v-else>
        <el-button @click="dialogVisible = false">关闭</el-button>
      </template>
    </template>

  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument, Loading, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import {
  revealKey,
  generateOpencodeConfigToken,
  getScriptURL,
  getOpencodeConfigStatus,
} from '@/services/control-plane'
import { useSettingsStore } from '@/stores/settings'
import { copyToClipboard } from '@/utils/clipboard'

const props = defineProps<{
  visible: boolean
  apiKeyId: string
  maskedKey: string
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
}>()

const dialogVisible = computed({
  get: () => props.visible,
  set: (v) => emit('update:visible', v),
})

// ── 常量 ──────────────────────────────────────────────────────────────────────
const PROVIDER_ID   = 'llm-gateway'
const POLL_INTERVAL = 2000   // ms
const POLL_TIMEOUT  = 120000 // ms — 2 分钟

// ── OS 选项 ───────────────────────────────────────────────────────────────────
const osList = [
  { value: 'mac',     label: 'macOS / Linux' },
  { value: 'windows', label: 'Windows (PowerShell)' },
]

// 自动检测当前 OS
const detectedOS = /Windows/.test(navigator.userAgent) ? 'windows' : 'mac'
const selectedOS = ref<'mac' | 'windows'>(detectedOS)

// ── 状态 ──────────────────────────────────────────────────────────────────────
type Step = 'intro' | 'waiting' | 'success' | 'failed'
const step        = ref<Step>('intro')
const loadingKey  = ref(false)
const generating  = ref(false)
const apiKey      = ref('')
const failMsg     = ref('')
const pollProgress = ref(0)
const currentToken = ref('')

const settingsStore = useSettingsStore()

let pollTimer:    ReturnType<typeof setInterval> | null = null
let timeoutTimer: ReturnType<typeof setTimeout>  | null = null

// ── 计算属性 ──────────────────────────────────────────────────────────────────
const previewBaseURL = computed(() => {
  const base = settingsStore.getBaseUrl()
  return base ? `${base}/v1` : '（未配置）'
})

const configPath = computed(() =>
  selectedOS.value === 'windows'
    ? '%USERPROFILE%\\.config\\opencode\\opencode.json'
    : '~/.config/opencode/opencode.json'
)

// 生成完整可执行命令
const scriptURL = computed(() => {
  if (!currentToken.value) return ''
  // 使用当前页面 origin 拼绝对路径，避免 localhost 问题
  const base = window.location.origin
  const url = `${base}${getScriptURL(currentToken.value, selectedOS.value)}`
  if (selectedOS.value === 'windows') {
    return `iex (irm '${url}')`
  }
  return `bash <(curl -fsSL '${url}')`
})

// intro 阶段展示的命令（token 未生成时展示占位）
const runCommand = computed(() => {
  if (!currentToken.value) return '点击「已复制，等待结果」后生成命令...'
  return scriptURL.value
})

// ── 生命周期 ──────────────────────────────────────────────────────────────────
async function onOpened() {
  step.value       = 'intro'
  apiKey.value     = ''
  failMsg.value    = ''
  currentToken.value = ''
  pollProgress.value = 0
  loadingKey.value = true

  try {
    const result = await revealKey(props.apiKeyId)
    apiKey.value = result.key
    // 立即生成 token，让命令可以直接复制
    await generateToken()
  } catch {
    ElMessage.error('获取 API Key 失败')
    dialogVisible.value = false
  } finally {
    loadingKey.value = false
  }
}

// token 换 OS 时自动刷新（新 OS 需要新命令）
watch(selectedOS, async () => {
  if (step.value !== 'intro') return
  await generateToken()
})

async function generateToken() {
  generating.value = true
  try {
    const base = settingsStore.getBaseUrl()
    const { token } = await generateOpencodeConfigToken(props.apiKeyId, {
      provider_id: PROVIDER_ID,
      base_url:    base ? `${base}/v1` : '',
      api_key:     apiKey.value,
    })
    currentToken.value = token
  } catch {
    ElMessage.error('生成配置 token 失败')
  } finally {
    generating.value = false
  }
}

// ── 用户操作 ──────────────────────────────────────────────────────────────────
async function copyCommand() {
  if (!currentToken.value) {
    await generateToken()
  }
  if (!scriptURL.value) return
  try {
    await copyToClipboard(scriptURL.value)
    ElMessage.success('命令已复制，请粘贴到终端执行')
  } catch {
    ElMessage.error('复制失败，请手动选中命令复制')
  }
}

/** 用户点「已复制，等待结果」——开始轮询 */
function confirm() {
  if (!currentToken.value) return
  step.value = 'waiting'
  pollProgress.value = 0
  startPolling()
}

function retry() {
  stopPolling()
  generateToken()
  step.value = 'intro'
}

// ── 轮询 ──────────────────────────────────────────────────────────────────────
function startPolling() {
  stopPolling()
  const start = Date.now()

  pollTimer = setInterval(async () => {
    const elapsed = Date.now() - start
    pollProgress.value = Math.min(95, (elapsed / POLL_TIMEOUT) * 100)

    try {
      const res = await getOpencodeConfigStatus(props.apiKeyId, currentToken.value)
      if (res.status === 'success') {
        stopPolling()
        step.value = 'success'
      } else if (res.status === 'failed') {
        stopPolling()
        failMsg.value = res.msg || '脚本执行失败'
        step.value = 'failed'
      }
    } catch { /* 网络抖动，继续等 */ }
  }, POLL_INTERVAL)

  timeoutTimer = setTimeout(() => {
    stopPolling()
    if (step.value === 'waiting') {
      step.value = 'intro'
      ElMessage.warning('超时未收到执行结果，请确认命令已运行')
    }
  }, POLL_TIMEOUT)
}

function stopPolling() {
  if (pollTimer)    { clearInterval(pollTimer);  pollTimer    = null }
  if (timeoutTimer) { clearTimeout(timeoutTimer); timeoutTimer = null }
}

onUnmounted(stopPolling)
</script>

<style scoped>
.intro-desc {
  color: var(--el-text-color-regular);
  font-size: 13px;
  margin: 0 0 14px;
  line-height: 1.6;
}

/* 配置预览 */
.config-preview {
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 10px 14px;
  display: flex;
  flex-direction: column;
  gap: 7px;
  margin-bottom: 14px;
}
.config-row {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
}
.config-key {
  color: var(--el-text-color-secondary);
  width: 68px;
  flex-shrink: 0;
}
.config-val {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 12px;
  background: var(--el-fill-color);
  padding: 2px 7px;
  border-radius: 4px;
  word-break: break-all;
}
.config-val.path   { color: var(--el-color-primary); }
.config-val.secret { color: var(--el-text-color-secondary); letter-spacing: 1px; }

/* OS 选项卡 */
.os-tabs {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}
.os-tab {
  padding: 5px 16px;
  border: 1px solid var(--el-border-color);
  border-radius: 20px;
  background: transparent;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
}
.os-tab:hover { border-color: var(--el-color-primary-light-5); color: var(--el-color-primary); }
.os-tab.active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}

/* 命令块 */
.cmd-block {
  background: #1e1e2e;
  border-radius: 8px;
  padding: 12px 14px;
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.cmd-text {
  flex: 1;
  margin: 0;
  color: #cdd6f4;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
.cmd-copy { flex-shrink: 0; align-self: center; }

/* 状态面板 */
.status-panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 28px 0;
  text-align: center;
  gap: 8px;
}
.status-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}
.status-sub {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 0;
  line-height: 1.6;
}
.status-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin: 0;
}
.error-msg { color: var(--el-color-danger) !important; }

.spin-icon {
  color: var(--el-color-primary);
  animation: spin 1s linear infinite;
}
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
.success-icon { color: var(--el-color-success); }
.error-icon   { color: var(--el-color-danger); }

.poll-bar { width: 280px; }

.mt-2 { margin-top: 8px; }
.mt-3 { margin-top: 12px; }
</style>