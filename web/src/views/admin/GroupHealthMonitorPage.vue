<template>
  <div class="group-health-monitor-page">
    <div class="monitor-card-outer">
      <!-- Header: title + controls -->
      <div class="card-header">
        <h3 class="card-title">模型组监控</h3>
        <div class="header-actions">
          <el-input v-model="searchText" placeholder="搜索模型组名称" size="small" clearable class="search-input" @keyup.enter="onSearch" @clear="onSearch" />
          <el-select v-model="sortBy" size="small" class="sort-select" @change="onSortChange">
            <el-option label="按健康度" value="health" />
            <el-option label="按成功率" value="success_rate" />
            <el-option label="按首Token" value="ttft" />
            <el-option label="按请求量" value="req_count" />
          </el-select>
          <span class="lookback-label">回溯：</span>
          <el-select v-model="lookback" class="lookback-select" size="small" @change="onLookbackChange">
            <el-option label="1 小时" value="1h" />
            <el-option label="6 小时" value="6h" />
            <el-option label="12 小时" value="12h" />
            <el-option label="24 小时" value="24h" />
            <el-option label="7 天" value="7d" />
          </el-select>
          <el-button :loading="loading" :icon="Refresh" size="small" circle @click="refreshData" />
        </div>
      </div>

      <!-- Info strip -->
      <div class="info-strip">
        <span class="info-icon-circle">i</span>
        <span>
          性能指标基于<strong>所选时间窗口（{{ lookbackLabel }}）</strong>内的真实请求统计。
          窗口内无人调用的模型组没有性能数据，会单独列在下方「无流量」区，并非异常。
          点击行可展开查看详细指标。
        </span>
      </div>

      <!-- Overview bar -->
      <div v-if="!loadingMonitors && sortedGroups.length > 0" class="overview-bar">
        <div class="overview-item">
          <span class="overview-num">{{ sortedGroups.length }}</span>
          <span class="overview-label">有流量</span>
        </div>
        <div class="overview-item is-excellent">
          <span class="overview-num">{{ statusCounts.excellent }}</span>
          <span class="overview-label">健康</span>
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
        <div class="overview-item overview-spacer"></div>
        <div class="overview-item">
          <span class="overview-num" style="color: var(--el-text-color-secondary)">{{ zeroTrafficGroups.length }}</span>
          <span class="overview-label">无流量</span>
        </div>
      </div>

      <!-- Main list -->
      <div ref="scrollBodyRef" class="card-body scroll-body" @scroll="onScroll">
        <div v-if="loadingMonitors" class="state-loading">
          <el-icon class="is-loading" size="20"><Loading /></el-icon>
          <span>加载中...</span>
        </div>

        <div v-else-if="sortedGroups.length === 0 && zeroTrafficGroups.length === 0" class="state-empty">
          <el-icon class="empty-icon" :size="40"><DataLine /></el-icon>
          <p class="empty-title">暂无模型组健康监控数据</p>
          <p class="sub">模型组下的模型尚未产生检测记录</p>
        </div>

        <div v-else class="monitor-list">
          <div
            v-for="group in sortedGroups"
            :key="group.group_name"
            class="monitor-item"
            :class="[statusClass(group), { expanded: expandedGroups.has(group.group_name) }]"
          >
            <!-- Main row (clickable to expand) -->
            <div class="item-row" @click="toggleExpand(group.group_name)">
              <!-- Status indicator -->
              <div class="status-cell">
                <span class="status-dot" :class="'dot-' + computeStatus(group).key"></span>
                <span class="status-text" :class="'text-' + computeStatus(group).key">
                  {{ computeStatus(group).label }}
                </span>
              </div>

              <!-- Group info -->
              <div class="item-info">
                <div class="info-line">
                  <el-tooltip :content="group.group_name" :show-after="300">
                    <el-tag class="format-badge" type="info" size="small" effect="plain">
                      <span class="mono">{{ group.group_name }}</span>
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
                <div class="meta-line">
                  <el-tag
                    :type="strategyTagType(group.strategy)"
                    size="small"
                    effect="plain"
                    class="strategy-tag"
                  >{{ strategyLabel(group.strategy) }}</el-tag>
                  <span class="model-count">{{ group.model_count }} 个模型</span>
                  <span v-if="perfMap.get(group.group_name)" class="req-count">{{ perfMap.get(group.group_name)!.req_count }} 请求</span>
                </div>
              </div>

              <!-- Timeline -->
              <div class="item-timeline">
                <HealthTimeline :timeline="group.timeline" :lookback="lookback" />
              </div>

              <!-- Performance metrics -->
              <div class="item-perf" v-if="perfMap.get(group.group_name)">
                <div class="perf-rate">
                  <span class="rate-value" :class="rateClass(perfMap.get(group.group_name)!.success_rate)">
                    {{ (perfMap.get(group.group_name)!.success_rate * 100).toFixed(1) }}%
                  </span>
                  <span class="perf-label">成功率</span>
                </div>
                <div class="perf-ttft">
                  <span class="ttft-value">{{ perfMap.get(group.group_name)!.p50_ms.toFixed(0) }}ms</span>
                  <span class="perf-label">TTFT</span>
                </div>
              </div>
              <div class="item-perf no-data" v-else>
                <span class="no-data-text">无流量</span>
              </div>

              <!-- Expand arrow -->
              <div class="expand-cell">
                <el-icon :size="16" class="chevron" :class="{ rotated: expandedGroups.has(group.group_name) }">
                  <ArrowDown />
                </el-icon>
              </div>
            </div>

            <!-- Expanded detail -->
            <transition name="slide">
              <div v-if="expandedGroups.has(group.group_name) && perfMap.get(group.group_name)" class="perf-detail">
                <div class="detail-metrics">
                  <div class="detail-metric">
                    <span class="dm-label">总延迟 P50 / P95</span>
                    <span class="dm-value">{{ formatMs(perfMap.get(group.group_name)!.p50_ms) }} / {{ formatMs(perfMap.get(group.group_name)!.p95_ms) }}</span>
                  </div>
                  <div class="detail-metric">
                    <span class="dm-label">首 Token P50 / P95</span>
                    <span class="dm-value">{{ formatMs(perfMap.get(group.group_name)!.ttft_p50_ms) }} / {{ formatMs(perfMap.get(group.group_name)!.ttft_p95_ms) }}</span>
                  </div>
                  <div v-if="perfMap.get(group.group_name)!.pct_in_24h_p50 != null" class="detail-metric">
                    <span class="dm-label">24h 延迟水平</span>
                    <span class="dm-value" :class="pctClass(perfMap.get(group.group_name)!.pct_in_24h_p50)">
                      P{{ perfMap.get(group.group_name)!.pct_in_24h_p50 }}（{{ percentileLabel(perfMap.get(group.group_name)!.pct_in_24h_p50) }}）
                    </span>
                  </div>
                  <div v-if="perfMap.get(group.group_name)!.failover_count > 0" class="detail-metric">
                    <span class="dm-label">Failover 次数</span>
                    <span class="dm-value is-warn">{{ perfMap.get(group.group_name)!.failover_count }}</span>
                  </div>
                  <div v-if="perfMap.get(group.group_name)!.err_429 > 0" class="detail-metric">
                    <span class="dm-label">限流 (429)</span>
                    <span class="dm-value is-warn">{{ perfMap.get(group.group_name)!.err_429 }}</span>
                  </div>
                  <div v-if="perfMap.get(group.group_name)!.err_5xx > 0" class="detail-metric">
                    <span class="dm-label">服务端错误 (5xx)</span>
                    <span class="dm-value is-danger">{{ perfMap.get(group.group_name)!.err_5xx }}</span>
                  </div>
                </div>

                <div v-if="perfMap.get(group.group_name)!.members && perfMap.get(group.group_name)!.members.length > 0" class="members-list">
                  <div class="members-title">组内模型流量</div>
                  <div v-for="member in perfMap.get(group.group_name)!.members" :key="member.model_name + member.provider" class="member-row">
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

          <div v-if="loadingMore" class="state-loading" style="padding:12px 0">
            <el-icon class="is-loading" size="16"><Loading /></el-icon>
            <span style="font-size:12px">加载更多...</span>
          </div>

          <!-- Zero traffic groups -->
          <div v-if="zeroTrafficGroups.length > 0" class="zero-traffic">
            <div class="zero-header" @click="zeroExpanded = !zeroExpanded">
              <el-icon :size="14" class="chevron" :class="{ rotated: zeroExpanded }"><ArrowDown /></el-icon>
              <span>无流量模型组</span>
              <el-tag size="small" type="info" effect="plain" round>{{ zeroTrafficGroups.length }}</el-tag>
              <span class="zero-hint">所选 {{ lookbackLabel }} 窗口内无请求，因此没有性能指标</span>
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

          <div v-if="sortedGroups.length > 0" class="state-end">
            共 {{ sortedGroups.length }} 个有流量的模型组
            <template v-if="zeroTrafficGroups.length > 0">
              ，{{ zeroTrafficGroups.length }} 个无流量
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { Refresh, Loading, ArrowDown, DataLine } from '@element-plus/icons-vue'
import { getGroupHealthTimeline, getPerformance } from '@/services/control-plane'
import type { GroupTimelineGroup, PerformanceItem } from '@/services/control-plane'
import HealthTimeline from '@/components/admin/HealthTimeline.vue'
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement, GroupListItem } from '@/types/group'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const PAGE_SIZE = 20
const loading = ref(false)
const loadingMonitors = ref(false)
const loadingMore = ref(false)
const groups = shallowRef<GroupTimelineGroup[]>([])
const total = ref(0)
const lookback = ref('6h')
const searchText = ref('')
const sortBy = ref<'health' | 'success_rate' | 'ttft' | 'req_count'>('health')
let timer: ReturnType<typeof setInterval> | null = null

