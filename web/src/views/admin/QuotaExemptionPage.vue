<template>
  <div class="exemption-page">
    <div class="page-header">
      <div class="header-info">
        <h2 class="page-title">配额豁免管理</h2>
        <p class="page-desc">为用户或 Service Key 创建配额豁免，豁免期间不受 RPM / 并发限制</p>
      </div>
      <div class="header-actions">
        <el-button type="primary" class="brand-btn" @click="openCreateDialog">
          <el-icon><Plus /></el-icon>
          创建豁免
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
        <el-button type="primary" @click="loadExemptions">重新加载</el-button>
      </template>
    </el-result>

    <el-empty
      v-else-if="!loading && exemptions.length === 0 && !loadError"
      description="暂无配额豁免，点击右上角创建"
    />

    <el-table
      v-else
      :data="exemptions"
      stripe
      highlight-current-row
      class="exemption-table"
      row-key="id"
    >
      <el-table-column label="主体" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          <span>{{ row.subject_name || row.subject_id }}</span>
          <el-tag :type="row.subject_type === 'user' ? 'primary' : 'warning'" size="small" round style="margin-left: 8px">
            {{ row.subject_type === 'user' ? '用户' : 'SK' }}
          </el-tag>
        </template>
      </el-table-column>

      <el-table-column label="原因" min-width="180" show-overflow-tooltip prop="reason" />

      <el-table-column label="过期时间" width="180">
        <template #default="{ row }">
          <span :class="expiryClass(row.expires_at)">
            {{ formatTime(row.expires_at) }}
          </span>
          <el-tag
            v-if="isExpired(row.expires_at)"
            type="danger"
            size="small"
            round
            style="margin-left: 6px"
          >已过期</el-tag>
          <el-tag
            v-else-if="isExpiringSoon(row.expires_at)"
            type="warning"
            size="small"
            round
            style="margin-left: 6px"
          >即将过期</el-tag>
        </template>
      </el-table-column>

      <el-table-column label="创建人" width="120" prop="created_by" show-overflow-tooltip />

      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          <span class="text-muted">{{ formatTime(row.created_at) }}</span>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="100" align="center" fixed="right">
        <template #default="{ row }">
          <el-popconfirm
            title="确定撤销此豁免？撤销后该主体将恢复配额限制"
            confirm-button-text="撤销"
            cancel-button-text="取消"
            confirm-button-type="danger"
            @confirm="handleDelete(row)"
          >
            <template #reference>
              <el-button type="danger" link size="small">撤销</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建豁免对话框 -->
    <el-dialog
      v-model="createDialogVisible"
      title="创建配额豁免"
      width="520px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form
        ref="createFormRef"
        :model="createForm"
        :rules="createRules"
        label-position="top"
        label-width="100px"
      >
        <el-form-item label="主体类型" prop="subject_type">
          <el-select v-model="createForm.subject_type" placeholder="选择主体类型" style="width: 100%" @change="onTypeChange">
            <el-option label="用户" value="user" />
            <el-option label="Service Key" value="service_key" />
          </el-select>
        </el-form-item>
        <el-form-item :label="createForm.subject_type === 'user' ? '选择用户' : '选择 Service Key'" prop="subject_id">
          <!-- 用户搜索 -->
          <el-select
            v-if="createForm.subject_type === 'user'"
            v-model="createForm.subject_id"
            filterable
            remote
            :remote-method="searchUser"
            :loading="userSearchLoading"
            placeholder="输入姓名或邮箱搜索"
            style="width: 100%"
            @change="onSubjectChange"
          >
            <el-option
              v-for="u in userSearchResults"
              :key="u.id"
              :label="`${u.name}（${u.email}）`"
              :value="u.id"
            />
          </el-select>
          <!-- Service Key 搜索 -->
          <el-select
            v-else
            v-model="createForm.subject_id"
            filterable
            remote
            :remote-method="searchServiceKey"
            :loading="skSearchLoading"
            placeholder="输入名称搜索"
            style="width: 100%"
            @change="onSubjectChange"
          >
            <el-option
              v-for="sk in skSearchResults"
              :key="sk.id"
              :label="`${sk.name}（${sk.deptName}）`"
              :value="sk.id"
            />
          </el-select>
          <div v-if="selectedSubjectLabel" class="form-hint">
            已选择：{{ selectedSubjectLabel }}
          </div>
        </el-form-item>
        <el-form-item label="豁免原因" prop="reason">
          <el-input
            v-model="createForm.reason"
            type="textarea"
            :rows="3"
            placeholder="说明豁免原因，如：紧急扩容、临时测试等"
            maxlength="500"
            show-word-limit
          />
        </el-form-item>
        <el-form-item label="过期时间" prop="expires_at">
          <el-date-picker
            v-model="createForm.expires_at"
            type="datetime"
            placeholder="选择过期时间"
            style="width: 100%"
            :disabled-date="(date: Date) => date.getTime() < Date.now() - 86400000"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
          />
          <div class="form-hint">豁免到期后自动失效，恢复配额限制</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" class="brand-btn" :loading="creating" @click="handleCreate">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  listQuotaExemptions,
  createQuotaExemption,
  deleteQuotaExemption,
} from '@/api/quota'
import type { QuotaExemption } from '@/api/quota'
import { useOrgStore } from '@/stores/org'
import type { UserSearchResult } from '@/stores/org'
import { listServiceKeys } from '@/services/control-plane'
import type { ServiceKeyItem } from '@/types/service-key'

const orgStore = useOrgStore()

const loading = ref(false)
const loadError = ref('')
const allExemptions = ref<QuotaExemption[]>([])

