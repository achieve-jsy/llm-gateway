<template>
  <div class="entitlements-page">
    <div class="page-header">
      <div>
        <h2 class="page-title">模型组授权</h2>
        <p class="page-desc">
          {{ roleDescription }}
        </p>
      </div>
    </div>

    <!-- ========== Admin 视图 ========== -->
    <template v-if="authStore.effectiveRole === 'admin'">
      <!-- 筛选工具栏 + 紧凑统计条 -->
      <div class="toolbar">
        <el-radio-group v-model="openModeFilter" size="default">
          <el-radio-button value="all">全部 {{ groups.length }}</el-radio-button>
          <el-radio-button value="public">公开 {{ publicCount }}</el-radio-button>
          <el-radio-button value="controlled">受控 {{ controlledCount }}</el-radio-button>
        </el-radio-group>
        <div class="toolbar-right">
          <span class="stat-inline">
            <span class="stat-inline__dot stat-inline__dot--ctrl"></span>
            {{ totalGrantedStudios }} 项工作室授权
          </span>
          <el-input
            v-model="searchQuery"
            placeholder="搜索模型组名称"
            clearable
            size="default"
            class="admin-search"
            :prefix-icon="Search"
          />
        </div>
      </div>

      <div v-loading="loading" class="content-area">
        <!-- 卡片网格 -->
        <div v-if="filteredGroups.length" class="card-grid">
          <GroupAuthCard
            v-for="group in filteredGroups"
            :key="group.id"
            :group="group"
            :cap="caps[group.name]"
            :accent="group.open_mode === 'public' ? 'success' : 'warning'"
            :status-text="group.open_mode === 'public' ? '公开' : '受控'"
            :hint="adminScopeHint(group)"
            :clickable="group.open_mode === 'controlled'"
            @click="openDrawer(group)"
          >
            <template #footer>
              <el-popconfirm
                :title="group.open_mode === 'public'
                  ? '切换为「受控」后，需为工作室/部门授权后才能使用，确定切换？'
                  : '切换为「公开」后，所有用户均可使用该组，现有授权将失效，确定切换？'"
                confirm-button-text="确定"
                @confirm="handleToggleOpenMode(group)"
              >
                <template #reference>
                  <el-button text size="small">
                    {{ group.open_mode === 'public' ? '切为受控' : '切为公开' }}
                  </el-button>
                </template>
              </el-popconfirm>
              <span v-if="group.open_mode === 'controlled'" class="detail-hint" @click="openDrawer(group)">
                管理授权
                <el-icon><ArrowRight /></el-icon>
              </span>
            </template>
          </GroupAuthCard>
        </div>

        <el-empty v-else-if="!loading" :description="searchQuery ? '没有匹配的模型组' : '暂无模型组'" :image-size="80" />
      </div>
    </template>

    <!-- ========== StudioAdmin 视图 ========== -->
    <template v-else-if="authStore.effectiveRole === 'studio_admin'">
      <!-- 工作室信息卡 -->
      <div v-if="myStudioInfo" class="dept-info-card">
        <div class="dept-info-row">
          <span class="dept-info-label">我的工作室：</span>
          <span class="dept-info-name">{{ myStudioInfo.name }}</span>
        </div>
      </div>

      <div class="toolbar">
        <el-radio-group v-model="studioFilter" size="default">
          <el-radio-button value="all">全部 {{ studioCounts.all }}</el-radio-button>
          <el-radio-button value="manage">可管理 {{ studioCounts.manage }}</el-radio-button>
          <el-radio-button value="public">全员可用 {{ studioCounts.public }}</el-radio-button>
          <el-radio-button value="locked">未授权 {{ studioCounts.locked }}</el-radio-button>
        </el-radio-group>
      </div>

      <div v-loading="loading" class="content-area">
        <div v-if="studioGrid.length" class="card-grid">
          <GroupAuthCard
            v-for="card in studioGrid"
            :key="card.group.id"
            :group="card.group"
            :cap="caps[card.group.name]"
            :accent="card.accent"
            :status-text="card.tagText"
            :hint="card.hint"
            :clickable="card.cat === 'manage'"
            @click="openDrawer(card.group)"
          >
            <template v-if="card.cat === 'manage'" #footer>
              <span class="footer-spacer"></span>
              <span class="detail-hint" @click="openDrawer(card.group)">
                管理部门开通
                <el-icon><ArrowRight /></el-icon>
              </span>
            </template>
          </GroupAuthCard>
        </div>
        <el-empty v-else-if="!loading" description="该分类下暂无模型组" :image-size="80" />
      </div>
    </template>

    <!-- ========== DeptAdmin 视图 ========== -->
    <template v-else-if="authStore.effectiveRole === 'dept_admin'">
      <!-- 部门信息卡 + 部门切换 -->
      <div class="dept-info-card">
        <div class="dept-info-row">
          <span class="dept-info-label">我的部门：</span>
          <el-select
            v-model="selectedDeptId"
            size="small"
            style="width: 200px; margin-right: 16px"
            @change="onDeptChange"
          >
            <el-option
              v-for="deptId in myDeptIds"
              :key="deptId"
              :label="allDepts.find(d => d.dept_id === deptId)?.dept_name || deptId"
              :value="deptId"
            />
          </el-select>
          <span v-if="myDeptInfo?.studio_name" class="dept-info-studio">
            所属工作室：{{ myDeptInfo.studio_name }}
          </span>
        </div>
      </div>

      <div class="toolbar">
        <el-radio-group v-model="deptFilter" size="default">
          <el-radio-button value="all">全部 {{ deptCounts.all }}</el-radio-button>
          <el-radio-button value="can_enable">可开通 {{ deptCounts.can_enable }}</el-radio-button>
          <el-radio-button value="available">已可用 {{ deptCounts.available }}</el-radio-button>
          <el-radio-button value="locked">未授权 {{ deptCounts.locked }}</el-radio-button>
        </el-radio-group>
      </div>

      <div v-loading="loading" class="content-area">
        <div v-if="deptGrid.length" class="card-grid">
          <GroupAuthCard
            v-for="card in deptGrid"
            :key="card.group.id"
            :group="card.group"
            :cap="caps[card.group.name]"
            :accent="card.accent"
            :status-text="card.tagText"
            :hint="card.hint"
          >
            <template
              v-if="card.cat === 'can_enable' || (card.cat === 'available' && card.group.open_mode === 'controlled')"
              #footer
            >
              <span class="footer-spacer"></span>
              <!-- 可开通：一键开通 -->
              <el-button
                v-if="card.cat === 'can_enable'"
                type="primary"
                size="small"
                :loading="enablingGroupId === card.group.id"
                @click="handleDeptAdminEnable(card.group)"
              >
                一键开通本部门
              </el-button>
              <!-- 已可用且受控：可撤销 -->
              <el-popconfirm
                v-else
                title="确定撤销本部门的开通？撤销后本部门将无法使用该模型组。"
                confirm-button-text="确定撤销"
                confirm-button-type="danger"
                @confirm="handleDeptAdminDisable(card.group)"
              >
                <template #reference>
                  <el-button type="danger" text size="small" :loading="disablingGroupId === card.group.id">
                    撤销本部门开通
                  </el-button>
                </template>
              </el-popconfirm>
            </template>
          </GroupAuthCard>
        </div>
        <el-empty v-else-if="!loading" description="该分类下暂无模型组" :image-size="80" />
      </div>
    </template>

    <!-- ========== 普通用户或未识别角色 ========== -->
    <template v-else>
      <div class="content-area">
        <el-empty description="您没有授权管理权限" :image-size="80" />
      </div>
    </template>

    <!-- GroupEntitlementDialog Drawer -->
    <GroupEntitlementDialog
      v-model:visible="drawerVisible"
      :group-id="activeGroupId"
      :group-name="activeGroupName"
      :actor-role="actorRole"
      :restrict-studio-id="myStudioId"
      :restrict-dept-id="selectedDeptId"
      @closed="onDrawerClosed"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, ArrowRight } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { groupsApi } from '@/api/groups'
