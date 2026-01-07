<template>
  <el-drawer v-model="localVisible" :title="drawerTitle" size="680px" destroy-on-close direction="rtl" @close="handleDrawerClose">
    <div v-loading="loading">
      <el-tabs v-model="activeTab">
        <el-tab-pane v-if="showStudioTab" name="studio" label="工作室授权">
          <div class="tab-toolbar">
            <el-button type="primary" size="small" @click="openAddStudio">授权工作室</el-button>
          </div>
          <el-table :data="studioEntitlements" stripe size="small" empty-text="暂无授权工作室">
            <el-table-column label="工作室名称" min-width="160" prop="scope_name" />
            <el-table-column label="授权人" min-width="120" prop="granted_by_name">
              <template #default="{ row }">{{ row.granted_by_name || row.granted_by || '-' }}</template>
            </el-table-column>
            <el-table-column label="授权时间" width="170" align="center">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="80" align="center">
              <template #default="{ row }">
                <el-popconfirm
                  title="撤销后下属部门将同时失效，确定撤销？"
                  confirm-button-text="确定撤销"
                  confirm-button-type="danger"
                  @confirm="handleRevokeStu(row)"
                >
                  <template #reference>
                    <el-button text type="danger" size="small">撤销</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane name="dept" label="部门开通">
          <div class="tab-toolbar">
            <el-select
              v-model="selectedStudioForDept"
              placeholder="按工作室筛选"
              size="small"
              :clearable="!restrictStudioId"
              :disabled="!!restrictStudioId"
              style="width: 180px; margin-right: 8px"
              @change="onStudioFilterChange"
            >
              <el-option
                v-for="s in studioEntitlements"
                :key="s.id"
                :label="s.scope_name"
                :value="s.scope_id"
              />
            </el-select>
            <el-button type="primary" size="small" @click="openAddDept" :disabled="!selectedStudioForDept">
              开通部门
            </el-button>
          </div>
          <el-table :data="filteredDeptEntitlements" stripe size="small" empty-text="暂无开通部门">
            <el-table-column label="部门名称" min-width="160" prop="scope_name" />
            <el-table-column label="所属工作室" min-width="120">
              <template #default="{ row }">
                {{ getStudioName(row.scope_id) || '-' }}
              </template>
            </el-table-column>
            <el-table-column label="授权人" width="100" prop="granted_by_name">
              <template #default="{ row }">{{ row.granted_by_name || row.granted_by || '-' }}</template>
            </el-table-column>
            <el-table-column label="开通时间" width="170" align="center">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="80" align="center">
              <template #default="{ row }">
                <el-popconfirm
                  title="确定撤销该部门开通？"
                  confirm-button-text="确定"
                  confirm-button-type="danger"
                  @confirm="handleRevokeDept(row)"
                >
                  <template #reference>
                    <el-button text type="danger" size="small">撤销</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="addStudioDialogVisible" title="授权工作室" width="400px" @close="resetAddStudio">
      <el-form label-width="80px">
        <el-form-item label="工作室">
          <el-select v-model="newStudioId" placeholder="选择工作室" style="width: 100%" filterable>
            <el-option
              v-for="s in availableStudios"
              :key="s.id"
              :label="s.name"
              :value="s.id"
              :disabled="isStudioAlreadyGranted(s.id)"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addStudioDialogVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!newStudioId" :loading="granting" @click="handleGrantStudio">
          确定授权
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="addDeptDialogVisible" title="开通部门" width="400px" @close="resetAddDept">
      <el-form label-width="80px">
        <el-form-item label="部门">
          <el-select
            v-model="newDeptId"
            placeholder="选择部门"
            style="width: 100%"
            filterable
            :disabled="props.actorRole === 'dept_admin' && !!props.restrictDeptId"
          >
            <el-option
              v-for="d in availableDepts"
              :key="d.dept_id"
              :label="d.dept_name"
              :value="d.dept_id"
              :disabled="isDeptAlreadyGranted(d.dept_id)"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="所属工作室">
          <span class="dept-studio-name">{{ deptStudioHint }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addDeptDialogVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!newDeptId" :loading="granting" @click="handleGrantDept">
          确定开通
        </el-button>
      </template>
    </el-dialog>
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { groupsApi } from '@/api/groups'
import type { GroupEntitlement } from '@/types/group'
import type { Studio, OrgDepartment } from '@/types/org'
import http from '@/services/http'

const props = defineProps<{
  visible: boolean
  groupId: number | null
  groupName: string
  // 抽屉调用者角色：决定哪些 Tab 可见、哪些功能可写
  // - admin：完整两 Tab（默认 studio）
  // - studio_admin：只显示 dept Tab，并锁定到自己的工作室
  // - dept_admin：只显示 dept Tab，且只能看到/操作本部门
  actorRole?: 'admin' | 'studio_admin' | 'dept_admin'
  // 调用者所属的 studio ID，用于 dept Tab 锁定工作室
  restrictStudioId?: string
  // 调用者所属的 dept ID，用于 dept_admin 锁定本部门
  restrictDeptId?: string
}>()

const emit = defineEmits<{
  'update:visible': [val: boolean]
  closed: []
}>()

const localVisible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val),
})

