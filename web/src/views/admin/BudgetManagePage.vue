<template>
  <div class="bm">
    <div class="bm-header">
      <div class="bm-header__left">
        <h2 class="bm-title">预算管理</h2>
        <span class="bm-subtitle" v-if="!loading && !error">共 {{ budgetList.length }} 项</span>
      </div>
      <div class="bm-header__right">
        <el-button :loading="loading" :icon="Refresh" circle size="small" title="刷新" @click="loadData" />
        <el-button v-if="authStore.isAdmin" type="primary" :icon="Plus" @click="openCreateDialog">新增预算</el-button>
      </div>
    </div>

    <div class="bm-filter">
      <el-select v-model="filterScopeType" placeholder="全部类型" clearable size="default" class="bm-filter__type" @change="loadData">
        <el-option v-for="(label, key) in SCOPE_TYPE_LABELS" :key="key" :label="label" :value="key" />
      </el-select>
      <el-input v-model="filterScopeId" placeholder="搜索 scope_id" clearable size="default" class="bm-filter__id" @change="loadData" @clear="loadData">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
    </div>

    <div v-if="loading" class="bm-skeleton">
      <el-skeleton v-for="i in 4" :key="i" :rows="4" animated class="bm-skeleton-card" />
    </div>

    <el-result v-else-if="error" icon="error" title="加载失败" :sub-title="errorMsg">
      <template #extra>
        <el-button type="primary" @click="loadData">重试</el-button>
      </template>
    </el-result>

    <el-empty v-else-if="budgetList.length === 0" description="暂无预算配置，请点击右上角新增" />

    <div v-else class="bm-grid">
      <div v-for="item in budgetList" :key="`${item.scope_type}:${item.scope_id}`" class="bm-card" :style="{ '--accent': SCOPE_TYPE_COLORS[item.scope_type] }">
        <div class="bm-card__top">
          <span class="bm-badge" :style="{ background: SCOPE_TYPE_COLORS[item.scope_type] }">
            {{ SCOPE_TYPE_LABELS[item.scope_type] }}
          </span>
          <span class="bm-card__scope-id">{{ item.scope_id }}</span>
        </div>

        <div class="bm-card__middle">
          <div class="bm-amount">${{ formatAmount(item.amount_usd) }} <span class="bm-amount__unit">USD</span></div>
          <el-progress
            :percentage="Math.round(item.used_ratio * 100)"
            :color="progressColor(item.used_ratio)"
            :stroke-width="8"
            :show-text="false"
            class="bm-progress"
          />
        </div>

        <div class="bm-card__bottom">
          <div class="bm-card__info">
            <span class="bm-card__usage">{{ formatAmount(item.used_usd) }} / {{ formatAmount(item.amount_usd) }}</span>
            <span class="bm-card__period">{{ item.period || '—' }}</span>
          </div>
          <div class="bm-card__actions">
            <el-button size="small" text type="primary" @click="openEditDialog(item)">编辑额度</el-button>
            <el-button size="small" text type="warning" @click="openExpandDialog(item)">申请扩容</el-button>
          </div>
        </div>
      </div>
    </div>

    <el-dialog v-model="editVisible" title="编辑额度" width="460px" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="类型">
          <el-input :model-value="SCOPE_TYPE_LABELS[editForm.scope_type]" disabled />
        </el-form-item>
        <el-form-item label="Scope ID">
          <el-input :model-value="editForm.scope_id" disabled />
        </el-form-item>
        <el-form-item label="额度 (USD)">
          <el-input-number v-model="editForm.amount_usd" :min="0" :precision="2" :step="100" controls-position="right" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleEditSave">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="createVisible" title="新增预算" width="460px" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="类型">
          <el-select v-model="createForm.scope_type" placeholder="请选择" style="width: 100%">
            <el-option v-for="(label, key) in SCOPE_TYPE_LABELS" :key="key" :label="label" :value="key" />
          </el-select>
        </el-form-item>
        <el-form-item label="Scope ID">
          <el-input v-model="createForm.scope_id" placeholder="请输入 scope_id" />
        </el-form-item>
        <el-form-item label="额度 (USD)">
          <el-input-number v-model="createForm.amount_usd" :min="0" :precision="2" :step="100" controls-position="right" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleCreateSave">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="expandVisible" title="申请扩容" width="460px" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="类型">
          <el-input :model-value="SCOPE_TYPE_LABELS[expandForm.scope_type]" disabled />
        </el-form-item>
        <el-form-item label="Scope ID">
          <el-input :model-value="expandForm.scope_id" disabled />
        </el-form-item>
        <el-form-item label="当前额度">
          <el-input :model-value="`$${formatAmount(expandForm.current_amount)} USD`" disabled />
        </el-form-item>
        <el-form-item label="新额度 (USD)">
          <el-input-number v-model="expandForm.amount_usd" :min="expandForm.current_amount" :precision="2" :step="100" controls-position="right" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="expandVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleExpandSave">提交</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Refresh, Plus, Search } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getBudgetList, setBudget } from '@/services/control-plane'
import type { BudgetItem, BudgetScopeType } from '@/types/budget'
import { SCOPE_TYPE_LABELS, SCOPE_TYPE_COLORS } from '@/types/budget'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const loading = ref(false)
const error = ref(false)
const errorMsg = ref('')
const saving = ref(false)

const budgetList = ref<BudgetItem[]>([])

const filterScopeType = ref<BudgetScopeType | ''>('')
const filterScopeId = ref('')

