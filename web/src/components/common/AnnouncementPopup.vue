<template>
  <el-dialog
    :model-value="visible"
    :show-close="false"
    :close-on-click-modal="false"
    width="min(620px, 92vw)"
    align-center
    class="announcement-popup-dialog"
    @close="emit('close')"
  >
    <template #header>
      <div class="popup-header">
        <div class="popup-header-bg"></div>
        <div class="popup-header-content">
          <div class="popup-header-left">
            <div class="popup-badge">
              <span class="popup-badge-dot"></span>
              <span>{{ items.length }} 条未读通知</span>
            </div>
            <div class="popup-header-title">📬 系统通知</div>
            <div class="popup-header-sub">点击查看完整公告内容</div>
          </div>
          <button type="button" class="popup-close-btn" @click="emit('close')">
            <el-icon :size="14"><Close /></el-icon>
            <span>稍后再看</span>
          </button>
        </div>
      </div>
    </template>

    <div class="popup-body">
      <div
        v-for="(item, i) in items"
        :key="item.id"
        class="popup-card"
        :class="`level--${item.level}`"
        @click="goDetail(item.id)"
      >
        <div class="popup-card-num">{{ String(i + 1).padStart(2, '0') }}</div>
        <div class="popup-card-main">
          <div class="popup-card-meta">
            <span class="popup-tag" :class="`cat--${item.category}`">{{ categoryLabel(item.category) }}</span>
            <span class="popup-level-dot" :class="`dot--${item.level}`"></span>
            <span class="popup-level-text" :class="`lv-text--${item.level}`">{{ levelLabel(item.level) }}</span>
            <span class="popup-card-time">{{ formatTime(item.published_at) }}</span>
          </div>
          <div class="popup-card-title">{{ item.title }}</div>
          <div class="popup-card-preview">{{ truncate(markdownToPlainText(item.content), 90) }}</div>
        </div>
        <div class="popup-card-arrow">→</div>
      </div>
    </div>

    <template #footer>
      <div class="popup-footer">
        <button type="button" class="footer-btn-later" @click="emit('close')">
          <el-icon :size="13"><Clock /></el-icon>
          稍后提醒
        </button>
        <button type="button" class="footer-btn-read" @click="handleReadAll">
          <el-icon :size="13"><Check /></el-icon>
          全部已读
        </button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Close, Clock, Check } from '@element-plus/icons-vue'
import { getUnreadAnnouncements, markAnnouncementRead } from '@/services/control-plane'
import { markdownToPlainText } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; 'all-read': [] }>()

const router = useRouter()
const items = ref<Announcement[]>([])

const categoryLabels: Record<string, string> = { system: '系统公告', feature: '功能更新', alert: '重要通知' }
const levelLabels: Record<string, string> = { info: '普通', warning: '重要', critical: '紧急' }

function categoryLabel(c: string) { return categoryLabels[c] || c }
function levelLabel(l: string) { return levelLabels[l] || l }
function formatTime(t: string | null) { return t ? new Date(t).toLocaleDateString('zh-CN') : '-' }
function truncate(text: string, max: number) { return text.length > max ? text.slice(0, max) + '...' : text }

function goDetail(id: string) {
  emit('close')
  router.push(`/announcements/${id}`)
}

async function handleReadAll() {
  await Promise.all(items.value.map(a => markAnnouncementRead(a.id).catch(() => {})))
  emit('all-read')
}

onMounted(async () => {
  try {
    const all = await getUnreadAnnouncements()
    items.value = (all || []).filter(a => a.display_mode === 'popup')
  } catch {}
})
</script>

<style>
.announcement-popup-dialog .el-dialog__header  { padding: 0 !important; }
.announcement-popup-dialog .el-dialog__body    { padding: 0 !important; }
.announcement-popup-dialog .el-dialog__footer  { padding: 0 !important; }
.announcement-popup-dialog {
  border-radius: 20px !important;
  overflow: hidden;
  background: rgba(255,255,255,0.65) !important;
  backdrop-filter: blur(24px) !important;
  -webkit-backdrop-filter: blur(24px) !important;
  border: 1px solid rgba(255,255,255,0.5) !important;
  box-shadow: 0 20px 60px rgba(0,0,0,0.12), 0 1px 0 rgba(255,255,255,0.8) inset !important;
}
</style>