import type { GroupCapability } from '@/api/groups'
import type { GroupListItem, GroupEntitlement, OpenMode } from '@/types/group'
import type { Studio, OrgDepartment } from '@/types/org'
import http from '@/services/http'
import GroupEntitlementDialog from '@/components/GroupEntitlementDialog.vue'
import GroupAuthCard from '@/components/GroupAuthCard.vue'

const authStore = useAuthStore()

// ─── 状态 ──────────────────────────────────────────────────────────────────────
const loading = ref(false)
const groups = ref<GroupListItem[]>([])
const caps = ref<Record<string, GroupCapability>>({})
const entitlementsMap = ref<Map<number, GroupEntitlement[]>>(new Map())
const allStudios = ref<Studio[]>([])
const allDepts = ref<OrgDepartment[]>([])

// Drawer 状态
const drawerVisible = ref(false)
const activeGroupId = ref<number | null>(null)
const activeGroupName = ref('')

// Admin 筛选
const openModeFilter = ref<'all' | 'public' | 'controlled'>('all')
const searchQuery = ref('')

// StudioAdmin / DeptAdmin 卡片网格筛选
const studioFilter = ref<'all' | 'manage' | 'public' | 'locked'>('all')
const deptFilter = ref<'all' | 'can_enable' | 'available' | 'locked'>('all')

