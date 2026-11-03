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

    <!-- 程序调用摘要条（dept 范围，独立于成员排行） -->
    <div v-if="showServiceStrip" class="ud-service-strip">
      <div class="ud-service-strip__icon"><el-icon><Connection /></el-icon></div>
      <div class="ud-service-strip__body">
        <div class="ud-service-strip__title">程序调用（Service Key）</div>
        <div class="ud-service-strip__meta">
          {{ fmtCost(serviceSummary!.cost_estimate) }} · {{ fmtNum(serviceSummary!.call_count) }} 次 · {{ fmtTokens(serviceSummary!.total_tokens) }}
        </div>
      </div>
      <el-button link type="primary" size="small" class="ud-service-strip__action" @click="$emit('viewServiceCalls')">
        查看全部 <el-icon><ArrowRight /></el-icon>
      </el-button>
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
            <th class="col-num col-sortable" :class="{ 'col-sortable--active': rankSort.field === 'cost' }" @click="$emit('toggleSort', 'cost')">
              成本 ({{ currency }})
              <SortIndicator field="cost" :active-field="rankSort.field" :order="rankSort.order" />
            </th>
            <th class="col-num col-sortable" :class="{ 'col-sortable--active': rankSort.field === 'calls' }" @click="$emit('toggleSort', 'calls')">
              调用次数
              <SortIndicator field="calls" :active-field="rankSort.field" :order="rankSort.order" />
            </th>
            <th class="col-num col-sortable" :class="{ 'col-sortable--active': rankSort.field === 'tokens' }" @click="$emit('toggleSort', 'tokens')">
              {{ currentScope === 'user' ? 'Token 消耗' : '活跃用户' }}
              <SortIndicator field="tokens" :active-field="rankSort.field" :order="rankSort.order" />
            </th>
            <th v-if="isEnhancedUserView" class="col-num col-sortable" :class="{ 'col-sortable--active': rankSort.field === 'change' }" @click="$emit('toggleSort', 'change')">
              <el-tooltip content="相比上一期的成本变化" placement="top">
                <span style="border-bottom:1px dashed currentColor">环比</span>
              </el-tooltip>
              <SortIndicator field="change" :active-field="rankSort.field" :order="rankSort.order" />
            </th>
            <th v-if="isEnhancedUserView" class="col-num col-sortable" :class="{ 'col-sortable--active': rankSort.field === 'cpt' }" @click="$emit('toggleSort', 'cpt')">
              <el-tooltip content="每千 Token 的平均成本" placement="top">
                <span style="border-bottom:1px dashed currentColor">千Token成本</span>
              </el-tooltip>
              <SortIndicator field="cpt" :active-field="rankSort.field" :order="rankSort.order" />
            </th>
            <th v-if="isEnhancedUserView" class="col-num col-sortable" :class="{ 'col-sortable--active': rankSort.field === 'err' }" @click="$emit('toggleSort', 'err')">
              <el-tooltip content="调用失败率" placement="top">
                <span style="border-bottom:1px dashed currentColor">错误率</span>
              </el-tooltip>
              <SortIndicator field="err" :active-field="rankSort.field" :order="rankSort.order" />
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
                :class="['ud-row-name', { drillable: canDrill || isEnhancedUserView, 'ud-row-name--detail': isEnhancedUserView }]"
                @click="isEnhancedUserView ? $emit('userClick', row) : (canDrill && $emit('drillDown', row))"
              >{{ row.name || row.id || '未关联用户' }}</span>
              <el-icon v-if="isEnhancedUserView" class="ud-detail-hint"><View /></el-icon>
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
            <td v-if="isEnhancedUserView" class="col-num" :class="pctClass(getEnhanced(row)?.cost_change_pct)">
              {{ fmtPct(getEnhanced(row)?.cost_change_pct) }}
            </td>
            <td v-if="isEnhancedUserView" class="col-num ud-mono">
              <template v-if="getEnhanced(row)">
                ${{ getEnhanced(row)!.cost_per_1k_tokens?.toFixed(4) ?? '—' }}
              </template>
              <template v-else>—</template>
            </td>
            <td v-if="isEnhancedUserView" class="col-num" :class="{ 'c-red': (getEnhanced(row)?.error_rate ?? 0) > 5 }">
              <template v-if="getEnhanced(row)">
                {{ getEnhanced(row)!.error_rate?.toFixed(1) ?? '—' }}%
              </template>
              <template v-else>—</template>
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
            <td :colspan="isEnhancedUserView ? 10 : (currentScope === 'user' ? 7 : 6)" class="ud-empty">暂无数据</td>
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

<script lang="ts">
// SortIndicator must be registered in a non-setup <script> because
// defineOptions() is hoisted and cannot reference locally-declared variables.
import { defineComponent } from 'vue'

const SortIndicator = defineComponent({
  name: 'SortIndicator',
  props: {
    field: { type: String, required: true },
    activeField: { type: String, required: true },
    order: { type: String, default: '' },
  },
  template: `
    <span class="sort-indicator">
      <svg class="sort-chevron" :class="{ active: field === activeField && order === 'asc' }" viewBox="0 0 10 6" width="10" height="6" fill="none">
        <path d="M1 5L5 1L9 5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
      <svg class="sort-chevron" :class="{ active: field === activeField && order === 'desc' }" viewBox="0 0 10 6" width="10" height="6" fill="none">
        <path d="M1 1L5 5L9 1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
    </span>
  `,
})

