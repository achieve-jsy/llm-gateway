<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue'
import http from '@/services/http'
import { useAuthStore } from '@/stores/auth'
import { getPublicSettings } from '@/services/control-plane'
import { copyToClipboard } from '@/utils/clipboard'
import { CopyDocument, Check, Key, Promotion } from '@element-plus/icons-vue'

const authStore = useAuthStore()

// ── 接入引导（精简）──
const apiBaseUrl = ref('')
const displayUrl = ref('https://your-gateway.example.com/v1')
const copied = ref(false)
let copyTimer: ReturnType<typeof setTimeout> | null = null
function buildUrl(base: string) {
  const b = base || 'https://your-gateway.example.com'
  return b.endsWith('/v1') ? b : b.replace(/\/$/, '') + '/v1'
}
function copyUrl() {
  copyToClipboard(displayUrl.value)
  copied.value = true
  if (copyTimer) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copied.value = false }, 2200)
}

// ── 数据 ──
interface Row {
  stat_date?: string; model?: string
  req_count: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number
  reasoning_tokens?: number; est_cost_usd: number
}
interface Total {
  req_count: number
  prompt_tokens: number; cached_tokens: number; completion_tokens: number
  reasoning_tokens?: number; est_cost_usd: number
}

const loading = ref(true)
const total = ref<Total | null>(null)
const dayRows = ref<Row[]>([])
const modelRows = ref<Row[]>([])
const hourRows = ref<Row[]>([])

const RANGE_DAYS = 30
const USD_TO_CNY = 7.2

function fmtLocal(d: Date) {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}
function parseLocal(s: string) {
  const [y, m, d] = s.split('T')[0].split('-').map(Number)
  return new Date(y, m - 1, d)
}

const reduceMotion = typeof window !== 'undefined'
  && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

// ── 数字滚动动画 ──
const display = reactive({ req: 0, tokens: 0, activeDays: 0, streak: 0, cost: 0 })
function animateTo(key: keyof typeof display, to: number, dur = 900, decimals = 0) {
  if (reduceMotion) { display[key] = to; return }
  const f = Math.pow(10, decimals)
  const start = performance.now()
  function step(now: number) {
    const p = Math.min((now - start) / dur, 1)
    const eased = 1 - Math.pow(1 - p, 3)
    display[key] = Math.round(to * eased * f) / f
    if (p < 1) requestAnimationFrame(step)
  }
  requestAnimationFrame(step)
}

async function load() {
  loading.value = true
  const end = new Date()
  const start = new Date()
  start.setDate(end.getDate() - (RANGE_DAYS - 1))
  const base = { from: fmtLocal(start), to: fmtLocal(end) }
  try {
    const [dayRes, modelRes, hourRes] = await Promise.all([
      http.get('/api/v1/my/usage', { params: { ...base, group_by: 'day' }, _silent: true } as any),
      http.get('/api/v1/my/usage', { params: { ...base, group_by: 'model' }, _silent: true } as any),
      http.get('/api/v1/my/usage', { params: { ...base, group_by: 'hour' }, _silent: true } as any),
    ])
    dayRows.value = dayRes.data.rows || []
    total.value = dayRes.data.total || null
    modelRows.value = modelRes.data.rows || []
    hourRows.value = hourRes.data.rows || []
  } catch { /* 静默：空态兜底 */ }
  finally { loading.value = false }
  // 启动计数动画
  animateTo('req', total.value?.req_count || 0)
  animateTo('tokens', totalTokens.value)
  animateTo('activeDays', activeDays.value)
  animateTo('streak', streak.value)
  animateTo('cost', totalCostCny.value, 1000, 2)
}

onMounted(async () => {
  try {
    const res = await getPublicSettings()
    apiBaseUrl.value = res.data.api_base_url || ''
    displayUrl.value = buildUrl(apiBaseUrl.value)
  } catch { /* noop */ }
  await load()
})

// ── 派生指标 ──
const hasData = computed(() => (total.value?.req_count || 0) > 0)
const totalTokens = computed(() => (total.value?.prompt_tokens || 0) + (total.value?.completion_tokens || 0))
const netInput = computed(() => Math.max((total.value?.prompt_tokens || 0) - (total.value?.cached_tokens || 0), 0))
const cacheRate = computed(() => {
  const p = total.value?.prompt_tokens || 0
  return p ? (total.value!.cached_tokens || 0) / p : 0
})
const outRatio = computed(() => {
  const out = total.value?.completion_tokens || 0
  return netInput.value ? out / netInput.value : 0
})
const reasoningShare = computed(() => {
  const out = total.value?.completion_tokens || 0
  return out ? (total.value?.reasoning_tokens || 0) / out : 0
})
const avgTokensPerReq = computed(() => {
  const r = total.value?.req_count || 0
  return r ? totalTokens.value / r : 0
})

