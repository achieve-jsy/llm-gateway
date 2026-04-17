<template>
  <main class="tier-page" aria-labelledby="tier-title">
    <header class="page-header">
      <div>
        <h1 id="tier-title">模型等级</h1>
        <p>按成本和能力对模型组分级，供等级预算独立管控。</p>
      </div>
      <el-button type="primary" @click="openCreate">新建等级</el-button>
    </header>

    <el-alert
      title="停用只阻止新关联；已有预算仍保留。归档后默认隐藏，已产生历史的等级不可删除。"
      type="info"
      :closable="false"
      show-icon
    />

    <el-table v-loading="loading" :data="tiers" row-key="id" empty-text="暂无模型等级">
      <el-table-column prop="sort_order" label="序号" width="60" />
      <el-table-column label="等级" min-width="180">
        <template #default="{ row }">
          <span class="color-dot" :style="{ backgroundColor: row.color }" />
          <strong>{{ row.name }}</strong>
          <el-tag v-if="row.archived_at" size="small" type="info">已归档</el-tag>
          <el-tag v-else-if="!row.enabled" size="small" type="warning">已停用</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="说明" min-width="220" show-overflow-tooltip />
      <el-table-column prop="group_count" label="模型组" width="90" />
      <el-table-column prop="budget_count" label="预算引用" width="100" />
      <el-table-column prop="history_count" label="历史引用" width="100" />
      <el-table-column label="排序" width="110">
        <template #default="{ row, $index }">
          <el-button text aria-label="上移" :disabled="row.archived_at || $index === 0" @click="move($index, -1)">↑</el-button>
          <el-button text aria-label="下移" :disabled="row.archived_at || $index === tiers.length - 1" @click="move($index, 1)">↓</el-button>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button v-if="!row.archived_at" link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button v-if="!row.archived_at && row.enabled" link @click="openGroups(row)">关联模型组</el-button>
          <el-button v-if="!row.archived_at" link :type="row.enabled ? 'warning' : 'success'" @click="toggle(row)">
            {{ row.enabled ? '停用' : '启用' }}
          </el-button>
          <el-button v-if="!row.archived_at && row.reference_count > 0" link type="info" @click="archive(row)">归档</el-button>
          <el-button v-if="row.reference_count === 0" link type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="formVisible" :title="editing ? '编辑等级' : '新建等级'" width="520px">
      <el-form label-position="top">
        <el-form-item label="名称" required><el-input v-model="form.name" maxlength="40" show-word-limit /></el-form-item>
        <el-form-item label="说明"><el-input v-model="form.description" type="textarea" maxlength="200" show-word-limit /></el-form-item>
        <el-form-item label="标识颜色" required><el-color-picker v-model="form.color" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="!form.name.trim()" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="groupsVisible" title="批量关联模型组" width="600px">
      <div class="group-filters">
        <el-input
          v-model="groupSearch"
          placeholder="搜索模型组名称"
          clearable
          data-test="tier-group-search"
        />
        <el-select v-model="groupTierFilter" aria-label="按当前等级筛选">
          <el-option label="全部等级" value="all" />
          <el-option label="未分级" value="unclassified" />
          <el-option v-for="tier in activeTiers" :key="tier.id" :label="tier.name" :value="tier.id" />
        </el-select>
      </div>
      <el-table
        ref="groupTableRef"
        v-loading="groupsLoading"
        :data="filteredGroups"
        row-key="id"
        max-height="360"
        data-test="tier-group-table"
        @selection-change="onGroupSelectionChange"
      >
        <el-table-column type="selection" width="48" />
        <el-table-column prop="name" label="模型组" />
        <el-table-column prop="model_tier_name" label="当前等级">
          <template #default="{ row }">{{ row.model_tier_name || '未分级' }}</template>
        </el-table-column>
      </el-table>
      <div v-if="!groupsLoading" class="group-result-meta">
        显示 {{ filteredGroups.length }} / 共 {{ groupsCatalog.length }} 个模型组
      </div>
      <template #footer>
        <el-button @click="groupsVisible = false">取消</el-button>
        <el-button :loading="saving" :disabled="selectedGroups.length === 0" @click="saveGroups(true)">
          清除关联（未分级）
        </el-button>
        <el-button type="primary" :loading="saving" :disabled="selectedGroups.length === 0" @click="saveGroups(false)">
          关联 {{ selectedGroups.length }} 个模型组
        </el-button>
      </template>
    </el-dialog>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { groupsApi } from '@/api/groups'
