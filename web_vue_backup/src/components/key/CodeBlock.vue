<template>
  <div class="code-block-wrapper">
    <div class="code-block-header">
      <span class="code-block-path">{{ title }}</span>
      <el-button
        v-if="canCopy"
        size="small"
        text
        type="primary"
        class="copy-btn"
        @click="handleCopy"
      >
        {{ copied ? '已复制' : '复制' }}
      </el-button>
    </div>
    <pre class="code-block-body"><code>{{ code }}</code></pre>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { copyToClipboard } from '@/utils/clipboard'

const props = defineProps<{
  title: string
  code: string
  canCopy?: boolean
}>()

const copied = ref(false)

async function handleCopy() {
  try {
    await copyToClipboard(props.code)
    ElMessage.success('已复制')
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    ElMessage.warning('复制失败')
  }
}
</script>

<style scoped>
.code-block-wrapper {
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  overflow: hidden;
  margin-bottom: 12px;
}

.code-block-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 14px;
  background: var(--el-fill-color-light);
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.code-block-path {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  font-family: 'SF Mono', 'JetBrains Mono', monospace;
}

.copy-btn {
  height: 24px;
  font-size: 12px;
}

.code-block-body {
  margin: 0;
  padding: 14px;
  background: var(--el-fill-color-lighter);
  font-size: 12.5px;
  line-height: 1.65;
  overflow-x: auto;
}

.code-block-body code {
  font-family: 'SF Mono', 'JetBrains Mono', 'Fira Code', monospace;
  color: var(--el-text-color-primary);
  white-space: pre;
}
</style>