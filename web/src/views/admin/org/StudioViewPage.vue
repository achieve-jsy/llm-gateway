<template>
  <div class="studio-view-page">

    <div class="page-header">
      <div class="header-left">
        <h2 class="page-title">我的工作室</h2>
        <span v-if="myStudio" class="studio-badge">{{ myStudio.name }}</span>
      </div>
    </div>

    <!-- 未加载 / 无工作室 -->
    <div v-if="!loading && !myStudio" class="empty-state">
      <el-icon size="48" color="#c0c4cc"><OfficeBuilding /></el-icon>
      <p class="empty-title">暂未分配工作室</p>
      <p class="empty-sub">请联系平台管理员为您设置工作室负责人权限</p>
    </div>

    <template v-else-if="myStudio">
      <!-- 工作室概览 -->
      <el-row :gutter="12" style="margin-bottom: 20px">
        <el-col :span="8">
          <div class="stat-card">
            <div class="stat-num">{{ depts.length }}</div>
            <div class="stat-label">部门数量</div>
          </div>
        </el-col>
        <el-col :span="8">
          <div class="stat-card">
            <div class="stat-num">{{ depts.filter(d => d.status === 'active').length }}</div>
            <div class="stat-label">已就绪部门</div>
          </div>
        </el-col>
        <el-col :span="8">
          <div class="stat-card warn">
            <div class="stat-num">{{ depts.filter(d => d.status === 'pending_studio').length }}</div>
            <div class="stat-label">待映射部门</div>
          </div>
        </el-col>
      </el-row>

      <!-- 部门列表 + 为部门指定负责人 -->
      <el-card shadow="never" v-loading="loading">
        <template #header>
          <div style="display:flex; justify-content:space-between; align-items:center">
            <span>部门列表</span>
            <el-text type="info" size="small">点击「指定负责人」为各部门设置管理员</el-text>
          </div>
        </template>

        <el-table :data="depts" stripe>
          <!-- 部门名称 -->
          <el-table-column label="部门" min-width="160">
            <template #default="{ row }">
              <div class="dept-cell">
                <el-icon class="dept-icon"><Folder /></el-icon>
                <span class="dept-name">{{ row.dept_name }}</span>
              </div>
            </template>
          </el-table-column>

          <!-- 状态 -->
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag :type="row.status === 'active' ? 'success' : 'warning'" size="small">
                {{ row.status === 'active' ? '正常' : '待映射' }}
              </el-tag>
            </template>
          </el-table-column>

          <!-- 当前负责人 -->
          <el-table-column label="当前负责人" min-width="160">
            <template #default="{ row }">
              <div v-if="deptAdminMap[row.dept_id]" class="admin-cell">
                <div class="mini-avatar">{{ deptAdminMap[row.dept_id].user_name?.charAt(0) }}</div>
                <div>
                  <div class="admin-name">{{ deptAdminMap[row.dept_id].user_name }}</div>
                  <div class="admin-email">{{ deptAdminMap[row.dept_id].email }}</div>
                </div>
              </div>
              <span v-else class="no-admin">未设置</span>
            </template>
          </el-table-column>

          <!-- 操作 -->
          <el-table-column label="操作" width="160" align="center">
            <template #default="{ row }">
              <el-button
                link type="primary" size="small"
                @click="openAssignDialog(row)"
              >
                {{ deptAdminMap[row.dept_id] ? '更换负责人' : '指定负责人' }}
              </el-button>
              <el-divider direction="vertical" v-if="deptAdminMap[row.dept_id]" />
              <el-popconfirm
                v-if="deptAdminMap[row.dept_id]"
                :title="`确认卸任「${deptAdminMap[row.dept_id]?.user_name}」？`"
                confirm-button-text="确认" confirm-button-type="danger"
                @confirm="revokeAdmin(row)"
              >
                <template #reference>
                  <el-button link type="danger" size="small">卸任</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>

        <div v-if="!loading && depts.length === 0" class="empty-tip">
          该工作室暂无部门
        </div>
      </el-card>
    </template>

    <!-- 指定部门负责人弹窗 -->
    <el-dialog
      v-model="assignDialogVisible"
      :title="`为「${assignTarget?.dept_name}」指定负责人`"
      width="440px"
      @close="resetAssign"
    >
      <div class="step-label">搜索并选择员工</div>
      <el-input
        v-model="searchKeyword"
        placeholder="输入姓名或邮箱..."
        clearable
        @input="onSearchInput"
        style="margin-bottom: 8px"
      />
      <div v-if="searchLoading" class="search-hint">搜索中...</div>
      <div v-else-if="selectedUser" class="selected-user" style="margin-top:8px">
        <div class="selected-avatar">{{ selectedUser.name.charAt(0) }}</div>
        <div class="selected-info">
          <span class="selected-name">{{ selectedUser.name }}</span>
          <span class="selected-meta">{{ selectedUser.email }}</span>
        </div>
        <el-button link size="small" @click="selectedUser = null; searchKeyword = ''">更换</el-button>
      </div>
      <div v-else-if="searchResults.length > 0" class="search-results">
        <div
          v-for="u in searchResults" :key="u.id"
          class="search-result-item"
          @click="selectedUser = u; searchResults = []"
        >
          <div class="result-avatar">{{ u.name.charAt(0) }}</div>
          <div class="result-info">
            <span class="result-name">{{ u.name }}</span>
            <span class="result-meta">{{ u.email }}<span v-if="u.dept_name"> · {{ u.dept_name }}</span></span>
          </div>
        </div>
      </div>
      <div v-else-if="searchKeyword && !searchLoading" class="search-hint">未找到员工</div>

      <template #footer>
        <el-button @click="assignDialogVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!selectedUser" :loading="assigning" @click="doAssign">
          确认指定
        </el-button>
      </template>
    </el-dialog>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { OfficeBuilding, Folder } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useOrgStore } from '@/stores/org'
