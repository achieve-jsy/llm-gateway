<template>
  <section aria-labelledby="risk-heading">
    <header class="section-header">
      <div><h2 id="risk-heading">实时风险事件</h2><p>事件由实时用量和实际阻断投影，风险消失后会自动移除。</p></div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </header>
    <div class="filters" aria-label="风险筛选">
      <el-select v-model="filter.period_type" placeholder="全部周期" clearable><el-option label="日" value="daily" /><el-option label="周" value="weekly" /><el-option label="月" value="monthly" /></el-select>
      <el-select v-model="filter.dimension_type" placeholder="全部维度" clearable><el-option label="总预算" value="total" /><el-option label="模型等级" value="tier" /></el-select>
      <el-select v-model="filter.risk_type" placeholder="全部类型" clearable><el-option label="达到阈值" value="threshold_reached" /><el-option label="预算耗尽" value="budget_exhausted" /><el-option label="请求被阻断" value="request_blocked" /></el-select>
      <el-select v-model="filter.severity" placeholder="全部严重度" clearable><el-option label="警告" value="warning" /><el-option label="严重" value="critical" /></el-select>
      <el-button type="primary" plain @click="load">筛选</el-button>
    </div>
    <DataFreshnessNotice :stale="page.stale" :updated-at="page.data_as_of" />
    <el-table v-loading="loading" :data="page.items" empty-text="当前没有预算风险" @row-click="open">
      <el-table-column label="严重度" width="90"><template #default="{ row }"><el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'">{{ row.severity === 'critical' ? '严重' : '警告' }}</el-tag></template></el-table-column>
      <el-table-column prop="scope_name" label="影响对象" min-width="150" />
      <el-table-column label="维度" min-width="130"><template #default="{ row }">{{ row.dimension_type === 'tier' ? (row.model_tier_name || '模型等级') : '总预算' }} / {{ periodLabel(row.period_type) }}</template></el-table-column>
      <el-table-column label="额度 / 已用" min-width="160"><template #default="{ row }">${{ row.effective_limit_usd }} / ${{ row.used_usd }}</template></el-table-column>
      <el-table-column prop="usage_ratio" label="使用率" width="100" />
      <el-table-column prop="limit_source" label="限制来源" min-width="140" />
      <el-table-column prop="blocked_requests" label="阻断次数" width="100" />
      <el-table-column prop="detected_at" label="发现时间" min-width="170"><template #default="{ row }">{{ formatTime(row.detected_at) }}</template></el-table-column>
    </el-table>

    <el-drawer v-model="drawer" title="风险详情" size="min(560px, 100%)">
      <template v-if="selected">
        <el-descriptions :column="1" border>
          <el-descriptions-item label="对象">{{ selected.scope_name }}</el-descriptions-item>
          <el-descriptions-item label="预算链限制">{{ selected.limit_source }}</el-descriptions-item>
          <el-descriptions-item label="额度">${{ selected.effective_limit_usd }}</el-descriptions-item>
          <el-descriptions-item label="已用">${{ selected.used_usd }}</el-descriptions-item>
          <el-descriptions-item label="实际阻断">{{ selected.blocked_requests }} 次</el-descriptions-item>
        </el-descriptions>
        <DataFreshnessNotice class="drawer-freshness" :stale="selected.stale" :updated-at="selected.data_as_of" />
        <div class="drawer-actions">
          <el-button @click="viewBudget">查看预算</el-button>
          <el-button type="primary" :disabled="selected.stale" @click="requestExpansion">发起扩容</el-button>
          <el-button @click="notifyOwner">通知负责人</el-button>
        </div>
      </template>
    </el-drawer>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import DataFreshnessNotice from '@/components/common/DataFreshnessNotice.vue'
import { listBudgetRisks, notifyBudgetOwner } from '@/services/control-plane'
import type { BudgetRiskEvent, BudgetRiskFilter, BudgetRiskPage } from '@/types/budget-risk'

const emit = defineEmits<{ viewBudget: [event: BudgetRiskEvent] }>()
const router = useRouter()
const loading = ref(false)
const drawer = ref(false)
const selected = ref<BudgetRiskEvent | null>(null)
const filter = reactive<BudgetRiskFilter>({})
const page = reactive<BudgetRiskPage>({ items: [], total: 0, data_as_of: '', stale: false })
async function load() {
  loading.value = true
  try { Object.assign(page, await listBudgetRisks(filter)) } finally { loading.value = false }
}
function open(event: BudgetRiskEvent) { selected.value = event; drawer.value = true }
function viewBudget() { if (selected.value) emit('viewBudget', selected.value); drawer.value = false }
function requestExpansion() {
  if (!selected.value) return
  router.push({ path: '/admin/expansion-requests', query: {
    scope_type: selected.value.scope_type, scope_id: selected.value.scope_id,
    period_type: selected.value.period_type, dimension_type: selected.value.dimension_type,
    model_tier_id: selected.value.model_tier_id || undefined,
  } })
}
async function notifyOwner() {
  if (!selected.value) return
  await notifyBudgetOwner({
    scope_type: selected.value.scope_type, scope_id: selected.value.scope_id,
    period_type: selected.value.period_type, dimension_type: selected.value.dimension_type,
    model_tier_id: selected.value.model_tier_id,
  })
  ElMessage.success('已通知预算负责人')
}
const periodLabel = (value: string) => ({ daily: '日', weekly: '周', monthly: '月' }[value] || value)
const formatTime = (value: string) => new Date(value).toLocaleString()
onMounted(load)
</script>

<style scoped>
section { display: grid; gap: 16px; }
.section-header { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
h2 { margin: 0 0 6px; font-size: 18px; }
p { margin: 0; color: var(--el-text-color-secondary); }
.filters { display: flex; flex-wrap: wrap; gap: 10px; }
.filters .el-select { width: 150px; }
.drawer-freshness, .drawer-actions { margin-top: 20px; }
.drawer-actions { display: flex; flex-wrap: wrap; gap: 8px; }
@media (max-width: 768px) { .filters .el-select { width: calc(50% - 5px); } }
</style>
