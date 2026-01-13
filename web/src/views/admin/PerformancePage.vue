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
            @keyup.enter="onSearch" @clear="onSearch" />
          <el-radio-group v-model="windowVal" size="small" @change="onWindowChange">
            <el-radio-button value="1h">1h</el-radio-button>
            <el-radio-button value="6h">6h</el-radio-button>
            <el-radio-button value="24h">24h</el-radio-button>
          </el-radio-group>
          <el-button :loading="loading" :icon="Refresh" size="small" circle @click="refreshData" />
        </div>
      </div>

      <div ref="scrollBodyRef" class="card-body scroll-body">
        <div v-if="loading && items.length === 0" class="state-loading">
          <el-icon class="is-loading" size="20"><Loading /></el-icon>
          <span>加载中...</span>
        </div>

        <div v-else-if="items.length === 0" class="state-empty">
          <p>暂无性能数据</p>
          <p class="sub">当前时间窗口内没有请求记录</p>
        </div>

        <div v-else class="card-grid">
          <div v-for="item in items" :key="item.name" class="perf-card">
            <div class="perf-card-header">
              <div class="card-title-area">
                <el-tooltip :content="item.name" :show-after="300">
                  <el-tag class="format-badge" type="info" size="small" effect="plain">
                    <span class="mono">{{ item.name }}</span>
                  </el-tag>
                </el-tooltip>
                <el-tag v-if="view === 'group' && item.strategy"
                  :type="strategyTagType(item.strategy)" size="small" effect="plain" class="strategy-tag">
                  {{ strategyLabel(item.strategy) }}
                </el-tag>
                <span v-if="view === 'group' && item.model_count" class="model-count">
                  {{ item.model_count }} 个模型
                </span>
                <span v-if="view === 'model' && item.used_by_groups" class="meta-text">
                  被 {{ item.used_by_groups }} 个组使用
                </span>
              </div>
              <div class="card-summary">
                <span>{{ item.req_count }} 请求</span>
                <span class="sep-dot"></span>
                <span>成功率 {{ formatPct(item.success_rate) }}</span>
                <template v-if="item.failover_count > 0">
                  <span class="sep-dot"></span>
                  <span>failover {{ item.failover_count }}</span>
                </template>
              </div>
            </div>

            <div class="perf-card-metrics">
              <div class="metric-row">
                <span class="metric-label">总延迟</span>
                <span class="metric-values">
                  <span>P50 <strong>{{ formatMs(item.p50_ms) }}</strong></span>
                  <span>P95 <strong>{{ formatMs(item.p95_ms) }}</strong></span>
                </span>
              </div>
              <div v-if="item.pct_in_24h_p50 != null" class="pct-indicator">
                当前 P50 处于 24h 的 P{{ item.pct_in_24h_p50 }} 水平
                <span class="pct-label" :class="pctClass(item.pct_in_24h_p50)">
                  ({{ percentileLabel(item.pct_in_24h_p50) }})
                </span>
              </div>
              <div class="metric-row">
                <span class="metric-label">首 Token</span>
                <span class="metric-values">
                  <span>P50 <strong>{{ formatMs(item.ttft_p50_ms) }}</strong></span>
                  <span>P95 <strong>{{ formatMs(item.ttft_p95_ms) }}</strong></span>
                </span>
              </div>
            </div>

            <div v-if="view === 'group' && item.members && item.members.length > 0" class="expand-area">
              <div class="expand-toggle" @click="toggleExpand(item.name)">
                <el-icon :size="14">
                  <component :is="expandedGroups.has(item.name) ? ArrowUp : ArrowDown" />
                </el-icon>
                <span>{{ expandedGroups.has(item.name) ? '收起' : '展开' }}组内模型</span>
              </div>
              <transition name="slide">
                <div v-if="expandedGroups.has(item.name)" class="members-list">
                  <div v-for="member in item.members" :key="member.model_name + member.provider" class="member-row">
                    <el-tag size="small" effect="plain">{{ member.model_name }}</el-tag>
                    <span class="member-traffic">{{ (member.traffic_pct * 100).toFixed(0) }}%</span>
                    <span class="member-metrics">P50 {{ formatMs(member.p50_ms) }} · 成功率 {{ formatPct(member.success_rate) }}</span>
                  </div>
                </div>
              </transition>
            </div>
          </div>
        </div>

        <!-- 滚动哨兵：进入可视区触发加载更多 -->
        <div ref="sentinelRef" class="scroll-sentinel"></div>

        <div v-if="loadingMore" class="state-load-more">
          <el-icon class="is-loading" size="16"><Loading /></el-icon>
          <span>加载更多...</span>
        </div>

        <div v-if="items.length > 0 && !hasMore && !loading" class="state-end">
          共 {{ total }} {{ view === 'group' ? '个模型组' : '个模型' }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { Refresh, Loading, ArrowDown, ArrowUp } from '@element-plus/icons-vue'
import { getPerformance } from '@/services/control-plane'
import type { PerformanceItem } from '@/services/control-plane'
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement } from '@/types/group'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const PAGE_SIZE = 50
const loading = ref(false)
const loadingMore = ref(false)
const items = shallowRef<PerformanceItem[]>([])
const total = ref(0)
const offset = ref(0)
const windowVal = ref('1h')
const view = ref('group')
const searchText = ref('')
let timer: ReturnType<typeof setInterval> | null = null

