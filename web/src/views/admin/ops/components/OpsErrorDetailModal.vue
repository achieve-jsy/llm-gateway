<template>
  <el-dialog
    :model-value="show"
    title="错误详情"
    width="800px"
    @update:model-value="$emit('update:show', $event)"
  >
    <div v-loading="loading" class="ops-error-detail">
      <template v-if="detail">
        <div class="ops-detail-grid">
          <div class="ops-detail-item">
            <span class="ops-detail-label">时间</span>
            <span class="ops-detail-value">{{ detail.ts }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">HTTP 状态码</span>
            <el-tag :type="statusType(detail.status)" effect="plain" round>{{ detail.status }}</el-tag>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">渠道</span>
            <span>{{ detail.provider }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">模型</span>
            <span>{{ detail.model }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">渠道组</span>
            <span>{{ detail.group_name || '-' }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">总耗时</span>
            <span>{{ detail.duration_ms }}ms</span>
          </div>
          <div class="ops-detail-item" v-if="'request_id' in detail && detail.request_id">
            <span class="ops-detail-label">request_id</span>
            <span class="ops-detail-copyable">
              <code>{{ detail.request_id }}</code>
              <el-button size="small" link @click="copyRequestId(detail.request_id)">
                <el-icon><CopyDocument /></el-icon>
              </el-button>
            </span>
          </div>
          <div class="ops-detail-item" v-if="'client_ip' in detail">
            <span class="ops-detail-label">客户端 IP</span>
            <span>{{ detail.client_ip || '-' }}</span>
          </div>
          <div class="ops-detail-item" v-if="'upstream_status' in detail">
            <span class="ops-detail-label">上游状态码</span>
            <el-tag v-if="detail.upstream_status" :type="statusType(detail.upstream_status)" effect="plain" round>
              {{ detail.upstream_status }}
            </el-tag>
            <span v-else>-</span>
          </div>
        </div>

        <!-- 延迟分解 -->
        <div class="ops-detail-section" v-if="detail.latency_breakdown">
          <span class="ops-detail-label">延迟分解</span>
          <div class="ops-latency-breakdown">
            <div class="ops-latency-item">
              <span class="ops-latency-icon">🔐</span>
              <span class="ops-latency-name">认证</span>
              <span class="ops-latency-value">{{ detail.latency_breakdown.auth_ms }}ms</span>
            </div>
            <div class="ops-latency-item">
              <span class="ops-latency-icon">🔀</span>
              <span class="ops-latency-name">路由</span>
              <span class="ops-latency-value">{{ detail.latency_breakdown.routing_ms }}ms</span>
            </div>
            <div class="ops-latency-item">
              <span class="ops-latency-icon">🌐</span>
              <span class="ops-latency-name">上游</span>
              <span class="ops-latency-value">{{ detail.latency_breakdown.upstream_ms }}ms</span>
            </div>
            <div class="ops-latency-item">
              <span class="ops-latency-icon">📤</span>
              <span class="ops-latency-name">响应</span>
              <span class="ops-latency-value">{{ detail.latency_breakdown.response_ms }}ms</span>
            </div>
            <div class="ops-latency-item">
              <span class="ops-latency-icon"><el-icon><Lightning /></el-icon></span>
              <span class="ops-latency-name">首字</span>
              <span class="ops-latency-value">{{ detail.latency_breakdown.ttft_ms }}ms</span>
            </div>
          </div>
        </div>

        <!-- 上游上下文 -->
        <div class="ops-detail-section" v-if="detail.upstream_context">
          <span class="ops-detail-label">上游上下文</span>
          <div class="ops-upstream-context">
            <div class="ops-detail-item" v-if="detail.upstream_context.upstream_status_code">
              <span class="ops-detail-label">状态码</span>
              <el-tag :type="statusType(detail.upstream_context.upstream_status_code)" effect="plain" round>
                {{ detail.upstream_context.upstream_status_code }}
              </el-tag>
            </div>
            <div class="ops-detail-item" v-if="detail.upstream_context.upstream_error_message">
              <span class="ops-detail-label">错误信息</span>
              <span class="ops-detail-value">{{ detail.upstream_context.upstream_error_message }}</span>
            </div>
            <div class="ops-detail-item" v-if="detail.upstream_context.upstream_endpoint">
              <span class="ops-detail-label">端点</span>
              <code>{{ detail.upstream_context.upstream_endpoint }}</code>
            </div>
          </div>
        </div>

        <div class="ops-detail-section" v-if="detail.error_message">
          <span class="ops-detail-label">错误信息</span>
          <pre class="ops-detail-pre">{{ detail.error_message }}</pre>
        </div>

        <div class="ops-detail-actions">
          <el-button
            v-if="!isResolved"
            type="success"
            size="small"
            :loading="resolving"
            @click="markResolved"
          >
            标记已解决
          </el-button>
          <el-tag v-else type="success" effect="plain">已解决</el-tag>
        </div>
      </template>
      <div v-else-if="!loading" class="ops-error-empty">
        无法加载错误详情
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage, ElButton, ElIcon } from 'element-plus'
import { CopyDocument, Lightning } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'

const props = defineProps<{
  show: boolean
  errorId: number
  errorType: 'request' | 'upstream'
}>()

defineEmits<{
  'update:show': [val: boolean]
}>()

const loading = ref(false)
const resolving = ref(false)
const detail = ref<any>(null)
const isResolved = ref(false)

watch(() => props.show, async (val) => {
  if (val && props.errorId) {
    await fetchDetail()
  }
})

function statusType(status: number): 'danger' | 'warning' | 'info' {
  if (status >= 500) return 'danger'
  if (status >= 400) return 'warning'
  return 'info'
}

async function fetchDetail() {
  loading.value = true
  try {
    if (props.errorType === 'request') {
      const data = await opsAPI.getRequestErrorDetail(props.errorId)
      detail.value = data
      isResolved.value = false
    } else {
      const data = await opsAPI.getUpstreamErrorDetail(props.errorId)
      detail.value = data
      isResolved.value = data.resolved
    }
  } catch {
    ElMessage.error('加载详情失败')
    detail.value = null
  } finally {
    loading.value = false
  }
}

async function markResolved() {
  resolving.value = true
  try {
    if (props.errorType === 'request') {
      await opsAPI.updateRequestErrorResolved(props.errorId, true)
    } else {
      await opsAPI.updateUpstreamErrorResolved(props.errorId, true)
    }
    isResolved.value = true
    ElMessage.success('已标记为已解决')
  } catch {
    ElMessage.error('操作失败')
  } finally {
    resolving.value = false
  }
}
</script>

<style scoped>
.ops-error-detail {
  padding: 4px 0;
}

.ops-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 12px;
}

.ops-detail-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ops-detail-label {
  font-size: var(--font-size-xxs);
  color: var(--text-placeholder, #94a3b8);
  font-weight: var(--font-weight-semibold);
}

.ops-detail-value {
  font-size: var(--font-size-xs, 12px);
  color: var(--text-primary, #1e293b);
}

.ops-detail-item code {
font-family: var(--font-family-mono);
  font-size: var(--font-size-xxs);
  background: var(--bg-subtle, #f8fafc);
  padding: 2px 6px;
  border-radius: 4px;
  word-break: break-all;
}

.ops-detail-section {
  margin-top: 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ops-detail-pre {
  background: var(--bg-subtle, #f8fafc);
  border: 1px solid var(--border-light, #e5e7eb);
  border-radius: var(--radius-base, 6px);
  padding: 10px 14px;
  font-size: var(--font-size-xxs);
  color: #ef4444;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
  font-family: var(--font-family-mono);
}

.ops-detail-actions {
  margin-top: 20px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.ops-error-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 120px;
  color: var(--text-placeholder, #94a3b8);
}
</style>