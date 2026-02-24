<template>
  <div class="service-key-page">
    <div class="page-header">
      <div class="header-info">
        <h2 class="page-title">Service Key 管理</h2>
        <p class="page-desc">程序服务使用的 API Key，绑定到部门，受预算和授权管控</p>
      </div>
      <div class="header-actions">
        <el-select
          v-model="filterDeptId"
          placeholder="按部门筛选"
          clearable
          filterable
          class="toolbar-field"
          :disabled="authStore.effectiveRole === 'dept_admin'"
          @change="onDeptFilterChange"
        >
          <el-option
            v-for="dept in creatableDepts"
            :key="dept.dept_id"
            :label="`${dept.dept_name}（${dept.dept_id}）`"
            :value="dept.dept_id"
          />
        </el-select>
        <el-button type="primary" class="brand-btn" @click="openCreateDialog">
          <el-icon><Plus /></el-icon>
          新建 Service Key
        </el-button>
      </div>
    </div>

    <el-skeleton v-if="loading" :rows="5" animated class="skeleton-wrap" />

    <el-result
      v-else-if="loadError"
      icon="error"
      title="加载失败"
      :sub-title="loadError"
    >
      <template #extra>
        <el-button type="primary" @click="loadKeys">重新加载</el-button>
      </template>
    </el-result>

    <el-empty
      v-else-if="!loading && keys.length === 0 && !loadError"
      description="暂无 Service Key，点击右上角新建"
    />

    <el-table
      v-else
      :data="keys"
      stripe
      highlight-current-row
      class="key-table"
      row-key="id"
    >
      <el-table-column label="名称" prop="name" min-width="140" show-overflow-tooltip />

      <el-table-column label="用途" min-width="140">
        <template #default="{ row }">
          <el-tag size="small" type="info" round class="purpose-tag">{{ row.purpose }}</el-tag>
        </template>
      </el-table-column>

      <el-table-column label="模型组" min-width="180">
        <template #default="{ row }">
          <el-tag v-if="row.accessAllGroups" size="small" type="success" round>全部可用组</el-tag>
          <template v-else-if="row.boundGroups?.length">
            <el-tag
              v-for="g in row.boundGroups"
              :key="g.id"
              size="small"
              type="info"
              round
              class="group-tag"
            >{{ g.name }}</el-tag>
          </template>
          <span v-else class="text-placeholder">未配置</span>
          <el-button link type="primary" size="small" @click="openGroupDialog(row)">编辑</el-button>
        </template>
      </el-table-column>

      <el-table-column label="IP 白名单" min-width="140">
        <template #default="{ row }">
          <span v-if="row.ipAllowlist && row.ipAllowlist.length > 0" class="ip-allowlist-tags">
            <el-tag
              v-for="(ip, idx) in row.ipAllowlist"
              :key="idx"
              size="small"
              type="info"
              round
              class="ip-tag"
            >{{ ip }}</el-tag>
            <span v-if="(row.ipAllowlist?.length ?? 0) > 2" class="ip-tag-more">
              +{{ row.ipAllowlist!.length - 2 }}
            </span>
          </span>
          <span v-else class="text-placeholder">不限</span>
          <el-button
            link type="primary"
            size="small"
            @click="openIPAllowlistDialog(row)"
            style="margin-left: 4px"
          >
            <el-icon><Edit /></el-icon>
          </el-button>
        </template>
      </el-table-column>
      <el-table-column label="所属部门" min-width="180">
        <template #default="{ row }">
          <div class="dept-cell">
            <span class="dept-name">{{ row.deptName || '—' }}</span>
            <span class="dept-id mono-muted">{{ row.deptId }}</span>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="Key 脱敏" min-width="220">
        <template #default="{ row }">
          <div class="key-cell">
            <code class="mono-key">{{ row.maskedKey }}</code>
            <el-tooltip content="脱敏 Key 仅供辨识，完整 Key 仅创建时展示一次">
              <el-icon class="copy-hint"><InfoFilled /></el-icon>
            </el-tooltip>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="状态" width="100" align="center">
        <template #default="{ row }">
          <el-switch
            :model-value="row.enabled"
            inline-prompt
            active-text="启"
            inactive-text="停"
            @change="handleToggle(row)"
          />
        </template>
      </el-table-column>

      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          <span class="text-muted">{{ formatTime(row.createdAt) }}</span>
        </template>
      </el-table-column>

      <el-table-column label="最后使用" width="170">
        <template #default="{ row }">
          <span :class="row.lastUsedAt ? 'text-muted' : 'text-placeholder'">
            {{ row.lastUsedAt ? formatTime(row.lastUsedAt) : '从未使用' }}
          </span>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="120" align="center" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" link size="small" @click="openQuotaDialog(row)">配额</el-button>
          <el-popconfirm
            title="确定吊销此 Service Key？吊销后不可恢复"
            confirm-button-text="吊销"
            cancel-button-text="取消"
            confirm-button-type="danger"
            @confirm="handleDelete(row)"
          >
            <template #reference>
              <el-button type="danger" link size="small">吊销</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <div v-if="total > pageSize" class="pagination-bar">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        size="small"
        @current-change="onPageChange"
        @size-change="onSizeChange"
      />
    </div>

    <!-- 新建对话框 -->
    <el-dialog
      v-model="createDialogVisible"
      title="新建 Service Key"
      width="480px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form
        ref="createFormRef"
        :model="createForm"
        :rules="createRules"
        label-position="top"
        label-width="80px"
      >
        <el-form-item label="部门" prop="deptId">
          <el-select
            v-model="createForm.deptId"
            filterable
            :allow-create="!isDeptLocked"
            default-first-option
            placeholder="选择部门"
            style="width: 100%"
            :disabled="isDeptLocked"
          >
            <el-option
              v-for="dept in creatableDepts"
              :key="dept.dept_id"
              :label="`${dept.dept_name}（${dept.dept_id}）`"
              :value="dept.dept_id"
            />
          </el-select>
          <div v-if="authStore.effectiveRole === 'dept_admin'" style="font-size:11px;color:var(--el-text-color-secondary);margin-top:4px">
            只能为自己管辖的部门创建 Service Key
          </div>
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="createForm.name" placeholder="例如：线上推荐服务" maxlength="60" />
        </el-form-item>
        <el-form-item label="用途标签" prop="purpose">
          <el-input v-model="createForm.purpose" placeholder="例如：recommend-svc" maxlength="40" />
        </el-form-item>
        <el-form-item label="模型组">
          <el-select
            v-model="createForm.groupIds"
            multiple
            filterable
            placeholder="选择可用的模型组"
            style="width: 100%"
            :loading="groupsLoading"
          >
            <el-option :key="'__all__'" label="全部可用组（部门授权内）" :value="ALL_GROUPS_SENTINEL" />
            <el-option
              v-for="g in availableGroups"
              :key="g.id"
              :label="g.name"
              :value="g.id"
              :disabled="createForm.accessAllGroups"
            />
          </el-select>
          <div class="form-hint">
            {{ createForm.accessAllGroups ? '可使用部门授权范围内的全部模型组' : '不选则 Key 暂无可调用模型' }}
          </div>
        </el-form-item>
        <el-form-item label="IP 白名单（可选）">
          <div class="ip-create-input-row">
            <el-input
              v-model="ipCreateInput"
              placeholder="如 10.1.2.3 或 10.0.0.0/8"
              maxlength="64"
              @keydown.enter="addCreateIP"
            />
            <el-button type="primary" :disabled="!ipCreateInput.trim()" @click="addCreateIP">添加</el-button>
          </div>
          <div v-if="createForm.ipAllowlist.length > 0" class="ip-create-tags">
            <el-tag
              v-for="(ip, idx) in createForm.ipAllowlist"
              :key="idx"
              closable
              size="small"
              type="success"
              @close="createForm.ipAllowlist.splice(idx, 1)"
            >{{ ip }}</el-tag>
          </div>
          <div v-else class="ip-create-empty text-placeholder">未填写则不限制 IP</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" class="brand-btn" :loading="creating" @click="handleCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- 模型组编辑对话框 -->
    <el-dialog
      v-model="groupDialogVisible"
      title="编辑模型组权限"
      width="520px"
      :close-on-click-modal="false"
      destroy-on-close
      @open="loadGroupDialogData"
    >
      <div v-if="editGroupKey">
        <el-alert
          :title="`Service Key：${editGroupKey.name}`"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom: 12px"
        />
        <el-tag
          v-if="editGroupKey.accessAllGroups || pendingAccessAll === true"
          size="large"
          type="success"
          closable
          class="group-all-tag"
          @close="handleAccessAll(false)"
        >全部可用组（部门授权内）</el-tag>
        <div v-else-if="editGroupKey.boundGroups?.length" class="bound-tags">
          <el-tag
            v-for="g in editGroupKey.boundGroups"
            :key="g.id"
            closable
            size="large"
            type="info"
            @close="removeGroup(g)"
          >{{ g.name }}</el-tag>
        </div>
        <div v-else class="text-placeholder" style="margin-bottom: 8px">暂未绑定模型组</div>
        <el-select
          v-model="addGroupIds"
          multiple
          filterable
          placeholder="添加模型组"
          style="width: 100%; margin-top: 8px"
          :loading="groupLoading"
          @change="handleAddGroupChange"
        >
          <el-option :key="'__all__'" label="全部可用组（部门授权内）" :value="ALL_GROUPS_SENTINEL" />
          <el-option
            v-for="g in availableAddGroups"
            :key="g.id"
            :label="g.name"
            :value="g.id"
            :disabled="editGroupKey.accessAllGroups || pendingAccessAll === true"
          />
        </el-select>
      </div>
      <template #footer>
        <el-button @click="groupDialogVisible = false">取消</el-button>
        <el-button type="primary" class="brand-btn" :loading="groupSaving" @click="saveGroupChanges">保存</el-button>
      </template>
    </el-dialog>

    <!-- Key 明文展示对话框 -->
    <el-dialog
      v-model="keyRevealVisible"
      title="Service Key 已创建"
      width="560px"
      :close-on-click-modal="false"
      :show-close="true"
      destroy-on-close
      class="key-reveal-dialog"
    >
      <div class="key-reveal-body">
        <el-alert
          title="请立即复制并妥善保存，此 Key 仅展示一次，关闭后将无法再次查看"
          type="warning"
          show-icon
          :closable="false"
          style="margin-bottom: 16px;"
        />
        <div class="key-reveal-card">
          <div class="key-reveal-label">API Key</div>
          <div class="key-reveal-value">
            <code class="key-reveal-code">{{ newKeyPlainText }}</code>
            <el-button
              size="small"
              class="key-copy-btn"
              @click="copyKey"
            >
              <el-icon><DocumentCopy /></el-icon>
              复制
            </el-button>
          </div>
          <div v-if="newKeyDept" class="key-reveal-meta">
            部门：<span class="key-meta-val">{{ newKeyDept }}</span>
          </div>
          <div v-if="newKeyPurpose" class="key-reveal-meta">
            用途：<span class="key-meta-val">{{ newKeyPurpose }}</span>
          </div>
        </div>
      </div>
      <template #footer>
        <el-button @click="keyRevealVisible = false">我已保存，关闭</el-button>
      </template>
    </el-dialog>

    <!-- IP 白名单编辑对话框 -->
    <el-dialog
      v-model="ipEditDialogVisible"
      title="编辑 IP 白名单"
      width="500px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <div v-if="ipEditingKey" class="ip-edit-body">
        <el-alert
          :title="`Service Key：${ipEditingKey.name}（${ipEditingKey.maskedKey}）`"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom: 12px"
        />
        <el-form-item label="允许的 IP / CIDR" style="margin-bottom: 0">
          <div class="ip-input-row">
            <el-input
              v-model="ipInputValue"
              placeholder="如 10.1.2.3 或 10.0.0.0/8"
              maxlength="64"
              @keydown.enter="addIP"
            />
            <el-button type="primary" :disabled="!ipInputValue.trim()" @click="addIP">添加</el-button>
          </div>
        </el-form-item>
        <div class="ip-tags-edit">
          <el-tag
            v-for="(ip, idx) in ipEditList"
            :key="idx"
            closable
            size="default"
            type="success"
            class="ip-tag-edit"
            @close="removeIP(idx)"
          >
            <span class="ip-tag-text">{{ ip }}</span>
          </el-tag>
          <span v-if="ipEditList.length === 0" class="text-placeholder">暂无，表示不限 IP</span>
        </div>
      </div>
      <template #footer>
        <el-button @click="ipEditDialogVisible = false">取消</el-button>
        <el-button type="primary" class="brand-btn" :loading="ipSaving" @click="saveIPAllowlist">保存</el-button>
      </template>
    </el-dialog>

    <!-- 配额编辑对话框 -->
    <el-dialog
      v-model="quotaDialogVisible"
      title="编辑 Service Key 配额"
      width="500px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <div v-if="quotaEditingKey" class="quota-edit-body">
        <el-alert
          :title="`Service Key：${quotaEditingKey.name}`"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom: 16px"
        />
        <el-form label-width="160px" label-position="top">
          <el-form-item label="每分钟请求数 (RPM)">
            <el-input-number
              v-model="quotaForm.rpm"
              :min="0"
              :max="10000"
              :step="10"
              controls-position="right"
              style="width: 100%"
            />
            <span class="quota-hint">0 = 不限制，留空 = 继承默认 ({{ quotaEffective?.default_rpm ?? 120 }})</span>
          </el-form-item>
          <el-form-item label="最大并发数">
            <el-input-number
              v-model="quotaForm.max_concurrency"
              :min="0"
              :max="1000"
              :step="1"
              controls-position="right"
              style="width: 100%"
            />
            <span class="quota-hint">0 = 不限制，留空 = 继承默认 ({{ quotaEffective?.default_max_concurrency ?? 20 }})</span>
          </el-form-item>
        </el-form>
        <div v-if="quotaEffective" class="quota-effective">
          <div class="quota-effective-title">当前生效配额</div>
          <div class="quota-effective-grid">
            <div class="quota-effective-item">
              <span class="label">RPM：</span>
              <span class="value">{{ quotaEffective.rpm === 0 ? '不限制' : quotaEffective.rpm }}</span>
              <el-tag size="small" :type="quotaEffective.source === 'override' ? 'success' : 'info'" style="margin-left: 6px">
                {{ quotaEffective.source === 'override' ? '自定义' : '继承默认' }}
              </el-tag>
            </div>
            <div class="quota-effective-item">
              <span class="label">并发：</span>
              <span class="value">{{ quotaEffective.max_concurrency === 0 ? '不限制' : quotaEffective.max_concurrency }}</span>
            </div>
          </div>
        </div>
      </div>
      <template #footer>
        <el-button @click="quotaDialogVisible = false">取消</el-button>
        <el-popconfirm title="清除配额设置？将恢复为继承默认值" @confirm="clearQuota">
          <template #reference>
            <el-button>清除设置</el-button>
          </template>
        </el-popconfirm>
        <el-button type="primary" :loading="quotaSaving" @click="saveQuota">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { Plus, InfoFilled, DocumentCopy, Edit } from '@element-plus/icons-vue'
