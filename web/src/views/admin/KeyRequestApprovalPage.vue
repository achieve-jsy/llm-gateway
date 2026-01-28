<template>
  <div class="key-request-page">
    <header class="page-header">
      <div>
        <h1>Key 申请审批</h1>
        <p class="page-desc">审批部门成员的 Key 创建申请；可在部门设置中开启/关闭 Key 创建审批。</p>
      </div>
      <div class="header-actions">
        <el-button @click="load" :loading="loading">刷新</el-button>
      </div>
    </header>

    <el-tabs v-model="tab">
      <!-- 待审批（仅负责人可见） -->
      <el-tab-pane name="pending">
        <template #label>
          待审批
          <el-badge v-if="pendingCount > 0" :value="pendingCount" :max="99" class="tab-badge" />
        </template>

        <el-empty v-if="!loading && pendingItems.length === 0" description="暂无待审批申请" />

        <div v-else class="pending-list">
          <div v-for="item in pendingItems" :key="item.id" class="req-card">
            <div class="req-head">
              <div>
                <div class="req-title">{{ item.key_name }}</div>
                <div class="req-meta">
                  申请人：
                  <strong>{{ item.requester_name || item.requester_id }}</strong>
                  <span v-if="item.dept_name" class="req-dept">· {{ item.dept_name }}</span>
                </div>
              </div>
              <el-tag type="warning" size="small">审批中</el-tag>
            </div>

            <div class="req-body">
              <div class="req-info-row" v-if="item.purpose">
                <span class="req-label">用途说明：</span>
                <span>{{ item.purpose }}</span>
              </div>
              <div class="req-info-row" v-if="item.budget_monthly > 0">
                <span class="req-label">预算申请：</span>
                <el-tag size="small" type="success">${{ item.budget_monthly }} /月</el-tag>
              </div>
              <div class="req-info-row" v-if="item.expires_at">
                <span class="req-label">Key 有效期：</span>
                <span>{{ formatDate(item.expires_at) }}</span>
              </div>
              <div class="req-info-row text-muted">{{ formatDate(item.created_at) }} 提交</div>
            </div>

            <div class="req-actions">
              <el-popconfirm
                title="确认通过该申请？将自动创建 Key 并配置预算。"
                confirm-button-text="确认通过"
                @confirm="handleDecide(item.id, 'approve')"
              >
                <template #reference>
                  <el-button type="primary" size="small">通过</el-button>
                </template>
              </el-popconfirm>

              <el-button size="small" type="danger" plain @click="openRejectDialog(item)">驳回</el-button>
            </div>
          </div>

          <div v-if="loading" v-for="i in 3" :key="i" class="req-card skeleton">
            <div class="sk-line" style="width:40%; height:18px" />
            <div class="sk-line" style="width:70%; height:14px; margin-top:8px" />
          </div>
        </div>
      </el-tab-pane>

      <!-- 审批历史 -->
      <el-tab-pane name="history" label="审批历史">
        <el-empty v-if="!loadingHistory && historyItems.length === 0" description="暂无审批记录" />

        <div v-else class="history-list">
          <div
            v-for="item in historyItems"
            :key="item.id"
            class="req-card"
            :class="`req-card--${item.status}`"
          >
            <div class="req-head">
              <div>
                <div class="req-title">{{ item.key_name }}</div>
                <div class="req-meta">
                  申请人：
                  <strong>{{ item.requester_name || item.requester_id }}</strong>
                  <span v-if="item.dept_name" class="req-dept">· {{ item.dept_name }}</span>
                </div>
              </div>
              <el-tag :type="STATUS_COLORS[item.status]" size="small">
                {{ STATUS_LABELS[item.status] }}
              </el-tag>
            </div>
            <div class="req-body">
              <div class="req-info-row" v-if="item.purpose">
                <span class="req-label">用途：</span>
                <span>{{ item.purpose }}</span>
              </div>
              <div class="req-info-row" v-if="item.budget_monthly > 0">
                <span class="req-label">预算申请：</span>
                <span>${{ item.budget_monthly }} /月</span>
              </div>
              <div class="req-info-row" v-if="item.approve_comment">
                <span class="req-label">审批备注：</span>
                <span>{{ item.approve_comment }}</span>
              </div>
              <div class="req-info-row text-muted">{{ formatDate(item.updated_at) }}</div>
            </div>
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 部门策略配置（与 Tabs 同级的独立区块，仅管理角色可见） -->
    <div v-if="authStore.isManager" class="policy-section">
      <h3 class="section-title">部门 Key 创建策略</h3>
      <el-alert
        type="info"
        :closable="false"
        style="margin-bottom:16px"
      >
        控制本部门成员新建 Key 时是否需要审批。「自由创建」= 默认行为，可直接创建；「需要审批」= 创建时弹窗填写用途并提交审批，由部门负责人通过后才能生成 Key。
      </el-alert>
      <el-form label-width="120px" style="max-width: 480px">
        <el-form-item label="当前部门">
          <span class="current-dept">{{ authStore.user?.departmentName || '未分配' }}</span>
        </el-form-item>
        <el-form-item label="Key 创建策略">
          <el-radio-group
            v-model="deptPolicy"
            :disabled="policySaving"
            @change="handlePolicyChange"
          >
            <el-radio value="free">自由创建（不需要审批）</el-radio>
            <el-radio value="approval">需要审批（部门负责人批准后才创建）</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
    </div>

    <!-- 驳回弹窗 -->
    <el-dialog v-model="showRejectDialog" title="驳回申请" width="400px" @closed="rejectComment = ''">
      <el-input v-model="rejectComment" type="textarea" :rows="3" placeholder="填写驳回理由（必填）" maxlength="500" show-word-limit />
      <template #footer>
        <el-button @click="showRejectDialog = false">取消</el-button>
        <el-button type="danger" :disabled="!rejectComment.trim()" :loading="deciding" @click="confirmReject">确认驳回</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  getPendingKeyRequests,
  getPendingKeyRequestCount,
  getKeyRequestHistory,
  getDeptKeyCreationPolicy,
  setDeptKeyCreationPolicy,
  decideKeyRequest,
} from '@/services/control-plane'
import type { KeyRequest } from '@/types/key-request'
import { KEY_REQUEST_STATUS_LABELS as STATUS_LABELS, KEY_REQUEST_STATUS_COLORS as STATUS_COLORS } from '@/types/key-request'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const tab = ref('pending')
const loading = ref(false)
const loadingHistory = ref(false)
const pendingItems = ref<KeyRequest[]>([])
const pendingCount = ref(0)
const historyItems = ref<KeyRequest[]>([])