// DeptAdmin 一键开通 loading
const enablingGroupId = ref<number | null>(null)

// DeptAdmin 撤销开通 loading
const disablingGroupId = ref<number | null>(null)

// ─── 计算属性 ──────────────────────────────────────────────────────────────────

// 传给 GroupEntitlementDialog 的调用者角色，用于隐藏 studio Tab 和锁定部门范围
const actorRole = computed<'admin' | 'studio_admin' | 'dept_admin' | undefined>(() => {
  const r = authStore.effectiveRole
  if (r === 'admin' || r === 'studio_admin' || r === 'dept_admin') return r
  return undefined
})

const roleDescription = computed(() => {
  switch (authStore.effectiveRole) {
    case 'admin':
      return '管理各模型组的开放/受控模式，以及向工作室和部门授权。'
    case 'studio_admin':
      return '查看本工作室已获授权的模型组，管理下属部门开通。'
    case 'dept_admin':
      return '查看可用的模型组，并可开通本部门的访问权限。'
    default:
      return '模型组授权管理'
  }
})

// Admin: 按 open_mode + 搜索 筛选后的组
const filteredGroups = computed(() => {
  let list = groups.value
  if (openModeFilter.value !== 'all') {
    list = list.filter(g => g.open_mode === openModeFilter.value)
  }
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    list = list.filter(g => g.name.toLowerCase().includes(q) || (g.description || '').toLowerCase().includes(q))
  }
  return list
})

// Admin 概览统计
const publicCount = computed(() => groups.value.filter(g => g.open_mode === 'public').length)
const controlledCount = computed(() => groups.value.filter(g => g.open_mode === 'controlled').length)
const totalGrantedStudios = computed(() => {
  let n = 0
  for (const g of groups.value) {
    if (g.open_mode === 'controlled') n += getStudioCount(g.id)
  }
  return n
})

// 获取本工作室 ID（从 orgRoles 提取）
const myStudioId = computed(() => {
  const role = authStore.orgRoles.find(
    r => r.role === 'studio_admin' && r.scope_type === 'studio'
  )
  return role?.scope_id || ''
})

// 获取本部门 IDs（dept_admin 可能有多个部门）
const myDeptIds = computed(() => {
  return authStore.orgRoles
    .filter(r => r.role === 'dept_admin' && r.scope_type === 'dept')
    .map(r => r.scope_id)
})

// 当前选中的部门（默认第一个）
const selectedDeptId = ref('')

// 监听部门列表变化，自动选中第一个
watch(myDeptIds, (ids) => {
  if (ids.length > 0 && !ids.includes(selectedDeptId.value)) {
    selectedDeptId.value = ids[0]
  }
}, { immediate: true })

// 当前部门信息
const myDeptInfo = computed(() => {
  if (!selectedDeptId.value) return null
  return allDepts.value.find(d => d.dept_id === selectedDeptId.value) || null
})

// 本工作室所属部门 IDs
const myStudioDeptIds = computed(() => {
  if (!myStudioId.value) return new Set<string>()
  return new Set(
    allDepts.value
      .filter(d => d.studio_id === myStudioId.value)
      .map(d => d.dept_id)
  )
})

// 本工作室详情对象
const myStudioInfo = computed(() => {
  if (!myStudioId.value) return null
  return allStudios.value.find(s => s.id === myStudioId.value) || null
})

