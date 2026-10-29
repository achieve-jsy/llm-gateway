<template>
  <div class="group-detail-page">
    <!-- 顶部面包屑 + 标题 -->
    <div class="page-header">
      <div class="header-breadcrumb">
        <el-button link @click="router.push('/admin/groups')">
          <el-icon><ArrowLeft /></el-icon> 模型组
        </el-button>
        <span class="bc-sep">/</span>
        <span class="bc-current">{{ group?.name || '加载中...' }}</span>
      </div>
      <div class="header-meta" v-if="group">
        <el-tag :type="group.enabled ? 'success' : 'info'" size="small">{{ group.enabled ? '启用' : '禁用' }}</el-tag>
        <el-tag :type="modelTypeTagType(group.model_type)" size="small">{{ modelTypeLabel(group.model_type) }}</el-tag>
        <el-tag v-if="group.model_type !== 'video'" type="info" size="small">{{ strategyLabel(group.strategy) }}</el-tag>
      </div>
    </div>

    <div v-if="pageLoading" v-loading="true" style="height: 200px" />
    <div v-else-if="!group" class="state-error">
      <p>模型组不存在或无权访问</p>
      <el-button @click="router.push('/admin/groups')">返回列表</el-button>
    </div>

    <el-tabs v-else v-model="activeTab" class="detail-tabs">
      <!-- Tab 1: 基本信息 -->
      <el-tab-pane label="基本信息" name="info">
        <el-descriptions :column="2" border size="small" class="info-desc">
          <el-descriptions-item label="名称">{{ group.name }}</el-descriptions-item>
          <el-descriptions-item label="类型">{{ modelTypeLabel(group.model_type) }}</el-descriptions-item>
          <el-descriptions-item label="策略">{{ group?.model_type === 'video' ? '—' : strategyLabel(group.strategy) }}</el-descriptions-item>
          <el-descriptions-item label="模型数量">{{ group.model_count }}</el-descriptions-item>
          <el-descriptions-item label="会话粘性" v-if="group?.model_type !== 'video'">{{ stickyLabel(group.sticky_enabled) }}<template v-if="group.sticky_ttl_seconds"> ({{ group.sticky_ttl_seconds }}s)</template></el-descriptions-item>
          <el-descriptions-item label="状态">{{ group.enabled ? '启用' : '禁用' }}</el-descriptions-item>
          <el-descriptions-item label="创建时间" :span="2">{{ group.created_at }}</el-descriptions-item>
        </el-descriptions>
      </el-tab-pane>

      <!-- Tab 2: 模型成员 -->
      <el-tab-pane label="模型成员" name="models">
        <div v-loading="modelsLoading">
          <el-table :data="models" stripe size="small" empty-text="暂无模型成员">
            <el-table-column label="模型名称" prop="model_name" min-width="200" />
            <el-table-column label="厂商" prop="provider_name" width="120" />
            <el-table-column label="权重" prop="weight" width="80" align="center" v-if="showWeightColumn" />
            <el-table-column label="优先级" prop="priority" width="80" align="center" v-if="showPriorityColumn" />
            <el-table-column label="复杂度档位" prop="complexity_tier" width="110" align="center" v-if="showComplexityColumn">
              <template #default="scope">
                <el-tag v-if="scope.row.complexity_tier" size="small">{{ scope.row.complexity_tier }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <!-- Tab 3: 授权管理 -->
      <el-tab-pane label="授权管理" name="entitlements">
        <div class="entitlement-section">
          <!-- 开放模式切换 -->
          <div class="open-mode-row">
            <div>
              <div class="om-title">开放模式</div>
              <div class="om-desc">公开：所有部门默认可用；受控：需单独授权</div>
            </div>
            <el-switch
              v-model="isPublic"
              active-text="公开"
              inactive-text="受控"
              :loading="modeSwitching"
              @change="onModeChange"
            />
          </div>

          <template v-if="!isPublic">
            <!-- 工作室授权 -->
            <div class="entitle-block">
              <div class="entitle-block-head">
                <span>工作室授权</span>
                <el-button size="small" type="primary" plain @click="openGrant('studio')">+ 添加</el-button>
              </div>
              <el-table :data="studioEntitlements" stripe size="small" empty-text="暂无工作室授权">
                <el-table-column label="工作室 ID" prop="scope_id" min-width="160" />
                <el-table-column label="添加时间" prop="created_at" width="160" />
                <el-table-column label="操作" width="80" align="center">
                  <template #default="{ row }">
                    <el-button link type="danger" size="small" @click="handleRevoke(row)">撤销</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <!-- 部门授权 -->
            <div class="entitle-block">
              <div class="entitle-block-head">
                <span>部门开通</span>
                <el-button size="small" type="primary" plain @click="openGrant('dept')">+ 添加</el-button>
              </div>
              <el-table :data="deptEntitlements" stripe size="small" empty-text="暂无部门开通">
                <el-table-column label="部门 ID" prop="scope_id" min-width="160" />
                <el-table-column label="添加时间" prop="created_at" width="160" />
                <el-table-column label="操作" width="80" align="center">
                  <template #default="{ row }">
                    <el-button link type="danger" size="small" @click="handleRevoke(row)">撤销</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </template>
          <div v-else class="public-hint">
            <el-icon><InfoFilled /></el-icon>
            公开模式下，所有部门默认可见可用，无需单独授权。
          </div>
        </div>
      </el-tab-pane>

      <!-- Tab 4: 能力设置 -->
      <el-tab-pane label="能力设置" name="capability">
        <div class="capability-section">
          <div class="cap-header">
            <div>
              <div class="cap-title">手动覆盖聚合能力</div>
              <div class="cap-desc">默认从成员 Catalog 自动聚合；开启后完全由管理员手动设定，不再随 Catalog 变化</div>
            </div>
            <div class="cap-actions">
              <el-switch
                v-model="capForm.is_capability_override"
                active-text="手动"
                inactive-text="自动"
                @change="onCapToggle"
              />
            </div>
          </div>

          <template v-if="capForm.is_capability_override">
            <el-form :model="capForm" label-width="180px" size="small" class="cap-form">
              <el-form-item label="图像输入 (Vision)">
                <el-switch v-model="capForm.override_supports_vision" />
              </el-form-item>
              <el-form-item label="工具调用 (Function Calling)">
                <el-switch v-model="capForm.override_supports_function_calling" />
              </el-form-item>
              <el-form-item label="Tool Choice">
                <el-switch v-model="capForm.override_supports_tool_choice" />
              </el-form-item>
              <el-form-item label="视频输入 (Video)">
                <el-switch v-model="capForm.override_supports_video_input" />
                <span style="margin-left: 12px; color: #e6a23c; font-size: 12px">
                  开启后整组成员都按「支持视频」路由，请先确认组内上游都接受 video_url
                </span>
              </el-form-item>
              <el-form-item label="Mode">
                <el-input
                  v-model="capForm.override_mode"
                  placeholder="留空 = 继承成员聚合值"
                  clearable
                  style="width: 240px"
                />
              </el-form-item>
              <el-form-item label="上下文长度">
                <el-input-number
                  v-model="capForm.override_context_length"
                  :min="1"
                  :controls="false"
                  placeholder="留空 = 继承聚合值"
                  style="width: 240px"
                />
              </el-form-item>
              <el-form-item label="最大输出">
                <el-input-number
                  v-model="capForm.override_max_output_tokens"
                  :min="1"
                  :controls="false"
                  placeholder="留空 = 继承聚合值"
                  style="width: 240px"
                />
              </el-form-item>
            </el-form>

            <div class="cap-btn-row">
              <el-button type="primary" :loading="capSaving" @click="saveCapability">保存</el-button>
              <el-popconfirm title="确定重置回自动聚合？手动设定的值将被清除" @confirm="resetCapability">
                <template #reference>
                  <el-button :loading="capSaving">重置为自动</el-button>
                </template>
              </el-popconfirm>
            </div>
          </template>

          <div v-else class="cap-auto-hint">
            <span>当前为自动聚合模式，能力字段由组内成员 Catalog 计算得出。</span>
            <template v-if="capAggregated">
              <el-tag v-if="capAggregated.mode" type="info" size="small" effect="light">mode: {{ capAggregated.mode }}</el-tag>
              <el-tag v-if="capAggregated.max_input_tokens > 0" type="info" size="small" effect="light">上下文: {{ capAggregated.max_input_tokens }}</el-tag>
              <el-tag v-if="capAggregated.max_output_tokens > 0" type="info" size="small" effect="light">输出: {{ capAggregated.max_output_tokens }}</el-tag>
              <el-tag v-if="capAggregated.supports_vision" type="success" size="small" effect="light">图像输入</el-tag>
              <el-tag v-if="capAggregated.supports_function_calling" type="warning" size="small" effect="light">工具调用</el-tag>
              <el-tag v-if="capAggregated.supports_tool_choice" type="warning" size="small" effect="plain">tool_choice</el-tag>
              <el-tag v-if="capAggregated.supports_video_input" type="success" size="small" effect="plain">视频输入</el-tag>
            </template>
          </div>
        </div>
     </el-tab-pane>
   </el-tabs>

    <!-- 配额设置 Tab -->
    <el-tab-pane label="配额设置" name="quota">
      <div class="quota-section">
        <el-alert
          type="info"
          :closable="false"
          show-icon
          style="margin-bottom: 20px"
        >
          <template #title>
            配额说明：设置此模型组的请求频率和并发限制。0 = 不限制，留空 = 继承默认值。
          </template>
        </el-alert>

        <div v-if="groupQuota" class="quota-current">
          <div class="quota-current-title">当前生效配额</div>
          <div class="quota-current-grid">
            <div class="quota-current-item">
              <span class="label">RPM：</span>
              <span class="value">{{ groupQuota.rpm === 0 ? '不限制' : groupQuota.rpm }}</span>
              <el-tag
                size="small"
                :type="groupQuota.source === 'override' ? 'success' : 'info'"
                style="margin-left: 6px"
              >
                {{ groupQuota.source === 'override' ? '自定义' : '继承默认' }}
              </el-tag>
            </div>
            <div class="quota-current-item">
              <span class="label">并发：</span>
              <span class="value">{{ groupQuota.max_concurrency === 0 ? '不限制' : groupQuota.max_concurrency }}</span>
            </div>
          </div>
        </div>
        <div v-else class="quota-current quota-current-empty">配额数据加载中…</div>

        <el-button type="primary" @click="openQuotaDialog">编辑配额</el-button>
      </div>
    </el-tab-pane>

    <!-- 配额编辑弹窗 -->
    <QuotaEditDialog
      v-model:visible="quotaDialogVisible"
      subject-type="group"
      :subject-id="quotaSubjectId"
      :subject-label="quotaSubjectLabel"
      :default-rpm="QUOTA_DEFAULTS.group.rpm"
      :default-concurrency="QUOTA_DEFAULTS.group.max_concurrency"
      @saved="onQuotaSaved"
    />

   <!-- 授权弹窗 -->
    <el-dialog v-model="grantVisible" title="添加授权" width="380px">
      <el-form label-width="80px">
        <el-form-item label="类型">
          <el-tag>{{ grantForm.scope_type === 'studio' ? '工作室' : '部门' }}</el-tag>
        </el-form-item>
        <el-form-item label="ID">
          <el-input v-model="grantForm.scope_id" :placeholder="`输入${grantForm.scope_type === 'studio' ? '工作室' : '部门'} ID`" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="grantVisible = false">取消</el-button>
        <el-button type="primary" :loading="grantSaving" @click="handleGrant">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, InfoFilled } from '@element-plus/icons-vue'
