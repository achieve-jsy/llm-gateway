<template>
  <div class="role-page">

    <div class="page-header">
      <div class="header-left">
        <h2 class="page-title">角色管理</h2>
        <span class="page-sub">管理各级负责人的授权与卸任</span>
      </div>
      <el-button type="primary" size="small" @click="openGrantDialog">
        <el-icon><Plus /></el-icon> 指定负责人
      </el-button>
    </div>

    <!-- 角色概览卡片 -->
    <el-row :gutter="12" style="margin-bottom: 20px">
      <el-col :span="8">
        <div class="role-stat-card admin">
          <div class="stat-icon"><el-icon size="22"><UserFilled /></el-icon></div>
          <div class="stat-body">
            <div class="stat-num">{{ adminCount }}</div>
            <div class="stat-label">平台管理员</div>
          </div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="role-stat-card studio">
          <div class="stat-icon"><el-icon size="22"><OfficeBuilding /></el-icon></div>
          <div class="stat-body">
            <div class="stat-num">{{ studioAdminCount }}</div>
            <div class="stat-label">工作室负责人</div>
          </div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="role-stat-card dept">
          <div class="stat-icon"><el-icon size="22"><Folder /></el-icon></div>
          <div class="stat-body">
            <div class="stat-num">{{ deptAdminCount }}</div>
            <div class="stat-label">部门负责人</div>
          </div>
        </div>
      </el-col>
    </el-row>

    <!-- 角色列表 -->
    <el-card shadow="never" v-loading="loading">
      <template #header>
        <div style="display:flex; justify-content:space-between; align-items:center">
          <span>当前授权列表</span>
          <el-radio-group v-model="filterRole" size="small" @change="applyFilter">
            <el-radio-button value="">全部</el-radio-button>
            <el-radio-button value="admin">管理员</el-radio-button>
            <el-radio-button value="studio_admin">工作室负责人</el-radio-button>
            <el-radio-button value="dept_admin">部门负责人</el-radio-button>
          </el-radio-group>
        </div>
      </template>

      <el-table :data="filteredRoles" stripe>
        <!-- 人员信息 -->
        <el-table-column label="人员" min-width="180">
          <template #default="{ row }">
            <div class="user-cell">
              <div class="user-avatar">{{ row.user_name?.charAt(0) || '?' }}</div>
              <div class="user-info">
                <span class="user-name">{{ row.user_name || row.user_id }}</span>
                <span class="user-email">{{ row.email }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <!-- 所在部门 -->
        <el-table-column label="所在部门" width="120">
          <template #default="{ row }">
            <span v-if="row.dept_name" class="dept-tag">{{ row.dept_name }}</span>
            <span v-else class="text-muted">—</span>
          </template>
        </el-table-column>

        <!-- 角色 -->
        <el-table-column label="角色" width="140">
          <template #default="{ row }">
            <el-tag :type="roleTagType(row.role)" size="small">{{ roleLabel(row.role) }}</el-tag>
          </template>
        </el-table-column>

        <!-- 管辖范围 -->
        <el-table-column label="管辖范围" min-width="160">
          <template #default="{ row }">
            <span v-if="row.role === 'admin'" class="scope-company">全公司</span>
            <span v-else-if="row.scope_id" class="scope-tag">
              {{ scopeLabel(row) }}
            </span>
            <span v-else class="text-muted">—</span>
          </template>
        </el-table-column>

        <!-- 授权时间 -->
        <el-table-column label="授权时间" width="120">
          <template #default="{ row }">
            <span class="time-text">{{ formatDate(row.created_at) }}</span>
          </template>
        </el-table-column>

        <!-- 操作 -->
        <el-table-column label="操作" width="80" align="center">
          <template #default="{ row }">
            <el-popconfirm
              :title="`确认卸任「${row.user_name || row.user_id}」的${roleLabel(row.role)}角色？`"
              confirm-button-text="确认卸任"
              confirm-button-type="danger"
              @confirm="doRevoke(row)"
            >
              <template #reference>
                <el-button link type="danger" size="small">卸任</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <div v-if="!loading && filteredRoles.length === 0" class="empty-tip">
        暂无授权记录
      </div>
    </el-card>

    <!-- 指定负责人弹窗 -->
    <el-dialog v-model="grantDialogVisible" title="指定负责人" width="460px" @close="resetGrant">
      <!-- 步骤 1：搜索员工 -->
      <div v-if="!selectedUser" class="search-step">
        <div class="step-label">第一步：搜索并选择员工</div>
        <el-input
          v-model="searchKeyword"
          placeholder="输入姓名或邮箱搜索..."
          :suffix-icon="Search"
          clearable
          @input="onSearchInput"
        />
        <div v-if="searchLoading" class="search-hint">搜索中...</div>
        <div v-else-if="searchResults.length > 0" class="search-results">
          <div
            v-for="u in searchResults"
            :key="u.id"
            class="search-result-item"
            @click="selectUser(u)"
          >
            <div class="result-avatar">{{ u.name.charAt(0) }}</div>
            <div class="result-info">
              <span class="result-name">{{ u.name }}</span>
              <span class="result-meta">{{ u.email }}<span v-if="u.dept_name"> · {{ u.dept_name }}</span></span>
            </div>
          </div>
        </div>
        <div v-else-if="searchKeyword.length >= 1 && !searchLoading" class="search-hint">未找到员工</div>
      </div>

      <!-- 步骤 2：选择角色和管辖域 -->
      <div v-else class="grant-step">
        <!-- 已选员工 -->
        <div class="selected-user">
          <div class="selected-avatar">{{ selectedUser.name.charAt(0) }}</div>
          <div class="selected-info">
            <span class="selected-name">{{ selectedUser.name }}</span>
            <span class="selected-meta">{{ selectedUser.email }}
              <span v-if="selectedUser.dept_name"> · {{ selectedUser.dept_name }}</span>
            </span>
          </div>
          <el-button link size="small" @click="selectedUser = null; searchKeyword = ''">更换</el-button>
        </div>

        <el-divider style="margin: 12px 0" />

        <div class="step-label">第二步：选择角色和管辖范围</div>
        <el-form :model="grantForm" label-width="90px" style="margin-top: 12px">
          <el-form-item label="角色">
            <el-radio-group v-model="grantForm.role" @change="grantForm.scopeId = ''">
              <el-radio value="admin">平台管理员</el-radio>
              <el-radio value="studio_admin">工作室负责人</el-radio>
              <el-radio value="dept_admin">部门负责人</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="管辖范围" v-if="grantForm.role !== 'admin'">
            <el-select
              v-if="grantForm.role === 'studio_admin'"
              v-model="grantForm.scopeId"
              placeholder="选择工作室"
              style="width: 100%"
            >
              <el-option v-for="s in orgStore.studios" :key="s.id" :label="s.name" :value="s.id">
                <span>{{ s.name }}</span>
                <span style="color:#999;font-size:12px;margin-left:8px">{{ s.id }}</span>
              </el-option>
            </el-select>
            <el-select
              v-else
              v-model="grantForm.scopeId"
              placeholder="选择部门"
              style="width: 100%"
            >
              <el-option
                v-for="d in orgStore.departments"
                :key="d.dept_id"
                :label="d.dept_name"
                :value="d.dept_id"
              >
                <span>{{ d.dept_name }}</span>
                <el-tag v-if="d.status === 'pending_studio'" type="warning" size="small" style="margin-left:8px">待归属</el-tag>
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item v-else label="管辖范围">
            <el-text type="info" size="small">平台管理员管辖全公司，无需指定范围</el-text>
          </el-form-item>
        </el-form>
      </div>

      <template #footer>
        <el-button @click="grantDialogVisible = false">取消</el-button>
        <el-button
          v-if="selectedUser"
          type="primary"
          :disabled="grantForm.role !== 'admin' && !grantForm.scopeId"
          :loading="granting"
          @click="doGrant"
        >
          确认授权
        </el-button>
      </template>
    </el-dialog>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, UserFilled, OfficeBuilding, Folder, Search } from '@element-plus/icons-vue'
