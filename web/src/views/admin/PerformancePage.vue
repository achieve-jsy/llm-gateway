<template>
  <div class="performance-page">
    <div class="monitor-card-outer">
      <div class="card-header">
        <div class="header-tabs">
          <el-radio-group v-model="view" size="small" @change="onViewChange">
            <el-radio-button value="group">模型组视角</el-radio-button>
            <el-radio-button value="model">模型视角</el-radio-button>
          </el-radio-group>
        </div>
        <div class="header-actions">
          <el-input v-model="searchText" placeholder="搜索名称" size="small" clearable class="search-input"
            :prefix-icon="Search" @keyup.enter="onSearch" @clear="onSearch" />
          <el-select v-model="sortBy" size="small" class="sort-select">
            <el-option label="按稳定性" value="stability" />
            <el-option label="按首 Token" value="ttft" />
            <el-option label="按延迟" value="latency" />
            <el-option label="按请求量" value="req" />
          </el-select>
          <el-radio-group v-model="windowVal" size="small" @change="onWindowChange">
            <el-radio-button value="1h">1h</el-radio-button>
            <el-radio-button value="6h">6h</el-radio-button>
            <el-radio-button value="24h">24h</el-radio-button>
          </el-radio-group>
          <el-button :loading="loading" :icon="Refresh" size="small" circle @click="refreshData" />
        </div>
      </div>

      <!-- 说明：数据口径 -->
      <div class="info-strip">
        <el-icon class="info-icon"><InfoFilled /></el-icon>
        <span>
          性能指标基于<strong>所选时间窗口（{{ windowVal }}）内的真实请求</strong>统计。
          <template v-if="view === 'group'">
            窗口内无人调用的模型组没有性能数据，会单独列在下方「无流量」区，并非异常。
          </template>
        </span>
      </div>

      <!-- 概览统计 -->
      <div v-if="!loading && sortedItems.length > 0" class="overview-bar">
        <div class="overview-item">
          <span class="overview-num">{{ statusCounts.total }}</span>
          <span class="overview-label">有流量{{ view === 'group' ? '组' : '模型' }}</span>
        </div>
        <div class="overview-item is-excellent">
          <span class="overview-num">{{ statusCounts.excellent }}</span>
          <span class="overview-label">稳定</span>
        </div>
        <div class="overview-item is-good">
          <span class="overview-num">{{ statusCounts.good }}</span>
          <span class="overview-label">良好</span>
        </div>
        <div class="overview-item is-fair">
          <span class="overview-num">{{ statusCounts.fair }}</span>
          <span class="overview-label">波动</span>
        </div>
        <div class="overview-item is-poor">
          <span class="overview-num">{{ statusCounts.poor }}</span>
          <span class="overview-label">异常</span>
        </div>
      </div>

      <div class="card-body scroll-body">
        <div v-if="loading && sortedItems.length === 0" class="state-loading">
          <el-icon class="is-loading" size="20"><Loading /></el-icon>
          <span>加载中...</span>
        </div>

        <div v-else-if="sortedItems.length === 0" class="state-empty">
          <el-icon class="empty-icon" :size="40"><DataLine /></el-icon>
          <p class="empty-title">当前 {{ windowVal }} 窗口内暂无调用</p>
          <p class="sub">
            <template v-if="view === 'group' && zeroTrafficGroups.length > 0">
              已配置 {{ zeroTrafficGroups.length }} 个模型组，但该时间窗口内没有任何请求记录。
            </template>
            <template v-else>该时间窗口内没有请求记录。</template>
          </p>
          <div class="empty-actions" v-if="windowVal !== '24h'">
            <el-button size="small" type="primary" plain @click="setWindow('24h')">查看近 24 小时</el-button>
          </div>
        </div>

        <div v-else class="card-grid">
          <div
            v-for="item in sortedItems"
            :key="item.name"
            class="perf-row"
            :class="['status-' + computeStatus(item).key, { expanded: expandedGroups.has(item.name) }]"
          >
            <div class="perf-row-main" @click="toggleExpand(item.name)">
              <!-- 状态指示 -->
              <div class="status-cell">
                <span class="status-dot" :class="'dot-' + computeStatus(item).key"></span>
                <span class="status-text" :class="'text-' + computeStatus(item).key">
                  {{ computeStatus(item).label }}
                </span>
              </div>

              <!-- 名称区 -->
              <div class="name-cell">
                <el-tooltip :content="item.name" :show-after="400" :disabled="item.name.length < 24">
                  <span class="row-name">{{ item.name }}</span>
                </el-tooltip>
                <div class="name-tags">
                  <el-tag v-if="view === 'group' && item.strategy"
                    :type="strategyTagType(item.strategy)" size="small" effect="plain" class="mini-tag">
                    {{ strategyLabel(item.strategy) }}
                  </el-tag>
                  <span v-if="view === 'group' && item.model_count" class="meta-text">
                    {{ item.model_count }} 模型
                  </span>
                  <span v-if="view === 'model' && item.used_by_groups" class="meta-text">
                    {{ item.used_by_groups }} 组在用
                  </span>
                  <span class="meta-text">{{ item.req_count }} 请求</span>
                </div>
              </div>

              <!-- 成功率 -->
              <div class="rate-cell">
                <span class="rate-value" :class="'text-' + computeStatus(item).key">
                  {{ formatPct(item.success_rate) }}
                </span>
                <span class="rate-label">成功率</span>
              </div>

              <!-- 首 Token 横向条 -->
              <div class="ttft-cell">
                <div class="ttft-head">
                  <span class="ttft-label">首 Token</span>
                  <span class="ttft-value">{{ formatMs(item.ttft_p50_ms) }}</span>
                </div>
                <div class="ttft-track">
                  <div
                    class="ttft-fill"
                    :class="'fill-' + speedLevel(item.ttft_p50_ms)"
                    :style="{ width: ttftBarWidth(item.ttft_p50_ms) }"
                  ></div>
                </div>
              </div>

              <!-- 展开箭头 -->
              <div class="expand-cell">
                <el-icon :size="16" class="chevron">
                  <ArrowDown />
                </el-icon>
              </div>
            </div>

            <!-- 详情（默认隐藏） -->
            <transition name="slide">
              <div v-if="expandedGroups.has(item.name)" class="perf-detail">
                <div class="detail-metrics">
                  <div class="detail-metric">
                    <span class="dm-label">总延迟 P50 / P95</span>
                    <span class="dm-value">{{ formatMs(item.p50_ms) }} / {{ formatMs(item.p95_ms) }}</span>
                  </div>
                  <div class="detail-metric">
                    <span class="dm-label">首 Token P50 / P95</span>
                    <span class="dm-value">{{ formatMs(item.ttft_p50_ms) }} / {{ formatMs(item.ttft_p95_ms) }}</span>
                  </div>
                  <div v-if="item.pct_in_24h_p50 != null" class="detail-metric">
                    <span class="dm-label">24h 延迟水平</span>
                    <span class="dm-value" :class="pctClass(item.pct_in_24h_p50)">
                      P{{ item.pct_in_24h_p50 }}（{{ percentileLabel(item.pct_in_24h_p50) }}）
                    </span>
                  </div>
                  <div v-if="item.failover_count > 0" class="detail-metric">
                    <span class="dm-label">Failover 次数</span>
                    <span class="dm-value is-warn">{{ item.failover_count }}</span>
                  </div>
                  <div v-if="item.err_429 > 0" class="detail-metric">
                    <span class="dm-label">限流 (429)</span>
                    <span class="dm-value is-warn">{{ item.err_429 }}</span>
                  </div>
                  <div v-if="item.err_5xx > 0" class="detail-metric">
                    <span class="dm-label">服务端错误 (5xx)</span>
                    <span class="dm-value is-danger">{{ item.err_5xx }}</span>
                  </div>
                </div>

                <div v-if="view === 'group' && item.members && item.members.length > 0" class="members-list">
                  <div class="members-title">组内模型流量</div>
                  <div v-for="member in item.members" :key="member.model_name + member.provider" class="member-row">
                    <el-tag size="small" effect="plain">{{ member.model_name }}</el-tag>
                    <div class="member-bar-track">
                      <div class="member-bar-fill" :style="{ width: (member.traffic_pct * 100).toFixed(0) + '%' }"></div>
                    </div>
                    <span class="member-traffic">{{ (member.traffic_pct * 100).toFixed(0) }}%</span>
                    <span class="member-metrics">P50 {{ formatMs(member.p50_ms) }} · 成功率 {{ formatPct(member.success_rate) }}</span>
                  </div>
                </div>
              </div>
            </transition>
          </div>
        </div>

        <!-- 无流量模型组 -->
        <div v-if="view === 'group' && zeroTrafficGroups.length > 0" class="zero-traffic">
          <div class="zero-header" @click="zeroExpanded = !zeroExpanded">
            <el-icon :size="14" class="chevron" :class="{ rotated: zeroExpanded }"><ArrowDown /></el-icon>
            <span>无流量模型组</span>
            <el-tag size="small" type="info" effect="plain" round>{{ zeroTrafficGroups.length }}</el-tag>
            <span class="zero-hint">所选 {{ windowVal }} 窗口内无请求，因此没有性能指标</span>
          </div>
          <transition name="slide">
            <div v-if="zeroExpanded" class="zero-grid">
              <div v-for="g in zeroTrafficGroups" :key="g.name" class="zero-chip">
                <span class="zero-dot"></span>
                <span class="zero-name">{{ g.name }}</span>
              </div>
            </div>
          </transition>
        </div>

        <div v-if="sortedItems.length > 0" class="state-end">
          共 {{ sortedItems.length }} 个有流量的{{ view === 'group' ? '模型组' : '模型' }}
          <template v-if="view === 'group' && zeroTrafficGroups.length > 0">
            ，{{ zeroTrafficGroups.length }} 个无流量
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, computed, shallowRef } from 'vue'
import { Refresh, Loading, ArrowDown, Search, InfoFilled, DataLine } from '@element-plus/icons-vue'
import { getPerformance } from '@/services/control-plane'
import type { PerformanceItem } from '@/services/control-plane'
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement, GroupListItem } from '@/types/group'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const PAGE_SIZE = 50
const loading = ref(false)
const items = shallowRef<PerformanceItem[]>([])
const windowVal = ref('1h')
const view = ref('group')
const searchText = ref('')
const sortBy = ref<'stability' | 'ttft' | 'latency' | 'req'>('stability')
let timer: ReturnType<typeof setInterval> | null = null