export default { components: { SortIndicator } }
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { Search, ArrowRight, User, View, Connection } from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import type { UserRankRow } from '@/types/dashboard-enhanced'
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
  rankSort: { field: string; order: string } // SortField union from parent
  totalCost: number
  currency: Currency
  userTopModels: Record<string, any[]>
  rankPage: number
  rankSearch: string
  enhancedUserMap?: Record<string, UserRankRow>
  serviceSummary?: { cost_estimate: number; call_count: number; total_tokens: number } | null
}>()

const emit = defineEmits<{
  drillDown: [row: any]
  toggleSort: [field: string]
  resetToTop: []
  popToLevel: [idx: number]
  loadSummary: []
  'update:rankPage': [value: number]
  'update:rankSearch': [value: string]
  userClick: [row: any]
  viewServiceCalls: []
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

function getEnhanced(row: any): UserRankRow | undefined {
  if (!props.enhancedUserMap || props.currentScope !== 'dept') return undefined
  return props.enhancedUserMap[row.id]
}

function fmtPct(v: number | undefined | null): string {
  if (v == null) return '—'
  const sign = v > 0 ? '+' : ''
  return `${sign}${v.toFixed(1)}%`
}

function pctClass(v: number | undefined | null, isCost = true): string {
  if (v == null || v === 0) return 'ud-pct--neutral'
  // For cost: increase is bad (red), decrease is good (green)
  if (isCost) return v > 0 ? 'ud-pct--bad' : 'ud-pct--good'
  return v > 0 ? 'ud-pct--up' : 'ud-pct--down'
}

const isEnhancedUserView = computed(() =>
  props.currentScope === 'dept' && props.enhancedUserMap && Object.keys(props.enhancedUserMap).length > 0
)

const showServiceStrip = computed(() =>
  props.currentScope === 'dept' &&
  props.serviceSummary != null &&
  ((props.serviceSummary.cost_estimate || 0) > 0 || (props.serviceSummary.call_count || 0) > 0)
)

function fmtTokens(n?: number | null): string {
  if (!n) return '0'
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
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
.ud-row-name--detail { color: var(--el-color-primary); cursor: pointer; font-weight: 500; }
.ud-row-name--detail:hover { text-decoration: underline; }
.ud-detail-hint { font-size: 13px; color: var(--el-color-primary); margin-left: 4px; vertical-align: middle; opacity: 0.6; }

.ud-bar-wrap { display: flex; align-items: center; gap: 8px; min-width: 120px; }
.ud-bar-bg   { flex: 1; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; }
.ud-bar-fill { height: 100%; border-radius: 3px; transition: width .3s; background: var(--el-color-primary); }
.ud-bar-fill--cost { background: linear-gradient(90deg, #67c23a, #95d475); }
.ud-bar-pct  { font-size: 12px; color: var(--ud-muted); white-space: nowrap; min-width: 36px; }
.ud-empty    { padding: 32px; text-align: center; color: var(--ud-faint); }

/* ── 程序调用摘要条 ── */
.ud-service-strip {
  display: flex; align-items: center; gap: 10px;
  margin: 8px 14px 0; padding: 8px 12px;
  border-radius: 8px; border: 1px solid rgba(139,92,246,0.25);
  border-left: 3px solid #8B5CF6; background: rgba(139,92,246,0.05);
}
.ud-service-strip__icon { color: #8B5CF6; display: flex; }
.ud-service-strip__body { flex: 1; min-width: 0; }
.ud-service-strip__title { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
.ud-service-strip__meta { font-size: 12px; color: var(--ud-muted); margin-top: 2px; }
.ud-service-strip__action { flex-shrink: 0; }

/* ── 面包屑 ── */
.ud-user-scope-banner {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 14px;
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-5);
  border-radius: 6px; font-size: 13px; color: var(--el-color-primary);
}
.ud-user-scope-banner strong { color: var(--el-color-primary-dark-2); }

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

.col-sortable {
  cursor: pointer; user-select: none;
  transition: color .2s, background .2s;
}
.col-sortable:hover { color: var(--el-color-primary); }

/* Active sort column header: subtle tinted background */
.col-sortable--active {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

/* ── SortIndicator (SVG chevron pair) ── */
.sort-indicator {
  display: inline-flex;
  flex-direction: column;
  align-items: center;
  margin-left: 4px;
  vertical-align: middle;
  gap: 1px;
  line-height: 1;
}
.sort-chevron {
  color: var(--el-border-color);
  transition: color .2s, transform .2s;
  flex-shrink: 0;
}
.sort-chevron.active {
  color: var(--el-color-primary);
  transform: scaleY(1.2);
}

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

/* ── 增强列样式 ── */
.ud-mono { font-family: var(--ud-mono, 'SFMono-Regular', ui-monospace, monospace); font-size: 12px; }
.ud-pct--good { color: #67c23a; }
.ud-pct--bad  { color: #f56c6c; }
.ud-pct--neutral { color: var(--el-text-color-placeholder); }
.ud-pct--up   { color: #67c23a; }
.ud-pct--down { color: #f56c6c; }
</style>
