<template>
  <el-drawer
    v-model="visible"
    title=""
    size="780px"
    direction="rtl"
    :before-close="() => emit('update:modelValue', false)"
    class="up-trace-drawer"
  >
    <template #header>
      <div class="up-trace-header">
        <span class="up-trace-title">请求详情</span>
        <span class="up-trace-id">{{ row?.request_id }}</span>
      </div>
    </template>

    <div v-if="traceLoading" class="up-trace-loading">
      <el-icon class="is-loading" size="32"><Loading /></el-icon>
    </div>
    <div v-else-if="traceData || row" class="up-trace-body">

      <!-- ── 概要条 ── -->
      <div class="up-trace-meta">
        <el-tag type="info" size="small" effect="plain">{{ row?.model }}</el-tag>
        <el-tag :type="(row?.status || 0) === 200 ? 'success' : 'danger'" size="small" effect="dark">{{ row?.status }}</el-tag>
        <el-tag v-if="row?.stream && !isNonChatEndpoint" type="primary" size="small" effect="plain">流式</el-tag>
        <el-tag v-if="traceEndpointLabel" size="small" :type="traceEndpointTagType" effect="plain">{{ traceEndpointLabel }}</el-tag>
        <el-tooltip v-if="row?.failover_used" content="首选模型失败，已自动切换到备用模型" placement="top">
          <el-tag type="warning" size="small" effect="light" class="up-tag-failover">
            <el-icon style="vertical-align:-2px;margin-right:2px"><SwitchButton /></el-icon>故障转移
          </el-tag>
        </el-tooltip>
        <span class="up-trace-time">{{ fmtDateTime(row?.ts) }}</span>
        <span class="up-trace-dur">{{ fmtDuration(row?.duration_ms) }}</span>
      </div>

      <!-- ── Workflow 时间线 ── -->
      <div class="up-timeline">
        <div
          v-for="stage in timelineStages"
          :key="stage.key"
          :class="['up-tl-node', stage.status,
                   { 'up-tl-node--total': stage.key === 'done',
                     'up-tl-node--failover': stage.key === 'failover' }]"
        >
          <div class="up-tl-dot">
            <el-icon v-if="stage.key === 'failover'"><SwitchButton /></el-icon>
            <el-icon v-else-if="stage.status === 'ok'"><SuccessFilled /></el-icon>
            <el-icon v-else-if="stage.status === 'error'"><CircleCloseFilled /></el-icon>
            <el-icon v-else><Loading /></el-icon>
          </div>
          <div class="up-tl-label">
            <el-tooltip v-if="stage.desc" :content="stage.desc" placement="top">
              <span class="up-tl-label-text">{{ stage.label }}</span>
            </el-tooltip>
            <span v-else class="up-tl-label-text">{{ stage.label }}</span>
            <span v-if="stage.key === 'done'" class="up-tl-measured">实测</span>
          </div>
          <div class="up-tl-ms" v-if="stage.ms != null">
            {{ stage.ms }}ms
            <el-tooltip v-if="stage.note" :content="stage.note" placement="top">
              <el-icon class="up-tl-warn"><WarningFilled /></el-icon>
            </el-tooltip>
          </div>
          <div class="up-tl-ms" v-else-if="stage.key === 'failover'">
            <span class="up-tl-failover-hint">已切换</span>
          </div>
        </div>
      </div>

      <!-- ── 内容区 Tabs ── -->
      <el-tabs v-model="traceTab" class="up-trace-tabs">
        <!-- 调用信息 -->
        <el-tab-pane label="调用信息" name="meta">
          <div class="up-meta-grid">
            <!-- 调用者（仅 full 模式显示） -->
            <div v-if="metaMode !== 'personal'" class="up-meta-section">
              <div class="up-meta-heading">调用者</div>
              <div class="up-meta-row"><span>Key</span><span>{{ row?.key_name || maskKey(row?.key_id || '') }}</span></div>
              <div v-if="row?.key_type" class="up-meta-row"><span>Key 类型</span><span>{{ keyTypeLabel(row.key_type) }}</span></div>
              <div class="up-meta-row"><span>用户</span><span>{{ row?.key_type === 'service' ? '—' : (row?.user_name || '—') }}</span></div>
              <div class="up-meta-row"><span>部门</span><span>{{ row?.dept_name || row?.dept_id || '—' }}</span></div>
              <div class="up-meta-row"><span>IP</span><code>{{ row?.ip_address || '—' }}</code></div>
              <div class="up-meta-row up-meta-row--wrap"><span>UA</span><code class="up-meta-ua">{{ row?.user_agent || '—' }}</code></div>
            </div>
            <!-- 路由 -->
            <div class="up-meta-section">
              <div class="up-meta-heading">路由</div>
              <div class="up-meta-row"><span>请求模型</span><code>{{ row?.model }}</code></div>
              <div class="up-meta-row"><span>实际模型</span><code>{{ row?.routed_model || '—' }}</code></div>
              <div class="up-meta-row"><span>渠道</span><el-tag size="small" type="info" effect="plain">{{ row?.provider || '—' }}</el-tag></div>
              <div class="up-meta-row"><span>分组</span><code>{{ row?.group_name || '—' }}</code></div>
              <div class="up-meta-row"><span>故障转移</span>
                <el-tag size="small" :type="row?.failover_used ? 'warning' : 'success'" effect="plain">
                  {{ row?.failover_used ? '是' : '否' }}
                </el-tag>
              </div>
              <div class="up-meta-row"><span>流式</span>
                <el-tag size="small" :type="row?.stream ? 'primary' : ''" effect="plain">{{ row?.stream ? '是' : '否' }}</el-tag>
              </div>
            </div>
            <!-- Token / 用量 -->
            <div class="up-meta-section">
              <div class="up-meta-heading">{{ usageSectionTitle }}</div>
              <template v-if="traceEndpointType === 'rerank'">
                <div class="up-meta-row"><span>文档数</span><b>{{ rerankDocCount ?? '—' }}</b></div>
              </template>
              <div class="up-meta-row"><span>{{ traceEndpointType === 'embedding' ? '输入 Token' : '输入' }}</span><b>{{ fmtNum(row?.prompt_tokens) }}</b></div>
              <template v-if="traceEndpointType === 'chat'">
                <div class="up-meta-row"><span>缓存命中</span><b class="c-cyan">{{ fmtNum(row?.cached_tokens) }}</b></div>
                <div class="up-meta-row"><span>缓存写入</span><b class="c-amber">{{ fmtNum(row?.cache_write_tokens) }}</b></div>
                <div class="up-meta-row"><span>输出</span><b>{{ fmtNum(row?.completion_tokens) }}</b></div>
                <div class="up-meta-row" v-if="(row?.reasoning_tokens || 0) > 0">
                  <span>推理</span><b class="c-purple">{{ fmtNum(row?.reasoning_tokens) }}</b>
                </div>
              </template>
              <div class="up-meta-row up-meta-sep"><span>合计</span><b class="c-blue">{{ fmtNum(row?.total_tokens) }}</b></div>
              <div class="up-meta-row" v-if="row?.usage_missing">
                <span></span><el-tag size="small" type="warning">用量缺失</el-tag>
              </div>
            </div>
            <!-- 费用（Chat 优先展示） -->
            <div v-if="traceEndpointType === 'chat'" class="up-meta-section">
              <div class="up-meta-heading">费用 ({{ currency ?? 'USD' }})</div>
              <div class="up-meta-row" v-if="(row?.input_price_snapshot || 0) > 0 || (row?.output_price_snapshot || 0) > 0">
                <span>输入单价/1M</span><b>{{ fmtPrice(row?.input_price_snapshot) }}</b>
              </div>
              <div class="up-meta-row" v-if="(row?.input_price_snapshot || 0) > 0 || (row?.output_price_snapshot || 0) > 0">
                <span>输出单价/1M</span><b>{{ fmtPrice(row?.output_price_snapshot) }}</b>
              </div>
              <div class="up-meta-row"><span>输入费用</span><b>{{ fmtCost(row?.input_cost_usd) }}</b></div>
              <div class="up-meta-row"><span>输出费用</span><b>{{ fmtCost(row?.output_cost_usd) }}</b></div>
              <div class="up-meta-row" v-if="(row?.cache_read_cost_usd || 0) > 0">
                <span>缓存读取</span><b class="c-cyan">{{ fmtCost(row?.cache_read_cost_usd) }}</b>
              </div>
              <div class="up-meta-row up-meta-sep"><span>合计</span><b class="c-green">{{ fmtCost(row?.est_cost_usd) }}</b></div>
            </div>
          </div>
        </el-tab-pane>

        <!-- 提示词 / 请求 -->
        <el-tab-pane :label="requestTabLabel" name="prompt">
          <div v-if="traceMessages.length" class="up-msgs">
            <div v-for="(msg, i) in traceMessages" :key="i" :class="['up-msg', `up-msg--${msg.role}`]">
              <span class="up-msg-role">{{ msg.role }}</span>
              <pre class="up-msg-content">{{ msg.content || '（空）' }}</pre>
            </div>
          </div>
          <div v-else-if="traceData?.request_body" class="up-json-view">
            <pre>{{ JSON.stringify(traceData.request_body, null, 2) }}</pre>
          </div>
          <div v-else class="up-trace-empty">无提示词数据</div>
        </el-tab-pane>

        <!-- 响应 -->
        <el-tab-pane name="response">
          <template #label>
            响应
            <el-tag v-if="traceIsStream" size="small" type="info" style="margin-left:4px;vertical-align:middle">Stream</el-tag>
          </template>
          <div v-if="traceResponseText" class="up-msgs">
            <div class="up-msg up-msg--assistant">
              <span class="up-msg-role">assistant</span>
              <pre class="up-msg-content">{{ traceResponseText }}</pre>
            </div>
          </div>
          <div v-else-if="traceResponseFallback" class="up-msgs">
            <div class="up-msg up-msg--assistant">
              <span class="up-msg-role">assistant</span>
              <pre class="up-msg-content up-msg-content--muted">{{ traceResponseFallback }}</pre>
            </div>
          </div>
          <div v-else-if="traceData?.response_body" class="up-json-view">
            <pre>{{ JSON.stringify(traceData.response_body, null, 2) }}</pre>
          </div>
          <div v-else class="up-trace-empty">无响应数据</div>
        </el-tab-pane>

        <!-- 原始请求 -->
        <el-tab-pane label="原始请求" name="raw_req">
          <div v-if="traceData?.request_body" class="up-json-view">
            <pre>{{ JSON.stringify(traceData.request_body, null, 2) }}</pre>
          </div>
          <div v-else class="up-trace-empty">无原始请求数据</div>
        </el-tab-pane>

        <!-- 请求头（仅 full 模式显示） -->
        <el-tab-pane v-if="metaMode !== 'personal'" label="请求头" name="headers">
          <div v-if="traceData?.request_headers && Object.keys(traceData.request_headers).length > 0" class="up-headers-table">
            <div v-for="(val, key) in traceData.request_headers" :key="key" class="up-header-row">
              <span class="up-header-key">{{ key }}</span>
              <span class="up-header-val">{{ val }}</span>
            </div>
          </div>
          <div v-else class="up-trace-empty">无请求头数据</div>
        </el-tab-pane>

        <!-- 错误 -->
        <el-tab-pane v-if="traceData?.error_message || (row?.status || 0) >= 400" label="错误" name="error">
          <div class="up-error-block">
            <div v-if="traceData?.error_code" class="up-error-code">{{ traceData.error_code }}</div>
            <pre class="up-error-msg">{{ traceData?.error_message || `HTTP ${row?.status}` }}</pre>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
    <div v-else class="up-trace-empty">暂无 trace 数据</div>
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import {
  Loading, SuccessFilled, CircleCloseFilled, WarningFilled, SwitchButton,
} from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'
import { fmtDateTime, fmtDuration } from '@/utils/time'
import {
  extractTraceMessages,
  extractTraceResponseText,
  isTraceStreamResponse,
} from '@/utils/trace-body'
import {
  endpointPathToType,
  parseRerankDocumentsCount,
  resolveRerankDocumentsCount,
  type UsageEndpointType,
} from '@/utils/usage-endpoint'
import type { LogRow, TraceData } from '@/types/usage-log'

