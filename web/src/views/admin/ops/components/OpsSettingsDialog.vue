<template>
  <el-dialog
    :model-value="show"
    title="显示设置"
    width="480px"
    :close-on-click-modal="false"
    @update:model-value="$emit('close')"
    @close="$emit('close')"
  >
    <div v-loading="loading" class="ops-settings-body">
      <div class="ops-settings-section">
        <div class="ops-settings-label">自动刷新间隔</div>
        <el-select v-model="form.auto_refresh_interval" class="ops-settings-select">
          <el-option label="10 秒" :value="10" />
          <el-option label="30 秒" :value="30" />
          <el-option label="60 秒" :value="60" />
        </el-select>
      </div>

      <div class="ops-settings-section">
        <div class="ops-settings-label">显示面板控制</div>
        <div class="ops-settings-checkboxes">
          <el-checkbox v-model="form.show_alert_events">告警事件</el-checkbox>
          <el-checkbox v-model="form.show_token_stats">Token 统计</el-checkbox>
          <el-checkbox v-model="form.show_concurrency_card">并发卡片</el-checkbox>
          <el-checkbox v-model="form.show_switch_rate_chart">切换率趋势</el-checkbox>
        </div>
      </div>

      <div class="ops-settings-section">
        <div class="ops-settings-label">默认查询模式</div>
        <el-select v-model="form.default_query_mode" class="ops-settings-select">
          <el-option label="自动" value="auto" />
          <el-option label="原始数据" value="raw" />
          <el-option label="预聚合" value="preagg" />
        </el-select>
      </div>
    </div>

    <template #footer>
      <el-button @click="$emit('close')">取消</el-button>
      <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { opsAPI } from '@/api/ops'
import type { OpsDashboardSettings, OpsQueryMode } from '@/api/ops'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  saved: [settings: OpsDashboardSettings]
}>()

const loading = ref(false)
const saving = ref(false)

const form = ref<OpsDashboardSettings>({
  auto_refresh_interval: 30,
  show_alert_events: true,
  show_token_stats: true,
  show_concurrency_card: true,
  show_switch_rate_chart: true,
  default_query_mode: 'auto' as OpsQueryMode,
})

watch(() => props.show, async (val) => {
  if (val) {
    loading.value = true
    try {
      const settings = await opsAPI.getDashboardSettings()
      form.value = settings
    } catch {
      ElMessage.error('加载设置失败')
    } finally {
      loading.value = false
    }
  }
})

async function handleSave() {
  saving.value = true
  try {
    const updated = await opsAPI.updateDashboardSettings(form.value)
    ElMessage.success('设置已保存')
    emit('saved', updated)
    emit('close')
  } catch {
    ElMessage.error('保存设置失败')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.ops-settings-body {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.ops-settings-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ops-settings-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.ops-settings-select {
  width: 200px;
}

.ops-settings-checkboxes {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}
</style>