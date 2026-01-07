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
      <!-- 筛选工具栏 -->
      <div class="toolbar">
        <el-radio-group v-model="openModeFilter" size="default">
          <el-radio-button value="all">全部</el-radio-button>
          <el-radio-button value="public">公开</el-radio-button>
          <el-radio-button value="controlled">受控</el-radio-button>
        </el-radio-group>
      </div>

      <div v-loading="loading" class="content-area">
        <!-- 卡片网格 -->
        <div v-if="filteredGroups.length" class="card-grid">
          <div
            v-for="group in filteredGroups"
            :key="group.id"
            class="group-card"
            :class="{ 'group-card--controlled': group.open_mode === 'controlled' }"
          >
            <div class="group-card__header">
              <span class="group-card__name">{{ group.name }}</span>
              <el-tag
                :type="group.open_mode === 'public' ? 'success' : 'warning'"
                size="small"
              >
                {{ group.open_mode === 'public' ? '公开' : '受控' }}
              </el-tag>
            </div>
            <div class="group-card__body">
              <p class="group-card__desc">{{ group.description || '暂无描述' }}</p>
              <div class="group-card__stats">
                <span v-if="group.open_mode === 'public'" class="stats-public-hint">全员可用</span>
                <template v-else>
                  <span>已授权 {{ getStudioCount(group.id) }} 工作室</span>
                  <span class="stats-separator">·</span>
                  <span>{{ getDeptCount(group.id) }} 部门开通</span>
                </template>
              </div>
            </div>
            <div class="group-card__footer">
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
              <el-button
                v-if="group.open_mode === 'controlled'"
                type="primary"
                text
                size="small"
                @click="openDrawer(group)"
              >
                管理授权 →
              </el-button>
            </div>
          </div>
        </div>

        <el-empty v-else-if="!loading" description="暂无模型组" :image-size="80" />
      </div>
    </template>

    <!-- ========== StudioAdmin 视图 ========== -->
    <template v-else-if="authStore.effectiveRole === 'studio_admin'">
      <div v-loading="loading" class="content-area">
        <div v-if="studioAdminGroups.length" class="card-grid">
          <div
            v-for="group in studioAdminGroups"
            :key="group.id"
            class="group-card group-card--controlled"
          >
            <div class="group-card__header">
              <span class="group-card__name">{{ group.name }}</span>
              <el-tag type="warning" size="small">受控</el-tag>
            </div>
            <div class="group-card__body">
              <p class="group-card__desc">{{ group.description || '暂无描述' }}</p>
              <div class="group-card__stats">
                <span class="stats-public-hint">本工作室已授权</span>
                <span class="stats-separator">·</span>
                <span>{{ getMyStudioDeptCount(group.id) }} 部门开通</span>
              </div>
            </div>
            <div class="group-card__footer">
              <el-button
                type="primary"
                text
                size="small"
                @click="openDrawer(group)"
              >
                管理部门开通 →
              </el-button>
            </div>
          </div>
        </div>

        <el-empty v-else-if="!loading" description="暂无已授权的模型组" :image-size="80" />
      </div>
    </template>

    <!-- ========== DeptAdmin 视图 ========== -->
    <template v-else-if="authStore.effectiveRole === 'dept_admin'">
      <!-- 部门信息卡 -->
      <div v-if="myDeptInfo" class="dept-info-card">
        <span class="dept-info-label">我的部门：</span>
        <span class="dept-info-name">{{ myDeptInfo.dept_name }}</span>
        <span v-if="myDeptInfo.studio_name" class="dept-info-studio">
          （所属工作室：{{ myDeptInfo.studio_name }}）
        </span>
      </div>

      <div v-loading="loading" class="content-area">
        <div class="dual-column-layout">
          <!-- 左列：已可用 -->
          <div class="column">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--success"></span>
              已可用
            </h3>
            <div v-if="deptAdminAvailable.length" class="column-cards">
              <div
                v-for="group in deptAdminAvailable"
                :key="group.id"
                class="mini-card mini-card--available"
              >
                <span class="mini-card__name">{{ group.name }}</span>
                <el-tag
                  :type="group.open_mode === 'public' ? 'success' : 'warning'"
                  size="small"
                >
                  {{ group.open_mode === 'public' ? '全员可用' : '已开通' }}
                </el-tag>
              </div>
            </div>
            <el-empty v-else description="暂无可用组" :image-size="48" />
          </div>

          <!-- 右列：可开通 -->
          <div class="column">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--warning"></span>
              可开通
            </h3>
            <div v-if="deptAdminCanEnable.length" class="column-cards">
              <div
                v-for="group in deptAdminCanEnable"
                :key="group.id"
                class="mini-card mini-card--can-enable"
              >
                <span class="mini-card__name">{{ group.name }}</span>
                <el-button
                  type="primary"
                  size="small"
                  :loading="enablingGroupId === group.id"
                  @click="handleDeptAdminEnable(group)"
                >
                  一键开通
                </el-button>
              </div>
            </div>
            <el-empty v-else description="暂无可开通组" :image-size="48" />
          </div>
        </div>
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
      :restrict-dept-id="myDeptId"
      @closed="onDrawerClosed"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { groupsApi } from '@/api/groups'