// 长 ↔ 短 任务（log 映射到 0-100，越大越偏长任务）
const taskLengthPct = computed(() => {
  const avg = avgTokensPerReq.value
  if (avg <= 0) return 0
  const lo = 3, hi = 5 // 1k -> 100k
  const v = (Math.log10(avg) - lo) / (hi - lo)
  return Math.round(Math.min(Math.max(v, 0), 1) * 100)
})
const taskLengthLabel = computed(() => {
  const p = taskLengthPct.value
  if (p < 25) return '短平快'
  if (p < 50) return '偏轻量'
  if (p < 75) return '偏深度'
  return '长任务'
})

// 输入/缓存/输出 构成
const composition = computed(() => {
  const inp = netInput.value
  const cache = total.value?.cached_tokens || 0
  const out = total.value?.completion_tokens || 0
  const sum = inp + cache + out || 1
  return [
    { key: 'in', label: '净输入', value: inp, pct: Math.round(inp / sum * 100), color: 'var(--pd-in)' },
    { key: 'cache', label: '缓存命中', value: cache, pct: Math.round(cache / sum * 100), color: 'var(--pd-cache)' },
    { key: 'out', label: '输出', value: out, pct: Math.round(out / sum * 100), color: 'var(--pd-out)' },
  ]
})

// token 按模型分布
const PALETTE = ['#2BA8A2', '#3b82f6', '#8b5cf6', '#f59e0b', '#ec4899', '#06b6d4']
const modelDist = computed(() => {
  const rows = modelRows.value.map(r => ({
    model: r.model || '未知',
    tokens: (r.prompt_tokens || 0) + (r.completion_tokens || 0),
  })).filter(r => r.tokens > 0).sort((a, b) => b.tokens - a.tokens)
  const sum = rows.reduce((s, r) => s + r.tokens, 0) || 1
  return rows.slice(0, 6).map((r, i) => ({
    ...r, pct: Math.round(r.tokens / sum * 100), color: PALETTE[i % PALETTE.length],
  }))
})
const topModel = computed(() => modelDist.value[0]?.model || '—')

// 时间节律
const peakHour = computed(() => {
  const buckets = Array(24).fill(0)
  for (const r of hourRows.value) {
    const m = (r.stat_date || '').match(/[T ](\d{2})/)
    if (m) { const h = parseInt(m[1]); if (h >= 0 && h < 24) buckets[h] += r.req_count || 0 }
  }
  let bestH = 0, bestC = 0
  buckets.forEach((c, h) => { if (c > bestC) { bestC = c; bestH = h } })
  return { hour: bestH, count: bestC, buckets, max: Math.max(...buckets, 1) }
})
const weekdayVsWeekend = computed(() => {
  let wd = 0, we = 0
  for (const r of dayRows.value) {
    if (!r.stat_date) continue
    const dow = parseLocal(r.stat_date).getDay()
    if (dow === 0 || dow === 6) we += r.req_count || 0
    else wd += r.req_count || 0
  }
  const sum = wd + we || 1
  return { wd, we, wdPct: Math.round(wd / sum * 100), wePct: Math.round(we / sum * 100) }
})
const activeDays = computed(() => dayRows.value.filter(r => (r.req_count || 0) > 0).length)
const streak = computed(() => {
  const set = new Set(dayRows.value.filter(r => (r.req_count || 0) > 0).map(r => (r.stat_date || '').split('T')[0]))
  let s = 0
  const d = new Date()
  // 允许从今天或昨天起算
  if (!set.has(fmtLocal(d))) d.setDate(d.getDate() - 1)
  while (set.has(fmtLocal(d))) { s++; d.setDate(d.getDate() - 1) }
  return s
})

