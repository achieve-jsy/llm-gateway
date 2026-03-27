<template>
  <el-drawer
    v-model="visible"
    title="视频任务详情"
    size="760px"
    :destroy-on-close="true"
  >
    <template #header>
      <div class="video-task-header">
        <span class="title">视频任务详情</span>
        <span class="task-id">{{ task?.task_id }}</span>
      </div>
    </template>

    <div v-loading="loading">
      <el-tabs v-model="activeTab" class="detail-tabs">
        <el-tab-pane label="任务信息" name="info">
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="任务 ID">{{ task?.task_id || '—' }}</el-descriptions-item>
            <el-descriptions-item label="模型">{{ task?.model || '—' }}</el-descriptions-item>
            <el-descriptions-item label="渠道">{{ task?.provider_name || '—' }}</el-descriptions-item>
            <el-descriptions-item label="渠道类型">{{ task?.provider_type || '—' }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="getStatusType(task?.status)" size="small">{{ task?.status || '—' }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="Key">{{ task?.api_key_name || task?.api_key_id || '—' }}</el-descriptions-item>
            <el-descriptions-item label="用户">{{ task?.user_name || task?.user_id || '—' }}</el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ fmtDateTime(task?.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="更新时间">{{ fmtDateTime(task?.updated_at) }}</el-descriptions-item>
            <el-descriptions-item label="生成耗时">{{ formatGenDuration(task) }}</el-descriptions-item>
            <el-descriptions-item v-if="task?.error_message" label="错误信息">
              <span class="error-text">{{ task.error_message }}</span>
            </el-descriptions-item>
          </el-descriptions>
        </el-tab-pane>

        <el-tab-pane label="生成结果" name="result">
          <div v-if="canPreviewVideo" class="result-panel">
            <video class="video-preview" :src="task?.video_url" controls />
            <a
              class="download-link"
              :href="task?.video_url"
              download
              target="_blank"
              rel="noopener noreferrer"
            >
              <el-button type="primary">下载视频</el-button>
            </a>
          </div>
          <el-empty v-else description="暂无可预览的视频结果" />
        </el-tab-pane>

        <el-tab-pane label="请求" name="request">
          <pre class="json-block">{{ requestJSON }}</pre>
        </el-tab-pane>

        <el-tab-pane label="响应" name="response">
          <pre class="json-block">{{ responseJSON }}</pre>
        </el-tab-pane>

        <el-tab-pane label="事件时间线" name="timeline">
          <el-timeline v-if="timelineItems.length" class="timeline">
            <el-timeline-item
              v-for="(item, idx) in timelineItems"
              :key="`${item.created_at}-${idx}`"
              :timestamp="fmtDateTime(item.created_at)"
            >
              <div class="timeline-title">{{ item.title }}</div>
              <div v-if="item.detail" class="timeline-detail">{{ item.detail }}</div>
            </el-timeline-item>
          </el-timeline>
          <el-empty v-else description="暂无事件记录" />
        </el-tab-pane>
      </el-tabs>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { videoTasksAPI, type VideoTask, type VideoTaskEvent } from '@/api/videoTasks'
import { fmtDateTime } from '@/utils/time'

const props = defineProps<{
  task: VideoTask | null
}>()

const visible = defineModel<boolean>({ required: true })

const loading = ref(false)
const activeTab = ref('info')
const localTask = ref<VideoTask | null>(props.task)
const task = localTask

watch(() => props.task, (val) => {
  localTask.value = val
  if (val?.task_id && visible.value) {
    refresh(val.task_id)
  }
}, { deep: true })

watch(visible, (v) => {
  if (v && localTask.value?.task_id) {
    activeTab.value = 'info'
    refresh(localTask.value.task_id)
  }
}, { immediate: true })

const canPreviewVideo = computed(() => task.value?.status === 'succeeded' && !!task.value?.video_url)

const requestJSON = computed(() => formatJSON(task.value?.request_body, '暂无请求内容'))
const responseJSON = computed(() => formatJSON(task.value?.response_body, '暂无响应内容'))

const timelineItems = computed(() => {
  const current = task.value
  if (!current) return []
  if (current.events?.length) {
    return current.events.map(eventToTimelineItem)
  }
  const items = []
  if (current.created_at) {
    items.push({
      title: '任务已提交',
      detail: current.task_id,
      created_at: current.created_at,
    })
  }
  if (current.updated_at && current.updated_at !== current.created_at) {
    items.push({
      title: `状态更新为 ${current.status || '—'}`,
      detail: current.error_message || '',
      created_at: current.updated_at,
    })
  }
  return items
})

async function refresh(taskId: string) {
  loading.value = true
  try {
    localTask.value = await videoTasksAPI.get(taskId)
  } catch {
    // 保留传入的初始数据
  } finally {
    loading.value = false
  }
}

function getStatusType(status: string | undefined): 'info' | 'warning' | 'success' | 'danger' {
  if (status === 'succeeded') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'warning'
  return 'info'
}

function formatGenDuration(row: VideoTask | null): string {
  if (!row?.updated_at || !row.created_at) return '—'
  if (row.status !== 'succeeded' && row.status !== 'failed') return '—'
  const ms = new Date(row.updated_at).getTime() - new Date(row.created_at).getTime()
  if (!Number.isFinite(ms) || ms < 0) return '—'
  const sec = ms / 1000
  if (sec < 60) return `${sec.toFixed(1)}s`
  return `${Math.floor(sec / 60)}m${Math.round(sec % 60)}s`
}

function formatJSON(raw: string | undefined, emptyText: string): string {
  if (!raw) return emptyText
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function eventToTimelineItem(event: VideoTaskEvent) {
  const title = formatEventTitle(event.event_type)
  const detail = (event.event_data || '').trim()
  const redundant =
    !detail ||
    detail === title ||
    /^状态更新为\s+\w+$/u.test(detail)
  return {
    title,
    detail: redundant ? '' : detail,
    created_at: event.created_at,
  }
}

function formatEventTitle(type: string): string {
  if (type === 'submitted') return '任务已提交'
  if (type === 'running') return '任务运行中'
  if (type === 'succeeded') return '任务已成功'
  if (type === 'failed') return '任务失败'
  return type || '任务更新'
}
</script>

<style scoped>
.video-task-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
  gap: 12px;
}
.title {
  font-size: 16px;
  font-weight: 600;
}
.task-id {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-family: var(--el-font-family);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.detail-tabs {
  min-height: 360px;
}
.result-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.video-preview {
  width: 100%;
  max-height: 420px;
  border-radius: 8px;
  background: var(--el-fill-color-darker);
}
.download-link {
  align-self: flex-start;
  text-decoration: none;
}
.json-block {
  min-height: 240px;
  max-height: 520px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-primary);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
.timeline {
  padding: 8px 0 0 2px;
}
.timeline-title {
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.timeline-detail {
  margin-top: 4px;
  color: var(--el-text-color-secondary);
  word-break: break-word;
}
.error-text {
  color: var(--el-color-danger);
}
</style>