import { useOrgStore } from '@/stores/org'
import type { RoleWithUser, UserSearchResult } from '@/stores/org'

const orgStore = useOrgStore()

const loading = ref(false)
const roles = ref<RoleWithUser[]>([])
const filterRole = ref('')
const filteredRoles = computed(() =>
  filterRole.value ? roles.value.filter(r => r.role === filterRole.value) : roles.value
)

const adminCount = computed(() => roles.value.filter(r => r.role === 'admin').length)
const studioAdminCount = computed(() => roles.value.filter(r => r.role === 'studio_admin').length)
const deptAdminCount = computed(() => roles.value.filter(r => r.role === 'dept_admin').length)

// ─── 授权弹窗 ─────────────────────────────────────────────────────────────────
const grantDialogVisible = ref(false)
const granting = ref(false)
const searchKeyword = ref('')
const searchResults = ref<UserSearchResult[]>([])
const searchLoading = ref(false)
const selectedUser = ref<UserSearchResult | null>(null)
const grantForm = ref({ role: 'dept_admin', scopeId: '' })

let searchTimer: ReturnType<typeof setTimeout> | null = null
function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  if (!searchKeyword.value.trim()) { searchResults.value = []; return }
  searchTimer = setTimeout(async () => {
    searchLoading.value = true
    try {
      searchResults.value = await orgStore.searchUsers(searchKeyword.value)
    } finally {
      searchLoading.value = false
    }
  }, 300)
}

