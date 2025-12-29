<template>
  <div class="expansion-page">
    <div class="page-header">
      <h2 class="page-title">扩容申请</h2>
    </div>

    <el-tabs v-model="activeTab" class="expansion-tabs">
      <el-tab-pane label="我的申请" name="mine">
        <div class="tab-content">
          <div class="tab-toolbar">
            <el-button type="primary" @click="openCreateDialog">新建申请</el-button>
          </div>

          <div v-loading="mineLoading" class="tab-body">
            <el-table v-if="mineList.length" :data="mineList" border stripe>
              <el-table-column label="申请对象" min-width="160">
                <template #default="{ row }">
                  <el-tag :color="scopeColor(row.scope_type)" class="scope-badge" effect="dark">
                    {{ scopeLabel(row.scope_type) }}
                  </el-tag>
                  <span class="scope-id">{{ row.scope_id }}</span>
                </template>
              </el-table-column>

              <el-table-column label="申请额度" width="160" align="center">
                <template #default="{ row }">
                  <span class="amount">${{ row.requested_amount.toFixed(2) }} USD</span>
                </template>
              </el-table-column>

              <el-table-column label="理由" min-width="200" show-overflow-tooltip>
                <template #default="{ row }">
                  {{ row.reason || '-' }}
                </template>
              </el-table-column>

              <el-table-column label="状态" width="120" align="center">
                <template #default="{ row }">
                  <span class="status-dot" :style="{ backgroundColor: STATUS_COLORS[row.status] }" />
                  <el-tag :color="STATUS_COLORS[row.status]" class="status-tag" effect="dark">
                    {{ STATUS_LABELS[row.status] }}
                  </el-tag>
                </template>
              </el-table-column>

              <el-table-column label="审批级别" width="120" align="center">
                <template #default="{ row }">
                  {{ LEVEL_LABELS[row.current_level] || row.current_level }}
                </template>
              </el-table-column>

              <el-table-column label="申请时间" width="170" align="center">
                <template #default="{ row }">
                  {{ formatDateTime(row.created_at) }}
                </template>
              </el-table-column>

              <el-table-column label="操作" width="100" align="center">
                <template #default="{ row }">
                  <el-popconfirm
                    v-if="row.status === 'pending'"
                    title="确定取消该申请？"
                    confirm-button-text="确定"
                    cancel-button-text="取消"
                    @confirm="handleCancel(row)"
                  >
                    <template #reference>
                      <el-button text type="danger" size="small">取消</el-button>
                    </template>
                  </el-popconfirm>
                  <span v-else class="text-muted">-</span>
                </template>
              </el-table-column>
            </el-table>

            <el-empty v-else-if="!mineLoading" description="暂无申请记录" />
          </div>
        </div>
      </el-tab-pane>

      <el-tab-pane v-if="isAdmin" label="待审批" name="pending">
        <div class="tab-content">
          <div v-loading="pendingLoading" class="tab-body">
            <div v-if="pendingList.length" class="pending-cards">
              <div
                v-for="item in pendingList"
                :key="item.id"
                class="pending-card"
                :style="{ borderLeftColor: levelColor(item.current_level) }"
              >
                <div class="card-header">
                  <div class="card-info">
                    <span class="requester">{{ item.requester_id }}</span>
                    <el-tag :color="scopeColor(item.scope_type)" class="scope-badge" effect="dark">
                      {{ scopeLabel(item.scope_type) }}
                    </el-tag>
                    <span class="scope-id">{{ item.scope_id }}</span>
                  </div>
                  <div class="card-amount">
                    ${{ item.requested_amount.toFixed(2) }} USD
                  </div>
                </div>

                <div class="card-reason">{{ item.reason || '无理由' }}</div>

                <div class="card-footer">
                  <span class="card-time">{{ formatDateTime(item.created_at) }}</span>
                  <div class="card-actions">
                    <el-button type="success" size="small" @click="openActionDialog(item.id, 'approve')">批准</el-button>
                    <el-button type="danger" size="small" @click="openActionDialog(item.id, 'reject')">拒绝</el-button>
                    <el-button type="primary" size="small" @click="openActionDialog(item.id, 'escalate')">升级</el-button>
                  </div>
                </div>
              </div>
            </div>

            <el-empty v-else-if="!pendingLoading" description="暂无待审批申请" />
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="createVisible" title="新建扩容申请" width="500px">
      <el-form :model="createForm" label-width="100px">
        <el-form-item label="申请类型">
          <el-select v-model="createForm.scope_type" placeholder="请选择">
            <el-option label="部门日额度" value="dept_daily" />
            <el-option label="部门服务额度" value="dept_service" />
            <el-option label="工作室额度" value="studio" />
          </el-select>
        </el-form-item>
        <el-form-item label="对象 ID">
          <el-input v-model="createForm.scope_id" placeholder="请输入 scope_id" />
        </el-form-item>
        <el-form-item label="申请额度">
          <el-input-number v-model="createForm.requested_amount" :min="0" :precision="2" controls-position="right" style="width: 100%" />
        </el-form-item>
        <el-form-item label="理由">
          <el-input v-model="createForm.reason" type="textarea" :rows="3" placeholder="请输入申请理由" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="createLoading" @click="handleCreate">提交</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="actionVisible" :title="actionTitle" width="400px">
      <el-input v-model="actionComment" type="textarea" :rows="3" placeholder="审批意见（可选）" />
      <template #footer>
        <el-button @click="actionVisible = false">取消</el-button>
        <el-button type="primary" :loading="actionLoading" @click="handleAction">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import {
  createExpansionRequest,
  getMyExpansionRequests,
  getPendingExpansions,
  decideExpansion
} from '@/services/control-plane'
import type {
  ExpansionRequest,
  CreateExpansionPayload,
  ExpansionStatus,
  ExpansionAction
} from '@/types/expansion'
import { STATUS_LABELS, STATUS_COLORS, LEVEL_LABELS } from '@/types/expansion'

