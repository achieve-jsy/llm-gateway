<template>
  <div class="group-health-monitor-page">
    <div class="monitor-card-outer">
      <!-- Header: title + controls -->
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

      <!-- Main list -->
      <div ref="scrollBodyRef" class="card-body scroll-body" @scroll="onScroll">
        <div v-if="loadingMonitors && displayItems.length === 0" class="state-loading">
          <el-icon class="is-loading" size="20"><Loading /></el-icon>
          <span>加载中...</span>
        </div>

        <div v-else-if="displayItems.length === 0 && (view === 'model' || zeroTrafficGroups.length === 0)" class="state-empty">
          <p>暂无{{ view === 'group' ? '模型组' : '模型' }}性能数据</p>
          <p class="sub">
            <template v-if="view === 'group'">模型组下的模型尚未产生检测记录</template>
            <template v-else>所选时间窗口内没有模型调用记录</template>
          </p>
        </div>

        <div v-else class="monitor-list">
          <!-- Group view -->
          <template v-if="view === 'group'">
            <div
              v-for="group in finalDisplayItems"
              :key="group.name"
              class="monitor-item"
              :class="[getItemStatusClass(group), { expanded: expandedGroups.has(group.name) }]"
              @click="toggleExpand(group.name)"
            >
              <div class="item-row">
                <!-- Left: info -->
                <div class="item-info">
                  <div class="info-line">
                    <el-tooltip :content="group.name" :show-after="300">
                      <el-tag class="format-badge" type="info" size="small" effect="plain">
                        <span class="mono">{{ group.name }}</span>
                      </el-tag>
                    </el-tooltip>
                    <span
                      v-if="group.health_percent != null"
                      class="health-pct"
                      :class="rateClass(group.health_percent / 100)"
                    >{{ group.health_percent }}%</span>
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
                    <template v-if="group.perf">
                      <span class="meta-sep">·</span>
                      <el-tooltip content="成功率" placement="top" :show-after="500">
                        <span class="meta-metric" :class="rateClass(group.perf.success_rate)">
                          {{ (group.perf.success_rate * 100).toFixed(1) }}%
                        </span>
                      </el-tooltip>
                      <span class="meta-sep">·</span>
                      <el-tooltip content="首 Token 响应时间 (TTFT) P50" placement="top" :show-after="500">
                        <span class="meta-metric">TTFT {{ formatMs(group.perf.ttft_p50_ms) }}</span>
                      </el-tooltip>
                      <span class="meta-sep">·</span>
                      <el-tooltip content="总延迟 P50（从请求到响应完成）" placement="top" :show-after="500">
                        <span class="meta-metric">延迟 {{ formatMs(group.perf.p50_ms) }}</span>
                      </el-tooltip>
                      <span v-if="group.perf.avg_tps > 0" class="meta-sep">·</span>
                      <el-tooltip v-if="group.perf.avg_tps > 0" content="平均每秒输出 Token 数 (Tokens/s)" placement="top" :show-after="500">
                        <span class="meta-metric tps-metric">{{ group.perf.avg_tps.toFixed(1) }} tok/s</span>
                      </el-tooltip>
                      <span class="meta-metric-dim">{{ group.req_count }} 请求</span>
                    </template>
                  </div>
                </div>

                <!-- Right: timeline (wide!) -->
                <div class="item-timeline" v-if="group.timeline">
                  <HealthTimeline :timeline="group.timeline" :lookback="lookback" />
                </div>
                <div class="item-timeline no-timeline" v-else>
                  <span class="no-data-text">无检测数据</span>
                </div>
              </div>

              <!-- Expanded detail -->
              <transition name="slide">
                <div v-if="expandedGroups.has(group.name) && group.perf" class="perf-detail">
                  <div class="detail-metrics">
                    <div class="detail-metric">
                      <span class="dm-label">成功率</span>
                      <span class="dm-value" :class="rateClass(group.perf.success_rate)">{{ (group.perf.success_rate * 100).toFixed(1) }}%</span>
                    </div>
                    <div class="detail-metric">
                      <span class="dm-label">总延迟 P50 / P95</span>
                      <span class="dm-value">{{ formatMs(group.perf.p50_ms) }} / {{ formatMs(group.perf.p95_ms) }}</span>
                    </div>
                    <div class="detail-metric">
                      <span class="dm-label">首 Token P50 / P95</span>
                      <span class="dm-value">{{ formatMs(group.perf.ttft_p50_ms) }} / {{ formatMs(group.perf.ttft_p95_ms) }}</span>
                    </div>
                    <div v-if="group.perf.avg_tps > 0" class="detail-metric">
                      <span class="dm-label">输出速率</span>
                      <span class="dm-value">{{ group.perf.avg_tps.toFixed(1) }} tokens/s</span>
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
                      <span class="member-metrics">TTFT {{ formatMs(member.ttft_p50_ms) }} · 延迟 {{ formatMs(member.p50_ms) }}<template v-if="member.avg_tps > 0"> · {{ member.avg_tps.toFixed(1) }} tok/s</template> · 成功率 {{ formatPct(member.success_rate) }}</span>
                    </div>
                  </div>
                </div>
              </transition>
            </div>
          </template>

          <!-- Model view -->
          <template v-else>
            <div
              v-for="item in finalDisplayItems"
              :key="item.name"
              class="monitor-item"
              :class="[getItemStatusClass(item), { expanded: expandedGroups.has(item.name) }]"
              @click="toggleExpand(item.name)"
            >
              <div class="item-row">
                <div class="item-info">
                  <div class="info-line">
                    <el-tooltip :content="item.name" :show-after="300">
                      <span class="row-name">{{ item.name }}</span>
                    </el-tooltip>
                  </div>
                  <div class="meta-line">
                    <span v-if="item.used_by_groups" class="meta-text">{{ item.used_by_groups }} 组在用</span>
                    <span class="meta-text">{{ item.req_count }} 请求</span>
                    <span class="meta-sep">·</span>
                    <span class="meta-metric" :class="rateClass(item.success_rate)">
                      {{ formatPct(item.success_rate) }}
                    </span>
                    <span class="meta-sep">·</span>
                    <span class="meta-metric">TTFT {{ formatMs(item.ttft_p50_ms) }}</span>
                    <span class="meta-sep">·</span>
                    <span class="meta-metric">延迟 {{ formatMs(item.p50_ms) }}</span>
                    <span v-if="item.avg_tps > 0" class="meta-sep">·</span>
                    <span v-if="item.avg_tps > 0" class="meta-metric tps-metric">{{ item.avg_tps.toFixed(1) }} tok/s</span>
                  </div>
                </div>

                <!-- TTFT bar (model view only) -->
                <div class="ttft-bar-wrap">
                  <div class="ttft-track">
                    <div
                      class="ttft-fill"
                      :class="'fill-' + speedLevel(item.ttft_p50_ms)"
                      :style="{ width: ttftBarWidth(item.ttft_p50_ms) }"
                    ></div>
                  </div>
                </div>
              </div>

              <!-- Expanded detail for model view -->
              <transition name="slide">
                <div v-if="expandedGroups.has(item.name)" class="perf-detail">
                  <div class="detail-metrics">
                    <div class="detail-metric">
                      <span class="dm-label">成功率</span>
                      <span class="dm-value" :class="rateClass(item.success_rate)">{{ formatPct(item.success_rate) }}</span>
                    </div>
                    <div class="detail-metric">
                      <span class="dm-label">总延迟 P50 / P95</span>
                      <span class="dm-value">{{ formatMs(item.p50_ms) }} / {{ formatMs(item.p95_ms) }}</span>
                    </div>
                    <div class="detail-metric">
                      <span class="dm-label">首 Token P50 / P95</span>
                      <span class="dm-value">{{ formatMs(item.ttft_p50_ms) }} / {{ formatMs(item.ttft_p95_ms) }}</span>
                    </div>
                    <div v-if="item.avg_tps > 0" class="detail-metric">
                      <span class="dm-label">输出速率</span>
                      <span class="dm-value">{{ item.avg_tps.toFixed(1) }} tokens/s</span>
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

          <!-- Zero traffic groups -->
          <div v-if="view === 'group' && zeroTrafficGroups.length > 0" class="zero-traffic">
            <div class="zero-header" @click.stop="zeroExpanded = !zeroExpanded">
              <el-icon :size="14" class="chevron" :class="{ rotated: zeroExpanded }"><ArrowDown /></el-icon>
              <span>无流量模型组</span>
              <el-tag size="small" type="info" effect="plain" round>{{ zeroTrafficGroups.length }}</el-tag>
              <span class="zero-hint">所选 {{ lookbackLabel }} 窗口内无请求</span>
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

          <div v-if="finalDisplayItems.length > 0" class="state-end">
            共 {{ finalDisplayItems.length }} 个有流量的{{ view === 'group' ? '模型组' : '模型' }}
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
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { Refresh, Loading, ArrowDown } from '@element-plus/icons-vue'
import { getGroupHealthTimeline, getPerformance } from '@/services/control-plane'
import type { GroupTimelineGroup, PerformanceItem } from '@/services/control-plane'
import HealthTimeline from '@/components/admin/HealthTimeline.vue'
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement, GroupListItem } from '@/types/group'
import { useAuthStore } from '@/stores/auth'
import { formatMs } from '@/utils/time'

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
  avg_tps: number
  members?: any[]
}