<style scoped>
/* ── 头部 ─────────────────────────────── */
.popup-header {
  position: relative;
  overflow: hidden;
  background: linear-gradient(135deg, rgba(15,107,102,0.85) 0%, rgba(30,140,134,0.8) 50%, rgba(47,191,184,0.75) 100%);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
}

.popup-header-bg::before {
  content: '';
  position: absolute;
  width: 180px; height: 180px;
  border-radius: 50%;
  background: rgba(255,255,255,0.07);
  top: -50px; right: -30px;
}
.popup-header-bg::after {
  content: '';
  position: absolute;
  width: 100px; height: 100px;
  border-radius: 50%;
  background: rgba(255,255,255,0.05);
  bottom: -30px; left: 30px;
}

.popup-header-content {
  position: relative;
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  padding: 22px 20px 18px;
  gap: 12px;
}

.popup-header-left { display: flex; flex-direction: column; gap: 4px; }

.popup-badge {
  display: inline-flex; align-items: center; gap: 6px;
  background: rgba(255,255,255,0.15);
  border: 1px solid rgba(255,255,255,0.2);
  padding: 3px 10px; border-radius: 20px;
  font-size: 11px; font-weight: 600;
  color: rgba(255,255,255,0.9);
  width: fit-content; margin-bottom: 2px;
}

.popup-badge-dot {
  width: 6px; height: 6px; border-radius: 50%; background: #fff;
  animation: pulse 1.8s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.5; transform: scale(0.75); }
}