// 判断某组是否已授权给本工作室
function isStudioGranted(groupId: number): boolean {
  if (!myStudioId.value) return false
  const ents = entitlementsMap.value.get(groupId) || []
  return ents.some(
    e => e.scope_type === 'studio' && e.scope_id === myStudioId.value && e.enabled
  )
}

// 判断某组是否已开通给本部门
function isDeptEnabled(groupId: number): boolean {
  if (!selectedDeptId.value) return false
  const ents = entitlementsMap.value.get(groupId) || []
  return ents.some(
    e => e.scope_type === 'dept' && e.scope_id === selectedDeptId.value && e.enabled
  )
}

// ─── 统一卡片网格（studio_admin / dept_admin） ──────────────────────────────────

interface GridCard {
  group: GroupListItem
  cat: string          // 分类键，用于筛选
  accent: 'success' | 'warning' | 'muted'
  tagType: 'success' | 'warning' | 'info'
  tagText: string
  hint: string
  order: number        // 排序优先级（可操作的靠前）
}

// 本部门所属工作室是否已被授权（dept_admin 用 dept.studio_id 判断，而非 myStudioId）
function deptStudioGranted(groupId: number): boolean {
  const sid = myDeptInfo.value?.studio_id
  if (!sid) return false
  const ents = entitlementsMap.value.get(groupId) || []
  return ents.some(e => e.scope_type === 'studio' && e.scope_id === sid && e.enabled)
}

function studioCardOf(group: GroupListItem): GridCard {
  if (group.open_mode === 'public') {
    return { group, cat: 'public', accent: 'success', tagType: 'success', tagText: '全员可用', hint: '公开组，全公司自动可用', order: 1 }
  }
  if (isStudioGranted(group.id)) {
    const n = getMyStudioDeptCount(group.id)
    return { group, cat: 'manage', accent: 'warning', tagType: 'warning', tagText: '已授权', hint: n > 0 ? `${n} 个下属部门已开通` : '尚无部门开通，点击管理', order: 0 }
  }
  return { group, cat: 'locked', accent: 'muted', tagType: 'info', tagText: '未授权', hint: '本工作室未获授权', order: 2 }
}

function deptCardOf(group: GroupListItem): GridCard {
  if (group.open_mode === 'public') {
    return { group, cat: 'available', accent: 'success', tagType: 'success', tagText: '全员可用', hint: '公开组，全公司自动可用', order: 2 }
  }
  if (isDeptEnabled(group.id)) {
    return { group, cat: 'available', accent: 'success', tagType: 'success', tagText: '已开通', hint: '本部门已开通', order: 1 }
  }
  if (deptStudioGranted(group.id)) {
    return { group, cat: 'can_enable', accent: 'warning', tagType: 'warning', tagText: '可开通', hint: '工作室已授权，可一键开通本部门', order: 0 }
  }
  return { group, cat: 'locked', accent: 'muted', tagType: 'info', tagText: '未授权', hint: '所属工作室尚未获授权', order: 3 }
}

const studioGrid = computed<GridCard[]>(() => {
  return groups.value
    .filter(g => g.enabled)
    .map(studioCardOf)
    .filter(c => studioFilter.value === 'all' || c.cat === studioFilter.value)
    .sort((a, b) => a.order - b.order || a.group.name.localeCompare(b.group.name))
})

const deptGrid = computed<GridCard[]>(() => {
  if (!selectedDeptId.value) return []
  return groups.value
    .filter(g => g.enabled)
    .map(deptCardOf)
    .filter(c => deptFilter.value === 'all' || c.cat === deptFilter.value)
    .sort((a, b) => a.order - b.order || a.group.name.localeCompare(b.group.name))
})

// 各分类计数（用于筛选 tab 上的角标）
const studioCounts = computed(() => {
  const c = { all: 0, manage: 0, public: 0, locked: 0 }
  for (const g of groups.value.filter(x => x.enabled)) {
    c.all++
    c[studioCardOf(g).cat as 'manage' | 'public' | 'locked']++
  }
  return c
})

const deptCounts = computed(() => {
  const c = { all: 0, can_enable: 0, available: 0, locked: 0 }
  if (!selectedDeptId.value) return c
  for (const g of groups.value.filter(x => x.enabled)) {
    c.all++
    c[deptCardOf(g).cat as 'can_enable' | 'available' | 'locked']++
  }
  return c
})

