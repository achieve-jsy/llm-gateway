// useAiInsight.ts — AI 分析洞察 composable（从 UsageDashboardPage 迁出）
// 负责：范围推导（对齐原型 currentScope()）/ 按范围+日期缓存 / 300ms 防抖 / segmentToHtml / 下钻分发。
// 页面只需提供筛选 refs + 日期区间，下钻具体行为通过 onDrill 回调注入。

import { ref, computed, watch, type Ref } from 'vue'
import { usageApi } from '@/api/usage'
import { tierLabel } from '@/utils/analytics-tier'
import type {
  InsightCardDrillPayload,
  InsightCardSegment,
  InsightMetrics,
  InsightResponse,
  InsightScope,
  Segment,
} from '@/types/analytics'

/** 面包屑节点（对齐 UsageDashboardPage 的 breadcrumbs 结构） */
export interface InsightCrumb {
  scope: string
  id: string
  name: string
}

export interface UseAiInsightOptions {
  /** 筛选状态（供范围推导：model > dept > tier > company） */
  filterModel: Ref<string>
  filterDept: Ref<string>
  filterTier: Ref<string>
  /** 页面面包屑（下钻收窄范围时可能写入 dept） */
  breadcrumbs: Ref<InsightCrumb[]>
  /** 日期区间 [start, end] */
  dateRange: Ref<[string, string]>
  /** 下钻分发：model → 页面跳模型表；dept → 页面收窄筛选；scroll → 页面锚点滚动 */
  onDrill?: (payload: InsightCardDrillPayload) => void
}

/** 对齐原型 currentScope()：model > dept（filter 或面包屑）> tier > company */
function deriveScope(
  filterModel: string,
  filterDept: string,
  filterTier: string,
  breadcrumbs: InsightCrumb[],
): InsightScope {
  if (filterModel) return { kind: 'model', key: filterModel }
  const last = breadcrumbs[breadcrumbs.length - 1]
  const deptKey = filterDept || (last?.scope === 'dept' ? last.name || last.id : '')
  if (deptKey) return { kind: 'dept', key: deptKey }
  if (filterTier) return { kind: 'tier', key: filterTier }
  return { kind: 'company' }
}

/** Segment → 卡片内联 html：文本 + 估算标记 + 下钻锚点（对齐原型 drillHtml） */
export function segmentToHtml(seg: Segment): string {
  const parts: string[] = [seg.text]
  if (seg.est) parts.push('<span class="ai-flag est">估</span>')
  if (seg.drill) {
    const d = seg.drill
    const attrs = [
      d.dept ? `data-dept="${d.dept}"` : '',
      d.model ? `data-model="${d.model}"` : '',
      `data-scroll="${d.scroll || 'overview'}"`,
    ].filter(Boolean).join(' ')
    parts.push(`<a class="ai-drill" ${attrs}>下钻</a>`)
  }
  return parts.join(' ')
}

export function useAiInsight(opts: UseAiInsightOptions) {
  const loading = ref(false)
  const segments = ref<InsightCardSegment[]>([])
  const scopeLabel = ref('全司')
  /** 裁决 / KPI 等需要 insight 指标的下游区块 */
  const metrics = ref<InsightMetrics | null>(null)
  /** 全司人均费用中位数（部门人均告警对照） */
  const gmed = ref(0)
  const cache = new Map<string, InsightResponse>()

  /** 当前洞察范围（computed，供页面展示范围标签 / 下钻收窄） */
  const scope = computed<InsightScope>(() =>
    deriveScope(opts.filterModel.value, opts.filterDept.value, opts.filterTier.value, opts.breadcrumbs.value),
  )

  /** 卡片头部范围标签：请求未返回时先用筛选推导的乐观标签 */
  const label = computed(() => {
    const s = scope.value
    if (s.kind === 'model') return `模型：${s.key || ''}`
    if (s.kind === 'dept') return `部门：${s.key || ''}`
    if (s.kind === 'tier') return `等级：${tierLabel(s.key || '')}`
    return scopeLabel.value || '全司'
  })

  function cacheKey(): string {
    const s = scope.value
    return `${opts.dateRange.value[0]}~${opts.dateRange.value[1]}|${s.kind}|${s.key || ''}`
  }

  function applyInsight(resp: InsightResponse) {
    if (resp.scope?.label) scopeLabel.value = resp.scope.label
    metrics.value = resp.metrics ?? null
    gmed.value = resp.gmed ?? 0
    const facts = (resp.segments?.facts || []).map(segmentToHtml).join(' ')
    const causes = (resp.segments?.causes || []).map(segmentToHtml).join(' ')
    const suggestions = (resp.segments?.suggestions || []).map(segmentToHtml).join(' ')
    const segs: InsightCardSegment[] = [
      { tag: '事实', html: facts },
      { tag: '原因', html: causes },
      { tag: '建议', html: suggestions },
    ]
    segments.value = segs.filter(s => s.html.length > 0)
  }

  async function load() {
    const [start, end] = opts.dateRange.value
    if (!start || !end) return
    const key = cacheKey()
    const cached = cache.get(key)
    if (cached) {
      applyInsight(cached)
      return
    }
    loading.value = true
    try {
      const resp = await usageApi.getInsight({ start, end, scope: scope.value })
      cache.set(key, resp)
      applyInsight(resp)
    } catch (e) {
      // 后端 insight 未就绪时静默降级：组件展示空态
      console.warn('Failed to load AI insight:', e)
      segments.value = []
    } finally {
      loading.value = false
    }
  }

  let timer: ReturnType<typeof setTimeout> | null = null
  function schedule() {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => load(), 300)
  }

  /** 清空缓存（供 30s 定时刷新 / 手动刷新绕过缓存） */
  function clearCache() {
    cache.clear()
  }

  /** 强制刷新：清缓存后重新拉取（页面 loadAll 的统一入口） */
  async function refresh() {
    clearCache()
    await load()
  }

  /** 轻量下钻：默认分发到 onDrill 回调；未注入时走内置收窄行为 */
  function handleDrill(payload: InsightCardDrillPayload) {
    if (opts.onDrill) {
      opts.onDrill(payload)
      return
    }
    if (payload.dept) opts.filterDept.value = payload.dept
  }

  /** 滚动到区块（供 onDrill 内部使用 / 页面手动调用） */
  function scrollTo(sec?: string) {
    const el = document.getElementById('sec-' + (sec || 'overview'))
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  watch([scope, opts.dateRange], () => schedule(), { immediate: true, deep: true })

  return { scope, label, segments, metrics, gmed, loading, load, schedule, refresh, clearCache, handleDrill, scrollTo }
}
