<template>
  <div class="group-page">
    <div class="page-header">
      <h2 class="page-title">模型组</h2>
      <div class="header-actions">
        <el-input
          v-model="search"
          placeholder="搜索模型组名称"
          clearable
          class="toolbar-field toolbar-field-lg"
          @input="handleSearch"
        />
        <el-button type="primary" @click="openCreateDialog">创建模型组</el-button>
      </div>
    </div>

    <div v-if="loading" class="state-wrapper">
      <el-skeleton :rows="5" animated />
    </div>

    <template v-else-if="error">
      <el-result icon="error" title="加载失败" sub-title="无法加载模型组列表，请稍后重试">
        <template #extra>
          <el-button type="primary" @click="loadGroups">重新加载</el-button>
        </template>
      </el-result>
    </template>

    <template v-else-if="isEmpty">
      <el-empty :description="search ? '未找到匹配的模型组' : '暂无模型组，点击上方按钮创建'" />
    </template>

    <template v-else>
      <div class="group-table-wrap"><el-table :data="groups" border stripe>
        <el-table-column prop="name" label="名称" min-width="160" show-overflow-tooltip />
        <el-table-column label="策略" width="120" align="center">
          <template #default="{ row }">
            <el-tag :type="strategyTagType(row.strategy)" size="small">
              {{ strategyLabel(row.strategy) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="开放模式" width="110" align="center">
          <template #default="{ row }">
            <template v-if="canManageEntitlements">
              <el-popconfirm
                :title="row.open_mode === 'public' ? '切换为受控？受控后需逐工作室/部门授权' : '切换为公开？全公司自动可用'"
                confirm-button-text="确定"
                cancel-button-text="取消"
                @confirm="handleToggleOpenMode(row)"
              >
                <template #reference>
                  <el-tag
                    :type="row.open_mode === 'public' ? 'success' : 'warning'"
                    size="small"
                    class="open-mode-tag"
                  >
                    {{ row.open_mode === 'public' ? '公开' : '受控' }}
                  </el-tag>
                </template>
              </el-popconfirm>
            </template>
            <el-tag
              v-else
              :type="row.open_mode === 'public' ? 'success' : 'warning'"
              size="small"
            >
              {{ row.open_mode === 'public' ? '公开' : '受控' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="模型数" width="100" align="center">
          <template #default="{ row }">
            {{ row.model_count ?? row.models?.length ?? 0 }}
          </template>
        </el-table-column>
        <el-table-column label="能力（聚合）" min-width="220">
          <template #default="{ row }">
            <div class="cap-cell">
              <el-tag v-if="row.is_capability_override" type="danger" size="small" effect="dark" class="cap-override-tag">手动</el-tag>
              <template v-if="capabilities[row.name]">
                <el-tag v-if="capabilities[row.name].supports_vision" type="success" size="small" effect="light">图像输入</el-tag>
                <el-tag v-if="capabilities[row.name].supports_function_calling" type="warning" size="small" effect="light">工具调用</el-tag>
                <el-tag v-if="capabilities[row.name].supports_tool_choice" type="warning" size="small" effect="plain">tool_choice</el-tag>
                <el-tag v-if="capabilities[row.name].max_input_tokens" type="info" size="small" effect="plain">
                  ctx {{ formatTokens(capabilities[row.name].max_input_tokens) }}
                </el-tag>
                <el-tag v-if="capabilities[row.name].max_output_tokens" type="info" size="small" effect="plain">
                  out {{ formatTokens(capabilities[row.name].max_output_tokens) }}
                </el-tag>
                <span
                  v-if="!hasAnyCapability(row.name)"
                  class="cap-empty"
                  title="组内并非全部成员都具备能力，或成员未绑定 Catalog"
                >无（成员未全绑定 Catalog）</span>
              </template>
              <span v-else class="cap-empty">—</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-switch
              :model-value="row.enabled"
              @change="(val: boolean) => handleToggleEnabled(row, val)"
            />
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170" align="center">
          <template #default="{ row }">
            {{ formatDateTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="配额" width="150" align="center">
          <template #default="{ row }">
            <span v-if="groupQuotaMap[row.id]" class="quota-cell">
              <template v-if="groupQuotaMap[row.id]!.source === 'override'">
                {{ groupQuotaMap[row.id]!.rpm === 0 ? '不限' : groupQuotaMap[row.id]!.rpm }} RPM /
                {{ groupQuotaMap[row.id]!.max_concurrency === 0 ? '不限' : groupQuotaMap[row.id]!.max_concurrency }} 并发
              </template>
              <span v-else class="text-placeholder">继承默认</span>
            </span>
            <span v-else class="text-placeholder">—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="340" align="center">
          <template #default="{ row }">
            <el-button v-if="canManageEntitlements" text type="primary" size="small" @click="openQuotaDialog(row)">配额</el-button>
            <el-button text type="primary" size="small" @click="router.push(`/admin/groups/${row.id}`)">详情</el-button>
            <el-button text type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
            <el-button text type="primary" size="small" @click="router.push(`/admin/groups/${row.id}?tab=capability`)">能力</el-button>
            <el-popconfirm
              title="确定删除该模型组？"
              confirm-button-text="确定"
              cancel-button-text="取消"
              @confirm="handleDelete(row)"
            >
              <template #reference>
                <el-button text type="danger" size="small">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table></div>

      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next, jumper"
          @current-change="loadGroups"
          @size-change="handleSizeChange"
        />
      </div>
    </template>

    <GroupEditDialog
      v-model:visible="dialogVisible"
      :group="editingGroup"
      @saved="onDialogSaved"
      @close="dialogVisible = false"
    />

    <GroupEntitlementDialog
      v-model:visible="entitlementDialogVisible"
      :group-id="entitlementGroup?.id ?? null"
      :group-name="entitlementGroup?.name ?? ''"
      @closed="entitlementGroup = null"
    />

    <QuotaEditDialog
      v-model:visible="quotaDialogVisible"
      subject-type="group"
      :subject-id="quotaSubjectId"
      :subject-label="quotaSubjectLabel"
      :default-rpm="QUOTA_DEFAULTS.group.rpm"
      :default-concurrency="QUOTA_DEFAULTS.group.max_concurrency"
      @saved="onQuotaSaved"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { groupsApi } from '@/api/groups'
import type { GroupCapability } from '@/api/groups'
import type { GroupListItem, Strategy, OpenMode } from '@/types/group'
import GroupEditDialog from '@/components/GroupEditDialog.vue'
import GroupEntitlementDialog from '@/components/GroupEntitlementDialog.vue'
import QuotaEditDialog from '@/components/QuotaEditDialog.vue'
import { QUOTA_DEFAULTS, getGroupQuota } from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const router = useRouter()
const groups = ref<GroupListItem[]>([])
const capabilities = ref<Record<string, GroupCapability>>({})
const loading = ref(false)
const error = ref(false)
const search = ref('')
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const dialogVisible = ref(false)
const editingGroup = ref<GroupListItem | null>(null)

const entitlementDialogVisible = ref(false)
const entitlementGroup = ref<GroupListItem | null>(null)

const quotaDialogVisible = ref(false)
const quotaSubjectId = ref('')
const quotaSubjectLabel = ref('')
const groupQuotaMap = ref<Record<string, QuotaEffective>>({})

const canManageEntitlements = computed(() => authStore.isAdmin)

const isEmpty = computed(() => {
  const val = groups.value
  return Array.isArray(val) ? val.length === 0 : false
})

const STRATEGY_MAP: Record<Strategy, string> = {
  round_robin: '轮询',
  priority: '优先级',
  weighted: '加权',
  auto: '自动',
}

function strategyLabel(strategy: Strategy): string {
  return STRATEGY_MAP[strategy] || strategy
}

function strategyTagType(strategy: Strategy): '' | 'success' | 'warning' | 'primary' {
  if (strategy === 'round_robin') return ''
  if (strategy === 'priority') return 'success'
  if (strategy === 'weighted') return 'warning'
  if (strategy === 'auto') return 'primary'
  return ''
}

function formatTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1000) return `${Math.round(n / 1000)}K`
  return String(n)
}

function hasAnyCapability(name: string): boolean {
  const c = capabilities.value[name]
  if (!c) return false
  return (
    c.supports_vision ||
    c.supports_function_calling ||
    c.supports_tool_choice ||
    c.max_input_tokens > 0 ||
    c.max_output_tokens > 0
  )
}

function formatDateTime(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

async function loadGroups() {
  loading.value = true
  error.value = false
  try {
    const res = await groupsApi.listGroups({
      search: search.value || undefined,
      page: currentPage.value,
      page_size: pageSize.value,
    })
    groups.value = res.data.data
    total.value = res.data.total
    // 并行加载模型组配额，供列表「配额」列回显
    loadGroupsQuotas(res.data.data)
    loadCapabilities()
  } catch {
    error.value = true
  } finally {
    loading.value = false
  }
}

// 拉取模型组聚合能力（失败不影响列表渲染，仅能力列显示 —）。
async function loadCapabilities() {
  try {
    const res = await groupsApi.getGroupCapabilities()
    capabilities.value = res.data.data || {}
  } catch {
    capabilities.value = {}
  }
}

function handleSearch() {
  currentPage.value = 1
  loadGroups()
}

function handleSizeChange() {
  currentPage.value = 1
  loadGroups()
}

function openCreateDialog() {
  editingGroup.value = null
  dialogVisible.value = true
}

function openEditDialog(row: GroupListItem) {
  editingGroup.value = row
  dialogVisible.value = true
}

async function handleDelete(row: GroupListItem) {
  try {
    await groupsApi.deleteGroup(row.id)
    ElMessage.success('删除成功')
    loadGroups()
  } catch {
    ElMessage.error('删除失败')
  }
}

async function handleToggleEnabled(row: GroupListItem, enabled: boolean) {
  try {
    await groupsApi.updateGroup(row.id, { enabled })
    row.enabled = enabled
    ElMessage.success(enabled ? '已启用' : '已禁用')
  } catch {
    ElMessage.error('状态切换失败')
  }
}

async function handleToggleOpenMode(row: GroupListItem) {
  const newMode: OpenMode = row.open_mode === 'public' ? 'controlled' : 'public'
  try {
    await groupsApi.setOpenMode(row.id, newMode)
    row.open_mode = newMode
    ElMessage.success(newMode === 'public' ? '已设为公开' : '已设为受控')
  } catch {
    ElMessage.error('设置开放模式失败')
  }
}

function openEntitlementDialog(row: GroupListItem) {
  entitlementGroup.value = row
  entitlementDialogVisible.value = true
}

function openQuotaDialog(group: GroupListItem) {
  quotaSubjectId.value = String(group.id)
  quotaSubjectLabel.value = `模型组：${group.name}`
  quotaDialogVisible.value = true
}

// 后端响应为 { code:0, data:{...} }，真实对象在 res.data.data
async function loadGroupQuota(groupId: string | number) {
  if (!groupId) return
  try {
    const res = await getGroupQuota(String(groupId))
    const data = (res as any)?.data?.data
    if (data) groupQuotaMap.value[String(groupId)] = data
  } catch {
    // 404 或错误：不填充，列显示占位符
  }
}

async function loadGroupsQuotas(items: GroupListItem[]) {
  await Promise.all(items.map(g => loadGroupQuota(g.id)))
}

function onQuotaSaved() {
  // 刷新该行配额，使列表列即时回显
  if (quotaSubjectId.value) {
    loadGroupQuota(quotaSubjectId.value)
  }
}

function onDialogSaved() {
  dialogVisible.value = false
  loadGroups()
}

onMounted(() => {
  loadGroups()
})
</script>

<style scoped>
.group-page {
  padding: 0;
}

.quota-cell { font-size: 13px; }
.text-placeholder { color: var(--el-text-color-placeholder); font-size: 13px; }

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

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.toolbar-field {
  width: 180px;
}

.toolbar-field-lg {
  width: 220px;
}

.state-wrapper {
  padding: 40px 0;
}

.cap-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.cap-empty {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.cap-override-tag {
  margin-right: 4px;
}

.open-mode-tag {
  cursor: pointer;
  user-select: none;
}

.open-mode-tag:hover {
  opacity: 0.8;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.group-table-wrap { width: 100%; }

@media (max-width: 900px) {
  .page-header {
    align-items: flex-start;
    flex-direction: column;
    gap: 12px;
  }

  .header-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .toolbar-field,
  .toolbar-field-lg {
    width: 100%;
  }

  .group-table-wrap { overflow-x: auto; }
  .group-table-wrap :deep(.el-table) { min-width: 750px; }

  .pagination-wrapper {
    justify-content: center;
  }
}

@media (max-width: 480px) {
  .header-actions { flex-wrap: wrap; gap: 8px; }
  .header-actions .el-button { width: 100%; }
}
</style>
