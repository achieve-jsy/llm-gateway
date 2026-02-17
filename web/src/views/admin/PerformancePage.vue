<template>
  <div class="performance-page">
    <!-- Page-level tabs for cross-navigation -->
    <div class="page-tabs">
      <el-radio-group v-model="activePage" size="default" @change="onPageChange">
        <el-radio-button value="group-health">模型组健康</el-radio-button>
        <el-radio-button value="performance">模型性能</el-radio-button>
      </el-radio-group>
    </div>

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
          <div class="time-range-wrap">
            <el-radio-group v-model="windowVal" size="small" @change="onWindowChange">
              <el-radio-button value="1h">1h</el-radio-button>
              <el-radio-button value="6h">6h</el-radio-button>
              <el-radio-button value="24h">24h</el-radio-button>
            </el-radio-group>
          </div>
          <el-button :loading="loading" :icon="Refresh" size="small" circle @click="refreshData" />
        </div>
      </div>

      <!-- 说明：数据口径 -->
      <div class="info-strip">
        <span class="info-icon-circle">i</span>
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

      <div ref="scrollBodyRef" class="card-body scroll-body">
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

        <!-- 滚动哨兵：进入可视区触发加载更多 -->
        <div ref="sentinelRef" class="scroll-sentinel"></div>

        <div v-if="loadingMore" class="state-load-more">
          <el-icon class="is-loading" size="16"><Loading /></el-icon>
          <span>加载更多...</span>
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
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, Loading, ArrowDown, Search, InfoFilled, DataLine } from '@element-plus/icons-vue'
import { getPerformance } from '@/services/control-plane'
import type { PerformanceItem } from '@/services/control-plane'

const router = useRouter()

// Page-level tab switching
const activePage = ref('performance')
function onPageChange(val: string) {
  if (val === 'group-health') {
    router.push('/admin/group-health-monitor')
  }
  // else stay on current page
}
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement, GroupListItem } from '@/types/group'
import { useAuthStore } from '@/stores/auth'
import { formatMs } from '@/utils/time'

const authStore = useAuthStore()

const PAGE_SIZE = 50
const loading = ref(false)
const loadingMore = ref(false)
const items = shallowRef<PerformanceItem[]>([])
const total = ref(0)
// rawLoaded / rawTotal 跟踪服务端原始分页状态，与客户端过滤无关
const rawLoaded = ref(0)
const rawTotal = ref(0)
const windowVal = ref('1h')
const view = ref('group')
const searchText = ref('')
const sortBy = ref<'stability' | 'ttft' | 'latency' | 'req'>('stability')
let timer: ReturnType<typeof setInterval> | null = null

// 无限滚动
const scrollBodyRef = ref<HTMLElement | null>(null)
const sentinelRef = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | null = null

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
  const maxMs = 5000
  const clamped = Math.min(ttft, maxMs)
  // Inverted: faster → more filled (intuitive gauge)
  const pct = Math.max(4, Math.round(((maxMs - clamped) / maxMs) * 100))
  return pct + '%'
}

function speedLevel(ttft: number): 'fast' | 'mid' | 'slow' {
  if (ttft <= 1200) return 'fast'
  if (ttft <= 3500) return 'mid'
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
  rawLoaded.value = 0
  rawTotal.value = 0
  try {
    const result = await getPerformance(windowVal.value, view.value, searchText.value, PAGE_SIZE, 0)
    let filteredItems = result.items || []
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null && view.value === 'group') {
      filteredItems = filteredItems.filter(item => isGroupAvailable(item.name))
    }
    items.value = filteredItems
    // rawTotal / rawLoaded 始终基于服务端原始数据，与客户端过滤无关
    rawTotal.value = result.total ?? result.items.length
    rawLoaded.value = result.items.length
    total.value = rawTotal.value
  } catch {
    items.value = []
    rawTotal.value = 0
    rawLoaded.value = 0
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  if (loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  try {
    // 用 rawLoaded 作为 offset，确保分页不错位
    const result = await getPerformance(windowVal.value, view.value, searchText.value, PAGE_SIZE, rawLoaded.value)
    const rawItems = result.items || []

    const role = authStore.effectiveRole
    const newItems = (role !== 'admin' && role !== null && view.value === 'group')
      ? rawItems.filter(item => isGroupAvailable(item.name))
      : rawItems

    items.value = [...items.value, ...newItems]
    rawTotal.value = result.total ?? rawTotal.value
    rawLoaded.value += rawItems.length  // 累加服务端原始数量，而非过滤后数量
    total.value = rawTotal.value
  } catch {
    // keep existing items on error
  } finally {
    loadingMore.value = false
  }
}

async function reloadAndRebind() {
  await loadData()
  nextTick(setupObserver)
}

function refreshData() { reloadAndRebind() }
function onWindowChange() { reloadAndRebind() }
function setWindow(w: string) { windowVal.value = w; reloadAndRebind() }
function onViewChange() { reloadAndRebind() }
function onSearch() { reloadAndRebind() }

// hasMore 基于服务端原始分页状态，与客户端过滤无关
const hasMore = computed(() => rawLoaded.value < rawTotal.value)

function setupObserver() {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (!sentinelRef.value || !scrollBodyRef.value) return

  observer = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting && hasMore.value && !loadingMore.value && !loading.value) {
        loadMore()
      }
    },
    { root: scrollBodyRef.value, rootMargin: '0px 0px 120px 0px', threshold: 0 }
  )
  observer.observe(sentinelRef.value)
}

