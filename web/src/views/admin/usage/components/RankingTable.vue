<template>
  <div class="ud-section">
    <!-- 个人视角提示条 -->
    <div v-if="currentScope === 'user'" class="ud-user-scope-banner">
      <el-icon><User /></el-icon>
      <span>当前仅展示 <strong>{{ breadcrumbs[breadcrumbs.length - 1]?.name }}</strong> 的个人用量数据</span>
      <el-button link size="small" type="primary" style="margin-left:auto" @click="$emit('resetToTop')">
        退出个人视角
      </el-button>
    </div>

    <div class="ud-section__header">
      <div class="ud-section__title-row">
        <span class="ud-section__title">{{ rankTitle }}</span>
        <el-input
          :model-value="rankSearch"
          @update:model-value="onSearchChange"
          placeholder="搜索名称"
          clearable
          size="small"
          class="ud-rank-search"
        >
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
      </div>
      <div class="ud-breadcrumb">
        <span class="ud-bc-home" @click="$emit('resetToTop')">{{ topLabel }}</span>
        <template v-for="(crumb, idx) in breadcrumbs" :key="crumb.id">
          <el-icon class="ud-bc-sep"><ArrowRight /></el-icon>
          <span
            :class="['ud-bc-item', { 'ud-bc-current': idx === breadcrumbs.length - 1 }]"
            @click="idx < breadcrumbs.length - 1 && $emit('popToLevel', idx)"
          >{{ crumb.name }}</span>
        </template>
      </div>
    </div>

    <div class="ud-table-wrap" v-loading="loading">
      <table class="ud-rank-table">
        <thead>
          <tr>
            <th class="col-rank">#</th>
            <th class="col-name">名称</th>
            <th>
              <el-tooltip content="该成员成本占本期团队总成本的比例" placement="top">
                <span style="cursor:help;border-bottom:1px dashed currentColor">成本占比</span>
              </el-tooltip>
            </th>
            <th class="col-num col-sortable" @click="$emit('toggleSort', 'cost')">
              成本 ({{ currency }})
              <span class="ud-sort-arrows">
                <span :class="['ud-sort-arrow', rankSort.field === 'cost' && rankSort.order === 'asc' ? 'ud-sort-arrow--active' : '']">▲</span>
                <span :class="['ud-sort-arrow', rankSort.field === 'cost' && rankSort.order === 'desc' ? 'ud-sort-arrow--active' : '']">▼</span>
              </span>
            </th>
            <th class="col-num col-sortable" @click="$emit('toggleSort', 'calls')">
              调用次数
              <span class="ud-sort-arrows">
                <span :class="['ud-sort-arrow', rankSort.field === 'calls' && rankSort.order === 'asc' ? 'ud-sort-arrow--active' : '']">▲</span>
                <span :class="['ud-sort-arrow', rankSort.field === 'calls' && rankSort.order === 'desc' ? 'ud-sort-arrow--active' : '']">▼</span>
              </span>
            </th>
            <th class="col-num col-sortable" @click="$emit('toggleSort', 'tokens')">
              {{ currentScope === 'user' ? 'Token 消耗' : '活跃用户' }}
              <span class="ud-sort-arrows">
                <span :class="['ud-sort-arrow', rankSort.field === 'tokens' && rankSort.order === 'asc' ? 'ud-sort-arrow--active' : '']">▲</span>
                <span :class="['ud-sort-arrow', rankSort.field === 'tokens' && rankSort.order === 'desc' ? 'ud-sort-arrow--active' : '']">▼</span>
              </span>
            </th>
            <th v-if="currentScope === 'user'" class="col-models">常用模型</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, i) in displayedBreakdown"
            :key="row.id ?? i"
            :class="{ 'ud-rank-drillable': canDrill }"
          >
            <td class="col-rank">
              <span :class="['ud-rank-num', i < 3 ? `ud-rank-num--top${i+1}` : '']">{{ row.rank ?? i + 1 }}</span>
            </td>
            <td class="col-name">
              <span
                :class="['ud-row-name', { drillable: canDrill }]"
                @click="canDrill && $emit('drillDown', row)"
              >{{ row.name || row.id || '(未归属)' }}</span>
            </td>
            <td>
              <div class="ud-bar-wrap">
                <div class="ud-bar-bg">
                  <div class="ud-bar-fill ud-bar-fill--cost" :style="{ width: barPct(row.cost_estimate, totalCost) + '%' }" />
                </div>
                <span class="ud-bar-pct">{{ barPct(row.cost_estimate, totalCost) }}%</span>
              </div>
            </td>
            <td class="col-num c-green fw6">{{ fmtCost(row.cost_estimate) }}</td>
            <td class="col-num">{{ fmtNum(row.call_count) }}</td>
            <td class="col-num">
              <template v-if="currentScope === 'user'">
                {{ row.total_tokens != null ? (row.total_tokens >= 1000000 ? (row.total_tokens/1000000).toFixed(1)+'M' : (row.total_tokens/1000).toFixed(1)+'K') : '—' }}
              </template>
              <template v-else>{{ row.user_count ?? '—' }}</template>
            </td>
            <td v-if="currentScope === 'user'" class="col-models">
              <div class="ud-model-tags">
                <el-tooltip
                  v-for="(m, mi) in getUserModels(row.id)"
                  :key="mi"
                  :content="`${m.model} · $${m.est_cost_usd.toFixed(2)}`"
                  placement="top"
                >
                  <span class="ud-model-tag">{{ shortModelName(m.model) }}</span>
                </el-tooltip>
                <span v-if="getUserModels(row.id).length === 0" class="ud-model-tag ud-model-tag--empty">—</span>
              </div>
            </td>
          </tr>
          <tr v-if="!loading && displayedBreakdown.length === 0">
            <td :colspan="currentScope === 'user' ? 7 : 6" class="ud-empty">暂无数据</td>
          </tr>
        </tbody>
      </table>
      <div v-if="rankTotal > rankPageSize" class="ud-pagination">
        <el-pagination
          v-model:current-page="rankPageModel"
          :page-size="rankPageSize"
          :total="rankTotal"
          layout="total, prev, pager, next"
          small
          @current-change="$emit('loadSummary')"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Search, ArrowRight, User } from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'

