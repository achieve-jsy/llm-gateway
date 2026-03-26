<template>
  <el-drawer
    v-model="visible"
    title="视频任务详情"
    size="600px"
    :destroy-on-close="true"
  >
    <template #header>
      <div class="video-task-header">
        <span class="title">🎬 视频任务详情</span>
        <span class="task-id">{{ task?.task_id }}</span>
      </div>
    </template>

    <el-tabs v-model="activeTab">
      <!-- Tab 1: 任务信息 -->
      <el-tab-pane label="任务信息" name="info">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="网关任务 ID">{{ task?.task_id }}</el-descriptions-item>
          <el-descriptions-item label="上游任务 ID">{{ task?.upstream_task_id }}</el-descriptions-item>
          <el-descriptions-item label="模型">{{ task?.model }}</el-descriptions-item>
          <el-descriptions-item label="渠道">{{ task?.provider_name }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="getStatusType(task?.status)">{{ task?.status }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ task?.created_at }}</el-descriptions-item>
          <el-descriptions-item label="更新时间">{{ task?.updated_at }}</el-descriptions-item>
        </el-descriptions>
      </el-tab-pane>

      <!-- Tab 2: 生成结果 -->
      <el-tab-pane label="生成结果" name="result">
        <div v-if="task?.status === 'succeeded'" class="video-result">
          <div class="video-preview">
            <video controls :src="task?.video_url" style="width: 100%; max-height: 400px;">
              您的浏览器不支持视频播放
            </video>
          </div>
          <el-button type="primary" @click="downloadVideo" style="margin-top: 16px;">
            下载视频（24h 内有效）
          </el-button>
        </div>
        <el-empty v-else description="视频尚未生成完成" />
      </el-tab-pane>

      <!-- Tab 3: 请求 -->
      <el-tab-pane label="请求" name="request">
        <pre class="json-viewer">{{ formatJSON(task?.request_body) }}</pre>
      </el-tab-pane>

      <!-- Tab 4: 响应 -->
      <el-tab-pane label="响应" name="response">
        <pre class="json-viewer">{{ formatJSON(task?.response_body) }}</pre>
      </el-tab-pane>

      <!-- Tab 5: 事件时间线 -->
      <el-tab-pane label="事件时间线" name="timeline">
        <el-timeline>
          <el-timeline-item
            v-for="event in task?.events"
            :key="event.timestamp"
            :timestamp="event.timestamp"
            :type="getEventType(event.type)"
          >
            {{ event.message }}
          </el-timeline-item>
        </el-timeline>
      </el-tab-pane>
    </el-tabs>
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

interface VideoTask {
  task_id: string
  upstream_task_id?: string
  model: string
  provider_name: string
  status: string
  created_at: string
  updated_at?: string
  video_url?: string
  request_body?: any
  response_body?: any
  events?: Array<{
    timestamp: string
    type: string
    message: string
  }>
}

const props = defineProps<{
  task: VideoTask | null
}>()

const visible = defineModel<boolean>({ required: true })

const activeTab = ref('info')

function getStatusType(status: string | undefined): string {
  if (!status) return 'info'
  const map: Record<string, string> = {
    submitted: 'info',
    running: 'warning',
    succeeded: 'success',
    failed: 'danger',
  }
  return map[status] || 'info'
}

function getEventType(type: string): string {
  const map: Record<string, string> = {
    submitted: 'primary',
    running: 'warning',
    succeeded: 'success',
    failed: 'danger',
  }
  return map[type] || 'info'
}

function formatJSON(data: any): string {
  if (!data) return '暂无数据'
  return JSON.stringify(data, null, 2)
}

function downloadVideo() {
  if (props.task?.video_url) {
    window.open(props.task.video_url, '_blank')
  }
}
</script>

<style scoped>
.video-task-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.title {
  font-size: 18px;
  font-weight: 600;
}

.task-id {
  font-size: 14px;
  color: var(--text-secondary);
  font-family: var(--font-mono);
}

.video-result {
  text-align: center;
}

.json-viewer {
  background: var(--bg-subtle);
  padding: 16px;
  border-radius: 8px;
  overflow-x: auto;
  font-size: 13px;
  line-height: 1.6;
  font-family: var(--font-mono);
}

:deep(.el-timeline) {
  padding-left: 8px;
}

:deep(.el-timeline-item__wrapper) {
  padding-left: 20px;
}
</style>