const displayItems = computed((): DisplayItem[] => {
  if (view.value === 'group') {
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
          avg_tps: perf.avg_tps,
          members: perf.members
        }
      })
  } else {
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
      avg_tps: item.avg_tps,
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

const statusRank: Record<StatusInfo['key'], number> = { excellent: 0, good: 1, fair: 2, poor: 3 }

// Sorted display items
const finalDisplayItems = computed(() => {
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

// Zero traffic groups
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
  if (reset) { loadingMonitors.value = true } else { loadingMore.value = true }
  try {
    const offset = reset ? 0 : timelineGroups.value.length
    const result = await getGroupHealthTimeline(lookback.value, searchText.value, offset, PAGE_SIZE)
    let filteredGroups = result.groups
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null) {
      if (reset) await loadEntitlements()
      filteredGroups = result.groups.filter(g => isGroupAvailable(g.group_name))
    }
    if (reset) { timelineGroups.value = filteredGroups }
    else { timelineGroups.value = [...timelineGroups.value, ...filteredGroups] }
    total.value = result.total
  } catch {
    if (reset) timelineGroups.value = []
  } finally {
    loadingMonitors.value = false
    loadingMore.value = false
  }
}

async function loadPerformanceData(reset: boolean = true) {
  if (reset) { rawLoaded.value = 0; rawTotal.value = 0 }
  try {
    const viewType = view.value
    const result = await getPerformance(lookback.value, viewType, searchText.value, PAGE_SIZE, rawLoaded.value)
    let filteredItems = result.items || []
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null && viewType === 'group') {
      filteredItems = filteredItems.filter(item => isGroupAvailable(item.name))
    }
    if (reset) { perfItems.value = filteredItems }
    else { perfItems.value = [...perfItems.value, ...filteredItems] }
    rawTotal.value = result.total ?? result.items.length
    rawLoaded.value += result.items.length
    total.value = rawTotal.value
  } catch {
    if (reset) { perfItems.value = []; rawTotal.value = 0; rawLoaded.value = 0; total.value = 0 }
  }
}

