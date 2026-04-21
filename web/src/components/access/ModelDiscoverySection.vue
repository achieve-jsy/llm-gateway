<template>
  <div class="mds">
    <!-- ── 工具栏 ──────────────────────────────── -->
    <div class="mds-bar">
      <el-input
        v-model="searchQuery"
        placeholder="搜索模型组..."
        clearable
        size="default"
        class="mds-search"
        :prefix-icon="Search"
      />
      <div class="mds-filters" v-if="allModes.length > 1">
        <button
          v-for="m in ['', ...allModes]"
          :key="m"
          class="mds-flt"
          :class="{ active: modeFilter === m }"
          @click="modeFilter = m"
        >{{ m === '' ? '全部' : (MODE_LABEL[m] ?? m) }}</button>
      </div>
      <el-select v-model="sortMode" size="default" class="mds-sort">
        <el-option label="热门" value="hot" />
        <el-option label="最快" value="fastest" />
        <el-option label="名称" value="name" />
        <el-option label="价格" value="price" />
        <el-option label="上下文" value="context" />
      </el-select>
      <button class="mds-refresh" @click="load" :disabled="loading">
        <el-icon :class="{ spinning: loading }"><Refresh /></el-icon>
      </button>
    </div>

    <!-- Chip filter row -->
    <div class="chip-row">
      <span class="chip-group-label">能力</span>
      <button class="chip" :class="{ active: chipFilters.vision }" @click="chipFilters.vision = !chipFilters.vision">视觉理解</button>
      <button class="chip" :class="{ active: chipFilters.functionCalling }" @click="chipFilters.functionCalling = !chipFilters.functionCalling">工具调用</button>
      <button class="chip" :class="{ active: chipFilters.longContext }" @click="chipFilters.longContext = !chipFilters.longContext">长上下文</button>
      <span class="chip-divider"></span>
      <span class="chip-group-label">价格</span>
      <button class="chip" :class="{ active: chipFilters.priceRange === 'free' }" @click="togglePriceChip('free')">免费</button>
      <button class="chip" :class="{ active: chipFilters.priceRange === 'cheap' }" @click="togglePriceChip('cheap')">≤ $1</button>
      <button class="chip" :class="{ active: chipFilters.priceRange === 'mid' }" @click="togglePriceChip('mid')">$1-$5</button>
      <button class="chip" :class="{ active: chipFilters.priceRange === 'expensive' }" @click="togglePriceChip('expensive')">> $5</button>
      <button v-if="hasAnyChipFilter" class="chip chip-clear" @click="clearChips">清除</button>
    </div>

    <!-- ── 加载态 ────────────────────────────── -->
    <div v-if="loading" class="mds-grid">
      <div v-for="n in 6" :key="n" class="gc gc-skeleton">
        <div class="sk-line sk-w70"></div>
        <div class="sk-line sk-w40 sk-sm"></div>
        <div class="sk-line sk-w90 sk-sm" style="margin-top:12px"></div>
        <div class="sk-line sk-w60 sk-sm"></div>
        <div class="sk-chips">
          <div class="sk-chip"></div>
          <div class="sk-chip"></div>
        </div>
      </div>
    </div>

    <!-- ── 错误态 ────────────────────────────── -->
    <div v-else-if="error" class="mds-state mds-err">
      <el-icon><CircleCloseFilled /></el-icon>
      <span>{{ error }}</span>
      <el-button size="small" type="primary" @click="load">重试</el-button>
    </div>

    <!-- ── 空态 ─────────────────────────────── -->
    <div v-else-if="groups.length === 0" class="mds-state mds-empty">
      <svg width="52" height="52" viewBox="0 0 52 52" fill="none">
        <rect x="6" y="14" width="40" height="28" rx="5" stroke="currentColor" stroke-width="1.5" fill="none"/>
        <path d="M6 20h40" stroke="currentColor" stroke-width="1.5"/>
        <circle cx="13" cy="17" r="1.5" fill="currentColor"/>
        <circle cx="19" cy="17" r="1.5" fill="currentColor"/>
      </svg>
      <p>暂无可用模型组</p>
      <span>请先在模型组管理中创建并启用模型组</span>
    </div>

    <!-- ── 卡片 ──────────────────────────────── -->
    <template v-else>
      <p class="mds-count" v-if="filteredGroups.length !== groups.filter(g=>g.enabled).length">
        显示 {{ filteredGroups.length }} / {{ groups.filter(g=>g.enabled).length }} 个模型组
      </p>

      <div v-if="filteredGroups.length > 0" class="mds-grid">
        <div
          v-for="g in filteredGroups"
          :key="g.id"
          class="gc"
        >
          <div class="gc-body">
            <!-- 名称行 -->
            <div class="gc-name-row">
              <span class="gc-name">{{ g.name }}</span>
              <span class="gc-copy-icon" :class="{ ok: copiedId === g.id }">
                <el-icon><component :is="copiedId === g.id ? Check : CopyDocument" /></el-icon>
              </span>
            </div>

            <!-- 描述 -->
            <p class="gc-desc">{{ g.description || '暂无描述' }}</p>

            <!-- 视频模型专属信息 -->
            <div class="gc-video-detail" v-if="caps[g.name]?.mode === 'video'">
              <div class="gc-video-endpoint">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
                <code>{{ caps[g.name].video_api_endpoint || 'POST /v1/contents/generations/tasks' }}</code>
              </div>
              <div class="gc-video-tags" v-if="caps[g.name].video_supported_actions?.length">
                <span class="video-tag" v-for="act in caps[g.name].video_supported_actions" :key="act">
                  {{ act === 'text_to_video' ? '文生视频' : act === 'image_to_video' ? '图生视频' : act === 'multimodal_reference' ? '多模态参考' : act }}
                </span>
              </div>
              <div class="gc-video-specs" v-if="caps[g.name].video_supported_resolutions?.length || caps[g.name].video_max_duration_seconds">
                <span class="video-spec" v-if="caps[g.name].video_supported_resolutions?.length">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><rect x="2" y="3" width="20" height="14" rx="2"/><polygon points="10,8 16,12 10,16"/></svg>
                  {{ caps[g.name].video_supported_resolutions?.join(' / ') }}
                </span>
                <span class="video-spec" v-if="caps[g.name].video_max_duration_seconds">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12,6 12,12 16,14"/></svg>
                  {{ formatVideoDuration(caps[g.name].video_max_duration_seconds) }}
                </span>
              </div>
            </div>

            <!-- Signal line: performance indicators -->
            <div class="gc-signals" v-if="perfMap[g.name]">
              <span class="gc-signal gc-signal-speed" :class="ttftSignalClass(perfMap[g.name].ttft_p50_ms)">
                <svg width="12" height="12" viewBox="0 0 16 16"><path d="M13 2L3 14" stroke="currentColor" stroke-width="2" stroke-linecap="round"/><path d="M7 8L3 14" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>
                {{ formatPerfMs(perfMap[g.name].ttft_p50_ms) }}
              </span>
              <span class="gc-signal gc-signal-rate" :class="rateSignalClass(perfMap[g.name].success_rate)">
                <svg width="12" height="12" viewBox="0 0 16 16"><path d="M13.5 4.5L6 12L2.5 8.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg>
                {{ (perfMap[g.name].success_rate * 100).toFixed(1) }}%
              </span>
            </div>

            <!-- 价格（紧凑单行） -->
            <div class="gc-price" v-if="caps[g.name] && (caps[g.name].input_price_per_million > 0 || caps[g.name].output_price_per_million > 0)">
              <span>输入 <b>{{ formatPrice(caps[g.name].input_price_per_million) }}</b></span>
              <span class="gc-price-sep">/</span>
              <span>输出 <b>{{ formatPrice(caps[g.name].output_price_per_million) }}</b></span>
              <span class="gc-price-unit">/1M</span>
            </div>

            <!-- 能力标签 -->
            <div class="gc-caps">
              <template v-if="caps[g.name]">
                <span class="cap cap-mode">{{ MODE_LABEL[caps[g.name].mode] ?? caps[g.name].mode }}</span>
                <span class="cap cap-v" v-if="caps[g.name].supports_vision">视觉</span>
                <span class="cap cap-f" v-if="caps[g.name].supports_function_calling || caps[g.name].supports_tool_choice">工具</span>
                <span class="cap cap-c" v-if="caps[g.name].max_input_tokens > 0">{{ formatTokens(caps[g.name].max_input_tokens) }}</span>
              </template>
            </div>

            <!-- 操作按钮 -->
            <div class="gc-actions">
              <button class="gc-btn gc-btn-copy" @click.stop="copyName(g)">
                <el-icon><CopyDocument /></el-icon>
                <span>复制</span>
              </button>
              <button class="gc-btn gc-btn-try" @click.stop="tryModel(g)">
                <span>试一试</span>
                <el-icon><ArrowRight /></el-icon>
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-else class="mds-no-result">
        没有匹配结果 &nbsp;
        <el-button text size="small" @click="resetFilters">清除筛选</el-button>
      </div>
    </template>

    <!-- 复制 Toast -->
    <Transition name="t">
      <div class="copy-toast" v-if="toastVisible">
        <el-icon><Check /></el-icon>
        <span>已复制 <b>{{ toastName }}</b></span>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import {
  Search, CopyDocument, Check, Refresh,
  CircleCloseFilled, ArrowRight,
} from '@element-plus/icons-vue'
import { groupsApi } from '@/api/groups'
import type { GroupListItem, GroupEntitlement } from '@/types/group'
import type { GroupCapability } from '@/api/groups'
import { copyToClipboard } from '@/utils/clipboard'
import { useAuthStore } from '@/stores/auth'
import { getPerformance } from '@/services/control-plane'
import type { PerformanceItem } from '@/services/control-plane'

