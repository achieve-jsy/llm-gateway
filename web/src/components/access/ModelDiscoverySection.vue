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
      <button class="mds-refresh" @click="load" :disabled="loading">
        <el-icon :class="{ spinning: loading }"><Refresh /></el-icon>
      </button>
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
          :class="`gc-s-${g.strategy}`"
          @click="copyName(g)"
          role="button"
          :title="`点击复制：${g.name}`"
        >
          <!-- 顶部彩色条 -->
          <div class="gc-stripe"></div>

          <!-- 主体 -->
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

            <!-- 单价（每百万 tokens） -->
            <div class="gc-price" v-if="caps[g.name] && (caps[g.name].input_price_per_million > 0 || caps[g.name].output_price_per_million > 0)">
              <span class="gc-price__item">
                <i class="gc-price__dot in" /> 输入
                <b>{{ formatPrice(caps[g.name].input_price_per_million) }}</b>
              </span>
              <span class="gc-price__item">
                <i class="gc-price__dot out" /> 输出
                <b>{{ formatPrice(caps[g.name].output_price_per_million) }}</b>
              </span>
              <span class="gc-price__unit">/ 1M tokens</span>
            </div>

            <!-- 底部：能力 + 元信息 -->
            <div class="gc-foot">
              <div class="gc-caps">
                <template v-if="caps[g.name]">
                  <span class="cap cap-mode">{{ (MODE_LABEL[caps[g.name].mode] ?? caps[g.name].mode) || 'chat' }}</span>
                  <span class="cap cap-v" v-if="caps[g.name].supports_vision">视觉</span>
                  <span class="cap cap-f" v-if="caps[g.name].supports_function_calling">工具</span>
                  <span class="cap cap-c" v-if="caps[g.name].max_input_tokens > 0">
                    {{ formatTokens(caps[g.name].max_input_tokens) }}
                  </span>
                </template>
                <span class="cap cap-loading" v-else-if="capsLoading">—</span>
              </div>

              <div class="gc-meta">
                <span class="gc-badge" :class="`badge-${g.strategy}`">
                  {{ STRATEGY_LABEL[g.strategy] ?? g.strategy }}
                </span>
                <span class="gc-models">{{ g.model_count ?? 0 }} 个模型</span>
              </div>
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
import { ref, computed, onMounted } from 'vue'
import {
  Search, CopyDocument, Check, Refresh,
  CircleCloseFilled,
} from '@element-plus/icons-vue'
import { groupsApi } from '@/api/groups'
import type { GroupListItem, GroupEntitlement } from '@/types/group'
import type { GroupCapability } from '@/api/groups'
import { copyToClipboard } from '@/utils/clipboard'
import { useAuthStore } from '@/stores/auth'
import http from '@/services/http'

const STRATEGY_LABEL: Record<string, string> = {
  round_robin: '轮询',
  priority: '优先级',
  weighted: '加权',
}

const MODE_LABEL: Record<string, string> = {
  chat: '对话',
  completion: '补全',
  embedding: '向量',
  image_generation: '图像生成',
  audio_transcription: '语音转写',
  audio_speech: '语音合成',
}

const groups = ref<GroupListItem[]>([])
const caps = ref<Record<string, GroupCapability>>({})
const loading = ref(false)
const capsLoading = ref(false)
const error = ref('')
const searchQuery = ref('')
const modeFilter = ref('')
const copiedId = ref<number | null>(null)
const toastVisible = ref(false)
const toastName = ref('')
let copyTimer: ReturnType<typeof setTimeout> | null = null
let toastTimer: ReturnType<typeof setTimeout> | null = null

const authStore = useAuthStore()
const entitlementsMap = ref<Map<number, GroupEntitlement[]>>(new Map())
const myDeptIds = ref<string[]>([])
const myStudioId = ref('')

const allModes = computed(() => {
  const s = new Set(Object.values(caps.value).map(c => c.mode).filter(Boolean))
  return Array.from(s).sort()
})

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
  return list
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