import { updateServiceKeyIPAllowlist } from '@/services/audit'
import { getServiceKeyQuota, setServiceKeyQuota } from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'

import {
  listServiceKeys,
  createServiceKey,
  toggleServiceKey,
  deleteServiceKey,
  getAvailableGroups,
  updateServiceKeyGroups,
  toggleServiceKeyAccessAllGroups,
} from '@/services/control-plane'
import type { ServiceKeyItem, CreateServiceKeyResponse } from '@/types/service-key'
import type { AvailableGroup, BoundGroupInfo } from '@/types/api-key'
import { useAuthStore } from '@/stores/auth'
import { useOrgStore } from '@/stores/org'

const ALL_GROUPS_SENTINEL = -1

function isRealGroupId(v: number | typeof ALL_GROUPS_SENTINEL): v is number {
  return typeof v === 'number' && v !== ALL_GROUPS_SENTINEL
}

const authStore = useAuthStore()
const orgStore = useOrgStore()

const filterDeptId = ref('')
const keys = ref<ServiceKeyItem[]>([])
const loading = ref(false)
const loadError = ref('')
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const createDialogVisible = ref(false)
const creating = ref(false)
const createFormRef = ref<FormInstance>()
const createForm = reactive({
  deptId: '',
  name: '',
  purpose: '',
  ipAllowlist: [] as string[],
  groupIds: [] as number[],
  accessAllGroups: false,
})

