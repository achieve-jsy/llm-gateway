<template>
  <div class="detail-page">
    <div class="detail-back">
      <el-button text @click="$router.push('/announcements')">
        <el-icon><ArrowLeft /></el-icon>
        返回公告中心
      </el-button>
    </div>

    <div v-if="loading" class="detail-loading">
      <el-skeleton :rows="5" animated />
    </div>

    <div v-else-if="!announcement" class="detail-empty">
      <el-empty description="公告不存在或已删除" />
    </div>

    <article v-else class="detail-article">
      <header class="detail-header">
        <div class="detail-meta">
          <span class="detail-category" :class="`cat--${announcement.category}`">
            {{ categoryLabel(announcement.category) }}
          </span>
          <span class="detail-level" :class="`level--${announcement.level}`">
            {{ levelLabel(announcement.level) }}
          </span>
          <span class="detail-dot">·</span>
          <span class="detail-time">{{ formatTime(announcement.published_at) }}</span>
        </div>
        <h1 class="detail-title">{{ announcement.title }}</h1>
        <div v-if="announcement.created_by" class="detail-author">
          发布者：{{ announcement.created_by }}
        </div>
      </header>

      <div class="detail-divider"></div>

      <div class="detail-body md-content" v-html="renderMarkdown(announcement.content)"></div>
    </article>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import { getPublicAnnouncement, markAnnouncementRead } from '@/services/control-plane'
import { renderMarkdown } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

const route = useRoute()
const announcement = ref<Announcement | null>(null)
const loading = ref(true)

const categoryLabels: Record<string, string> = { system: '系统公告', feature: '功能更新', alert: '重要通知' }
const levelLabels: Record<string, string> = { info: '信息', warning: '警告', critical: '紧急' }

function categoryLabel(c: string) { return categoryLabels[c] || c }
function levelLabel(l: string) { return levelLabels[l] || l }
function formatTime(t: string | null) { return t ? new Date(t).toLocaleString('zh-CN') : '-' }

onMounted(async () => {
  try {
    announcement.value = await getPublicAnnouncement(route.params.id as string)
    markAnnouncementRead(announcement.value.id).catch(() => {})
  } catch {}
  loading.value = false
})
</script>

<style scoped>
.detail-page {
  width: 100%;
  padding: clamp(1rem, 3vw, 2rem) clamp(1rem, 3vw, 2rem);
}

.detail-back { margin-bottom: clamp(1rem, 2vw, 1.5rem); }

.detail-article {
  background: var(--bg-surface);
  border: 1px solid var(--el-border-color-light);
  border-radius: clamp(0.75rem, 1.5vw, 1rem);
  padding: clamp(1.25rem, 3vw, 2.5rem);
}

.detail-meta {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: clamp(0.75rem, 1.5vw, 1rem);
  flex-wrap: wrap;
}

.detail-category,
.detail-level {
  padding: 0.15rem 0.625rem;
  border-radius: 0.375rem;
  font-size: clamp(0.688rem, 1.1vw, 0.75rem);
  font-weight: 600;
}

.cat--system { background: var(--color-success-bg); color: var(--brand-green); }
.cat--feature { background: #EDE7F6; color: #5E35B1; }
.cat--alert { background: var(--color-warning-bg); color: var(--el-color-warning); }

.level--info { background: var(--color-success-bg); color: var(--brand-green); }
.level--warning { background: var(--color-warning-bg); color: var(--el-color-warning); }
.level--critical { background: var(--color-danger-bg); color: var(--color-danger); }

.detail-dot { color: var(--el-text-color-placeholder); }

.detail-time {
  font-size: clamp(0.688rem, 1.1vw, 0.75rem);
  color: var(--el-text-color-placeholder);
}

.detail-title {
  font-size: clamp(1.25rem, 3vw, 1.75rem);
  font-weight: 800;
  color: var(--text-primary);
  margin: 0 0 0.5rem 0;
  line-height: 1.4;
  word-break: break-word;
}

.detail-author {
  font-size: clamp(0.75rem, 1.2vw, 0.813rem);
  color: var(--el-text-color-secondary);
}

.detail-divider {
  height: 1px;
  background: var(--el-border-color-light);
  margin: clamp(1rem, 2vw, 1.5rem) 0;
}

.detail-body {
  word-break: break-word;
}

.detail-loading { max-width: 48rem; margin: clamp(1.5rem, 3vw, 2.5rem) auto; }
.detail-empty { padding: clamp(2rem, 5vw, 4rem) 0; }
</style>