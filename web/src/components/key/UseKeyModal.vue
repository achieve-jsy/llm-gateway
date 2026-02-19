<template>
  <el-dialog
    v-model="visible"
    title="使用密钥"
    width="720px"
    destroy-on-close
    @closed="resetState"
  >
    <div v-if="apiKeyId" class="use-key-modal">
      <p class="desc">
        将 API Key 配置到以下任一客户端，即可使用 LLM Gateway 提供的模型服务。
      </p>

      <el-tabs v-model="activeTab">
        <el-tab-pane label="cURL" name="curl">
          <CodeBlock
            title="终端命令"
            :code="curlContent"
            :can-copy="true"
          />
        </el-tab-pane>

        <el-tab-pane label="Python SDK" name="python">
          <p class="hint">使用 OpenAI 官方 Python SDK 接入：</p>
          <CodeBlock
            title="example.py"
            :code="pythonContent"
            :can-copy="true"
          />
        </el-tab-pane>

        <!-- Codex CLI tab hidden for now -->
        <!-- <el-tab-pane label="Codex CLI" name="codex"> ... </el-tab-pane> -->

        <el-tab-pane label="Claude Code" name="claude">
          <p class="hint">设置环境变量后启动 Claude Code：</p>
          <CodeBlock
            :title="osLabel"
            :code="claudeCodeEnvContent"
            :can-copy="true"
          />
          <p class="hint">启动时通过 <code>--model</code> 指定模型，例如：<code>claude --model claude-sonnet-4-6</code></p>
          <div class="shell-tabs">
            <button
              v-for="t in shellOptions"
              :key="t.id"
              :class="['shell-tab', { active: osTab === t.id }]"
              @click="osTab = t.id"
            >{{ t.label }}</button>
          </div>
          <p class="hint mt-2">或者写入配置文件 <code>~/.claude/settings.json</code>：</p>
          <CodeBlock
            title="~/.claude/settings.json"
            :code="claudeCodeSettingsContent"
            :can-copy="true"
          />
        </el-tab-pane>

        <el-tab-pane label="OpenCode" name="opencode">
          <p class="hint">
            OpenCode 支持两种密钥配置方式：
          </p>
          <div class="sub-tabs">
            <button
              :class="['sub-tab', { active: opencodeMode === 'json' }]"
              @click="opencodeMode = 'json'"
            >opencode.json 配置文件（推荐）</button>
            <button
              :class="['sub-tab', { active: opencodeMode === 'env' }]"
              @click="opencodeMode = 'env'"
            >环境变量</button>
          </div>

          <template v-if="opencodeMode === 'json'">
            <p class="hint">将以下内容保存为 <code>~/opencode.json</code>（或放在 <code>~/.config/opencode/opencode.json</code>）：</p>
            <CodeBlock
              title="opencode.json"
              :code="opencodeJsonContent"
              :can-copy="true"
            />
            <el-alert type="success" :closable="false" show-icon class="config-note">
              将 JSON 内容复制后，在终端中执行以下命令即可一键写入：
              <br />
              <code class="inline-command">mkdir -p ~/.config/opencode && echo '[粘贴 JSON 内容]' > ~/.config/opencode/opencode.json</code>
            </el-alert>
          </template>

          <template v-else>
            <p class="hint">设置环境变量后启动 OpenCode：</p>
            <CodeBlock
              :title="osLabel"
              :code="opencodeEnvContent"
              :can-copy="true"
            />
            <div class="shell-tabs">
              <button
                v-for="t in shellOptions"
                :key="t.id"
                :class="['shell-tab', { active: osTab === t.id }]"
                @click="osTab = t.id"
              >{{ t.label }}</button>
            </div>
          </template>
        </el-tab-pane>
      </el-tabs>

      <el-alert type="info" :closable="false" show-icon>
        安全提示：请勿将 API Key 暴露在客户端代码或公共仓库中。
      </el-alert>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import CodeBlock from '@/components/key/CodeBlock.vue'
import { useSettingsStore } from '@/stores/settings'
import { revealKey } from '@/services/control-plane'
import { fetchModelsByApiKey } from '@/services/gateway-api'