// 无限滚动
const scrollBodyRef = ref<HTMLElement | null>(null)
const sentinelRef = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | null = null

const expandedGroups = ref<Set<string>>(new Set())

// 授权相关
const entitlementsMap = ref<Map<string, GroupEntitlement[]>>(new Map())
const openModeMap = ref<Map<string, string>>(new Map()) // groupName -> open_mode
const myDeptIds = ref<string[]>([])
const myStudioId = ref('')

// 判断模型组是否对当前用户可用
function isGroupAvailable(groupName: string): boolean {
  const role = authStore.effectiveRole
  
  // admin 可以看到所有组
  if (role === 'admin') return true
  
  // 公开组所有人都可见
  if (openModeMap.value.get(groupName) === 'public') return true
  
  // 受控组需要检查授权
  const ents = entitlementsMap.value.get(groupName) || []
  
  // studio_admin: 检查工作室是否被授权
  if (role === 'studio_admin' && myStudioId.value) {
    return ents.some(e => e.scope_type === 'studio' && e.scope_id === myStudioId.value && e.enabled)
  }
  
  // dept_admin: 检查本部门是否被开通
  if (role === 'dept_admin' && myDeptIds.value.length > 0) {
    return myDeptIds.value.some(deptId => 
      ents.some(e => e.scope_type === 'dept' && e.scope_id === deptId && e.enabled)
    )
  }
  
  return false
}

async function loadEntitlements() {
  const role = authStore.effectiveRole
  if (role !== 'studio_admin' && role !== 'dept_admin') return
  
  // 从 orgRoles 提取部门/工作室 ID
  const deptRoles = authStore.orgRoles.filter(r => r.role === 'dept_admin' && r.scope_type === 'dept')
  myDeptIds.value = deptRoles.map(r => r.scope_id)
  
  const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin' && r.scope_type === 'studio')
  myStudioId.value = studioRole?.scope_id || ''
  
  // 获取所有模型组的授权记录 + open_mode
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    const allGroups = res.data.data ?? []
    
    // 存储所有组的 open_mode
    const newOpenModeMap = new Map<string, string>()
    for (const g of allGroups) {
      newOpenModeMap.set(g.name, g.open_mode)
    }
    openModeMap.value = newOpenModeMap
    
    // 只拉取受控组的授权记录
    const controlledGroups = allGroups.filter(g => g.open_mode === 'controlled' && g.enabled)
    
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
  } catch {
    entitlementsMap.value = new Map()
  }
}

async function loadData() {
  loading.value = true
  offset.value = 0
  try {
    const result = await getPerformance(windowVal.value, view.value, searchText.value, PAGE_SIZE, 0)
    let filteredItems = result.items || []
    
    // 对非 admin 用户进行授权过滤（仅 group 视角需要过滤）
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null && view.value === 'group') {
      filteredItems = filteredItems.filter(item => isGroupAvailable(item.name))
    }
    
    items.value = filteredItems
    total.value = result.total ?? filteredItems.length
    offset.value = result.items?.length ?? 0
  } catch {
    items.value = []
    total.value = 0
  } finally {
    loading.value = false
    // 数据刷新后重新绑定 observer（sentinel 位置可能已变化）
    setupObserver()
  }
}

async function loadMore() {
  if (loadingMore.value) return
  loadingMore.value = true
  try {
    const result = await getPerformance(windowVal.value, view.value, searchText.value, PAGE_SIZE, offset.value)
    let newItems = result.items || []

    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null && view.value === 'group') {
      newItems = newItems.filter(item => isGroupAvailable(item.name))
    }

    items.value = [...items.value, ...newItems]
    total.value = result.total ?? items.value.length
    offset.value += newItems.length
  } catch {
    // keep existing items on error
  } finally {
    loadingMore.value = false
  }
}