.popup-header-title { font-size: 20px; font-weight: 800; color: #fff; line-height: 1.2; }
.popup-header-sub   { font-size: 12px; color: rgba(255,255,255,0.55); }

.popup-close-btn {
  display: flex; align-items: center; gap: 5px;
  padding: 7px 12px;
  border: 1px solid rgba(255,255,255,0.2);
  background: rgba(255,255,255,0.1);
  backdrop-filter: blur(8px);
  border-radius: 10px;
  color: rgba(255,255,255,0.8);
  font-size: 12px; cursor: pointer;
  transition: all 0.15s; flex-shrink: 0; white-space: nowrap;
}
.popup-close-btn:hover { background: rgba(255,255,255,0.22); color: #fff; }

/* ── 列表 ─────────────────────────────── */
.popup-body {
  display: flex; flex-direction: column;
  max-height: 380px; overflow-y: auto;
  background: transparent;
}

.popup-body::-webkit-scrollbar { width: 3px; }
.popup-body::-webkit-scrollbar-thumb { background: rgba(43,168,162,0.2); border-radius: 2px; }

.popup-card {
  display: flex; align-items: center; gap: 0;
  background: transparent;
  cursor: pointer;
  transition: background 0.15s;
  border-bottom: 1px solid rgba(0,0,0,0.06);
  position: relative;
}
.popup-card:last-child { border-bottom: none; }
.popup-card:hover { background: rgba(255,255,255,0.4); }

.popup-card::before {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0; width: 3px;
  border-radius: 0 2px 2px 0;
  opacity: 0; transition: opacity 0.15s;
}
.popup-card { position: relative; }
.popup-card.level--info::before    { background: #2BA8A2; }
.popup-card.level--warning::before { background: #F57C00; }
.popup-card.level--critical::before{ background: #D32F2F; }
.popup-card:hover::before { opacity: 1; }

.popup-card-num {
  width: 48px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 18px; font-weight: 900;
  color: rgba(0,0,0,0.1);
  font-family: Georgia, serif;
  padding: 18px 0; user-select: none;
}

.popup-card-main {
  flex: 1; min-width: 0;
  padding: 14px 14px 14px 0;
}

.popup-card-meta {
  display: flex; align-items: center; gap: 6px;
  margin-bottom: 6px; flex-wrap: wrap;
}

.popup-tag {
  padding: 2px 7px; border-radius: 4px;
  font-size: 11px; font-weight: 600;
}
.cat--system  { background: rgba(43,168,162,0.12); color: #1E8C86; }
.cat--feature { background: rgba(94,53,177,0.1);   color: #5E35B1; }
.cat--alert   { background: rgba(230,81,0,0.1);    color: #E65100; }

.popup-level-dot { width: 5px; height: 5px; border-radius: 50%; }
.dot--info     { background: #2BA8A2; }
.dot--warning  { background: #F57C00; }
.dot--critical { background: #D32F2F; }

.popup-level-text { font-size: 11px; font-weight: 600; }
.lv-text--info     { color: #2BA8A2; }
.lv-text--warning  { color: #F57C00; }
.lv-text--critical { color: #D32F2F; }

.popup-card-time { font-size: 11px; color: rgba(0,0,0,0.3); margin-left: auto; }

.popup-card-title {
  font-size: 14px; font-weight: 700;
  color: #1a1a1a; line-height: 1.45; margin-bottom: 4px;
}

.popup-card-preview {
  font-size: 12px; color: #64748b; line-height: 1.6;
}

.popup-card-arrow {
  padding: 0 14px 0 6px;
  font-size: 13px; color: rgba(0,0,0,0.15);
  flex-shrink: 0; transition: all 0.15s;
}
.popup-card:hover .popup-card-arrow { color: #2BA8A2; transform: translateX(3px); }

/* ── 底部 ─────────────────────────────── */
.popup-footer {
  display: flex; align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background: transparent;
  gap: 8px;
}

.footer-btn-later,
.footer-btn-read {
  display: flex; align-items: center; gap: 6px;
  padding: 9px 0; border-radius: 12px;
  font-size: 13px; font-weight: 500;
  cursor: pointer; transition: all 0.15s;
  border: none; flex: 1; justify-content: center;
}

.footer-btn-later {
  background: rgba(0,0,0,0.05);
  color: #64748b;
  border: 1px solid rgba(0,0,0,0.07);
}
.footer-btn-later:hover { background: rgba(0,0,0,0.09); color: #475569; }

.footer-btn-read {
  background: linear-gradient(135deg, #1E8C86, #2BA8A2);
  color: #fff;
  box-shadow: 0 2px 10px rgba(43,168,162,0.3);
}
.footer-btn-read:hover {
  background: linear-gradient(135deg, #177a75, #239e98);
  box-shadow: 0 4px 14px rgba(43,168,162,0.4);
}
</style>

<style scoped>
/* ── 头部 ─────────────────────────────── */
.popup-header {
  position: relative;
  overflow: hidden;
}

.popup-header-bg {
  position: absolute; inset: 0;
  background: linear-gradient(135deg, #0f6b66 0%, #1E8C86 40%, #2fbfb8 100%);
}

/* 装饰圆圈 */
.popup-header-bg::before {
  content: '';
  position: absolute;
  width: 200px; height: 200px;
  border-radius: 50%;
  background: rgba(255,255,255,0.06);
  top: -60px; right: -40px;
}
.popup-header-bg::after {
  content: '';
  position: absolute;
  width: 120px; height: 120px;
  border-radius: 50%;
  background: rgba(255,255,255,0.05);
  bottom: -40px; left: 20px;
}

.popup-header-content {
  position: relative;
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  padding: 24px 22px 20px;
  gap: 12px;
}

.popup-header-left { display: flex; flex-direction: column; gap: 4px; }

.popup-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: rgba(255,255,255,0.15);
  backdrop-filter: blur(8px);
  border: 1px solid rgba(255,255,255,0.2);
  padding: 3px 10px;
  border-radius: 20px;
  font-size: 11px;
  font-weight: 600;
  color: rgba(255,255,255,0.9);
  width: fit-content;
  margin-bottom: 2px;
}

.popup-badge-dot {
  width: 6px; height: 6px;
  border-radius: 50%;
  background: #fff;
  animation: pulse 1.8s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50%       { opacity: 0.5; transform: scale(0.8); }
}

.popup-header-title {
  font-size: 22px; font-weight: 800;
  color: #fff; line-height: 1.2;
  letter-spacing: -0.3px;
}

.popup-header-sub {
  font-size: 12px;
  color: rgba(255,255,255,0.6);
}

.popup-close-btn {
  display: flex; align-items: center; gap: 5px;
  padding: 7px 13px;
  border: 1px solid rgba(255,255,255,0.25);
  background: rgba(255,255,255,0.1);
  backdrop-filter: blur(8px);
  border-radius: 10px;
  color: rgba(255,255,255,0.85);
  font-size: 12px; cursor: pointer;
  transition: all 0.15s; flex-shrink: 0;
  white-space: nowrap;
}
.popup-close-btn:hover {
  background: rgba(255,255,255,0.2);
  color: #fff;
}

/* ── 列表 ─────────────────────────────── */
.popup-body {
  display: flex; flex-direction: column;
  max-height: 380px; overflow-y: auto;
  background: #f8f9fa;
}

.popup-body::-webkit-scrollbar { width: 4px; }
.popup-body::-webkit-scrollbar-thumb { background: #d1e9e7; border-radius: 2px; }

.popup-card {
  display: flex; align-items: center; gap: 0;
  background: #fff;
  cursor: pointer;
  transition: all 0.15s;
  border-bottom: 1px solid #f0f4f4;
  position: relative;
}
.popup-card:last-child { border-bottom: none; }

.popup-card::before {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  width: 3px;
  border-radius: 0 2px 2px 0;
  opacity: 0;
  transition: opacity 0.15s;
}
.popup-card.level--info::before    { background: #2BA8A2; }
.popup-card.level--warning::before { background: #F57C00; }
.popup-card.level--critical::before{ background: #D32F2F; }

.popup-card:hover { background: #f8fffe; }
.popup-card:hover::before { opacity: 1; }

.popup-card-num {
  width: 52px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 20px; font-weight: 900;
  color: #e2e8f0;
  font-family: 'Georgia', serif;
  padding: 20px 0;
  user-select: none;
}

.popup-card-main {
  flex: 1; min-width: 0;
  padding: 16px 16px 16px 0;
}

.popup-card-meta {
  display: flex; align-items: center; gap: 6px;
  margin-bottom: 7px; flex-wrap: wrap;
}

.popup-tag {
  padding: 2px 8px; border-radius: 4px;
  font-size: 11px; font-weight: 600;
}
.cat--system  { background: #e8f6f5; color: #1E8C86; }
.cat--feature { background: #ede7f6; color: #5E35B1; }
.cat--alert   { background: #fff3e0; color: #E65100; }

.popup-level-dot {
  width: 5px; height: 5px; border-radius: 50%;
}
.dot--info     { background: #2BA8A2; }
.dot--warning  { background: #F57C00; }
.dot--critical { background: #D32F2F; }

.popup-level-text {
  font-size: 11px; font-weight: 600;
}
.lv-text--info     { color: #2BA8A2; }
.lv-text--warning  { color: #F57C00; }
.lv-text--critical { color: #D32F2F; }

.popup-card-time {
  font-size: 11px; color: #94a3b8; margin-left: auto;
}

.popup-card-title {
  font-size: 14px; font-weight: 700;
  color: #1a1a1a; line-height: 1.45;
  margin-bottom: 5px;
}

.popup-card-preview {
  font-size: 12px; color: #64748b; line-height: 1.6;
}

.popup-card-arrow {
  padding: 0 16px 0 8px;
  font-size: 14px; color: #cbd5e1;
  flex-shrink: 0; transition: all 0.15s;
}
.popup-card:hover .popup-card-arrow {
  color: #2BA8A2; transform: translateX(3px);
}

/* ── 底部 ─────────────────────────────── */
.popup-footer {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 20px;
  background: #fff;
  border-top: 1px solid #f0f4f4;
  gap: 10px;
}

.footer-btn-later,
.footer-btn-read {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 18px;
  border-radius: 10px;
  font-size: 13px; font-weight: 500;
  cursor: pointer; transition: all 0.15s;
  border: none;
}

.footer-btn-later {
  background: #f1f5f9; color: #64748b;
  flex: 1;
  justify-content: center;
}
.footer-btn-later:hover { background: #e2e8f0; color: #475569; }

.footer-btn-read {
  background: linear-gradient(135deg, #1E8C86, #2BA8A2);
  color: #fff;
  flex: 1;
  justify-content: center;
  box-shadow: 0 2px 8px rgba(43,168,162,0.35);
}
.footer-btn-read:hover {
  background: linear-gradient(135deg, #177a75, #239e98);
  box-shadow: 0 4px 12px rgba(43,168,162,0.45);
}
</style>