import {
  archiveModelTier, createModelTier, deleteModelTier, listModelTiers, reorderModelTiers,
  setGroupBudgetTierBatch, setModelTierStatus, updateModelTier,
} from '@/services/control-plane'
import type { GroupListItem } from '@/types/group'
import type { ModelTier } from '@/types/model-tier'

const tiersRaw = ref<ModelTier[]>([])
const tiers = computed(() => [...tiersRaw.value].sort((a, b) => a.sort_order - b.sort_order))
/** 打开弹窗时拉取的全量目录，保存时用于计算取消勾选 */
const groupsCatalog = ref<GroupListItem[]>([])
/** 当前展示数据源（全量或搜索结果） */
const groups = ref<GroupListItem[]>([])
const selectedGroups = ref<GroupListItem[]>([])
const loading = ref(false)
const groupsLoading = ref(false)
const saving = ref(false)
const formVisible = ref(false)
const groupsVisible = ref(false)
const editing = ref<ModelTier | null>(null)
const selectedTier = ref<ModelTier | null>(null)
const groupTableRef = ref<any>(null)
const groupSearch = ref('')
const groupTierFilter = ref('all')
const form = reactive({ name: '', description: '', color: '#409EFF' })
const activeTiers = computed(() => tiers.value.filter(t => !t.archived_at && t.enabled).sort((a, b) => a.sort_order - b.sort_order))

function matchesGroupSearch(group: GroupListItem, keyword: string): boolean {
  const q = keyword.trim().toLowerCase()
  if (!q) return true
  const name = String(group.name || '').toLowerCase()
  const desc = String(group.description || '').toLowerCase()
  const id = String(group.id ?? '')
  return name.includes(q) || desc.includes(q) || id.includes(q)
}

const filteredGroups = computed(() => groups.value.filter((group) => {
  if (!matchesGroupSearch(group, groupSearch.value)) return false
  if (groupTierFilter.value === 'unclassified') return !group.model_tier_id
  if (groupTierFilter.value !== 'all') return group.model_tier_id === groupTierFilter.value
  return true
}))

async function load() {
  loading.value = true
  try { tiersRaw.value = await listModelTiers(true) } finally { loading.value = false }
}
function openCreate() {
  editing.value = null
  Object.assign(form, { name: '', description: '', color: '#409EFF' })
  formVisible.value = true
}
function openEdit(row: ModelTier) {
  editing.value = row
  Object.assign(form, { name: row.name, description: row.description, color: row.color })
  formVisible.value = true
}
async function save() {
  saving.value = true
  try {
    if (editing.value) await updateModelTier(editing.value.id, form)
    else await createModelTier(form)
    formVisible.value = false
    ElMessage.success('已保存')
    await load()
  } finally { saving.value = false }
}
async function move(index: number, offset: number) {
  const copy = [...tiersRaw.value]
  ;[copy[index], copy[index + offset]] = [copy[index + offset], copy[index]]
  await reorderModelTiers(copy.map(item => item.id), '调整模型等级顺序')
  tiersRaw.value = copy
}
async function toggle(row: ModelTier) {
  await ElMessageBox.confirm(`确定${row.enabled ? '停用' : '启用'}等级「${row.name}」？`, '确认操作', { type: 'warning' })
  await setModelTierStatus(row.id, !row.enabled, '模型等级状态变更')
  await load()
}
async function archive(row: ModelTier) {
  await ElMessageBox.confirm(`确定归档等级「${row.name}」？归档后不可重新启用。`, '确认归档', { type: 'warning' })
  await archiveModelTier(row.id, '归档模型等级')
  await load()
}
async function remove(row: ModelTier) {
  await ElMessageBox.confirm('仅无模型组、预算和历史引用的等级可永久删除。', '删除等级', { type: 'warning' })
  await deleteModelTier(row.id, '删除模型等级')
  await load()
}