defineOptions({ name: 'RankingTable' })

const props = defineProps<{
  loading: boolean
  displayedBreakdown: any[]
  currentScope: string
  canDrill: boolean
  breadcrumbs: Array<{ scope: string; id: string; name: string }>
  topLabel: string
  rankTitle: string
  rankTotal: number
  rankPageSize: number
  rankSort: { field: string; order: string }
  totalCost: number
  currency: Currency
  userTopModels: Record<string, any[]>
  rankPage: number
  rankSearch: string
}>()

const emit = defineEmits<{
  drillDown: [row: any]
  toggleSort: [field: string]
  resetToTop: []
  popToLevel: [idx: number]
  loadSummary: []
  'update:rankPage': [value: number]
  'update:rankSearch': [value: string]
}>()

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

// ── rankPage v-model proxy ──────────────────────────────────────
const rankPageModel = computed({
  get: () => props.rankPage,
  set: (val: number) => emit('update:rankPage', val),
})

function onSearchChange(val: string) {
  emit('update:rankSearch', val)
  emit('update:rankPage', 1)
}

// ── Helper functions ────────────────────────────────────────────

function fmtCost(v?: number | null): string {
  const n = convertCost(Number(v || 0), cc.value)
  const sym = currencySymbol(cc.value.currency)
  if (n === 0) return `${sym}0.00`
  let s: string
  if (Math.abs(n) >= 1)        s = n.toFixed(2)
  else if (Math.abs(n) >= 0.01) s = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001) s = n.toFixed(6)
  else s = n.toFixed(8)
  return sym + s.replace(/(\.\d{2,}?)0+$/, '$1')
}

function fmtNum(n?: number | null): string {
  return (n ?? 0).toLocaleString()
}

function barPct(v: number, max: number) {
  return !max || !v ? 0 : Math.round(v / max * 100)
}

function shortModelName(name: string): string {
  return name.replace(/-\d{4}-\d{2}-\d{2}$/, '').replace(/-(mini|nano|flash|thinking)$/, ' $1')
}

function getUserModels(userId: string): { model: string; est_cost_usd: number }[] {
  return props.userTopModels[userId] ?? []
}
</script>