const expandedGroups = ref<Set<string>>(new Set())
const zeroExpanded = ref(false)

// 全量组（用于无流量识别 + 授权过滤）
const allGroups = ref<GroupListItem[]>([])

// 授权相关
const entitlementsMap = ref<Map<string, GroupEntitlement[]>>(new Map())
const openModeMap = ref<Map<string, string>>(new Map())
const myDeptIds = ref<string[]>([])
const myStudioId = ref('')

// ─── 状态计算 ──────────────────────────────────────────────────────────────
interface StatusInfo {
  key: 'excellent' | 'good' | 'fair' | 'poor'
  label: string
}

function computeStatus(item: PerformanceItem): StatusInfo {
  const sr = item.success_rate
  const pct = item.pct_in_24h_p50
  if (sr < 0.85) return { key: 'poor', label: '异常' }
  if (sr < 0.95) return { key: 'fair', label: '波动' }
  // sr >= 0.95
  if (sr >= 0.99 && (pct == null || pct <= 70)) return { key: 'excellent', label: '稳定' }
  if (pct != null && pct > 85) return { key: 'fair', label: '偏慢' }
  return { key: 'good', label: '良好' }
}

const statusCounts = computed(() => {
  const c = { total: 0, excellent: 0, good: 0, fair: 0, poor: 0 }
  for (const it of sortedItems.value) {
    c.total++
    c[computeStatus(it).key]++
  }
  return c
})

