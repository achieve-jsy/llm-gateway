<template>
  <div class="group-health-monitor-page">
    <div class="monitor-card-outer">
      <!-- Header: view toggle + controls -->
      <div class="card-header">
        <div class="header-left">
          <el-radio-group v-model="view" size="small" @change="onViewChange">
            <el-radio-button value="group">模型组视角</el-radio-button>
            <el-radio-button value="model">模型视角</el-radio-button>
          </el-radio-group>
        </div>
        <div class="header-actions">
          <el-input v-model="searchText" placeholder="搜索名称" size="small" clearable class="search-input" @keyup.enter="onSearch" @clear="onSearch" />
          <el-select v-model="sortBy" size="small" class="sort-select" @change="onSortChange">
            <el-option v-if="view === 'group'" label="按健康度" value="health" />
            <el-option v-if="view === 'group'" label="按成功率" value="success_rate" />
            <el-option v-if="view === 'model'" label="按稳定性" value="stability" />
            <el-option label="按首Token" value="ttft" />
            <el-option v-if="view === 'model'" label="按延迟" value="latency" />
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
          <template v-if="view === 'group'">
            窗口内无人调用的模型组没有性能数据，会单独列在下方「无流量」区，并非异常。
          </template>
          点击行可展开查看详细指标。
        </span>
      </div>

      <!-- Overview bar -->
      <div v-if="!loadingMonitors && displayItems.length > 0" class="overview-bar">
        <div class="overview-item">
          <span class="overview-num">{{ displayItems.length }}</span>
          <span class="overview-label">有流量{{ view === 'group' ? '组' : '模型' }}</span>
        </div>
        <div class="overview-item is-excellent">
          <span class="overview-num">{{ statusCounts.excellent }}</span>
          <span class="overview-label">{{ view === 'group' ? '健康' : '稳定' }}</span>
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
        <div v-if="view === 'group'" class="overview-item">
          <span class="overview-num" style="color: var(--el-text-color-secondary)">{{ zeroTrafficGroups.length }}</span>
          <span class="overview-label">无流量</span>
        </div>
      </div>

      <!-- Main list -->
      <div ref="scrollBodyRef" class="card-body scroll-body" @scroll="onScroll">
        <div v-if="loadingMonitors && displayItems.length === 0" class="state-loading">
          <el-icon class="is-loading" size="20"><Loading /></el-icon>
          <span>加载中...</span>
        </div>

        <div v-else-if="displayItems.length === 0 && (view === 'model' || zeroTrafficGroups.length === 0)" class="state-empty">
          <el-icon class="empty-icon" :size="40"><DataLine /></el-icon>
          <p class="empty-title">暂无{{ view === 'group' ? '模型组' : '模型' }}性能数据</p>
          <p class="sub">
            <template v-if="view === 'group'">
              模型组下的模型尚未产生检测记录
            </template>
            <template v-else>
              所选时间窗口内没有模型调用记录
            </template>
          </p>
        </div>

        <div v-else class="monitor-list">
          <!-- Group view: show groups with timeline -->
          <template v-if="view === 'group'">
            <div
              v-for="group in displayItems"
              :key="group.name"
              class="monitor-item"
              :class="[getItemStatusClass(group), { expanded: expandedGroups.has(group.name) }]"
            >
              <!-- Main row (clickable to expand) -->
              <div class="item-row" @click="toggleExpand(group.name)">
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
                    <el-tooltip :content="group.name" :show-after="300">
                      <el-tag class="format-badge" type="info" size="small" effect="plain">
                        <span class="mono">{{ group.name }}</span>
                      </el-tag>
                    </el-tooltip>
                    <el-tag
                      v-if="group.health_percent != null"
                      :type="rateType(group.health_percent)"
                      size="small"
                      effect="dark"
                    >
                      {{ group.health_percent }}%
                    </el-tag>
                  </div>
                  <div class="meta-line">
                    <el-tag
                      v-if="group.strategy"
                      :type="strategyTagType(group.strategy)"
                      size="small"
                      effect="plain"
                      class="strategy-tag"
                    >{{ strategyLabel(group.strategy) }}</el-tag>
                    <span v-if="group.model_count" class="model-count">{{ group.model_count }} 个模型</span>
                    <span v-if="group.req_count" class="req-count">{{ group.req_count }} 请求</span>
                  </div>
                </div>

                <!-- Timeline (only for group view with timeline data) -->
                <div class="item-timeline" v-if="group.timeline">
                  <HealthTimeline :timeline="group.timeline" :lookback="lookback" />
                </div>

                <!-- Performance metrics -->
                <div class="item-perf" v-if="group.perf">
                  <div class="perf-rate">
                    <span class="rate-value" :class="rateClass(group.perf.success_rate)">
                      {{ (group.perf.success_rate * 100).toFixed(1) }}%
                    </span>
                    <span class="perf-label">成功率</span>
                  </div>
                  <div class="perf-ttft">
                    <span class="ttft-value">{{ group.perf.p50_ms.toFixed(0) }}ms</span>
                    <span class="perf-label">TTFT</span>
                  </div>
                </div>
                <div class="item-perf no-data" v-else>
                  <span class="no-data-text">无流量</span>
                </div>

                <!-- Expand arrow -->
                <div class="expand-cell">
                  <el-icon :size="16" class="chevron" :class="{ rotated: expandedGroups.has(group.name) }">
                    <ArrowDown />
                  </el-icon>
                </div>
              </div>

              <!-- Expanded detail -->
              <transition name="slide">
                <div v-if="expandedGroups.has(group.name) && group.perf" class="perf-detail">
                  <div class="detail-metrics">
                    <div class="detail-metric">
                      <span class="dm-label">总延迟 P50 / P95</span>
                      <span class="dm-value">{{ formatMs(group.perf.p50_ms) }} / {{ formatMs(group.perf.p95_ms) }}</span>
                    </div>
                    <div class="detail-metric">
                      <span class="dm-label">首 Token P50 / P95</span>
                      <span class="dm-value">{{ formatMs(group.perf.ttft_p50_ms) }} / {{ formatMs(group.perf.ttft_p95_ms) }}</span>
                    </div>
                    <div v-if="group.perf.pct_in_24h_p50 != null" class="detail-metric">
                      <span class="dm-label">24h 延迟水平</span>
                      <span class="dm-value" :class="pctClass(group.perf.pct_in_24h_p50)">
                        P{{ group.perf.pct_in_24h_p50 }}（{{ percentileLabel(group.perf.pct_in_24h_p50) }}）
                      </span>
                    </div>
                    <div v-if="group.perf.failover_count > 0" class="detail-metric">
                      <span class="dm-label">Failover 次数</span>
                      <span class="dm-value is-warn">{{ group.perf.failover_count }}</span>
                    </div>
                    <div v-if="group.perf.err_429 > 0" class="detail-metric">
                      <span class="dm-label">限流 (429)</span>
                      <span class="dm-value is-warn">{{ group.perf.err_429 }}</span>
                    </div>
                    <div v-if="group.perf.err_5xx > 0" class="detail-metric">
                      <span class="dm-label">服务端错误 (5xx)</span>
                      <span class="dm-value is-danger">{{ group.perf.err_5xx }}</span>
                    </div>
                  </div>

                  <div v-if="group.perf.members && group.perf.members.length > 0" class="members-list">
                    <div class="members-title">组内模型流量</div>
                    <div v-for="member in group.perf.members" :key="member.model_name + member.provider" class="member-row">
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
          </template>

          <!-- Model view: show individual models -->
          <template v-else>
            <div
              v-for="item in displayItems"
              :key="item.name"
              class="monitor-item"
              :class="[getItemStatusClass(item), { expanded: expandedGroups.has(item.name) }]"
            >
              <div class="item-row" @click="toggleExpand(item.name)">
                <!-- Status indicator -->
                <div class="status-cell">
                  <span class="status-dot" :class="'dot-' + computeStatus(item).key"></span>
                  <span class="status-text" :class="'text-' + computeStatus(item).key">
                    {{ computeStatus(item).label }}
                  </span>
                </div>

                <!-- Model info -->
                <div class="item-info">
                  <div class="info-line">
                    <el-tooltip :content="item.name" :show-after="300">
                      <span class="row-name">{{ item.name }}</span>
                    </el-tooltip>
                  </div>
                  <div class="meta-line">
                    <span v-if="item.used_by_groups" class="meta-text">{{ item.used_by_groups }} 组在用</span>
                    <span class="meta-text">{{ item.req_count }} 请求</span>
                  </div>
                </div>

                <!-- Rate cell -->
                <div class="rate-cell">
                  <span class="rate-value" :class="'text-' + computeStatus(item).key">
                    {{ formatPct(item.success_rate) }}
                  </span>
                  <span class="rate-label">成功率</span>
                </div>

                <!-- TTFT bar -->
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

                <!-- Expand arrow -->
                <div class="expand-cell">
                  <el-icon :size="16" class="chevron" :class="{ rotated: expandedGroups.has(item.name) }">
                    <ArrowDown />
                  </el-icon>
                </div>
              </div>

              <!-- Expanded detail for model view -->
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
                </div>
              </transition>
            </div>
          </template>

          <div v-if="loadingMore" class="state-loading" style="padding:12px 0">
            <el-icon class="is-loading" size="16"><Loading /></el-icon>
            <span style="font-size:12px">加载更多...</span>
          </div>

          <!-- Zero traffic groups (only in group view) -->
          <div v-if="view === 'group' && zeroTrafficGroups.length > 0" class="zero-traffic">
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

          <div v-if="displayItems.length > 0" class="state-end">
            共 {{ displayItems.length }} 个有流量的{{ view === 'group' ? '模型组' : '模型' }}
            <template v-if="view === 'group' && zeroTrafficGroups.length > 0">
              ，{{ zeroTrafficGroups.length }} 个无流量
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { Refresh, Loading, ArrowDown, DataLine } from '@element-plus/icons-vue'
import { getGroupHealthTimeline, getPerformance } from '@/services/control-plane'
import type { GroupTimelineGroup, PerformanceItem } from '@/services/control-plane'
import HealthTimeline from '@/components/admin/HealthTimeline.vue'
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement, GroupListItem } from '@/types/group'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const PAGE_SIZE = 50
const loading = ref(false)
const loadingMonitors = ref(false)
const loadingMore = ref(false)

