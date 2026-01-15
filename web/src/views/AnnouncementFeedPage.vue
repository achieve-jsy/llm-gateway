<template>
  <div class="feed-page">
    <div class="feed-header">
      <div class="feed-header-icon">📢</div>
      <div class="feed-header-text">
        <h1 class="feed-header-title">公告中心</h1>
        <p class="feed-header-desc">系统更新、功能发布与重要通知</p>
      </div>
    </div>

    <div v-if="loading && announcements.length === 0" class="feed-loading">
      <el-skeleton :rows="4" animated />
    </div>

    <div v-else-if="!loading && announcements.length === 0" class="feed-empty">
      <el-empty description="暂无公告，一切正常" :image-size="80" />
    </div>

    <div v-else class="feed-timeline">
      <div
        v-for="item in announcements"
        :key="item.id"
        class="timeline-item"
      >
        <div class="timeline-dot" :class="`dot--${item.level}`"></div>
        <div class="timeline-card" @click="goDetail(item.id)">
          <div class="card-top">
            <div class="card-meta">
              <span class="card-category-tag" :class="`cat--${item.category}`">
                {{ categoryLabel(item.category) }}
              </span>
              <span class="card-level-tag" :class="`level--${item.level}`">
                {{ levelLabel(item.level) }}
              </span>
            </div>
            <span class="card-time">{{ relativeTime(item.published_at) }}</span>
          </div>
          <h3 class="card-title">{{ item.title }}</h3>
          <div class="card-preview">{{ truncate(markdownToPlainText(item.content), 150) }}</div>
          <div class="card-footer">
            <span class="card-expand-link">
              查看详情
              <el-icon :size="12"><ArrowRight /></el-icon>
            </span>
          </div>
        </div>
      </div>

      <!-- 底部加载触发器 -->
      <div ref="loadMoreRef" class="load-more">
        <div v-if="loadingMore" class="load-more-spinner">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span>加载中...</span>
        </div>
        <div v-else-if="noMore" class="load-more-end">— 已加载全部 {{ total }} 条公告 —</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, Loading } from '@element-plus/icons-vue'
import { getActiveAnnouncements } from '@/services/control-plane'
import { markdownToPlainText } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

const router = useRouter()
const announcements = ref<Announcement[]>([])
const loading = ref(true)
const loadingMore = ref(false)
const page = ref(1)
const pageSize = 10
const total = ref(0)
const loadMoreRef = ref<HTMLElement>()

const noMore = computed(() => !loading.value && !loadingMore.value && announcements.value.length >= total.value && total.value > 0)

const categoryLabels: Record<string, string> = { system: '系统公告', feature: '功能更新', alert: '重要通知' }
const levelLabels: Record<string, string> = { info: '信息', warning: '警告', critical: '紧急' }

function categoryLabel(c: string) { return categoryLabels[c] || c }
function levelLabel(l: string) { return levelLabels[l] || l }
function truncate(text: string, max: number) { return text.length > max ? text.slice(0, max) + '...' : text }
function goDetail(id: string) { router.push(`/announcements/${id}`) }

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

async function loadPage(p: number) {
  if (p === 1) {
    loading.value = true
  } else {
    loadingMore.value = true
  }
  try {
    const result = await getActiveAnnouncements(p, pageSize)
    total.value = result.total
    if (p === 1) {
      announcements.value = result.data
    } else {
      announcements.value = [...announcements.value, ...result.data]
    }
    page.value = p
  } catch {}
  loading.value = false
  loadingMore.value = false
}

let observer: IntersectionObserver | null = null

onMounted(async () => {
  await loadPage(1)

  observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && !loadingMore.value && !loading.value && !noMore.value) {
      loadPage(page.value + 1)
    }
  }, { threshold: 0.1 })

  if (loadMoreRef.value) observer.observe(loadMoreRef.value)
})

onUnmounted(() => {
  observer?.disconnect()
})
</script>

<style scoped>
.feed-page {
  width: 100%;
  padding: clamp(1rem, 3vw, 2rem) clamp(1rem, 3vw, 2rem);
}

.feed-header {
  display: flex;
  align-items: center;
  gap: clamp(0.75rem, 1.5vw, 1rem);
  margin-bottom: clamp(1.5rem, 3vw, 2.5rem);
  padding-bottom: clamp(1rem, 2vw, 1.5rem);
  border-bottom: 1px solid var(--el-border-color-light);
}

