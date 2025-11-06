<template>
  <div class="settings-page">
    <div class="page-header">
      <h2 class="page-title">系统设置</h2>
    </div>

    <el-card v-loading="settingsStore.loading" class="settings-card">
      <template #header>
        <span class="card-title">基础配置</span>
      </template>

      <el-form ref="formRef" :model="form" label-width="120px" label-position="left">
        <el-form-item label="站点名称">
          <el-input v-model="form.site_name" placeholder="LLM Gateway" maxlength="64" />
          <div class="form-hint">显示在页面标题和 CCSwitch 导入时的提供商名称</div>
        </el-form-item>

        <el-form-item label="API 基础 URL">
          <el-input v-model="form.api_base_url" placeholder="留空则自动使用当前域名" />
          <div class="form-hint">CCSwitch 导入时会使用此地址作为 API 端点。例如 https://api.example.com</div>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="saving" @click="handleSave">保存设置</el-button>
          <el-button @click="handleReset">重置</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useSettingsStore } from '@/stores/settings'

const settingsStore = useSettingsStore()
const saving = ref(false)

const form = reactive({
  site_name: '',
  api_base_url: '',
})

onMounted(async () => {
  await settingsStore.loadSystemSettings()
  form.site_name = settingsStore.systemSettings?.site_name || ''
  form.api_base_url = settingsStore.systemSettings?.api_base_url || ''
})

async function handleSave() {
  saving.value = true
  try {
    const ok = await settingsStore.saveSettings({
      site_name: form.site_name.trim(),
      api_base_url: form.api_base_url.trim(),
    })
    if (ok) {
      ElMessage.success('设置已保存')
    } else {
      ElMessage.error('保存失败')
    }
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

function handleReset() {
  form.site_name = settingsStore.systemSettings?.site_name || ''
  form.api_base_url = settingsStore.systemSettings?.api_base_url || ''
}
</script>

<style scoped>
.settings-page { padding: 0; }

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size, 22px);
  font-weight: var(--font-title-weight, 800);
  margin: 0;
}

.settings-card {
  max-width: 640px;
}

.card-title {
  font-weight: 600;
  font-size: 15px;
}

.form-hint {
  font-size: 12px;
  color: #98989D;
  margin-top: 4px;
}
</style>