// View mode
const view = ref<'group' | 'model'>('group')

// Data stores
const timelineGroups = shallowRef<GroupTimelineGroup[]>([])
const perfItems = shallowRef<PerformanceItem[]>([])
const total = ref(0)
const rawLoaded = ref(0)
const rawTotal = ref(0)

const lookback = ref('6h')
const searchText = ref('')
const sortBy = ref<string>('health')

let timer: ReturnType<typeof setInterval> | null = null

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

// Scroll
const scrollBodyRef = ref<HTMLElement | null>(null)

// Lookback label
const lookbackLabel = computed(() => {
  const map: Record<string, string> = { '1h': '1小时', '6h': '6小时', '12h': '12小时', '24h': '24小时', '7d': '7天' }
  return map[lookback.value] || lookback.value
})

// Unified display items
interface DisplayItem {
  name: string
  strategy?: string
  model_count?: number
  health_percent?: number
  req_count?: number
  timeline?: any
  perf?: PerformanceItem
  // Model view specific
  used_by_groups?: string
  success_rate: number
  p50_ms: number
  p95_ms: number
  ttft_p50_ms: number
  ttft_p95_ms: number
  pct_in_24h_p50?: number
  failover_count: number
  err_429: number
  err_5xx: number
  members?: any[]
}