export type { LogRow, TraceData } from '@/types/usage-log'

const props = withDefaults(defineProps<{
  modelValue: boolean
  row: LogRow | null
  traceData: TraceData | null
  traceLoading: boolean
  currency?: Currency
  metaMode?: 'full' | 'personal'
  /** Tab 上下文端点类型（row.inbound_endpoint 缺失时回退） */
  endpointType?: UsageEndpointType
}>(), {
  currency: 'USD',
  metaMode: 'full',
  endpointType: '',
})

const emit = defineEmits<{
  'update:modelValue': [boolean]
}>()

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const traceTab = ref('prompt')

const traceEndpointType = computed((): UsageEndpointType => {
  const fromRow = endpointPathToType(props.row?.inbound_endpoint)
  if (fromRow) return fromRow
  if (props.endpointType === 'embedding' || props.endpointType === 'rerank' || props.endpointType === 'rerank_vl') return props.endpointType
  return 'chat'
})

const isNonChatEndpoint = computed(() =>
  traceEndpointType.value === 'embedding' || traceEndpointType.value === 'rerank' || traceEndpointType.value === 'rerank_vl',
)

const traceEndpointLabel = computed(() => {
  const map: Record<string, string> = {
    embedding: 'Embedding',
    rerank: 'Rerank',
    rerank_vl: 'Rerank VL',
  }
  return map[traceEndpointType.value] || ''
})

