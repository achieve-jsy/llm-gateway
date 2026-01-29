<template>
  <div class="ops-card" v-loading="loading">
    <!-- Header -->
    <div class="ops-card-header">
      <h3 class="ops-card-title">渠道并发状态</h3>
      <div class="ops-header-right">
        <el-radio-group
          v-model="viewMode"
          size="small"
          class="ops-view-toggle"
          @change="onViewChange"
        >
          <el-radio-button value="provider">Provider</el-radio-button>
          <el-radio-button value="user">User</el-radio-button>
        </el-radio-group>
        <el-button
          size="small"
          :icon="Refresh"
          :loading="loading"
          circle
          @click="fetchData"
          title="刷新"
          class="ops-refresh-btn"
        />
      </div>
    </div>

    <!-- Provider 视图 -->
    <template v-if="viewMode === 'provider'">
      <div class="ops-concurrency-summary" v-if="stats">
        <span class="ops-stat-chip">总计 {{ stats.total_providers }}</span>
        <span class="ops-stat-chip ops-stat-chip--green">可用 {{ stats.available_providers }}</span>
        <span class="ops-stat-chip ops-stat-chip--amber">限流 {{ stats.rate_limited_providers }}</span>
        <span class="ops-stat-chip ops-stat-chip--red">错误 {{ stats.error_providers }}</span>
      </div>

      <div class="ops-concurrency-list" v-if="stats && stats.providers.length > 0">
        <div
          v-for="p in stats.providers"
          :key="p.provider_id"
          class="ops-concurrency-row"
        >
          <!-- 提供商信息 -->
          <div class="ops-provider-info">
            <span class="ops-provider-name">{{ p.provider_name }}</span>
            <span class="ops-provider-type">{{ p.provider_type }}</span>
          </div>

          <!-- 状态徽章 -->
          <el-tag
            :type="statusTagType(p.status)"
            size="small"
            effect="plain"
            class="ops-status-tag"
            round
          >
            <span class="ops-status-badge-icon">●</span>
            {{ statusLabel(p.status) }}
          </el-tag>

          <!-- 速率限制徽章 -->
          <el-tag
            v-if="p.is_rate_limited"
            type="danger"
            size="small"
            effect="dark"
            class="ops-rate-limit-badge"
            round
          >
            限流 {{ p.rate_limit_reset_seconds }}s
          </el-tag>

          <!-- Rate limit remaining -->
          <span
            v-if="!p.is_rate_limited && p.rate_limit_remaining >= 0"
            class="ops-rate-remaining"
            :class="{ 'ops-rate-remaining--low': p.rate_limit_remaining < 5 }"
          >
            余量 {{ p.rate_limit_remaining }}
          </span>

          <!-- 加载条 -->
          <div class="ops-load-bar-wrap">
            <div
              class="ops-load-bar"
              :style="{
                width: loadPercent(p) + '%',
                background: loadColor(p)
              }"
            ></div>
          </div>
          <span class="ops-load-text">{{ loadPercent(p) }}%</span>

          <!-- 活跃 / 队列 -->
          <span class="ops-active-count">活跃 {{ p.active_requests }}</span>
          <span class="ops-queue-count">队列 {{ p.waiting_queue }}</span>

          <!-- 5分钟错误数 -->
          <span
            class="ops-error-count"
            :class="{ 'ops-error-count--has': p.error_count_5min > 0 }"
          >
            错误 {{ p.error_count_5min }}
          </span>
        </div>
      </div>

      <div v-else class="ops-card-empty">
        {{ loading ? '加载中...' : '暂无渠道数据' }}
      </div>
    </template>

    <!-- User 视图 -->
    <template v-else>
      <div class="ops-user-stats-header" v-if="userStats">
        <span class="ops-stat-chip">活跃用户 {{ userStats.total_users }}</span>
      </div>

      <div class="ops-user-table-wrap" v-if="userStats && userStats.users.length > 0">
        <table class="ops-user-table">
          <thead>
            <tr>
              <th>用户ID</th>
              <th>请求数</th>
              <th>错误数</th>
              <th>平均耗时</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in userStats.users" :key="u.user_id">
              <td class="ops-user-id">{{ u.user_id }}</td>
              <td>{{ u.request_count }}</td>
              <td>
                <span :class="{ 'ops-error-count--has': u.error_count > 0 }">
                  {{ u.error_count }}
                </span>
              </td>
              <td>{{ u.avg_duration_ms }} ms</td>
              <td>
                <el-tag
                  :type="userStatusTagType(u.status)"
                  size="small"
                  effect="plain"
                  round
                  class="ops-status-tag"
                >
                  <span class="ops-status-badge-icon">●</span>
                  {{ userStatusLabel(u.status) }}
                </el-tag>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="ops-card-empty">
        {{ loading ? '加载中...' : '暂无用户数据' }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'
import type {
  OpsConcurrencyStatsResponse,
  OpsUserConcurrencyResponse,
  OpsProviderConcurrency
} from '@/api/ops'

// ---- Props ----
const props = withDefaults(
  defineProps<{
    platformFilter?: string
    groupIdFilter?: number | null
    refreshToken?: number
  }>(),
  {
    refreshToken: 0
  }
)

// ---- 状态 ----
const viewMode = ref<'provider' | 'user'>('provider')
const loading = ref(false)
const stats = ref<OpsConcurrencyStatsResponse | null>(null)
const userStats = ref<OpsUserConcurrencyResponse | null>(null)

// ---- 视图切换 ----
function onViewChange() {
  if (viewMode.value === 'provider' && !stats.value) {
    fetchProviderData()
  } else if (viewMode.value === 'user' && !userStats.value) {
    fetchUserData()
  }
}

// ---- 状态映射（Provider） ----
function statusTagType(status: OpsProviderConcurrency['status']): 'success' | 'warning' | 'danger' | 'info' {
  switch (status) {
    case 'available': return 'success'
    case 'rate_limited': return 'danger'
    case 'overloaded': return 'warning'
    case 'error': return 'info'
    case 'unavailable': return 'info'
    default: return 'info'
  }
}

function statusLabel(status: OpsProviderConcurrency['status']): string {
  switch (status) {
    case 'available': return '可用'
    case 'rate_limited': return '限流'
    case 'overloaded': return '过载'
    case 'error': return '错误'
    case 'unavailable': return '不可用'
    default: return status
  }
}

// ---- 状态映射（User） ----
function userStatusTagType(status: 'healthy' | 'warning' | 'critical'): 'success' | 'warning' | 'danger' {
  switch (status) {
    case 'healthy': return 'success'
    case 'warning': return 'warning'
    case 'critical': return 'danger'
    default: return 'success'
  }
}

function userStatusLabel(status: 'healthy' | 'warning' | 'critical'): string {
  switch (status) {
    case 'healthy': return '健康'
    case 'warning': return '警告'
    case 'critical': return '严重'
    default: return status
  }
}

// ---- 加载条 ----
function loadPercent(p: OpsProviderConcurrency): number {
  if (p.max_capacity <= 0) return 0
  return Math.min(100, Math.round((p.current_load / p.max_capacity) * 100))
}

function loadColor(p: OpsProviderConcurrency): string {
  const pct = loadPercent(p)
  if (pct <= 60) return '#10b981'
  if (pct <= 80) return '#f59e0b'
  return '#ef4444'
}

// ---- 数据获取 ----
async function fetchProviderData() {
  try {
    stats.value = await opsAPI.getConcurrencyStats()
  } catch {
    stats.value = null
  }
}

async function fetchUserData() {
  try {
    userStats.value = await opsAPI.getUserConcurrencyStats()
  } catch {
    userStats.value = null
  }
}

async function fetchData() {
  loading.value = true
  try {
    if (viewMode.value === 'provider') {
      await fetchProviderData()
    } else {
      await fetchUserData()
    }
  } catch {
    // 静默处理
  } finally {
    loading.value = false
  }
}

// ---- Watch refreshToken ----
watch(
  () => props.refreshToken,
  () => {
    fetchData()
  }
)

// ---- 生命周期 ----
onMounted(() => {
  fetchData()
})
</script>

<style scoped>
/* ====== 卡片容器 ====== */
.ops-card {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 24px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

/* ====== 卡片头部 ====== */
.ops-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 8px;
}

.ops-card-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
  margin: 0;
}

