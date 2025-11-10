<template>
  <div class="health-monitor-page">
    <!-- 外层 Card -->
    <div class="monitor-card-outer">
      <!-- Card Header -->
      <div class="card-header">
        <h3 class="card-title">健康监控</h3>
        <div class="header-actions">
          <span class="lookback-label">回溯时间：</span>
          <el-select v-model="lookback" class="lookback-select" size="small" @change="loadTimeline">
            <el-option label="6 小时" value="6h" />
            <el-option label="12 小时" value="12h" />
            <el-option label="24 小时" value="24h" />
            <el-option label="7 天" value="7d" />
          </el-select>
          <el-button :loading="loading" :icon="Refresh" size="small" circle @click="refreshData" />
        </div>
      </div>

      <!-- Card Body -->
      <div class="card-body">
        <div v-if="loadingMonitors" class="state-loading">
          <el-icon class="is-loading" size="20"><Loading /></el-icon>
          <span>加载中...</span>
        </div>

        <div v-else-if="groups.length === 0" class="state-empty">
          <p>暂无健康监控数据</p>
          <p class="sub">端点尚未产生请求记录</p>
        </div>

        <div v-else class="monitor-list">
          <div
            v-for="group in groups"
            :key="group.target_name"
            class="monitor-item"
          >
            <div class="item-row">
              <!-- 左侧：信息区域 -->
              <div class="item-info">
                <div class="info-line">
                  <el-tag class="format-badge" type="info" size="small" effect="plain">
                    <span class="mono">{{ group.target_name }}</span>
                  </el-tag>
                  <el-tag
                    v-if="group.total_checks > 0"
                    :type="rateType(group.health_percent)"
                    size="small"
                    effect="dark"
                  >
                    {{ group.health_percent }}%
                  </el-tag>
                </div>
                <div class="provider-line">
                  {{ group.provider_count }} 个提供商 / {{ group.key_count }} 个密钥
                </div>
              </div>

              <!-- 右侧：时间线 -->
              <div class="item-timeline">
                <HealthTimeline
                  :timeline="group.timeline"
                  :lookback="lookback"
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { Refresh, Loading } from '@element-plus/icons-vue'
import { getHealthTimeline } from '@/services/control-plane'
import type { TimelineGroup } from '@/services/control-plane'
import HealthTimeline from '@/components/admin/HealthTimeline.vue'

const loading = ref(false)
const loadingMonitors = ref(false)
const groups = ref<TimelineGroup[]>([])
const lookback = ref('6h')
let timer: ReturnType<typeof setInterval> | null = null

async function loadTimeline() {
  loadingMonitors.value = true
  try {
    const result = await getHealthTimeline(lookback.value)
    groups.value = result.groups || []
  } catch {
    groups.value = []
  } finally {
    loadingMonitors.value = false
  }
}

async function refreshData() {
  loading.value = true
  try { await loadTimeline() } finally { loading.value = false }
}

function rateType(percent: number): 'success' | 'warning' | 'danger' {
  if (percent >= 95) return 'success'
  if (percent >= 80) return 'warning'
  return 'danger'
}

onMounted(() => { refreshData(); timer = setInterval(loadTimeline, 60000) })
onUnmounted(() => { if (timer) { clearInterval(timer); timer = null } })
</script>

<style scoped>
.health-monitor-page { max-width: 1200px; }

/* Card */
.monitor-card-outer {
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
  overflow: hidden;
  box-shadow: 0 1px 2px rgba(0,0,0,0.04);
}

/* Header */
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.card-title { font-size: 15px; font-weight: 600; margin: 0; color: var(--el-text-color-primary); }
.header-actions { display: flex; align-items: center; gap: 8px; }
.lookback-label { font-size: 12px; color: var(--el-text-color-secondary); }
.lookback-select { width: 100px; }

/* Body */
.card-body { padding: 20px; }

/* States */
.state-loading {
  display: flex; align-items: center; justify-content: center;
  gap: 8px; padding: 40px 0; color: var(--el-text-color-secondary);
}
.state-empty {
  display: flex; flex-direction: column; align-items: center;
  padding: 40px 0; color: var(--el-text-color-secondary);
}
.state-empty .sub { font-size: 12px; margin-top: 4px; }

/* Monitor items */
.monitor-list { display: flex; flex-direction: column; gap: 10px; }

.monitor-item {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 14px 16px;
  transition: border-color 0.15s;
}
.monitor-item:hover { border-color: var(--el-color-primary-light-5); }

.item-row {
  display: flex;
  align-items: center;
  gap: 20px;
}

/* Left: info */
.item-info { flex-shrink: 0; width: 200px; }

.info-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }

.format-badge { cursor: default; }
.mono { font-family: monospace; font-size: 11px; }

.provider-line {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

/* Right: timeline */
.item-timeline { flex: 1; min-width: 0; }

@media (max-width: 768px) {
  .item-row { flex-direction: column; align-items: stretch; gap: 10px; }
  .item-info { width: 100%; }
  .card-header { flex-direction: column; align-items: flex-start; gap: 8px; }
}
</style>