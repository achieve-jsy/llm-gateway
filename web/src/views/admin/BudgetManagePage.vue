<template>
  <div class="budget-page">
    <div class="page-header">
      <div>
        <h2>{{ t('budget.title') }}</h2>
        <p class="description">{{ t('budget.description') }}</p>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <el-select v-model="filters.scope_type" :placeholder="t('budget.filter.type')" clearable style="width: 180px" @change="loadBudgets">
        <el-option v-for="(label, key) in SCOPE_TYPE_LABELS" :key="key" :label="label" :value="key" />
      </el-select>
      <el-input v-model="filters.scope_id" :placeholder="t('budget.filter.scopeId')" clearable style="width: 240px" @clear="loadBudgets" @keyup.enter="loadBudgets" />
      <el-button type="primary" @click="loadBudgets">{{ t('common.search') }}</el-button>
      <el-button @click="resetFilters">{{ t('common.reset') }}</el-button>
      <el-button type="success" :disabled="scopeOptions.length === 0" @click="openCreateDialog">{{ t('budget.addBudget') }}</el-button>
    </div>

    <!-- 预算列表 -->
    <el-table :data="budgetList" stripe v-loading="loading" :empty-text="t('budget.empty')">
      <el-table-column :label="t('budget.columns.type')" width="140">
        <template #default="{ row }">
          <el-tag :color="SCOPE_TYPE_COLORS[row.scope_type]" style="color: white; border: none">
            {{ SCOPE_TYPE_LABELS[row.scope_type] }}
          </el-tag>
        </template>
      </el-table-column>
      
      <el-table-column :label="t('budget.columns.name')" min-width="180">
        <template #default="{ row }">
          <div class="scope-name">
            <span class="name">{{ getScopeName(row.scope_type, row.scope_id) }}</span>
            <span class="id" :title="row.scope_id">{{ row.scope_id }}</span>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="t('budget.columns.amount')" width="160" align="right">
        <template #default="{ row }">
          <span class="amount">${{ formatAmount(row.amount_usd) }}</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('budget.columns.used')" width="180" align="right">
        <template #default="{ row }">
          <div class="usage">
            <span class="used">${{ formatAmount(row.used_usd) }}</span>
            <span class="separator"> / </span>
            <span class="total">${{ formatAmount(row.amount_usd) }}</span>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="t('budget.columns.usage')" width="160">
        <template #default="{ row }">
          <div class="progress-cell">
            <el-progress 
              :percentage="Math.round(row.used_ratio * 100)" 
              :color="getProgressColor(row.used_ratio)"
              :stroke-width="10"
            />
            <span class="ratio">{{ (row.used_ratio * 100).toFixed(1) }}%</span>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="t('budget.columns.period')" width="100" align="center">
        <template #default="{ row }">
          <span>{{ row.period }}</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('common.actions')" width="180" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEditDialog(row)">{{ t('common.edit') }}</el-button>
          <el-button link type="danger" @click="handleDelete(row)">{{ t('common.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建对话框 -->
    <el-dialog v-model="createDialogVisible" :title="t('budget.dialog.createTitle')" width="520px">
      <el-form :model="createForm" :rules="formRules" ref="createFormRef" label-width="100px">
        <el-form-item :label="t('budget.form.type')" prop="scope_type">
          <el-select v-model="createForm.scope_type" :placeholder="t('budget.form.typePlaceholder')" @change="onScopeTypeChange">
            <el-option v-for="(label, key) in SCOPE_TYPE_LABELS" :key="key" :label="label" :value="key" />
          </el-select>
        </el-form-item>

        <!-- 公司级不需要选择 -->
        
        <!-- 工作室选择 -->
        <el-form-item v-if="createForm.scope_type === 'studio'" :label="t('budget.form.studio')" prop="scope_id">
          <el-select v-model="createForm.scope_id" :placeholder="t('budget.form.studioPlaceholder')" filterable>
            <el-option v-for="studio in orgStore.studios" :key="studio.id" :label="studio.name" :value="studio.id" />
          </el-select>
        </el-form-item>

        <!-- 部门选择 -->
        <el-form-item v-if="createForm.scope_type === 'dept_daily' || createForm.scope_type === 'dept_service'" :label="t('budget.form.department')" prop="scope_id">
          <el-select v-model="createForm.scope_id" :placeholder="t('budget.form.deptPlaceholder')" filterable>
            <el-option v-for="dept in orgStore.departments" :key="dept.dept_id" :label="`${dept.dept_name} (${dept.dept_id})`" :value="dept.dept_id" />
          </el-select>
        </el-form-item>

        <!-- 成员选择 -->
        <el-form-item v-if="createForm.scope_type === 'member'" :label="t('budget.form.member')" prop="scope_id">
          <el-select v-model="createForm.scope_id" :placeholder="t('budget.form.memberPlaceholder')" filterable remote :remote-method="searchMembers">
            <el-option v-for="user in memberOptions" :key="user.id" :label="`${user.name} (${user.email})`" :value="user.id" />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('budget.form.amount')" prop="amount_usd">
          <el-input-number v-model="createForm.amount_usd" :min="0" :precision="2" :step="10" style="width: 100%" />
          <div class="form-tip">{{ t('budget.form.amountTip') }}</div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="closeCreateDialog">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 编辑对话框 -->
    <el-dialog v-model="editDialogVisible" :title="t('budget.dialog.editTitle')" width="520px">
      <el-form :model="editForm" :rules="formRules" ref="editFormRef" label-width="100px">
        <el-form-item :label="t('budget.form.type')">
          <el-tag :color="SCOPE_TYPE_COLORS[editForm.scope_type]" style="color: white; border: none">
            {{ SCOPE_TYPE_LABELS[editForm.scope_type] }}
          </el-tag>
        </el-form-item>

        <el-form-item :label="t('budget.form.name')">
          <div class="scope-name-edit">
            <span class="name">{{ getScopeName(editForm.scope_type, editForm.scope_id) }}</span>
            <span class="id">{{ editForm.scope_id }}</span>
          </div>
        </el-form-item>

        <el-form-item :label="t('budget.form.amount')" prop="amount_usd">
          <el-input-number v-model="editForm.amount_usd" :min="0" :precision="2" :step="10" style="width: 100%" />
          <div class="form-tip">{{ t('budget.form.amountTip') }}</div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="closeEditDialog">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleEdit" :loading="editing">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useOrgStore } from '@/stores/org'
import type { BudgetItem, BudgetScopeType, BudgetSetPayload } from '@/types/budget'
import { SCOPE_TYPE_LABELS, SCOPE_TYPE_COLORS } from '@/types/budget'
import { listBudgets, setBudget, deleteBudget } from '@/services/control-plane'

const { t } = useI18n()
const orgStore = useOrgStore()

const loading = ref(false)
const budgetList = ref<BudgetItem[]>([])
const loadingOrg = ref(false)

const filters = reactive({
  scope_type: '' as BudgetScopeType | '',
  scope_id: ''
})

const createDialogVisible = ref(false)
const createFormRef = ref<FormInstance>()
const creating = ref(false)
const createForm = reactive<{
  scope_type: BudgetScopeType | ''
  scope_id: string
  amount_usd: number
}>({
  scope_type: '',
  scope_id: '',
  amount_usd: 1000
})

const editDialogVisible = ref(false)
const editFormRef = ref<FormInstance>()
const editing = ref(false)
const editForm = reactive<{
  scope_type: BudgetScopeType
  scope_id: string
  amount_usd: number
}>({
  scope_type: 'company',
  scope_id: '',
  amount_usd: 0
})

const memberOptions = ref<Array<{ id: string; name: string; email: string }>>([])

const formRules = {
  scope_type: [{ required: true, message: t('budget.form.typeRequired'), trigger: 'change' }],
  scope_id: [{ required: true, message: t('budget.form.scopeRequired'), trigger: 'change' }],
  amount_usd: [{ required: true, message: t('budget.form.amountRequired'), trigger: 'blur' }]
}

// 计算可选的 scope 类型（排除已存在的 company）
const scopeOptions = computed(() => {
  return Object.keys(SCOPE_TYPE_LABELS).filter(key => {
    if (key === 'company') {
      // 公司级只能有一个
      const exists = budgetList.value.some(item => item.scope_type === 'company')
      return !exists
    }
    return true
  })
})

// 格式化金额
const formatAmount = (amount: number) => {
  return amount.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

// 根据使用率返回进度条颜色
const getProgressColor = (ratio: number) => {
  if (ratio >= 0.9) return '#f56c6c'
  if (ratio >= 0.7) return '#e6a23c'
  return '#67c23a'
}

// 获取 scope 的友好名称
const getScopeName = (scopeType: BudgetScopeType, scopeId: string) => {
  if (scopeType === 'company') {
    return t('budget.scopeName.company')
  }
  if (scopeType === 'studio') {
    const studio = orgStore.studios.find(s => s.id === scopeId)
    return studio?.name || t('budget.scopeName.unknown')
  }
  if (scopeType === 'dept_daily' || scopeType === 'dept_service') {
    const dept = orgStore.departments.find(d => d.dept_id === scopeId)
    return dept?.dept_name || t('budget.scopeName.unknown')
  }
  if (scopeType === 'member') {
    const user = memberOptions.value.find(u => u.id === scopeId)
    return user ? `${user.name} (${user.email})` : t('budget.scopeName.unknown')
  }
  return t('budget.scopeName.unknown')
}

// 加载预算列表
const loadBudgets = async () => {
  loading.value = true
  try {
    const data = await listBudgets({
      scope_type: filters.scope_type || undefined,
      scope_id: filters.scope_id || undefined
    })
    budgetList.value = data
  } catch (error: any) {
    ElMessage.error(error.message || t('budget.error.loadFailed'))
  } finally {
    loading.value = false
  }
}

// 加载组织数据
const loadOrgData = async () => {
  loadingOrg.value = true
  try {
    await Promise.all([
      orgStore.fetchStudios(),
      orgStore.fetchDepartments()
    ])
  } catch (error: any) {
    console.error('Failed to load org data:', error)
  } finally {
    loadingOrg.value = false
  }
}

// 重置筛选
const resetFilters = () => {
  filters.scope_type = ''
  filters.scope_id = ''
  loadBudgets()
}

// 打开创建对话框
const openCreateDialog = () => {
  createDialogVisible.value = true
  Object.assign(createForm, { scope_type: 'company', scope_id: '', amount_usd: 1000 })
  memberOptions.value = []
}

const closeCreateDialog = () => {
  createDialogVisible.value = false
  createFormRef.value?.resetFields()
  Object.assign(createForm, { scope_type: '', scope_id: '', amount_usd: 1000 })
  memberOptions.value = []
}

const onScopeTypeChange = () => {
  createForm.scope_id = ''
  if (createForm.scope_type === 'company') {
    createForm.scope_id = 'company'
  }
  memberOptions.value = []
}

const searchMembers = async (query: string) => {
  if (!query) {
    memberOptions.value = []
    return
  }
  try {
    const users = await orgStore.searchUsers(query)
    memberOptions.value = users
  } catch (error) {
    console.error('Search members failed:', error)
  }
}

// 处理创建
const handleCreate = async () => {
  if (!createFormRef.value) return
  
  await createFormRef.value.validate(async (valid) => {
    if (!valid) return

    creating.value = true
    try {
      const payload: BudgetSetPayload = {
        scope_type: createForm.scope_type!,
        scope_id: createForm.scope_id,
        amount_usd: createForm.amount_usd
      }
      await setBudget(payload)
      ElMessage.success(t('budget.success.createSuccess'))
      closeCreateDialog()
      loadBudgets()
    } catch (error: any) {
      ElMessage.error(error.message || t('budget.error.createFailed'))
    } finally {
      creating.value = false
    }
  })
}

// 打开编辑对话框
const openEditDialog = (row: BudgetItem) => {
  editDialogVisible.value = true
  Object.assign(editForm, {
    scope_type: row.scope_type,
    scope_id: row.scope_id,
    amount_usd: row.amount_usd
  })
}

const closeEditDialog = () => {
  editDialogVisible.value = false
  editFormRef.value?.resetFields()
}

// 处理编辑
const handleEdit = async () => {
  if (!editFormRef.value) return
  
  await editFormRef.value.validate(async (valid) => {
    if (!valid) return

    editing.value = true
    try {
      const payload: BudgetSetPayload = {
        scope_type: editForm.scope_type,
        scope_id: editForm.scope_id,
        amount_usd: editForm.amount_usd
      }
      await setBudget(payload)
      ElMessage.success(t('budget.success.updateSuccess'))
      closeEditDialog()
      loadBudgets()
    } catch (error: any) {
      ElMessage.error(error.message || t('budget.error.updateFailed'))
    } finally {
      editing.value = false
    }
  })
}

// 处理删除
const handleDelete = async (row: BudgetItem) => {
  try {
    await ElMessageBox.confirm(
      t('budget.confirm.delete', { name: getScopeName(row.scope_type, row.scope_id) }),
      t('common.confirm'),
      { type: 'warning' }
    )

    await deleteBudget(row.scope_type, row.scope_id)
    ElMessage.success(t('budget.success.deleteSuccess'))
    loadBudgets()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || t('budget.error.deleteFailed'))
    }
  }
}

onMounted(async () => {
  await loadOrgData()
  await loadBudgets()
})
</script>

<style scoped lang="scss">
.budget-page {
  padding: 20px;

  .page-header {
    margin-bottom: 24px;

    h2 {
      margin: 0 0 8px 0;
      font-size: 24px;
      font-weight: 600;
      color: var(--el-text-color-primary);
    }

    .description {
      margin: 0;
      font-size: 14px;
      color: var(--el-text-color-secondary);
    }
  }

  .filter-bar {
    display: flex;
    gap: 12px;
    margin-bottom: 20px;
  }

  .scope-name,
  .scope-name-edit {
    display: flex;
    flex-direction: column;
    gap: 4px;

    .name {
      font-size: 14px;
      color: var(--el-text-color-primary);
      font-weight: 500;
    }

    .id {
      font-size: 12px;
      color: var(--el-text-color-secondary);
    }
  }

  .scope-name-edit {
    width: 100%;
  }

  .amount {
    font-weight: 600;
    font-size: 16px;
  }

  .usage {
    font-family: 'Courier New', monospace;

    .separator {
      color: var(--el-text-color-secondary);
    }

    .used {
      color: var(--el-text-color-primary);
      font-weight: 500;
    }

    .total {
      color: var(--el-text-color-secondary);
    }
  }

  .progress-cell {
    display: flex;
    align-items: center;
    gap: 8px;

    .el-progress {
      flex: 1;
    }

    .ratio {
      font-size: 12px;
      color: var(--el-text-color-regular);
      min-width: 48px;
      text-align: right;
    }
  }

  .form-tip {
    margin-top: 4px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}
</style>