const groupsLoading = ref(false)
const availableGroups = ref<AvailableGroup[]>([])

const groupDialogVisible = ref(false)
const editGroupKey = ref<ServiceKeyItem | null>(null)
const groupLoading = ref(false)
const groupSaving = ref(false)
const availableAddGroups = ref<AvailableGroup[]>([])
const addGroupIds = ref<(number | typeof ALL_GROUPS_SENTINEL)[]>([])
const removedGroupIds = ref<number[]>([])
const pendingAccessAll = ref<boolean | null>(null)
const initialAccessAllGroups = ref(false)

watch(() => createForm.groupIds, (val) => {
  if (val.includes(ALL_GROUPS_SENTINEL)) {
    if (val.length > 1) {
      createForm.groupIds = [ALL_GROUPS_SENTINEL]
    }
    createForm.accessAllGroups = true
  } else {
    createForm.accessAllGroups = false
  }
})

const createRules: FormRules = {
  deptId: [{ required: true, message: '请选择或输入部门 ID', trigger: 'blur' }],
  name: [{ required: true, message: '请输入名称', trigger: 'blur' },
    { max: 60, message: '名称不超过 60 个字符', trigger: 'blur' }],
  purpose: [{ required: true, message: '请输入用途标签', trigger: 'blur' },
    { max: 40, message: '用途标签不超过 40 个字符', trigger: 'blur' }],
}