.ops-header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ops-view-toggle {
  --el-radio-button-checked-bg-color: var(--el-color-primary-light-9);
}

.ops-view-toggle :deep(.el-radio-button__inner) {
  padding: 5px 14px;
  font-size: 12px;
  border-radius: 16px !important;
}

.ops-refresh-btn {
  width: 28px;
  height: 28px;
}

/* ====== 统计摘要芯片 ====== */
.ops-concurrency-summary {
  display: flex;
  gap: 6px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}

.ops-user-stats-header {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
}

.ops-stat-chip {
  font-size: 11px;
  padding: 2px 10px;
  border-radius: 12px;
  background: var(--bg-subtle);
  color: var(--text-secondary);
  white-space: nowrap;
}

.ops-stat-chip--green {
  background: rgba(16, 185, 129, 0.12);
  color: #10b981;
}

.ops-stat-chip--amber {
  background: rgba(245, 158, 11, 0.12);
  color: #f59e0b;
}

.ops-stat-chip--red {
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
}

/* ====== Provider 列表 ====== */
.ops-concurrency-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.ops-concurrency-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 8px;
  border-radius: 8px;
  transition: background-color 0.15s;
}

.ops-concurrency-row:hover {
  background-color: var(--bg-subtle);
}

/* ====== 提供商信息 ====== */
.ops-provider-info {
  display: flex;
  flex-direction: column;
  min-width: 90px;
  flex-shrink: 0;
}