function refreshData() { loadData() }
function onWindowChange() { loadData() }
function onViewChange() { loadData() }
function onSearch() { loadData() }

const hasMore = computed(() => items.value.length < total.value)

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
    { root: scrollBodyRef.value, rootMargin: '0px 0px 80px 0px', threshold: 0 }
  )
  observer.observe(sentinelRef.value)
}

function toggleExpand(name: string) {
  const s = new Set(expandedGroups.value)
  if (s.has(name)) s.delete(name); else s.add(name)
  expandedGroups.value = s
}

onMounted(() => {
  // 非 admin 用户需要先加载授权数据
  const role = authStore.effectiveRole
  if (role !== 'admin' && role !== null) {
    loadEntitlements().then(() => {
      loadData().then(() => setupObserver())
    })
  } else {
    loadData().then(() => setupObserver())
  }
  timer = setInterval(() => loadData(), 60000)
})
onBeforeUnmount(() => {
  if (timer) { clearInterval(timer); timer = null }
  if (observer) { observer.disconnect(); observer = null }
})

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
  box-shadow: 0 1px 2px rgba(0,0,0,0.04);
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
  width: 140px;
}

.card-body {
  padding: 20px;
}

.scroll-body {
  max-height: calc(100vh - 200px);
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
  padding: 40px 0;
  color: var(--el-text-color-secondary);
}

.state-empty .sub {
  font-size: 12px;
  margin-top: 4px;
}

.scroll-sentinel {
  height: 1px;
}

.state-load-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 12px 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.state-end {
  text-align: center;
  padding: 8px 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.card-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.perf-card {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 16px;
  transition: border-color 0.15s;
}

.perf-card:hover {
  border-color: var(--el-color-primary-light-5);
}

.perf-card-header {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.card-title-area {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.format-badge {
  cursor: default;
  max-width: 220px;
  overflow: hidden;
  min-width: 0;
}

.format-badge :deep(.el-tag__content) {
  overflow: hidden;
}

.mono {
  font-family: monospace;
  font-size: 11px;
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.strategy-tag {
  flex-shrink: 0;
}

.model-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.meta-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.card-summary {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}

.sep-dot::before {
  content: '·';
  margin: 0 2px;
}

.perf-card-metrics {
  margin-top: 12px;
}

.metric-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
}

.metric-row:first-child {
  margin-top: 0;
}

.metric-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  width: 60px;
  flex-shrink: 0;
}

.metric-values {
  display: flex;
  align-items: center;
  gap: 16px;
  font-size: 12px;
}

.metric-values strong {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.pct-indicator {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
  margin-left: 72px;
}

.pct-label {
  margin-left: 2px;
}

.pct-label.is-fast {
  color: var(--el-color-success);
}

.pct-label.is-normal {
  color: var(--el-text-color-regular);
}

.pct-label.is-slow {
  color: var(--el-color-warning);
}

.pct-label.is-very-slow {
  color: var(--el-color-danger);
  font-weight: 600;
}

.expand-area {
  margin-top: 12px;
  border-top: 1px dashed var(--el-border-color-lighter);
  padding-top: 8px;
}

.expand-toggle {
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  padding: 6px 0;
  color: var(--el-text-color-secondary);
  user-select: none;
}

.expand-toggle:hover {
  color: var(--el-color-primary);
}

.members-list {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.member-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.member-traffic {
  color: var(--el-text-color-secondary);
  min-width: 32px;
}

.member-metrics {
  color: var(--el-text-color-secondary);
}

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
  max-height: 500px;
}

@media (max-width: 1024px) {
  .search-input {
    width: 120px;
  }
}

@media (max-width: 768px) {
  .card-header {
    flex-direction: column;
    align-items: stretch;
  }

  .header-tabs {
    margin-bottom: 4px;
  }

  .header-actions {
    justify-content: space-between;
  }

  .search-input {
    flex: 1;
    width: 100%;
  }

  .card-body {
    padding: 14px 12px;
  }

  .scroll-body {
    max-height: calc(100vh - 160px);
  }

  .perf-card {
    padding: 12px;
  }

  .state-loading,
  .state-empty {
    padding: 24px 0;
  }
}

@media (max-width: 480px) {
  .header-actions {
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }

  .search-input {
    width: 100%;
  }

  .card-header {
    padding: 10px 12px;
  }

  .card-body {
    padding: 10px 8px;
  }

  .perf-card {
    padding: 10px;
    border-radius: 6px;
  }

  .card-grid {
    gap: 8px;
  }

  .metric-label {
    width: 50px;
  }

  .pct-indicator {
    margin-left: 62px;
  }
}
</style>