const drawerTitle = computed(() => `授权管理 — ${props.groupName}`)

const loading = ref(false)
const activeTab = ref('studio')
const studioEntitlements = ref<GroupEntitlement[]>([])
const deptEntitlements = ref<GroupEntitlement[]>([])
const allStudios = ref<Studio[]>([])
const allDepts = ref<OrgDepartment[]>([])

const addStudioDialogVisible = ref(false)
const newStudioId = ref('')
const granting = ref(false)

const addDeptDialogVisible = ref(false)
const newDeptId = ref('')
const selectedStudioForDept = ref('')

// studio Tab 仅 admin 可见（spec 3.7 / 3.8：studio_admin/dept_admin 打开抽屉不应看到工作室授权 Tab）
const showStudioTab = computed(() => (props.actorRole ?? 'admin') === 'admin')

const availableStudios = computed(() => {
  const grantedIds = new Set(studioEntitlements.value.filter(e => e.enabled).map(e => e.scope_id))
  return allStudios.value.filter(s => !grantedIds.has(s.id))
})

const availableDepts = computed(() => {
  if (!selectedStudioForDept.value) return []
  const grantedIds = new Set(deptEntitlements.value.filter(e => e.enabled).map(e => e.scope_id))
  return allDepts.value.filter(
    d => d.studio_id === selectedStudioForDept.value && !grantedIds.has(d.dept_id)
  )
})

const filteredDeptEntitlements = computed(() => {
  let rows = deptEntitlements.value

  // dept_admin 锁定本部门（spec 5.9：部门列表只显示本部门的记录）
  if (props.restrictDeptId && props.actorRole === 'dept_admin') {
    rows = rows.filter(e => e.scope_id === props.restrictDeptId)
  } else if (props.restrictStudioId) {
    // studio_admin/dept_admin（指定了 studio 范围）：只显示该工作室下属部门
    rows = rows.filter(e => {
      const dept = allDepts.value.find(d => d.dept_id === e.scope_id)
      return dept?.studio_id === props.restrictStudioId
    })
  } else if (selectedStudioForDept.value) {
    // admin 自由筛选
    const studioGrantedIds = new Set(studioEntitlements.value.filter(e => e.enabled).map(e => e.scope_id))
    rows = rows.filter(e => {
      const dept = allDepts.value.find(d => d.dept_id === e.scope_id)
      return dept?.studio_id === selectedStudioForDept.value && studioGrantedIds.has(dept.studio_id)
    })
  }
  return rows
})

const deptStudioHint = computed(() => {
  if (!newDeptId.value) return '-'
  const dept = allDepts.value.find(d => d.dept_id === newDeptId.value)
  return dept?.studio_name || dept?.studio_id || '-'
})

function handleDrawerClose() {
  emit('closed')
}

