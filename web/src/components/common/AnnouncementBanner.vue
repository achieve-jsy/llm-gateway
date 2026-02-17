<template>
  <div v-if="bannerItems.length" class="ticker-wrap">
    <component :is="categoryIcon(current.category)" class="ticker-icon" />
    <span class="ticker-tag" :class="`tag--${current.level}`">{{ levelLabel(current.level) }}</span>
    <div class="ticker-track-wrap" @click="goDetail(current.id)">
      <div class="ticker-track" :class="{ paused: hovered }"
        @mouseenter="hovered = true" @mouseleave="hovered = false"
      >
        <span class="ticker-text">{{ current.title }}：{{ truncate(markdownToPlainText(current.content), 80) }}</span>
        <span class="ticker-sep">&nbsp;&nbsp;·&nbsp;&nbsp;</span>
        <span class="ticker-text">{{ current.title }}：{{ truncate(markdownToPlainText(current.content), 80) }}</span>
      </div>
    </div>
    <div class="ticker-controls">
      <button v-if="bannerItems.length > 1" type="button" class="ticker-btn" @click.stop="prev">
        <el-icon :size="11"><ArrowLeft /></el-icon>
      </button>
      <span v-if="bannerItems.length > 1" class="ticker-count">{{ currentIndex + 1 }}/{{ bannerItems.length }}</span>
      <button v-if="bannerItems.length > 1" type="button" class="ticker-btn" @click.stop="next">
        <el-icon :size="11"><ArrowRight /></el-icon>
      </button>
      <button type="button" class="ticker-btn ticker-close" @click.stop="handleClose(current)">
        <el-icon :size="11"><Close /></el-icon>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Close, ArrowLeft, ArrowRight, Setting, StarFilled, Bell } from '@element-plus/icons-vue'
import { getUnreadAnnouncements, markAnnouncementRead } from '@/services/control-plane'
import { markdownToPlainText } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

const router = useRouter()
const bannerItems = ref<Announcement[]>([])
const currentIndex = ref(0)
const hovered = ref(false)

const current = computed(() => bannerItems.value[currentIndex.value] ?? bannerItems.value[0])

import type { Component } from 'vue'
const categoryIcons: Record<string, Component> = { system: Setting, feature: StarFilled, alert: Bell }
const levelLabels: Record<string, string> = { info: '公告', warning: '警告', critical: '紧急' }

function categoryIcon(c: string): Component { return categoryIcons[c] || Bell }
function levelLabel(l: string) { return levelLabels[l] || '公告' }
function truncate(text: string, max: number) { return text.length > max ? text.slice(0, max) + '...' : text }

function prev() { currentIndex.value = (currentIndex.value - 1 + bannerItems.value.length) % bannerItems.value.length }
function next() { currentIndex.value = (currentIndex.value + 1) % bannerItems.value.length }

function goDetail(id: string) { router.push(`/announcements/${id}`) }

const DISMISS_PREFIX = 'banner_dismissed_'

async function loadBanners() {
  if (!localStorage.getItem('access_token')) return
  try {
    const items = await getUnreadAnnouncements()
    // 过滤掉已在本会话中关闭的公告
    const filtered = (items || []).filter(a => a.display_mode === 'banner' && !localStorage.getItem(DISMISS_PREFIX + a.id))
    bannerItems.value = filtered
    currentIndex.value = 0
  } catch { bannerItems.value = [] }
}

async function handleClose(item: Announcement) {
  try { await markAnnouncementRead(item.id) } catch {}
  // 持久化到 localStorage，刷新页面后也不会再次显示
  localStorage.setItem(DISMISS_PREFIX + item.id, '1')
  bannerItems.value = bannerItems.value.filter(a => a.id !== item.id)
  if (currentIndex.value >= bannerItems.value.length) currentIndex.value = Math.max(0, bannerItems.value.length - 1)
}

onMounted(loadBanners)
defineExpose({ refresh: loadBanners })
</script>

<style scoped>
.ticker-wrap {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  height: 28px;
  background: rgba(0,0,0,0.03);
  border-radius: 6px;
  padding: 0 8px;
  margin: 0 12px;
}

.ticker-icon { font-size: 13px; flex-shrink: 0; }

.ticker-tag {
  font-size: 12px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: 3px;
  flex-shrink: 0;
  white-space: nowrap;
}

.tag--info    { background: rgba(54,179,78,0.12); color: var(--brand-green-deep); }
.tag--warning { background: rgba(245,124,0,0.12);  color: var(--el-color-warning); }
.tag--critical{ background: rgba(211,47,47,0.12);  color: var(--color-danger); }

.ticker-track-wrap {
  flex: 1;
  overflow: hidden;
  cursor: pointer;
  height: 100%;
  display: flex;
  align-items: center;
  min-width: 0;
}

.ticker-track {
  display: inline-flex;
  white-space: nowrap;
  animation: ticker-scroll 18s linear infinite;
}

.ticker-track.paused { animation-play-state: paused; }

@keyframes ticker-scroll {
  0%   { transform: translateX(0); }
  100% { transform: translateX(-50%); }
}

.ticker-text {
  font-size: 12px;
  color: var(--el-text-color-regular);
  padding: 0 24px;
}

.ticker-sep { color: var(--el-text-color-placeholder); font-size: 12px; }

.ticker-controls {
  display: flex;
  align-items: center;
  gap: 1px;
  flex-shrink: 0;
}

.ticker-count {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  min-width: 26px;
  text-align: center;
}

.ticker-btn {
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  border-radius: 4px;
  cursor: pointer;
  color: rgba(0,0,0,0.3);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  flex-shrink: 0;
}

.ticker-btn:hover { background: rgba(0,0,0,0.07); color: rgba(0,0,0,0.6); }

.ticker-close { margin-left: 2px; }
</style>