async function loadEntitlements() {
  const role = authStore.effectiveRole
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    const groups = res.data.data ?? []
    allGroups.value = groups
    const newOpenModeMap = new Map<string, string>()
    for (const g of groups) { newOpenModeMap.set(g.name, g.open_mode) }
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
        } catch { return { groupName: g.name, items: [] } }
      })
      const entResults = await Promise.all(entitlementPromises)
      const newMap = new Map<string, GroupEntitlement[]>()
      for (const r of entResults) { newMap.set(r.groupName, r.items) }
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
  } catch { allGroups.value = [] }
}

async function refreshData() {
  loading.value = true
  try {
    if (view.value === 'group') {
      await Promise.all([loadTimelineData(true), loadPerformanceData(true), loadGroupsForZeroTraffic()])
    } else {
      await loadPerformanceData(true)
    }
  } finally { loading.value = false }
}

function onViewChange() {
  sortBy.value = view.value === 'group' ? 'health' : 'stability'
  refreshData()
}

function onLookbackChange() { refreshData() }
function onSearch() { refreshData() }
function onSortChange() { /* sorting is computed */ }

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (loadingMore.value) return
  if (rawLoaded.value >= rawTotal.value) return
  const { scrollTop, scrollHeight, clientHeight } = el
  if (scrollHeight - scrollTop - clientHeight < 100) { loadMore() }
}

async function loadMore() {
  if (loadingMore.value || rawLoaded.value >= rawTotal.value) return
  if (view.value === 'group') { await loadTimelineData(false); await loadPerformanceData(false) }
  else { await loadPerformanceData(false) }
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



function formatPct(rate: number): string { return (rate * 100).toFixed(1) + '%' }

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
    loadEntitlements().then(() => { refreshData() })
  } else { refreshData() }
  timer = setInterval(() => { refreshData() }, 60000)
})

onBeforeUnmount(() => { if (timer) { clearInterval(timer); timer = null } })
</script>