// 按角色过滤可选部门：admin 全部、studio_admin 仅自己工作室下的部门、dept_admin 仅自己的部门
const creatableDepts = computed(() => {
  const role = authStore.effectiveRole
  if (role === 'admin') return orgStore.departments

  const myStudioIds = new Set(
    authStore.orgRoles.filter(r => r.role === 'studio_admin').map(r => r.scope_id)
  )
  const myDeptIds = new Set(
    authStore.orgRoles.filter(r => r.role === 'dept_admin').map(r => r.scope_id)
  )

  if (role === 'studio_admin') {
    return orgStore.departments.filter(d => d.studio_id && myStudioIds.has(d.studio_id))
  }
  if (role === 'dept_admin') {
    return orgStore.departments.filter(d => myDeptIds.has(d.dept_id))
  }
  return orgStore.departments
})

// 只有 dept_admin 且只管辖 1 个部门时才锁定；多部门时显示下拉让其选择
const isDeptLocked = computed(() =>
  authStore.effectiveRole === 'dept_admin' && creatableDepts.value.length === 1
)

const keyRevealVisible = ref(false)
const newKeyPlainText = ref('')
const newKeyDept = ref('')
const newKeyPurpose = ref('')

// ── IP 白名单编辑 ──────────────────────────────────────────────
const ipEditDialogVisible = ref(false)
const ipEditingKey = ref<ServiceKeyItem | null>(null)
const ipEditList = ref<string[]>([])
const ipInputValue = ref('')
const ipSaving = ref(false)