const MODE_LABEL: Record<string, string> = {
  chat: '对话',
  completion: '补全',
  embedding: '向量',
  embedding_vl: '多模态向量',
  rerank: '重排序',
  rerank_vl: '多模态重排序',
  image_generation: '图像生成',
  audio_transcription: '语音转写',
  audio_speech: '语音合成',
  video: '视频生成',
}

const router = useRouter()
const groups = ref<GroupListItem[]>([])
const caps = ref<Record<string, GroupCapability>>({})
const loading = ref(false)
const capsLoading = ref(false)
const error = ref('')
const searchQuery = ref('')
const modeFilter = ref('')
const sortMode = ref('hot')
const copiedId = ref<number | null>(null)
const toastVisible = ref(false)
const toastName = ref('')
const perfMap = ref<Record<string, PerformanceItem>>({})
let copyTimer: ReturnType<typeof setTimeout> | null = null
let toastTimer: ReturnType<typeof setTimeout> | null = null

const chipFilters = reactive({
  vision: false,
  functionCalling: false,
  longContext: false,
  priceRange: '' as '' | 'free' | 'cheap' | 'mid' | 'expensive',
})

const authStore = useAuthStore()
const entitlementsMap = ref<Map<number, GroupEntitlement[]>>(new Map())
const myDeptIds = ref<string[]>([])
const myStudioId = ref('')

