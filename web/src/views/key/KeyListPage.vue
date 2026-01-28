<template>
  <div class="key-list-page">
    <div class="page-header">
      <div>
        <h2 class="page-title">API 密钥</h2>
        <p class="page-desc">管理你的 Key{{ authStore.isManager ? '，审批团队成员的申请' : '' }}</p>
      </div>
      <div class="header-actions">
        <el-button type="primary" @click="handleCreateClick">{{ requiresApproval ? '申请创建 Key' : '创建 Key' }}</el-button>
      </div>
    </div>

    <el-alert v-if="requiresApproval" type="info" :closable="false" show-icon style="margin-bottom:16px">
      本部门开启了 Key 创建审批，新 Key 需经部门负责人审批后才会生效。
    </el-alert>

    <el-alert v-if="requiresApproval" type="info" :closable="false" show-icon style="margin-bottom:16px">
      本部门开启了 Key 创建审批，新 Key 需经部门负责人审批后才会生效。
    </el-alert>

    <!-- 多 Tab：存在「我的申请」「审批管理」任一时展示 -->
    <!-- 角色未加载完时保守渲染多 Tab 分支，避免首帧错位 -->
    <el-tabs v-if="hasMultiTab" v-model="activeTab" :key="tabsKey">
      <el-tab-pane label="我的密钥" name="keys">
        <!-- key table -->
        <div class="key-table-wrap">
          <el-empty v-if="keys.length === 0 && !loading" description="暂无 API 密钥">
            <el-button type="primary" @click="handleCreateClick">{{ requiresApproval ? '申请创建第一个 Key' : '创建第一个 Key' }}</el-button>
          </el-empty>
          <el-table v-else v-loading="loading" :data="keys" border stripe :default-sort="{ prop: 'createdAt', order: 'descending' }">
            <el-table-column prop="name" label="名称" min-width="120" show-overflow-tooltip />
            <el-table-column label="API 密钥" min-width="160">
              <template #default="{ row }"><span class="key-masked">{{ row.maskedKey }}</span><el-button size="small" text type="primary" @click.stop="copyFullKey(row)">复制</el-button></template>
            </el-table-column>
            <el-table-column label="我的模型" min-width="140">
              <template #default="{ row }">
                <template v-if="row.accessAllGroups"><el-tag size="small" type="success">全部模型</el-tag></template>
                <template v-else-if="row.boundGroups?.length"><el-tag v-for="g in row.boundGroups" :key="g.id" size="small" type="info" style="margin-right:4px;margin-bottom:2px">{{ g.name }}</el-tag></template>
                <span v-else class="text-muted">未分配</span>
                <el-button size="small" text type="primary" @click.stop="openGroupDialog(row)">管理</el-button>
              </template>
            </el-table-column>
            <el-table-column label="过期" width="150"><template #default="{ row }">{{ row.expiresAt ? formatDate(row.expiresAt) : '永久' }}</template></el-table-column>
            <el-table-column label="状态" width="100" align="center"><template #default="{ row }"><el-switch v-model="row.enabled" size="small" inline-prompt active-text="活跃" inactive-text="禁用" @change="(v: boolean) => handleToggle(row, v)" /></template></el-table-column>
            <el-table-column label="日志" width="95" align="center"><template #default="{ row }"><el-switch v-model="row.logEnabled" size="small" inline-prompt active-text="记录" inactive-text="关闭" @change="(v: boolean) => handleToggleLog(row, v)" /></template></el-table-column>
            <el-table-column label="创建时间" width="155" sortable prop="createdAt"><template #default="{ row }">{{ formatDate(row.createdAt) }}</template></el-table-column>
            <el-table-column label="操作" width="320" fixed="right" align="center">
              <template #default="{ row }">
                <el-button size="small" text type="primary" @click.stop="openUseKey(row)">使用密钥</el-button>
                <el-button size="small" text type="primary" @click.stop="openOpenCode(row)">配置 OpenCode</el-button>
                <el-button size="small" text type="warning" @click.stop="handleReset(row)">重置</el-button>
                <el-button size="small" text type="danger" @click.stop="handleDelete(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div v-if="total > pageSize" class="pagination-wrapper">
          <el-pagination v-model:current-page="currentPage" :page-size="pageSize" :total="total" layout="prev, pager, next" @current-change="handlePageChange" />
        </div>
      </el-tab-pane>

      <el-tab-pane v-if="showMyApplicationsTab" name="my-applications">
        <template #label>我的申请<el-badge v-if="myPendingCount>0" :value="myPendingCount" type="warning" /></template>
        <el-empty v-if="!myAppLoading && myApplications.length === 0" description="暂无申请记录" />
        <div v-else v-loading="myAppLoading" class="app-list">
          <div v-for="app in myApplications" :key="app.id" class="app-card" :class="'app-card--'+app.status">
            <div class="app-card-head">
              <span class="app-key-name">{{ app.key_name }}</span>
              <el-tag :type="STATUS_COLORS[app.status]" size="small">{{ STATUS_LABELS[app.status] }}</el-tag>
            </div>
            <div class="app-card-body" v-if="app.purpose || app.budget_monthly">
              <div class="info-row" v-if="app.purpose"><span class="info-label">用途</span><span>{{ app.purpose }}</span></div>
              <div class="info-row" v-if="app.budget_monthly > 0"><span class="info-label">预算</span><span>${{ app.budget_monthly }}/月</span></div>
              <div class="info-row" v-if="app.approve_comment"><span class="info-label">备注</span><span class="app-comment">{{ app.approve_comment }}</span></div>
            </div>
            <div class="app-card-foot">
              <span class="text-muted">{{ formatDate(app.created_at) }}</span>
              <el-button v-if="app.status === 'pending'" size="small" text type="danger" @click="handleCancelApp(app.id)">取消申请</el-button>
            </div>
          </div>
        </div>
      </el-tab-pane>

      <el-tab-pane v-if="authStore.isManager" name="approval">
        <template #label>审批管理<el-badge v-if="pendingCount>0" :value="pendingCount" type="danger" /></template>
        <div v-if="currentDeptId" class="policy-card" :class="{ 'policy-card--on': deptPolicy === 'approval' }">
          <div class="policy-card-head">
            <div>
              <h3 class="policy-title">部门 Key 创建审批<el-tag v-if="deptPolicy==='approval'" size="small" type="warning" style="margin-left:6px">已开启</el-tag><el-tag v-else size="small" style="margin-left:6px">未开启</el-tag></h3>
              <p class="policy-sub">部门：<b>{{ authStore.user?.departmentName || '未分配' }}</b></p>
            </div>
            <el-switch :model-value="deptPolicy==='approval'" :loading="policySaving" :disabled="policySaving" inline-prompt active-text="开" inactive-text="关" style="transform:scale(1.3)" @change="togglePolicy" />
          </div>
          <p class="policy-hint">开启后，本部门成员创建 Key 需先提交申请（可同时申请预算），由部门负责人审批通过后才会自动生成 Key。</p>
        </div>
        <div class="approval-sub">
          <el-radio-group v-model="approvalSub" size="small" @change="onApprovalSubChange">
            <el-radio-button label="pending">待审批<el-badge v-if="pendingCount>0" :value="pendingCount" type="danger" style="margin-left:4px" /></el-radio-button>
            <el-radio-button label="history">审批历史</el-radio-button>
          </el-radio-group>
        </div>
        <div v-if="approvalSub==='pending'" v-loading="pendingLoading">
          <el-empty v-if="!pendingItems.length" description="暂无待审批" />
          <div v-else class="app-list">
            <div v-for="item in pendingItems" :key="item.id" class="app-card app-card--pending">
              <div class="app-card-head">
                <div class="applicant-info">
                  <el-avatar :size="36" :style="{ backgroundColor: avatarColor(item.requester_name) }">{{ (item.requester_name||'?').charAt(0) }}</el-avatar>
                  <div><div class="applicant-name">{{ item.requester_name||'未知' }}</div><div v-if="item.dept_name" class="text-muted" style="font-size:12px">{{ item.dept_name }}</div></div>
                </div>
                <span class="text-muted">{{ formatDate(item.created_at) }}</span>
              </div>
              <div class="app-card-body">
                <div class="info-row"><span class="info-label">Key 名称</span><span>{{ item.key_name }}</span></div>
                <div class="info-row" v-if="item.purpose"><span class="info-label">用途</span><span>{{ item.purpose }}</span></div>
                <div class="info-row"><span class="info-label">预算</span><span>{{ item.budget_monthly>0 ? '$'+item.budget_monthly+'/月' : '无' }}</span></div>
              </div>
              <div class="app-card-actions">
                <el-popconfirm title="通过后自动创建 Key" @confirm="handleApprove(item.id)"><template #reference><el-button type="success" size="small">通过</el-button></template></el-popconfirm>
                <el-button size="small" type="danger" plain @click="openReject(item)">驳回</el-button>
              </div>
            </div>
          </div>
        </div>
        <div v-if="approvalSub==='history'" v-loading="historyLoading">
          <el-empty v-if="!historyItems.length" description="暂无历史" />
          <el-table v-else :data="historyItems" border stripe size="small">
            <el-table-column label="申请人" width="110"><template #default="{row}">{{ row.requester_name||'未知' }}</template></el-table-column>
            <el-table-column prop="key_name" label="Key 名称" width="140" />
            <el-table-column label="申请时间" width="150"><template #default="{row}">{{ formatDate(row.created_at) }}</template></el-table-column>
            <el-table-column label="处理时间" width="150"><template #default="{row}">{{ formatDate(row.updated_at) }}</template></el-table-column>
            <el-table-column label="状态" width="90"><template #default="{row}"><el-tag :type="STATUS_COLORS[row.status]" size="small">{{ STATUS_LABELS[row.status] }}</el-tag></template></el-table-column>
            <el-table-column label="预算" width="100"><template #default="{row}">{{ row.budget_monthly>0 ? '$'+row.budget_monthly+'/月' : '-' }}</template></el-table-column>
            <el-table-column label="备注" min-width="140"><template #default="{row}">{{ row.approve_comment||'-' }}</template></el-table-column>
          </el-table>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 单 Tab：只展示「我的密钥」，无 Tabs 栏 -->
    <template v-else>
      <div class="key-table-wrap">
        <el-empty v-if="keys.length === 0 && !loading" description="暂无 API 密钥">
          <el-button type="primary" @click="handleCreateClick">{{ requiresApproval ? '申请创建第一个 Key' : '创建第一个 Key' }}</el-button>
        </el-empty>
        <el-table v-else v-loading="loading" :data="keys" border stripe :default-sort="{ prop: 'createdAt', order: 'descending' }">
          <el-table-column prop="name" label="名称" min-width="120" show-overflow-tooltip />
          <el-table-column label="API 密钥" min-width="160">
            <template #default="{ row }"><span class="key-masked">{{ row.maskedKey }}</span><el-button size="small" text type="primary" @click.stop="copyFullKey(row)">复制</el-button></template>
          </el-table-column>
          <el-table-column label="我的模型" min-width="140">
            <template #default="{ row }">
              <template v-if="row.accessAllGroups"><el-tag size="small" type="success">全部模型</el-tag></template>
              <template v-else-if="row.boundGroups?.length"><el-tag v-for="g in row.boundGroups" :key="g.id" size="small" type="info" style="margin-right:4px;margin-bottom:2px">{{ g.name }}</el-tag></template>
              <span v-else class="text-muted">未分配</span>
              <el-button size="small" text type="primary" @click.stop="openGroupDialog(row)">管理</el-button>
            </template>
          </el-table-column>
          <el-table-column label="过期" width="150"><template #default="{ row }">{{ row.expiresAt ? formatDate(row.expiresAt) : '永久' }}</template></el-table-column>
          <el-table-column label="状态" width="100" align="center"><template #default="{ row }"><el-switch v-model="row.enabled" size="small" inline-prompt active-text="活跃" inactive-text="禁用" @change="(v: boolean) => handleToggle(row, v)" /></template></el-table-column>
          <el-table-column label="日志" width="95" align="center"><template #default="{ row }"><el-switch v-model="row.logEnabled" size="small" inline-prompt active-text="记录" inactive-text="关闭" @change="(v: boolean) => handleToggleLog(row, v)" /></template></el-table-column>
          <el-table-column label="创建时间" width="155" sortable prop="createdAt"><template #default="{ row }">{{ formatDate(row.createdAt) }}</template></el-table-column>
          <el-table-column label="操作" width="320" fixed="right" align="center">
            <template #default="{ row }">
              <el-button size="small" text type="primary" @click.stop="openUseKey(row)">使用密钥</el-button>
              <el-button size="small" text type="primary" @click.stop="openOpenCode(row)">配置 OpenCode</el-button>
              <el-button size="small" text type="warning" @click.stop="handleReset(row)">重置</el-button>
              <el-button size="small" text type="danger" @click.stop="handleDelete(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div v-if="total > pageSize" class="pagination-wrapper">
        <el-pagination v-model:current-page="currentPage" :page-size="pageSize" :total="total" layout="prev, pager, next" @current-change="handlePageChange" />
      </div>
    </template>

    <KeyCreateDialog v-model="showCreateDialog" @success="onKeyCreated" />
    <KeyApplyDialog v-model="showApplyDialog" @submitted="onAppSubmitted" />
    <UseKeyModal v-model="showUsageModal" :api-key-id="usageKeyId" :key-hint="usageKeyHint" />
    <OpenCodeConfigDialog v-model:visible="showOpenCode" :api-key-id="openCodeKeyId" :masked-key="openCodeMaskedKey" />

    <el-dialog v-model="showGroupDialog" title="管理我的模型" width="500px" destroy-on-close @opened="loadGroupDialogData">
      <div v-if="editGroupKey">
        <div class="group-section"><h4>当前可用模型</h4>
          <el-tag v-if="editGroupKey.accessAllGroups" size="large" type="success" closable @close="handleAccessAll(false)">全部模型</el-tag>
          <div v-else-if="editGroupKey.boundGroups?.length" class="bound-tags"><el-tag v-for="g in editGroupKey.boundGroups" :key="g.id" closable size="large" type="info" @close="removeGroup(g)">{{ g.name }}</el-tag></div>
          <span v-else class="text-muted">未分配</span>
        </div>
        <div class="group-section"><h4>添加模型</h4>
          <el-select v-model="addGroupIds" multiple filterable placeholder="选择模型" style="width:100%" :loading="groupLoading" @change="handleAddGroupChange">
            <el-option key="__all__" label="全部模型（不限制）" value="__all__" :disabled="editGroupKey.accessAllGroups" />
            <el-option v-for="g in availableAddGroups" :key="g.id" :label="g.name" :value="g.id" :disabled="editGroupKey.accessAllGroups" />
          </el-select>
        </div>
      </div>
      <template #footer><el-button @click="showGroupDialog=false">取消</el-button><el-button type="primary" :loading="groupSaving" @click="saveGroupChanges">保存</el-button></template>
    </el-dialog>

    <el-dialog v-model="showReject" title="驳回申请" width="420px">
      <p style="margin-bottom:8px;color:var(--el-text-color-secondary)">请填写驳回原因：</p>
      <el-input v-model="rejectReason" type="textarea" :rows="3" placeholder="请填写驳回原因..." maxlength="200" show-word-limit />
      <template #footer><el-button @click="showReject=false">取消</el-button><el-button type="danger" :disabled="!rejectReason.trim()" @click="confirmReject">确认驳回</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listKeys, disableKey, enableKey, deleteKey, resetKey, revealKey,
  updateKeyGroups, getAvailableGroups, toggleAccessAllGroups, toggleLogEnabled,
  getKeyCreationPolicy, getMyKeyRequests, getMyKeyRequestSummary,
  decideKeyRequest, getPendingKeyRequests, getPendingKeyRequestCount,
  getKeyRequestHistory, getDeptKeyCreationPolicy, setDeptKeyCreationPolicy,
} from '@/services/control-plane'
import { mapKeysResponse, mapCreateKeyResponse, type BoundGroupInfo } from '@/types/api-key'
import type { ApiKey, AvailableGroup } from '@/types/api-key'
import type { KeyRequest } from '@/types/key-request'
import { KEY_REQUEST_STATUS_LABELS as STATUS_LABELS, KEY_REQUEST_STATUS_COLORS as STATUS_COLORS } from '@/types/key-request'
import KeyCreateDialog from '@/components/key/KeyCreateDialog.vue'
import KeyApplyDialog from '@/components/key/KeyApplyDialog.vue'
import UseKeyModal from '@/components/key/UseKeyModal.vue'
import OpenCodeConfigDialog from '@/components/key/OpenCodeConfigDialog.vue'
import { copyToClipboard } from '@/utils/clipboard'
import { useSettingsStore } from '@/stores/settings'
import { useKeyCacheStore } from '@/stores/key-cache'
import { useAuthStore } from '@/stores/auth'