// 创建对话框里的 IP 输入临时值
const ipCreateInput = ref('')

// ── 配额编辑 ──────────────────────────────────────────────
const quotaDialogVisible = ref(false)
const quotaEditingKey = ref<ServiceKeyItem | null>(null)
const quotaForm = ref<{ rpm: number | null; max_concurrency: number | null }>({ rpm: null, max_concurrency: null })
const quotaSaving = ref(false)
const quotaEffective = ref<QuotaEffective | null>(null)

async function openQuotaDialog(row: ServiceKeyItem) {
  quotaEditingKey.value = row
  quotaForm.value = { rpm: null, max_concurrency: null }
  quotaEffective.value = null
  quotaDialogVisible.value = true
  try {
    const res = await getServiceKeyQuota(row.id)
    const data = (res as any)?.data
    if (data) {
      quotaEffective.value = data
      if (data.source === 'override') {
        quotaForm.value.rpm = data.rpm
        quotaForm.value.max_concurrency = data.max_concurrency
      }
    }
  } catch { /* ignore */ }
}

async function saveQuota() {
  if (!quotaEditingKey.value) return
  quotaSaving.value = true
  try {
    await setServiceKeyQuota(quotaEditingKey.value.id, {
      rpm: quotaForm.value.rpm,
      max_concurrency: quotaForm.value.max_concurrency,
    })
    ElMessage.success('配额已保存')
    await openQuotaDialog(quotaEditingKey.value)
  } catch (e: any) {
    ElMessage.error(e?.message || '配额保存失败')
  } finally { quotaSaving.value = false }
}

async function clearQuota() {
  if (!quotaEditingKey.value) return
  quotaSaving.value = true
  try {
    await setServiceKeyQuota(quotaEditingKey.value.id, { rpm: null, max_concurrency: null })
    quotaForm.value = { rpm: null, max_concurrency: null }
    ElMessage.success('已清除，恢复为继承默认')
    await openQuotaDialog(quotaEditingKey.value)
  } catch (e: any) {
    ElMessage.error(e?.message || '清除失败')
  } finally { quotaSaving.value = false }
}

function openIPAllowlistDialog(row: ServiceKeyItem) {
  ipEditingKey.value = row
  ipEditList.value = row.ipAllowlist ? [...row.ipAllowlist] : []
  ipInputValue.value = ''
  ipEditDialogVisible.value = true
}

function addIP() {
  const v = ipInputValue.value.trim()
  if (!v) return
  if (!ipEditList.value.includes(v)) ipEditList.value.push(v)
  ipInputValue.value = ''
}

function removeIP(idx: number) {
  ipEditList.value.splice(idx, 1)
}

async function saveIPAllowlist() {
  if (!ipEditingKey.value) return
  ipSaving.value = true
  try {
    await updateServiceKeyIPAllowlist(ipEditingKey.value.id, ipEditList.value)
    ipEditingKey.value.ipAllowlist = [...ipEditList.value]
    ipEditDialogVisible.value = false
    ElMessage.success('IP 白名单已保存')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || e?.message || '保存失败')
  } finally {
    ipSaving.value = false
  }
}