<style scoped>
.group-health-monitor-page {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  /* Single-hue green gradient based on brand #36B34E */
  --perf-excellent: #36B34E;
  --perf-good: rgba(54, 179, 78, 0.55);
  --perf-fair: rgba(54, 179, 78, 0.25);
  --perf-poor: #D97706;
}

.monitor-card-outer {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}

/* ── Header ── */
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  flex-wrap: wrap;
  gap: 8px;
  flex-shrink: 0;
}
.header-left { flex-shrink: 0; }
.header-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.lookback-label { font-size: 12px; color: var(--el-text-color-secondary); }
.lookback-select { width: 100px; }
.sort-select { width: 110px; }
.search-input { width: 160px; }

/* ── Body ── */
.card-body { padding: 16px 20px; }
.scroll-body { flex: 1; overflow-y: auto; min-height: 0; }
.scroll-body::-webkit-scrollbar { width: 5px; }
.scroll-body::-webkit-scrollbar-track { background: transparent; }
.scroll-body::-webkit-scrollbar-thumb { background: var(--el-border-color); border-radius: 3px; }

/* ── States ── */
.state-loading {
  display: flex; align-items: center; justify-content: center;
  gap: 8px; padding: 40px 0; color: var(--el-text-color-secondary);
}
.state-empty {
  display: flex; flex-direction: column; align-items: center;
  padding: 40px 0; color: var(--el-text-color-secondary); text-align: center;
}
.state-empty p { margin: 0; }
.state-empty .sub { font-size: 12px; margin-top: 6px; max-width: 420px; line-height: 1.6; }
.state-end { text-align: center; padding: 12px 0 4px; font-size: 12px; color: var(--el-text-color-secondary); }

/* ── Monitor list ── */
.monitor-list { display: flex; flex-direction: column; gap: 10px; }

.monitor-item {
  border: 1px solid var(--el-border-color-lighter);
  border-left: 4px solid var(--el-border-color-lighter);
  border-radius: 8px;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  background: var(--el-bg-color);
  cursor: pointer;
}
.monitor-item.status-excellent { border-left-color: var(--perf-excellent); }
.monitor-item.status-good { border-left-color: var(--perf-good); }
.monitor-item.status-fair { border-left-color: var(--perf-fair); }
.monitor-item.status-poor { border-left-color: var(--perf-poor); }
.monitor-item:hover {
  border-color: var(--el-color-primary-light-5);
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.06);
}
.monitor-item.expanded { box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06); }
.monitor-item.no-traffic { opacity: 0.55; border-left-color: var(--el-border-color-lighter); }
.monitor-item.no-traffic:hover { opacity: 0.75; }

/* ── Item row: info (fixed) + timeline (flex: 1, wide!) ── */
.item-row {
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 14px 16px;
  user-select: none;
}