const allModes = computed(() => {
  const s = new Set(Object.values(caps.value).map(c => c.mode).filter(Boolean))
  return Array.from(s).sort()
})

const hasAnyChipFilter = computed(() =>
  chipFilters.vision || chipFilters.functionCalling || chipFilters.longContext || chipFilters.priceRange !== ''
)

// 判断模型组是否对当前用户可用
function isGroupAvailable(group: GroupListItem): boolean {
  // 公开组始终可用
  if (group.open_mode === 'public') return true

  // 受控组需要检查授权状态
  const role = authStore.effectiveRole
  const ents = entitlementsMap.value.get(group.id) || []

  // admin 可以看到所有组
  if (role === 'admin') return true

  // studio_admin: 检查工作室是否被授权
  if (role === 'studio_admin' && myStudioId.value) {
    return ents.some(e => e.scope_type === 'studio' && e.scope_id === myStudioId.value && e.enabled)
  }

  // dept_admin: 检查本部门是否被开通
  if (role === 'dept_admin' && myDeptIds.value.length > 0) {
    return myDeptIds.value.some(deptId =>
      ents.some(e => e.scope_type === 'dept' && e.scope_id === deptId && e.enabled)
    )
  }

  // 普通用户：只能看到公开组（已在上面处理）
  return false
}