import { groupsApi } from '@/api/groups'
import type { GroupCapability } from '@/api/groups'
import { getGroupEntitlements, grantEntitlement, revokeEntitlement, setGroupOpenMode } from '@/services/control-plane'
import type { GroupEntitlement } from '@/types/entitlement'
import QuotaEditDialog from '@/components/QuotaEditDialog.vue'
import { QUOTA_DEFAULTS, getGroupQuota } from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'

const route = useRoute()
const router = useRouter()
const groupId = computed(() => Number(route.params.id))

const activeTab = ref((route.query.tab as string) || 'info')
const pageLoading = ref(false)
const modelsLoading = ref(false)
const modeSwitching = ref(false)
const grantVisible = ref(false)
const grantSaving = ref(false)
const grantForm = ref<{ scope_type: 'studio' | 'dept'; scope_id: string }>({ scope_type: 'studio', scope_id: '' })

const group = ref<any>(null)
const models = ref<any[]>([])
const entitlements = ref<GroupEntitlement[]>([])

const isPublic = ref(false)
const studioEntitlements = computed(() => entitlements.value.filter(e => e.scope_type === 'studio'))
const deptEntitlements = computed(() => entitlements.value.filter(e => e.scope_type === 'dept'))

const capForm = ref({
  is_capability_override: false,
  override_supports_vision: false,
  override_supports_function_calling: false,
  override_supports_tool_choice: false,
  override_supports_video_input: false,
  override_mode: '',
  override_context_length: null as number | null,
  override_max_output_tokens: null as number | null,
})
const capSaving = ref(false)
const capAggregated = ref<GroupCapability | null>(null)