// Performance data cache
const perfMap = ref<Map<string, PerformanceItem>>(new Map())

// Expand state
const expandedGroups = ref<Set<string>>(new Set())
const zeroExpanded = ref(false)

// All groups (for zero-traffic detection + auth filtering)
const allGroups = ref<GroupListItem[]>([])

// Authorization
const entitlementsMap = ref<Map<string, GroupEntitlement[]>>(new Map())
const openModeMap = ref<Map<string, string>>(new Map())
const myDeptIds = ref<string[]>([])
const myStudioId = ref('')

// Lookback label
const lookbackLabel = computed(() => {
  const map: Record<string, string> = { '1h': '1小时', '6h': '6小时', '12h': '12小时', '24h': '24小时', '7d': '7天' }
  return map[lookback.value] || lookback.value
})

// Status computation
interface StatusInfo {
  key: 'excellent' | 'good' | 'fair' | 'poor'
  label: string
}

function computeStatus(group: GroupTimelineGroup): StatusInfo {
  const perf = perfMap.value.get(group.group_name)
  if (!perf) return { key: 'excellent', label: '无流量' }
  
  const sr = perf.success_rate
  if (sr < 0.85) return { key: 'poor', label: '异常' }
  if (sr < 0.95) return { key: 'fair', label: '波动' }
  if (sr >= 0.99) return { key: 'excellent', label: '健康' }
  return { key: 'good', label: '良好' }
}

