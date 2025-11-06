<template>
  <div class="model-list-page">
    <div class="page-header">
      <h2 class="page-title">可用模型</h2>
      <div class="header-actions">
        <el-button v-if="error" type="primary" @click="loadModels" size="small">刷新</el-button>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom:16px" />

    <el-table v-loading="loading" :data="models" border stripe>
      <el-table-column label="模型" min-width="240">
        <template #default="{ row }">
          <span class="mono-text">{{ row.id }}</span>
        </template>
      </el-table-column>
      <el-table-column label="类型" width="120" align="center">
        <template #default="{ row }">
          <el-tag :type="row.type === 'chat' ? 'primary' : 'warning'" size="small" effect="plain">
            {{ row.type || 'unknown' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="owned_by" label="厂商" min-width="160" show-overflow-tooltip />
      <el-table-column label="健康" width="120" align="center">
        <template #default="{ row }">
          <div class="health-cell">
            <template v-if="row.consecutive_failures >= 3">
              <el-tooltip :content="row.last_check_error || '连续失败达阈值'" placement="top">
                <div class="health-inner">
                  <span class="health-dot health-error" />
                  <span class="health-text health-error-text">异常</span>
                </div>
              </el-tooltip>
            </template>
            <template v-else-if="row.consecutive_failures > 0">
              <el-tooltip :content="row.last_check_error || '偶发失败'" placement="top">
                <div class="health-inner">
                  <span class="health-dot health-warn" />
                  <span class="health-text health-warn-text">告警</span>
                </div>
              </el-tooltip>
            </template>
            <template v-else-if="row.response_time > 0">
              <div class="health-inner">
                <span class="health-dot health-ok" />
                <span class="health-text health-ok-text">{{ row.response_time }}ms</span>
              </div>
            </template>
            <template v-else>
              <div class="health-inner">
                <span class="health-dot health-unknown" />
                <span class="health-text text-muted">未检测</span>
              </div>
            </template>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100" align="center">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'danger'" size="small" effect="plain">
            {{ row.enabled ? '可用' : '不可用' }}
          </el-tag>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无可用的模型" />
      </template>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchMyModels } from '@/services/gateway-api'
import type { MeModelItem } from '@/services/gateway-api'

const loading = ref(false)
const error = ref('')
const models = ref<MeModelItem[]>([])

async function loadModels() {
  loading.value = true
  error.value = ''
  try {
    models.value = await fetchMyModels()
  } catch (e: any) {
    error.value = '加载模型列表失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(() => { loadModels() })
</script>

<style scoped>
.model-list-page { padding: 0; }

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

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.mono-text {
  font-family: monospace;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.health-cell { display: flex; justify-content: center; }
.health-inner { display: flex; align-items: center; gap: 6px; }
.health-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.health-ok      { background: #27AE60; }
.health-warn    { background: #EAB308; }
.health-error   { background: #E74C3C; }
.health-unknown { background: #CBD5E1; }
.health-text { font-size: 12px; line-height: 1; }
.health-ok-text    { color: #27AE60; }
.health-warn-text  { color: #EAB308; }
.health-error-text { color: #E74C3C; }
.text-muted { color: #94A3B8; }

@media (max-width: 900px) {
  .page-header { flex-direction: column; align-items: flex-start; gap: 12px; }
}
</style>