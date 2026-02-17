<template>
  <div class="urt-wrap">
    <!-- 表格 -->
    <div class="urt-table-wrap" v-loading="loading">
      <el-table
        :data="rows"
        size="small"
        :header-cell-style="TABLE_HEADER"
        style="width:100%;cursor:pointer"
        @row-click="openTrace"
      >
        <!-- Key（showKey 时显示，fixed=left） -->
        <el-table-column v-if="showKey" prop="key_name" label="Key" min-width="110" fixed="left">
          <template #default="{ row }">
            <span class="urt-name">{{ resolveKey(row) }}</span>
          </template>
        </el-table-column>

        <!-- 用户 -->
        <el-table-column v-if="showUser" prop="user_name" :label="userLabel ?? '用户'" min-width="90" :fixed="showKey ? 'left' : false">
          <template #default="{ row }">
            <span v-if="row.user_name" class="urt-name">{{ row.user_name }}</span>
            <span v-else class="urt-dash">—</span>
          </template>
        </el-table-column>

        <!-- 部门 -->
        <el-table-column v-if="showDept" prop="dept_name" label="部门" min-width="100">
          <template #default="{ row }">
            <span v-if="row.dept_name">{{ row.dept_name }}</span>
            <span v-else-if="row.dept_id" class="urt-mono urt-mono--sm">{{ row.dept_id?.substring(0, 8) }}…</span>
            <span v-else class="urt-dash">—</span>
          </template>
        </el-table-column>

        <!-- 事业部 / 工作室 -->
        <el-table-column v-if="showStudio" prop="studio_name" :label="studioLabel ?? '事业部'" min-width="90">
          <template #default="{ row }">
            <span v-if="row.studio_name">{{ row.studio_name }}</span>
            <span v-else class="urt-dash">—</span>
          </template>
        </el-table-column>

        <!-- 模型 / 路由 -->
        <el-table-column prop="model" label="模型 / 路由" min-width="160">
          <template #default="{ row }">
            <div class="urt-model-cell">
              <span class="urt-model-name">{{ row.model || '—' }}</span>
              <div v-if="showRoutedModel(row)" class="urt-model-routed">
                <span class="urt-arrow">↳</span>
                <span class="urt-model-name urt-model-name--muted">{{ row.routed_model }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <!-- 模型组 -->
        <el-table-column v-if="showGroup" prop="group_name" label="模型组" min-width="180" class-name="urt-col--wrap">
          <template #default="{ row }">
            <span v-if="row.group_name">{{ row.group_name }}</span>
            <span v-else class="urt-dash">—</span>
          </template>
        </el-table-column>

        <!-- 渠道 -->
        <el-table-column v-if="showProvider" prop="provider" label="渠道" min-width="90">
          <template #default="{ row }">
            <span v-if="row.provider" class="urt-provider">{{ row.provider }}</span>
            <span v-else class="urt-dash">—</span>
          </template>
        </el-table-column>

        <!-- 类型 -->
        <el-table-column label="类型" min-width="70" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.stream" type="primary" size="small" effect="plain">流式</el-tag>
            <el-tag v-else type="" size="small" effect="plain">同步</el-tag>
          </template>
        </el-table-column>

        <!-- Token 用量 -->
        <el-table-column label="Token 用量" min-width="210">
          <template #default="{ row }">
            <div class="urt-token-row">
              <span class="urt-token-item urt-token-item--in" title="输入">
                <el-icon class="urt-token-icon"><Bottom /></el-icon>{{ fmtTokens(row.prompt_tokens) }}
              </span>
              <span class="urt-token-item urt-token-item--out" title="输出">
                <el-icon class="urt-token-icon"><Top /></el-icon>{{ fmtTokens(row.completion_tokens) }}
              </span>
              <span v-if="row.cached_tokens" class="urt-token-item urt-token-item--cache" title="缓存命中">
                <el-icon class="urt-token-icon"><Finished /></el-icon>{{ fmtTokens(row.cached_tokens) }}
              </span>
              <span v-if="row.reasoning_tokens" class="urt-token-item urt-token-item--reason" title="推理">
                <el-icon class="urt-token-icon"><MagicStick /></el-icon>{{ fmtTokens(row.reasoning_tokens) }}
              </span>
              <el-tooltip placement="top" :show-after="120" popper-class="urt-tt-popper">
                <template #content>
                  <div class="urt-tt">
                    <div class="urt-tt__title">Token 明细</div>
                    <div class="urt-tt__row"><span>输入（含缓存）</span><b>{{ fmtNum(row.prompt_tokens) }}</b></div>
                    <div class="urt-tt__row"><span>输出</span><b>{{ fmtNum(row.completion_tokens) }}</b></div>
                    <div class="urt-tt__row" v-if="row.reasoning_tokens"><span class="urt-tt__sub">└ 推理</span><b class="c-purple">{{ fmtNum(row.reasoning_tokens) }}</b></div>
                    <div class="urt-tt__sep"></div>
                    <div class="urt-tt__group">缓存</div>
                    <div class="urt-tt__row"><span>缓存命中（读）</span><b :class="row.cached_tokens ? 'c-cyan' : ''">{{ fmtNum(row.cached_tokens) }}</b></div>
                    <div class="urt-tt__row"><span>缓存写入</span><b :class="row.cache_write_tokens ? 'c-amber' : ''">{{ fmtNum(row.cache_write_tokens) }}</b></div>
                    <div class="urt-tt__row"><span class="urt-tt__sub">未缓存输入</span><b>{{ fmtNum(Math.max((row.prompt_tokens||0) - (row.cached_tokens||0) - (row.cache_write_tokens||0), 0)) }}</b></div>
                    <div class="urt-tt__sep"></div>
                    <div class="urt-tt__row"><span>总计</span><b class="c-blue">{{ fmtNum(row.total_tokens) }}</b></div>
                  </div>
                </template>
                <span class="urt-info"><el-icon><InfoFilled /></el-icon></span>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>

        <!-- 状态 -->
        <el-table-column prop="status" label="状态" min-width="70" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 200 ? 'success' : 'danger'" size="small" effect="dark">{{ row.status || '—' }}</el-tag>
          </template>
        </el-table-column>

        <!-- TTFT -->
        <el-table-column prop="first_token_ms" label="TTFT" min-width="70" align="right">
          <template #default="{ row }">
            <span v-if="row.first_token_ms != null" class="urt-duration">{{ fmtDuration(row.first_token_ms) }}</span>
            <span v-else class="urt-dash">—</span>
          </template>
        </el-table-column>

        <!-- 耗时 -->
        <el-table-column
          prop="duration_ms"
          label="耗时"
          min-width="70"
          align="right"
          :sortable="sortableColumns?.includes('duration_ms') || false"
        >
          <template #default="{ row }">
            <span class="urt-duration">{{ fmtDuration(row.duration_ms) }}</span>
          </template>
        </el-table-column>

        <!-- 标记 -->
        <el-table-column label="标记" min-width="90" align="center">
          <template #default="{ row }">
            <div class="urt-flags">
              <el-tag v-if="row.reasoning_effort" :type="reasoningType(row.reasoning_effort)" size="small" effect="plain" class="urt-tag--effort">{{ row.reasoning_effort }}</el-tag>
              <el-tag v-if="row.failover_used" type="warning" size="small" effect="plain">切换</el-tag>
              <el-tag v-if="row.usage_missing" type="info" size="small" effect="plain">无Usage</el-tag>
            </div>
          </template>
        </el-table-column>

        <!-- 费用 -->
        <el-table-column
          prop="est_cost_usd"
          label="费用"
          min-width="140"
          align="right"
          :sortable="sortableColumns?.includes('est_cost_usd') || false"
        >
          <template #default="{ row }">
            <div class="urt-cost-cell">
              <span class="urt-cost" :class="logCostColor(row.est_cost_usd)">{{ fmtCost(row.est_cost_usd) }}</span>
              <el-tooltip placement="left" :show-after="120" popper-class="urt-tt-popper">
                <template #content>
                  <div class="urt-tt">
                    <div class="urt-tt__title">成本明细 ({{ currency ?? 'USD' }})</div>
                    <template v-if="(row.input_price_snapshot ?? 0) > 0 || (row.output_price_snapshot ?? 0) > 0">
                      <div class="urt-tt__group">单价 / 1M tokens</div>
                      <div class="urt-tt__row"><span>输入单价</span><b>{{ fmtPrice(row.input_price_snapshot) }}</b></div>
                      <div class="urt-tt__row"><span>输出单价</span><b>{{ fmtPrice(row.output_price_snapshot) }}</b></div>
                      <div class="urt-tt__sep"></div>
                    </template>
                    <div class="urt-tt__group">费用</div>
                    <div class="urt-tt__row">
                      <span>输入<span class="urt-tt__sub" v-if="row.prompt_tokens"> ×{{ fmtNum(Math.max((row.prompt_tokens||0) - (row.cached_tokens||0) - (row.cache_write_tokens||0), 0)) }}</span></span><b>{{ fmtCost6(row.input_cost_usd) }}</b>
                    </div>
                    <div class="urt-tt__row">
                      <span>输出<span class="urt-tt__sub" v-if="row.completion_tokens"> ×{{ fmtNum(row.completion_tokens) }}</span></span><b>{{ fmtCost6(row.output_cost_usd) }}</b>
                    </div>
                    <div class="urt-tt__sep"></div>
                    <div class="urt-tt__group">缓存</div>
                    <div class="urt-tt__row">
                      <span>缓存读取<span class="urt-tt__sub" v-if="row.cached_tokens"> ×{{ fmtNum(row.cached_tokens) }}</span></span>
                      <b :class="row.cache_read_cost_usd > 0 ? 'c-cyan' : ''">{{ fmtCost6(row.cache_read_cost_usd) }}</b>
                    </div>
                    <div class="urt-tt__sep"></div>
                    <div class="urt-tt__row"><span>合计</span><b class="c-green">{{ fmtCost(row.est_cost_usd) }}</b></div>
                  </div>
                </template>
                <span class="urt-info"><el-icon><InfoFilled /></el-icon></span>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>

        <!-- 来源 -->
        <el-table-column v-if="showSource" label="来源" min-width="190">
          <template #default="{ row }">
            <div class="urt-source">
              <template v-if="showSource === 'full'">
                <el-tooltip v-if="row.user_agent" :content="row.user_agent" placement="top" :show-after="300" popper-class="urt-ua-popper">
                  <span class="urt-ua">{{ row.user_agent }}</span>
                </el-tooltip>
                <span v-else class="urt-dash">—</span>
              </template>
              <span v-if="row.ip_address" class="urt-ip">{{ row.ip_address }}</span>
            </div>
          </template>
        </el-table-column>

        <!-- 时间 -->
        <el-table-column
          prop="ts"
          label="时间"
          min-width="155"
          :sortable="sortableColumns?.includes('ts') || false"
        >
          <template #default="{ row }">
            <span class="urt-time">{{ fmtDateTime(row.ts) }}</span>
          </template>
        </el-table-column>
      </el-table>

      <div v-if="!loading && !rows.length" class="urt-empty">
        <el-icon size="48" color="#c0c4cc"><List /></el-icon>
        <p>暂无调用记录</p>
      </div>

      <!-- 分页 -->
      <div v-if="total > 0" class="urt-pagination">
        <el-pagination
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="pageSizes ?? [20, 50, 100, 200]"
          :total="total"
          :layout="paginationLayout ?? 'total, sizes, prev, pager, next'"
          small
          @size-change="(s: number) => emit('update:pageSize', s)"
          @current-change="(p: number) => emit('update:page', p)"
        />
      </div>
    </div>

    <!-- 内嵌 Trace Drawer -->
    <UsageTraceDrawer
      v-model="traceVisible"
      :row="traceRow"
      :trace-data="traceData"
      :trace-loading="traceLoading"
      :currency="currency"
      :meta-mode="traceMetaMode"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  Bottom, Top, Finished, MagicStick, InfoFilled, List,
} from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'
import { fmtDateTime, fmtDuration } from '@/utils/time'
import UsageTraceDrawer from './UsageTraceDrawer.vue'
import type { LogRow, TraceData } from '@/types/usage-log'