const statusRank: Record<StatusInfo['key'], number> = { excellent: 0, good: 1, fair: 2, poor: 3 }

const sortedItems = computed(() => {
  const arr = [...items.value]
  switch (sortBy.value) {
    case 'stability':
      arr.sort((a, b) => {
        const ra = statusRank[computeStatus(a).key]
        const rb = statusRank[computeStatus(b).key]
        if (ra !== rb) return ra - rb
        if (b.success_rate !== a.success_rate) return b.success_rate - a.success_rate
        return (a.ttft_p50_ms || 0) - (b.ttft_p50_ms || 0)
      })
      break
    case 'ttft':
      arr.sort((a, b) => (a.ttft_p50_ms || 0) - (b.ttft_p50_ms || 0))
      break
    case 'latency':
      arr.sort((a, b) => (a.p50_ms || 0) - (b.p50_ms || 0))
      break
    case 'req':
      arr.sort((a, b) => (b.req_count || 0) - (a.req_count || 0))
      break
  }
  return arr
})

const maxTtft = computed(() => {
  let m = 1
  for (const it of items.value) {
    if (it.ttft_p50_ms > m) m = it.ttft_p50_ms
  }
  return m
})

function ttftBarWidth(ttft: number): string {
  const pct = Math.max(4, Math.round((ttft / maxTtft.value) * 100))
  return pct + '%'
}