// ─── 辅助函数 ──────────────────────────────────────────────────────────────────

function getStudioCount(groupId: number): number {
  const ents = entitlementsMap.value.get(groupId) || []
  return ents.filter(e => e.scope_type === 'studio' && e.enabled).length
}

function getDeptCount(groupId: number): number {
  const ents = entitlementsMap.value.get(groupId) || []
  return ents.filter(e => e.scope_type === 'dept' && e.enabled).length
}

function getMyStudioDeptCount(groupId: number): number {
  const ents = entitlementsMap.value.get(groupId) || []
  return ents.filter(
    e => e.scope_type === 'dept' && e.enabled && myStudioDeptIds.value.has(e.scope_id)
  ).length
}

// Admin 卡片范围摘要：公开组提示全员可用，受控组给出影响范围
function adminScopeHint(group: GroupListItem): string {
  if (group.open_mode === 'public') return '全公司所有成员均可使用'
  const studios = getStudioCount(group.id)
  if (studios === 0) return '尚未授权，点击卡片去配置'
  return `${studios} 工作室 · ${getDeptCount(group.id)} 部门 · 约 ${getAffectedMembers(group.id)} 人`
}

// 影响范围：开通部门的成员数之和（缺失 member_count 的部门按 0 计）
function getAffectedMembers(groupId: number): number {
  const ents = entitlementsMap.value.get(groupId) || []
  const deptIds = ents.filter(e => e.scope_type === 'dept' && e.enabled).map(e => e.scope_id)
  let total = 0
  for (const id of deptIds) {
    const dept = allDepts.value.find(d => d.dept_id === id)
    total += dept?.member_count || 0
  }
  return total
}

// ─── 数据加载 ──────────────────────────────────────────────────────────────────

async function loadData() {
  loading.value = true
  try {
    // 1. 并发请求：组列表 + 工作室 + 部门
    const [groupsRes, studiosRes, deptsRes] = await Promise.all([
      groupsApi.listGroups({ page: 1, page_size: 200 }),
      http.get('/api/v1/org/studios', { params: { page: 1, page_size: 100 } }),
      http.get('/api/v1/org/departments', { params: { page: 1, page_size: 500 } }),
    ])

    groups.value = groupsRes.data.data || []
    allStudios.value = studiosRes.data.studios || []
    allDepts.value = deptsRes.data.departments || []

    // 2. 对 enabled 组并发拉取授权记录
    const enabledGroups = groups.value.filter(g => g.enabled)
    const entitlementPromises = enabledGroups.map(async g => {
      try {
        const res = await groupsApi.listEntitlements(g.id)
        return { groupId: g.id, items: res.data.data?.items || [] }
      } catch {
        return { groupId: g.id, items: [] }
      }
    })

    const entResults = await Promise.all(entitlementPromises)
    const newMap = new Map<number, GroupEntitlement[]>()
    for (const r of entResults) {
      newMap.set(r.groupId, r.items)
    }
    entitlementsMap.value = newMap

    // 模型组聚合能力 / 价格（与模型广场同源）
    groupsApi.getGroupCapabilities()
      .then(r => { caps.value = r.data.data ?? {} })
      .catch(() => { /* 能力缺失不阻塞主流程 */ })
  } catch (e) {
    console.error('[EntitlementsPage] loadData error:', e)
    ElMessage.error('加载数据失败')
  } finally {
    loading.value = false
  }
}

// ─── 操作处理 ──────────────────────────────────────────────────────────────────

async function handleToggleOpenMode(group: GroupListItem) {
  const newMode: OpenMode = group.open_mode === 'public' ? 'controlled' : 'public'
  try {
    await groupsApi.setOpenMode(group.id, newMode)
    ElMessage.success(`已切换为「${newMode === 'public' ? '公开' : '受控'}」模式`)
    await loadData()
  } catch {
    ElMessage.error('切换失败')
  }
}

function openDrawer(group: GroupListItem) {
  activeGroupId.value = group.id
  activeGroupName.value = group.name
  drawerVisible.value = true
}

function onDrawerClosed() {
  // 刷新数据以更新统计
  loadData()
}

function onDeptChange() {
  // 切换部门后刷新数据
  loadData()
}

