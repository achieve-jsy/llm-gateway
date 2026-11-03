<template>
  <div class="account-page">
    <div class="page-header">
      <div class="header-left">
        <h2 class="page-title">账号与角色</h2>
        <span class="page-sub">一人一行；管理员来自角色授权（可授可撤），负责人（EHR 同步）在组织页维护</span>
      </div>
      <el-button size="small" :loading="loading" @click="loadAccounts">
        <el-icon><Refresh /></el-icon> 刷新
      </el-button>
    </div>

    <!-- 四个角色的人数统计：点击卡片即按该角色筛选 -->
    <el-row :gutter="12" class="stat-row">
      <el-col v-for="key in ROLE_KEYS" :key="key" :xs="12" :sm="6">
        <div
          class="role-stat-card"
          :class="[`is-${key}`, { active: roleFilter === key }]"
          :data-test="`stat-${key}`"
          @click="selectRole(key)"
        >
          <div class="stat-num">{{ roleCounts[key] || 0 }}</div>
          <div class="stat-label">{{ ROLE_META[key].label }}</div>
        </div>
      </el-col>
    </el-row>

    <el-card shadow="never">
      <div class="toolbar">
        <el-input
          v-model="keyword"
          data-test="role-search"
          placeholder="搜索姓名 / 工号 / 岗位 / 邮箱"
          clearable
          class="filter filter-keyword"
          @input="onKeywordInput"
          @keyup.enter="applyFilters"
          @clear="applyFilters"
        >
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>

        <el-select v-model="roleFilter" placeholder="全部角色" clearable class="filter" @change="applyFilters">
          <el-option v-for="key in ROLE_KEYS" :key="key" :label="ROLE_META[key].label" :value="key" />
        </el-select>

        <el-select v-model="studioFilter" placeholder="全部工作室" clearable class="filter" @change="onStudioChange">
          <el-option v-for="s in orgStore.studios" :key="s.id" :label="s.name" :value="s.id" />
        </el-select>

        <!-- 两级下拉：按工作室分组的团队 -->
        <el-select v-model="deptFilter" placeholder="全部团队" clearable filterable class="filter" @change="applyFilters">
          <el-option-group v-for="group in deptGroups" :key="group.label" :label="group.label">
            <el-option v-for="d in group.depts" :key="d.dept_id" :label="d.dept_name" :value="d.dept_id" />
          </el-option-group>
        </el-select>

        <el-date-picker
          v-model="dateRange"
          type="daterange"
          value-format="YYYY-MM-DD"
          start-placeholder="创建起始"
          end-placeholder="创建截止"
          class="filter filter-date"
          @change="applyFilters"
        />

        <el-button link size="small" @click="resetFilters">重置</el-button>
      </div>

      <div class="list-head">
        <span>账号列表</span>
        <span class="list-total">{{ total }} 人</span>
      </div>

      <AccountTable :accounts="rows" :loading="loading" :empty-text="emptyText" @set-role="openRoleDialog" />

      <div v-if="total > PAGE_SIZE" class="pager">
        <el-pagination
          :current-page="page"
          :page-size="PAGE_SIZE"
          :total="total"
          layout="total, prev, pager, next, jumper"
          size="small"
          @current-change="onPageChange"
        />
      </div>
    </el-card>

    <!-- 设置角色：查看/卸任现有角色 + 新增授权 -->
    <el-dialog v-model="dialogVisible" title="设置角色" width="520px" @close="resetDialog">
      <template v-if="dialogUser">
        <div class="dialog-user">
          <div class="dialog-avatar">{{ dialogUser.name?.charAt(0) || '?' }}</div>
          <div class="dialog-info">
            <span class="dialog-name">{{ dialogUser.name || dialogUser.user_id }}</span>
            <span class="dialog-meta">
              {{ dialogUser.employee_id || '无工号' }} · {{ dialogUser.dept_name || '未归属团队' }}
              <template v-if="dialogUser.studio_name"> · {{ dialogUser.studio_name }}</template>
            </span>
          </div>
        </div>

        <div class="section-label">现有角色</div>
        <div v-if="currentRoles.length === 0" class="empty-roles">暂无角色授权（员工）</div>
        <div v-for="r in currentRoles" :key="r.role + r.scope_id" class="role-line">
          <RoleBadge :role="r.role" />
          <span class="role-scope">{{ r.scope_name || '全公司' }}</span>
          <el-popconfirm
            :title="`确认卸任该用户的${roleLabel(r.role)}？`"
            confirm-button-text="确认卸任"
            confirm-button-type="danger"
            @confirm="doRevoke(r)"
          >
            <template #reference>
              <el-button link type="danger" size="small">卸任</el-button>
            </template>
          </el-popconfirm>
        </div>

        <el-divider style="margin: 14px 0" />

        <div class="section-label">新增角色</div>
        <el-form :model="grantForm" label-width="80px" style="margin-top: 8px">
          <el-form-item label="角色">
            <el-radio-group v-model="grantForm.role" @change="grantForm.scopeId = ''">
              <el-radio value="admin">平台管理员</el-radio>
              <el-radio value="studio_admin">工作室管理员</el-radio>
              <el-radio value="dept_admin">团队管理员</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="grantForm.role !== 'admin'" label="管辖范围">
            <el-select
              v-if="grantForm.role === 'studio_admin'"
              v-model="grantForm.scopeId"
              placeholder="选择工作室"
              style="width: 100%"
            >
              <el-option v-for="s in orgStore.studios" :key="s.id" :label="s.name" :value="s.id" />
            </el-select>
            <el-select v-else v-model="grantForm.scopeId" placeholder="选择团队" filterable style="width: 100%">
              <el-option v-for="d in orgStore.departments" :key="d.dept_id" :label="d.dept_name" :value="d.dept_id">
                <span>{{ d.dept_name }}</span>
                <span v-if="d.studio_name" class="option-sub">{{ d.studio_name }}</span>
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item v-else label="管辖范围">
            <el-text type="info" size="small">平台管理员管辖全公司，无需指定范围</el-text>
          </el-form-item>
        </el-form>
      </template>

      <template #footer>
        <el-button @click="dialogVisible = false">关闭</el-button>
        <el-button
          type="primary"
          :disabled="grantForm.role !== 'admin' && !grantForm.scopeId"
          :loading="granting"
          @click="doGrant"
        >
          授权
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { useOrgStore } from '@/stores/org'
import type { AccountRow, OrgDepartment, RoleGrantItem, RoleKey } from '@/types/org'
import AccountTable from '@/components/org/account/AccountTable.vue'
import RoleBadge from '@/components/org/account/RoleBadge.vue'
import { ROLE_KEYS, ROLE_META, roleLabel } from '@/components/org/account/roles'

