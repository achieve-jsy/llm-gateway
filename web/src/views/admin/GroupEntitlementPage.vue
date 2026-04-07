<template>
  <div class="entitlement-page">
    <div class="page-header">
      <div>
        <h2 class="page-title">模型组授权管理</h2>
        <p class="page-subtitle">控制哪些模型组对哪些部门可见可用</p>
      </div>
      <div class="header-actions">
        <el-input
          v-model="searchQuery"
          placeholder="搜索模型组"
          clearable
          style="width: 220px"
          @input="onSearchDebounced"
        />
      </div>
    </div>

    <div v-loading="pageLoading" class="group-list">
      <div
        v-for="group in filteredGroups"
        :key="group.id"
        class="group-card"
      >
        <div class="group-header" @click="toggleExpand(group.id)">
          <div class="group-info">
            <span class="expand-icon" :class="{ expanded: expandedSet.has(group.id) }">
              <el-icon><CaretRight /></el-icon>
            </span>
            <span class="group-name">{{ group.name }}</span>
            <span class="strategy-badge">{{ group.strategy_label || group.strategy }}</span>
            <span
              class="mode-badge"
              :class="openModeMap[group.id] === 'public' ? 'mode--public' : 'mode--controlled'"
            >
              {{ openModeMap[group.id] === 'public' ? '公开' : '受控' }}
            </span>
          </div>
          <div class="group-actions" @click.stop>
            <el-switch
              :model-value="openModeMap[group.id] === 'public'"
              active-text="公开"
              inactive-text="受控"
              inline-prompt
              :loading="switchingSet.has(group.id)"
              @change="onOpenModeChange(group)"
            />
          </div>
        </div>

        <Transition name="expand">
          <div v-if="expandedSet.has(group.id)" class="group-body">
            <div v-if="openModeMap[group.id] === 'public'" class="public-hint">
              <el-icon><InfoFilled /></el-icon>
              <span>公开组无需授权，所有部门默认可见可用</span>
            </div>

            <el-tabs
              v-else
              v-model="activeTabMap[group.id]"
              class="entitlement-tabs"
              @tab-change="(tab: string) => onTabChange(group.id, tab)"
            >
              <el-tab-pane label="工作室授权" name="studio">
                <div class="tab-toolbar">
                  <el-button size="small" type="primary" @click="openGrantDialog(group.id, 'studio')">
                    添加工作室授权
                  </el-button>
                </div>
                <el-table
                  :data="entitlementMap[group.id]?.filter(e => e.scope_type === 'studio') ?? []"
                  v-loading="loadingSet.has(group.id)"
                  size="small"
                  class="inner-table"
                >
                  <el-table-column prop="scope_id" label="工作室 ID" min-width="160" />
                  <el-table-column label="启用" width="70" align="center">
                    <template #default="{ row }">
                      <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                        {{ row.enabled ? '是' : '否' }}
                      </el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column prop="granted_by" label="授予人" width="120" />
                  <el-table-column label="创建时间" width="170">
                    <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
                  </el-table-column>
                  <el-table-column label="操作" width="80" align="center">
                    <template #default="{ row }">
                      <el-popconfirm
                        title="确定撤销该授权？"
                        confirm-button-text="确定"
                        cancel-button-text="取消"
                        @confirm="handleRevoke(group.id, row)"
                      >
                        <template #reference>
                          <el-button text type="danger" size="small">撤销</el-button>
                        </template>
                      </el-popconfirm>
                    </template>
                  </el-table-column>
                </el-table>
              </el-tab-pane>

              <el-tab-pane label="部门开通" name="dept">
                <div class="tab-toolbar">
                  <el-button size="small" type="primary" @click="openGrantDialog(group.id, 'dept')">
                    添加部门开通
                  </el-button>
                </div>
                <el-table
                  :data="entitlementMap[group.id]?.filter(e => e.scope_type === 'dept') ?? []"
                  v-loading="loadingSet.has(group.id)"
                  size="small"
                  class="inner-table"
                >
                  <el-table-column prop="scope_id" label="部门 ID" min-width="160" />
                  <el-table-column label="启用" width="70" align="center">
                    <template #default="{ row }">
                      <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                        {{ row.enabled ? '是' : '否' }}
                      </el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column prop="granted_by" label="授予人" width="120" />
                  <el-table-column label="创建时间" width="170">
                    <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
                  </el-table-column>
                  <el-table-column label="操作" width="80" align="center">
                    <template #default="{ row }">
                      <el-popconfirm
                        title="确定撤销该授权？"
                        confirm-button-text="确定"
                        cancel-button-text="取消"
                        @confirm="handleRevoke(group.id, row)"
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
        </Transition>
      </div>

      <el-empty v-if="!pageLoading && filteredGroups.length === 0" description="暂无模型组" />
    </div>

    <el-dialog
      v-model="grantDialogVisible"
      :title="grantDialogScopeType === 'studio' ? '添加工作室授权' : '添加部门开通'"
      width="420px"
      :close-on-click-modal="false"
      @close="resetGrantForm"
    >
      <el-form :model="grantForm" label-width="90px">
        <el-form-item label="模型组">
          <span>{{ grantFormGroupName }}</span>
        </el-form-item>
        <el-form-item label="授权类型">
          <span>{{ grantDialogScopeType === 'studio' ? '工作室' : '部门' }}</span>
        </el-form-item>
        <el-form-item label="目标 ID">
          <el-input v-model="grantForm.scope_id" placeholder="请输入工作室/部门 ID" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="grantDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="grantSaving" @click="handleGrant">确认授权</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CaretRight, InfoFilled } from '@element-plus/icons-vue'