<style scoped>
/* ── 排行表格 ── */
.ud-table-wrap { padding: 0; overflow: auto; }
.ud-rank-table {
  width: 100%; border-collapse: collapse;
  font-size: 13px; table-layout: auto;
}
.ud-rank-table thead tr { border-bottom: 1px solid var(--ud-border); }
.ud-rank-table th {
  padding: 8px 12px; text-align: left;
  font-weight: 600; font-size: 12px;
  color: var(--ud-muted); white-space: nowrap;
  background: var(--el-fill-color-light);
}
.ud-rank-table td { padding: 8px 12px; border-bottom: 1px solid var(--ud-border); }
.ud-rank-table tbody tr:last-child td { border-bottom: none; }
.ud-rank-table tbody tr:hover td { background: var(--el-fill-color-extra-light); }
.ud-rank-drillable td { cursor: pointer; }

.col-rank { width: 40px; text-align: center; }
.col-name { min-width: 120px; }
.col-num  { text-align: right; white-space: nowrap; }
.col-models { min-width: 180px; }

/* ── 模型标签 ── */
.ud-model-tags { display: flex; gap: 4px; flex-wrap: wrap; }
.ud-model-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 500;
  line-height: 1.5;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  border: 1px solid var(--el-color-primary-light-7);
  white-space: nowrap;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  cursor: default;
  transition: background .15s, border-color .15s;
}
.ud-model-tag:hover {
  background: var(--el-color-primary-light-8);
  border-color: var(--el-color-primary-light-5);
}
.ud-model-tag--empty {
  background: var(--el-fill-color);
  color: var(--ud-faint);
  border-color: transparent;
}

.ud-rank-num {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px; border-radius: 6px;
  background: var(--el-fill-color); color: var(--ud-muted);
  font-size: 12px; font-weight: 600;
}
.ud-rank-num--top1 { background: #ffd700; color: #7a5f00; }
.ud-rank-num--top2 { background: #c0c0c0; color: #555; }
.ud-rank-num--top3 { background: #cd7f32; color: #fff; }

.ud-row-name { color: var(--el-text-color-primary); }
.ud-row-name.drillable { color: var(--el-color-primary); cursor: pointer; font-weight: 500; }
.ud-row-name.drillable:hover { text-decoration: underline; }

.ud-bar-wrap { display: flex; align-items: center; gap: 8px; min-width: 120px; }
.ud-bar-bg   { flex: 1; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; }
.ud-bar-fill { height: 100%; border-radius: 3px; transition: width .3s; background: var(--el-color-primary); }
.ud-bar-fill--cost { background: linear-gradient(90deg, #67c23a, #95d475); }
.ud-bar-pct  { font-size: 12px; color: var(--ud-muted); white-space: nowrap; min-width: 36px; }
.ud-empty    { padding: 32px; text-align: center; color: var(--ud-faint); }

/* ── 面包屑 ── */
.ud-user-scope-banner {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 14px;
  background: #ecf5ff; border: 1px solid #b3d8ff;
  border-radius: 6px; font-size: 13px; color: #409eff;
}
.ud-user-scope-banner strong { color: #1a6fd4; }

.ud-breadcrumb { display: flex; align-items: center; gap: 4px; font-size: 13px; flex-wrap: wrap; }
.ud-bc-home { color: var(--el-color-primary); cursor: pointer; font-weight: 500; }
.ud-bc-home:hover { text-decoration: underline; }
.ud-bc-sep  { color: var(--ud-faint); font-size: 11px; }
.ud-bc-item { color: var(--ud-muted); cursor: pointer; }
.ud-bc-item:hover { color: var(--el-color-primary); }
.ud-bc-current { color: var(--el-text-color-primary); font-weight: 500; cursor: default; }

/* ── 排行表格排序 & 搜索 ── */
.ud-section__title-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ud-rank-search { width: 140px; }

.col-sortable { cursor: pointer; user-select: none; }
.col-sortable:hover { color: var(--el-color-primary); }
.ud-sort-arrows { display: inline-flex; flex-direction: column; margin-left: 4px; vertical-align: middle; line-height: 1; gap: 0; }
.ud-sort-arrow  { font-size: 8px; color: var(--el-border-color); line-height: 1; display: block; }
.ud-sort-arrow--active { color: var(--el-color-primary); }

/* ── 分页 ── */
.ud-pagination { display: flex; justify-content: flex-end; padding: 8px 12px; }

/* ── Section 通用 (self-contained) ── */
.ud-section {
  background: var(--ud-surface);
  border: 1px solid var(--ud-border);
  border-radius: 14px;
}
.ud-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 14px 6px; border-bottom: 1px solid var(--ud-border);
  flex-wrap: wrap; gap: 6px;
}
.ud-section__title { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
</style>
