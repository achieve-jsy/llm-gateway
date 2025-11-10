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

        <el-tab-pane label="Codex CLI" name="codex">
          <p class="hint">将以下内容写入 <code>~/.codex/config.toml</code>：</p>
          <CodeBlock
            title="~/.codex/config.toml"
            :code="codexConfigContent"
            :can-copy="true"
          />
          <p class="hint">将以下内容写入 <code>~/.codex/auth.json</code>：</p>
          <CodeBlock
            title="~/.codex/auth.json"
            :code="codexAuthContent"
            :can-copy="true"
          />
        </el-tab-pane>

        <el-tab-pane label="OpenCode" name="opencode">
          <p class="hint">设置环境变量后启动 OpenCode：</p>
          <CodeBlock
            :title="osTab === 'unix' ? '终端 (macOS / Linux)' : 'PowerShell (Windows)'"
            :code="opencodeEnvContent"
            :can-copy="true"
          />
          <div class="shell-tabs">
            <button
              :class="['shell-tab', { active: osTab === 'unix' }]"
              @click="osTab = 'unix'"
            >macOS / Linux</button>
            <button
              :class="['shell-tab', { active: osTab === 'windows' }]"
              @click="osTab = 'windows'"
            >Windows</button>
          </div>
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
const osTab = ref('unix')
const fullKey = ref('')

const settingsStore = useSettingsStore()
const baseUrl = computed(() => settingsStore.getBaseUrl() + '/v1')
const keyPlaceholder = computed(() => fullKey.value || `sk-...${props.keyHint}`)

watch(() => props.apiKeyId, async (id) => {
  if (id) {
    try {
      const result = await revealKey(id)
      fullKey.value = result.key
    } catch {
      fullKey.value = ''
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

const codexConfigContent = computed(() =>
  `model_provider = "OpenAI"
model = "gpt-4"

[model_providers.OpenAI]
name = "OpenAI"
base_url = "${baseUrl.value}"
wire_api = "responses"
requires_openai_auth = true`
)

const codexAuthContent = computed(() =>
  `{
  "OPENAI_API_KEY": "${keyPlaceholder.value}"
}`)

const opencodeEnvContent = computed(() => {
  if (osTab.value === 'unix') {
    return `export OPENAI_API_KEY="${keyPlaceholder.value}"
export OPENAI_BASE_URL="${baseUrl.value}"

opencode`
  }
  return `$env:OPENAI_API_KEY="${keyPlaceholder.value}"
$env:OPENAI_BASE_URL="${baseUrl.value}"

opencode`
})

function resetState() {
  activeTab.value = 'curl'
  osTab.value = 'unix'
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
</style>