const traceEndpointTagType = computed(() => {
  if (traceEndpointType.value === 'embedding') return 'success'
  if (traceEndpointType.value === 'rerank' || traceEndpointType.value === 'rerank_vl') return 'warning'
  return 'info'
})

const usageSectionTitle = computed(() => {
  if (traceEndpointType.value === 'embedding') return 'Embedding 用量'
  if (traceEndpointType.value === 'rerank') return 'Rerank 请求'
  if (traceEndpointType.value === 'rerank_vl') return 'Rerank VL 请求'
  return 'Token 用量'
})

const requestTabLabel = computed(() => {
  if (traceEndpointType.value === 'embedding') return '输入'
  if (traceEndpointType.value === 'rerank' || traceEndpointType.value === 'rerank_vl') return '请求'
  return '提示词'
})

const rerankDocCount = computed(() => {
  const fromRow = resolveRerankDocumentsCount(props.row ?? {})
  if (fromRow != null) return fromRow
  return parseRerankDocumentsCount(props.traceData?.request_body)
})

watch(() => props.modelValue, (val) => {
  if (val) traceTab.value = isNonChatEndpoint.value ? 'meta' : 'prompt'
})

watch(() => props.traceLoading, (loading) => {
  if (!loading && !props.traceData) traceTab.value = 'meta'
})

