<template>
  <div class="settings-page">
    <div class="page-header">
      <div>
        <h2 class="page-title">系统设置</h2>
        <p class="page-desc">管理站点品牌标识与 API 接入配置</p>
      </div>
    </div>

    <div v-loading="settingsStore.loading" class="settings-content">
      <!-- 站点品牌 -->
      <div class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>
            </span>
            <h3 class="card-title">站点品牌</h3>
          </div>
          <p class="card-desc">自定义站点的视觉标识，Logo 将显示在侧边栏顶部</p>
        </div>

        <div class="card-body">
          <!-- Logo -->
          <div class="form-group">
            <label class="form-label">站点 Logo</label>
            <ImageUpload
              v-model="form.site_logo"
              upload-label="上传 Logo"
              remove-label="移除"
              hint="支持 PNG、JPG、SVG 格式，建议正方形图片，最大 300KB"
              :max-size="300 * 1024"
            />
          </div>

          <div class="form-row">
            <div class="form-group">
              <label class="form-label">站点名称</label>
              <el-input
                v-model="form.site_name"
                placeholder="LLM Gateway"
                maxlength="64"
                class="form-input"
              />
              <p class="form-hint">显示在页面标题、侧边栏和 CCSwitch 导入时</p>
            </div>
            <div class="form-group">
              <label class="form-label">站点副标题</label>
              <el-input
                v-model="form.site_subtitle"
                placeholder="LLM API 统一网关"
                maxlength="128"
                class="form-input"
              />
              <p class="form-hint">侧边栏下方或首页的描述性文字</p>
            </div>
          </div>
        </div>
      </div>

      <!-- 接入配置 -->
      <div class="settings-card">
        <div class="card-header">
          <div class="card-title-row">
            <span class="card-icon">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
            </span>
            <h3 class="card-title">接入配置</h3>
          </div>
          <p class="card-desc">配置对外暴露的 API 地址，供客户端工具自动发现</p>
        </div>

        <div class="card-body">
          <div class="form-group">
            <label class="form-label">API 基础 URL</label>
            <el-input
              v-model="form.api_base_url"
              placeholder="留空则自动使用当前访问域名"
              class="form-input mono"
            >
              <template #prefix>
                <span class="input-prefix-icon">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
                </span>
              </template>
            </el-input>
            <p class="form-hint">客户端（如 CCSwitch、Codex CLI）将通过此地址连接网关 API。例如 <code class="inline-code">https://api.example.com</code></p>
          </div>

          <!-- 预览 -->
          <div class="preview-box">
            <div class="preview-label">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              接入地址预览
            </div>
            <code class="preview-url">{{ resolvedBaseUrl }}/v1</code>
          </div>
        </div>
      </div>

      <!-- 操作栏 -->
      <div class="action-bar">
        <div class="action-status">
          <span v-if="formChanged" class="change-indicator">有未保存的更改</span>
          <span v-else-if="settingsStore.loaded" class="saved-indicator">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>
            所有更改已保存
          </span>
        </div>
        <div class="action-buttons">
          <el-button @click="handleReset" :disabled="!formChanged">重置</el-button>
          <el-button type="primary" :loading="saving" @click="handleSave" :disabled="!formChanged">
            保存设置
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { useSettingsStore } from '@/stores/settings'
import ImageUpload from '@/components/common/ImageUpload.vue'

const settingsStore = useSettingsStore()
const saving = ref(false)

const form = reactive({
  site_name: '',
  site_logo: '',
  site_subtitle: '',
  api_base_url: '',
})

const initialForm = reactive({
  site_name: '',
  site_logo: '',
  site_subtitle: '',
  api_base_url: '',
})

const formChanged = computed(() => {
  return (
    form.site_name !== initialForm.site_name ||
    form.site_logo !== initialForm.site_logo ||
    form.site_subtitle !== initialForm.site_subtitle ||
    form.api_base_url !== initialForm.api_base_url
  )
})

const resolvedBaseUrl = computed(() => {
  const url = form.api_base_url.trim()
  if (url) return url.replace(/\/+$/, '')
  return window.location.origin
})