const PAGE_SIZE = 20

const orgStore = useOrgStore()

const loading = ref(false)
const accounts = ref<AccountRow[]>([])
const total = ref(0)
const page = ref(1)
const roleCounts = ref<Record<RoleKey, number>>({ admin: 0, studio_admin: 0, dept_admin: 0, user: 0 })
const apiReady = ref(true)

const keyword = ref('')
const roleFilter = ref<RoleKey | ''>('')
const studioFilter = ref('')
const deptFilter = ref('')
const dateRange = ref<[string, string] | null>(null)

// ─── 列表 ───────────────────────────────────────────────────────────────────
// 创建时间区间走服务端 created_from/created_to（YYYY-MM-DD，右界含当日），不再前端过滤当前页
const rows = computed(() => accounts.value)

const emptyText = computed(() => {
  if (!apiReady.value) return '账号接口暂未就绪（GET /api/v1/org/accounts），稍后重试'
  if (keyword.value || roleFilter.value || studioFilter.value || deptFilter.value || dateRange.value) {
    return '未找到匹配的账号'
  }
  return '暂无账号'
})

const deptGroups = computed(() => {
  const list = studioFilter.value
    ? orgStore.departments.filter(d => d.studio_id === studioFilter.value)
    : orgStore.departments
  const groups = new Map<string, OrgDepartment[]>()
  for (const d of list) {
    const label = d.studio_name || '未归属工作室'
    const arr = groups.get(label) || []
    arr.push(d)
    groups.set(label, arr)
  }
  return [...groups.entries()].map(([label, depts]) => ({ label, depts }))
})