const props = withDefaults(defineProps<{
  rows: LogRow[]
  loading: boolean
  total: number
  page: number
  pageSize: number

  // 列可见性
  showKey?: boolean
  showUser?: boolean
  showDept?: boolean
  showStudio?: boolean
  showGroup?: boolean
  showProvider?: boolean
  showSource?: 'full' | 'ip-only' | false

  // 行为
  sortableColumns?: ('duration_ms' | 'est_cost_usd' | 'ts')[]
  currency?: Currency
  pageSizes?: number[]
  paginationLayout?: string

  // Trace
  traceEndpoint: (requestId: string) => Promise<TraceData>

  // 可选 label 覆盖
  userLabel?: string
  studioLabel?: string

  // Key 格式化
  keyResolver?: (row: LogRow) => string

  // Trace Drawer 模式
  traceMetaMode?: 'full' | 'personal'
}>(), {
  showKey: false,
  showUser: false,
  showDept: false,
  showStudio: false,
  showGroup: false,
  showProvider: false,
  showSource: false,
  currency: 'USD',
  traceMetaMode: 'full',
})

const emit = defineEmits<{
  'update:page': [number]
  'update:pageSize': [number]
}>()

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
  borderBottom: '1px solid var(--el-border-color-light)',
}

// ── Trace 内部状态 ──
const traceVisible  = ref(false)
const traceLoading  = ref(false)
const traceRow      = ref<LogRow | null>(null)
const traceData     = ref<TraceData | null>(null)