const displayItems = computed((): DisplayItem[] => {
  if (view.value === 'group') {
    // Convert timeline groups to display items with perf data merged
    const perfMap = new Map<string, PerformanceItem>()
    for (const item of perfItems.value) {
      perfMap.set(item.name, item)
    }
    
    return timelineGroups.value
      .filter(g => perfMap.has(g.group_name))
      .map(g => {
        const perf = perfMap.get(g.group_name)!
        return {
          name: g.group_name,
          strategy: g.strategy,
          model_count: g.model_count,
          health_percent: g.health_percent,
          req_count: perf.req_count,
          timeline: g.timeline,
          perf,
          success_rate: perf.success_rate,
          p50_ms: perf.p50_ms,
          p95_ms: perf.p95_ms,
          ttft_p50_ms: perf.ttft_p50_ms,
          ttft_p95_ms: perf.ttft_p95_ms,
          pct_in_24h_p50: perf.pct_in_24h_p50,
          failover_count: perf.failover_count,
          err_429: perf.err_429,
          err_5xx: perf.err_5xx,
          members: perf.members
        }
      })
  } else {
    // Model view: directly use perf items
    return perfItems.value.map(item => ({
      name: item.name,
      req_count: item.req_count,
      used_by_groups: item.used_by_groups,
      success_rate: item.success_rate,
      p50_ms: item.p50_ms,
      p95_ms: item.p95_ms,
      ttft_p50_ms: item.ttft_p50_ms,
      ttft_p95_ms: item.ttft_p95_ms,
      pct_in_24h_p50: item.pct_in_24h_p50,
      failover_count: item.failover_count,
      err_429: item.err_429,
      err_5xx: item.err_5xx,
      members: item.members
    }))
  }
})

// Status computation
interface StatusInfo {
  key: 'excellent' | 'good' | 'fair' | 'poor'
  label: string
}