function statusClass(group: GroupTimelineGroup): string {
  const perf = perfMap.value.get(group.group_name)
  if (!perf) return 'no-traffic'
  return 'status-' + computeStatus(group).key
}

const statusCounts = computed(() => {
  const c = { excellent: 0, good: 0, fair: 0, poor: 0 }
  for (const g of sortedGroups.value) {
    const status = computeStatus(g)
    if (status.key !== 'excellent' || perfMap.value.has(g.group_name)) {
      c[status.key]++
    }
  }
  return c
})

// Sorted groups
const sortedGroups = computed(() => {
  const arr = [...groups.value].filter(g => perfMap.value.has(g.group_name))
  const perf = perfMap.value
  
  switch (sortBy.value) {
    case 'health':
      arr.sort((a, b) => b.health_percent - a.health_percent)
      break
    case 'success_rate':
      arr.sort((a, b) => {
        const pa = perf.get(a.group_name)
        const pb = perf.get(b.group_name)
        return (pb?.success_rate || 0) - (pa?.success_rate || 0)
      })
      break
    case 'ttft':
      arr.sort((a, b) => {
        const pa = perf.get(a.group_name)
        const pb = perf.get(b.group_name)
        return (pa?.p50_ms || 0) - (pb?.p50_ms || 0)
      })
      break
    case 'req_count':
      arr.sort((a, b) => {
        const pa = perf.get(a.group_name)
        const pb = perf.get(b.group_name)
        return (pb?.req_count || 0) - (pa?.req_count || 0)
      })
      break
  }
  return arr
})