// ─── 配额设置 ───
const quotaDialogVisible = ref(false)
const quotaSubjectId = ref('')
const quotaSubjectLabel = ref('')
const groupQuota = ref<QuotaEffective | null>(null)

// 后端响应为 { code:0, data:{...} }，真实对象在 res.data.data
async function loadGroupQuota() {
  if (!groupId.value) return
  try {
    const res = await getGroupQuota(String(groupId.value))
    groupQuota.value = (res as any)?.data?.data ?? null
  } catch {
    groupQuota.value = null
  }
}

function openQuotaDialog() {
  quotaSubjectId.value = String(groupId.value)
  quotaSubjectLabel.value = `模型组：${group.value?.name || groupId.value}`
  quotaDialogVisible.value = true
}

function onQuotaSaved() {
  // 刷新当前配额，使「配额设置」Tab 即时回显
  loadGroupQuota()
}

function strategyLabel(s: string): string {
  const map: Record<string, string> = { round_robin: '轮询', priority: '优先级', weighted: '权重轮询', auto: '自动选择' }
  return map[s] || s
}

const showWeightColumn = computed(() => group.value?.model_type !== 'video' && group.value?.strategy === 'weighted')
const showPriorityColumn = computed(() => group.value?.model_type !== 'video' && group.value?.strategy === 'priority')
const showComplexityColumn = computed(() => group.value?.model_type !== 'video' && group.value?.strategy === 'auto')

