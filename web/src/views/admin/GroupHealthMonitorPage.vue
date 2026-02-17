<template>
  <div class="group-health-monitor-page">
    <!-- Page-level tabs for cross-navigation -->
    <div class="page-tabs">
      <el-radio-group v-model="activePage" size="default" @change="onPageChange">
        <el-radio-button value="group-health">模型组健康</el-radio-button>
        <el-radio-button value="performance">模型性能</el-radio-button>
      </el-radio-group>
    </div>

    <div class="monitor-card-outer">
      <div class="card-header">
        <h3 class="card-title">模型组健康监控</h3>
        <div class="header-actions">
          <el-input v-model="searchText" placeholder="搜索模型组名称" size="small" clearable class="search-input" @keyup.enter="onSearch" @clear="onSearch" />
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
          <p>暂无模型组健康监控数据</p>
          <p class="sub">模型组下的模型尚未产生检测记录</p>
        </div>

        <div v-else class="monitor-list">
          <div
            v-for="group in groups"
            :key="group.group_name"
            class="monitor-item"
            :class="{ 'no-traffic': !perfMap.get(group.group_name) }"
          >
            <div class="item-row">
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
                </div>
              </div>
              <div class="item-timeline">
                <HealthTimeline :timeline="group.timeline" :lookback="lookback" />
              </div>
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
            </div>
          </div>

          <div v-if="loadingMore" class="state-loading" style="padding:12px 0">
            <el-icon class="is-loading" size="16"><Loading /></el-icon>
            <span style="font-size:12px">加载更多...</span>
          </div>
          <div v-else-if="groups.length >= total" class="state-end">
            <span>共 {{ total }} 个模型组</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, Loading } from '@element-plus/icons-vue'
import { getGroupHealthTimeline, getPerformance } from '@/services/control-plane'
import type { GroupTimelineGroup, PerformanceItem } from '@/services/control-plane'

const router = useRouter()

// Page-level tab switching
const activePage = ref('group-health')
function onPageChange(val: string) {
  if (val === 'performance') {
    router.push('/admin/performance')
  }
  // else stay on current page
}
import HealthTimeline from '@/components/admin/HealthTimeline.vue'
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement } from '@/types/group'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const PAGE_SIZE = 10
const loading = ref(false)
const loadingMonitors = ref(false)
const loadingMore = ref(false)
const groups = shallowRef<GroupTimelineGroup[]>([])
const total = ref(0)
const lookback = ref('6h')
const searchText = ref('')
let timer: ReturnType<typeof setInterval> | null = null

// 性能数据缓存：groupName -> PerformanceItem
const perfMap = ref<Map<string, PerformanceItem>>(new Map())

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
    
    // 对非 admin 用户进行授权过滤
    const role = authStore.effectiveRole
    if (role !== 'admin' && role !== null) {
      // 首次加载时获取授权数据
      if (reset) {
        await loadEntitlements()
      }
      
      // 过滤组：只保留用户有权限查看的组
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

  // 所有非 admin 用户都需要知道哪些组是 public（普通员工仅靠 open_mode 判断可见性）。
  // 之前这里对非 studio_admin/dept_admin 直接 return，导致 openModeMap 为空，
  // 普通员工的 public 组全部被 isGroupAvailable 过滤掉、整页空白。
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    const allGroups = res.data.data ?? []

    // 存储所有组的 open_mode
    const newOpenModeMap = new Map<string, string>()
    for (const g of allGroups) {
      newOpenModeMap.set(g.name, g.open_mode)
    }
    openModeMap.value = newOpenModeMap

    // 仅 studio_admin / dept_admin 需要进一步加载受控组授权记录
    if (role === 'studio_admin' || role === 'dept_admin') {
      // 从 orgRoles 提取部门/工作室 ID
      const deptRoles = authStore.orgRoles.filter(r => r.role === 'dept_admin' && r.scope_type === 'dept')
      myDeptIds.value = deptRoles.map(r => r.scope_id)

      const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin' && r.scope_type === 'studio')
      myStudioId.value = studioRole?.scope_id || ''

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
    }
  } catch {
    // 数据加载失败：清空，受控组不显示（public 组也会因 openModeMap 为空而不显示）
    openModeMap.value = new Map()
    entitlementsMap.value = new Map()
  }
}

async function refreshData() {
  loading.value = true
  try { 
    await Promise.all([
      loadTimeline(true),
      loadPerformanceData()
    ])
  } finally { 
    loading.value = false 
  }
}

// 加载性能数据（成功率/TTFT）
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

onMounted(() => {
  // 非 admin 用户需要先加载授权数据
  const role = authStore.effectiveRole
  if (role !== 'admin' && role !== null) {
    loadEntitlements().then(() => {
      refreshData()
    })
  } else {
    refreshData()
  }
  timer = setInterval(() => loadTimeline(true), 60000)
})
onBeforeUnmount(() => { if (timer) { clearInterval(timer); timer = null } })
</script>

<style scoped>
.group-health-monitor-page {
  width: 100%;
}

/* ── Page-level tabs ── */
.page-tabs {
  margin-bottom: 12px;
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
.item-info { flex-shrink: 0; width: 260px; }
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
.model-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

/* Right: timeline */
.item-timeline { flex: 1; min-width: 0; }

/* Performance metrics column */
.item-perf {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-shrink: 0;
  width: 160px;
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

/* No traffic state - dimmed */
.monitor-item.no-traffic {
  opacity: 0.6;
}
.monitor-item.no-traffic:hover {
  opacity: 0.8;
  border-color: var(--el-border-color-lighter);
}

/* ─── 平板 (768 ~ 1024) ─── */
@media (max-width: 1024px) {
  .search-input { width: 140px; }
  .item-info { width: 220px; }
  .item-perf { width: 140px; gap: 12px; }
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

  .item-row { 
    flex-direction: column; 
    align-items: stretch; 
    gap: 10px; 
  }
  .item-info { min-width: 0; max-width: 100%; width: 100%; }
  .item-perf { 
    width: 100%; 
    justify-content: center;
    padding-top: 8px;
    border-top: 1px solid var(--el-border-color-lighter);
  }
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