function computeStatus(item: DisplayItem): StatusInfo {
  const sr = item.success_rate
  const pct = item.pct_in_24h_p50
  
  if (sr < 0.85) return { key: 'poor', label: '异常' }
  if (sr < 0.95) return { key: 'fair', label: '波动' }
  
  if (view.value === 'group') {
    if (sr >= 0.99) return { key: 'excellent', label: '健康' }
    return { key: 'good', label: '良好' }
  } else {
    if (sr >= 0.99 && (pct == null || pct <= 70)) return { key: 'excellent', label: '稳定' }
    if (pct != null && pct > 85) return { key: 'fair', label: '偏慢' }
    return { key: 'good', label: '良好' }
  }
}

function getItemStatusClass(item: DisplayItem): string {
  if (!item.perf && view.value === 'group') return 'no-traffic'
  return 'status-' + computeStatus(item).key
}

const statusCounts = computed(() => {
  const c = { excellent: 0, good: 0, fair: 0, poor: 0 }
  for (const item of displayItems.value) {
    const status = computeStatus(item)
    if (status.key !== 'excellent' || (view.value === 'model' || item.perf)) {
      c[status.key]++
    }
  }
  return c
})

// Sorted display items
const sortedDisplayItems = computed(() => {
  const arr = [...displayItems.value]
  
  switch (sortBy.value) {
    case 'health':
      arr.sort((a, b) => (b.health_percent || 0) - (a.health_percent || 0))
      break
    case 'success_rate':
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
    case 'req_count':
      arr.sort((a, b) => (b.req_count || 0) - (a.req_count || 0))
      break
  }
  return arr
})

const statusRank: Record<StatusInfo['key'], number> = { excellent: 0, good: 1, fair: 2, poor: 3 }

// Use sorted items for display
const finalDisplayItems = computed(() => sortedDisplayItems.value)

// Zero traffic groups (only in group view)
const zeroTrafficGroups = computed(() => {
  if (view.value !== 'group') return []
  const trafficNames = new Set(timelineGroups.value.map(g => g.group_name))
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

// Data loading
async function loadTimelineData(reset: boolean = true) {
  if (reset) {
    loadingMonitors.value = true
  } else {
    loadingMore.value = true
  }
  try {
    const offset = reset ? 0 : timelineGroups.value.length
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
      timelineGroups.value = filteredGroups
    } else {
      timelineGroups.value = [...timelineGroups.value, ...filteredGroups]
    }
    total.value = result.total
  } catch {
    if (reset) timelineGroups.value = []
  } finally {
    loadingMonitors.value = false
    loadingMore.value = false
  }
}

async function loadPerformanceData(reset: boolean = true) {
  if (reset) {
    rawLoaded.value = 0
    rawTotal.value = 0
  }
  
  try {
    const viewType = view.value
    const result = await getPerformance(lookback.value, viewType, searchText.value, PAGE_SIZE, rawLoaded.value)
    
    let filteredItems = result.items || []
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null && viewType === 'group') {
      filteredItems = filteredItems.filter(item => isGroupAvailable(item.name))
    }
    
    if (reset) {
      perfItems.value = filteredItems
    } else {
      perfItems.value = [...perfItems.value, ...filteredItems]
    }
    
    rawTotal.value = result.total ?? result.items.length
    rawLoaded.value += result.items.length
    total.value = rawTotal.value
  } catch {
    if (reset) {
      perfItems.value = []
      rawTotal.value = 0
      rawLoaded.value = 0
      total.value = 0
    }
  }
}

async function loadEntitlements() {
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
    if (view.value === 'group') {
      await Promise.all([
        loadTimelineData(true),
        loadPerformanceData(true),
        loadGroupsForZeroTraffic()
      ])
    } else {
      await loadPerformanceData(true)
    }
  } finally {
    loading.value = false
  }
}

function onViewChange() {
  // Reset sort when changing view
  if (view.value === 'group') {
    sortBy.value = 'health'
  } else {
    sortBy.value = 'stability'
  }
  refreshData()
}

function onLookbackChange() {
  refreshData()
}

function onSearch() {
  refreshData()
}

function onSortChange() {
  // Sorting is computed, no need to reload
}

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (loadingMore.value) return
  if (rawLoaded.value >= rawTotal.value) return
  const { scrollTop, scrollHeight, clientHeight } = el
  if (scrollHeight - scrollTop - clientHeight < 100) {
    loadMore()
  }
}