function stickyLabel(enabled: boolean | null | undefined): string {
  if (enabled === null || enabled === undefined) return '跟随全局'
  return enabled ? '开启' : '关闭'
}

function modelTypeLabel(t?: string): string {
  const map: Record<string, string> = {
    chat: 'Chat',
    embedding: 'Embedding',
    embedding_vl: 'Embedding VL',
    rerank: 'Rerank',
    video: 'Video',
  }
  if (!t) return 'Chat'
  return map[t] || t
}

function modelTypeTagType(t?: string): '' | 'success' | 'warning' | 'info' | 'danger' {
  switch (t) {
    case 'embedding':
    case 'embedding_vl':
      return 'success'
    case 'rerank':
      return 'warning'
    case 'video':
      return 'danger'
    default:
      return 'info'
  }
}

async function loadCapabilities() {
  try {
    const res = await groupsApi.getGroupCapabilities([group.value?.name || ''])
    const data = (res.data as any)?.data || res.data
    capAggregated.value = data?.[group.value?.name || ''] || null
  } catch {
    capAggregated.value = null
  }
}

function syncCapFormFromGroup(g: any) {
  if (!g) return
  capForm.value = {
    is_capability_override: !!g.is_capability_override,
    override_supports_vision: !!g.override_supports_vision,
    override_supports_function_calling: !!g.override_supports_function_calling,
    override_supports_tool_choice: !!g.override_supports_tool_choice,
    override_supports_video_input: !!g.override_supports_video_input,
    override_mode: g.override_mode || '',
    override_context_length: g.override_context_length ?? null,
    override_max_output_tokens: g.override_max_output_tokens ?? null,
  }
}

async function loadGroup() {
  pageLoading.value = true
  try {
    const res = await groupsApi.getGroup(groupId.value)
    group.value = res.data?.data || res.data
    isPublic.value = (group.value?.open_mode || 'controlled') === 'public'
    syncCapFormFromGroup(group.value)
  } catch { group.value = null }
  finally { pageLoading.value = false }
}

async function loadModels() {
  modelsLoading.value = true
  try {
    const res = await groupsApi.listGroupModels(groupId.value)
    models.value = res.data?.data || []
  } catch { models.value = [] }
  finally { modelsLoading.value = false }
}

async function loadEntitlements() {
  try {
    entitlements.value = await getGroupEntitlements(groupId.value)
  } catch { entitlements.value = [] }
}

async function onModeChange(val: boolean) {
  modeSwitching.value = true
  try {
    await setGroupOpenMode(groupId.value, val ? 'public' : 'controlled')
    ElMessage.success('已更新，约 30 秒后对用户生效')
  } catch (e: any) {
    isPublic.value = !val
    ElMessage.error(e?.message || '更新失败')
  } finally { modeSwitching.value = false }
}

function openGrant(type: 'studio' | 'dept') {
  grantForm.value = { scope_type: type, scope_id: '' }
  grantVisible.value = true
}

async function handleGrant() {
  if (!grantForm.value.scope_id) { ElMessage.warning('请填写 ID'); return }
  grantSaving.value = true
  try {
    await grantEntitlement({ group_id: groupId.value, scope_type: grantForm.value.scope_type, scope_id: grantForm.value.scope_id })
    ElMessage.success('授权成功')
    grantVisible.value = false
    await loadEntitlements()
  } catch (e: any) { ElMessage.error(e?.message || '授权失败') }
  finally { grantSaving.value = false }
}