function selectUser(u: UserSearchResult) {
  selectedUser.value = u
  searchResults.value = []
}

function openGrantDialog() {
  grantDialogVisible.value = true
}

function resetGrant() {
  searchKeyword.value = ''
  searchResults.value = []
  selectedUser.value = null
  grantForm.value = { role: 'dept_admin', scopeId: '' }
}

async function doGrant() {
  if (!selectedUser.value) return
  const role = grantForm.value.role
  // admin 角色不需要 scopeId
  if (role !== 'admin' && !grantForm.value.scopeId) return
  granting.value = true

  let scopeType: string
  let scopeId: string
  if (role === 'admin') {
    scopeType = 'company'
    scopeId = ''
  } else if (role === 'studio_admin') {
    scopeType = 'studio'
    scopeId = grantForm.value.scopeId
  } else {
    scopeType = 'dept'
    scopeId = grantForm.value.scopeId
  }

  try {
    await orgStore.grantRole(selectedUser.value.id, role, scopeType, scopeId)
    ElMessage.success(`已将「${selectedUser.value.name}」设为 ${roleLabel(role)}`)
    grantDialogVisible.value = false
    await loadRoles()
  } catch (e: any) {
    // 错误已由 http 拦截器处理
  } finally {
    granting.value = false
  }
}

// ─── 卸任 ─────────────────────────────────────────────────────────────────────
async function doRevoke(row: RoleWithUser) {
  try {
    await orgStore.revokeRole(row.user_id, row.role, row.scope_id)
    ElMessage.success('已卸任')
    await loadRoles()
  } catch (e: any) {
    // 错误已由 http 拦截器处理
  }
}

// ─── 辅助函数 ─────────────────────────────────────────────────────────────────
function roleLabel(role: string) {
  return { admin: '平台管理员', studio_admin: '工作室负责人', dept_admin: '部门负责人', user: '员工' }[role] || role
}

function roleTagType(role: string): 'danger' | 'warning' | 'info' | '' {
  return { admin: 'danger', studio_admin: 'warning', dept_admin: 'info' }[role] as any || ''
}

function scopeLabel(row: RoleWithUser) {
  if (row.role === 'studio_admin') {
    const s = orgStore.studios.find(s => s.id === row.scope_id)
    return s ? `工作室：${s.name}` : `工作室：${row.scope_id}`
  }
  const d = orgStore.departments.find(d => d.dept_id === row.scope_id)
  return d ? `部门：${d.dept_name}` : `部门：${row.scope_id}`
}