async function loadAllGroups(search?: string): Promise<GroupListItem[]> {
  // 后端 List 限制 page_size ∈ [1, 200]，超出回退为 20；必须分页拉全量。
  const pageSize = 200
  let page = 1
  const all: GroupListItem[] = []
  const keyword = search?.trim() || undefined
  while (true) {
    const response = await groupsApi.listGroups({
      page,
      page_size: pageSize,
      search: keyword,
    })
    const batch = response.data.data || []
    const total = Number(response.data.total || 0)
    all.push(...batch)
    if (batch.length === 0 || all.length >= total || batch.length < pageSize) break
    page++
  }
  return all
}

function applyPreselection() {
  if (!selectedTier.value) return
  const preSelected = groupsCatalog.value.filter(g => g.model_tier_id === selectedTier.value!.id)
  selectedGroups.value = [...preSelected]
  setTimeout(() => {
    groupTableRef.value?.clearSelection?.()
    preSelected.forEach((g) => groupTableRef.value?.toggleRowSelection?.(g, true))
  }, 50)
}

async function openGroups(row: ModelTier) {
  selectedTier.value = row
  groupSearch.value = ''
  groupTierFilter.value = 'all'
  groupsVisible.value = true
  groupsLoading.value = true
  try {
    groupsCatalog.value = await loadAllGroups()
    groups.value = groupsCatalog.value
    applyPreselection()
  } finally {
    groupsLoading.value = false
  }
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
let searchSeq = 0
watch(groupSearch, (raw) => {
  if (!groupsVisible.value) return
  const keyword = raw.trim()
  // 本地即时过滤：全量目录已在内存时立即生效
  if (!keyword) {
    groups.value = groupsCatalog.value
  } else {
    groups.value = groupsCatalog.value.filter(g => matchesGroupSearch(g, keyword))
  }
  // 再走后端 search 对齐 ILIKE
  if (searchTimer) clearTimeout(searchTimer)
  const seq = ++searchSeq
  searchTimer = setTimeout(async () => {
    if (!groupsVisible.value || seq !== searchSeq) return
    if (!keyword) {
      groups.value = groupsCatalog.value
      return
    }
    groupsLoading.value = true
    try {
      const found = await loadAllGroups(keyword)
      if (!groupsVisible.value || seq !== searchSeq) return
      groups.value = found
    } finally {
      if (seq === searchSeq) groupsLoading.value = false
    }
  }, 300)
})

function onGroupSelectionChange(rows: GroupListItem[]) {
  selectedGroups.value = rows
}
async function saveGroups(clear = false) {
  if (!selectedTier.value) return
  saving.value = true
  try {
    const tierId = selectedTier.value.id
    if (clear) {
      await setGroupBudgetTierBatch(selectedGroups.value.map(g => g.id), null)
    } else {
      const toAssign = selectedGroups.value.map(g => g.id)
      // 取消勾选：基于全量目录，避免搜索态漏清
      const toClear = groupsCatalog.value
        .filter(g => g.model_tier_id === tierId && !selectedGroups.value.some(sg => sg.id === g.id))
        .map(g => g.id)
      if (toClear.length > 0) {
        await setGroupBudgetTierBatch(toClear, null)
      }
      if (toAssign.length > 0) {
        await setGroupBudgetTierBatch(toAssign, tierId)
      }
    }
    groupsVisible.value = false
    ElMessage.success(clear ? '已清除模型组等级关联' : '模型组关联已更新')
    await load()
  } finally { saving.value = false }
}
onMounted(load)

defineExpose({ selectedGroups, saveGroups, groupsCatalog, filteredGroups, groupSearch })
</script>

<style scoped>
.tier-page { padding: 24px; display: grid; gap: 20px; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
h1 { margin: 0 0 6px; font-size: 24px; }
p { margin: 0; color: var(--el-text-color-secondary); }
.color-dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 8px; }
.el-tag { margin-left: 8px; }
.group-filters { display: grid; grid-template-columns: 1fr 180px; gap: 12px; margin-bottom: 12px; }
.group-result-meta {
  margin-top: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
@media (max-width: 768px) {
  .tier-page { padding: 16px; }
  .page-header { align-items: stretch; flex-direction: column; }
}
</style>