import type { OrgDepartment } from '@/types/org'
import type { RoleWithUser, UserSearchResult } from '@/stores/org'

const authStore = useAuthStore()
const orgStore = useOrgStore()

const loading = ref(false)
const depts = ref<OrgDepartment[]>([])
// 只保存本工作室相关的角色（所辖部门负责人）
const myStudioScopeId = ref<string | null>(null)
const deptRoles = ref<RoleWithUser[]>([])

// 当前用户管辖的工作室
const myStudio = computed(() => {
  if (!myStudioScopeId.value) return null
  return orgStore.studios.find(s => s.id === myStudioScopeId.value) || null
})

// dept_id → 该部门的负责人 role record
const deptAdminMap = computed(() => {
  const map: Record<string, RoleWithUser> = {}
  deptRoles.value
    .filter(r => r.role === 'dept_admin')
    .forEach(r => { map[r.scope_id] = r })
  return map
})

// ─── 指定部门负责人 ───────────────────────────────────────────────────────────
const assignDialogVisible = ref(false)
const assignTarget = ref<OrgDepartment | null>(null)
const assigning = ref(false)
const searchKeyword = ref('')
const searchResults = ref<UserSearchResult[]>([])
const searchLoading = ref(false)
const selectedUser = ref<UserSearchResult | null>(null)

function openAssignDialog(dept: OrgDepartment) {
  assignTarget.value = dept
  assignDialogVisible.value = true
}

function resetAssign() {
  assignTarget.value = null
  searchKeyword.value = ''
  searchResults.value = []
  selectedUser.value = null
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  if (!searchKeyword.value.trim()) { searchResults.value = []; return }
  searchTimer = setTimeout(async () => {
    searchLoading.value = true
    try { searchResults.value = await orgStore.searchUsers(searchKeyword.value) }
    finally { searchLoading.value = false }
  }, 300)
}

async function doAssign() {
  if (!selectedUser.value || !assignTarget.value) return
  assigning.value = true
  try {
    // 如有旧负责人先卸任
    const oldAdmin = deptAdminMap.value[assignTarget.value.dept_id]
    if (oldAdmin) {
      await orgStore.revokeRole(oldAdmin.user_id, 'dept_admin', assignTarget.value.dept_id)
    }
    await orgStore.grantRole(selectedUser.value.id, 'dept_admin', 'dept', assignTarget.value.dept_id)
    ElMessage.success(`已将「${selectedUser.value.name}」设为 ${assignTarget.value.dept_name} 负责人`)
    assignDialogVisible.value = false
    await loadData()
  } catch (e: any) {
    // 错误已由 http 拦截器处理
  } finally {
    assigning.value = false
  }
}

async function revokeAdmin(dept: OrgDepartment) {
  const admin = deptAdminMap.value[dept.dept_id]
  if (!admin) return
  try {
    await orgStore.revokeRole(admin.user_id, 'dept_admin', dept.dept_id)
    ElMessage.success('已卸任')
    await loadData()
  } catch {
    // 错误已由 http 拦截器处理
  }
}

