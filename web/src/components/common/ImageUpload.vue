<template>
  <div class="image-upload">
    <div class="upload-preview">
      <div class="preview-box" :class="{ 'has-image': !!modelValue }" @click="triggerUpload">
        <img v-if="modelValue" :src="modelValue" alt="" class="preview-img" />
        <div v-else class="placeholder-content">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
            <circle cx="8.5" cy="8.5" r="1.5"/>
            <polyline points="21 15 16 10 5 21"/>
          </svg>
        </div>
      </div>
    </div>
    <div class="upload-info">
      <div class="upload-controls">
        <button type="button" class="upload-btn" @click="triggerUpload">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="17 8 12 3 7 8"/>
            <line x1="12" y1="3" x2="12" y2="15"/>
          </svg>
          {{ uploadLabel }}
        </button>
        <button
          v-if="modelValue"
          type="button"
          class="remove-btn"
          @click="handleRemove"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="3 6 5 6 21 6"/>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
          </svg>
          {{ removeLabel }}
        </button>
      </div>
      <p v-if="hint" class="upload-hint">{{ hint }}</p>
      <p v-if="error" class="upload-error">{{ error }}</p>
    </div>
    <input
      ref="fileInputRef"
      type="file"
      :accept="acceptTypes"
      class="hidden-input"
      @change="handleUpload"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: string
  mode?: 'image' | 'svg'
  uploadLabel?: string
  removeLabel?: string
  hint?: string
  maxSize?: number
}>(), {
  mode: 'image',
  uploadLabel: '上传',
  removeLabel: '移除',
  hint: '',
  maxSize: 300 * 1024,
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const error = ref('')
const fileInputRef = ref<HTMLInputElement>()

const acceptTypes = computed(() => props.mode === 'svg' ? '.svg' : 'image/*')

function triggerUpload() {
  fileInputRef.value?.click()
}

function handleRemove() {
  error.value = ''
  emit('update:modelValue', '')
}

function handleUpload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  error.value = ''

  if (!file) return

  if (props.maxSize && file.size > props.maxSize) {
    error.value = `文件过大 (${(file.size / 1024).toFixed(1)} KB)，最大允许 ${(props.maxSize / 1024).toFixed(0)} KB`
    input.value = ''
    return
  }

  if (props.mode !== 'svg' && !file.type.startsWith('image/')) {
    error.value = '请选择图片文件'
    input.value = ''
    return
  }

  const reader = new FileReader()
  reader.onload = (e) => {
    emit('update:modelValue', e.target?.result as string)
  }
  reader.onerror = () => {
    error.value = '文件读取失败，请重试'
  }

  if (props.mode === 'svg') {
    reader.readAsText(file)
  } else {
    reader.readAsDataURL(file)
  }
  input.value = ''
}
</script>

<style scoped>
.image-upload {
  display: flex;
  gap: 20px;
  align-items: flex-start;
}

.upload-preview {
  flex-shrink: 0;
}

.preview-box {
  width: 88px;
  height: 88px;
  border-radius: 12px;
  border: 2px dashed var(--el-border-color);
  background: var(--el-fill-color-lighter);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  cursor: pointer;
  transition: border-color 0.2s, background 0.2s;
}

.preview-box:hover {
  border-color: var(--el-color-primary-light-3);
  background: var(--el-color-primary-light-9);
}

.preview-box.has-image {
  border-style: solid;
  border-color: var(--el-border-color);
  background: var(--el-bg-color);
}

.preview-box.has-image:hover {
  border-color: var(--el-color-primary-light-1);
}

.preview-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.placeholder-content {
  color: var(--el-text-color-placeholder);
  display: flex;
  align-items: center;
  justify-content: center;
}

.upload-info {
  flex: 1;
  min-width: 0;
  padding-top: 2px;
}

.upload-controls {
  display: flex;
  align-items: center;
  gap: 10px;
}

.upload-btn,
.remove-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 7px 14px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  border: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
  color: var(--el-text-color-primary);
  cursor: pointer;
  transition: all 0.15s;
}

.upload-btn:hover {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.remove-btn {
  color: var(--el-color-danger);
  border-color: var(--el-color-danger-light-5);
}

.remove-btn:hover {
  background: var(--el-color-danger-light-9);
  border-color: var(--el-color-danger);
}

.hidden-input {
  display: none;
}

.upload-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin: 8px 0 0;
  line-height: 1.5;
}

.upload-error {
  font-size: 12px;
  color: var(--el-color-danger);
  margin: 8px 0 0;
  line-height: 1.5;
}
</style>