function speedLevel(ttft: number): 'fast' | 'mid' | 'slow' {
  if (ttft <= 1500) return 'fast'
  if (ttft <= 4000) return 'mid'
  return 'slow'
}

// 无流量组（有授权但窗口内无请求）
const zeroTrafficGroups = computed(() => {
  if (view.value !== 'group') return []
  const trafficNames = new Set(items.value.map(i => i.name))
  const role = authStore.effectiveRole
  return allGroups.value
    .filter(g => g.enabled && !trafficNames.has(g.name))
    .filter(g => {
      if (role === 'admin' || role == null) return true
      return isGroupAvailable(g.name)
    })
    .map(g => ({ name: g.name, strategy: g.strategy }))
    .sort((a, b) => a.name.localeCompare(b.name))
})

// 判断模型组是否对当前用户可用
function isGroupAvailable(groupName: string): boolean {
  const role = authStore.effectiveRole
  if (role === 'admin') return true
  if (openModeMap.value.get(groupName) === 'public') return true
  const ents = entitlementsMap.value.get(groupName) || []
  if (role === 'studio_admin' && myStudioId.value) {
    return ents.some(e => e.scope_type === 'studio' && e.scope_id === myStudioId.value && e.enabled)
  }
  if (role === 'dept_admin' && myDeptIds.value.length > 0) {
    return myDeptIds.value.some(deptId =>
      ents.some(e => e.scope_type === 'dept' && e.scope_id === deptId && e.enabled)
    )
  }
  return false
}

async function loadGroupsAndEntitlements() {
  const role = authStore.effectiveRole
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    const groups = res.data.data ?? []
    allGroups.value = groups

    const newOpenModeMap = new Map<string, string>()
    for (const g of groups) {
      newOpenModeMap.set(g.name, g.open_mode)
    }
    openModeMap.value = newOpenModeMap

    if (role === 'studio_admin' || role === 'dept_admin') {
      const deptRoles = authStore.orgRoles.filter(r => r.role === 'dept_admin' && r.scope_type === 'dept')
      myDeptIds.value = deptRoles.map(r => r.scope_id)
      const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin' && r.scope_type === 'studio')
      myStudioId.value = studioRole?.scope_id || ''

      const controlledGroups = groups.filter(g => g.open_mode === 'controlled' && g.enabled)
      const entitlementPromises = controlledGroups.map(async g => {
        try {
          const entRes = await groupsApi.listEntitlements(g.id)
          return { groupName: g.name, items: entRes.data.data?.items || [] }
        } catch {
          return { groupName: g.name, items: [] as GroupEntitlement[] }
        }
      })
      const entResults = await Promise.all(entitlementPromises)
      const newMap = new Map<string, GroupEntitlement[]>()
      for (const r of entResults) {
        newMap.set(r.groupName, r.items)
      }
      entitlementsMap.value = newMap
    }
  } catch {
    allGroups.value = []
    openModeMap.value = new Map()
    entitlementsMap.value = new Map()
  }
}

async function loadData() {
  loading.value = true
  try {
    const result = await getPerformance(windowVal.value, view.value, searchText.value, PAGE_SIZE)
    let filteredItems = result.items || []
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null && view.value === 'group') {
      filteredItems = filteredItems.filter(item => isGroupAvailable(item.name))
    }
    items.value = filteredItems
  } catch {
    items.value = []
  } finally {
    loading.value = false
  }
}

function refreshData() { loadData() }
function onWindowChange() { loadData() }
function setWindow(w: string) { windowVal.value = w; loadData() }
function onViewChange() { loadData() }
function onSearch() { loadData() }
function toggleExpand(name: string) {
  const s = new Set(expandedGroups.value)
  if (s.has(name)) s.delete(name); else s.add(name)
  expandedGroups.value = s
}

onMounted(async () => {
  await loadGroupsAndEntitlements()
  await loadData()
  timer = setInterval(() => loadData(), 60000)
})
onBeforeUnmount(() => { if (timer) { clearInterval(timer); timer = null } })

function formatMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}

function formatPct(rate: number): string {
  return (rate * 100).toFixed(1) + '%'
}

function strategyLabel(s: string): string {
  if (s === 'priority') return '优先级'
  if (s === 'weighted') return '加权'
  return '轮询'
}

function strategyTagType(s: string): 'success' | 'warning' | 'info' {
  if (s === 'priority') return 'success'
  if (s === 'weighted') return 'warning'
  return 'info'
}