function addCreateIP() {
  const v = ipCreateInput.value.trim()
  if (!v) return
  if (!createForm.ipAllowlist.includes(v)) createForm.ipAllowlist.push(v)
  ipCreateInput.value = ''
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso)
    if (isNaN(d.getTime())) return iso
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  } catch {
    return iso
  }
}

async function loadKeys() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await listServiceKeys(filterDeptId.value, currentPage.value, pageSize.value)
    keys.value = res.items
    total.value = res.total
  } catch (e: any) {
    loadError.value = e?.response?.data?.error?.message || e?.message || '未知错误'
    keys.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function onDeptFilterChange() {
  currentPage.value = 1
  loadKeys()
}

function onPageChange(page: number) {
  currentPage.value = page
  loadKeys()
}

function onSizeChange(size: number) {
  pageSize.value = size
  currentPage.value = 1
  loadKeys()
}

function openCreateDialog() {
  // dept_admin：自动锁定到自己的第一个部门
  if (isDeptLocked.value && creatableDepts.value.length > 0) {
    createForm.deptId = creatableDepts.value[0].dept_id
  } else {
    createForm.deptId = filterDeptId.value || ''
  }
  createForm.name = ''
  createForm.purpose = ''
  createForm.ipAllowlist = []
  createForm.groupIds = []
  createForm.accessAllGroups = false
  ipCreateInput.value = ''
  loadAvailableGroups()
  createDialogVisible.value = true
  setTimeout(() => createFormRef.value?.clearValidate(), 80)
}

async function loadAvailableGroups() {
  groupsLoading.value = true
  try {
    const resp = await getAvailableGroups()
    availableGroups.value = resp.data.groups || []
  } catch {
    ElMessage.error('加载模型组失败')
  } finally {
    groupsLoading.value = false
  }
}

function wantsAllGroupsMode(): boolean {
  return addGroupIds.value.includes(ALL_GROUPS_SENTINEL)
    && addGroupIds.value.filter(isRealGroupId).length === 0
}

function openGroupDialog(row: ServiceKeyItem) {
  editGroupKey.value = {
    ...row,
    boundGroups: [...(row.boundGroups || [])],
  }
  initialAccessAllGroups.value = row.accessAllGroups
  addGroupIds.value = row.accessAllGroups ? [ALL_GROUPS_SENTINEL] : []
  removedGroupIds.value = []
  pendingAccessAll.value = null
  groupDialogVisible.value = true
}

async function loadGroupDialogData() {
  groupLoading.value = true
  try {
    const resp = await getAvailableGroups()
    const all = resp.data.groups || []
    const bound = new Set((editGroupKey.value?.boundGroups || []).map((g) => g.id))
    availableAddGroups.value = all.filter((g) => !bound.has(g.id))
  } catch {
    ElMessage.error('加载模型组失败')
  } finally {
    groupLoading.value = false
  }
}

function removeGroup(g: BoundGroupInfo) {
  if (!editGroupKey.value) return
  editGroupKey.value.boundGroups = (editGroupKey.value.boundGroups || []).filter((x) => x.id !== g.id)
  removedGroupIds.value.push(g.id)
  addGroupIds.value = addGroupIds.value.filter((id) => id !== g.id)
}

function handleAccessAll(value: boolean) {
  if (!editGroupKey.value) return
  pendingAccessAll.value = value
  editGroupKey.value.accessAllGroups = value
  if (value) {
    addGroupIds.value = [ALL_GROUPS_SENTINEL]
    for (const g of editGroupKey.value.boundGroups || []) {
      if (!removedGroupIds.value.includes(g.id)) {
        removedGroupIds.value.push(g.id)
      }
    }
    editGroupKey.value.boundGroups = []
  } else {
    addGroupIds.value = addGroupIds.value.filter(isRealGroupId)
  }
}

function handleAddGroupChange(vals: (number | typeof ALL_GROUPS_SENTINEL)[]) {
  if (!editGroupKey.value) return

  if (vals.includes(ALL_GROUPS_SENTINEL)) {
    if (vals.length > 1) {
      addGroupIds.value = [ALL_GROUPS_SENTINEL]
    }
    pendingAccessAll.value = true
    for (const g of editGroupKey.value.boundGroups || []) {
      if (!removedGroupIds.value.includes(g.id)) {
        removedGroupIds.value.push(g.id)
      }
    }
    editGroupKey.value.boundGroups = []
    editGroupKey.value.accessAllGroups = true
    return
  }

  const wasAllMode = pendingAccessAll.value === true || editGroupKey.value.accessAllGroups
  if (wasAllMode) {
    pendingAccessAll.value = false
    editGroupKey.value.accessAllGroups = false
  }
  addGroupIds.value = vals.filter(isRealGroupId)
}

async function saveGroupChanges() {
  if (!editGroupKey.value) return
  const keyId = editGroupKey.value.id
  const add = addGroupIds.value.filter(isRealGroupId)
  const remove = removedGroupIds.value
  const targetAll = wantsAllGroupsMode()

  groupSaving.value = true
  try {
    if (targetAll) {
      // 先清绑定（此时 access_all 仍为 false），再开启全部可用组
      if (remove.length > 0 && !initialAccessAllGroups.value) {
        await updateServiceKeyGroups(keyId, [], remove)
      }
      if (!initialAccessAllGroups.value) {
        await toggleServiceKeyAccessAllGroups(keyId, true)
      }
    } else {
      // 绑具体组前必须先关闭 access_all_groups
      if (initialAccessAllGroups.value || pendingAccessAll.value === false) {
        await toggleServiceKeyAccessAllGroups(keyId, false)
      }
      if (add.length > 0 || remove.length > 0) {
        await updateServiceKeyGroups(keyId, add, remove)
      }
    }
    ElMessage.success('模型组权限已更新')
    groupDialogVisible.value = false
    await loadKeys()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || e?.message || '更新失败')
  } finally {
    groupSaving.value = false
  }
}

