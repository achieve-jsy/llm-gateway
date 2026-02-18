<template>
  <div class="notification-bell" @click="togglePanel" ref="bellRef">
    <el-badge :value="totalUnread" :hidden="totalUnread === 0" :max="99"
      :color="notifUnreadCount > 0 ? '#F57C00' : undefined">
      <el-icon :size="20"><Bell /></el-icon>
    </el-badge>
    <el-popover
      :visible="panelVisible"
      :width="440"
      placement="bottom-end"
      trigger="manual"
      @hide="panelVisible = false"
    >
      <template #reference><span></span></template>
      <div class="notification-panel" @click.stop>
        <!-- 标题行：左侧来源 tab，右侧操作 -->
        <div class="panel-header">
          <div class="panel-source-tabs">
            <button
              class="source-tab"
              :class="{ active: activeSource === 'announcements' }"
              @click="switchSource('announcements')"
            >
              公告
              <span v-if="annUnreadCount > 0" class="source-badge">{{ annUnreadCount }}</span>
            </button>
            <button
              class="source-tab"
              :class="{ active: activeSource === 'notifications' }"
              @click="switchSource('notifications')"
            >
              通知
              <span v-if="notifUnreadCount > 0" class="source-badge source-badge--warn">{{ notifUnreadCount }}</span>
            </button>
          </div>
          <div class="panel-header-actions">
            <el-button v-if="currentUnreadCount > 0" text size="small" type="primary" @click="handleReadAll">全部已读</el-button>
            <el-button v-if="activeSource === 'announcements'" text size="small" @click="handleViewAll">查看全部</el-button>
          </div>
        </div>

        <!-- 未读 / 已读 pill 切换 -->
        <div class="panel-tab-pill">
          <button class="pill-btn" :class="{ active: activeTab === 'unread' }" @click="activeTab = 'unread'">
            未读<span v-if="currentUnreadCount > 0" class="pill-count">{{ currentUnreadCount }}</span>
          </button>
          <button class="pill-btn" :class="{ active: activeTab === 'history' }" @click="activeTab = 'history'">已读</button>
        </div>

        <!-- 公告列表 -->
        <template v-if="activeSource === 'announcements'">
          <div class="panel-list" v-show="activeTab === 'unread'">
            <div v-if="annUnreadItems.length === 0" class="panel-empty">
              <el-icon :size="28" color="var(--text-placeholder)"><Bell /></el-icon>
              <span class="empty-text">暂无未读公告</span>
            </div>
            <div
              v-for="item in annUnreadItems"
              :key="item.id"
              class="notification-item"
              :class="`level--${item.level}`"
            >
              <span class="item-dot" :class="`dot--${item.level}`"></span>
              <div class="item-body">
                <div class="item-header" @click="expandedId = expandedId === item.id ? '' : item.id">
                  <component :is="annCategoryIcon(item.category)" class="item-category-icon" />
                  <span class="item-title">{{ item.title }}</span>
                  <span class="item-time">{{ relativeTime(item.published_at) }}</span>
                </div>
                <div class="item-preview" v-if="expandedId !== item.id" @click="expandedId = item.id">
                  {{ truncate(markdownToPlainText(item.content), 60) }}
                </div>
                <div v-else class="item-expanded">
                  <div class="item-expanded-body">
                    <div class="item-full-content md-content" v-html="renderMarkdown(item.content)"></div>
                  </div>
                  <div class="item-actions">
                    <el-button size="small" text @click.stop="router.push(`/announcements/${item.id}`)">查看详情</el-button>
                    <el-button size="small" type="primary" @click.stop="handleAnnRead(item.id)">标记已读</el-button>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="panel-list" v-show="activeTab === 'history'">
            <div v-if="annHistoryItems.length === 0" class="panel-empty">
              <el-icon :size="28" color="var(--text-placeholder)"><Bell /></el-icon>
              <span class="empty-text">暂无已读公告</span>
            </div>
            <div
              v-for="item in annHistoryItems"
              :key="item.id"
              class="notification-item history-item"
              @click="expandedId = expandedId === item.id ? '' : item.id"
            >
              <div class="item-body">
                <div class="item-header">
                  <component :is="annCategoryIcon(item.category)" class="item-category-icon" />
                  <span class="item-title">{{ item.title }}</span>
                  <span class="item-time">{{ relativeTime(item.published_at) }}</span>
                </div>
                <div v-if="expandedId === item.id" class="item-expanded history-expanded">
                  <div class="item-expanded-body">
                    <div class="item-full-content md-content" v-html="renderMarkdown(item.content)"></div>
                  </div>
                  <div class="item-actions">
                    <el-button size="small" text @click.stop="router.push(`/announcements/${item.id}`)">查看详情</el-button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </template>

        <!-- 通知列表 -->
        <template v-else>
          <div class="panel-list" v-show="activeTab === 'unread'">
            <div v-if="notifUnreadItems.length === 0" class="panel-empty">
              <el-icon :size="28" color="var(--text-placeholder)"><Bell /></el-icon>
              <span class="empty-text">暂无未读通知</span>
            </div>
            <div
              v-for="item in notifUnreadItems"
              :key="item.id"
              class="notification-item"
              :class="`level--${item.level}`"
            >
              <span class="item-dot" :class="`dot--${item.level}`"></span>
              <div class="item-body">
                <div class="item-header" @click="expandedId = expandedId === item.id ? '' : item.id">
                  <component :is="notifTypeIcon(item.type)" class="item-category-icon" />
                  <span class="item-title">{{ item.title }}</span>
                  <span class="item-time">{{ relativeTime(item.created_at) }}</span>
                </div>
                <div class="item-preview" v-if="expandedId !== item.id" @click="expandedId = item.id">
                  {{ truncate(markdownToPlainText(item.content), 60) }}
                </div>
                <div v-else class="item-expanded">
                  <div class="item-expanded-body">
                    <div class="item-full-content md-content" v-html="renderMarkdown(item.content)"></div>
                  </div>
                  <div class="item-actions">
                    <el-button size="small" type="primary" @click.stop="handleNotifRead(item.id)">标记已读</el-button>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="panel-list" v-show="activeTab === 'history'">
            <div v-if="notifHistoryItems.length === 0" class="panel-empty">
              <el-icon :size="28" color="var(--text-placeholder)"><Bell /></el-icon>
              <span class="empty-text">暂无已读通知</span>
            </div>
            <div
              v-for="item in notifHistoryItems"
              :key="item.id"
              class="notification-item history-item"
              @click="expandedId = expandedId === item.id ? '' : item.id"
            >
              <div class="item-body">
                <div class="item-header">
                  <component :is="notifTypeIcon(item.type)" class="item-category-icon" />
                  <span class="item-title">{{ item.title }}</span>
                  <span class="item-time">{{ relativeTime(item.created_at) }}</span>
                </div>
                <div v-if="expandedId === item.id" class="item-expanded history-expanded">
                  <div class="item-expanded-body">
                    <div class="item-full-content md-content" v-html="renderMarkdown(item.content)"></div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </el-popover>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { Bell, Setting, StarFilled, WarningFilled, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import {
  getUnreadAnnouncements, getUnreadAnnouncementCount, getReadHistoryAnnouncements, markAnnouncementRead,
  getUnreadNotifications, getUnreadNotificationCount, getNotificationHistory, markNotificationRead, markAllNotificationsRead,
} from '@/services/control-plane'
import { renderMarkdown, markdownToPlainText } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'
import type { Notification } from '@/types/notification'

const router = useRouter()

// ── 公告状态 ────────────────────────────────────────────────────────────────
const annUnreadCount = ref(0)
const annUnreadItems = ref<Announcement[]>([])
const annHistoryItems = ref<Announcement[]>([])

// ── 通知状态 ────────────────────────────────────────────────────────────────
const notifUnreadCount = ref(0)
const notifUnreadItems = ref<Notification[]>([])
const notifHistoryItems = ref<Notification[]>([])

// ── 面板状态 ────────────────────────────────────────────────────────────────
const totalUnread = computed(() => annUnreadCount.value + notifUnreadCount.value)
const currentUnreadCount = computed(() =>
  activeSource.value === 'announcements' ? annUnreadCount.value : notifUnreadCount.value
)
const panelVisible = ref(false)
const activeSource = ref<'announcements' | 'notifications'>('announcements')
const activeTab = ref<'unread' | 'history'>('unread')
const expandedId = ref('')
const bellRef = ref<HTMLElement>()

// ── 图标 ─────────────────────────────────────────────────────────────────────
import type { Component } from 'vue'
const annCategoryIcons: Record<string, Component> = { system: Setting, feature: StarFilled, alert: Bell }
function annCategoryIcon(c: string): Component { return annCategoryIcons[c] || Bell }

const notifTypeIcons: Record<string, Component> = { budget_alert: WarningFilled, expansion_approved: CircleCheckFilled, expansion_rejected: CircleCloseFilled }
function notifTypeIcon(t: string): Component { return notifTypeIcons[t] || Bell }

function truncate(text: string, max: number) { return text.length > max ? text.slice(0, max) + '...' : text }

function relativeTime(dateStr: string | null): string {
  if (!dateStr) return '-'
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return '刚刚'
  if (mins < 60) return `${mins}分钟前`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}小时前`
  const days = Math.floor(hours / 24)
  if (days === 1) return '昨天'
  if (days < 7) return `${days}天前`
  return new Date(dateStr).toLocaleDateString('zh-CN')
}

// ── 数据加载 ──────────────────────────────────────────────────────────────────
async function loadAnnUnread() {
  if (!localStorage.getItem('access_token')) return
  try {
    const [items, count] = await Promise.all([getUnreadAnnouncements(), getUnreadAnnouncementCount()])
    annUnreadItems.value = items || []
    annUnreadCount.value = count || 0
  } catch {}
}

async function loadAnnHistory() {
  if (!localStorage.getItem('access_token')) return
  try { annHistoryItems.value = (await getReadHistoryAnnouncements()) || [] } catch {}
}

async function loadNotifUnread() {
  if (!localStorage.getItem('access_token')) return
  try {
    const [items, count] = await Promise.all([getUnreadNotifications(), getUnreadNotificationCount()])
    notifUnreadItems.value = items || []
    notifUnreadCount.value = count || 0
  } catch {}
}

async function loadNotifHistory() {
  if (!localStorage.getItem('access_token')) return
  try { notifHistoryItems.value = (await getNotificationHistory()) || [] } catch {}
}

// 供外部（App.vue）调用的整体刷新
async function refresh() {
  await Promise.all([loadAnnUnread(), loadNotifUnread()])
}

// ── 操作 ──────────────────────────────────────────────────────────────────────
async function handleAnnRead(id: string) {
  try {
    await markAnnouncementRead(id)
    const item = annUnreadItems.value.find(a => a.id === id)
    annUnreadItems.value = annUnreadItems.value.filter(a => a.id !== id)
    annUnreadCount.value = Math.max(0, annUnreadCount.value - 1)
    if (item) annHistoryItems.value = [item, ...annHistoryItems.value]
    expandedId.value = ''
  } catch {}
}

async function handleNotifRead(id: string) {
  try {
    await markNotificationRead(id)
    const item = notifUnreadItems.value.find(n => n.id === id)
    notifUnreadItems.value = notifUnreadItems.value.filter(n => n.id !== id)
    notifUnreadCount.value = Math.max(0, notifUnreadCount.value - 1)
    if (item) notifHistoryItems.value = [item, ...notifHistoryItems.value]
    expandedId.value = ''
  } catch {}
}

async function handleReadAll() {
  if (activeSource.value === 'announcements') {
    await Promise.all(annUnreadItems.value.map(a => markAnnouncementRead(a.id).catch(() => {})))
    annHistoryItems.value = [...annUnreadItems.value, ...annHistoryItems.value]
    annUnreadItems.value = []
    annUnreadCount.value = 0
  } else {
    await markAllNotificationsRead().catch(() => {})
    notifHistoryItems.value = [...notifUnreadItems.value, ...notifHistoryItems.value]
    notifUnreadItems.value = []
    notifUnreadCount.value = 0
  }
}

function handleViewAll() {
  panelVisible.value = false
  router.push('/announcements')
}

function switchSource(src: 'announcements' | 'notifications') {
  activeSource.value = src
  activeTab.value = 'unread'
  expandedId.value = ''
  if (src === 'notifications' && notifHistoryItems.value.length === 0) loadNotifHistory()
  if (src === 'announcements' && annHistoryItems.value.length === 0) loadAnnHistory()
}

async function togglePanel() {
  if (!panelVisible.value) {
    // 先并行拉取两侧未读数，再决定默认展示哪个 tab
    await Promise.all([loadAnnUnread(), loadNotifUnread()])
    if (notifUnreadCount.value > 0) {
      activeSource.value = 'notifications'
      loadNotifHistory()
    } else {
      activeSource.value = 'announcements'
      loadAnnHistory()
    }
    activeTab.value = 'unread'
  }
  panelVisible.value = !panelVisible.value
}

onMounted(() => {
  refresh()
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})

function handleClickOutside(e: MouseEvent) {
  if (!panelVisible.value) return
  const target = e.target as HTMLElement
  if (bellRef.value && !bellRef.value.contains(target)) {
    const popover = document.querySelector('.el-popover')
    if (popover && !popover.contains(target)) panelVisible.value = false
  }
}

defineExpose({ refresh })
</script>

<style>
.el-popover .item-title { color: var(--text-primary); }
.el-popover .source-tab { color: var(--text-secondary); }
.el-popover .source-tab.active { color: var(--brand-green-deep); }
/* panel-empty / empty-text 必须放在非 scoped 块，因为 el-popover teleport 到 body 后 scoped 属性丢失 */
.el-popover .panel-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 24px 0;
}
.el-popover .empty-text {
  font-size: 12px;
  color: #c0c4cc;
}
</style>

<style scoped>
.notification-bell {
  cursor: pointer;
  color: var(--el-text-color-secondary);
  padding: 4px;
  display: flex;
  align-items: center;
  height: 100%;
}
.notification-bell:hover { color: var(--el-text-color-primary); }
.notification-bell :deep(.el-badge) { display: flex; align-items: center; }

/* ── 面板整体 ─────────────────── */
.notification-panel { display: flex; flex-direction: column; gap: 0; }

/* ── 标题行：来源 tab + 操作按钮 ── */
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 0 0;
  margin-bottom: 0;
}

.panel-source-tabs {
  display: flex;
  gap: 2px;
  background: rgba(0,0,0,0.04);
  border-radius: 8px;
  padding: 3px;
}

.source-tab {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 14px;
  border: none;
  background: none;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-placeholder);
  cursor: pointer;
  transition: all 0.15s;
  white-space: nowrap;
}
.source-tab.active {
  background: var(--bg-surface);
  color: var(--brand-green-deep);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0,0,0,0.1);
}
.source-tab:hover:not(.active) { color: var(--text-secondary); }

.source-badge {
  background: var(--brand-green);
  color: var(--bg-surface);
  font-size: 10px;
  font-weight: 700;
  line-height: 1;
  padding: 2px 5px;
  border-radius: 8px;
  min-width: 16px;
  text-align: center;
}
.source-badge--warn { background: var(--color-warning); }

.panel-header-actions { display: flex; gap: 2px; flex-shrink: 0; }

/* ── 未读/已读 pill ───────────── */
.panel-tab-pill {
  display: flex;
  gap: 0;
  border-bottom: 1px solid rgba(0,0,0,0.07);
  margin-top: 10px;
}
.pill-btn {
  flex: 1;
  padding: 7px 0;
  border: none;
  background: none;
  font-size: 12px;
  color: var(--text-placeholder);
  cursor: pointer;
  position: relative;
  transition: color 0.15s;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
}
.pill-btn.active {
  color: var(--brand-green-deep);
  font-weight: 600;
}
.pill-btn.active::after {
  content: '';
  position: absolute;
  bottom: -1px;
  left: 20%;
  right: 20%;
  height: 2px;
  background: var(--brand-green-deep);
  border-radius: 1px;
}
.pill-btn:hover:not(.active) { color: var(--text-secondary); }

.pill-count {
  background: rgba(54,179,78,0.15);
  color: var(--brand-green-deep);
  font-size: 10px;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 8px;
  min-width: 16px;
  text-align: center;
}

/* ── 列表 ────────────────────── */
.panel-list {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  max-height: 340px;
}
.panel-list::-webkit-scrollbar { width: 3px; }
.panel-list::-webkit-scrollbar-thumb { background: rgba(54,179,78,0.2); border-radius: 2px; }

/* ── 条目 ────────────────────── */
.notification-item {
  display: flex;
  gap: 10px;
  padding: 11px 2px;
  border-bottom: 1px solid rgba(0,0,0,0.05);
  cursor: pointer;
  transition: background 0.15s;
}
.notification-item:last-child { border-bottom: none; }
.notification-item:hover { background: rgba(54,179,78,0.04); border-radius: 6px; }
.notification-item.history-item { opacity: 0.65; }

.item-dot {
  width: 7px; height: 7px; border-radius: 50%;
  flex-shrink: 0; margin-top: 5px;
}
.dot--info     { background: var(--brand-green); box-shadow: 0 0 0 2px rgba(54,179,78,0.15); }
.dot--warning  { background: var(--color-warning); box-shadow: 0 0 0 2px rgba(245,124,0,0.15); }
.dot--critical { background: var(--color-danger); box-shadow: 0 0 0 2px rgba(211,47,47,0.15); }

.item-body { flex: 1; min-width: 0; }
.item-header { display: flex; align-items: center; gap: 6px; cursor: pointer; }
.item-category-icon { font-size: 12px; flex-shrink: 0; }
.item-title {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
  flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.item-time { font-size: 12px; color: var(--text-placeholder); flex-shrink: 0; }

.item-preview {
  font-size: 12px; color: var(--text-secondary);
  margin-top: 3px; line-height: 1.5;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  cursor: pointer;
}

.item-expanded {
  margin-top: 8px; border-radius: 10px; overflow: hidden;
  background: rgba(255,255,255,0.7); backdrop-filter: blur(16px);
  border: 1px solid rgba(54,179,78,0.18);
  box-shadow: 0 2px 12px rgba(54,179,78,0.08);
}
.item-expanded.history-expanded {
  border-color: rgba(0,0,0,0.07); box-shadow: none; background: rgba(255,255,255,0.5);
}
.item-expanded-body {
  padding: 10px 12px; max-height: 180px; overflow-y: auto;
}
.item-expanded-body::-webkit-scrollbar { width: 3px; }
.item-expanded-body::-webkit-scrollbar-thumb { background: rgba(54,179,78,0.2); border-radius: 2px; }

.item-full-content { font-size: 12px; color: var(--text-regular); line-height: 1.7; }
.item-full-content :deep(p)  { margin: 0 0 0.4em; }
.item-full-content :deep(strong) { font-weight: 700; color: var(--text-primary); }
.item-full-content :deep(code) { background: rgba(0,0,0,0.06); padding: 1px 4px; border-radius: 3px; font-size: 12px; }
.item-full-content :deep(h1),
.item-full-content :deep(h2),
.item-full-content :deep(h3) { color: var(--text-primary); margin: 0.5em 0 0.25em; font-size: 13px; }
.item-full-content :deep(ul),
.item-full-content :deep(ol) { padding-left: 1.2em; margin: 0.25em 0; }

.item-actions {
  display: flex; justify-content: flex-end; align-items: center; gap: 4px;
  padding: 5px 8px; border-top: 1px solid rgba(54,179,78,0.08);
}

.panel-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 0;
}
.empty-text {
  font-size: 12px;
  color: var(--text-placeholder);
}
</style>