async function openTrace(row: LogRow) {
  traceRow.value  = row
  traceData.value = null
  traceVisible.value = true
  traceLoading.value = true
  try {
    traceData.value = await props.traceEndpoint(row.request_id)
  } catch {
    traceData.value = null
  } finally {
    traceLoading.value = false
  }
}

// ── Key 解析 ──
const maskKey = (k?: string) => !k ? '—' : k.length > 14 ? k.slice(0, 6) + '···' + k.slice(-4) : k

function resolveKey(row: LogRow): string {
  if (props.keyResolver) return props.keyResolver(row)
  return row.key_name || maskKey(row.key_id)
}

// ── 路由模型显示条件 ──
// 只要 routed_model 有值就显示，不因名称相同或 via_group 标志而隐藏
function showRoutedModel(row: LogRow): boolean {
  return !!row.routed_model
}

// ── 成本颜色（基于当页最大成本） ──
const logMaxCost = computed(() => Math.max(...props.rows.map(r => r.est_cost_usd || 0), 0.0001))
const logCostColor = (v: number) => v > logMaxCost.value * 0.7 ? 'c-red fw6' : v > logMaxCost.value * 0.3 ? 'c-orange fw6' : 'c-green'

// ── Formatters ──
const fmtNum = (n?: number | null) => n?.toLocaleString() ?? '0'

