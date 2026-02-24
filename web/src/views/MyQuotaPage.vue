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
                  {{ quota.rpm === 0 ? '不限制' : quota.rpm }}
                </div>
                <div class="quota-source">
                  <el-tag :type="sourceTagType(quota.source)" size="small" effect="light">
                    {{ sourceLabel(quota.source) }}
                  </el-tag>
                </div>
              </div>
              <div class="quota-item">
                <div class="quota-label">最大并发数</div>
                <div class="quota-value">
                  {{ quota.max_concurrency === 0 ? '不限制' : quota.max_concurrency }}
                </div>
                <div class="quota-source">
                  <el-tag :type="sourceTagType(quota.source)" size="small" effect="light">
                    {{ sourceLabel(quota.source) }}
                  </el-tag>
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
                  <span v-if="quota.source === 'default_user'">
                    当前使用全局默认配额（用户默认：{{ quota.default_rpm ?? '-' }} RPM / {{ quota.default_max_concurrency ?? '-' }} 并发）。管理员可为您单独调整。
                  </span>
                  <span v-else-if="quota.source === 'override'">
                    管理员已为您配置专属配额。
                  </span>
                  <span v-else>
                    配额来源：{{ sourceLabel(quota.source) }}
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
        <p>无法加载配额信息</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getMyQuota } from '@/api/quota'
import type { QuotaEffective } from '@/api/quota'

const loading = ref(false)
const quota = ref<QuotaEffective | null>(null)

function sourceLabel(source: string): string {
  const map: Record<string, string> = {
    default_user: '用户默认',
    default_service_key: 'Service Key 默认',
    override: '自定义',
    unset: '未设置（不限）',
  }
  return map[source] || source
}

function sourceTagType(source: string): '' | 'success' | 'warning' | 'info' {
  if (source === 'override') return 'success'
  if (source === 'unset') return 'info'
  return ''
}

onMounted(async () => {
  loading.value = true
  try {
    const res = await getMyQuota()
    quota.value = (res as any)?.data || null
  } catch {
    quota.value = null
  } finally {
    loading.value = false
  }
})
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
  margin-bottom: 16px;
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
.quota-info {
  margin-top: 8px;
}
.quota-note {
  margin-top: 16px;
}
.state-empty {
  text-align: center;
  padding: 60px;
  color: var(--el-text-color-secondary);
}
</style>