const editVisible = ref(false)
const editForm = ref({ scope_type: '' as BudgetScopeType, scope_id: '', amount_usd: 0 })

const createVisible = ref(false)
const createForm = ref({ scope_type: '' as BudgetScopeType, scope_id: '', amount_usd: 0 })

const expandVisible = ref(false)
const expandForm = ref({ scope_type: '' as BudgetScopeType, scope_id: '', current_amount: 0, amount_usd: 0 })

async function loadData() {
  loading.value = true
  error.value = false
  errorMsg.value = ''
  try {
    const st = filterScopeType.value || undefined
    const sid = filterScopeId.value || undefined
    budgetList.value = await getBudgetList(st, sid)
  } catch (e: any) {
    error.value = true
    errorMsg.value = e?.message || '未知错误'
  } finally {
    loading.value = false
  }
}

function formatAmount(v: number): string {
  return v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function progressColor(ratio: number): string {
  if (ratio > 0.9) return '#ef4444'
  if (ratio > 0.7) return '#f59e0b'
  return '#22c55e'
}

function openEditDialog(item: BudgetItem) {
  editForm.value = { scope_type: item.scope_type, scope_id: item.scope_id, amount_usd: item.amount_usd }
  editVisible.value = true
}

function openCreateDialog() {
  createForm.value = { scope_type: '', scope_id: '', amount_usd: 0 }
  createVisible.value = true
}

function openExpandDialog(item: BudgetItem) {
  expandForm.value = { scope_type: item.scope_type, scope_id: item.scope_id, current_amount: item.amount_usd, amount_usd: item.amount_usd }
  expandVisible.value = true
}

async function handleEditSave() {
  saving.value = true
  try {
    await setBudget({ scope_type: editForm.value.scope_type, scope_id: editForm.value.scope_id, amount_usd: editForm.value.amount_usd })
    ElMessage.success('额度已更新')
    editVisible.value = false
    await loadData()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function handleCreateSave() {
  if (!createForm.value.scope_type) { ElMessage.warning('请选择类型'); return }
  if (!createForm.value.scope_id) { ElMessage.warning('请输入 scope_id'); return }
  saving.value = true
  try {
    await setBudget({ scope_type: createForm.value.scope_type, scope_id: createForm.value.scope_id, amount_usd: createForm.value.amount_usd })
    ElMessage.success('预算已创建')
    createVisible.value = false
    await loadData()
  } catch (e: any) {
    ElMessage.error(e?.message || '创建失败')
  } finally {
    saving.value = false
  }
}

async function handleExpandSave() {
  saving.value = true
  try {
    await setBudget({ scope_type: expandForm.value.scope_type, scope_id: expandForm.value.scope_id, amount_usd: expandForm.value.amount_usd })
    ElMessage.success('扩容已提交')
    expandVisible.value = false
    await loadData()
  } catch (e: any) {
    ElMessage.error(e?.message || '提交失败')
  } finally {
    saving.value = false
  }
}

onMounted(() => { loadData() })
</script>

<style scoped>
.bm {
  padding: 24px;
  max-width: 1400px;
  margin: 0 auto;
}

.bm-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.bm-header__left {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.bm-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.bm-subtitle {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.bm-header__right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bm-filter {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
}

.bm-filter__type {
  width: 160px;
}

.bm-filter__id {
  width: 220px;
}

.bm-skeleton {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}

.bm-skeleton-card {
  padding: 20px;
  border-radius: 12px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
}

.bm-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}

.bm-card {
  border-radius: 12px;
  padding: 20px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  transition: transform 0.2s, box-shadow 0.2s;
  position: relative;
  overflow: hidden;
}

.bm-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 3px;
  background: var(--accent);
}

.bm-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.1);
}

.bm-card__top {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}

.bm-badge {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 999px;
  color: #fff;
  font-size: 12px;
  font-weight: 500;
  line-height: 1.6;
  white-space: nowrap;
}

.bm-card__scope-id {
  font-size: 13px;
  color: var(--el-text-color-regular);
  font-family: monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bm-card__middle {
  margin-bottom: 16px;
}

.bm-amount {
  font-size: 28px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  margin-bottom: 10px;
  line-height: 1.2;
}

.bm-amount__unit {
  font-size: 14px;
  font-weight: 400;
  color: var(--el-text-color-secondary);
}

.bm-progress {
  margin-top: 4px;
}

.bm-progress :deep(.el-progress-bar__outer) {
  height: 8px;
  border-radius: 4px;
}

.bm-progress :deep(.el-progress-bar__inner) {
  border-radius: 4px;
}

.bm-card__bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 12px;
}

.bm-card__info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.bm-card__usage {
  font-size: 12px;
  color: var(--el-text-color-regular);
  font-family: monospace;
}

.bm-card__period {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.bm-card__actions {
  display: flex;
  gap: 4px;
}

html.dark .bm-card {
  background: var(--el-bg-color-overlay, #1d1e1f);
  border-color: var(--el-border-color);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

html.dark .bm-card:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
}

html.dark .bm-skeleton-card {
  background: var(--el-bg-color-overlay, #1d1e1f);
  border-color: var(--el-border-color);
}

@media (max-width: 640px) {
  .bm {
    padding: 16px;
  }

  .bm-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .bm-header__right {
    width: 100%;
    justify-content: flex-end;
  }

  .bm-filter {
    flex-direction: column;
  }

  .bm-filter__type,
  .bm-filter__id {
    width: 100%;
  }

  .bm-grid,
  .bm-skeleton {
    grid-template-columns: 1fr;
  }

  .bm-card__bottom {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}
</style>
