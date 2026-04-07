<template>
  <el-dialog
    :model-value="visible"
    title="站内通知"
    width="680px"
    :close-on-click-modal="false"
    :show-close="true"
    @close="emit('close')"
  >
    <div v-if="announcements.length === 0" class="modal-empty">
      <el-empty description="暂无新通知" :image-size="80" />
    </div>
    <div v-else class="modal-list">
      <div
        v-for="item in announcements"
        :key="item.id"
        class="modal-item"
        :class="[`level--${item.level}`, { 'is-expanded': expandedId === item.id }]"
        @click="toggleExpand(item)"
      >
        <div class="modal-item-header">
          <span class="modal-item-icon">{{ levelIcon(item.level) }}</span>
          <span class="modal-item-title">{{ item.title }}</span>
          <span class="modal-item-time">{{ formatTime(item.published_at) }}</span>
        </div>
        <div class="modal-item-body">
          <div
            v-if="expandedId === item.id"
            class="modal-item-full"
            v-html="renderMarkdown(item.content)"
          ></div>
          <div v-else class="modal-item-preview">{{ truncate(item.content, 150) }}</div>
          <div class="modal-item-hint" v-if="expandedId !== item.id">点击展开详情</div>
        </div>
        <div class="modal-item-footer">
          <el-button size="small" @click.stop="handleRead(item.id)">已读</el-button>
        </div>
      </div>
    </div>
    <template #footer v-if="announcements.length > 0">
      <el-button @click="emit('close')">关闭</el-button>
      <el-button type="primary" @click="handleReadAll">全部已读</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getUnreadAnnouncements, markAnnouncementRead } from '@/services/control-plane'
import { renderMarkdown } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: [] }>()

const announcements = ref<Announcement[]>([])
const expandedId = ref('')

const levelIcons: Record<string, string> = { info: '📢', warning: '⚠️', critical: '🚨' }
function levelIcon(l: string) { return levelIcons[l] || '📢' }
function formatTime(t: string | null) { return t ? new Date(t).toLocaleString('zh-CN') : '-' }
function truncate(text: string, max: number) { return text.length > max ? text.slice(0, max) + '...' : text }

function toggleExpand(item: Announcement) {
  expandedId.value = expandedId.value === item.id ? '' : item.id
}

async function handleRead(id: string) {
  try {
    await markAnnouncementRead(id)
    announcements.value = announcements.value.filter(a => a.id !== id)
    if (announcements.value.length === 0) emit('close')
  } catch {}
}

async function handleReadAll() {
  await Promise.all(announcements.value.map(a => markAnnouncementRead(a.id).catch(() => {})))
  emit('close')
}

onMounted(async () => {
  try { announcements.value = await getUnreadAnnouncements() } catch {}
})
</script>

<style scoped>
.modal-list { max-height: 450px; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; }

.modal-item {
  padding: 18px 20px;
  border-radius: 10px;
  background: var(--el-fill-color-light);
  border-left: 4px solid var(--el-color-primary);
  cursor: pointer;
  transition: all 0.15s;
}

.modal-item:hover { box-shadow: 0 2px 8px rgba(0,0,0,0.06); }
.modal-item.level--warning { border-left-color: var(--color-warning); }
.modal-item.level--critical { border-left-color: var(--color-danger); background: var(--color-danger-bg); }
.modal-item.is-expanded { background: var(--bg-surface); }

.modal-item-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.modal-item-icon { font-size: 22px; }
.modal-item-title { font-size: 16px; font-weight: 700; flex: 1; }
.modal-item-time { font-size: 12px; color: var(--el-text-color-placeholder); flex-shrink: 0; }

.modal-item-body { margin-bottom: 12px; }

.modal-item-preview {
  font-size: 14px;
  color: var(--el-text-color-regular);
  line-height: 1.7;
}

.modal-item-full {
  font-size: 14px;
  color: var(--el-text-color-regular);
  line-height: 1.8;
  padding: 12px;
  background: rgba(0,0,0,0.03);
  border-radius: 8px;
}

.modal-item-full :deep(code) { background: rgba(0,0,0,0.07); padding: 2px 6px; border-radius: 4px; font-size: 13px; }
.modal-item-full :deep(strong) { font-weight: 700; }

.modal-item-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin-top: 6px;
}

.modal-item-footer {
  display: flex;
  justify-content: flex-end;
  padding-top: 4px;
}

.modal-empty { padding: 40px 0; }

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95%;
    max-width: 95vw;
  }
  .modal-item-header {
    flex-wrap: wrap;
  }
  .modal-item-time {
    width: 100%;
    text-align: left;
  }
}
</style>