let keywordTimer: ReturnType<typeof setTimeout> | null = null
function onKeywordInput() {
  if (keywordTimer) clearTimeout(keywordTimer)
  keywordTimer = setTimeout(applyFilters, 300)
}

function applyFilters() {
  if (keywordTimer) {
    clearTimeout(keywordTimer)
    keywordTimer = null
  }
  page.value = 1
  void loadAccounts()
}

function onStudioChange() {
  // 切换工作室后团队不在范围内就清掉，避免两个筛选互相打架查不出人
  if (deptFilter.value && !deptGroups.value.some(g => g.depts.some(d => d.dept_id === deptFilter.value))) {
    deptFilter.value = ''
  }
  applyFilters()
}

function selectRole(key: RoleKey) {
  roleFilter.value = roleFilter.value === key ? '' : key
  applyFilters()
}

function resetFilters() {
  keyword.value = ''
  roleFilter.value = ''
  studioFilter.value = ''
  deptFilter.value = ''
  dateRange.value = null
  applyFilters()
}

function onPageChange(next: number) {
  page.value = next
  void loadAccounts()
}

async function loadAccounts() {
  loading.value = true
  const [createdFrom, createdTo] = dateRange.value || []
  try {
    // createdFrom/createdTo 已在契约里，store 的入参类型待 Lead 补两个可选字段；
    // 这里放宽一次类型，store 补齐后即可去掉断言（行为不变）
    const query = {
      keyword: keyword.value.trim(),
      role: roleFilter.value,
      studioId: studioFilter.value,
      deptId: deptFilter.value,
      createdFrom,
      createdTo,
      page: page.value,
      pageSize: PAGE_SIZE,
    }
    const result = await orgStore.fetchAccounts(query)
    accounts.value = result.accounts || []
    total.value = Number(result.total || 0)
    page.value = Number(result.page || page.value)
    roleCounts.value = { ...roleCounts.value, ...(result.role_counts || {}) }
    apiReady.value = true
  } catch (e) {
    // 接口未就绪时的兜底：清空列表 + 中文空态，不阻塞页面
    apiReady.value = false
    accounts.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

// ─── 角色授权 ───────────────────────────────────────────────────────────────
const dialogVisible = ref(false)
const dialogUser = ref<AccountRow | null>(null)
const granting = ref(false)
const grantForm = ref<{ role: 'admin' | 'studio_admin' | 'dept_admin'; scopeId: string }>({
  role: 'dept_admin',
  scopeId: '',
})

const currentRoles = computed<RoleGrantItem[]>(() => dialogUser.value?.roles || [])

function openRoleDialog(row: AccountRow) {
  dialogUser.value = row
  grantForm.value = { role: 'dept_admin', scopeId: '' }
  dialogVisible.value = true
}

function resetDialog() {
  dialogUser.value = null
  grantForm.value = { role: 'dept_admin', scopeId: '' }
}

async function refreshDialogRow() {
  // 授权/卸任后刷新列表，并把弹窗里的当前行换成最新数据（行内即时更新）
  const userId = dialogUser.value?.user_id
  await loadAccounts()
  const fresh = accounts.value.find(a => a.user_id === userId)
  if (fresh) {
    dialogUser.value = fresh
  } else {
    dialogVisible.value = false
  }
}

async function doGrant() {
  const user = dialogUser.value
  if (!user) return
  const role = grantForm.value.role
  if (role !== 'admin' && !grantForm.value.scopeId) return

  let scopeType: RoleGrantItem['scope_type'] = 'dept'
  let scopeId = grantForm.value.scopeId
  if (role === 'admin') {
    scopeType = 'company'
    scopeId = ''
  } else if (role === 'studio_admin') {
    scopeType = 'studio'
  }

  granting.value = true
  try {
    await orgStore.grantRole(user.user_id, role, scopeType, scopeId)
    ElMessage.success(`已将「${user.name || user.user_id}」设为${roleLabel(role as RoleKey)}`)
    grantForm.value = { role: 'dept_admin', scopeId: '' }
    await refreshDialogRow()
  } catch (e) {
    // 错误已由 http 拦截器提示
  } finally {
    granting.value = false
  }
}

async function doRevoke(r: RoleGrantItem) {
  const user = dialogUser.value
  if (!user) return
  try {
    await orgStore.revokeRole(user.user_id, r.role, r.scope_id)
    ElMessage.success(`已卸任${roleLabel(r.role)}`)
    await refreshDialogRow()
  } catch (e) {
    // 错误已由 http 拦截器提示
  }
}

onMounted(async () => {
  await Promise.all([
    loadAccounts(),
    orgStore.fetchStudios().catch(() => null),
    orgStore.fetchDepartments().catch(() => null),
  ])
})
</script>

<style scoped>
.account-page { padding: 0; }

.page-header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 18px; flex-wrap: wrap; gap: 12px;
}
.header-left { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.page-title { margin: 0; font-size: 20px; font-weight: 700; }
.page-sub { color: var(--el-text-color-secondary); font-size: 13px; }

/* 角色统计卡：点击筛选 */
.stat-row { margin-bottom: 16px; }
.role-stat-card {
  padding: 14px 16px;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
  cursor: pointer;
  transition: box-shadow .15s, border-color .15s, transform .15s;
}
.role-stat-card:hover { border-color: var(--el-color-primary-light-5); }
.role-stat-card.active { box-shadow: 0 0 0 2px var(--el-color-primary-light-7); border-color: var(--el-color-primary); }
.role-stat-card.is-admin { background: #fff1f0; }
.role-stat-card.is-studio_admin { background: #fffbe6; }
.role-stat-card.is-dept_admin { background: #f0f9eb; }
.role-stat-card.is-user { background: var(--el-fill-color-light); }
.stat-num { font-size: 22px; font-weight: 700; line-height: 1.1; }
.stat-label { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }

.toolbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 14px; }
.filter { width: 150px; }
.filter-keyword { width: 240px; }
.filter-date { width: 250px; }

.list-head {
  display: flex; align-items: baseline; gap: 8px;
  font-weight: 600; margin-bottom: 8px;
}
.list-total { color: var(--el-text-color-secondary); font-size: 12px; font-weight: 400; }
.pager { display: flex; justify-content: flex-end; margin-top: 14px; }

/* 弹窗 */
.dialog-user { display: flex; align-items: center; gap: 10px; padding: 10px 14px; background: var(--el-fill-color-light); border-radius: 6px; }
.dialog-avatar {
  width: 34px; height: 34px; border-radius: 50%;
  background: var(--el-color-primary-light-5); color: var(--el-color-primary-dark-2);
  display: flex; align-items: center; justify-content: center; font-weight: 600; flex-shrink: 0;
}
.dialog-info { display: flex; flex-direction: column; min-width: 0; }
.dialog-name { font-size: 14px; font-weight: 600; }
.dialog-meta { font-size: 12px; color: var(--el-text-color-secondary); }
.section-label { font-size: 13px; font-weight: 600; color: var(--el-text-color-secondary); margin: 14px 0 8px; }
.empty-roles { font-size: 13px; color: var(--el-text-color-placeholder); }
.role-line { display: flex; align-items: center; gap: 8px; padding: 6px 0; border-bottom: 1px dashed var(--el-border-color-lighter); }
.role-line:last-child { border-bottom: none; }
.role-scope { font-size: 13px; color: var(--el-text-color-regular); flex: 1; }
.option-sub { color: var(--el-text-color-placeholder); font-size: 12px; margin-left: 8px; }

@media (max-width: 720px) {
  .page-header { flex-direction: column; align-items: flex-start; }
  .filter, .filter-keyword, .filter-date { width: 100%; }
  .pager { justify-content: center; overflow-x: auto; }
}
</style>