// ─── 数据加载 ─────────────────────────────────────────────────────────────────
async function loadData() {
  loading.value = true
  try {
    const uid = authStore.user?.id
    if (!uid) return

    // 仅拉取当前用户自己的角色，避免全量加载
    const [myRolesData] = await Promise.all([
      orgStore.getUserRoles(uid),
      orgStore.fetchStudios(),
      orgStore.fetchDepartments(),
    ])

    // 从当前用户自己的角色中找 studio_admin scope_id
    const myRoles: Array<{ role: string; scope_id: string }> = myRolesData?.roles || []
    const studioAdminRole = myRoles.find(r => r.role === 'studio_admin')
    myStudioScopeId.value = studioAdminRole?.scope_id || null

    if (myStudioScopeId.value) {
      depts.value = orgStore.departments.filter(d => d.studio_id === myStudioScopeId.value)
      // 只加载本工作室所属部门的负责人
      const allRolesData = await orgStore.fetchAllRoles()
      const deptIdSet = new Set(depts.value.map(d => d.dept_id))
      deptRoles.value = allRolesData.filter(
        (r: RoleWithUser) => r.role === 'dept_admin' && deptIdSet.has(r.scope_id)
      )
    } else {
      depts.value = []
      deptRoles.value = []
    }
  } finally {
    loading.value = false
  }
}

onMounted(loadData)
</script>

<style scoped>
.studio-view-page { padding: 0; }

.page-header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 20px; flex-wrap: wrap; gap: 10px;
}
.header-left { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.page-title { margin: 0; font-size: 20px; font-weight: 700; }
.studio-badge {
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  padding: 3px 10px; border-radius: 12px; font-size: 13px; font-weight: 500;
  white-space: nowrap;
}

/* 统计 */
.stat-card {
  padding: 16px; border-radius: 8px; text-align: center;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
}
.stat-card.warn { background: #fffbe6; border-color: #ffe58f; }
.stat-num { font-size: 28px; font-weight: 700; }
.stat-label { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }

/* 表格 */
.dept-cell { display: flex; align-items: center; gap: 6px; }
.dept-icon { color: var(--el-text-color-placeholder); flex-shrink: 0; }
.dept-name { font-size: 14px; }
.admin-cell { display: flex; align-items: center; gap: 8px; min-width: 0; }
.mini-avatar {
  width: 28px; height: 28px; border-radius: 50%;
  background: var(--el-color-primary-light-7); color: var(--el-color-primary);
  display: flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 600; flex-shrink: 0;
}
.admin-name { font-size: 13px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.admin-email { font-size: 12px; color: var(--el-text-color-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.no-admin { color: var(--el-text-color-placeholder); font-size: 13px; }
.empty-tip { text-align: center; padding: 24px; color: var(--el-text-color-placeholder); }

/* 空态 */
.empty-state {
  display: flex; flex-direction: column; align-items: center;
  gap: 8px; padding: 60px 16px; text-align: center;
}
.empty-title { font-size: 15px; font-weight: 600; color: var(--el-text-color-secondary); margin: 0; }
.empty-sub { color: var(--el-text-color-placeholder); font-size: 13px; margin: 0; }

/* 弹窗 */
.step-label { font-size: 13px; font-weight: 600; color: var(--el-text-color-secondary); margin-bottom: 8px; }
.search-hint { font-size: 13px; color: var(--el-text-color-placeholder); text-align: center; padding: 12px 0; }
.search-results {
  border: 1px solid var(--el-border-color); border-radius: 6px;
  max-height: 200px; overflow-y: auto;
}
.search-result-item {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; cursor: pointer;
  border-bottom: 1px solid var(--el-border-color-extra-light);
  transition: background .15s;
}
.search-result-item:last-child { border-bottom: none; }
.search-result-item:hover { background: var(--el-fill-color-light); }
.result-avatar {
  width: 32px; height: 32px; border-radius: 50%;
  background: var(--el-color-primary-light-8); color: var(--el-color-primary);
  display: flex; align-items: center; justify-content: center;
  font-size: 13px; font-weight: 600; flex-shrink: 0;
}
.result-info { display: flex; flex-direction: column; flex: 1; min-width: 0; }
.result-name { font-size: 14px; font-weight: 500; }
.result-meta { font-size: 12px; color: var(--el-text-color-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.selected-user {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; background: var(--el-fill-color-light); border-radius: 6px;
}
.selected-avatar {
  width: 36px; height: 36px; border-radius: 50%;
  background: var(--el-color-primary-light-5); color: var(--el-color-primary-dark-2);
  display: flex; align-items: center; justify-content: center;
  font-size: 15px; font-weight: 600; flex-shrink: 0;
}
.selected-info { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.selected-name { font-size: 14px; font-weight: 600; }
.selected-meta { font-size: 12px; color: var(--el-text-color-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ─── 响应式 ─────────────────────────────────────────────────── */
@media (max-width: 900px) {
  .page-header { flex-direction: column; align-items: flex-start; }
  .stat-num { font-size: 22px; }
}
</style>