import {
  setGroupOpenMode,
  getGroupEntitlements,
  grantEntitlement,
  revokeEntitlement,
} from '@/services/control-plane'
import { groupsApi } from '@/api/groups'
import type {
  GroupOpenMode,
  GroupEntitlement,
  EntitlementScopeType,
} from '@/types/entitlement'
import type { GroupListItem } from '@/types/group'

type GroupRecord = GroupListItem

const groups = ref<GroupRecord[]>([])
const pageLoading = ref(false)
const searchQuery = ref('')
let searchTimer: ReturnType<typeof setTimeout> | null = null

const expandedSet = ref<Set<number>>(new Set())
const loadingSet = ref<Set<number>>(new Set())
const switchingSet = ref<Set<number>>(new Set())
const openModeMap = reactive<Record<number, GroupOpenMode>>({})
const entitlementMap = reactive<Record<number, GroupEntitlement[]>>({})
const activeTabMap = reactive<Record<number, string>>({})

const grantDialogVisible = ref(false)
const grantSaving = ref(false)
const grantForm = reactive<{ group_id: number; scope_type: EntitlementScopeType; scope_id: string }>({
  group_id: 0,
  scope_type: 'studio',
  scope_id: '',
})
const grantFormGroupName = ref('')
const grantDialogScopeType = computed(() => grantForm.scope_type)

const filteredGroups = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return groups.value
  return groups.value.filter(g => g.name.toLowerCase().includes(q))
})

function formatTime(t: string) {
  return t ? new Date(t).toLocaleString('zh-CN') : '-'
}

function onSearchDebounced() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {}, 300)
}

async function loadGroups() {
  pageLoading.value = true
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    const items: GroupRecord[] = res.data.data
    groups.value = items
    for (const g of items) {
      openModeMap[g.id] = g.open_mode ?? 'public'
    }
  } catch {
    ElMessage.error('加载模型组列表失败')
  } finally {
    pageLoading.value = false
  }
}

async function loadEntitlements(groupId: number) {
  loadingSet.value.add(groupId)
  try {
    const items = await getGroupEntitlements(groupId)
    entitlementMap[groupId] = items
  } catch {
    ElMessage.error('加载授权列表失败')
  } finally {
    loadingSet.value.delete(groupId)
  }
}

function toggleExpand(groupId: number) {
  if (expandedSet.value.has(groupId)) {
    expandedSet.value.delete(groupId)
  } else {
    expandedSet.value.add(groupId)
    if (!entitlementMap[groupId]) {
      loadEntitlements(groupId)
    }
    if (!activeTabMap[groupId]) {
      activeTabMap[groupId] = 'studio'
    }
  }
}

function onTabChange(groupId: number, _tab: string) {
  activeTabMap[groupId] = _tab
}