function percentileLabel(pct: number | null | undefined): string | null {
  if (pct == null) return null
  if (pct <= 25) return '较快'
  if (pct <= 60) return '正常'
  if (pct <= 85) return '偏慢'
  return '较慢'
}

function pctClass(pct: number | null | undefined): string {
  if (pct == null) return ''
  if (pct <= 25) return 'is-fast'
  if (pct <= 60) return 'is-normal'
  if (pct <= 85) return 'is-slow'
  return 'is-very-slow'
}
</script>

<style scoped>
.performance-page {
  width: 100%;
}

.monitor-card-outer {
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color);
  overflow: hidden;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  flex-wrap: wrap;
  gap: 8px;
}

.header-tabs {
  flex-shrink: 0;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.search-input {
  width: 160px;
}

.sort-select {
  width: 120px;
}

/* 说明条 */
.info-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 20px;
  background: var(--el-color-primary-light-9);
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-size: 12px;
  color: var(--el-text-color-regular);
  line-height: 1.5;
}

.info-strip strong {
  color: var(--el-text-color-primary);
  font-weight: 600;
}

.info-icon {
  color: var(--el-color-primary);
  flex-shrink: 0;
}

/* 概览 */
.overview-bar {
  display: flex;
  align-items: stretch;
  gap: 0;
  padding: 12px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  flex-wrap: wrap;
}

.overview-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-width: 76px;
  padding: 2px 16px;
  border-right: 1px solid var(--el-border-color-lighter);
}

.overview-item:last-child {
  border-right: none;
}

.overview-num {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.1;
  color: var(--el-text-color-primary);
}

.overview-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}

.overview-item.is-excellent .overview-num { color: var(--el-color-success); }
.overview-item.is-good .overview-num { color: var(--el-color-primary); }
.overview-item.is-fair .overview-num { color: var(--el-color-warning); }
.overview-item.is-poor .overview-num { color: var(--el-color-danger); }

.card-body {
  padding: 16px 20px;
}

.scroll-body {
  max-height: calc(100vh - 280px);
  min-height: 200px;
  overflow-y: auto;
}

.state-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 0;
  color: var(--el-text-color-secondary);
}

.state-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 56px 20px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.empty-icon {
  color: var(--el-text-color-placeholder);
  margin-bottom: 12px;
}

.empty-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}

.state-empty .sub {
  font-size: 12px;
  margin-top: 6px;
  max-width: 420px;
  line-height: 1.6;
}

.empty-actions {
  margin-top: 16px;
}

.state-end {
  text-align: center;
  padding: 12px 0 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.card-grid {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 行卡片 */
.perf-row {
  border: 1px solid var(--el-border-color-lighter);
  border-left: 3px solid var(--el-border-color);
  border-radius: 8px;
  transition: border-color 0.15s, box-shadow 0.15s;
  background: var(--el-bg-color);
}

.perf-row.status-excellent { border-left-color: var(--el-color-success); }
.perf-row.status-good { border-left-color: var(--el-color-primary); }
.perf-row.status-fair { border-left-color: var(--el-color-warning); }
.perf-row.status-poor { border-left-color: var(--el-color-danger); }

.perf-row:hover {
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.06);
}

.perf-row-main {
  display: grid;
  grid-template-columns: 84px minmax(160px, 1.6fr) 90px minmax(150px, 1.3fr) 28px;
  align-items: center;
  gap: 14px;
  padding: 12px 14px;
  cursor: pointer;
  user-select: none;
}

/* 状态列 */
.status-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}

.status-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex-shrink: 0;
}

.dot-excellent { background: var(--el-color-success); box-shadow: 0 0 0 3px var(--el-color-success-light-8); }
.dot-good { background: var(--el-color-primary); box-shadow: 0 0 0 3px var(--el-color-primary-light-8); }
.dot-fair { background: var(--el-color-warning); box-shadow: 0 0 0 3px var(--el-color-warning-light-8); }
.dot-poor { background: var(--el-color-danger); box-shadow: 0 0 0 3px var(--el-color-danger-light-8); }

.status-text {
  font-size: 13px;
  font-weight: 600;
}

.text-excellent { color: var(--el-color-success); }
.text-good { color: var(--el-color-primary); }
.text-fair { color: var(--el-color-warning); }
.text-poor { color: var(--el-color-danger); }

/* 名称列 */
.name-cell {
  min-width: 0;
}

