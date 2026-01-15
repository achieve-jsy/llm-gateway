<script setup lang="ts">
import { ref, onMounted } from 'vue'
import ModelDiscoverySection from '@/components/access/ModelDiscoverySection.vue'
import { getPublicSettings } from '@/services/control-plane'
import { CopyDocument, Check } from '@element-plus/icons-vue'
import { copyToClipboard } from '@/utils/clipboard'

const apiBaseUrl = ref('')
const copied = ref(false)
let copyTimer: ReturnType<typeof setTimeout> | null = null

const displayUrl = ref('https://your-gateway.example.com/v1')

function buildUrl(base: string) {
  const b = base || 'https://your-gateway.example.com'
  return b.endsWith('/v1') ? b : b.replace(/\/$/, '') + '/v1'
}

function copyUrl() {
  copyToClipboard(displayUrl.value)
  copied.value = true
  if (copyTimer) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copied.value = false }, 2200)
}

onMounted(async () => {
  try {
    const res = await getPublicSettings()
    apiBaseUrl.value = res.data.api_base_url || ''
    displayUrl.value = buildUrl(apiBaseUrl.value)
  } catch { /* noop */ }
})
</script>

<template>
  <div class="msq">
    <div class="msq-head">
      <div class="msq-head__text">
        <h1 class="msq-title">模型广场</h1>
        <p class="msq-desc">
          下方每个模型组名即为 API 调用时 <code>model</code> 参数的值，复制后直接使用
        </p>
      </div>
      <div class="msq-url">
        <span class="msq-url__prefix">Base URL</span>
        <span class="msq-url__value">{{ displayUrl }}</span>
        <button class="msq-url__btn" :class="{ ok: copied }" @click="copyUrl">
          <el-icon><component :is="copied ? Check : CopyDocument" /></el-icon>
          {{ copied ? '已复制' : '复制' }}
        </button>
      </div>
    </div>

    <ModelDiscoverySection />
  </div>
</template>

<style lang="scss" scoped>
.msq {
  width: min(100%, 1080px);
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.msq-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  flex-wrap: wrap;
}
.msq-head__text { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.msq-title {
  font-size: 24px; font-weight: 800; margin: 0; letter-spacing: -0.3px;
  color: var(--el-text-color-primary);
}
.msq-desc {
  font-size: 13px; color: var(--el-text-color-secondary); margin: 0; line-height: 1.5;
  code {
    font-family: 'Fira Code', monospace; font-size: 12px;
    background: rgba(54,179,78,0.08); color: #1F8A3B;
    padding: 1px 5px; border-radius: 4px; border: 1px solid rgba(54,179,78,0.15);
  }
}

.msq-url {
  display: flex; align-items: center;
  background: var(--el-fill-color-blank);
  border: 1.5px solid #C8E4E2; border-radius: 10px; overflow: hidden;
  box-shadow: 0 2px 8px rgba(54,179,78,0.06);
  max-width: 100%;
}
.msq-url__prefix {
  padding: 0 12px; font-size: 11px; font-weight: 700; color: #36B34E;
  letter-spacing: 0.6px; text-transform: uppercase;
  border-right: 1px solid #D8EEEC; height: 40px;
  display: flex; align-items: center; white-space: nowrap;
  background: rgba(54,179,78,0.04);
}
.msq-url__value {
  flex: 1; padding: 0 14px;
  font-family: 'Fira Code', 'JetBrains Mono', monospace; font-size: 13px;
  color: #1A5C58; font-weight: 600;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: all;
}
.msq-url__btn {
  display: flex; align-items: center; gap: 5px; padding: 0 16px; height: 40px;
  background: #36B34E; border: none; color: #fff; font-size: 12px; font-weight: 600;
  cursor: pointer; transition: background 0.15s; white-space: nowrap;
  .el-icon { font-size: 14px; }
  &:hover { background: #229A94; }
  &.ok { background: #0EA371; }
  &:focus-visible { outline: 2px solid #36B34E; outline-offset: 2px; }
}

@media (max-width: 640px) {
  .msq { width: 100%; }
  .msq-title { font-size: 20px; }
  .msq-url { width: 100%; }
}

:global(html.dark) .msq-url { border-color: #1F5230; background: #0A1B1A; box-shadow: none; }
:global(html.dark) .msq-url__prefix { background: rgba(54,179,78,0.07); border-color: #1F5230; }
:global(html.dark) .msq-url__value { color: #6CD083; }
:global(html.dark) .msq-desc code { background: rgba(54,179,78,0.12); color: #6CD083; border-color: rgba(54,179,78,0.25); }
</style>