import type { GroupListItem, GroupEntitlement, OpenMode } from '@/types/group'
import type { Studio, OrgDepartment } from '@/types/org'
import http from '@/services/http'
import GroupEntitlementDialog from '@/components/GroupEntitlementDialog.vue'

const authStore = useAuthStore()

// ─── 状态 ──────────────────────────────────────────────────────────────────────
const loading = ref(false)
const groups = ref<GroupListItem[]>([])
const entitlementsMap = ref<Map<number, GroupEntitlement[]>>(new Map())
const allStudios = ref<Studio[]>([])
const allDepts = ref<OrgDepartment[]>([])

// Drawer 状态
const drawerVisible = ref(false)
const activeGroupId = ref<number | null>(null)
const activeGroupName = ref('')

// Admin 筛选
const openModeFilter = ref<'all' | 'public' | 'controlled'>('all')

// DeptAdmin 一键开通 loading
const enablingGroupId = ref<number | null>(null)

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

// Admin: 按 open_mode 筛选后的组
const filteredGroups = computed(() => {
  if (openModeFilter.value === 'all') return groups.value
  return groups.value.filter(g => g.open_mode === openModeFilter.value)
})

// 获取本工作室 ID（从 orgRoles 提取）
const myStudioId = computed(() => {
  const role = authStore.orgRoles.find(
    r => r.role === 'studio_admin' && r.scope_type === 'studio'
  )
  return role?.scope_id || ''
})

// 获取本部门 ID（从 orgRoles 提取）
const myDeptId = computed(() => {
  const role = authStore.orgRoles.find(
    r => r.role === 'dept_admin' && r.scope_type === 'dept'
  )
  return role?.scope_id || ''
})

// 本部门信息
const myDeptInfo = computed(() => {
  if (!myDeptId.value) return null
  return allDepts.value.find(d => d.dept_id === myDeptId.value) || null
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

// StudioAdmin: 只显示已授权给本工作室的 controlled 组
const studioAdminGroups = computed(() => {
  if (!myStudioId.value) return []
  return groups.value.filter(g => {
    if (g.open_mode !== 'controlled') return false
    const ents = entitlementsMap.value.get(g.id) || []
    return ents.some(
      e => e.scope_type === 'studio' && e.scope_id === myStudioId.value && e.enabled
    )
  })
})

// DeptAdmin: 已可用 = public 组 + 本部门已开通的 controlled 组
const deptAdminAvailable = computed(() => {
  if (!myDeptId.value) return []
  return groups.value.filter(g => {
    if (g.open_mode === 'public') return true
    const ents = entitlementsMap.value.get(g.id) || []
    return ents.some(
      e => e.scope_type === 'dept' && e.scope_id === myDeptId.value && e.enabled
    )
  })
})

// DeptAdmin: 可开通 = 本工作室已获授权但本部门未开通的 controlled 组
const deptAdminCanEnable = computed(() => {
  if (!myDeptId.value || !myDeptInfo.value?.studio_id) return []
  const studioId = myDeptInfo.value.studio_id
  return groups.value.filter(g => {
    if (g.open_mode !== 'controlled') return false
    const ents = entitlementsMap.value.get(g.id) || []
    // 本工作室已获授权
    const studioGranted = ents.some(
      e => e.scope_type === 'studio' && e.scope_id === studioId && e.enabled
    )
    if (!studioGranted) return false
    // 但本部门未开通
    const deptEnabled = ents.some(
      e => e.scope_type === 'dept' && e.scope_id === myDeptId.value && e.enabled
    )
    return !deptEnabled
  })
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

    groups.value = groupsRes.data.items || []
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

// DeptAdmin 一键开通
async function handleDeptAdminEnable(group: GroupListItem) {
  if (!myDeptId.value) {
    ElMessage.warning('无法获取您的部门信息')
    return
  }
  enablingGroupId.value = group.id
  try {
    await groupsApi.grantEntitlement(group.id, {
      scope_type: 'dept',
      scope_id: myDeptId.value,
    })
    ElMessage.success('已开通')
    await loadData()
  } catch {
    ElMessage.error('开通失败')
  } finally {
    enablingGroupId.value = null
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

/* ===== 工具栏 ===== */
.toolbar {
  margin-bottom: 16px;
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
  transition: box-shadow 0.2s, transform 0.15s;
}

.group-card:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.06);
  transform: translateY(-2px);
}

.group-card--controlled {
  border-left: 4px solid var(--el-color-warning);
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