const fmtTokens = (n?: number | null) => {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}

const fmtCost = (v?: number | null): string => {
  const n = convertCost(Number(v || 0), cc.value)
  const sym = currencySymbol(cc.value.currency)
  if (n === 0) return `${sym}0.00`
  let s: string
  if (Math.abs(n) >= 1)            s = n.toFixed(2)
  else if (Math.abs(n) >= 0.01)    s = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001)  s = n.toFixed(6)
  else                              s = n.toFixed(8)
  return sym + s.replace(/(\.\d{2,}?)0+$/, '$1')
}

const fmtCost6 = (v?: number | null): string => {
  const n = convertCost(Number(v || 0), cc.value)
  return `${currencySymbol(cc.value.currency)}${n.toFixed(6)}`
}

const fmtPrice = (n?: number | null) => {
  const sym = currencySymbol(cc.value.currency)
  if (n == null) return '—'
  if (n === 0) return `${sym}0`
  const v = convertCost(n, cc.value)
  return sym + (Math.abs(v) >= 1 ? v.toFixed(2) : v.toFixed(4).replace(/0+$/, '').replace(/\.$/, ''))
}


const reasoningType = (effort?: string) => {
  if (!effort) return ''
  if (effort === 'high') return 'danger'
  if (effort === 'medium') return 'warning'
  return 'info'
}
</script>