function resetFilters() {
  searchQuery.value = ''
  modeFilter.value = ''
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
  margin-bottom: 18px;
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

/* ── 计数 ─────────────────────────────────── */
.mds-count {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin: 0 0 12px;
}

/* ── 网格 ─────────────────────────────────── */
.mds-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(288px, 1fr));
  gap: 14px;
}

/* ── 骨架屏卡片 ──────────────────────────── */
.gc-skeleton {
  cursor: default !important;
  pointer-events: none;
  &:hover { transform: none !important; box-shadow: none !important; }
  &:hover .gc-stripe { width: 0 !important; }
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
  cursor: pointer;
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

/* 顶部彩条 */
.gc-stripe {
  height: 3px;
  background: #D8EEEC;
  transition: width 0.3s;
}
.gc-s-round_robin .gc-stripe { background: linear-gradient(90deg, #3498DB, #76C5F0); }
.gc-s-priority    .gc-stripe { background: linear-gradient(90deg, #9B59B6, #C39BD3); }
.gc-s-weighted    .gc-stripe { background: linear-gradient(90deg, #E67E22, #F0A96A); }

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

/* 单价 */
.gc-price {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 12px;
  padding: 7px 10px;
  background: #F6FBFB;
  border: 1px solid #E2F1F0;
  border-radius: 8px;
  font-size: 11.5px;
  color: #6A8E8C;
}
.gc-price__item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  b {
    color: #0D4A47;
    font-family: 'Fira Code', 'JetBrains Mono', monospace;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
}
.gc-price__dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  display: inline-block;
  &.in { background: #3498DB; }
  &.out { background: #36B34E; }
}
.gc-price__unit {
  margin-left: auto;
  font-size: 10.5px;
  color: #A8C4C2;
}

/* 底部 */
.gc-foot {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 8px;
  margin-top: 2px;
  flex-wrap: wrap;
}

.gc-caps {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
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
.cap-loading { color: #B0CCCB; background: transparent; border-color: transparent; }

.gc-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 3px;
  flex-shrink: 0;
}

.gc-badge {
  font-size: 10.5px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 99px;
  border: 1px solid;
  white-space: nowrap;
}

.badge-round_robin { color: #2471A3; background: rgba(36,113,163,0.08);   border-color: rgba(36,113,163,0.2); }
.badge-priority    { color: #7D3C98; background: rgba(125,60,152,0.08);   border-color: rgba(125,60,152,0.2); }
.badge-weighted    { color: #CA6F1E; background: rgba(202,111,30,0.08);   border-color: rgba(202,111,30,0.2); }

.gc-models {
  font-size: 11px;
  color: #A8C4C2;
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
  .mds-refresh { margin-left: 0; align-self: flex-end; }
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
:global(html.dark) .gc-models { color: #2A5A58; }
:global(html.dark) .gc-price { background: #0B1817; border-color: #1A3230; color: #4A7A78; }
:global(html.dark) .gc-price__item b { color: #A0E0DC; }
:global(html.dark) .gc-price__unit { color: #2A5A58; }

:global(html.dark) .cap-mode { color: #5DC370; background: rgba(54,179,78,0.1); border-color: rgba(54,179,78,0.25); }
:global(html.dark) .cap-v    { color: #5DADE2; background: rgba(52,152,219,0.1); border-color: rgba(52,152,219,0.22); }
:global(html.dark) .cap-f    { color: #BB8FCE; background: rgba(155,89,182,0.1); border-color: rgba(155,89,182,0.22); }
:global(html.dark) .cap-c    { color: #2A6A68; background: transparent; border-color: #1A3230; }

:global(html.dark) .badge-round_robin { color: #5DADE2; background: rgba(52,152,219,0.1); border-color: rgba(52,152,219,0.22); }
:global(html.dark) .badge-priority    { color: #BB8FCE; background: rgba(155,89,182,0.1); border-color: rgba(155,89,182,0.22); }
:global(html.dark) .badge-weighted    { color: #F0B27A; background: rgba(230,126,34,0.1); border-color: rgba(230,126,34,0.22); }

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
</style>