async function loadMore() {
  if (loadingMore.value || rawLoaded.value >= rawTotal.value) return
  
  if (view.value === 'group') {
    await loadTimelineData(false)
    await loadPerformanceData(false)
  } else {
    await loadPerformanceData(false)
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

function ttftBarWidth(ttft: number): string {
  const maxMs = 5000
  const clamped = Math.min(ttft, maxMs)
  const pct = Math.max(4, Math.round(((maxMs - clamped) / maxMs) * 100))
  return pct + '%'
}

function speedLevel(ttft: number): 'fast' | 'mid' | 'slow' {
  if (ttft <= 1200) return 'fast'
  if (ttft <= 3500) return 'mid'
  return 'slow'
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
    refreshData()
  }, 60000)
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
})
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

/* Page root: fill viewport */
.group-health-monitor-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.monitor-card-outer {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}

/* Header: view toggle + controls */
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 14px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  flex-wrap: wrap;
  gap: 8px;
  flex-shrink: 0;
}

.header-left { flex-shrink: 0; }

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.lookback-label { font-size: 12px; color: var(--el-text-color-secondary); }
.lookback-select { width: 90px; }
.sort-select { width: 110px; }
.search-input { width: 150px; }

/* Info strip */
.info-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 14px;
  background: var(--bg-subtle, #F1F5F9);
  border-bottom: 1px solid var(--border-light, #F1F5F9);
  font-size: 12px;
  color: var(--text-secondary, #64748B);
  line-height: 1.5;
  flex-shrink: 0;
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
  padding: 5px 12px;
  border-bottom: 1px solid var(--border-light, #F1F5F9);
  flex-wrap: wrap;
  flex-shrink: 0;
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
.card-body { padding: 8px 14px; }
.scroll-body {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
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
.monitor-list { display: flex; flex-direction: column; gap: 4px; }

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
  gap: 12px;
  padding: 8px 12px;
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

.row-name {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #0F172A);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.meta-line {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 3px;
}
.meta-text {
  font-size: 11px;
  color: var(--text-secondary, #64748B);
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

/* Rate cell (model view) */
.rate-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  text-align: right;
  flex-shrink: 0;
  width: 80px;
}
.rate-cell .rate-value {
  font-size: 16px;
  font-weight: 700;
  line-height: 1.2;
}
.rate-label {
  font-size: 10px;
  color: var(--text-secondary, #64748B);
  margin-top: 1px;
  font-weight: 500;
}

/* TTFT cell (model view) */
.ttft-cell {
  flex-shrink: 0;
  width: 120px;
}
.ttft-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 3px;
}
.ttft-label {
  font-size: 10px;
  color: var(--text-secondary, #64748B);
  font-weight: 500;
}
.ttft-track {
  height: 4px;
  border-radius: 2px;
  background: var(--bg-subtle, #F1F5F9);
  overflow: hidden;
}
.ttft-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.5s cubic-bezier(0.4, 0, 0.2, 1);
}
.fill-fast { background: var(--perf-excellent); }
.fill-mid { background: var(--perf-fair); }
.fill-slow { background: var(--perf-poor); }

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
  padding: 10px 14px;
  border-top: 1px solid var(--el-border-color-lighter);
  background: var(--bg-subtle, #FAFAFA);
}

.detail-metrics {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 8px 12px;
}

.detail-metric {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 5px 8px;
  background: var(--bg-surface, #fff);
  border-radius: 6px;
  border: 1px solid var(--border-light, #F1F5F9);
}

.dm-label {
  font-size: 10px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.dm-value {
  font-size: 13px;
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
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.members-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}

.member-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 0;
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
  margin-top: 8px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  overflow: hidden;
}

.zero-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
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
  gap: 6px;
  padding: 8px 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.zero-chip {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  background: var(--el-fill-color);
  border-radius: 4px;
  font-size: 11px;
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
  .ttft-cell { width: 100px; }
}

@media (max-width: 768px) {
  .card-header { flex-direction: column; align-items: stretch; }
  .header-actions { justify-content: space-between; flex-wrap: wrap; }
  .search-input { flex: 1; width: 100%; }
  
  .card-body { padding: 8px 12px; }
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
  .ttft-cell { width: 100%; order: 4; }
  
  .state-loading, .state-empty { padding: 24px 0; }
}

@media (max-width: 480px) {
  .header-actions { flex-direction: column; align-items: stretch; gap: 6px; }
  .lookback-label { display: none; }
  .lookback-select { width: 100%; }
  .sort-select { width: 100%; }
  .search-input { width: 100%; }
  
  .card-header { padding: 8px 12px; }
  .card-body { padding: 8px; }
  .monitor-item { border-radius: 6px; }
  .monitor-list { gap: 5px; }
  
  .detail-metrics { grid-template-columns: 1fr; }
}
</style>