.feed-header-icon {
  width: clamp(2.5rem, 5vw, 3.5rem);
  height: clamp(2.5rem, 5vw, 3.5rem);
  border-radius: clamp(0.75rem, 1.5vw, 1rem);
  background: linear-gradient(135deg, #EAF7EC, #D6EBD9);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: clamp(1.25rem, 2.5vw, 1.75rem);
  flex-shrink: 0;
}

.feed-header-title {
  font-size: clamp(1.25rem, 3vw, 1.75rem);
  font-weight: 800;
  color: var(--el-text-color-primary);
  margin: 0 0 0.25rem 0;
  line-height: 1.2;
}

.feed-header-desc {
  font-size: clamp(0.75rem, 1.5vw, 0.875rem);
  color: var(--el-text-color-secondary);
  margin: 0;
}

.feed-timeline {
  position: relative;
  padding-left: clamp(1.5rem, 3vw, 2rem);
}

.feed-timeline::before {
  content: '';
  position: absolute;
  left: 0.35rem;
  top: 0;
  bottom: 0;
  width: 2px;
  background: linear-gradient(to bottom, var(--el-color-primary, #36B34E), var(--el-border-color-light, #e5e7eb));
  border-radius: 1px;
}

.timeline-item {
  position: relative;
  margin-bottom: clamp(1.25rem, 2.5vw, 1.75rem);
}

.timeline-dot {
  position: absolute;
  left: calc(clamp(1.5rem, 3vw, 2rem) * -1);
  top: 1.1rem;
  width: 0.875rem;
  height: 0.875rem;
  border-radius: 50%;
  border: 2px solid #fff;
  box-shadow: 0 0 0 2px var(--el-color-primary, #36B34E);
  background: var(--el-color-primary, #36B34E);
  z-index: 1;
}

.dot--warning { box-shadow: 0 0 0 2px #F57C00; background: #F57C00; }
.dot--critical { box-shadow: 0 0 0 2px #D32F2F; background: #D32F2F; }

.timeline-card {
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: clamp(0.75rem, 1.5vw, 1rem);
  padding: clamp(1rem, 2vw, 1.5rem);
  cursor: pointer;
  transition: all 0.2s;
}

.timeline-card:hover {
  box-shadow: 0 4px 20px rgba(0,0,0,0.06);
  border-color: var(--el-border-color);
}

.card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: clamp(0.5rem, 1vw, 0.75rem);
  flex-wrap: wrap;
}

.card-meta {
  display: flex;
  gap: clamp(0.25rem, 0.5vw, 0.5rem);
  flex-wrap: wrap;
}

.card-category-tag,
.card-level-tag {
  padding: 0.125rem 0.5rem;
  border-radius: 0.375rem;
  font-size: clamp(0.625rem, 1.2vw, 0.75rem);
  font-weight: 600;
  white-space: nowrap;
}

.cat--system { background: #EAF7EC; color: #36B34E; }
.cat--feature { background: #EDE7F6; color: #5E35B1; }
.cat--alert { background: #FFF3E0; color: #E65100; }

.level--info { background: #EAF7EC; color: #36B34E; }
.level--warning { background: #FFF3E0; color: #E65100; }
.level--critical { background: #FFEBEE; color: #C62828; }

.card-time {
  font-size: clamp(0.625rem, 1.2vw, 0.75rem);
  color: var(--el-text-color-placeholder);
  flex-shrink: 0;
}

.card-title {
  font-size: clamp(0.938rem, 2vw, 1.125rem);
  font-weight: 700;
  color: #1a1a1a;
  margin: 0 0 clamp(0.5rem, 1vw, 0.75rem) 0;
  line-height: 1.5;
  word-break: break-word;
}

.card-preview {
  font-size: clamp(0.813rem, 1.5vw, 0.938rem);
  color: var(--el-text-color-secondary);
  line-height: 1.7;
  margin-bottom: 0.625rem;
}

.card-expand-link {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-size: clamp(0.688rem, 1.1vw, 0.75rem);
  color: var(--el-color-primary);
  opacity: 0.8;
  transition: opacity 0.15s;
}

.timeline-card:hover .card-expand-link { opacity: 1; }

.feed-loading { margin: clamp(1.5rem, 3vw, 2.5rem) 0; }
.feed-empty { padding: clamp(2rem, 5vw, 4rem) 0; }

.load-more {
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 24px 0 8px;
  min-height: 48px;
}

.load-more-spinner {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--el-text-color-placeholder);
}

.load-more-spinner .is-loading {
  animation: rotating 1s linear infinite;
}

@keyframes rotating {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}

.load-more-end {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  letter-spacing: 0.5px;
}

@media (max-width: 640px) {
  .card-top { flex-direction: column; align-items: flex-start; gap: 0.375rem; }
  .card-time { order: -1; }
  .feed-timeline { padding-left: 1.5rem; }
  .timeline-dot { left: -1.5rem; width: 0.75rem; height: 0.75rem; }
  .timeline-card { border-radius: 0.75rem; padding: 1rem; }
}
</style>