async function onOpenModeChange(group: GroupRecord) {
  const currentMode = openModeMap[group.id] ?? 'public'
  const newMode: GroupOpenMode = currentMode === 'public' ? 'controlled' : 'public'
  const label = newMode === 'public' ? '公开' : '受控'

  try {
    await ElMessageBox.confirm(
      newMode === 'public'
        ? `确定将「${group.name}」切换为公开模式？切换后所有部门将默认可见可用。`
        : `确定将「${group.name}」切换为受控模式？切换后需要逐一授权部门才可访问。`,
      '切换开放模式',
      { confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }

  switchingSet.value.add(group.id)
  try {
    await setGroupOpenMode(group.id, newMode)
    openModeMap[group.id] = newMode
    ElMessage.success(`已切换为「${label}」模式`)
  } catch {
    ElMessage.error('切换开放模式失败')
  } finally {
    switchingSet.value.delete(group.id)
  }
}

function openGrantDialog(groupId: number, scopeType: EntitlementScopeType) {
  grantForm.group_id = groupId
  grantForm.scope_type = scopeType
  grantForm.scope_id = ''
  grantFormGroupName.value = groups.value.find(g => g.id === groupId)?.name ?? ''
  grantDialogVisible.value = true
}

function resetGrantForm() {
  grantForm.group_id = 0
  grantForm.scope_type = 'studio'
  grantForm.scope_id = ''
  grantFormGroupName.value = ''
}

async function handleGrant() {
  if (!grantForm.scope_id.trim()) {
    ElMessage.warning('请输入目标 ID')
    return
  }
  grantSaving.value = true
  try {
    await grantEntitlement({
      group_id: grantForm.group_id,
      scope_type: grantForm.scope_type,
      scope_id: grantForm.scope_id.trim(),
    })
    ElMessage.success('授权成功')
    grantDialogVisible.value = false
    await loadEntitlements(grantForm.group_id)
  } catch {
    ElMessage.error('授权失败')
  } finally {
    grantSaving.value = false
  }
}

async function handleRevoke(groupId: number, row: GroupEntitlement) {
  try {
    await revokeEntitlement({
      group_id: row.group_id,
      scope_type: row.scope_type,
      scope_id: row.scope_id,
    })
    ElMessage.success('已撤销授权')
    await loadEntitlements(groupId)
  } catch {
    ElMessage.error('撤销失败')
  }
}

onMounted(() => {
  loadGroups()
})
</script>

<style scoped>
.entitlement-page {
  padding: 0;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
  gap: 16px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0 0 6px;
}

.page-subtitle {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0;
  line-height: 1.6;
}

.header-actions {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.group-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.group-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.04);
  transition: box-shadow 0.25s;
}

.group-card:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.07);
}

.group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}

.group-header:hover {
  background: var(--el-fill-color-light);
}

.group-info {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 1;
}

.expand-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: transform 0.25s ease;
  color: var(--el-text-color-secondary);
  font-size: 14px;
  flex-shrink: 0;
}

.expand-icon.expanded {
  transform: rotate(90deg);
}

.group-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.strategy-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 500;
  background: var(--el-fill-color);
  color: var(--el-text-color-regular);
  flex-shrink: 0;
}

.mode-badge {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  flex-shrink: 0;
}

.mode--public {
  background: rgba(16, 185, 129, 0.12);
  color: #10b981;
}

.mode--controlled {
  background: rgba(245, 158, 11, 0.12);
  color: #f59e0b;
}

.group-actions {
  flex-shrink: 0;
  margin-left: 12px;
}

.group-body {
  border-top: 1px solid var(--el-border-color-lighter);
  padding: 16px 18px;
  overflow: hidden;
}

.public-hint {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px 0;
  color: var(--el-text-color-secondary);
  font-size: 14px;
}

.public-hint .el-icon {
  color: #10b981;
  font-size: 18px;
}

.entitlement-tabs {
  --el-tabs-header-height: 36px;
}

.entitlement-tabs :deep(.el-tabs__header) {
  margin-bottom: 12px;
}

.entitlement-tabs :deep(.el-tabs__item) {
  font-size: 13px;
  height: 36px;
  line-height: 36px;
}

.tab-toolbar {
  margin-bottom: 10px;
}

.inner-table :deep(.el-table) {
  --el-table-border-color: transparent;
}

.inner-table :deep(.el-table::before) {
  display: none;
}

.inner-table :deep(.el-table th.el-table__cell) {
  background: var(--el-fill-color-light);
  font-size: 12px;
  font-weight: 600;
}

.inner-table :deep(.el-table td.el-table__cell) {
  font-size: 12px;
}

.expand-enter-active,
.expand-leave-active {
  transition: all 0.3s ease;
  max-height: 600px;
  opacity: 1;
  overflow: hidden;
}

.expand-enter-from,
.expand-leave-to {
  max-height: 0;
  opacity: 0;
  padding-top: 0;
  padding-bottom: 0;
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    gap: 12px;
  }

  .header-actions {
    width: 100%;
  }

  .header-actions :deep(.el-input) {
    width: 100%;
  }

  .group-header {
    padding: 12px 14px;
    flex-wrap: wrap;
    gap: 8px;
  }

  .group-info {
    flex-wrap: wrap;
    gap: 6px;
  }

  .group-body {
    padding: 12px 14px;
  }

  .inner-table :deep(.el-table) {
    font-size: 11px;
  }
}

@media (prefers-color-scheme: dark) {
  .group-card {
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.2);
  }

  .group-card:hover {
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.3);
  }

  .mode--public {
    background: rgba(16, 185, 129, 0.18);
  }

  .mode--controlled {
    background: rgba(245, 158, 11, 0.18);
  }

  .inner-table :deep(.el-table th.el-table__cell) {
    background: var(--el-fill-color);
  }
}
</style>