function toggleExpand(name: string) {
  const s = new Set(expandedGroups.value)
  if (s.has(name)) s.delete(name); else s.add(name)
  expandedGroups.value = s
}

async function initialLoad() {
  await loadGroupsAndEntitlements()
  await loadData()
  // nextTick 保证 DOM 已渲染新 items 后再绑定 observer，避免 sentinel 位置错误
  nextTick(setupObserver)
}

onMounted(() => {
  initialLoad()
  timer = setInterval(async () => {
    await loadData()
    // 定时刷新后重新绑定 observer（DOM 可能已变化）
    nextTick(setupObserver)
  }, 60000)
})
onBeforeUnmount(() => {
  if (timer) { clearInterval(timer); timer = null }
  if (observer) { observer.disconnect(); observer = null }
})



function formatPct(rate: number): string {
  return (rate * 100).toFixed(1) + '%'
}

function strategyLabel(s: string): string {
  if (s === 'priority') return '优先级'
  if (s === 'weighted') return '加权'
  if (s === 'auto') return '自动'
  return '轮询'
}

function strategyTagType(s: string): 'success' | 'warning' | 'info' | 'primary' {
  if (s === 'priority') return 'success'
  if (s === 'weighted') return 'warning'
  if (s === 'auto') return 'primary'
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
/* ═══════════════════════════════════════════════════════
   PerformancePage — Polished UI (7-item optimization)
   Design tokens from variables.scss + local overrides
   ═══════════════════════════════════════════════════════ */

/* Status color palette (local, not polluting global) */
.performance-page {
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
  width: 100%;
}

/* ── Page-level tabs ── */
.page-tabs {
  margin-bottom: 8px;
}

.page-tabs :deep(.el-radio-group) {
  background: var(--bg-surface, #fff);
  border-radius: 8px;
  padding: 4px;
  border: 1px solid var(--border-light, #F1F5F9);
}

.page-tabs :deep(.el-radio-button__inner) {
  border: none !important;
  border-radius: 6px !important;
  background: transparent !important;
  box-shadow: none !important;
  padding: 8px 20px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary, #64748B);
  transition: all 0.2s;
}

.page-tabs :deep(.el-radio-button.is-active .el-radio-button__inner) {
  background: var(--brand-green, #36B34E) !important;
  color: #fff !important;
  font-weight: 600;
}

/* ── Outer card ── */
.monitor-card-outer {
  border: 1px solid var(--border-default, #E2E8F0);
  border-radius: var(--radius-lg, 12px);
  background: var(--bg-surface, #fff);
  overflow: hidden;
  box-shadow: var(--shadow-sm);
}

/* ── Header ── */
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border-light, #F1F5F9);
  flex-wrap: wrap;
  gap: 10px;
}

.header-tabs { flex-shrink: 0; }

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* [7] Time range segmented control container */
.time-range-wrap {
  display: inline-flex;
  background: var(--bg-subtle, #F1F5F9);
  border-radius: 8px;
  padding: 3px;
}

.time-range-wrap :deep(.el-radio-group) {
  background: transparent;
}

.time-range-wrap :deep(.el-radio-button__inner) {
  border: none !important;
  border-radius: 6px !important;
  background: transparent !important;
  box-shadow: none !important;
  padding: 4px 14px;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary, #64748B);
  transition: all 0.2s;
}

.time-range-wrap :deep(.el-radio-button__original-radio:focus-visible + .el-radio-button__inner) {
  box-shadow: 0 0 0 2px var(--color-info, #2563EB) !important;
}

.time-range-wrap :deep(.el-radio-button.is-active .el-radio-button__inner) {
  background: var(--bg-surface, #fff) !important;
  color: var(--text-primary, #0F172A) !important;
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0,0,0,0.1) !important;
}

.search-input { width: 150px; }
.sort-select { width: 110px; }

/* ── [2] Info strip — neutral gray, no brand color ── */
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

.info-strip strong {
  color: var(--text-primary, #0F172A);
  font-weight: 600;
}

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

/* ── [4] Overview bar — compact horizontal ── */
.overview-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 6px 12px;
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

/* ── Card body ── */
.card-body { padding: 6px 12px 10px; }

.scroll-body {
  max-height: calc(100vh - 220px);
  min-height: 120px;
  overflow-y: auto;
}

/* Scrollbar */
.scroll-body::-webkit-scrollbar { width: 5px; }
.scroll-body::-webkit-scrollbar-track { background: transparent; }
.scroll-body::-webkit-scrollbar-thumb {
  background: var(--border-default, #E2E8F0);
  border-radius: 3px;
}
.scroll-body::-webkit-scrollbar-thumb:hover {
  background: var(--border-strong, #CBD5E1);
}

/* ── States ── */
.state-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 0;
  color: var(--text-secondary);
}

.state-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 48px 20px;
  color: var(--text-secondary);
  text-align: center;
}

.empty-icon {
  color: var(--text-placeholder);
  margin-bottom: 12px;
}

.empty-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.state-empty .sub {
  font-size: 12px;
  margin-top: 6px;
  max-width: 420px;
  line-height: 1.6;
}

.empty-actions { margin-top: 16px; }

.scroll-sentinel { height: 1px; }

.state-load-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 0;
  font-size: 12px;
  color: var(--text-secondary);
}

.state-end {
  text-align: center;
  padding: 10px 0 4px;
  font-size: 12px;
  color: var(--text-secondary);
}

/* ── Card grid ── */
.card-grid {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

/* ── [5] Perf row — flex layout matching GroupHealthMonitor ── */
.perf-row {
  border: 1px solid var(--border-light, #F1F5F9);
  border-left: 4px solid var(--border-default, #E2E8F0);
  border-radius: 8px;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  background: var(--bg-surface, #fff);
}

.perf-row.status-excellent { border-left-color: var(--perf-excellent); }
.perf-row.status-good { border-left-color: var(--perf-good); }
.perf-row.status-fair { border-left-color: var(--perf-fair); }
.perf-row.status-poor { border-left-color: var(--perf-poor); }

.perf-row:hover {
  border-left-width: 5px;
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.07);
}

.perf-row.expanded {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  border-color: var(--border-default, #E2E8F0);
}

.perf-row-main {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
  cursor: pointer;
  user-select: none;
}

/* ── Status cell ── */
.status-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  width: 70px;
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

/* ── Name cell ── */
.name-cell { 
  flex: 1;
  min-width: 0;
}

.row-name {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #0F172A);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.name-tags {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 2px;
  flex-wrap: wrap;
}

.mini-tag { flex-shrink: 0; }

.meta-text {
  font-size: 11px;
  color: var(--text-secondary, #64748B);
}

/* ── [1] Rate cell ── */
.rate-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  text-align: right;
  flex-shrink: 0;
  width: 80px;
}

.rate-value {
  font-size: 16px;
  font-weight: 700;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
}

.rate-label {
  font-size: 10px;
  color: var(--text-secondary, #64748B);
  margin-top: 1px;
  font-weight: 500;
}

/* ── [1] TTFT cell ── */
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

.ttft-value {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #0F172A);
  font-variant-numeric: tabular-nums;
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

/* ── Expand chevron ── */
.expand-cell {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-placeholder, #94A3B8);
}

.chevron { transition: transform 0.25s ease; }
.perf-row.expanded .chevron { transform: rotate(180deg); }

/* ── Detail section ── */
.perf-detail {
  border-top: 1px solid var(--border-light, #F1F5F9);
  padding: 10px 12px 12px;
  overflow: hidden;
  background: var(--bg-subtle, #F1F5F9);
}

.detail-metrics {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 8px 14px;
}

.detail-metric {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 10px;
  background: var(--bg-surface, #fff);
  border-radius: 6px;
  border: 1px solid var(--border-light, #F1F5F9);
}

.dm-label {
  font-size: 10px;
  color: var(--text-secondary, #64748B);
  font-weight: 500;
}

.dm-value {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #0F172A);
  font-variant-numeric: tabular-nums;
}

.dm-value.is-warn { color: var(--perf-fair); }
.dm-value.is-danger { color: var(--perf-poor); }
.dm-value.is-fast { color: var(--perf-excellent); }
.dm-value.is-normal { color: var(--text-regular, #334155); }
.dm-value.is-slow { color: var(--perf-fair); }
.dm-value.is-very-slow { color: var(--perf-poor); }

/* ── Members list ── */
.members-list {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--border-light, #F1F5F9);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.members-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary, #64748B);
  margin-bottom: 2px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.member-row {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) 80px 36px auto;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.member-bar-track {
  height: 5px;
  border-radius: 3px;
  background: var(--bg-subtle, #F1F5F9);
  overflow: hidden;
}

.member-bar-fill {
  height: 100%;
  border-radius: 3px;
  background: var(--perf-good);
  opacity: 0.6;
}

.member-traffic {
  color: var(--text-secondary, #64748B);
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.member-metrics {
  color: var(--text-secondary, #64748B);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

/* ── [6] Zero traffic — solid border, refined ── */
.zero-traffic {
  margin-top: 12px;
  border: 1px solid var(--border-default, #E2E8F0);
  border-radius: 8px;
  background: var(--bg-subtle, #F1F5F9);
  overflow: hidden;
}

.zero-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  cursor: pointer;
  user-select: none;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary, #64748B);
  transition: background 0.15s;
}

.zero-header:hover {
  background: var(--border-light, #F1F5F9);
}

.zero-header .chevron.rotated { transform: rotate(180deg); }

.zero-hint {
  font-size: 11px;
  font-weight: 400;
  color: var(--text-placeholder, #94A3B8);
  margin-left: 4px;
}

.zero-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 4px 12px 12px;
}

.zero-chip {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  background: var(--bg-surface, #fff);
  border: 1px solid var(--border-default, #E2E8F0);
  border-radius: 14px;
  font-size: 11px;
  transition: all 0.15s;
}

.zero-chip:hover {
  border-color: var(--border-strong, #CBD5E1);
  box-shadow: 0 1px 3px rgba(0,0,0,0.05);
}

.zero-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--text-placeholder, #94A3B8);
}

.zero-name {
  color: var(--text-regular, #334155);
  font-weight: 500;
}

/* ── Slide animation ── */
.slide-enter-active,
.slide-leave-active {
  transition: all 0.2s ease;
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

/* ── Responsive ── */
@media (max-width: 1024px) {
  .search-input { width: 120px; }
  .perf-row-main { gap: 12px; padding: 10px 12px; }
  .name-cell { min-width: 120px; }
  .ttft-cell { width: 100px; }
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
    flex-wrap: wrap;
    gap: 8px;
  }
  .status-cell { width: auto; }
  .name-cell { flex: 1 1 60%; min-width: 0; }
  .rate-cell { width: auto; }
  .ttft-cell { width: 100%; }
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
  .card-header { padding: 8px 10px; }
  .card-body { padding: 10px; }
  .info-strip { padding: 6px 10px; }
  .overview-bar { padding: 6px 10px; gap: 4px; }
  .overview-item { padding: 3px 8px; }
  .overview-num { font-size: 16px; }
  .overview-label { font-size: 11px; }
}

/* ── Dark mode ── */
html.dark .monitor-card-outer {
  border-color: var(--border-default);
  background: var(--bg-surface);
}

html.dark .info-strip { background: var(--bg-subtle); }
html.dark .perf-detail { background: var(--bg-subtle); }
html.dark .zero-traffic { background: var(--bg-subtle); border-color: var(--border-default); }
html.dark .zero-chip { background: var(--bg-surface); border-color: var(--border-default); }
html.dark .detail-metric { background: var(--bg-surface); border-color: var(--border-default); }

html.dark .perf-row:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
}

html.dark .overview-item.is-excellent { background: rgba(5, 150, 105, 0.12); }
html.dark .overview-item.is-good { background: rgba(79, 107, 246, 0.12); }
html.dark .overview-item.is-fair { background: rgba(217, 119, 6, 0.12); }
html.dark .overview-item.is-poor { background: rgba(220, 38, 38, 0.12); }

html.dark .time-range-wrap {
  background: var(--bg-subtle);
}
html.dark .time-range-wrap :deep(.el-radio-button.is-active .el-radio-button__inner) {
  background: var(--bg-surface) !important;
  color: var(--text-primary) !important;
}
</style>