function formatDate(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${p(d.getMonth()+1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

const authStore = useAuthStore()
const keyCache = useKeyCacheStore()
const activeTab = ref('keys')
const loading = ref(false)
const keys = ref<ApiKey[]>([])
const currentPage = ref(1)
const total = ref(0)
const pageSize = 20
const showCreateDialog = ref(false)
const showApplyDialog = ref(false)
const requiresApproval = ref(false)
const showUsageModal = ref(false)
const usageKeyId = ref<string | null>(null)
const usageKeyHint = ref('')
const showOpenCode = ref(false)
const openCodeKeyId = ref('')
const openCodeMaskedKey = ref('')
const showGroupDialog = ref(false)
const editGroupKey = ref<ApiKey | null>(null)
const addGroupIds = ref<(string|number)[]>([])
const removedGroupIds = ref<number[]>([])
const availableAddGroups = ref<AvailableGroup[]>([])
const groupLoading = ref(false)
const groupSaving = ref(false)
const pendingAccessAll = ref<boolean|null>(null)
const myAppLoading = ref(false)
const myApplications = ref<KeyRequest[]>([])
const myPendingCount = ref(0)
const currentDeptId = ref('')
const deptPolicy = ref<'free'|'approval'>('free')
const policySaving = ref(false)
const approvalSub = ref<'pending'|'history'>('pending')
const pendingLoading = ref(false)
const pendingItems = ref<KeyRequest[]>([])
const pendingCount = ref(0)
const historyLoading = ref(false)
const historyItems = ref<KeyRequest[]>([])
const showReject = ref(false)
const rejectReason = ref('')
const rejectingItem = ref<KeyRequest|null>(null)

const showMyApplicationsTab = computed(() => requiresApproval.value && !authStore.isManager)

// ── 关键：角色未加载完时保守渲染多 Tab 分支 ──
// 避免：首帧 single-tab 分支渲染后 isManager 才变 true 触发重渲染导致「审批管理」缺失
const hasMultiTab = computed(() => {
  if (authStore.effectiveRole === null) return true
  return showMyApplicationsTab.value || authStore.isManager
})
const tabsKey = computed(() => 'keys-' + (showMyApplicationsTab.value ? 'app' : '') + (authStore.isManager ? '-approval' : ''))

watch(showMyApplicationsTab, (v) => { if (!v && activeTab.value === 'my-applications') activeTab.value = 'keys' })

function avatarColor(n?: string): string {
  const c = ['#409EFF','#67C23A','#E6A23C','#F56C6C','#909399','#8B5CF6','#EC4899','#14B8A6']
  if (!n) return c[0]; let h = 0; for (let i = 0; i < n.length; i++) h = n.charCodeAt(i) + ((h << 5) - h)
  return c[Math.abs(h) % c.length]
}

async function loadKeys() { loading.value = true; try { const r = await listKeys(currentPage.value, pageSize); keys.value = mapKeysResponse(r); total.value = r.data.total } catch { ElMessage.error('加载失败') } finally { loading.value = false } }
function handlePageChange(p: number) { currentPage.value = p; loadKeys() }
async function copyFullKey(row: ApiKey) { try { const r = await revealKey(row.id); await copyToClipboard(r.key); ElMessage.success('已复制') } catch { ElMessage.warning('获取失败') } }
function openUseKey(row: ApiKey) { usageKeyId.value = row.id; usageKeyHint.value = row.keyHint; showUsageModal.value = true }
function openOpenCode(row: ApiKey) { openCodeKeyId.value = row.id; openCodeMaskedKey.value = row.maskedKey; showOpenCode.value = true }
async function handleToggle(row: ApiKey, en: boolean) { try { if (en) await enableKey(row.id); else { await ElMessageBox.confirm('确定禁用「'+row.name+'」？','确认',{type:'warning'}); await disableKey(row.id) } ElMessage.success(en?'已启用':'已禁用'); await loadKeys() } catch { row.enabled = !en } }
async function handleToggleLog(row: ApiKey, v: boolean) { try { await toggleLogEnabled(row.id, v); ElMessage.success(v?'已开启日志':'已关闭日志') } catch { row.logEnabled = !v; ElMessage.error('操作失败') } }
async function handleReset(row: ApiKey) { try { await ElMessageBox.confirm('重置「'+row.name+'」，旧密钥立即失效。确定？','确认',{type:'warning'}); const r = await resetKey(row.id); const m = mapCreateKeyResponse(r); keyCache.cacheKey(m.id,m.key); await ElMessageBox.alert('<pre style="background:var(--el-fill-color-light);padding:10px;border-radius:6px;font-size:13px;white-space:pre-wrap;word-break:break-all">'+m.key+'</pre>','新密钥',{dangerouslyUseHTMLString:true,confirmButtonText:'关闭'}); await loadKeys() } catch {} }
async function handleDelete(row: ApiKey) { try { await ElMessageBox.confirm('删除「'+row.name+'」不可恢复，确定？','确认',{type:'error'}); await deleteKey(row.id); keyCache.clearKey(row.id); ElMessage.success('已删除'); await loadKeys() } catch {} }
function onKeyCreated() { showCreateDialog.value = false; currentPage.value = 1; loadKeys() }

function openGroupDialog(row: ApiKey) { editGroupKey.value = { ...row, boundGroups: [...(row.boundGroups||[])] }; addGroupIds.value = []; removedGroupIds.value = []; pendingAccessAll.value = null; showGroupDialog.value = true }
async function loadGroupDialogData() { groupLoading.value = true; try { const r = await getAvailableGroups(); const all = r.data.groups||[]; const bound = new Set((editGroupKey.value?.boundGroups||[]).map((g:BoundGroupInfo)=>g.id)); availableAddGroups.value = all.filter((g:AvailableGroup)=>!bound.has(g.id)) } catch { ElMessage.error('加载失败') } finally { groupLoading.value = false } }
function removeGroup(g: BoundGroupInfo) { if(!editGroupKey.value) return; editGroupKey.value.boundGroups = (editGroupKey.value.boundGroups||[]).filter(x=>x.id!==g.id); removedGroupIds.value.push(g.id); addGroupIds.value = addGroupIds.value.filter(id=>id!==g.id) }
function handleAddGroupChange(vals: (string|number)[]) { if(vals.includes('__all__')) { addGroupIds.value = []; pendingAccessAll.value = true; if(editGroupKey.value){ for(const g of editGroupKey.value.boundGroups||[]) removedGroupIds.value.push(g.id); editGroupKey.value.boundGroups = []; editGroupKey.value.accessAllGroups = true } } }
function handleAccessAll(v: boolean) { if(!editGroupKey.value) return; pendingAccessAll.value = v; editGroupKey.value.accessAllGroups = v }
async function saveGroupChanges() { if(!editGroupKey.value) return; groupSaving.value = true; try { if(pendingAccessAll.value!==null) await toggleAccessAllGroups(editGroupKey.value.id,pendingAccessAll.value); const add = addGroupIds.value.filter((v):v is number=>typeof v==='number'); const remove = removedGroupIds.value; if(add.length>0||remove.length>0) await updateKeyGroups(editGroupKey.value.id,add,remove); ElMessage.success('已更新'); showGroupDialog.value = false; await loadKeys() } catch(e:any){ ElMessage.error(e?.response?.data?.message||'更新失败') } finally { groupSaving.value = false } }

async function handleCreateClick() { if (authStore.isManager) { showCreateDialog.value = true; return }; try { const p = await getKeyCreationPolicy(); if(p.requires_approval) showApplyDialog.value = true; else showCreateDialog.value = true } catch { showCreateDialog.value = true } }

async function loadMyApplications() { myAppLoading.value = true; try { myApplications.value = await getMyKeyRequests() } catch { myApplications.value = [] } finally { myAppLoading.value = false } }
async function loadRequestSummary() { try { const s = await getMyKeyRequestSummary(); myPendingCount.value = s.pending||0 } catch {} }
async function handleCancelApp(id: string) { try { await ElMessageBox.confirm('确定取消？','取消',{type:'warning'}); await decideKeyRequest(id,'cancel'); ElMessage.success('已取消'); loadMyApplications(); loadRequestSummary() } catch {} }
function onAppSubmitted() { showApplyDialog.value = false; loadMyApplications(); loadRequestSummary() }

function getJwtDeptId(): string { try { const t = localStorage.getItem('access_token'); if(!t) return ''; const p = t.split('.')[1].replace(/-/g,'+').replace(/_/g,'/'); return JSON.parse(decodeURIComponent(atob(p).split('').map(c=>'%'+('00'+c.charCodeAt(0).toString(16)).slice(-2)).join(''))).dept_id||'' } catch { return '' } }
async function loadPolicy() { const id = (authStore.user as any)?.departmentId || getJwtDeptId() || ''; currentDeptId.value = id; if(!id) return; try { const r = await getDeptKeyCreationPolicy(id); deptPolicy.value = (r.key_creation_policy as 'free'|'approval')||'free' } catch { deptPolicy.value = 'free' } }
async function loadRequiresApproval() { try { const p = await getKeyCreationPolicy(); requiresApproval.value = p.requires_approval } catch {} }
async function togglePolicy() { if(!currentDeptId.value) return; const nv = deptPolicy.value==='approval'?'free':'approval'; const prev = deptPolicy.value; deptPolicy.value = nv; policySaving.value = true; try { await setDeptKeyCreationPolicy(currentDeptId.value,nv); requiresApproval.value = nv==='approval'; ElMessage.success(nv==='approval'?'已开启':'已关闭') } catch(e:any){ deptPolicy.value = prev; ElMessage.error(e?.response?.data?.error||'更新失败') } finally { policySaving.value = false } }

async function loadPending() { pendingLoading.value = true; try { const [i,c] = await Promise.all([getPendingKeyRequests(),getPendingKeyRequestCount()]); pendingItems.value = i||[]; pendingCount.value = c||0 } catch { pendingItems.value = []; pendingCount.value = 0 } finally { pendingLoading.value = false } }
async function loadHistory() { historyLoading.value = true; try { historyItems.value = await getKeyRequestHistory(50) } catch { historyItems.value = [] } finally { historyLoading.value = false } }
function onApprovalSubChange(v: string|number|boolean) { if(v==='history') loadHistory(); else loadPending() }
async function handleApprove(id: string) { try { await decideKeyRequest(id,'approve'); ElMessage.success('已通过'); await Promise.all([loadPending(),loadHistory()]) } catch(e:any){ ElMessage.error(e?.response?.data?.error||'操作失败') } }
function openReject(item: KeyRequest) { rejectingItem.value = item; rejectReason.value = ''; showReject.value = true }
async function confirmReject() { if(!rejectingItem.value||!rejectReason.value.trim()) return; try { await decideKeyRequest(rejectingItem.value.id,'reject',rejectReason.value.trim()); ElMessage.success('已驳回'); showReject.value = false; await Promise.all([loadPending(),loadHistory()]) } catch(e:any){ ElMessage.error(e?.response?.data?.error||'操作失败') } finally { rejectingItem.value = null } }

onMounted(() => { loadKeys(); loadRequestSummary(); loadMyApplications(); loadRequiresApproval(); loadPolicy(); useSettingsStore().loadPublicSettings(); if(authStore.isManager) loadPending() })
watch(() => authStore.isManager, (v) => { if (v) loadPending() })
</script>

<style scoped>
.key-list-page { padding: 0; }
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.page-title { font-size: 22px; font-weight: 700; margin: 0; }
.page-desc { font-size: 13px; color: var(--el-text-color-secondary); margin: 4px 0 0; }
.header-actions { flex-shrink: 0; }
.key-cell { display: flex; align-items: center; gap: 6px; }
.key-masked { font-family: 'SF Mono',monospace; font-size: 13px; background: var(--el-fill-color-light); padding: 2px 10px; border-radius: 4px; color: var(--el-text-color-secondary); }
.text-muted { color: var(--el-text-color-secondary); font-size: 13px; }
.pagination-wrapper { display: flex; justify-content: flex-end; margin-top: 16px; }
.bound-tags { min-height: 32px; }
.group-section { margin-bottom: 16px; }
.group-section h4 { margin: 0 0 8px; font-size: 14px; }
.key-table-wrap { width: 100%; }
.app-list { display: flex; flex-direction: column; gap: 12px; margin-top: 8px; }
.app-card { border: 1px solid var(--el-border-color-lighter); border-radius: 8px; padding: 16px 20px; background: var(--el-bg-color); }
.app-card:hover { border-color: var(--el-border-color); }
.app-card--approved { border-left: 3px solid #10b981; }
.app-card--rejected { border-left: 3px solid #ef4444; }
.app-card--pending  { border-left: 3px solid #f59e0b; }
.app-card--cancelled { border-left: 3px solid #6b7280; }
.app-card-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.app-key-name { font-weight: 600; font-size: 15px; }
.applicant-info { display: flex; align-items: center; gap: 10px; }
.applicant-name { font-weight: 500; }
.app-card-body { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
.info-row { display: flex; gap: 6px; }
.info-label { color: var(--el-text-color-secondary); flex-shrink: 0; min-width: 60px; }
.app-comment { background: var(--el-fill-color-lighter); padding: 2px 6px; border-radius: 4px; font-size: 12px; }
.app-card-foot { flex-shrink: 0; }
</style>