// ── 使用画像 / 称号 ──
interface Persona { emoji: string; title: string; desc: string }
const persona = computed<Persona>(() => {
  if (!hasData.value) return { emoji: '🌱', title: '新手上路', desc: '还没有调用记录，去创建 Key 开启你的 AI 之旅吧。' }
  const scores: { p: Persona; score: number }[] = []
  const night = peakHour.value.hour <= 5 || peakHour.value.hour >= 22
  scores.push({ score: (night ? 2 : 0) + taskLengthPct.value / 100, p: { emoji: '🦉', title: '夜行深思者', desc: `常在 ${peakHour.value.hour}:00 出没，偏爱有深度的长任务。` } })
  scores.push({ score: outRatio.value > 1.2 ? 2 + outRatio.value * 0.3 : 0, p: { emoji: '✍️', title: '内容创作者', desc: '输出远多于输入，是个高产的创作型选手。' } })
  scores.push({ score: outRatio.value > 0 && outRatio.value < 0.35 && netInput.value > 5000 ? 2.2 : 0, p: { emoji: '📚', title: '阅读总结家', desc: '喂进大量上下文、要言不烦，擅长读长文做总结。' } })
  scores.push({ score: reasoningShare.value > 0.15 ? 2 + reasoningShare.value * 2 : 0, p: { emoji: '🧠', title: '推理控', desc: '钟爱会"思考"的模型，深度推理 token 占比很高。' } })
  scores.push({ score: cacheRate.value > 0.35 ? 1.8 + cacheRate.value : 0, p: { emoji: '♻️', title: '缓存复用王', desc: `${Math.round(cacheRate.value * 100)}% 的输入命中缓存，把复用玩明白了。` } })
  scores.push({ score: (total.value!.req_count > 200 && taskLengthPct.value < 35) ? 1.6 : 0, p: { emoji: '⚡', title: '短平快效率党', desc: '高频、轻量的调用风格，追求即问即答。' } })
  scores.push({ score: 0.5 + modelDist.value.length * 0.2, p: { emoji: '🧭', title: '全能探索者', desc: `用过 ${modelDist.value.length} 个模型，什么都想试试。` } })
  scores.sort((a, b) => b.score - a.score)
  return scores[0].p
})

const traits = computed<string[]>(() => {
  if (!hasData.value) return []
  const t: string[] = []
  t.push(peakHour.value.hour <= 5 || peakHour.value.hour >= 22 ? '🌙 夜猫子' : peakHour.value.hour < 12 ? '🌅 晨型人' : '☀️ 日间型')
  t.push(taskLengthLabel.value === '长任务' || taskLengthLabel.value === '偏深度' ? '🏔 深度长任务' : '🍃 轻量短任务')
  if (cacheRate.value > 0.2) t.push(`♻️ 缓存 ${Math.round(cacheRate.value * 100)}%`)
  if (reasoningShare.value > 0.1) t.push('🧠 爱用推理')
  if (weekdayVsWeekend.value.wePct > 35) t.push('🏖 周末也卷')
  return t.slice(0, 4)
})

// ── 问候语 ──
const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 9) return '早上好'
  if (h < 12) return '上午好'
  if (h < 14) return '中午好'
  if (h < 18) return '下午好'
  if (h < 23) return '晚上好'
  return '夜深了'
})
const greetEmoji = computed(() => {
  const h = new Date().getHours()
  if (h < 6 || h >= 23) return '🌙'
  if (h < 9) return '🌅'
  if (h < 18) return '☀️'
  return '🌆'
})
const userName = computed(() => authStore.userName || '同学')
const heroSummary = computed(() => {
  if (!hasData.value) return '过去 30 天还没有调用记录，去模型广场挑一个开始吧。'
  return `过去 30 天，你调用了 ${(total.value?.req_count || 0).toLocaleString()} 次、花费约 ¥${fmtCny(totalCostCny.value)}，最常喂的是 ${topModel.value}。`
})

// ── 格式化 ──
const fmtTokens = (n?: number | null) => {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(1) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}
const fmtNum = (n?: number | null) => (n || 0).toLocaleString()
const totalCostCny = computed(() => (total.value?.est_cost_usd || 0) * USD_TO_CNY)
const fmtCny = (n: number) => n < 0.01 && n > 0 ? '<0.01' : n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
</script>