function setFormValues(source: typeof form) {
  form.site_name = source.site_name
  form.site_logo = source.site_logo
  form.site_subtitle = source.site_subtitle
  form.api_base_url = source.api_base_url
}

function captureInitial() {
  initialForm.site_name = form.site_name
  initialForm.site_logo = form.site_logo
  initialForm.site_subtitle = form.site_subtitle
  initialForm.api_base_url = form.api_base_url
}

onMounted(async () => {
  await settingsStore.loadSystemSettings()
  const s = settingsStore.systemSettings
  if (s) {
    form.site_name = s.site_name || ''
    form.site_logo = s.site_logo || ''
    form.site_subtitle = s.site_subtitle || ''
    form.api_base_url = s.api_base_url || ''
  }
  captureInitial()
})

async function handleSave() {
  saving.value = true
  try {
    const ok = await settingsStore.saveSettings({
      site_name: form.site_name.trim(),
      site_logo: form.site_logo,
      site_subtitle: form.site_subtitle.trim(),
      api_base_url: form.api_base_url.trim(),
    })
    if (ok) {
      ElMessage.success('设置已保存')
      captureInitial()
    } else {
      ElMessage.error('保存失败，请重试')
    }
  } catch {
    ElMessage.error('保存失败，请重试')
  } finally {
    saving.value = false
  }
}

function handleReset() {
  setFormValues(initialForm)
}
</script>

<style scoped>
.settings-page {
  padding: 0;
  max-width: 720px;
}

.page-header {
  margin-bottom: 28px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: 24px;
  font-weight: 700;
  margin: 0 0 6px;
  letter-spacing: -0.3px;
}

.page-desc {
  color: var(--el-text-color-secondary);
  font-size: 14px;
  margin: 0;
}

/* ===== 卡片 ===== */
.settings-content {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.settings-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;
  transition: border-color 0.15s;
}

.settings-card:hover {
  border-color: var(--el-border-color);
}

.card-header {
  padding: 20px 24px 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.card-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.card-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  flex-shrink: 0;
}

.card-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}

.card-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 8px 0 0 42px;
  line-height: 1.5;
}

.card-body {
  padding: 20px 24px 24px;
}

/* ===== 表单 ===== */
.form-group {
  margin-bottom: 20px;
}

.form-group:last-child {
  margin-bottom: 0;
}

.form-label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 6px;
}

.form-input :deep(.el-input__wrapper) {
  border-radius: 8px;
  box-shadow: 0 0 0 1px var(--el-border-color) inset;
  transition: box-shadow 0.2s, border-color 0.2s;
}

.form-input :deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px var(--el-border-color-darker) inset;
}

.form-input :deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 1px var(--el-color-primary) inset;
}

.form-input.mono :deep(.el-input__inner) {
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
}

.input-prefix-icon {
  display: flex;
  align-items: center;
  color: var(--el-text-color-placeholder);
  margin-right: 6px;
}

.form-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin: 6px 0 0;
  line-height: 1.5;
}

.inline-code {
  display: inline;
  padding: 1px 5px;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 11px;
  background: var(--el-fill-color-dark);
  border-radius: 4px;
  color: var(--el-text-color-secondary);
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}

@media (max-width: 600px) {
  .form-row {
    grid-template-columns: 1fr;
  }
}

/* ===== 预览 ===== */
.preview-box {
  margin-top: 20px;
  padding: 14px 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
}

.preview-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.preview-url {
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  color: var(--el-color-primary);
  word-break: break-all;
}

/* ===== 操作栏 ===== */
.action-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 24px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  position: sticky;
  bottom: 20px;
  backdrop-filter: blur(8px);
}

.action-status {
  display: flex;
  align-items: center;
}

.change-indicator {
  font-size: 13px;
  color: var(--el-color-warning);
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 6px;
}

.change-indicator::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-warning);
}

.saved-indicator {
  font-size: 13px;
  color: var(--el-color-success);
  display: flex;
  align-items: center;
  gap: 6px;
}

.action-buttons {
  display: flex;
  gap: 8px;
}
</style>