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
      <!-- 工作室信息卡 -->
      <div v-if="myStudioInfo" class="dept-info-card">
        <div class="dept-info-row">
          <span class="dept-info-label">我的工作室：</span>
          <span class="dept-info-name">{{ myStudioInfo.name }}</span>
        </div>
      </div>

      <div v-loading="loading" class="content-area">
        <div class="triple-column-layout">
          <!-- 列1：全网关受控模型 -->
          <div class="column column--readonly">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--muted"></span>
              全网关受控模型
            </h3>
            <p class="column-subtitle">系统全部受控组，标记您的权限范围</p>
            <div v-if="studioAllControlled.length" class="column-cards">
              <div
                v-for="group in studioAllControlled"
                :key="group.id"
                class="mini-card"
                :class="{
                  'mini-card--available': isStudioGranted(group.id),
                  'mini-card--muted': !isStudioGranted(group.id),
                }"
              >
                <span class="mini-card__name">{{ group.name }}</span>
                <el-tag v-if="isStudioGranted(group.id)" type="success" size="small" effect="light">
                  已授权
                </el-tag>
                <el-tag v-else type="info" size="small" effect="plain">
                  未授权
                </el-tag>
              </div>
            </div>
            <el-empty v-else description="暂无受控模型" :image-size="48" />
          </div>

          <!-- 列2：本工作室已授权 + 管理入口 -->
          <div class="column">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--warning"></span>
              本工作室已授权
            </h3>
            <p class="column-subtitle">可为下属部门开通</p>
            <div v-if="studioAdminGroups.length" class="column-cards">
              <div
                v-for="group in studioAdminGroups"
                :key="group.id"
                class="mini-card mini-card--can-enable"
              >
                <div class="mini-card__info">
                  <span class="mini-card__name">{{ group.name }}</span>
                  <span class="mini-card__hint">{{ getMyStudioDeptCount(group.id) }} 部门已开通</span>
                </div>
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
            <el-empty v-else description="暂无已授权模型组" :image-size="48" />
          </div>

          <!-- 列3：全员可用（公开组） -->
          <div class="column">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--success"></span>
              全员可用
            </h3>
            <p class="column-subtitle">公开模型组，全公司自动可用</p>
            <div v-if="studioPublicGroups.length" class="column-cards">
              <div
                v-for="group in studioPublicGroups"
                :key="group.id"
                class="mini-card mini-card--available"
              >
                <span class="mini-card__name">{{ group.name }}</span>
                <el-tag type="success" size="small">公开</el-tag>
              </div>
            </div>
            <el-empty v-else description="暂无公开模型组" :image-size="48" />
          </div>
        </div>
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

      <div v-loading="loading" class="content-area">
        <div class="triple-column-layout">
          <!-- 列1：全网关受控模型 -->
          <div class="column column--readonly">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--muted"></span>
              全网关受控模型
            </h3>
            <p class="column-subtitle">系统全部受控组，标记您的权限范围</p>
            <div v-if="deptAllControlled.length" class="column-cards">
              <div
                v-for="group in deptAllControlled"
                :key="group.id"
                class="mini-card"
                :class="{
                  'mini-card--available': isDeptEnabled(group.id),
                  'mini-card--can-enable': isStudioGranted(group.id) && !isDeptEnabled(group.id),
                  'mini-card--muted': !isStudioGranted(group.id),
                }"
              >
                <span class="mini-card__name">{{ group.name }}</span>
                <el-tag v-if="isDeptEnabled(group.id)" type="success" size="small" effect="light">
                  已开通
                </el-tag>
                <el-tag v-else-if="isStudioGranted(group.id)" type="warning" size="small" effect="plain">
                  工作室已授权
                </el-tag>
                <el-tag v-else type="info" size="small" effect="plain">
                  未授权
                </el-tag>
              </div>
            </div>
            <el-empty v-else description="暂无受控模型" :image-size="48" />
          </div>

          <!-- 列2：可开通（工作室已授权但本部门未开通） -->
          <div class="column">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--warning"></span>
              可开通
            </h3>
            <p class="column-subtitle">工作室已授权，一键开通本部门</p>
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
            <el-empty v-else description="暂无可开通模型组" :image-size="48" />
          </div>

          <!-- 列3：已可用（本部门已开通的 controlled 组 + 公开组） -->
          <div class="column">
            <h3 class="column-title">
              <span class="column-title-dot column-title-dot--success"></span>
              已可用
            </h3>
            <p class="column-subtitle">本部门可用的模型组</p>
            <div v-if="deptAdminAvailable.length" class="column-cards">
              <div
                v-for="group in deptAdminAvailable"
                :key="group.id"
                class="mini-card mini-card--available"
              >
                <span class="mini-card__name">{{ group.name }}</span>
                <div class="mini-card__actions">
                  <el-tag
                    :type="group.open_mode === 'public' ? 'success' : 'warning'"
                    size="small"
                  >
                    {{ group.open_mode === 'public' ? '全员可用' : '已开通' }}
                  </el-tag>
                  <el-popconfirm
                    v-if="group.open_mode === 'controlled'"
                    title="确定撤销本部门的开通？撤销后本部门将无法使用该模型组。"
                    confirm-button-text="确定撤销"
                    confirm-button-type="danger"
                    @confirm="handleDeptAdminDisable(group)"
                  >
                    <template #reference>
                      <el-button
                        type="danger"
                        text
                        size="small"
                        :loading="disablingGroupId === group.id"
                      >
                        撤销
                      </el-button>
                    </template>
                  </el-popconfirm>
                </div>
              </div>
            </div>
            <el-empty v-else description="暂无可用模型组" :image-size="48" />
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
      :restrict-dept-id="selectedDeptId"
      @closed="onDrawerClosed"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
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

// 全网关受控模型列表
const studioAllControlled = computed(() => {
  return groups.value.filter(g => g.open_mode === 'controlled' && g.enabled)
})

const deptAllControlled = computed(() => {
  return groups.value.filter(g => g.open_mode === 'controlled' && g.enabled)
})

// 全网关公开模型列表
const studioPublicGroups = computed(() => {
  return groups.value.filter(g => g.open_mode === 'public' && g.enabled)
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
  if (!selectedDeptId.value) return []
  return groups.value.filter(g => {
    if (g.open_mode === 'public') return true
    const ents = entitlementsMap.value.get(g.id) || []
    return ents.some(
      e => e.scope_type === 'dept' && e.scope_id === selectedDeptId.value && e.enabled
    )
  })
})

// DeptAdmin: 可开通 = 本工作室已获授权但本部门未开通的 controlled 组
const deptAdminCanEnable = computed(() => {
  if (!selectedDeptId.value || !myDeptInfo.value?.studio_id) return []
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
      e => e.scope_type === 'dept' && e.scope_id === selectedDeptId.value && e.enabled
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

/* ===== 三列布局（全网关 / 管辖范围 / 已可用） ===== */
.triple-column-layout {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
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