/* Left: info — fixed width like the original */
.item-info { flex-shrink: 0; width: 280px; }
.info-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.format-badge { cursor: default; max-width: 180px; overflow: hidden; min-width: 0; }
.format-badge :deep(.el-tag__content) { overflow: hidden; }
.mono { font-family: monospace; font-size: 11px; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* Health percentage — text-only, uses brand green gradient */
.health-pct {
  font-size: 13px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
}
.health-pct.rate-excellent { color: var(--perf-excellent); }
.health-pct.rate-good { color: var(--perf-good); }
.health-pct.rate-fair { color: var(--perf-fair); }
.health-pct.rate-poor { color: var(--perf-poor); }

.row-name {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.meta-line {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  flex-wrap: wrap;
}
.meta-text { font-size: 11px; color: var(--el-text-color-secondary); }
.strategy-tag { flex-shrink: 0; }
.model-count { font-size: 12px; color: var(--el-text-color-secondary); }

/* Inline metrics in meta-line */
.meta-sep { font-size: 10px; color: var(--el-text-color-placeholder); }
.meta-metric {
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-primary);
}
.meta-metric.rate-excellent { color: var(--perf-excellent); }
.meta-metric.rate-good { color: var(--perf-good); }
.meta-metric.rate-fair { color: var(--perf-fair); }
.meta-metric.rate-poor { color: var(--perf-poor); }
.meta-metric.tps-metric { color: #6366f1; font-weight: 500; }
.meta-metric-dim {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}

/* Right: timeline — flex: 1, NO max-width, wide and spacious! */
.item-timeline { flex: 1; min-width: 0; }

.no-timeline {
  display: flex;
  align-items: center;
  justify-content: center;
}
.no-data-text { font-size: 12px; color: var(--el-text-color-placeholder); }

/* TTFT bar (model view) */
.ttft-bar-wrap {
  flex: 1;
  min-width: 80px;
}
.ttft-track {
  height: 6px;
  border-radius: 3px;
  background: var(--el-fill-color-light, #F1F5F9);
  overflow: hidden;
}
.ttft-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.5s cubic-bezier(0.4, 0, 0.2, 1);
}
.fill-fast { background: var(--perf-excellent); }
.fill-mid { background: var(--perf-fair); }
.fill-slow { background: var(--perf-poor); }

/* ── Expanded detail ── */
.perf-detail {
  padding: 12px 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-lighter, #FAFAFA);
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
  padding: 6px 10px;
  background: var(--el-bg-color);
  border-radius: 6px;
  border: 1px solid var(--el-border-color-lighter);
}
.dm-label { font-size: 11px; color: var(--el-text-color-secondary); font-weight: 500; }
.dm-value {
  font-size: 13px; font-weight: 600; color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.dm-value.rate-excellent { color: var(--perf-excellent); }
.dm-value.rate-good { color: var(--perf-good); }
.dm-value.rate-fair { color: var(--perf-fair); }
.dm-value.rate-poor { color: var(--perf-poor); }
.dm-value.is-warn { color: var(--perf-fair); }
.dm-value.is-danger { color: var(--perf-poor); }
.dm-value.is-fast { color: var(--perf-excellent); }
.dm-value.is-normal { color: var(--perf-good); }
.dm-value.is-slow { color: var(--perf-fair); }
.dm-value.is-very-slow { color: var(--perf-poor); }

/* Members list */
.members-list { margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--el-border-color-lighter); }
.members-title { font-size: 12px; font-weight: 600; color: var(--el-text-color-secondary); margin-bottom: 8px; }
.member-row { display: flex; align-items: center; gap: 10px; padding: 5px 0; }
.member-bar-track { flex: 1; height: 6px; background: var(--el-fill-color); border-radius: 3px; overflow: hidden; }
.member-bar-fill { height: 100%; background: linear-gradient(90deg, var(--perf-excellent), var(--perf-good)); border-radius: 3px; transition: width 0.3s; }
.member-traffic { font-size: 12px; font-weight: 600; color: var(--el-text-color-primary); min-width: 36px; text-align: right; }
.member-metrics { font-size: 11px; color: var(--el-text-color-secondary); white-space: nowrap; }

/* ── Zero traffic ── */
.zero-traffic {
  margin-top: 8px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  overflow: hidden;
}
.zero-header {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 12px;
  background: var(--el-fill-color-lighter, #FAFAFA);
  cursor: pointer; font-size: 13px; color: var(--el-text-color-regular);
  transition: background 0.15s;
}
.zero-header:hover { background: var(--el-fill-color); }
.zero-hint { font-size: 11px; color: var(--el-text-color-secondary); margin-left: auto; }
.zero-grid { display: flex; flex-wrap: wrap; gap: 6px; padding: 8px 12px; border-top: 1px solid var(--el-border-color-lighter); }
.zero-chip { display: flex; align-items: center; gap: 4px; padding: 3px 8px; background: var(--el-fill-color); border-radius: 4px; font-size: 11px; color: var(--el-text-color-regular); }
.zero-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--el-text-color-placeholder); }
.zero-name { font-family: monospace; font-size: 11px; }

/* Chevron */
.chevron { transition: transform 0.2s; color: var(--el-text-color-secondary); }
.chevron.rotated { transform: rotate(180deg); }

/* Slide transition */
.slide-enter-active, .slide-leave-active { transition: all 0.2s ease; }
.slide-enter-from, .slide-leave-to { opacity: 0; max-height: 0; }
.slide-enter-to, .slide-leave-from { opacity: 1; max-height: 500px; }

/* ── Responsive ── */
@media (max-width: 1024px) {
  .search-input { width: 140px; }
  .item-info { width: 240px; }
}

@media (max-width: 768px) {
  .card-header { flex-direction: column; align-items: stretch; }
  .header-actions { justify-content: space-between; flex-wrap: wrap; }
  .search-input { flex: 1; width: 100%; }
  .card-body { padding: 12px; }
  .item-row { flex-direction: column; align-items: stretch; gap: 10px; }
  .item-info { width: 100%; }
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
  .detail-metrics { grid-template-columns: 1fr; }
}
</style>