<style scoped>
/* ── CSS 变量（与主题统一） ── */
.urt-wrap {
  --urt-input:  #36B34E;
  --urt-output: #0EA371;
  --urt-cache:  #5DADE2;
  --urt-cost:   #C7900A;
  --urt-reason: #8B5CF6;
  --urt-danger: #E74C3C;
  --urt-warn:   #E0A100;
  --urt-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  --urt-surface: var(--el-fill-color-blank);
  --urt-border:  var(--el-border-color-light);
  --urt-muted:   var(--el-text-color-secondary);
  --urt-faint:   var(--el-text-color-placeholder);
  --urt-ink:     var(--el-text-color-regular);
}

/* ── 表格包裹 ── */
.urt-table-wrap { overflow-x: auto; }

.urt-table-wrap :deep(.el-table) {
  --el-table-border-color: var(--urt-border);
  --el-table-bg-color: var(--urt-surface);
  --el-table-tr-bg-color: var(--urt-surface);
  --el-table-row-hover-bg-color: var(--el-color-primary-light-9);
  background: var(--urt-surface);
}
.urt-table-wrap :deep(.el-table td.el-table__cell) {
  background: var(--urt-surface);
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.urt-table-wrap :deep(.el-table__row:hover > td.el-table__cell) { background: var(--el-color-primary-light-9) !important; }
.urt-table-wrap :deep(.el-table .el-table-fixed-column--left.is-last-column.el-table__cell),
.urt-table-wrap :deep(.el-table__fixed) {
  box-shadow: 6px 0 12px -6px rgba(0, 0, 0, .12);
}
.urt-table-wrap :deep(.urt-col--wrap) {
  white-space: normal; word-break: break-all;
  overflow: visible; text-overflow: clip;
}

/* ── 名称 / 占位 ── */
.urt-name     { font-size: 12px; color: var(--el-text-color-regular); font-weight: 500; }
.urt-dash     { color: var(--urt-faint); }
.urt-mono     { font-family: var(--urt-mono); font-size: 12px; color: var(--urt-muted); }
.urt-mono--sm { font-size: 10px; }
.urt-provider { font-size: 12px; color: var(--urt-muted); }

/* ── 模型格 ── */
.urt-model-name         { font-size: 13px; font-weight: 500; color: var(--el-text-color-regular); }
.urt-model-name--muted  { color: var(--urt-muted); font-weight: 400; }
.urt-model-cell         { display: flex; flex-direction: column; gap: 2px; }
.urt-model-routed       { display: flex; align-items: center; gap: 3px; }
.urt-arrow              { color: var(--urt-faint); font-size: 10px; }

/* ── Token 用量 ── */
.urt-token-row { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.urt-token-item {
  display: inline-flex; align-items: center; gap: 3px; font-size: 12px; font-weight: 600;
  padding: 1px 7px; border-radius: 999px;
}
.urt-token-item--in     { color: var(--urt-input);  background: color-mix(in srgb, var(--urt-input) 12%, transparent); }
.urt-token-item--out    { color: var(--urt-output); background: color-mix(in srgb, var(--urt-output) 12%, transparent); }
.urt-token-item--cache  { color: var(--urt-cache);  background: color-mix(in srgb, var(--urt-cache) 14%, transparent); }
.urt-token-item--reason { color: var(--urt-reason); background: color-mix(in srgb, var(--urt-reason) 12%, transparent); }
.urt-token-icon         { font-size: 10px; }

/* ── 标记 ── */
.urt-flags       { display: flex; flex-wrap: wrap; gap: 3px; justify-content: center; }
.urt-tag--effort { font-size: 10px; }

/* ── 时间 / 耗时 ── */
.urt-time     { font-size: 12px; color: var(--urt-muted); white-space: nowrap; font-family: var(--urt-mono); }
.urt-duration { font-size: 12px; color: var(--urt-ink); font-variant-numeric: tabular-nums; }

/* ── 成本格 ── */
.urt-cost-cell { display: flex; align-items: center; gap: 6px; justify-content: flex-end; white-space: nowrap; }
.urt-cost      { font-family: var(--urt-mono); font-size: 12px; font-weight: 600; font-variant-numeric: tabular-nums; }

/* ── 信息触发按钮 ── */
.urt-info {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; border-radius: 999px; flex-shrink: 0;
  background: var(--el-fill-color); color: var(--urt-faint);
  cursor: help; transition: background .15s ease, color .15s ease, transform .15s ease;
}
.urt-info:hover { background: var(--el-color-primary-light-9); color: var(--el-color-primary); transform: scale(1.08); }
.urt-info .el-icon { font-size: 12px; }

/* ── 来源格 ── */
.urt-source { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.urt-ua     { display: block; font-size: 12px; color: var(--urt-muted); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: help; }
.urt-ip     { font-family: var(--urt-mono); font-size: 10px; color: var(--urt-faint); }

/* ── 空态 ── */
.urt-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 48px 24px; }
.urt-empty p { font-size: 13px; color: var(--urt-faint); margin: 0; }

/* ── 分页 ── */
.urt-pagination { padding: 12px 16px; border-top: 1px solid var(--urt-border); display: flex; justify-content: flex-end; }

/* ── 颜色工具 ── */
.c-blue   { color: var(--urt-input); }
.c-green  { color: var(--urt-output); }
.c-cyan   { color: var(--urt-cache); }
.c-orange { color: var(--urt-warn); }
.c-amber  { color: var(--urt-warn); }
.c-purple { color: var(--urt-reason); }
.c-red    { color: var(--urt-danger); }
.fw6      { font-weight: 600; }
</style>

<!-- Global styles: tooltip poppers teleport outside component shadow -->
<style>
.urt-tt-popper.el-popper {
  background: var(--text-primary) !important;
  border: 1px solid #374151 !important;
  border-radius: 10px !important;
  box-shadow: 0 12px 32px rgba(0, 0, 0, .35) !important;
  padding: 10px 12px !important;
}
.urt-tt-popper.el-popper .el-popper__arrow::before {
  background: var(--text-primary) !important;
  border: 1px solid #374151 !important;
}

.urt-tt {
  min-width: 172px;
  font-size: 12px;
  line-height: 1.7;
  font-variant-numeric: tabular-nums;
}
.urt-tt__title {
  font-weight: 700; font-size: 12px; margin-bottom: 7px;
  color: var(--text-placeholder); border-bottom: 1px solid #374151; padding-bottom: 5px; letter-spacing: .2px;
}
.urt-tt__row {
  display: flex; justify-content: space-between; gap: 18px; align-items: center; color: var(--text-placeholder);
}
.urt-tt__sub   { color: var(--text-secondary); padding-left: 2px; }
.urt-tt__group {
  margin: 6px 0 3px; font-size: 10px; font-weight: 600; letter-spacing: 0.5px;
  text-transform: uppercase; color: #38bdf8; padding-left: 6px;
  border-left: 2px solid rgba(56, 189, 248, 0.4);
}
.urt-tt__row b          { color: var(--bg-surface); font-weight: 600; }
.urt-tt__row b.c-cyan   { color: #38bdf8; }
.urt-tt__row b.c-amber  { color: #fbbf24; }
.urt-tt__row b.c-purple { color: #a78bfa; }
.urt-tt__row b.c-blue   { color: #5eead4; }
.urt-tt__row b.c-green  { color: #4ade80; }
.urt-tt__sep { border-top: 1px solid #374151; margin: 6px 0; }

/* 来源 UA 悬浮：完整内容自动换行 */
.urt-ua-popper.el-popper {
  max-width: 440px; line-height: 1.5; word-break: break-all;
}
</style>