// Zero traffic groups
const zeroTrafficGroups = computed(() => {
  const trafficNames = new Set(groups.value.map(g => g.group_name))
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

// Check if group is available to current user
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

async function loadTimeline(reset: boolean = true) {
  if (reset) {
    loadingMonitors.value = true
  } else {
    loadingMore.value = true
  }
  try {
    const offset = reset ? 0 : groups.value.length
    const result = await getGroupHealthTimeline(lookback.value, searchText.value, offset, PAGE_SIZE)
    
    let filteredGroups = result.groups
    
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null) {
      if (reset) {
        await loadEntitlements()
      }
      filteredGroups = result.groups.filter(g => isGroupAvailable(g.group_name))
    }
    
    if (reset) {
      groups.value = filteredGroups
    } else {
      groups.value = [...groups.value, ...filteredGroups]
    }
    total.value = result.total
  } catch {
    if (reset) groups.value = []
  } finally {
    loadingMonitors.value = false
    loadingMore.value = false
  }
}

async function loadEntitlements() {
  const role = authStore.effectiveRole
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    const allGroupsList = res.data.data ?? []
    allGroups.value = allGroupsList

    const newOpenModeMap = new Map<string, string>()
    for (const g of allGroupsList) {
      newOpenModeMap.set(g.name, g.open_mode)
    }
    openModeMap.value = newOpenModeMap

    if (role === 'studio_admin' || role === 'dept_admin') {
      const deptRoles = authStore.orgRoles.filter(r => r.role === 'dept_admin' && r.scope_type === 'dept')
      myDeptIds.value = deptRoles.map(r => r.scope_id)
      const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin' && r.scope_type === 'studio')
      myStudioId.value = studioRole?.scope_id || ''

      const controlledGroups = allGroupsList.filter(g => g.open_mode === 'controlled' && g.enabled)
      const entitlementPromises = controlledGroups.map(async g => {
        try {
          const entRes = await groupsApi.listEntitlements(g.id)
          return { groupName: g.name, items: entRes.data.data?.items || [] }
        } catch {
          return { groupName: g.name, items: [] }
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
    openModeMap.value = new Map()
    entitlementsMap.value = new Map()
  }
}

async function loadPerformanceData() {
  try {
    const result = await getPerformance(lookback.value, 'group', '', 200, 0)
    const newMap = new Map<string, PerformanceItem>()
    for (const item of result.items) {
      newMap.set(item.name, item)
    }
    perfMap.value = newMap
  } catch {
    perfMap.value = new Map()
  }
}

async function loadGroupsForZeroTraffic() {
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    allGroups.value = res.data.data ?? []
  } catch {
    allGroups.value = []
  }
}

async function refreshData() {
  loading.value = true
  try { 
    await Promise.all([
      loadTimeline(true),
      loadPerformanceData(),
      loadGroupsForZeroTraffic()
    ])
  } finally { 
    loading.value = false 
  }
}

function onLookbackChange() {
  refreshData()
}

function onSearch() {
  loadTimeline(true)
}

function onSortChange() {
  // Sorting is computed, no need to reload
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

function toggleExpand(name: string) {
  const s = new Set(expandedGroups.value)
  if (s.has(name)) s.delete(name); else s.add(name)
  expandedGroups.value = s
}

function rateType(percent: number): 'success' | 'warning' | 'danger' {
  if (percent >= 95) return 'success'
  if (percent >= 80) return 'warning'
  return 'danger'
}

function rateClass(rate: number): string {
  if (rate >= 0.95) return 'rate-excellent'
  if (rate >= 0.80) return 'rate-good'
  if (rate >= 0.50) return 'rate-fair'
  return 'rate-poor'
}

function strategyLabel(strategy: string): string {
  switch (strategy) {
    case 'round_robin': return '轮询'
    case 'priority': return '优先级'
    case 'weighted': return '加权'
    case 'auto': return '自动'
    default: return strategy || '轮询'
  }
}

function strategyTagType(strategy: string): 'info' | 'success' | 'warning' | 'primary' {
  switch (strategy) {
    case 'priority': return 'success'
    case 'weighted': return 'warning'
    case 'auto': return 'primary'
    default: return 'info'
  }
}

function formatMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}

function formatPct(rate: number): string {
  return (rate * 100).toFixed(1) + '%'
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

onMounted(() => {
  const role = authStore.effectiveRole
  if (role !== 'admin' && role !== null) {
    loadEntitlements().then(() => {
      refreshData()
    })
  } else {
    refreshData()
  }
  timer = setInterval(() => {
    loadTimeline(true)
    loadPerformanceData()
  }, 60000)
})
onBeforeUnmount(() => { if (timer) { clearInterval(timer); timer = null } })
</script>

<style scoped>
.group-health-monitor-page {
  width: 100%;
  --perf-excellent: #059669;
  --perf-excellent-bg: #ECFDF5;
  --perf-excellent-light: #D1FAE5;
  --perf-good: #4F6BF6;
  --perf-good-bg: #EEF0FF;
  --perf-good-light: #DDE1FF;
  --perf-fair: #D97706;
  --perf-fair-bg: #FFFBEB;
  --perf-fair-light: #FEF3C7;
  --perf-poor: #DC2626;
  --perf-poor-bg: #FEF2F2;
  --perf-poor-light: #FECACA;
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
.lookback-select { width: 90px; }
.sort-select { width: 100px; }
.search-input { width: 160px; }

/* Info strip */
.info-strip {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 16px;
  background: var(--bg-subtle, #F1F5F9);
  border-bottom: 1px solid var(--border-light, #F1F5F9);
  font-size: 12px;
  color: var(--text-secondary, #64748B);
  line-height: 1.5;
}
.info-strip strong { color: var(--text-primary, #0F172A); font-weight: 600; }
.info-icon-circle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--border-default, #E2E8F0);
  color: var(--text-secondary, #64748B);
  font-size: 11px;
  font-weight: 700;
  font-style: italic;
  flex-shrink: 0;
  font-family: Georgia, 'Times New Roman', serif;
}

/* Overview bar */
.overview-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 8px 16px;
  border-bottom: 1px solid var(--border-light, #F1F5F9);
  flex-wrap: wrap;
}
.overview-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: 6px;
  transition: background 0.15s;
}
.overview-num {
  font-size: 18px;
  font-weight: 700;
  line-height: 1;
  color: var(--text-primary, #0F172A);
  font-variant-numeric: tabular-nums;
}
.overview-label {
  font-size: 12px;
  color: var(--text-secondary, #64748B);
  font-weight: 500;
}
.overview-item.is-excellent .overview-num { color: var(--perf-excellent); }
.overview-item.is-excellent { background: var(--perf-excellent-bg); }
.overview-item.is-good .overview-num { color: var(--perf-good); }
.overview-item.is-good { background: var(--perf-good-bg); }
.overview-item.is-fair .overview-num { color: var(--perf-fair); }
.overview-item.is-fair { background: var(--perf-fair-bg); }
.overview-item.is-poor .overview-num { color: var(--perf-poor); }
.overview-item.is-poor { background: var(--perf-poor-bg); }
.overview-spacer { flex: 1; }

/* Body */
.card-body { padding: 16px 20px; }
.scroll-body {
  max-height: calc(100vh - 240px);
  min-height: 200px;
  overflow-y: auto;
}
.scroll-body::-webkit-scrollbar { width: 5px; }
.scroll-body::-webkit-scrollbar-track { background: transparent; }
.scroll-body::-webkit-scrollbar-thumb { background: var(--el-border-color); border-radius: 3px; }

/* States */
.state-loading {
  display: flex; align-items: center; justify-content: center;
  gap: 8px; padding: 40px 0; color: var(--el-text-color-secondary);
}
.state-empty {
  display: flex; flex-direction: column; align-items: center;
  padding: 48px 20px; color: var(--el-text-color-secondary); text-align: center;
}
.empty-icon { color: var(--el-text-color-placeholder); margin-bottom: 12px; }
.empty-title { font-size: 15px; font-weight: 600; color: var(--el-text-color-primary); margin: 0; }
.state-empty .sub { font-size: 12px; margin-top: 6px; max-width: 420px; line-height: 1.6; }
.state-end { text-align: center; padding: 12px 0 4px; font-size: 12px; color: var(--el-text-color-secondary); }

/* Monitor items */
.monitor-list { display: flex; flex-direction: column; gap: 8px; }

.monitor-item {
  border: 1px solid var(--el-border-color-lighter);
  border-left: 4px solid var(--el-border-color-lighter);
  border-radius: 8px;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  background: var(--el-bg-color);
}

.monitor-item.status-excellent { border-left-color: var(--perf-excellent); }
.monitor-item.status-good { border-left-color: var(--perf-good); }
.monitor-item.status-fair { border-left-color: var(--perf-fair); }
.monitor-item.status-poor { border-left-color: var(--perf-poor); }

.monitor-item:hover {
  border-left-width: 5px;
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.07);
}

.monitor-item.expanded {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

.monitor-item.no-traffic {
  opacity: 0.6;
  border-left-color: var(--el-border-color-lighter);
}
.monitor-item.no-traffic:hover {
  opacity: 0.8;
  border-color: var(--el-border-color-lighter);
}

.item-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
  cursor: pointer;
  user-select: none;
}

/* Status cell */
.status-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  width: 60px;
}
.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dot-excellent { background: var(--perf-excellent); box-shadow: 0 0 0 3px var(--perf-excellent-light); }
.dot-good { background: var(--perf-good); box-shadow: 0 0 0 3px var(--perf-good-light); }
.dot-fair { background: var(--perf-fair); box-shadow: 0 0 0 3px var(--perf-fair-light); }
.dot-poor { background: var(--perf-poor); box-shadow: 0 0 0 3px var(--perf-poor-light); }
.status-text { font-size: 12px; font-weight: 600; }
.text-excellent { color: var(--perf-excellent); }
.text-good { color: var(--perf-good); }
.text-fair { color: var(--perf-fair); }
.text-poor { color: var(--perf-poor); }

/* Left: info */
.item-info { flex: 1; min-width: 0; }
.info-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.format-badge { cursor: default; max-width: 180px; overflow: hidden; min-width: 0; }
.format-badge :deep(.el-tag__content) { overflow: hidden; }
.mono { font-family: monospace; font-size: 11px; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.meta-line {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
}
.strategy-tag { flex-shrink: 0; }
.model-count, .req-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

/* Timeline */
.item-timeline { flex: 1; min-width: 120px; max-width: 200px; }

/* Performance metrics column */
.item-perf {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-shrink: 0;
  width: 140px;
}

.perf-rate, .perf-ttft {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
}

.rate-value {
  font-size: 14px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.rate-excellent { color: var(--perf-excellent, #059669); }
.rate-good { color: var(--perf-good, #4F6CF6); }
.rate-fair { color: var(--perf-fair, #D97706); }
.rate-poor { color: var(--perf-poor, #DC2626); }

.ttft-value {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}

.perf-label {
  font-size: 10px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.item-perf.no-data {
  justify-content: center;
}
.no-data-text {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

/* Expand cell */
.expand-cell {
  flex-shrink: 0;
  display: flex;
  align-items: center;
}
.chevron {
  transition: transform 0.2s;
  color: var(--el-text-color-secondary);
}
.chevron.rotated {
  transform: rotate(180deg);
}

/* Expanded detail */
.perf-detail {
  padding: 16px 20px;
  border-top: 1px solid var(--el-border-color-lighter);
  background: var(--bg-subtle, #FAFAFA);
}

.detail-metrics {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 12px 24px;
}

.detail-metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.dm-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.dm-value {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}

.dm-value.is-warn { color: var(--perf-fair); }
.dm-value.is-danger { color: var(--perf-poor); }
.dm-value.is-fast { color: var(--perf-excellent); }
.dm-value.is-normal { color: var(--perf-good); }
.dm-value.is-slow { color: var(--perf-fair); }
.dm-value.is-very-slow { color: var(--perf-poor); }

/* Members list */
.members-list {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.members-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  margin-bottom: 10px;
}

.member-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 0;
}

.member-bar-track {
  flex: 1;
  height: 6px;
  background: var(--el-fill-color);
  border-radius: 3px;
  overflow: hidden;
}

.member-bar-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--perf-excellent), var(--perf-good));
  border-radius: 3px;
  transition: width 0.3s;
}

.member-traffic {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  min-width: 36px;
  text-align: right;
}

.member-metrics {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

/* Zero traffic section */
.zero-traffic {
  margin-top: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  overflow: hidden;
}

.zero-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  background: var(--bg-subtle, #FAFAFA);
  cursor: pointer;
  font-size: 13px;
  color: var(--el-text-color-regular);
  transition: background 0.15s;
}

.zero-header:hover {
  background: var(--el-fill-color);
}

.zero-hint {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-left: auto;
}

.zero-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.zero-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  background: var(--el-fill-color);
  border-radius: 4px;
  font-size: 12px;
  color: var(--el-text-color-regular);
}

.zero-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-text-color-placeholder);
}

.zero-name {
  font-family: monospace;
  font-size: 11px;
}

/* Slide transition */
.slide-enter-active, .slide-leave-active {
  transition: all 0.2s ease;
}
.slide-enter-from, .slide-leave-to {
  opacity: 0;
  max-height: 0;
}
.slide-enter-to, .slide-leave-from {
  opacity: 1;
  max-height: 500px;
}

/* Responsive */
@media (max-width: 1024px) {
  .search-input { width: 130px; }
  .item-timeline { max-width: 150px; }
  .item-perf { width: 120px; gap: 12px; }
}

@media (max-width: 768px) {
  .card-header { flex-direction: column; align-items: stretch; }
  .header-actions { justify-content: space-between; flex-wrap: wrap; }
  .search-input { flex: 1; width: 100%; }
  .card-title { font-size: 14px; }
  
  .card-body { padding: 12px; }
  .scroll-body { max-height: calc(100vh - 200px); }
  
  .item-row { 
    flex-wrap: wrap;
    gap: 10px;
  }
  .item-info { min-width: 0; flex: 1; }
  .item-timeline { 
    width: 100%; 
    max-width: 100%;
    order: 3;
  }
  .item-perf { 
    width: auto;
    order: 2;
  }
  .expand-cell { order: 1; }
  .monitor-item { padding: 10px; }
  
  .state-loading, .state-empty { padding: 24px 0; }
}

@media (max-width: 480px) {
  .header-actions { flex-direction: column; align-items: stretch; gap: 6px; }
  .lookback-label { display: none; }
  .lookback-select { width: 100%; }
  .sort-select { width: 100%; }
  .search-input { width: 100%; }
  
  .card-header { padding: 10px 12px; }
  .card-body { padding: 10px 8px; }
  .monitor-item { border-radius: 6px; }
  .monitor-list { gap: 6px; }
  .card-title { font-size: 13px; }
  
  .detail-metrics { grid-template-columns: 1fr; }
}
</style>