// ── Formatters ──
const fmtNum = (n?: number | null) => n?.toLocaleString() ?? '0'

const keyTypeLabel = (t?: string) => {
  if (t === 'service') return '程序 Key'
  if (t === 'personal') return '个人 Key'
  return t || '—'
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

const fmtPrice = (n?: number | null) => {
  const sym = currencySymbol(cc.value.currency)
  if (n == null) return '—'
  if (n === 0) return `${sym}0`
  const v = convertCost(n, cc.value)
  return sym + (Math.abs(v) >= 1 ? v.toFixed(2) : v.toFixed(4).replace(/0+$/, '').replace(/\.$/, ''))
}


const maskKey = (k?: string) => !k ? '—' : k.length > 14 ? k.slice(0, 6) + '···' + k.slice(-4) : k

// ── Trace computeds ──
const traceMessages = computed(() => extractTraceMessages(props.traceData?.request_body))

const traceResponseText = computed(() => extractTraceResponseText(props.traceData?.response_body))

/** 有 response 但正文块为空（如仅 reasoning token）时的说明 */
const traceResponseFallback = computed(() => {
  const body = props.traceData?.response_body
  if (!body || typeof body !== 'object') return ''
  const b = body as Record<string, unknown>
  if (extractTraceResponseText(body)) return ''
  const usage = b.usage as Record<string, unknown> | undefined
  if (usage && (usage.reasoning_tokens || usage.output_tokens_details)) {
    const details = usage.output_tokens_details as Record<string, unknown> | undefined
    const reasoning = details?.reasoning_tokens ?? usage.reasoning_tokens
    if (reasoning) return `（模型返回无可见文本，含 ${reasoning} reasoning tokens）`
  }
  return ''
})

const traceIsStream = computed(() =>
  isTraceStreamResponse(props.traceData?.response_body) || !!props.row?.stream,
)

const timelineStages = computed(() => {
  const td = props.traceData
  const row = props.row
  if (!row) return []

  const authMs  = td?.stage_auth_ms  ?? row.stage_auth_ms  ?? null
  const routeMs = td?.stage_route_ms ?? row.stage_route_ms ?? null
  const ttfbMs  = td?.stage_ttfb_ms  ?? row.stage_ttfb_ms  ?? null
  const ttftMs  = td?.stage_ttft_ms  ?? row.first_token_ms ?? null
  const total   = row.duration_ms    ?? td?.stage_total_ms ?? null

  const isStream   = !!row.stream && !isNonChatEndpoint.value
  const isFailover = !!row.failover_used
  const isError    = (row.status ?? 200) >= 400
  const errorInAuth  = isError && authMs  == null && routeMs == null
  const errorInRoute = isError && routeMs == null && authMs  != null

  const authDur  = authMs  != null ? Math.max(0, authMs) : null
  const routeDur = (routeMs != null && authMs != null)
    ? Math.max(0, routeMs - authMs)
    : (routeMs != null ? Math.max(0, routeMs) : null)

  const ttfbDur      = ttfbMs != null ? Math.max(0, ttfbMs) : null
  const ttftDeltaDur = (ttftMs != null && ttfbMs != null)
    ? (ttftMs - ttfbMs > 0 ? ttftMs - ttfbMs : null)
    : (ttftMs != null && ttftMs > 0 ? ttftMs : null)

  const streamReturnDur = (isStream && ttftMs != null && routeMs != null && total != null)
    ? Math.max(0, total - routeMs - ttftMs)
    : null

  const upstreamDur = (!isStream && routeMs != null && total != null)
    ? Math.max(0, total - routeMs)
    : null

  type Status = 'ok' | 'error' | 'skip'
  const stages: { key: string; label: string; ms: number | null; status: Status; note?: string; desc?: string }[] = []

  stages.push({
    key: 'auth', label: '鉴权',
    desc: '请求到达 → API Key 验证完成',
    ms: authDur,
    status: authMs != null ? 'ok' : (errorInAuth ? 'error' : 'skip'),
  })

  stages.push({
    key: 'route', label: '路由',
    desc: '鉴权完成 → 模型组调度 + 选定上游渠道',
    ms: routeDur,
    status: errorInRoute ? 'error' : routeMs != null ? 'ok' : 'skip',
    note: (routeMs != null && authMs != null && routeMs < authMs) ? '计时异常' : undefined,
  })

  if (isFailover) {
    stages.push({
      key: 'failover', label: '故障转移',
      desc: `首选模型调用失败，自动切换到备用模型重试（实际路由至 ${row.routed_model || '备用模型'}）`,
      ms: null,
      status: 'ok',
    })
  }

  if (isStream) {
    stages.push({
      key: 'ttfb', label: 'TTFB',
      desc: '上游请求发出 → 收到 HTTP 响应头（网络 + 排队耗时）',
      ms: ttfbDur,
      status: isError ? 'error' : (ttfbDur != null ? 'ok' : 'skip'),
    })
    stages.push({
      key: 'ttft', label: 'TTFT',
      desc: ttfbMs != null
        ? '收到响应头 → 首个 content token（模型推理生成首字）'
        : '上游请求发出 → 首个 content token',
      ms: ttftDeltaDur,
      status: isError ? 'error' : (ttftDeltaDur != null ? 'ok' : 'skip'),
    })
    stages.push({
      key: 'stream', label: '流式返回',
      desc: '首个 token 返回 → 流式输出全部完成',
      ms: streamReturnDur,
      status: isError ? 'error' : (streamReturnDur != null ? 'ok' : 'skip'),
    })
  } else {
    stages.push({
      key: 'upstream', label: '上游响应',
      desc: '上游请求发出 → 收到完整响应',
      ms: upstreamDur,
      status: isError ? 'error' : (upstreamDur != null ? 'ok' : 'skip'),
    })
  }

  stages.push({
    key: 'done', label: '总耗时',
    ms: total,
    status: isError ? 'error' : 'ok',
  })

  return stages.filter(s => s.status !== 'skip' || s.key === 'done')
})
</script>

<style scoped>
/* ── mono font ── */
.up-trace-body { --up-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace; }

/* ── Drawer header ── */
.up-trace-header { display: flex; flex-direction: column; gap: 2px; }
.up-trace-title  { font-size: 16px; font-weight: 700; color: var(--el-text-color-primary); }
.up-trace-id     { font-size: 12px; color: var(--el-text-color-secondary); font-family: var(--up-mono); word-break: break-all; }

/* ── Body states ── */
.up-trace-loading { display: flex; justify-content: center; align-items: center; padding: 60px 0; color: var(--el-color-primary); }
.up-trace-body    { display: flex; flex-direction: column; gap: 20px; }
.up-trace-empty   { text-align: center; padding: 40px 0; color: var(--el-text-color-secondary); font-size: 13px; }

/* ── 概要条 ── */
.up-trace-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.up-trace-time { font-size: 12px; color: var(--el-text-color-secondary); margin-left: 4px; }
.up-trace-dur  { font-size: 12px; color: var(--el-color-primary); font-weight: 600; }
.up-tag-failover { cursor: help; }

/* ── Timeline ── */
.up-timeline {
  display: flex; align-items: flex-start; gap: 0;
  background: var(--el-fill-color-light);
  border-radius: 10px; padding: 16px 20px; overflow-x: auto;
}
.up-tl-node {
  display: flex; flex-direction: column; align-items: center; gap: 4px;
  position: relative; flex: 1; min-width: 72px;
}
.up-tl-node + .up-tl-node::before {
  content: ''; position: absolute; left: -50%; top: 13px;
  width: 100%; height: 2px; background: var(--el-border-color);
  z-index: 0;
}
.up-tl-node.ok    + .up-tl-node::before { background: var(--el-color-success-light-5); }
.up-tl-node.error + .up-tl-node::before { background: var(--el-color-danger-light-5); }
.up-tl-dot {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: var(--el-fill-color); border: 2px solid var(--el-border-color);
  font-size: 14px; position: relative; z-index: 1;
}
.up-tl-node.ok    .up-tl-dot { background: color-mix(in srgb, var(--el-color-success) 12%, #fff); border-color: var(--el-color-success); color: var(--el-color-success); }
.up-tl-node.error .up-tl-dot { background: color-mix(in srgb, var(--el-color-danger)  12%, #fff); border-color: var(--el-color-danger);  color: var(--el-color-danger); }
.up-tl-node--failover .up-tl-dot { background: color-mix(in srgb, var(--el-color-warning) 12%, #fff); border-color: var(--el-color-warning); color: var(--el-color-warning); }
.up-tl-node--failover .up-tl-label { color: var(--el-color-warning-dark-2); }
.up-tl-node--failover + .up-tl-node::before { background: var(--el-color-warning-light-7); }
.up-tl-failover-hint { font-size: 10px; color: var(--el-color-warning-dark-2); font-style: italic; }
.up-tl-label { font-size: 12px; color: var(--el-text-color-secondary); font-weight: 600; }
.up-tl-label-text { cursor: default; }
.up-tl-ms    { font-size: 12px; color: var(--el-text-color-regular); font-family: var(--up-mono); }
.up-tl-measured { font-size: 9px; color: var(--el-text-color-placeholder); margin-left: 2px; }

/* ── Tabs ── */
.up-trace-tabs { margin-top: -8px; }

/* ── Messages ── */
.up-msgs { display: flex; flex-direction: column; gap: 12px; }
.up-msg  { border-radius: 8px; overflow: hidden; border: 1px solid var(--el-border-color-light); }
.up-msg-role {
  display: block; padding: 5px 12px; font-size: 12px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .05em;
  background: var(--el-fill-color-light); color: var(--el-text-color-secondary);
}
.up-msg--system    .up-msg-role { background: var(--color-warning-bg); color: #92400e; }
.up-msg--user      .up-msg-role { background: var(--color-info-bg); color: #1e40af; }
.up-msg--assistant .up-msg-role { background: var(--color-success-bg); color: #065f46; }
.up-msg-content {
  margin: 0; padding: 12px; font-size: 12.5px; line-height: 1.65;
  white-space: pre-wrap; word-break: break-word; overflow-x: auto; -webkit-overflow-scrolling: touch;
  font-family: var(--up-mono); background: var(--el-bg-color); max-height: 400px; overflow-y: auto;
}
.up-msg-content--muted { color: var(--el-text-color-secondary); font-style: italic; }

/* ── JSON view ── */
.up-json-view {
  background: var(--el-fill-color-light); border-radius: 8px; padding: 14px;
  max-height: 500px; overflow-y: auto;
}
.up-json-view pre {
  margin: 0; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all;
  font-family: var(--up-mono); overflow-x: auto; -webkit-overflow-scrolling: touch;
}

/* ── Error block ── */
.up-error-block { background: var(--color-danger-bg); border: 1px solid #fca5a5; border-radius: 8px; padding: 16px; }
.up-error-code  { font-size: 12px; font-weight: 700; color: var(--color-danger); text-transform: uppercase; margin-bottom: 8px; }
.up-error-msg   { margin: 0; font-size: 12.5px; color: #7f1d1d; white-space: pre-wrap; word-break: break-word; }

/* ── Meta grid ── */
.up-meta-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}
.up-meta-section {
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 12px 14px;
  display: flex; flex-direction: column; gap: 6px;
}
.up-meta-heading {
  font-size: 12px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--el-text-color-secondary);
  margin-bottom: 4px; border-bottom: 1px solid var(--el-border-color-lighter); padding-bottom: 6px;
}
.up-meta-row {
  display: flex; justify-content: space-between; align-items: center; gap: 8px;
  font-size: 12.5px; min-height: 22px;
}
.up-meta-row > span:first-child { color: var(--el-text-color-secondary); flex-shrink: 0; }
.up-meta-row > span:last-child, .up-meta-row > b, .up-meta-row > code { text-align: right; word-break: break-all; }
.up-meta-row--wrap { align-items: flex-start; }
.up-meta-row--wrap > span:first-child { padding-top: 2px; }
.up-meta-sep { border-top: 1px solid var(--el-border-color-lighter); padding-top: 6px; margin-top: 2px; }
.up-meta-ua {
  font-family: var(--up-mono); font-size: 12px;
  color: var(--el-text-color-regular); word-break: break-all; text-align: right;
}

/* ── Headers tab ── */
.up-headers-table { padding: 4px 0; }
.up-header-row {
  display: flex; gap: 12px; align-items: baseline;
  padding: 5px 8px; border-radius: 4px;
}
.up-header-row:hover { background: var(--el-fill-color-light); }
.up-header-key {
  font-family: var(--up-mono); font-size: 12px; color: var(--el-color-primary);
  min-width: 200px; flex-shrink: 0; word-break: break-all;
}
.up-header-val {
  font-family: var(--up-mono); font-size: 12px;
  color: var(--el-text-color-primary); word-break: break-all;
}

/* ── Utility colors ── */
.c-blue   { color: #60a5fa; }
.c-green  { color: #4ade80; }
.c-cyan   { color: #06b6d4; }
.c-amber  { color: #f59e0b; }
.c-purple { color: #a78bfa; }

code { font-family: var(--up-mono); font-size: 12px; color: var(--el-text-color-primary); }
</style>

<!-- Global styles for teleported drawer and tooltip poppers -->
<style>
/* ── Trace Drawer 移动端适配（非 scoped，drawer 通过 Teleport 渲染到 body）── */
@media (max-width: 768px) {
  .up-trace-drawer.el-drawer { width: 100% !important; }

  .up-trace-drawer .up-msg-content {
    font-size: 12px !important; padding: 8px !important;
    overflow-x: auto; -webkit-overflow-scrolling: touch;
  }
  .up-trace-drawer .up-json-view { padding: 10px; }
  .up-trace-drawer .up-json-view pre {
    font-size: 12px !important; overflow-x: auto; -webkit-overflow-scrolling: touch;
    white-space: pre;
  }
  .up-trace-drawer .up-error-msg { font-size: 12px; padding: 10px; }

  .up-trace-drawer .up-header-row { flex-direction: column; gap: 2px; padding: 6px 8px; }
  .up-trace-drawer .up-header-key { min-width: auto; font-size: 12px; font-weight: 700; }
  .up-trace-drawer .up-header-val { font-size: 12px; }

  .up-trace-drawer .up-meta-grid { grid-template-columns: 1fr !important; }

  .up-trace-drawer .el-tabs__nav-wrap {
    overflow-x: auto; -webkit-overflow-scrolling: touch;
  }
  .up-trace-drawer .el-tabs__nav {
    flex-wrap: nowrap; display: inline-flex;
  }
}
</style>