const auth = useAuthStore()
const isAdmin = computed(() => ['studio_admin', 'admin'].includes(auth.effectiveRole || ''))

const activeTab = ref('mine')

const mineList = ref<ExpansionRequest[]>([])
const mineLoading = ref(false)

const pendingList = ref<ExpansionRequest[]>([])
const pendingLoading = ref(false)

const createVisible = ref(false)
const createLoading = ref(false)
const createForm = ref<CreateExpansionPayload>({
  scope_type: 'dept_daily',
  scope_id: '',
  requested_amount: 100,
  reason: ''
})

const actionVisible = ref(false)
const actionLoading = ref(false)
const actionRequestId = ref('')
const actionType = ref<ExpansionAction>('approve')
const actionComment = ref('')

const actionTitle = computed(() => {
  const map: Record<ExpansionAction, string> = {
    approve: '批准申请',
    reject: '拒绝申请',
    escalate: '升级申请',
    cancel: '取消申请'
  }
  return map[actionType.value]
})

async function loadMine() {
  mineLoading.value = true
  try {
    mineList.value = await getMyExpansionRequests()
  } catch {
    ElMessage.error('加载申请列表失败')
  } finally {
    mineLoading.value = false
  }
}

async function loadPending() {
  if (!isAdmin.value) return
  pendingLoading.value = true
  try {
    pendingList.value = await getPendingExpansions()
  } catch {
    ElMessage.error('加载待审批列表失败')
  } finally {
    pendingLoading.value = false
  }
}

function openCreateDialog() {
  createForm.value = { scope_type: 'dept_daily', scope_id: '', requested_amount: 100, reason: '' }
  createVisible.value = true
}

async function handleCreate() {
  if (!createForm.value.scope_id) {
    ElMessage.warning('请填写对象 ID')
    return
  }
  createLoading.value = true
  try {
    const created = await createExpansionRequest(createForm.value)
    mineList.value.unshift(created)
    ElMessage.success('申请已提交')
    createVisible.value = false
  } catch {
    ElMessage.error('提交失败')
  } finally {
    createLoading.value = false
  }
}

async function handleCancel(row: ExpansionRequest) {
  try {
    await decideExpansion(row.id, 'cancel')
    ElMessage.success('已取消')
    loadMine()
  } catch {
    ElMessage.error('取消失败')
  }
}

function openActionDialog(requestId: string, action: ExpansionAction) {
  actionRequestId.value = requestId
  actionType.value = action
  actionComment.value = ''
  actionVisible.value = true
}

async function handleAction() {
  actionLoading.value = true
  try {
    await decideExpansion(actionRequestId.value, actionType.value, actionComment.value || undefined)
    const map: Record<ExpansionAction, string> = {
      approve: '已批准',
      reject: '已拒绝',
      escalate: '已升级',
      cancel: '已取消'
    }
    ElMessage.success(map[actionType.value])
    actionVisible.value = false
    loadPending()
  } catch {
    ElMessage.error('操作失败')
  } finally {
    actionLoading.value = false
  }
}

function scopeLabel(type: string): string {
  const map: Record<string, string> = {
    dept_daily: '部门日额度',
    dept_service: '部门服务额度',
    studio: '工作室额度'
  }
  return map[type] || type
}

function scopeColor(type: string): string {
  const map: Record<string, string> = {
    dept_daily: '#3b82f6',
    dept_service: '#8b5cf6',
    studio: '#f59e0b'
  }
  return map[type] || '#6b7280'
}

function levelColor(level: string): string {
  return level === 'platform' ? '#ef4444' : '#f59e0b'
}

function formatDateTime(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

onMounted(() => {
  loadMine()
  if (isAdmin.value) loadPending()
})
</script>

<style scoped>
.expansion-page {
  padding: 0;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0;
}

.expansion-tabs {
  background: transparent;
}

.tab-content {
  padding: 16px 0;
}

.tab-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 16px;
}

.tab-body {
  min-height: 300px;
}

.scope-badge {
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 4px;
  margin-right: 8px;
}

.scope-id {
  color: var(--el-text-color-primary);
  font-family: monospace;
  font-size: 13px;
}

.amount {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-color-primary);
}

.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  vertical-align: middle;
}

.status-tag {
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 12px;
  vertical-align: middle;
}

.text-muted {
  color: var(--el-text-color-secondary);
}

.pending-cards {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.pending-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-left: 4px solid #f59e0b;
  border-radius: 12px;
  padding: 16px 20px;
  transition: box-shadow 0.2s;
}

.pending-card:hover {
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.card-info {
  display: flex;
  align-items: center;
  gap: 10px;
}

.requester {
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.card-amount {
  font-size: 18px;
  font-weight: 700;
  color: var(--el-color-primary);
}

.card-reason {
  color: var(--el-text-color-regular);
  margin-bottom: 12px;
  line-height: 1.5;
}

.card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-time {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.card-actions {
  display: flex;
  gap: 8px;
}

@media (max-width: 768px) {
  .card-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }

  .card-footer {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
}
</style>