async function handleCreate() {
  if (!createFormRef.value) return
  try {
    await createFormRef.value.validate()
  } catch { return }

  creating.value = true
  try {
    const resp: CreateServiceKeyResponse = await createServiceKey(
      createForm.deptId.trim(),
      createForm.name.trim(),
      createForm.purpose.trim(),
      {
        ipAllowlist: createForm.ipAllowlist.length > 0 ? createForm.ipAllowlist : undefined,
        accessAllGroups: createForm.accessAllGroups || undefined,
        groupIds: !createForm.accessAllGroups && createForm.groupIds.length > 0
          ? createForm.groupIds.filter(isRealGroupId)
          : undefined,
      }
    )
    newKeyPlainText.value = resp.key
    const dept = orgStore.departments.find(d => d.dept_id === resp.dept_id)
    newKeyDept.value = dept ? `${dept.dept_name}（${resp.dept_id}）` : resp.dept_name || resp.dept_id
    newKeyPurpose.value = resp.purpose
    createDialogVisible.value = false
    keyRevealVisible.value = true
    currentPage.value = 1
    await loadKeys()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error?.message || e?.message || '创建失败')
  } finally {
    creating.value = false
  }
}

async function copyKey() {
  if (!newKeyPlainText.value) return
  try {
    await navigator.clipboard.writeText(newKeyPlainText.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动复制')
  }
}

async function handleToggle(row: ServiceKeyItem) {
  const newVal = !row.enabled
  try {
    await toggleServiceKey(row.id, newVal)
    row.enabled = newVal
    ElMessage.success(newVal ? '已启用' : '已停用')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error?.message || e?.message || '操作失败')
  }
}

async function handleDelete(row: ServiceKeyItem) {
  try {
    await deleteServiceKey(row.id)
    ElMessage.success('已吊销')
    await loadKeys()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error?.message || e?.message || '吊销失败')
  }
}

onMounted(async () => {
  await orgStore.fetchDepartments().catch(() => {})
  // dept_admin 单部门时自动锁定筛选；多部门时留空让用户自选
  if (authStore.effectiveRole === 'dept_admin' && isDeptLocked.value) {
    filterDeptId.value = creatableDepts.value[0]?.dept_id || ''
  }
  await loadKeys()
})
</script>

<style scoped>
.service-key-page {
  padding: 0;
  width: 100%;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 24px;
  gap: 16px;
  flex-wrap: wrap;
}

.header-info {
  flex: 1;
  min-width: 200px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size, 20px);
  font-weight: var(--font-title-weight, 600);
  margin: 0 0 4px 0;
}