const filteredGroups = computed(() => {
  let list = groups.value.filter(g => g.enabled && isGroupAvailable(g))
  if (modeFilter.value) list = list.filter(g => caps.value[g.name]?.mode === modeFilter.value)
  const q = searchQuery.value.toLowerCase().trim()
  if (q) list = list.filter(g => g.name.toLowerCase().includes(q) || (g.description || '').toLowerCase().includes(q))
  if (chipFilters.vision) {
    list = list.filter(g => caps.value[g.name]?.supports_vision)
  }
  if (chipFilters.functionCalling) {
    list = list.filter(g => {
      const cap = caps.value[g.name]
      return cap?.supports_function_calling || cap?.supports_tool_choice
    })
  }
  if (chipFilters.longContext) {
    list = list.filter(g => (caps.value[g.name]?.max_input_tokens || 0) > 32_000)
  }

  if (chipFilters.priceRange) {
    list = list.filter(g => {
      const cap = caps.value[g.name]
      if (!cap) return true
      const avgPrice = getAveragePrice(cap)
      switch (chipFilters.priceRange) {
        case 'free':
          return cap.input_price_per_million <= 0 && cap.output_price_per_million <= 0
        case 'cheap':
          return avgPrice > 0 && avgPrice <= 1
        case 'mid':
          return avgPrice > 1 && avgPrice <= 5
        case 'expensive':
          return avgPrice > 5
        default:
          return true
      }
    })
  }

  return [...list].sort(compareGroups)
})

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(0)}M`
  if (n >= 1_000) return `${Math.round(n / 1_000)}K`
  return `${n}`
}

// 单价单位为 USD / 1M tokens。免费或缺价返回占位符。
function formatPrice(n: number): string {
  if (!n || n <= 0) return '免费'
  if (n < 0.01) return `$${n.toFixed(4)}`
  if (n < 1) return `$${n.toFixed(3)}`
  return `$${n.toFixed(2)}`
}

function getAveragePrice(cap?: GroupCapability): number {
  if (!cap) return 0
  return ((cap.input_price_per_million || 0) + (cap.output_price_per_million || 0)) / 2
}

function compareGroups(a: GroupListItem, b: GroupListItem): number {
  const capA = caps.value[a.name]
  const capB = caps.value[b.name]

  if (sortMode.value === 'hot') {
    const countA = perfMap.value[a.name]?.req_count || 0
    const countB = perfMap.value[b.name]?.req_count || 0
    return countB - countA
  }
  if (sortMode.value === 'fastest') {
    const ttftA = perfMap.value[a.name]?.ttft_p50_ms ?? Infinity
    const ttftB = perfMap.value[b.name]?.ttft_p50_ms ?? Infinity
    return ttftA - ttftB
  }
  if (sortMode.value === 'name') {
    return a.name.toLowerCase().localeCompare(b.name.toLowerCase())
  }
  if (sortMode.value === 'price') {
    return getAveragePrice(capA) - getAveragePrice(capB)
  }
  // context
  return (capA?.max_input_tokens || 0) - (capB?.max_input_tokens || 0)
}

function resetFilters() {
  searchQuery.value = ''
  modeFilter.value = ''
  clearChips()
}

function togglePriceChip(range: 'free' | 'cheap' | 'mid' | 'expensive') {
  chipFilters.priceRange = chipFilters.priceRange === range ? '' : range
}

function clearChips() {
  chipFilters.vision = false
  chipFilters.functionCalling = false
  chipFilters.longContext = false
  chipFilters.priceRange = ''
}

function ttftSignalClass(ttft: number): string {
  if (ttft <= 1200) return 'signal-good'
  if (ttft <= 3000) return 'signal-warn'
  return 'signal-danger'
}

function rateSignalClass(rate: number): string {
  if (rate >= 0.99) return 'signal-good'
  if (rate >= 0.95) return 'signal-warn'
  return 'signal-danger'
}

function formatPerfMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}

function formatVideoDuration(sec: number): string {
  if (sec <= 0) return ''
  if (sec >= 60) {
    const min = Math.floor(sec / 60)
    const s = sec % 60
    return s > 0 ? `最长 ${min}分${s}秒` : `最长 ${min}分钟`
  }
  return `最长 ${sec}秒`
}

function tryModel(g: GroupListItem) {
  router.push({ path: '/admin/chat-playground', query: { model: g.name } })
}

function copyName(g: GroupListItem) {
  const name = g.name
  copyToClipboard(name)
  copiedId.value = g.id
  if (copyTimer) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copiedId.value = null }, 1800)

  toastName.value = name
  toastVisible.value = true
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toastVisible.value = false }, 2200)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await groupsApi.listGroups({ page_size: 200 })
    groups.value = res.data.data ?? []

    // 获取用户角色和部门/工作室信息
    const role = authStore.effectiveRole
    if (role === 'studio_admin' || role === 'dept_admin') {
      // 从 orgRoles 提取部门/工作室 ID
      const deptRoles = authStore.orgRoles.filter(r => r.role === 'dept_admin' && r.scope_type === 'dept')
      myDeptIds.value = deptRoles.map(r => r.scope_id)

      const studioRole = authStore.orgRoles.find(r => r.role === 'studio_admin' && r.scope_type === 'studio')
      myStudioId.value = studioRole?.scope_id || ''

      // 获取所有模型组的授权记录
      const enabledGroups = groups.value.filter(g => g.enabled && g.open_mode === 'controlled')
      const entitlementPromises = enabledGroups.map(async g => {
        try {
          const res = await groupsApi.listEntitlements(g.id)
          return { groupId: g.id, items: res.data.data?.items || [] }
        } catch {
          return { groupId: g.id, items: [] }
        }
      })

      const entResults = await Promise.all(entitlementPromises)
      const newMap = new Map<number, GroupEntitlement[]>()
      for (const r of entResults) {
        newMap.set(r.groupId, r.items)
      }
      entitlementsMap.value = newMap
    }
  } catch (e: any) {
    error.value = e?.message || '获取模型组失败'
    return
  } finally {
    loading.value = false
  }
  capsLoading.value = true
  try {
    const r = await groupsApi.getGroupCapabilities()
    caps.value = r.data.data ?? {}
  } catch { /* noop */ } finally {
    capsLoading.value = false
  }
  // After loading caps, also fetch performance data
  try {
    const perfResult = await getPerformance('1h', 'group', '', 200, 0)
    const map: Record<string, PerformanceItem> = {}
    for (const item of perfResult.items || []) {
      map[item.name] = item
    }
    perfMap.value = map
  } catch { /* performance data is optional */ }
}

onMounted(() => { load() })
</script>

<style scoped>
/* ── 根 ──────────────────────────────────── */
.mds { position: relative; }

/* ── 工具栏 ───────────────────────────────── */
.mds-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.mds-search { width: 240px; }

.mds-filters {
  display: flex;
  gap: 3px;
  padding: 3px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
}

.mds-flt {
  padding: 4px 13px;
  font-size: 12px;
  font-weight: 500;
  color: var(--el-text-color-secondary);
  background: transparent;
  border: none;
  border-radius: 5px;
  cursor: pointer;
  transition: all 0.14s;
  white-space: nowrap;

  &:hover { color: var(--el-color-primary); background: var(--el-bg-color); }
  &.active {
    background: var(--el-bg-color);
    color: var(--el-color-primary);
    font-weight: 700;
    box-shadow: 0 1px 4px rgba(0,0,0,0.07);
  }
}

.mds-sort {
  width: 110px;
}

.mds-refresh {
  width: 34px;
  height: 34px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  color: var(--el-text-color-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  margin-left: auto;
  flex-shrink: 0;

  &:hover { background: var(--el-fill-color); color: var(--el-color-primary); }
  &:disabled { opacity: 0.5; cursor: not-allowed; }
  .el-icon { font-size: 15px; }
}

.spinning { animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

/* ── Chip filter row ─────────────────────── */
.chip-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 12px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
  margin-bottom: 16px;
}

.chip-group-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  margin-right: 2px;
}

.chip-divider {
  width: 1px;
  height: 20px;
  background: var(--el-border-color-lighter);
  margin: 0 4px;
}

.chip {
  display: inline-flex;
  align-items: center;
  height: 28px;
  padding: 0 12px;
  font-size: 12px;
  font-weight: 500;
  border-radius: 999px;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
  cursor: pointer;
  transition: all 0.15s;
  white-space: nowrap;

  &:hover {
    border-color: #36B34E;
    color: #36B34E;
  }

  &.active {
    background: #36B34E;
    border-color: #36B34E;
    color: #FFFFFF;
  }
}

.chip-clear {
  background: transparent;
  border: none;
  color: #36B34E;
  font-weight: 600;

  &:hover {
    text-decoration: underline;
    background: transparent;
  }
}

/* ── 计数 ─────────────────────────────────── */
.mds-count {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin: 0 0 12px;
}

/* ── 网格 ─────────────────────────────────── */
.mds-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 14px;
}

/* ── 骨架屏卡片 ──────────────────────────── */
.gc-skeleton {
  cursor: default;
  pointer-events: none;
  opacity: 0.7;
}

.sk-line {
  height: 14px;
  background: var(--el-fill-color);
  border-radius: 4px;
  animation: shimmer 1.4s ease-in-out infinite;
}
.sk-sm { height: 11px; margin-top: 8px; }
.sk-w40 { width: 40%; }
.sk-w60 { width: 60%; }
.sk-w70 { width: 70%; }
.sk-w90 { width: 90%; }
.sk-chips { display: flex; gap: 6px; margin-top: 14px; }
.sk-chip { width: 52px; height: 20px; border-radius: 4px; background: var(--el-fill-color); animation: shimmer 1.4s ease-in-out infinite; }

@keyframes shimmer {
  0%,100% { opacity: 1; }
  50% { opacity: 0.5; }
}

/* ── 模型组卡片 ─────────────────────────── */
.gc {
  background: #FFFFFF;
  border: 1px solid #E8F0EF;
  border-radius: 14px;
  overflow: hidden;
  transition: box-shadow 0.22s, transform 0.22s, border-color 0.22s;
  position: relative;
  display: flex;
  flex-direction: column;
}

.gc:hover {
  box-shadow: 0 10px 36px rgba(54,179,78,0.12), 0 2px 8px rgba(0,0,0,0.05);
  transform: translateY(-3px);
  border-color: #A8DAD7;
}

.gc-body {
  padding: 16px 18px 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
}

/* 名称行 */
.gc-name-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.gc-name {
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 14px;
  font-weight: 700;
  color: #0D4A47;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gc-copy-icon {
  flex-shrink: 0;
  width: 26px;
  height: 26px;
  border-radius: 6px;
  background: #EEF9F0;
  border: 1px solid #D0E8E6;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #7ABCBA;
  font-size: 13px;
  transition: all 0.15s;
}

.gc:hover .gc-copy-icon {
  background: rgba(54,179,78,0.1);
  border-color: #36B34E;
  color: #36B34E;
}

.gc-copy-icon.ok {
  background: rgba(14,163,113,0.1);
  border-color: #0EA371;
  color: #0EA371;
}

/* 描述 */
.gc-desc {
  font-size: 12.5px;
  color: #7A9A98;
  line-height: 1.6;
  margin: 0;
  overflow: hidden;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  flex: 1;
}

/* Signal line */
.gc-signals {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 2px;
}

.gc-signal {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.gc-signal svg {
  flex-shrink: 0;
}

.signal-good { color: #059669; }
.signal-warn { color: #D97706; }
.signal-danger { color: #DC2626; }

/* 价格 */
.gc-price {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
  font-size: 13px;
  color: #6A8E8C;
  padding: 4px 0;
}

.gc-price b {
  color: #0D4A47;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.gc-price-sep {
  color: #C0D6D4;
  margin: 0 2px;
}

.gc-price-unit {
  font-size: 11px;
  color: #A8C4C2;
}

/* 能力标签 */
.gc-caps {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 2px;
}

.cap {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 5px;
  line-height: 1.6;
  border: 1px solid;
}

.cap-mode    { color: #1A7A37; background: rgba(54,179,78,0.08);  border-color: rgba(54,179,78,0.2); }
.cap-v       { color: #2471A3; background: rgba(36,113,163,0.08);   border-color: rgba(36,113,163,0.2); }
.cap-f       { color: #7D3C98; background: rgba(125,60,152,0.07);   border-color: rgba(125,60,152,0.18); }
.cap-c       { color: #8A9A98; background: #F5FAFA; border-color: #D8EEEC; font-variant-numeric: tabular-nums; }

/* ── 视频模型专属卡片样式 ──────────────────── */
.gc-video-detail {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  background: rgba(124, 58, 237, 0.04);
  border: 1px solid rgba(124, 58, 237, 0.12);
  border-radius: 8px;
  margin-top: 2px;
}

.gc-video-endpoint {
  display: flex;
  align-items: center;
  gap: 5px;
  color: #7C3AED;
  font-size: 11px;

  svg {
    flex-shrink: 0;
    color: #8B5CF6;
  }

  code {
    font-family: 'Fira Code', 'JetBrains Mono', monospace;
    font-size: 10.5px;
    font-weight: 600;
    color: #6D28D9;
    background: rgba(124, 58, 237, 0.06);
    padding: 1px 5px;
    border-radius: 3px;
  }
}

.gc-video-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.video-tag {
  display: inline-flex;
  align-items: center;
  font-size: 10.5px;
  font-weight: 600;
  padding: 1px 7px;
  border-radius: 4px;
  background: rgba(124, 58, 237, 0.08);
  color: #7C3AED;
  border: 1px solid rgba(124, 58, 237, 0.18);
}

.gc-video-specs {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.video-spec {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  color: #6D6D7A;
  font-weight: 500;

  svg {
    flex-shrink: 0;
    color: #8B8B9A;
  }
}

:global(html.dark) .gc-video-detail {
  background: rgba(139, 92, 246, 0.06);
  border-color: rgba(139, 92, 246, 0.15);
}

:global(html.dark) .gc-video-endpoint {
  color: #A78BFA;

  svg { color: #A78BFA; }

  code {
    color: #C4B5FD;
    background: rgba(139, 92, 246, 0.1);
  }
}

:global(html.dark) .video-tag {
  background: rgba(139, 92, 246, 0.1);
  color: #A78BFA;
  border-color: rgba(139, 92, 246, 0.2);
}

:global(html.dark) .video-spec {
  color: #9D9DAD;

  svg { color: #A0A0B0; }
}

/* 操作按钮 */
.gc-actions {
  display: flex;
  gap: 8px;
  margin-top: 6px;
}

.gc-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  font-weight: 600;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
  border: none;
  white-space: nowrap;
  flex: 1;
}

.gc-btn-copy {
  background: transparent;
  border: 1px solid var(--el-border-color-lighter);
  color: var(--el-text-color-secondary);

  &:hover {
    border-color: #36B34E;
    color: #36B34E;
    background: rgba(54,179,78,0.04);
  }
}

.gc-btn-try {
  background: #36B34E;
  color: #FFFFFF;

  &:hover {
    background: #2D9A42;
  }
}

/* ── 错误 / 空态 ─────────────────────────── */
.mds-state {
  border-radius: 14px;
  padding: 52px 24px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}

.mds-err {
  background: rgba(231,76,60,0.04);
  border: 1px solid rgba(231,76,60,0.16);
  .el-icon { font-size: 36px; color: #E74C3C; }
  span { color: #C0392B; font-size: 13px; font-weight: 500; }
}

.mds-empty {
  background: #FAFEFE;
  border: 1.5px dashed #C8E4E2;
  svg { color: #A8CECA; }
  p { font-size: 15px; font-weight: 700; color: #4A7A78; margin: 0; }
  span { font-size: 12px; color: #8ABCBA; }
}

.mds-no-result {
  padding: 40px;
  text-align: center;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

/* ── Toast ────────────────────────────────── */
.copy-toast {
  position: fixed;
  bottom: 28px;
  left: 50%;
  transform: translateX(-50%);
  background: #0D2B2A;
  color: #D0F0EE;
  font-size: 13px;
  padding: 11px 22px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  z-index: 9999;
  pointer-events: none;
  box-shadow: 0 12px 32px rgba(0,0,0,0.22);
  white-space: nowrap;

  b { color: #6CD083; }
  .el-icon { color: #6CD083; font-size: 15px; }
}

.t-enter-active, .t-leave-active { transition: opacity 0.2s, transform 0.2s; }
.t-enter-from, .t-leave-to { opacity: 0; transform: translateX(-50%) translateY(8px); }

/* ── 响应式 ───────────────────────────────── */
@media (max-width: 640px) {
  .mds-bar { flex-direction: column; align-items: stretch; }
  .mds-search { width: 100%; }
  .mds-filters { overflow-x: auto; }
  .mds-sort { width: 100%; }
  .mds-refresh { margin-left: 0; align-self: flex-end; }
  .chip-row { overflow-x: auto; flex-wrap: nowrap; padding-bottom: 8px; }
  .mds-grid { grid-template-columns: 1fr; }
}

/* ── 深色模式 ─────────────────────────────── */
:global(html.dark) .gc {
  background: #0E1D1C;
  border-color: #1A3230;
}
:global(html.dark) .gc:hover {
  border-color: #36B34E;
  box-shadow: 0 10px 36px rgba(0,0,0,0.4);
}
:global(html.dark) .gc-name { color: #A0E0DC; }
:global(html.dark) .gc-desc { color: #4A7A78; }
:global(html.dark) .gc-copy-icon { background: #132322; border-color: #1F3A38; color: #3A7A78; }
:global(html.dark) .gc-copy-icon.ok { background: rgba(14,163,113,0.12); border-color: #0EA371; color: #0EA371; }

:global(html.dark) .gc-price { color: #4A7A78; }
:global(html.dark) .gc-price b { color: #A0E0DC; }
:global(html.dark) .gc-price-unit { color: #2A5A58; }
:global(html.dark) .gc-price-sep { color: #2A5A58; }

:global(html.dark) .cap-mode { color: #5DC370; background: rgba(54,179,78,0.1); border-color: rgba(54,179,78,0.25); }
:global(html.dark) .cap-v    { color: #5DADE2; background: rgba(52,152,219,0.1); border-color: rgba(52,152,219,0.22); }
:global(html.dark) .cap-f    { color: #BB8FCE; background: rgba(155,89,182,0.1); border-color: rgba(155,89,182,0.22); }
:global(html.dark) .cap-c    { color: #2A6A68; background: transparent; border-color: #1A3230; }

:global(html.dark) .gc-btn-copy {
  background: transparent;
  border-color: #1A3230;
  color: #4A7A78;
}
:global(html.dark) .gc-btn-copy:hover {
  border-color: #36B34E;
  color: #36B34E;
  background: rgba(54,179,78,0.08);
}
:global(html.dark) .gc-btn-try {
  background: #36B34E;
  color: #FFFFFF;
}
:global(html.dark) .gc-btn-try:hover {
  background: #2D9A42;
}

:global(html.dark) .chip {
  background: #0A1B1A;
  border-color: #1A3230;
  color: #6A8E8C;
}
:global(html.dark) .chip:hover {
  border-color: #36B34E;
  color: #36B34E;
}
:global(html.dark) .chip.active {
  background: #36B34E;
  border-color: #36B34E;
  color: #FFFFFF;
}
:global(html.dark) .chip-clear {
  background: transparent;
  border: none;
  color: #5DC370;
}
:global(html.dark) .chip-divider {
  background: #1A3230;
}

:global(html.dark) .mds-empty {
  background: #091514;
  border-color: #1A3230;
}

:global(html.dark) .mds-filters {
  background: #0A1B1A;
  border-color: #1A3230;
}
:global(html.dark) .mds-flt.active { background: #132322; }
:global(html.dark) .mds-refresh {
  background: #0A1B1A;
  border-color: #1A3230;
}
:global(html.dark) .signal-good { color: #34D399; }
:global(html.dark) .signal-warn { color: #FBBF24; }
:global(html.dark) .signal-danger { color: #F87171; }
</style>