// 自动过滤：只显示未过期的豁免
const exemptions = computed(() =>
  allExemptions.value.filter(e => !isExpired(e.expires_at))
)

const createDialogVisible = ref(false)
const creating = ref(false)
const createFormRef = ref<FormInstance>()

const createForm = reactive({
  subject_type: 'user' as 'user' | 'service_key',
  subject_id: '',
  reason: '',
  expires_at: '',
})

const createRules: FormRules = {
  subject_type: [{ required: true, message: '请选择主体类型', trigger: 'change' }],
  subject_id: [
    { required: true, message: '请选择主体', trigger: 'change' },
  ],
  reason: [
    { required: true, message: '请输入豁免原因', trigger: 'blur' },
    { max: 500, message: '原因不超过 500 个字符', trigger: 'blur' },
  ],
  expires_at: [{ required: true, message: '请选择过期时间', trigger: 'change' }],
}

// ── 用户远程搜索 ──────────────────────────────────────────
const userSearchLoading = ref(false)
const userSearchResults = ref<UserSearchResult[]>([])

async function searchUser(keyword: string) {
  if (!keyword.trim()) {
    userSearchResults.value = []
    return
  }
  userSearchLoading.value = true
  try {
    userSearchResults.value = await orgStore.searchUsers(keyword, 20)
  } catch {
    userSearchResults.value = []
  } finally {
    userSearchLoading.value = false
  }
}

// ── Service Key 远程搜索 ──────────────────────────────────
const skSearchLoading = ref(false)
const skSearchResults = ref<ServiceKeyItem[]>([])

async function searchServiceKey(keyword: string) {
  if (!keyword.trim()) {
    skSearchResults.value = []
    return
  }
  skSearchLoading.value = true
  try {
    // 后端无 keyword 搜索，先拉全量再前端过滤
    const res = await listServiceKeys('', 1, 100)
    const kw = keyword.toLowerCase()
    skSearchResults.value = res.items.filter(
      sk => sk.name.toLowerCase().includes(kw) || sk.deptName?.toLowerCase().includes(kw)
    )
  } catch {
    skSearchResults.value = []
  } finally {
    skSearchLoading.value = false
  }
}

// ── 已选主体展示标签 ─────────────────────────────────────
const selectedSubjectLabel = computed(() => {
  if (!createForm.subject_id) return ''
  if (createForm.subject_type === 'user') {
    const u = userSearchResults.value.find(x => x.id === createForm.subject_id)
    return u ? `${u.name}（${u.email}）` : createForm.subject_id
  } else {
    const sk = skSearchResults.value.find(x => x.id === createForm.subject_id)
    return sk ? `${sk.name}（${sk.deptName}）` : createForm.subject_id
  }
})

function onSubjectChange() {
  createFormRef.value?.clearValidate('subject_id')
}

function onTypeChange() {
  createForm.subject_id = ''
  userSearchResults.value = []
  skSearchResults.value = []
  createFormRef.value?.clearValidate('subject_id')
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

function isExpired(expiresAt: string): boolean {
  return new Date(expiresAt).getTime() < Date.now()
}

function isExpiringSoon(expiresAt: string): boolean {
  const diff = new Date(expiresAt).getTime() - Date.now()
  return diff > 0 && diff < 24 * 60 * 60 * 1000
}

function expiryClass(expiresAt: string): string {
  if (isExpired(expiresAt)) return 'expiry-expired'
  if (isExpiringSoon(expiresAt)) return 'expiry-soon'
  return 'expiry-normal'
}

async function loadExemptions() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await listQuotaExemptions()
    const data = (res as any)?.data
    allExemptions.value = data?.data || []
  } catch (e: any) {
    loadError.value = e?.response?.data?.error || e?.message || '未知错误'
    allExemptions.value = []
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  createForm.subject_type = 'user'
  createForm.subject_id = ''
  createForm.reason = ''
  createForm.expires_at = ''
  userSearchResults.value = []
  skSearchResults.value = []
  createDialogVisible.value = true
  setTimeout(() => createFormRef.value?.clearValidate(), 80)
}

async function handleCreate() {
  if (!createFormRef.value) return
  try {
    await createFormRef.value.validate()
  } catch { return }

  creating.value = true
  try {
    await createQuotaExemption({
      subject_type: createForm.subject_type,
      subject_id: createForm.subject_id.trim(),
      reason: createForm.reason.trim(),
      expires_at: createForm.expires_at,
    })
    ElMessage.success('豁免已创建')
    createDialogVisible.value = false
    await loadExemptions()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || e?.message || '创建失败')
  } finally {
    creating.value = false
  }
}

async function handleDelete(row: QuotaExemption) {
  try {
    await deleteQuotaExemption(row.id)
    ElMessage.success('豁免已撤销')
    await loadExemptions()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || e?.message || '撤销失败')
  }
}

onMounted(() => {
  loadExemptions()
})
</script>

<style scoped>
.exemption-page {
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

.exemption-table {
  border-radius: 12px;
  overflow: hidden;
}

.mono-id {
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-lighter, #f5f7fa);
  padding: 2px 6px;
  border-radius: 4px;
  word-break: break-all;
}

.text-muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.expiry-expired {
  color: var(--el-color-danger);
  font-weight: 600;
}

.expiry-soon {
  color: var(--el-color-warning);
  font-weight: 600;
}

.expiry-normal {
  color: var(--el-color-success);
}

.form-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

@media (max-width: 900px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .header-actions {
    width: 100%;
  }

  .brand-btn {
    flex: 1;
  }
}
</style>