<template>
  <div class="pdash">

    <!-- ── 问候 Hero ── -->
    <header class="pd-hero">
      <div class="pd-hero__greet">
        <div class="pd-hero__emoji" aria-hidden="true">{{ greetEmoji }}</div>
        <div class="pd-hero__txt">
          <h1 class="pd-hero__h1">{{ greeting }}，{{ userName }}</h1>
          <p class="pd-hero__sub">{{ heroSummary }}</p>
        </div>
      </div>
      <div class="pd-hero__url" :title="displayUrl">
        <span class="pd-hero__url-label">Base URL</span>
        <span class="pd-hero__url-val">{{ displayUrl }}</span>
        <button class="pd-hero__url-btn" :class="{ ok: copied }" @click="copyUrl" :aria-label="copied ? '已复制 Base URL' : '复制 Base URL'">
          <el-icon><component :is="copied ? Check : CopyDocument" /></el-icon>
        </button>
      </div>
    </header>

    <!-- ── 加载骨架 ── -->
    <div v-if="loading" class="pd-skeleton">
      <div class="pd-sk-card" v-for="n in 3" :key="n" />
    </div>

    <!-- ── 空态 ── -->
    <div v-else-if="!hasData" class="pd-empty">
      <div class="pd-empty__emoji">🌱</div>
      <h2 class="pd-empty__title">还没有调用记录</h2>
      <p class="pd-empty__desc">创建一个 API Key，去模型广场挑一个模型组，开启你的 AI 之旅吧。</p>
      <div class="pd-empty__actions">
        <router-link to="/keys" class="pd-btn pd-btn--primary"><el-icon><Key /></el-icon> 创建 Key</router-link>
        <router-link to="/model-square" class="pd-btn"><el-icon><Promotion /></el-icon> 逛模型广场</router-link>
      </div>
    </div>

    <!-- ── 看板主体 ── -->
    <template v-else>
      <!-- 画像 + 里程碑 -->
      <div class="pd-top">
        <section class="pd-persona pd-card">
          <div class="pd-persona__halo" aria-hidden="true" />
          <div class="pd-persona__emoji">{{ persona.emoji }}</div>
          <div class="pd-persona__body">
            <div class="pd-persona__eyebrow">你的使用画像</div>
            <h2 class="pd-persona__title">{{ persona.title }}</h2>
            <p class="pd-persona__desc">{{ persona.desc }}</p>
            <div class="pd-traits">
              <span v-for="t in traits" :key="t" class="pd-trait">{{ t }}</span>
            </div>
          </div>
        </section>

        <section class="pd-milestones">
          <div class="pd-ms pd-ms--cost">
            <div class="pd-ms__costtop">
              <span class="pd-ms__icon pd-ms__icon--cost">💰</span>
              <span class="pd-ms__label pd-ms__label--cost">近 30 天花费</span>
            </div>
            <div class="pd-ms__costnum">¥{{ display.cost.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}</div>
            <div class="pd-ms__costsub">日均约 ¥{{ fmtCny(totalCostCny / RANGE_DAYS) }}</div>
          </div>
          <div class="pd-ms">
            <div class="pd-ms__icon" style="--c:var(--pd-in)">📊</div>
            <div class="pd-ms__num">{{ display.req.toLocaleString() }}</div>
            <div class="pd-ms__label">累计调用</div>
          </div>
          <div class="pd-ms">
            <div class="pd-ms__icon" style="--c:var(--pd-out)">🎫</div>
            <div class="pd-ms__num">{{ fmtTokens(display.tokens) }}</div>
            <div class="pd-ms__label">累计 Token</div>
          </div>
          <div class="pd-ms">
            <div class="pd-ms__icon" style="--c:var(--pd-cache)">📅</div>
            <div class="pd-ms__num">{{ display.activeDays }}<span class="pd-ms__unit">天</span></div>
            <div class="pd-ms__label">活跃天数</div>
          </div>
          <div class="pd-ms">
            <div class="pd-ms__icon" style="--c:#f59e0b">🔥</div>
            <div class="pd-ms__num">{{ display.streak }}<span class="pd-ms__unit">天</span></div>
            <div class="pd-ms__label">连续活跃</div>
          </div>
        </section>
      </div>

      <!-- 任务画像：长短 + 构成 -->
      <section class="pd-card pd-task">
        <div class="pd-card__hd">
          <h3 class="pd-card__title">任务画像</h3>
          <span class="pd-card__hint">从 token 用量看你偏好的任务类型</span>
        </div>
        <div class="pd-task__body">
          <div class="pd-task__len">
            <div class="pd-task__len-row">
              <span class="pd-task__len-end">🍃 短任务</span>
              <span class="pd-task__len-badge">{{ taskLengthLabel }}</span>
              <span class="pd-task__len-end">长任务 🏔</span>
            </div>
            <div class="pd-slider">
              <div class="pd-slider__fill" :style="{ width: taskLengthPct + '%' }" />
              <div class="pd-slider__knob" :style="{ left: taskLengthPct + '%' }" />
            </div>
            <p class="pd-task__note">单次平均约 <b>{{ fmtTokens(Math.round(avgTokensPerReq)) }}</b> tokens · 输入输出比 <b>1:{{ outRatio.toFixed(1) }}</b></p>
          </div>

          <div class="pd-comp">
            <div class="pd-comp__spacer" aria-hidden="true"></div>
            <div class="pd-comp__bar">
              <div
                v-for="seg in composition" :key="seg.key"
                class="pd-comp__seg"
                :style="{ width: seg.pct + '%', background: seg.color }"
                :title="`${seg.label} ${fmtTokens(seg.value)} (${seg.pct}%)`"
              />
            </div>
            <div class="pd-comp__legend">
              <span v-for="seg in composition" :key="seg.key" class="pd-comp__item">
                <i class="pd-dot" :style="{ background: seg.color }" />
                {{ seg.label }} <b>{{ seg.pct }}%</b>
              </span>
            </div>
          </div>
        </div>
      </section>

      <!-- token 按模型分布 -->
      <section class="pd-card pd-models">
        <div class="pd-card__hd">
          <h3 class="pd-card__title">你的 Token 都喂给了谁</h3>
          <span class="pd-card__hint">按 token 占比 · 看你最爱用哪些模型</span>
        </div>
        <div class="pd-models__list">
          <div v-for="(m, i) in modelDist" :key="m.model" class="pd-mrow">
            <span class="pd-mrow__rank" :style="{ background: m.color }">{{ i + 1 }}</span>
            <span class="pd-mrow__name" :title="m.model">{{ m.model }}</span>
            <div class="pd-mrow__track">
              <div class="pd-mrow__fill" :style="{ width: m.pct + '%', background: m.color }" />
            </div>
            <span class="pd-mrow__pct">{{ m.pct }}%</span>
            <span class="pd-mrow__tok">{{ fmtTokens(m.tokens) }}</span>
          </div>
        </div>
      </section>

      <!-- 时间节律（精简） -->
      <section class="pd-card pd-rhythm">
        <div class="pd-card__hd">
          <h3 class="pd-card__title">使用节律</h3>
          <span class="pd-card__hint">近 30 天</span>
        </div>
        <div class="pd-rhythm__body">
          <div class="pd-rhythm__stat">
            <div class="pd-rhythm__big">{{ peakHour.hour }}:00</div>
            <div class="pd-rhythm__cap">最活跃时段</div>
          </div>
          <div class="pd-spark" aria-hidden="true">
            <div
              v-for="(c, h) in peakHour.buckets" :key="h"
              class="pd-spark__bar"
              :class="{ 'is-peak': h === peakHour.hour }"
              :style="{ height: Math.max(8, c / peakHour.max * 100) + '%' }"
              :title="`${h}:00 · ${fmtNum(c)} 次`"
            />
          </div>
          <div class="pd-rhythm__ww">
            <div class="pd-rhythm__wwbar">
              <div class="pd-rhythm__wd" :style="{ flex: weekdayVsWeekend.wd || 0.01 }" />
              <div class="pd-rhythm__we" :style="{ flex: weekdayVsWeekend.we || 0.01 }" />
            </div>
            <div class="pd-rhythm__wwlegend">
              <span><i class="pd-dot" style="background:var(--pd-in)" />工作日 {{ weekdayVsWeekend.wdPct }}%</span>
              <span><i class="pd-dot" style="background:#8b5cf6" />周末 {{ weekdayVsWeekend.wePct }}%</span>
            </div>
          </div>
        </div>
      </section>

      <div class="pd-foot">
        <router-link to="/my/usage" class="pd-foot__link">查看完整用量明细 →</router-link>
      </div>
    </template>

  </div>
</template>

<style scoped>
.pdash {
  --pd-brand: #2BA8A2;
  --pd-in: #3b82f6;
  --pd-out: #10b981;
  --pd-cache: #06b6d4;
  width: min(100%, 1080px);
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* ── Hero ── */
.pd-hero {
  display: flex; align-items: center; justify-content: space-between; gap: 20px; flex-wrap: wrap;
  background: linear-gradient(135deg, #F0FAFA 0%, #FFFFFF 60%, #F7F9FF 100%);
  border: 1px solid #D8EEEC; border-radius: 16px; padding: 22px 26px;
}
.pd-hero__greet { display: flex; align-items: center; gap: 16px; min-width: 0; }
.pd-hero__emoji { font-size: 40px; line-height: 1; flex-shrink: 0; animation: pd-bob 4s ease-in-out infinite; }
.pd-hero__h1 { font-size: 24px; font-weight: 800; margin: 0; color: #0D2B2A; letter-spacing: -0.3px; text-wrap: balance; }
.pd-hero__sub { font-size: 13.5px; color: #5A7A7A; margin: 4px 0 0; line-height: 1.5; }
.pd-hero__url {
  display: flex; align-items: center; gap: 8px; flex-shrink: 0;
  background: #fff; border: 1.5px solid #C8E4E2; border-radius: 10px; padding: 5px 6px 5px 12px;
  max-width: 100%;
}
.pd-hero__url-label { font-size: 10px; font-weight: 700; color: var(--pd-brand); letter-spacing: 0.5px; text-transform: uppercase; white-space: nowrap; }
.pd-hero__url-val { font-family: 'Fira Code', monospace; font-size: 12px; color: #1A5C58; font-weight: 600; max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pd-hero__url-btn { display: inline-flex; align-items: center; justify-content: center; width: 30px; height: 30px; border: none; border-radius: 7px; background: var(--pd-brand); color: #fff; cursor: pointer; transition: background .15s; }
.pd-hero__url-btn:hover { background: #229A94; }
.pd-hero__url-btn.ok { background: #27AE60; }
.pd-hero__url-btn:focus-visible { outline: 2px solid var(--pd-brand); outline-offset: 2px; }

/* ── 卡片通用 ── */
.pd-card {
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px; padding: 18px 20px;
}
.pd-card__hd { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.pd-card__title { font-size: 15px; font-weight: 700; color: var(--el-text-color-primary); margin: 0; }
.pd-card__hint { font-size: 12px; color: var(--el-text-color-secondary); }

/* ── 顶部：画像 + 里程碑 ── */
.pd-top { display: grid; grid-template-columns: 1.4fr 1fr; gap: 16px; }
@media (max-width: 860px) { .pd-top { grid-template-columns: 1fr; } }

.pd-persona {
  position: relative; overflow: hidden;
  display: flex; align-items: center; gap: 20px;
  background: linear-gradient(135deg, rgba(43,168,162,0.10), rgba(59,130,246,0.08));
  border-color: rgba(43,168,162,0.25);
}
.pd-persona__halo { position: absolute; top: -40px; right: -30px; width: 180px; height: 180px; border-radius: 50%; background: radial-gradient(circle, rgba(43,168,162,0.18), transparent 70%); pointer-events: none; }
.pd-persona__emoji { font-size: 56px; line-height: 1; flex-shrink: 0; animation: pd-bob 5s ease-in-out infinite; z-index: 1; }
.pd-persona__body { z-index: 1; min-width: 0; }
.pd-persona__eyebrow { font-size: 11px; font-weight: 700; letter-spacing: 1px; text-transform: uppercase; color: var(--pd-brand); }
.pd-persona__title { font-size: 22px; font-weight: 800; margin: 3px 0 4px; color: var(--el-text-color-primary); }
.pd-persona__desc { font-size: 13px; color: var(--el-text-color-regular); margin: 0; line-height: 1.55; }
.pd-traits { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.pd-trait { font-size: 11.5px; font-weight: 600; color: var(--pd-brand); background: rgba(43,168,162,0.10); border: 1px solid rgba(43,168,162,0.22); border-radius: 99px; padding: 3px 10px; }

.pd-milestones { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.pd-ms--cost {
  grid-column: 1 / -1;
  background: linear-gradient(135deg, rgba(245,158,11,0.14), rgba(236,72,153,0.10));
  border-color: rgba(245,158,11,0.30);
}
.pd-ms__costtop { display: flex; align-items: center; gap: 8px; margin-bottom: 2px; }
.pd-ms__icon--cost { width: 28px; height: 28px; margin-bottom: 0; border-radius: 8px; background: rgba(245,158,11,0.18); }
.pd-ms__label--cost { font-size: 12px; font-weight: 600; color: var(--el-text-color-secondary); }
.pd-ms__costnum { font-size: 30px; font-weight: 800; color: #d97706; line-height: 1.1; font-variant-numeric: tabular-nums; letter-spacing: -.5px; }
.pd-ms__costsub { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.pd-ms {
  background: var(--el-fill-color-blank); border: 1px solid var(--el-border-color-light);
  border-radius: 12px; padding: 14px 16px; display: flex; flex-direction: column; gap: 2px;
  transition: transform .2s ease, box-shadow .2s ease;
}
.pd-ms:hover { transform: translateY(-2px); box-shadow: 0 6px 18px rgba(0,0,0,.06); }
.pd-ms__icon { width: 34px; height: 34px; border-radius: 9px; display: flex; align-items: center; justify-content: center; font-size: 18px; background: color-mix(in srgb, var(--c) 14%, transparent); margin-bottom: 6px; }
.pd-ms__num { font-size: 24px; font-weight: 800; color: var(--el-text-color-primary); line-height: 1.1; font-variant-numeric: tabular-nums; }
.pd-ms__unit { font-size: 14px; font-weight: 600; margin-left: 2px; color: var(--el-text-color-secondary); }
.pd-ms__label { font-size: 12px; color: var(--el-text-color-secondary); }

/* ── 任务画像 ── */
/* 用 subgrid 让左右两列共享同一组行轨：行1=标题/占位、行2=进度条、行3=注释/图例，
   从而保证左侧滑条与右侧构成条始终落在同一水平线上。 */
.pd-task__body { display: grid; grid-template-columns: 1fr 1fr; grid-template-rows: auto auto auto; column-gap: 24px; align-items: start; }
.pd-task__len, .pd-comp { display: grid; grid-template-rows: subgrid; grid-row: 1 / span 3; row-gap: 0; }
.pd-comp__spacer { grid-row: 1; }
.pd-slider, .pd-comp__bar { grid-row: 2; align-self: center; }
.pd-task__note, .pd-comp__legend { grid-row: 3; }
@media (max-width: 720px) {
  .pd-task__body { grid-template-columns: 1fr; grid-template-rows: none; row-gap: 20px; }
  .pd-task__len, .pd-comp { display: block; grid-row: auto; }
  .pd-comp__spacer { display: none; }
}
.pd-task__len-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.pd-task__len-end { font-size: 12px; color: var(--el-text-color-secondary); }
.pd-task__len-badge { font-size: 12px; font-weight: 700; color: var(--pd-brand); background: rgba(43,168,162,0.10); border-radius: 99px; padding: 2px 12px; }
.pd-slider { position: relative; height: 8px; border-radius: 99px; background: linear-gradient(90deg, var(--pd-out), var(--pd-cache), var(--pd-in)); opacity: .9; }
.pd-slider__fill { display: none; }
.pd-slider__knob { position: absolute; top: 50%; width: 18px; height: 18px; border-radius: 50%; background: #fff; border: 3px solid var(--pd-brand); transform: translate(-50%, -50%); box-shadow: 0 2px 6px rgba(0,0,0,.18); transition: left .6s cubic-bezier(.22,1,.36,1); }
.pd-task__note { font-size: 12px; color: var(--el-text-color-secondary); margin: 12px 0 0; }
.pd-task__note b { color: var(--el-text-color-primary); font-variant-numeric: tabular-nums; }

.pd-comp__bar { display: flex; height: 14px; border-radius: 99px; overflow: hidden; background: var(--el-fill-color); }
.pd-comp__seg { height: 100%; transition: width .6s cubic-bezier(.22,1,.36,1); min-width: 2px; }
.pd-comp__legend { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 10px; }
.pd-comp__item { font-size: 12px; color: var(--el-text-color-secondary); display: inline-flex; align-items: center; gap: 5px; }
.pd-comp__item b { color: var(--el-text-color-primary); font-variant-numeric: tabular-nums; }
.pd-dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }

/* ── 模型分布 ── */
.pd-models__list { display: flex; flex-direction: column; gap: 10px; }
.pd-mrow { display: flex; align-items: center; gap: 10px; }
.pd-mrow__rank { width: 20px; height: 20px; border-radius: 6px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; font-size: 11px; font-weight: 700; color: #fff; }
.pd-mrow__name { font-size: 13px; font-weight: 500; color: var(--el-text-color-primary); width: 150px; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pd-mrow__track { flex: 1; height: 8px; background: var(--el-fill-color); border-radius: 99px; overflow: hidden; min-width: 40px; }
.pd-mrow__fill { height: 100%; border-radius: 99px; transition: width .6s cubic-bezier(.22,1,.36,1); }
.pd-mrow__pct { font-size: 12px; font-weight: 700; color: var(--el-text-color-primary); width: 38px; text-align: right; font-variant-numeric: tabular-nums; flex-shrink: 0; }
.pd-mrow__tok { font-size: 11px; color: var(--el-text-color-secondary); width: 52px; text-align: right; font-variant-numeric: tabular-nums; flex-shrink: 0; }
@media (max-width: 560px) { .pd-mrow__name { width: 90px; } .pd-mrow__tok { display: none; } }

/* ── 节律 ── */
.pd-rhythm__body { display: flex; align-items: center; gap: 24px; flex-wrap: wrap; }
.pd-rhythm__stat { flex-shrink: 0; }
.pd-rhythm__big { font-size: 30px; font-weight: 800; color: var(--pd-brand); line-height: 1; font-variant-numeric: tabular-nums; }
.pd-rhythm__cap { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }
.pd-spark { flex: 1; min-width: 200px; display: flex; align-items: flex-end; gap: 2px; height: 56px; }
.pd-spark__bar { flex: 1; background: color-mix(in srgb, var(--pd-in) 35%, transparent); border-radius: 3px 3px 0 0; transition: height .5s ease; min-height: 4px; }
.pd-spark__bar.is-peak { background: var(--pd-brand); }
.pd-rhythm__ww { flex-shrink: 0; width: 200px; }
.pd-rhythm__wwbar { display: flex; height: 10px; border-radius: 99px; overflow: hidden; gap: 2px; }
.pd-rhythm__wd { background: var(--pd-in); border-radius: 99px; transition: flex .5s ease; }
.pd-rhythm__we { background: #8b5cf6; border-radius: 99px; transition: flex .5s ease; }
.pd-rhythm__wwlegend { display: flex; justify-content: space-between; gap: 8px; margin-top: 8px; font-size: 11.5px; color: var(--el-text-color-secondary); }
.pd-rhythm__wwlegend span { display: inline-flex; align-items: center; gap: 5px; }

/* ── 底部链接 ── */
.pd-foot { text-align: center; padding: 4px 0 8px; }
.pd-foot__link { font-size: 13px; color: var(--pd-brand); font-weight: 600; text-decoration: none; }
.pd-foot__link:hover { text-decoration: underline; }

/* ── 空态 / 骨架 ── */
.pd-empty { text-align: center; padding: 48px 24px; display: flex; flex-direction: column; align-items: center; gap: 8px; }
.pd-empty__emoji { font-size: 52px; }
.pd-empty__title { font-size: 18px; font-weight: 700; margin: 4px 0 0; color: var(--el-text-color-primary); }
.pd-empty__desc { font-size: 13px; color: var(--el-text-color-secondary); margin: 0; max-width: 420px; line-height: 1.6; }
.pd-empty__actions { display: flex; gap: 12px; margin-top: 14px; flex-wrap: wrap; justify-content: center; }
.pd-btn { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 600; padding: 9px 18px; border-radius: 10px; text-decoration: none; border: 1px solid var(--el-border-color); color: var(--el-text-color-regular); transition: border-color .15s, color .15s; }
.pd-btn:hover { border-color: var(--pd-brand); color: var(--pd-brand); }
.pd-btn--primary { background: var(--pd-brand); border-color: var(--pd-brand); color: #fff; }
.pd-btn--primary:hover { background: #229A94; color: #fff; }

.pd-skeleton { display: flex; flex-direction: column; gap: 16px; }
.pd-sk-card { height: 120px; border-radius: 14px; background: linear-gradient(100deg, var(--el-fill-color-light) 30%, var(--el-fill-color) 50%, var(--el-fill-color-light) 70%); background-size: 200% 100%; animation: pd-shimmer 1.3s infinite; }

@keyframes pd-bob { 0%,100% { transform: translateY(0); } 50% { transform: translateY(-6px); } }
@keyframes pd-shimmer { 0% { background-position: 200% 0; } 100% { background-position: -200% 0; } }

@media (prefers-reduced-motion: reduce) {
  .pdash *, .pdash *::before, .pdash *::after { animation: none !important; transition-duration: .01ms !important; }
}

/* ── 深色模式 ── */
:global(html.dark) .pd-hero {
  background: linear-gradient(135deg, #102826 0%, #0E1F1E 60%, #131820 100%);
  border-color: #1A3A38;
}
:global(html.dark) .pd-hero__h1 { color: #E0F5F3; }
:global(html.dark) .pd-hero__sub { color: #5A8888; }
:global(html.dark) .pd-hero__url { background: #0A1B1A; border-color: #1A3A38; }
:global(html.dark) .pd-hero__url-val { color: #4DC8C2; }
:global(html.dark) .pd-persona {
  background: linear-gradient(135deg, rgba(43,168,162,0.14), rgba(59,130,246,0.10));
  border-color: rgba(43,168,162,0.30);
}
:global(html.dark) .pd-ms--cost { background: linear-gradient(135deg, rgba(245,158,11,0.16), rgba(236,72,153,0.12)); border-color: rgba(245,158,11,0.32); }
:global(html.dark) .pd-ms__costnum { color: #fbbf24; }
</style>