.ops-provider-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.3;
}

.ops-provider-type {
  font-size: 11px;
  color: var(--text-placeholder);
}

/* ====== 状态徽章 ====== */
.ops-status-tag {
  padding: 4px 12px !important;
  font-size: 12px !important;
  border-radius: 16px !important;
  flex-shrink: 0;
}

.ops-status-badge-icon {
  font-size: 8px;
  margin-right: 2px;
  vertical-align: 0.5px;
}

/* ====== 速率限制徽章 ====== */
.ops-rate-limit-badge {
  padding: 2px 10px !important;
  font-size: 11px !important;
  border-radius: 12px !important;
  flex-shrink: 0;
  animation: ops-pulse 2s ease-in-out infinite;
}

@keyframes ops-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}

.ops-rate-remaining {
  font-size: 11px;
  color: var(--text-placeholder);
  white-space: nowrap;
  flex-shrink: 0;
  width: 52px;
}

.ops-rate-remaining--low {
  color: #f59e0b;
  font-weight: 600;
}

/* ====== 加载条 ====== */
.ops-load-bar-wrap {
  flex: 1;
  height: 8px;
  background: var(--bg-subtle);
  border-radius: 4px;
  overflow: hidden;
  min-width: 60px;
}

.ops-load-bar {
  height: 100%;
  border-radius: 4px;
  transition: width 0.3s ease, background-color 0.3s ease;
}

/* ====== 数值文本 ====== */
.ops-load-text {
  font-size: 11px;
  color: var(--text-secondary);
  width: 36px;
  text-align: right;
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}

.ops-active-count,
.ops-queue-count {
  font-size: 11px;
  color: var(--text-placeholder);
  width: 52px;
  flex-shrink: 0;
  white-space: nowrap;
}

.ops-error-count {
  font-size: 11px;
  color: var(--text-placeholder);
  width: 52px;
  flex-shrink: 0;
  white-space: nowrap;
}

.ops-error-count--has {
  color: #ef4444;
  font-weight: 600;
}

/* ====== User 视图表格 ====== */
.ops-user-table-wrap {
  overflow-x: auto;
}

.ops-user-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.ops-user-table th {
  text-align: left;
  padding: 8px 12px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-default);
  white-space: nowrap;
}

.ops-user-table td {
  padding: 10px 12px;
  color: var(--text-primary);
  border-bottom: 1px solid var(--border-light);
}

.ops-user-table tbody tr {
  transition: background-color 0.15s;
}

.ops-user-table tbody tr:hover {
  background-color: var(--bg-subtle);
}

.ops-user-table tbody tr:last-child td {
  border-bottom: none;
}

.ops-user-id {
  font-family: var(--font-family-mono, monospace);
  font-size: 12px;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ====== 空状态 ====== */
.ops-card-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100px;
  color: var(--text-placeholder);
  font-size: 13px;
}

/* ====== 响应式 ====== */
@media (max-width: 900px) {
  .ops-concurrency-row {
    flex-wrap: wrap;
    gap: 8px;
  }
  .ops-load-bar-wrap {
    order: 10;
    flex-basis: 100%;
  }
  .ops-rate-remaining,
  .ops-error-count,
  .ops-active-count,
  .ops-queue-count {
    width: auto;
  }
  .ops-card {
    border-radius: 16px;
    padding: 16px;
  }
}
</style>