const showRejectDialog = ref(false)
const rejectingItem = ref<KeyRequest | null>(null)
const rejectComment = ref('')
const deciding = ref(false)

const deptPolicy = ref<'free' | 'approval'>('free')
const policySaving = ref(false)
const currentDeptId = ref('')

function formatDate(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function load() {
  loading.value = true
  try {
    const [items, count] = await Promise.all([
      getPendingKeyRequests(),
      getPendingKeyRequestCount(),
    ])
    pendingItems.value = items
    pendingCount.value = count
  } catch {
    pendingItems.value = []
    pendingCount.value = 0
  } finally {
    loading.value = false
  }
}

// 查询审批人已处理的申请（approved / rejected）
async function loadHistory() {
  loadingHistory.value = true
  try {
    historyItems.value = await getKeyRequestHistory(50)
  } catch {
    historyItems.value = []
  } finally {
    loadingHistory.value = false
  }
}

async function handleDecide(id: string, action: 'approve') {
  deciding.value = true
  try {
    await decideKeyRequest(id, action)
    ElMessage.success('已通过')
    await Promise.all([load(), loadHistory()])
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '操作失败')
  } finally {
    deciding.value = false
  }
}

function openRejectDialog(item: KeyRequest) {
  rejectingItem.value = item
  showRejectDialog.value = true
}

async function confirmReject() {
  if (!rejectingItem.value) return
  deciding.value = true
  try {
    await decideKeyRequest(rejectingItem.value.id, 'reject', rejectComment.value.trim())
    ElMessage.success('已驳回')
    showRejectDialog.value = false
    await Promise.all([load(), loadHistory()])
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '操作失败')
  } finally {
    deciding.value = false
  }
}

async function loadPolicy() {
  const deptId = (authStore.user as any)?.departmentId || ''
  currentDeptId.value = deptId
  if (!deptId) return
  try {
    const res = await getDeptKeyCreationPolicy(deptId)
    deptPolicy.value = (res.key_creation_policy as 'free' | 'approval') || 'free'
  } catch {
    deptPolicy.value = 'free'
  }
}

async function handlePolicyChange(val: string | number | boolean) {
  if (!currentDeptId.value) {
    ElMessage.warning('当前用户未分配部门，无法设置部门策略')
    return
  }
  policySaving.value = true
  try {
    await setDeptKeyCreationPolicy(currentDeptId.value, val as 'free' | 'approval')
    ElMessage.success('部门策略已更新')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '更新失败')
  } finally {
    policySaving.value = false
  }
}

watch(tab, (t) => {
  if (t === 'pending') load()
  else if (t === 'history') loadHistory()
})

onMounted(() => { load(); loadHistory(); loadPolicy() })
</script>

<style scoped>
.key-request-page { padding: 0; }
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
}
.page-header h1 {
  font-size: 22px;
  font-weight: 700;
  margin: 0 0 4px 0;
  color: var(--el-text-color-primary);
}
.page-desc { color: var(--el-text-color-secondary); font-size: 13px; margin: 0; }
.tab-badge { margin-left: 4px; }

.pending-list,
.mine-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 4px;
}

.req-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 16px 20px;
}
.req-card:hover { border-color: var(--el-border-color); }

.req-card--approved { border-left: 3px solid #10b981; }
.req-card--rejected { border-left: 3px solid #ef4444; }
.req-card--pending  { border-left: 3px solid #f59e0b; }
.req-card--cancelled { border-left: 3px solid #6b7280; }

.req-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; margin-bottom: 10px; }
.req-title { font-weight: 600; font-size: 15px; color: var(--el-text-color-primary); }
.req-meta { font-size: 13px; color: var(--el-text-color-secondary); margin-top: 3px; }
.req-dept { margin-left: 4px; }

.req-body { display: flex; flex-direction: column; gap: 4px; font-size: 13px; color: var(--el-text-color-regular); }
.req-info-row { display: flex; align-items: center; gap: 4px; }
.req-label { flex-shrink: 0; color: var(--el-text-color-secondary); }
.text-muted { color: var(--el-text-color-secondary); }

.req-actions { display: flex; gap: 8px; margin-top: 12px; }

.skeleton { opacity: 0.4; animation: pulse 1.2s infinite; }
.sk-line { background: var(--el-fill-color-light); border-radius: 4px; height: 14px; }
@keyframes pulse { 0%,100%{opacity:.4} 50%{opacity:.7} }

.policy-section {
  margin-top: 32px;
  padding-top: 24px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.section-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0 0 12px 0;
}

.current-dept {
  font-weight: 600;
  color: var(--el-color-primary);
}
</style>