function formatDate(str: string) {
  if (!str) return '—'
  return str.slice(0, 10)
}

function applyFilter() { /* filteredRoles computed 自动响应 */ }

async function loadRoles() {
  loading.value = true
  try {
    roles.value = await orgStore.fetchAllRoles()
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await Promise.all([
    loadRoles(),
    orgStore.fetchStudios(),
    orgStore.fetchDepartments(),
  ])
})
</script>

<style scoped>
.role-page { padding: 0; }

.page-header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 20px; flex-wrap: wrap; gap: 12px;
}
.header-left { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.page-title { margin: 0; font-size: 20px; font-weight: 700; }
.page-sub { color: var(--el-text-color-secondary); font-size: 13px; }

/* 统计卡片 */
.role-stat-card {
  display: flex; align-items: center; gap: 12px;
  padding: 14px 18px;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
}
.role-stat-card.admin { background: #fff1f0; border-color: #ffccc7; }
.role-stat-card.studio { background: #fffbe6; border-color: #ffe58f; }
.role-stat-card.dept { background: #e6f4ff; border-color: #bae0ff; }
.stat-icon { opacity: .7; flex-shrink: 0; }
.stat-num { font-size: 24px; font-weight: 700; line-height: 1; }
.stat-label { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }

/* 表格人员列 */
.user-cell { display: flex; align-items: center; gap: 10px; }
.user-avatar {
  width: 32px; height: 32px; border-radius: 50%;
  background: var(--el-color-primary-light-5);
  color: var(--el-color-primary-dark-2);
  display: flex; align-items: center; justify-content: center;
  font-size: 14px; font-weight: 600; flex-shrink: 0;
}
.user-info { display: flex; flex-direction: column; min-width: 0; }
.user-name { font-size: 14px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.user-email { font-size: 12px; color: var(--el-text-color-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.dept-tag {
  font-size: 12px; background: var(--el-fill-color);
  padding: 2px 6px; border-radius: 4px;
}
.scope-company { font-size: 13px; font-weight: 500; color: var(--el-color-danger); }
.scope-tag { font-size: 13px; color: var(--el-text-color-regular); }
.time-text { font-size: 12px; color: var(--el-text-color-secondary); }
.text-muted { color: var(--el-text-color-placeholder); }
.empty-tip { text-align: center; padding: 24px; color: var(--el-text-color-placeholder); }

/* 弹窗 - 搜索 */
.step-label { font-size: 13px; font-weight: 600; color: var(--el-text-color-secondary); margin-bottom: 10px; }
.search-step { min-height: 180px; }
.search-results {
  margin-top: 8px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px; overflow: hidden; max-height: 240px; overflow-y: auto;
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
  width: 34px; height: 34px; border-radius: 50%;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  display: flex; align-items: center; justify-content: center;
  font-size: 14px; font-weight: 600; flex-shrink: 0;
}
.result-info { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.result-name { font-size: 14px; font-weight: 500; }
.result-meta { font-size: 12px; color: var(--el-text-color-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-hint { padding: 12px 0; font-size: 13px; color: var(--el-text-color-placeholder); text-align: center; }

/* 弹窗 - 已选用户 */
.selected-user {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
}
.selected-avatar {
  width: 36px; height: 36px; border-radius: 50%;
  background: var(--el-color-primary-light-5);
  color: var(--el-color-primary-dark-2);
  display: flex; align-items: center; justify-content: center;
  font-size: 15px; font-weight: 600; flex-shrink: 0;
}
.selected-info { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.selected-name { font-size: 14px; font-weight: 600; }
.selected-meta { font-size: 12px; color: var(--el-text-color-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ─── 响应式 ─────────────────────────────────────────────────── */
@media (max-width: 900px) {
  .page-header { flex-direction: column; align-items: flex-start; }
}

@media (max-width: 640px) {
  /* 窄屏统计卡片纵向排列由 el-row gutter 控制，无需额外处理 */
  .stat-num { font-size: 20px; }
}
</style>
