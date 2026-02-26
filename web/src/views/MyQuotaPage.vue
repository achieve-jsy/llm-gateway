<template>
  <div class="my-quota-page">
    <div class="page-header">
      <h2 class="page-title">我的配额</h2>
      <p class="page-desc">查看当前生效的请求频率和并发限制配置</p>
    </div>

    <div v-loading="loading" class="quota-content">
      <template v-if="quota">
        <div class="quota-card">
          <div class="card-header">
            <div class="card-title-row">
              <span class="card-icon">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
              </span>
              <h3 class="card-title">当前生效配额</h3>
            </div>
          </div>
          <div class="card-body">
            <div class="quota-grid">
              <div class="quota-item">
                <div class="quota-label">每分钟请求数 (RPM)</div>
                <div class="quota-value">
                  {{ quota.effective.rpm === 0 ? '不限制' : quota.effective.rpm }}
                </div>
                <div class="quota-source">
                  <el-tag :type="sourceTagType(quota.effective.source)" size="small" effect="light">
                    {{ sourceLabel(quota.effective.source) }}
                  </el-tag>
                </div>
              </div>
              <div class="quota-item">
                <div class="quota-label">最大并发数</div>
                <div class="quota-value">
                  {{ quota.effective.max_concurrency === 0 ? '不限制' : quota.effective.max_concurrency }}
                </div>
                <div class="quota-source">
                  <el-tag :type="sourceTagType(quota.effective.source)" size="small" effect="light">
                    {{ sourceLabel(quota.effective.source) }}
                  </el-tag>
                </div>
              </div>
            </div>

            <!-- 当前使用量 -->
            <div class="usage-section">
              <h4 class="usage-title">当前使用量</h4>
              <div class="usage-grid">
                <div class="usage-item">
                  <div class="usage-label">
                    当前 RPM: {{ quota.current_usage.rpm }}
                    <span v-if="quota.effective.rpm > 0"> / {{ quota.effective.rpm }}</span>
                  </div>
                  <el-progress
                    :percentage="rpmPercentage"
                    :stroke-width="16"
                    :color="getProgressColor(rpmPercentage)"
                  />
                </div>
                <div class="usage-item">
                  <div class="usage-label">
                    当前并发: {{ quota.current_usage.max_concurrency }}
                    <span v-if="quota.effective.max_concurrency > 0"> / {{ quota.effective.max_concurrency }}</span>
                  </div>
                  <el-progress
                    :percentage="concPercentage"
                    :stroke-width="16"
                    :color="getProgressColor(concPercentage)"
                  />
                </div>
              </div>
            </div>

            <div class="quota-info">
              <el-alert
                type="info"
                :closable="false"
                show-icon
              >
                <template #title>
                  <span v-if="quota.effective.source === 'default_user'">
                    当前使用全局默认配额（用户默认：{{ quota.effective.default_rpm ?? '-' }} RPM / {{ quota.effective.default_max_concurrency ?? '-' }} 并发）。管理员可为您单独调整。
                  </span>
                  <span v-else-if="quota.effective.source === 'override'">
                    管理员已为您配置专属配额。
                  </span>
                  <span v-else>
                    配额来源：{{ sourceLabel(quota.effective.source) }}
                  </span>
                </template>
              </el-alert>
            </div>
          </div>
        </div>

        <div class="quota-note">
          <el-alert type="warning" :closable="false" show-icon>
            <template #title>
              <strong>配额</strong>（RPM/并发，超限返回 429）与<strong>预算</strong>（费用/Token 额度，超限返回 402）是两个独立体系，互不影响。
            </template>
          </el-alert>
        </div>
      </template>

      <div v-else-if="!loading" class="state-empty">
        <el-result icon="warning" title="无法加载配额信息" sub-title="请检查网络后重试">
          <template #extra>
            <el-button type="primary" @click="loadQuota">重新加载</el-button>
          </template>
        </el-result>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { getMyQuota } from '@/api/quota'
import type { MyQuotaResponse } from '@/api/quota'

const loading = ref(false)
const quota = ref<MyQuotaResponse | null>(null)

const rpmPercentage = computed(() => {
  if (!quota.value || quota.value.effective.rpm === 0) return 0
  return Math.min(100, Math.round((quota.value.current_usage.rpm / quota.value.effective.rpm) * 100))
})

const concPercentage = computed(() => {
  if (!quota.value || quota.value.effective.max_concurrency === 0) return 0
  return Math.min(100, Math.round((quota.value.current_usage.max_concurrency / quota.value.effective.max_concurrency) * 100))
})

function getProgressColor(percentage: number): string {
  if (percentage > 80) return '#f56c6c'  // 红色
  if (percentage > 60) return '#e6a23c'  // 黄色
  return '#67c23a'  // 绿色
}

async function loadQuota() {
  loading.value = true
  try {
    const res = await getMyQuota()
    quota.value = (res as any)?.data || null
  } catch {
    quota.value = null
  } finally {
    loading.value = false
  }
}

function sourceLabel(source: string): string {
  const map: Record<string, string> = {
    default_user: '用户默认',
    default_service_key: 'Service Key 默认',
    override: '自定义',
    unset: '未设置（不限）',
  }
  return map[source] || source
}

function sourceTagType(source: string): string {
  switch (source) {
    case 'override': return 'success'      // 绿色
    case 'default_user': return ''         // 蓝色（默认）
    case 'default_service_key': return ''   // 蓝色（默认）
    case 'unset': return 'info'            // 灰色
    default: return 'info'
  }
}

onMounted(loadQuota)
</script>

<style scoped>
.my-quota-page {
  padding: 24px;
  max-width: 640px;
  margin: 0 auto;
}
.page-header {
  margin-bottom: 24px;
}
.page-title {
  font-size: 20px;
  font-weight: 600;
  margin: 0 0 4px;
  color: var(--el-text-color-primary);
}
.page-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 0;
}
.quota-card {
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  overflow: hidden;
}
.card-header {
  padding: 16px 20px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.card-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.card-icon {
  display: flex;
  align-items: center;
  color: var(--el-color-primary);
}
.card-title {
  font-size: 15px;
  font-weight: 600;
  margin: 0;
}
.card-body {
  padding: 20px;
}
.quota-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  margin-bottom: 20px;
}
.quota-item {
  padding: 16px;
  background: var(--el-fill-color-light);
  border-radius: 8px;
  text-align: center;
}
.quota-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}
.quota-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  margin-bottom: 8px;
}
.quota-source {
  font-size: 12px;
}
.usage-section {
  margin-top: 20px;
  padding: 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 8px;
}
.usage-title {
  font-size: 14px;
  font-weight: 600;
  margin: 0 0 16px;
  color: var(--el-text-color-primary);
}
.usage-grid {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.usage-item {
  padding: 12px;
  background: #fff;
  border-radius: 6px;
}
.usage-label {
  font-size: 13px;
  color: var(--el-text-color-regular);
  margin-bottom: 8px;
}
.quota-info {
  margin-top: 16px;
}
.quota-note {
  margin-top: 16px;
}
.state-empty {
  text-align: center;
  padding: 60px;
  color: var(--el-text-color-secondary);
}

@media (max-width: 480px) {
  .quota-grid {
    grid-template-columns: 1fr;
  }
}
</style>
