<template>
  <div class="notification-bell" @click="togglePanel" ref="bellRef">
    <el-badge :value="unreadCount" :hidden="unreadCount === 0" :max="99">
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
        <div class="panel-header">
          <span class="panel-title">站内通知</span>
          <div class="panel-header-actions">
            <el-button v-if="unreadCount > 0" text size="small" type="primary" @click="handleReadAll">全部已读</el-button>
            <el-button text size="small" @click="handleViewAll">查看全部</el-button>
          </div>
        </div>
        <div class="panel-tab-bar">
          <button
            class="tab-btn"
            :class="{ active: activeTab === 'unread' }"
            @click="activeTab = 'unread'"
          >未读 ({{ unreadCount }})</button>
          <button
            class="tab-btn"
            :class="{ active: activeTab === 'history' }"
            @click="activeTab = 'history'"
          >已读</button>
        </div>
        <div class="panel-list" v-show="activeTab === 'unread'">
          <div v-if="unreadItems.length === 0" class="panel-empty">
            <el-empty description="暂无未读通知" :image-size="50" />
          </div>
          <div
            v-for="item in unreadItems"
            :key="item.id"
            class="notification-item"
            :class="`level--${item.level}`"
          >
            <span class="item-dot" :class="`dot--${item.level}`"></span>
            <div class="item-body">
              <div class="item-header" @click="expandedId = expandedId === item.id ? '' : item.id">
                <span class="item-category-icon">{{ categoryIcon(item.category) }}</span>
                <span class="item-title">{{ item.title }}</span>
                <span class="item-time">{{ relativeTime(item.published_at) }}</span>
              </div>
              <div class="item-preview" v-if="expandedId !== item.id" @click="expandedId = item.id">
                {{ truncate(markdownToPlainText(item.content), 60) }}
              </div>
              <div v-else class="item-expanded">
                <div class="item-expanded-header">
                  <span class="item-category-icon">{{ categoryIcon(item.category) }}</span>
                  <span class="item-expanded-title">{{ item.title }}</span>
                  <span class="item-time">{{ relativeTime(item.published_at) }}</span>
                </div>
                <div class="item-expanded-body">
                  <div class="item-full-content md-content" v-html="renderMarkdown(item.content)"></div>
                </div>
                <div class="item-actions">
                  <el-button size="small" text @click.stop="router.push(`/announcements/${item.id}`)">
                    查看详情
                  </el-button>
                  <el-button size="small" type="primary" @click.stop="handleRead(item.id)">
                    标记已读
                  </el-button>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="panel-list" v-show="activeTab === 'history'">
          <div v-if="historyItems.length === 0" class="panel-empty">
            <el-empty description="暂无已读记录" :image-size="50" />
          </div>
          <div
            v-for="item in historyItems"
            :key="item.id"
            class="notification-item history-item"
            @click="expandedId = expandedId === item.id ? '' : item.id"
          >
            <div class="item-body">
              <div class="item-header">
                <span class="item-category-icon">{{ categoryIcon(item.category) }}</span>
                <span class="item-title">{{ item.title }}</span>
                <span class="item-time">{{ relativeTime(item.published_at) }}</span>
              </div>
              <div v-if="expandedId === item.id" class="item-expanded history-expanded">
                <div class="item-expanded-body">
                  <div class="item-full-content md-content" v-html="renderMarkdown(item.content)"></div>
                </div>
                <div class="item-actions">
                  <el-button size="small" text @click.stop="router.push(`/announcements/${item.id}`)">
                    查看详情
                  </el-button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </el-popover>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { Bell } from '@element-plus/icons-vue'
import { getUnreadAnnouncements, getUnreadAnnouncementCount, getReadHistoryAnnouncements, markAnnouncementRead } from '@/services/control-plane'
import { renderMarkdown, markdownToPlainText } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

const router = useRouter()
const unreadCount = ref(0)
const unreadItems = ref<Announcement[]>([])
const historyItems = ref<Announcement[]>([])
const panelVisible = ref(false)
const activeTab = ref('unread')
const expandedId = ref('')
const bellRef = ref<HTMLElement>()

const categoryIcons: Record<string, string> = { system: '🔧', feature: '✨', alert: '📣' }
function categoryIcon(c: string) { return categoryIcons[c] || '📢' }
function truncate(text: string, max: number) { return text.length > max ? text.slice(0, max) + '...' : text }

function relativeTime(dateStr: string | null): string {
  if (!dateStr) return '-'
  const now = Date.now()
  const then = new Date(dateStr).getTime()
  const diff = now - then
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

async function loadUnread() {
  try {
    const [items, count] = await Promise.all([getUnreadAnnouncements(), getUnreadAnnouncementCount()])
    unreadItems.value = items || []
    unreadCount.value = count || 0
  } catch {}
}

async function loadHistory() {
  try {
    const data = await getReadHistoryAnnouncements()
    historyItems.value = data || []
  } catch {}
}

async function handleRead(id: string) {
  try {
    await markAnnouncementRead(id)
    const item = unreadItems.value.find(a => a.id === id)
    unreadItems.value = unreadItems.value.filter(a => a.id !== id)
    unreadCount.value = Math.max(0, unreadCount.value - 1)
    if (item) historyItems.value = [item, ...historyItems.value]
    expandedId.value = ''
  } catch {}
}

async function handleReadAll() {
  await Promise.all(unreadItems.value.map(a => markAnnouncementRead(a.id).catch(() => {})))
  historyItems.value = [...unreadItems.value, ...historyItems.value]
  unreadItems.value = []
  unreadCount.value = 0
}

function handleViewAll() {
  panelVisible.value = false
  router.push('/announcements')
}

function togglePanel() {
  if (!panelVisible.value) {
    loadUnread()
    loadHistory()
  }
  panelVisible.value = !panelVisible.value
}

onMounted(() => {
  loadUnread()
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
    if (popover && !popover.contains(target)) {
      panelVisible.value = false
    }
  }
}

defineExpose({ refresh: loadUnread })
</script>

<style scoped>
.notification-bell {
  cursor: pointer;
  color: var(--el-text-color-secondary);
  padding: 4px;
  display: flex;
  align-items: center;
}
.notification-bell:hover { color: var(--el-text-color-primary); }

/* ── 面板整体 ────────────── */
.notification-panel { display: flex; flex-direction: column; }

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 0 10px;
  border-bottom: 1px solid rgba(0,0,0,0.06);
}