// DeptAdmin 一键开通
async function handleDeptAdminEnable(group: GroupListItem) {
  if (!selectedDeptId.value) {
    ElMessage.warning('请先选择部门')
    return
  }
  enablingGroupId.value = group.id
  try {
    await groupsApi.grantEntitlement(group.id, {
      scope_type: 'dept',
      scope_id: selectedDeptId.value,
    })
    ElMessage.success(`已为「${myDeptInfo.value?.dept_name || selectedDeptId.value}」开通`)
    await loadData()
  } catch (e: any) {
    const msg = e?.response?.data?.message || e?.message || '开通失败'
    // 解析后端错误信息，给出友好提示
    if (msg.includes('studio not granted') || msg.includes('ErrStudioNotGranted')) {
      ElMessage.error('该模型组尚未授权给本部门所属工作室，无法开通')
    } else if (msg.includes('forbidden') || msg.includes('ErrEntitlementForbidden')) {
      ElMessage.error('权限不足，无法开通该部门')
    } else if (msg.includes('duplicate') || msg.includes('ErrEntitlementExists')) {
      ElMessage.warning('该部门已开通此模型组')
    } else {
      ElMessage.error(msg)
    }
  } finally {
    enablingGroupId.value = null
  }
}

// DeptAdmin 撤销开通
async function handleDeptAdminDisable(group: GroupListItem) {
  if (!selectedDeptId.value) {
    ElMessage.warning('请先选择部门')
    return
  }
  
  // 查找本部门对该组的授权记录 ID
  const ents = entitlementsMap.value.get(group.id) || []
  const entitlement = ents.find(
    e => e.scope_type === 'dept' && e.scope_id === selectedDeptId.value && e.enabled
  )
  
  if (!entitlement) {
    ElMessage.error('未找到开通记录')
    return
  }
  
  disablingGroupId.value = group.id
  try {
    await groupsApi.revokeEntitlement(group.id, entitlement.id)
    ElMessage.success(`已撤销「${myDeptInfo.value?.dept_name || selectedDeptId.value}」的开通`)
    await loadData()
  } catch (e: any) {
    const msg = e?.response?.data?.message || e?.message || '撤销失败'
    ElMessage.error(msg)
  } finally {
    disablingGroupId.value = null
  }
}

// ─── 生命周期 ──────────────────────────────────────────────────────────────────

onMounted(() => {
  loadData()
})
</script>

<style scoped>
.entitlements-page {
  padding: 0;
  width: 100%;
}

.page-header {
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: 24px;
  font-weight: 700;
  margin: 0 0 6px;
  letter-spacing: -0.3px;
}

.page-desc {
  color: var(--el-text-color-secondary);
  font-size: 14px;
  margin: 0;
}

/* ===== 工具栏 + 紧凑统计 ===== */
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-inline {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.stat-inline__dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.stat-inline__dot--ctrl {
  background: var(--el-color-warning);
}

.admin-search {
  width: 240px;
}

/* ===== 影响范围 ===== */
.scope-public {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-color-success);
  background: var(--el-color-success-light-9);
  padding: 8px 12px;
  border-radius: 8px;
}

.scope-block {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.scope-line {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-regular);
}

.scope-line b {
  color: var(--el-text-color-primary);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.scope-line__icon {
  color: var(--el-color-warning);
  font-size: 15px;
}

.scope-line__sep {
  color: var(--el-text-color-placeholder);
}

.scope-chips {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  min-width: 0;
}

.scope-chip {
  max-width: 110px;
  overflow: hidden;
  flex-shrink: 1;
  min-width: 0;
}

.scope-chip :deep(.el-tag__content) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.scope-chip--more {
  font-weight: 600;
  flex-shrink: 0;
  max-width: none;
}

.scope-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.scope-empty__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-text-color-placeholder);
  flex-shrink: 0;
}

.content-area {
  min-height: 200px;
}

/* ===== 卡片网格 ===== */
.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}

.group-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  min-height: 158px;
  transition: box-shadow 0.2s, transform 0.15s;
}

.group-card:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.06);
  transform: translateY(-2px);
}

.group-card--controlled {
  border-left: 4px solid var(--el-color-warning);
}

/* 统一卡片状态色条（studio/dept 网格复用） */
.gc--accent-success {
  border-left: 4px solid var(--el-color-success);
}