async function handleRevoke(row: GroupEntitlement) {
  try {
    await revokeEntitlement({ group_id: groupId.value, scope_type: row.scope_type, scope_id: row.scope_id })
    ElMessage.success('已撤销')
    await loadEntitlements()
  } catch (e: any) { ElMessage.error(e?.message || '撤销失败') }
}

function onCapToggle(val: boolean | string | number) {
  if (!val) {
    capForm.value.is_capability_override = false
    capForm.value = { ...capForm.value, is_capability_override: false }
  }
}

async function saveCapability() {
  capSaving.value = true
  try {
    const res = await groupsApi.updateGroup(groupId.value, {
      is_capability_override: capForm.value.is_capability_override,
      override_supports_vision: capForm.value.override_supports_vision,
      override_supports_function_calling: capForm.value.override_supports_function_calling,
      override_supports_tool_choice: capForm.value.override_supports_tool_choice,
      override_supports_video_input: capForm.value.override_supports_video_input,
      override_mode: capForm.value.override_mode || '',
      override_context_length: capForm.value.override_context_length ?? null,
      override_max_output_tokens: capForm.value.override_max_output_tokens ?? null,
    })
    group.value = { ...group.value, ...(res.data as any)?.data }
    ElMessage.success('能力设置已保存')
    await loadCapabilities()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally { capSaving.value = false }
}

async function resetCapability() {
  capSaving.value = true
  try {
    const res = await groupsApi.resetCapability(groupId.value)
    const updated = (res.data as any)?.data
    if (updated) {
      group.value = { ...group.value, ...updated }
      syncCapFormFromGroup(group.value)
    }
    ElMessage.success('已重置为自动聚合')
    await loadCapabilities()
  } catch (e: any) {
    ElMessage.error(e?.message || '重置失败')
  } finally { capSaving.value = false }
}

onMounted(async () => {
  await loadGroup()
  await Promise.all([loadModels(), loadEntitlements(), loadCapabilities(), loadGroupQuota()])
})
</script>

<style scoped>
.group-detail-page { padding: 24px; max-width: 960px; margin: 0 auto; }
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.header-breadcrumb { display: flex; align-items: center; gap: 6px; font-size: 14px; }
.bc-sep { color: var(--el-text-color-placeholder); }
.bc-current { font-weight: 600; color: var(--el-text-color-primary); }
.header-meta { display: flex; gap: 8px; }
.state-error { text-align: center; padding: 60px; color: var(--el-text-color-secondary); }
.detail-tabs { }
.info-desc { margin-top: 16px; }
.entitlement-section { display: flex; flex-direction: column; gap: 20px; padding-top: 16px; }
.open-mode-row { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px; border: 1px solid var(--el-border-color-light); border-radius: 10px; }
.om-title { font-weight: 600; font-size: 14px; color: var(--el-text-color-primary); }
.om-desc { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.entitle-block { }
.entitle-block-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; font-size: 14px; font-weight: 500; color: var(--el-text-color-regular); }
.public-hint { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--el-text-color-secondary); padding: 12px 16px; background: var(--el-fill-color-light); border-radius: 8px; }

.capability-section { display: flex; flex-direction: column; gap: 20px; padding-top: 16px; }
.cap-header { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px; border: 1px solid var(--el-border-color-light); border-radius: 10px; }
.cap-title { font-weight: 600; font-size: 14px; color: var(--el-text-color-primary); }
.cap-desc { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.cap-form { padding: 12px 16px; border: 1px solid var(--el-border-color-lighter); border-radius: 8px; }
.cap-btn-row { display: flex; gap: 8px; padding: 0 4px; }
.cap-auto-hint { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; font-size: 13px; color: var(--el-text-color-secondary); padding: 12px 16px; background: var(--el-fill-color-light); border-radius: 8px; }
.quota-section { padding-top: 16px; }
.quota-hint { margin-left: 12px; font-size: 12px; color: var(--el-text-color-secondary); }
.quota-current { margin-bottom: 16px; padding: 12px 16px; background: var(--el-fill-color-light); border-radius: 8px; }
.quota-current-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; color: var(--el-text-color-primary); }
.quota-current-grid { display: flex; gap: 24px; }
.quota-current-item { font-size: 13px; display: flex; align-items: center; }
.quota-current-item .label { color: var(--el-text-color-secondary); }
.quota-current-item .value { font-weight: 600; color: var(--el-text-color-primary); margin-left: 4px; }
.quota-current-empty { font-size: 13px; color: var(--el-text-color-placeholder); }
</style>