.page-desc {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0;
  line-height: 1.5;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

.toolbar-field {
  width: 220px;
}

.brand-btn {
  background-color: #36B34E;
  border-color: #36B34E;
}

.brand-btn:hover,
.brand-btn:focus {
  background-color: #249089;
  border-color: #249089;
}

.brand-btn.is-loading {
  background-color: #7fc8c4;
  border-color: #7fc8c4;
}

.skeleton-wrap {
  padding: 8px 0;
}

.pagination-bar {
  display: flex;
  justify-content: flex-end;
  padding: 16px 0 4px;
}

/* 表格 */
.key-table {
  border-radius: 12px;
  overflow: hidden;
}

.dept-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.dept-name {
  font-size: 13px;
  color: var(--el-text-color-primary);
  font-weight: 500;
}

.dept-id {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.mono-muted {
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', Menlo, Monaco, monospace;
}

.key-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mono-key {
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', Menlo, Monaco, monospace;
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-lighter, #f5f7fa);
  padding: 2px 8px;
  border-radius: 4px;
  letter-spacing: 0.02em;
  word-break: break-all;
  line-height: 1.6;
}

html.dark .mono-key {
  background: #1f1f2e;
  color: #a0a8c0;
}

.copy-hint {
  color: var(--el-text-color-placeholder);
  cursor: help;
  flex-shrink: 0;
}

.purpose-tag {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  border-radius: 12px;
  letter-spacing: 0.02em;
}

.text-muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.text-placeholder {
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

/* Key 明文弹出框 */
:deep(.key-reveal-dialog .el-dialog) {
  border-radius: 16px;
}

:deep(.key-reveal-dialog .el-dialog__header) {
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding-bottom: 16px;
}

:deep(.key-reveal-dialog .el-dialog__body) {
  padding: 20px 24px;
}

.key-reveal-body {
  width: 100%;
}

.key-reveal-card {
  background: #1a1a2e;
  border-radius: 10px;
  padding: 20px 20px 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  border: 1px solid #2a2a4a;
}

html.dark .key-reveal-card {
  background: #0d0d1a;
  border-color: #2a2a4a;
}

.key-reveal-label {
  font-size: 12px;
  color: #8a92b0;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-weight: 600;
}

.key-reveal-value {
  display: flex;
  align-items: center;
  gap: 12px;
}

.key-reveal-code {
  flex: 1;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', Menlo, Monaco, monospace;
  font-size: 13px;
  color: #e0e8ff;
  background: transparent;
  word-break: break-all;
  line-height: 1.7;
  letter-spacing: 0.02em;
  user-select: all;
}

.key-copy-btn {
  flex-shrink: 0;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #c0c8e0;
  font-size: 12px;
  border-radius: 6px;
}

.key-copy-btn:hover,
.key-copy-btn:focus {
  background: rgba(255, 255, 255, 0.18);
  border-color: rgba(255, 255, 255, 0.35);
  color: #fff;
}

.key-reveal-meta {
  font-size: 12px;
  color: #6b7390;
  line-height: 1.6;
}

.key-meta-val {
  color: #a0a8c0;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
}

/* 响应式 */
@media (max-width: 900px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .header-actions {
    width: 100%;
    flex-wrap: wrap;
  }

  .toolbar-field {
    width: 100%;
  }

  .brand-btn {
    flex: 1;
  }

  .key-table {
    font-size: 13px;
  }

  .key-reveal-value {
    flex-direction: column;
    align-items: flex-start;
  }

  .key-copy-btn {
    width: 100%;
    justify-content: center;
  }
}

@media (max-width: 600px) {
  .mono-key {
    font-size: 11px;
    padding: 2px 6px;
  }

  .key-reveal-code {
    font-size: 12px;
  }
}

/* ── IP 白名单样式 ────────────────────────────────────── */
.ip-allowlist-tags {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.ip-tag {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
}

.ip-tag-more {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-left: 4px;
}

.ip-create-input-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.ip-create-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.ip-create-empty {
  margin-top: 4px;
  font-size: 12px;
}

.ip-edit-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ip-input-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.ip-tags-edit {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-height: 40px;
  padding: 8px 0;
  align-items: flex-start;
}

.ip-tag-edit {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
}

.ip-tag-text {
  font-size: 12px;
}

.form-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

.group-tag {
  margin-right: 4px;
  margin-bottom: 2px;
}

.bound-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 8px;
}

.group-all-tag {
  margin-bottom: 8px;
}

.group-all-hint {
  font-size: 13px;
  color: var(--el-color-success);
  margin-bottom: 8px;
}

/* Quota dialog styles */
.quota-edit-body { padding: 0 4px; }
.quota-hint { display: block; font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }
.quota-effective { margin-top: 16px; padding: 12px; background: var(--el-fill-color-light); border-radius: 8px; }
.quota-effective-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; color: var(--el-text-color-primary); }
.quota-effective-grid { display: flex; gap: 24px; }
.quota-effective-item { font-size: 13px; }
.quota-effective-item .label { color: var(--el-text-color-secondary); }
.quota-effective-item .value { font-weight: 600; color: var(--el-text-color-primary); }
</style>