.gc--accent-warning {
  border-left: 4px solid var(--el-color-warning);
}

.gc--accent-muted {
  border-left: 4px solid var(--el-border-color);
}

.gc--accent-muted .group-card__name {
  color: var(--el-text-color-regular);
}

.scope-empty__dot.dot--success {
  background: var(--el-color-success);
}

.scope-empty__dot.dot--warning {
  background: var(--el-color-warning);
}

.scope-empty__dot.dot--muted {
  background: var(--el-text-color-placeholder);
}

.footer-placeholder {
  display: inline-block;
  height: 24px;
}

/* GroupAuthCard 操作区：把右侧动作推到末尾 */
.footer-spacer {
  flex: 1;
}

.group-card--clickable {
  cursor: pointer;
}

.group-card--clickable:hover {
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.1);
  border-color: var(--el-color-primary-light-5);
}

.group-card--clickable:hover .detail-hint {
  color: var(--el-color-primary);
  opacity: 1;
}

.detail-hint {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  opacity: 0.7;
  transition: color 0.15s, opacity 0.15s;
}

.group-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.group-card__name {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-right: 8px;
}

.group-card__body {
  flex: 1;
  margin-bottom: 12px;
}

.group-card__desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
  margin: 0 0 8px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.group-card__stats {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.stats-separator {
  margin: 0 6px;
}

.stats-public-hint {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--el-color-success-light-8);
  color: var(--el-color-success);
  font-weight: 500;
}

.group-card__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 12px;
  margin-top: auto;
}

/* ===== DeptAdmin 部门信息卡 ===== */
.dept-info-card {
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 12px 16px;
  margin-bottom: 20px;
  font-size: 14px;
}

.dept-info-label {
  color: var(--el-text-color-secondary);
}

.dept-info-name {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.dept-info-studio {
  color: var(--el-text-color-secondary);
  margin-left: 4px;
}

/* ===== DeptAdmin 双列布局 ===== */
.dual-column-layout {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24px;
}

/* ===== 三列布局（全网关 / 管辖范围 / 已可用） ===== */
/* 只读参考列收窄、降权，让真正可操作的两列聚焦 */
.triple-column-layout {
  display: grid;
  grid-template-columns: 0.78fr 1.1fr 1.1fr;
  gap: 20px;
}

@media (max-width: 1024px) {
  .triple-column-layout {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .dual-column-layout {
    grid-template-columns: 1fr;
  }
}

.column {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  padding: 16px;
}

.column-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0 0 16px;
}

.column-title-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.column-title-dot--success {
  background: var(--el-color-success);
}

.column-title-dot--warning {
  background: var(--el-color-warning);
}

.column-cards {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.mini-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 10px 12px;
  transition: background 0.15s;
}

.mini-card:hover {
  background: var(--el-fill-color-light);
}

.mini-card--available {
  border-left: 3px solid var(--el-color-success);
}

.mini-card--can-enable {
  border-left: 3px solid var(--el-color-warning);
}

.mini-card--muted {
  opacity: 0.55;
  border-left: 3px solid var(--el-border-color-lighter);
}

.mini-card__name {
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-right: 12px;
}

.mini-card__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.mini-card__info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
  margin-right: 12px;
}

.mini-card__hint {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

/* ===== 三列布局扩展样式 ===== */
.column--readonly {
  background: var(--el-fill-color-lighter);
}

.column-subtitle {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin: -12px 0 16px;
  line-height: 1;
}

.column-title-dot--muted {
  background: var(--el-border-color);
}

.dept-info-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
}

/* ===== 响应式 ===== */
@media (max-width: 900px) {
  .page-header {
    margin-bottom: 16px;
  }

  .page-title {
    font-size: 20px;
  }

  .card-grid {
    grid-template-columns: 1fr;
  }

  .toolbar {
    flex-direction: column;
    align-items: stretch;
  }

  .toolbar-right {
    justify-content: space-between;
  }

  .admin-search {
    width: 100%;
  }
}

@media (max-width: 480px) {
  .page-title {
    font-size: 18px;
  }

  .group-card {
    padding: 14px;
    border-radius: 10px;
  }

  .group-card__name {
    font-size: 14px;
  }
}
</style>
