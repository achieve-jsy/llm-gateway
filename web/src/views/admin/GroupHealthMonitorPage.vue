<template>
  <div class="group-health-monitor-page">
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
import { Refresh, Loading } from '@element-plus/icons-vue'
import { getGroupHealthTimeline } from '@/services/control-plane'
import type { GroupTimelineGroup } from '@/services/control-plane'
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
    // 授权数据加载失败，不显示任何受控组
    entitlementsMap.value = new Map()
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

function strategyLabel(strategy: string): string {
  switch (strategy) {
    case 'round_robin': return '轮询'
    case 'priority': return '优先级'
    case 'weighted': return '加权'
    default: return strategy || '轮询'
  }
}

function strategyTagType(strategy: string): 'info' | 'success' | 'warning' {
  switch (strategy) {
    case 'priority': return 'success'
    case 'weighted': return 'warning'
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

/* ─── 平板 (768 ~ 1024) ─── */
@media (max-width: 1024px) {
  .search-input { width: 140px; }
  .item-info { width: 220px; }
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