.panel-title { font-size: 15px; font-weight: 700; color: #1a1a1a; }
.panel-header-actions { display: flex; gap: 2px; }

/* ── Tab ─────────────────── */
.panel-tab-bar {
  display: flex;
  gap: 0;
  border-bottom: 1px solid rgba(0,0,0,0.06);
  margin-bottom: 6px;
}

.tab-btn {
  flex: 1; padding: 8px 0;
  border: none; background: none;
  font-size: 13px; color: #94a3b8;
  cursor: pointer; position: relative; transition: color 0.15s;
}
.tab-btn.active { color: #1E8C86; font-weight: 600; }
.tab-btn.active::after {
  content: '';
  position: absolute; bottom: -1px; left: 25%; right: 25%;
  height: 2px; background: #1E8C86; border-radius: 1px;
}
.tab-btn:hover { color: #1E8C86; }

/* ── 列表 ────────────────── */
.panel-list {
  flex: 1; overflow-y: auto;
  display: flex; flex-direction: column;
  max-height: 400px;
}
.panel-list::-webkit-scrollbar { width: 3px; }
.panel-list::-webkit-scrollbar-thumb { background: rgba(43,168,162,0.2); border-radius: 2px; }

/* ── 未读条目 ─────────────── */
.notification-item {
  display: flex; gap: 10px;
  padding: 12px 4px;
  border-bottom: 1px solid rgba(0,0,0,0.05);
  cursor: pointer;
  transition: background 0.15s;
  border-radius: 0;
}
.notification-item:last-child { border-bottom: none; }
.notification-item:hover { background: rgba(43,168,162,0.04); }
.notification-item.history-item { opacity: 0.7; }

.item-dot {
  width: 7px; height: 7px;
  border-radius: 50%; flex-shrink: 0;
  margin-top: 5px;
}
.dot--info     { background: #2BA8A2; box-shadow: 0 0 0 2px rgba(43,168,162,0.15); }
.dot--warning  { background: #F57C00; box-shadow: 0 0 0 2px rgba(245,124,0,0.15); }
.dot--critical { background: #D32F2F; box-shadow: 0 0 0 2px rgba(211,47,47,0.15); }

.item-body { flex: 1; min-width: 0; }

.item-header {
  display: flex; align-items: center; gap: 6px;
  cursor: pointer;
}
.item-category-icon { font-size: 12px; flex-shrink: 0; }
.item-title {
  font-size: 13px; font-weight: 600; color: #1a1a1a;
  flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.item-time { font-size: 11px; color: #94a3b8; flex-shrink: 0; }

.item-preview {
  font-size: 12px; color: #64748b;
  margin-top: 3px; line-height: 1.5;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  cursor: pointer;
}

/* ── 展开详情 ─────────────── */
.item-expanded {
  margin-top: 8px;
  border-radius: 10px;
  overflow: hidden;
  background: rgba(255,255,255,0.7);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border: 1px solid rgba(43,168,162,0.18);
  box-shadow: 0 2px 12px rgba(43,168,162,0.08);
}
.item-expanded.history-expanded {
  border-color: rgba(0,0,0,0.07);
  box-shadow: none;
  background: rgba(255,255,255,0.5);
}

.item-expanded-header {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 12px;
  background: rgba(43,168,162,0.06);
  border-bottom: 1px solid rgba(43,168,162,0.1);
}
.item-expanded-title {
  font-size: 12px; font-weight: 700; color: #1a1a1a;
  flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.item-expanded-body {
  padding: 10px 12px;
  max-height: 180px; overflow-y: auto;
}
.item-expanded-body::-webkit-scrollbar { width: 3px; }
.item-expanded-body::-webkit-scrollbar-thumb { background: rgba(43,168,162,0.2); border-radius: 2px; }

.item-full-content { font-size: 12px; color: #334155; line-height: 1.7; }
.item-full-content :deep(p)  { margin: 0 0 0.4em; }
.item-full-content :deep(strong) { font-weight: 700; color: #1a1a1a; }
.item-full-content :deep(code) { background: rgba(0,0,0,0.06); padding: 1px 4px; border-radius: 3px; font-size: 11px; }
.item-full-content :deep(h1), .item-full-content :deep(h2), .item-full-content :deep(h3) { color: #1a1a1a; margin: 0.5em 0 0.25em; font-size: 13px; }
.item-full-content :deep(ul), .item-full-content :deep(ol) { padding-left: 1.2em; margin: 0.25em 0; }

.item-actions {
  display: flex; justify-content: flex-end; align-items: center; gap: 4px;
  padding: 5px 8px;
  border-top: 1px solid rgba(43,168,162,0.08);
}

.panel-empty { padding: 20px 0; }
</style>