const props = defineProps<{
  modelValue: boolean
  apiKeyId: string | null
  keyHint: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const activeTab = ref('curl')
const osTab = ref<'unix' | 'cmd' | 'powershell'>('unix')
const opencodeMode = ref('json')
const fullKey = ref('')
const availableModels = ref<string[]>([])

type OsTab = 'unix' | 'cmd' | 'powershell'

const shellOptions: { id: OsTab; label: string }[] = [
  { id: 'unix', label: 'macOS / Linux' },
  { id: 'cmd', label: 'Windows CMD' },
  { id: 'powershell', label: 'PowerShell' },
]

const osLabel = computed(() => {
  const labels: Record<OsTab, string> = {
    unix: '终端 (macOS / Linux)',
    cmd: 'Command Prompt (Windows)',
    powershell: 'PowerShell (Windows)',
  }
  return labels[osTab.value]
})

const settingsStore = useSettingsStore()
const baseUrl = computed(() => settingsStore.getBaseUrl() + '/v1')
const keyPlaceholder = computed(() => fullKey.value || `sk-...${props.keyHint}`)

watch(() => props.apiKeyId, async (id) => {
  if (id) {
    // 先拿完整 key，再用 key 调 /v1/models 获取有权限的模型
    let key = ''
    try {
      const result = await revealKey(id)
      key = result.key
      fullKey.value = key
    } catch {
      fullKey.value = ''
    }

    if (key) {
      const base = settingsStore.getBaseUrl() + '/v1'
      const models = await fetchModelsByApiKey(base, key)
      availableModels.value = models.map(m => m.id)
    } else {
      availableModels.value = []
    }
  }
})

const curlContent = computed(() =>
  `curl ${baseUrl.value}/chat/completions \\
  -H "Authorization: Bearer ${keyPlaceholder.value}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`
)

const pythonContent = computed(() =>
  `from openai import OpenAI

client = OpenAI(
    base_url="${baseUrl.value}",
    api_key="${keyPlaceholder.value}"
)

response = client.chat.completions.create(
    model="gpt-4",
    messages=[{"role": "user", "content": "Hello!"}]
)
print(response.choices[0].message.content)`
)

const opencodeEnvContent = computed(() => {
  const key = keyPlaceholder.value
  const url = baseUrl.value
  if (osTab.value === 'unix') {
    return `export OPENAI_API_KEY="${key}"
export OPENAI_BASE_URL="${url}"

opencode`
  }
  if (osTab.value === 'cmd') {
    return `set OPENAI_API_KEY=${key}
set OPENAI_BASE_URL=${url}

opencode`
  }
  return `$env:OPENAI_API_KEY="${key}"
$env:OPENAI_BASE_URL="${url}"

opencode`
})

const claudeCodeEnvContent = computed(() => {
  const key = keyPlaceholder.value
  const url = settingsStore.getBaseUrl()
  if (osTab.value === 'unix') {
    return `export ANTHROPIC_BASE_URL="${url}"
export ANTHROPIC_API_KEY="${key}"

claude`
  }
  if (osTab.value === 'cmd') {
    return `set ANTHROPIC_BASE_URL=${url}
set ANTHROPIC_API_KEY=${key}

claude`
  }
  return `$env:ANTHROPIC_BASE_URL="${url}"
$env:ANTHROPIC_API_KEY="${key}"

claude`
})

const claudeCodeSettingsContent = computed(() =>
  `{
  "env": {
    "ANTHROPIC_BASE_URL": "${settingsStore.getBaseUrl()}",
    "ANTHROPIC_API_KEY": "${keyPlaceholder.value}"
  },
  "model": "claude-sonnet-4-6"
}`)

const opencodeJsonContent = computed(() => {
  // 构建 models 对象（key 为模型 ID，value 为展示名）
  const models: Record<string, { name: string }> = {}
  for (const id of availableModels.value) {
    models[id] = { name: id }
  }

  const providerEntry: Record<string, unknown> = {
    npm: '@ai-sdk/openai-compatible',
    name: 'LLM Gateway',
    options: {
      baseURL: baseUrl.value,
      apiKey: keyPlaceholder.value,
    },
  }
  if (Object.keys(models).length > 0) {
    providerEntry.models = models
  }

  const config: Record<string, unknown> = {
    $schema: 'https://opencode.ai/config.json',
    provider: {
      'llm-gateway': providerEntry,
    },
  }

  return JSON.stringify(config, null, 2)
})

function resetState() {
  activeTab.value = 'curl'
  osTab.value = 'unix'
  opencodeMode.value = 'json'
  fullKey.value = ''
  availableModels.value = []
}
</script>

<style scoped>
.use-key-modal {
  padding: 0;
}

.desc {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0 0 16px;
  line-height: 1.6;
}

.hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0 0 8px;
}

.hint code {
  background: var(--el-fill-color);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
}

.shell-tabs {
  display: flex;
  gap: 4px;
  margin-top: 8px;
  padding-bottom: 4px;
}

.shell-tab {
  padding: 4px 12px;
  border: none;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
}

.shell-tab:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}

.shell-tab.active {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}

.sub-tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 12px;
  border-bottom: 1px solid var(--el-border-color-light);
  padding-bottom: 8px;
}

.sub-tab {
  padding: 6px 16px;
  border: none;
  border-radius: 6px 6px 0 0;
  background: transparent;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
  border-bottom: 2px solid transparent;
}

.sub-tab:hover {
  color: var(--el-text-color-primary);
  background: var(--el-fill-color-light);
}

.sub-tab.active {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  border-bottom-color: var(--el-color-primary);
  font-weight: 600;
}

.config-note {
  margin-top: 8px;
}

.inline-command {
  background: var(--el-fill-color);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  color: var(--el-text-color-primary);
  word-break: break-all;
}

.mt-2 {
  margin-top: 12px;
}

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95%;
    max-width: 95vw;
  }
  :deep(.el-dialog .el-dialog__body) {
    padding: 14px;
  }
  :deep(.el-tabs__nav) {
    overflow-x: auto;
    flex-wrap: nowrap;
    white-space: nowrap;
  }
  .sub-tabs {
    flex-wrap: wrap;
  }
  .sub-tab {
    font-size: 12px;
    padding: 4px 10px;
  }
  .shell-tabs {
    flex-wrap: wrap;
  }
}
</style>