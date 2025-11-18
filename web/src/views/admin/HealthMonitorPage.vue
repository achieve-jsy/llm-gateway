<template>
  <div class="health-monitor-page">
    <div class="monitor-card-outer">
      <div class="card-header">
        <h3 class="card-title">模型健康监控</h3>
        <div class="header-actions">
          <el-input v-model="searchText" placeholder="搜索模型名称" size="small" clearable class="search-input" @keyup.enter="onSearch" @clear="onSearch" />
          <span class="lookback-label">回溯时间：</span>
          <el-select v-model="lookback" class="lookback-select" size="small" @change="onLookbackChange">
            <el-option label="6 小时" value="6h" />
            <el-option label="12 小时" value="12h" />
            <el-option label="24 小时" value="24h" />
            <el-option label="7 天" value="7d" />
          </el-select>
          <el-button :loading="loading" :icon="Refresh" size="small" circle @click="refreshData" />
        </div>
      </div>

      <div class="card-body scroll-body" @scroll="onScroll">
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
            :key="group.target_name + group.provider_name"
            class="monitor-item"
          >
            <div class="item-row">
              <div class="item-info">
                <div class="info-line">
                  <el-tooltip :content="group.target_name" :show-after="300">
                    <el-tag class="format-badge" type="info" size="small" effect="plain">
                      <span class="mono">{{ group.target_name }}</span>
                    </el-tag>
                  </el-tooltip>
                  <el-tag
                    v-if="group.total_checks > 0"
                    :type="rateType(group.health_percent)"
                    size="small"
                    effect="dark"
                  >
                    {{ group.health_percent }}%
                  </el-tag>
                </div>
                <div class="provider-line" v-if="group.provider_name">
                  渠道：{{ group.provider_name }}
                </div>
              </div>
              <div class="item-timeline">
                <HealthTimeline :timeline="group.timeline" :lookback="lookback" />
              </div>
            </div>
          </div>

          <div v-if="loadingMore" class="state-loading" style="padding:12px 0">
            <el-icon class="is-loading" size="16"><Loading /></el-icon>
            <span style="font-size:12px">加载更多...</span>
          </div>
          <div v-else-if="groups.length >= total" class="state-end">
            <span>共 {{ total }} 个模型</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { Refresh, Loading } from '@element-plus/icons-vue'
import { getHealthTimeline } from '@/services/control-plane'
import type { TimelineGroup } from '@/services/control-plane'
import HealthTimeline from '@/components/admin/HealthTimeline.vue'

const PAGE_SIZE = 10
const loading = ref(false)
const loadingMonitors = ref(false)
const loadingMore = ref(false)
const groups = shallowRef<TimelineGroup[]>([])
const total = ref(0)
const lookback = ref('6h')
const searchText = ref('')
let timer: ReturnType<typeof setInterval> | null = null

async function loadTimeline(reset: boolean = true) {
  if (reset) {
    loadingMonitors.value = true
  } else {
    loadingMore.value = true
  }
  try {
    const offset = reset ? 0 : groups.value.length
    const result = await getHealthTimeline(lookback.value, searchText.value, offset, PAGE_SIZE)
    if (reset) {
      groups.value = result.groups
    } else {
      groups.value = [...groups.value, ...result.groups]
    }
    total.value = result.total
  } catch {
    if (reset) groups.value = []
  } finally {
    loadingMonitors.value = false
    loadingMore.value = false
  }
}

async function refreshData() {
  loading.value = true
  try { await loadTimeline(true) } finally { loading.value = false }
}

function onLookbackChange() {
  loadTimeline(true)
}

function onSearch() {
  loadTimeline(true)
}

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (loadingMore.value) return
  if (groups.value.length >= total.value) return
  const { scrollTop, scrollHeight, clientHeight } = el
  if (scrollHeight - scrollTop - clientHeight < 60) {
    loadTimeline(false)
  }
}

function rateType(percent: number): 'success' | 'warning' | 'danger' {
  if (percent >= 95) return 'success'
  if (percent >= 80) return 'warning'
  return 'danger'
}

onMounted(() => {
  refreshData()
  timer = setInterval(() => loadTimeline(true), 60000)
})
onBeforeUnmount(() => { if (timer) { clearInterval(timer); timer = null } })
</script>

<style scoped>
.health-monitor-page {
  width: 100%;
}

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
  flex-wrap: wrap;
  gap: 8px;
}
.card-title { font-size: 15px; font-weight: 600; margin: 0; color: var(--el-text-color-primary); white-space: nowrap; }
.header-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.lookback-label { font-size: 12px; color: var(--el-text-color-secondary); }
.lookback-select { width: 100px; }
.search-input { width: 180px; }

/* Body */
.card-body { padding: 20px; }
.scroll-body {
  max-height: calc(100vh - 200px);
  min-height: 200px;
  overflow-y: auto;
}

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
.state-end { text-align: center; padding: 8px 0; font-size: 12px; color: var(--el-text-color-secondary); }

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
.item-info { flex-shrink: 0; min-width: 180px; max-width: 260px; }
.info-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.format-badge { cursor: default; max-width: 180px; overflow: hidden; min-width: 0; }
.format-badge :deep(.el-tag__content) { overflow: hidden; }
.mono { font-family: monospace; font-size: 11px; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.provider-line {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

/* Right: timeline */
.item-timeline { flex: 1; min-width: 0; }

/* ─── 平板 (768 ~ 1024) ─── */
@media (max-width: 1024px) {
  .search-input { width: 140px; }
  .item-info { min-width: 150px; max-width: 200px; }
}

/* ─── 窄屏 / 手机 (≤768) ─── */
@media (max-width: 768px) {
  .card-header { flex-direction: column; align-items: stretch; }
  .header-actions { justify-content: space-between; }
  .search-input { flex: 1; width: 100%; }
  .lookback-select { flex-shrink: 0; }
  .card-title { font-size: 14px; }

  .card-body { padding: 14px 12px; }
  .scroll-body { max-height: calc(100vh - 160px); }

  .item-row { flex-direction: column; align-items: stretch; gap: 10px; }
  .item-info { min-width: 0; max-width: 100%; width: 100%; }
  .monitor-item { padding: 12px; }

  .state-loading, .state-empty { padding: 24px 0; }
}

/* ─── 小屏手机 (≤480) ─── */
@media (max-width: 480px) {
  .header-actions { flex-direction: column; align-items: stretch; gap: 6px; }
  .lookback-label { display: none; }
  .lookback-select { width: 100%; }
  .search-input { width: 100%; }

  .card-header { padding: 10px 12px; }
  .card-body { padding: 10px 8px; }
  .monitor-item { padding: 10px; border-radius: 6px; }
  .monitor-list { gap: 8px; }
  .card-title { font-size: 13px; }
}
</style>