.row-name {
  display: block;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.name-tags {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 3px;
  flex-wrap: wrap;
}

.mini-tag {
  flex-shrink: 0;
}

.meta-text {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

/* 成功率列 */
.rate-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  text-align: right;
}

.rate-value {
  font-size: 18px;
  font-weight: 700;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}

.rate-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 1px;
}

/* 首 Token 列 */
.ttft-cell {
  min-width: 0;
}

.ttft-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 5px;
}

.ttft-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.ttft-value {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}

.ttft-track {
  height: 6px;
  border-radius: 3px;
  background: var(--el-fill-color);
  overflow: hidden;
}

.ttft-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.4s ease;
}

.fill-fast { background: var(--el-color-success); }
.fill-mid { background: var(--el-color-warning); }
.fill-slow { background: var(--el-color-danger); }

/* 展开列 */
.expand-cell {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-secondary);
}

.chevron {
  transition: transform 0.25s ease;
}

.perf-row.expanded .chevron {
  transform: rotate(180deg);
}

/* 详情 */
.perf-detail {
  border-top: 1px dashed var(--el-border-color-lighter);
  padding: 12px 14px 14px;
  overflow: hidden;
}

.detail-metrics {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 10px 16px;
}

.detail-metric {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.dm-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.dm-value {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}

.dm-value.is-warn { color: var(--el-color-warning); }
.dm-value.is-danger { color: var(--el-color-danger); }
.dm-value.is-fast { color: var(--el-color-success); }
.dm-value.is-normal { color: var(--el-text-color-regular); }
.dm-value.is-slow { color: var(--el-color-warning); }
.dm-value.is-very-slow { color: var(--el-color-danger); }

.members-list {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px dashed var(--el-border-color-lighter);
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.members-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  margin-bottom: 2px;
}

.member-row {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) 80px 40px auto;
  align-items: center;
  gap: 10px;
  font-size: 12px;
}

.member-bar-track {
  height: 6px;
  border-radius: 3px;
  background: var(--el-fill-color);
  overflow: hidden;
}

.member-bar-fill {
  height: 100%;
  border-radius: 3px;
  background: var(--el-color-primary-light-3);
}

.member-traffic {
  color: var(--el-text-color-secondary);
  text-align: right;
}

.member-metrics {
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

/* 无流量 */
.zero-traffic {
  margin-top: 16px;
  border: 1px dashed var(--el-border-color);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
}

.zero-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  cursor: pointer;
  user-select: none;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}

.zero-header .chevron.rotated {
  transform: rotate(180deg);
}

.zero-hint {
  font-size: 11px;
  font-weight: 400;
  color: var(--el-text-color-secondary);
  margin-left: 4px;
}

.zero-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 4px 14px 14px;
}

.zero-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 16px;
  font-size: 12px;
}

.zero-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-text-color-placeholder);
}

.zero-name {
  color: var(--el-text-color-primary);
  font-weight: 500;
}

/* 动画 */
.slide-enter-active,
.slide-leave-active {
  transition: all 0.22s ease;
  overflow: hidden;
}

.slide-enter-from,
.slide-leave-to {
  opacity: 0;
  max-height: 0;
}

.slide-enter-to,
.slide-leave-from {
  max-height: 800px;
}

@media (max-width: 1024px) {
  .search-input { width: 130px; }
  .perf-row-main {
    grid-template-columns: 72px minmax(120px, 1.4fr) 78px minmax(110px, 1fr) 24px;
    gap: 10px;
  }
}

@media (max-width: 768px) {
  .card-header {
    flex-direction: column;
    align-items: stretch;
  }

  .header-actions {
    justify-content: space-between;
  }

  .search-input { flex: 1; width: auto; }

  .scroll-body { max-height: calc(100vh - 240px); }

  .perf-row-main {
    grid-template-columns: 1fr auto;
    grid-template-areas:
      "status rate"
      "name name"
      "ttft ttft";
    gap: 8px;
  }

  .status-cell { grid-area: status; }
  .rate-cell { grid-area: rate; }
  .name-cell { grid-area: name; }
  .ttft-cell { grid-area: ttft; }
  .expand-cell { display: none; }

  .member-row {
    grid-template-columns: 1fr auto;
    grid-template-areas:
      "tag traffic"
      "bar bar"
      "metrics metrics";
    gap: 4px 8px;
  }
}

@media (max-width: 480px) {
  .card-header { padding: 10px 12px; }
  .card-body { padding: 12px; }
  .info-strip { padding: 8px 12px; }
  .overview-bar { padding: 10px 12px; }
  .overview-item { min-width: 60px; padding: 2px 10px; }
}
</style>