async function loadData() {
  if (!props.groupId) return
  loading.value = true
  try {
    const [entRes, stuRes, deptRes] = await Promise.all([
      groupsApi.listEntitlements(props.groupId),
      http.get('/api/v1/org/studios', { params: { page: 1, page_size: 100 } }),
      http.get('/api/v1/org/departments', { params: { page: 1, page_size: 500 } }),
    ])
    const items = entRes.data.data?.items || []
    studioEntitlements.value = items.filter(i => i.scope_type === 'studio')
    deptEntitlements.value = items.filter(i => i.scope_type === 'dept')
    allStudios.value = stuRes.data.studios || []
    allDepts.value = deptRes.data.departments || []
  } catch {
    ElMessage.error('加载授权信息失败')
  } finally {
    loading.value = false
  }
}

watch(() => props.visible, (val) => {
  if (val && props.groupId) {
    // 锁定范围：按角色/props 约束 dept Tab 的工作室筛选器
    if (props.restrictStudioId) {
      selectedStudioForDept.value = props.restrictStudioId
    } else {
      selectedStudioForDept.value = ''
    }
    // 按角色决定默认 Tab：admin 进 studio，管理角色进 dept
    activeTab.value = showStudioTab.value ? 'studio' : 'dept'
    loadData()
  } else {
    studioEntitlements.value = []
    deptEntitlements.value = []
    allStudios.value = []
    allDepts.value = []
  }
})

function openAddStudio() {
  addStudioDialogVisible.value = true
  newStudioId.value = ''
}

function resetAddStudio() {
  newStudioId.value = ''
}

function isStudioAlreadyGranted(studioId: string) {
  return studioEntitlements.value.some(e => e.scope_id === studioId && e.enabled)
}

async function handleGrantStudio() {
  if (!props.groupId || !newStudioId.value) return
  granting.value = true
  try {
    await groupsApi.grantEntitlement(props.groupId, {
      scope_type: 'studio',
      scope_id: newStudioId.value,
    })
    ElMessage.success('已授权工作室')
    addStudioDialogVisible.value = false
    await loadData()
  } catch {
    ElMessage.error('授权失败')
  } finally {
    granting.value = false
  }
}

async function handleRevokeStu(row: GroupEntitlement) {
  if (!props.groupId) return
  try {
    await groupsApi.revokeEntitlement(props.groupId, row.id)
    ElMessage.success('已撤销工作室授权')
    await loadData()
  } catch {
    ElMessage.error('撤销失败')
  }
}

function openAddDept() {
  if (!props.groupId || !selectedStudioForDept.value) return
  addDeptDialogVisible.value = true
  // dept_admin 锁定本部门，自动选中且禁止修改（spec 5.9：dept_admin 不能开通其他部门）
  newDeptId.value =
    props.actorRole === 'dept_admin' && props.restrictDeptId ? props.restrictDeptId : ''
}

function resetAddDept() {
  newDeptId.value = ''
}

function isDeptAlreadyGranted(deptId: string) {
  return deptEntitlements.value.some(e => e.scope_id === deptId && e.enabled)
}

function onStudioFilterChange() {
  newDeptId.value = ''
}

async function handleGrantDept() {
  if (!props.groupId || !newDeptId.value) return
  granting.value = true
  try {
    await groupsApi.grantEntitlement(props.groupId, {
      scope_type: 'dept',
      scope_id: newDeptId.value,
    })
    ElMessage.success('已开通部门')
    addDeptDialogVisible.value = false
    await loadData()
  } catch {
    ElMessage.error('开通失败')
  } finally {
    granting.value = false
  }
}

async function handleRevokeDept(row: GroupEntitlement) {
  if (!props.groupId) return
  try {
    await groupsApi.revokeEntitlement(props.groupId, row.id)
    ElMessage.success('已撤销部门开通')
    await loadData()
  } catch {
    ElMessage.error('撤销失败')
  }
}

function getStudioName(scopeId: string) {
  const dept = allDepts.value.find(d => d.dept_id === scopeId)
  if (dept?.studio_name) return dept.studio_name
  const studio = allStudios.value.find(s => s.id === scopeId)
  return studio?.name
}

function formatDate(value: string) {
  if (!value) return '-'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
</script>

<style scoped>
.tab-toolbar {
  margin-bottom: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.dept-studio-name {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
</style>
