<template>
  <Teleport to="body">
    <Transition name="popup">
      <div v-if="visible && items.length" class="overlay" @click.self="emit('close')">
        <div class="panel">

          <div class="panel-header">
            <div class="header-dot"></div>
            <span class="header-title">{{ items.length }} 条未读通知</span>
            <button class="header-close" @click="emit('close')">
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
                <path d="M1 1l12 12M13 1L1 13" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>
              </svg>
            </button>
          </div>

          <div class="panel-items">
            <div
              v-for="item in items"
              :key="item.id"
              class="item"
              @click="goDetail(item.id)"
            >
              <div class="item-left">
                <span class="item-icon">{{ levelIcon(item.level) }}</span>
              </div>
              <div class="item-content">
                <div class="item-title">{{ item.title }}</div>
                <div class="item-preview">{{ truncate(markdownToPlainText(item.content), 72) }}</div>
              </div>
              <div class="item-meta">
                <span class="item-time">{{ formatTime(item.published_at) }}</span>
                <svg class="item-arrow" width="14" height="14" viewBox="0 0 14 14" fill="none">
                  <path d="M3 7h8M8 4l3 3-3 3" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
              </div>
            </div>
          </div>

          <div class="panel-footer">
            <button class="btn-ghost" @click="emit('close')">稍后再看</button>
            <button class="btn-solid" @click="handleReadAll">全部已读</button>
          </div>

        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getUnreadAnnouncements, markAnnouncementRead } from '@/services/control-plane'
import { markdownToPlainText } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; 'all-read': [] }>()

const router = useRouter()
const items = ref<Announcement[]>([])

const levelIcons: Record<string, string> = { info: '📢', warning: '⚠️', critical: '🚨' }
function levelIcon(l: string) { return levelIcons[l] || '📢' }
function formatTime(t: string | null) { return t ? new Date(t).toLocaleDateString('zh-CN') : '' }
function truncate(t: string, n: number) { return t.length > n ? t.slice(0, n) + '...' : t }

function goDetail(id: string) {
  emit('close')
  router.push(`/announcements/${id}`)
}

async function handleReadAll() {
  await Promise.all(items.value.map(a => markAnnouncementRead(a.id).catch(() => {})))
  emit('all-read')
}

onMounted(async () => {
  if (!localStorage.getItem('access_token')) return
  try {
    const all = await getUnreadAnnouncements()
    items.value = (all || []).filter(a => a.display_mode === 'popup')
  } catch {}
})
</script>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  z-index: 2000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0,0,0,0.28);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  padding: 20px;
}

.panel {
  width: 100%;
  max-width: 560px;
  max-height: 90vh;
  background: var(--bg-surface);
  border-radius: 20px;
  box-shadow: 0 24px 64px rgba(0,0,0,0.14), 0 0 0 1px rgba(0,0,0,0.06);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* ── 头部 ──────────── */
.panel-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 20px 22px;
  border-bottom: 1px solid var(--bg-subtle);
}

.header-dot {
  width: 8px; height: 8px;
  border-radius: 50%;
  background: var(--brand-green);
  flex-shrink: 0;
  box-shadow: 0 0 0 3px rgba(54,179,78,0.15);
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { box-shadow: 0 0 0 3px rgba(54,179,78,0.15); }
  50%       { box-shadow: 0 0 0 5px rgba(54,179,78,0.08); }
}

.header-title {
  flex: 1;
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}

.header-close {
  width: 26px; height: 26px;
  border: none;
  background: none;
  border-radius: 6px;
  color: var(--text-placeholder);
  cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: all 0.15s;
  flex-shrink: 0;
}
.header-close:hover { background: var(--bg-subtle); color: var(--text-secondary); }

/* ── 列表 ──────────── */
.panel-items {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
}
.panel-items::-webkit-scrollbar { width: 3px; }
.panel-items::-webkit-scrollbar-thumb { background: var(--border-default); border-radius: 2px; }

.item {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 22px;
  cursor: pointer;
  transition: background 0.12s;
  border-bottom: 1px solid #f8fafc;
}
.item:last-child { border-bottom: none; }
.item:hover { background: var(--el-color-primary-light-9); }

.item-left { flex-shrink: 0; }
.item-icon { font-size: 22px; line-height: 1; }

.item-content { flex: 1; min-width: 0; }

.item-title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1.4;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-preview {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.5;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  flex-shrink: 0;
}

.item-time { font-size: 12px; color: var(--text-placeholder); white-space: nowrap; }

.item-arrow { color: var(--border-strong); transition: all 0.15s; }
.item:hover .item-arrow { color: var(--brand-green); transform: translateX(2px); }

/* ── 底部 ──────────── */
.panel-footer {
  display: flex;
  gap: 10px;
  padding: 16px 20px;
  border-top: 1px solid var(--bg-subtle);
  background: var(--bg-page);
}

.btn-ghost, .btn-solid {
  flex: 1;
  padding: 11px 0;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  border: none;
  transition: all 0.15s;
}

.btn-ghost {
  background: var(--bg-subtle);
  color: var(--text-secondary);
}
.btn-ghost:hover { background: var(--border-default); }

.btn-solid {
  background: var(--brand-green-deep);
  color: var(--bg-surface);
  box-shadow: 0 2px 6px rgba(31,138,59,0.25);
}
.btn-solid:hover { background: var(--brand-green-deep); box-shadow: 0 3px 10px rgba(31,138,59,0.35); }

/* ── 动画 ──────────── */
.popup-enter-active { transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1); }
.popup-leave-active { transition: all 0.18s ease; }
.popup-enter-from   { opacity: 0; transform: translateY(12px) scale(0.97); }
.popup-leave-to     { opacity: 0; transform: translateY(6px) scale(0.98); }

@media (max-width: 768px) {
  .panel {
    max-width: 95vw;
    border-radius: 16px;
  }
  .item {
    gap: 10px;
    padding: 14px 16px;
  }
  .item-meta {
    display: none;
  }
  .panel-header {
    padding: 16px;
  }
  .panel-footer {
    padding: 14px 16px;
  }
}
</style>