<template>
  <div class="notification-history-page">
    <div class="page-header">
      <h2 class="page-title">通知历史</h2>
    </div>

    <div class="filter-bar">
      <el-select
        v-model="filterCategory"
        placeholder="按分类筛选"
        clearable
        style="width: 160px"
        @change="handleCategoryChange"
      >
        <el-option
          v-for="c in categoryOptions"
          :key="c.name"
          :label="c.displayName"
          :value="c.name"
        />
      </el-select>
      <el-select
        v-model="filterType"
        placeholder="按类型筛选"
        clearable
        filterable
        style="width: 200px"
        @change="handleTypeChange"
      >
        <el-option
          v-for="t in typeOptions"
          :key="t.name"
          :label="t.displayName"
          :value="t.name"
        />
      </el-select>
      <el-select
        v-model="filterChannel"
        placeholder="按渠道筛选"
        clearable
        style="width: 140px"
        @change="handleFilterChange"
      >
        <el-option label="站内信" value="inapp" />
        <el-option label="邮件" value="email" />
        <el-option label="Webhook" value="webhook" />
      </el-select>
      <el-select
        v-model="filterLevel"
        placeholder="按级别筛选"
        clearable
        style="width: 140px"
        @change="handleFilterChange"
      >
        <el-option label="提示" value="info" />
        <el-option label="警告" value="warning" />
        <el-option label="严重" value="critical" />
      </el-select>
      <el-input
        v-model="filterKeyword"
        placeholder="搜索标题 / 内容"
        clearable
        style="width: 200px"
        @change="handleFilterChange"
      />
      <el-input
        v-model="filterUser"
        placeholder="按接收人筛选"
        clearable
        style="width: 200px"
        @change="handleFilterChange"
      />
      <el-date-picker
        v-model="dateRange"
        type="datetimerange"
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        style="width: 360px"
        @change="handleFilterChange"
      />
    </div>

    <!-- ═══════════════════════════════════════════
         桌面表格
    ═══════════════════════════════════════════ -->
    <el-table v-loading="loading" :data="items" border stripe class="desktop-table">
      <el-table-column label="时间" width="170" align="center">
        <template #default="{ row }">
          <span>{{ formatFullTime(row.created_at) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="渠道" width="90" align="center">
        <template #default="{ row }">
          <el-tag :type="channelTag(row.channel)" size="small">{{ channelLabel(row.channel) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="类型" width="150">
        <template #default="{ row }">
          <span>{{ typeLabel(row.notification_type) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="title" label="标题" min-width="180" show-overflow-tooltip />
      <el-table-column label="内容" min-width="220" show-overflow-tooltip>
        <template #default="{ row }">
          <span class="content-cell">{{ row.content || '-' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="接收人" width="150" show-overflow-tooltip>
        <template #default="{ row }">
          <span>{{ recipientLabel(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="级别" width="80" align="center">
        <template #default="{ row }">
          <el-tag v-if="row.level" :type="levelTag(row.level)" size="small">{{ levelLabel(row.level) }}</el-tag>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="投递状态" width="130" align="center">
        <template #default="{ row }">
          <el-tooltip v-if="row.delivery_status" :content="deliveryTooltip(row)" placement="top">
            <el-tag :type="deliveryTag(row.delivery_status)" size="small">
              {{ deliveryLabel(row) }}
            </el-tag>
          </el-tooltip>
          <el-tag v-else :type="row.is_read ? 'info' : 'warning'" size="small">
            {{ row.is_read ? '已读' : '未读' }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>

    <!-- ═══════════════════════════════════════════
         移动端卡片列表
    ═══════════════════════════════════════════ -->
    <div class="mobile-cards" v-if="items.length">
      <div v-for="(row, i) in items" :key="i" class="notif-card">
        <div class="notif-card__header">
          <span class="notif-card__title">{{ row.title }}</span>
          <el-tag :type="channelTag(row.channel)" size="small">{{ channelLabel(row.channel) }}</el-tag>
        </div>
        <div class="notif-card__row">
          <span class="notif-card__label">时间</span>
          <span>{{ formatFullTime(row.created_at) }}</span>
        </div>
        <div class="notif-card__row">
          <span class="notif-card__label">类型</span>
          <span>{{ typeLabel(row.notification_type) }}</span>
        </div>
        <div class="notif-card__row">
          <span class="notif-card__label">内容</span>
          <span>{{ row.content || '-' }}</span>
        </div>
        <div class="notif-card__row">
          <span class="notif-card__label">接收人</span>
          <span>{{ recipientLabel(row) }}</span>
        </div>
        <div class="notif-card__row">
          <span class="notif-card__label">投递状态</span>
          <span>{{ row.delivery_status ? deliveryLabel(row) : (row.is_read ? '已读' : '未读') }}</span>
        </div>
      </div>
    </div>

    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="loadItems"
        @size-change="handleSizeChange"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getAdminNotificationHistory, getNotificationConfigMetadata } from '@/services/control-plane'
import type { AdminNotification } from '@/types/notification'

const items = ref<AdminNotification[]>([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const filterCategory = ref('')
const filterType = ref('')
const filterChannel = ref('')
const filterLevel = ref('')
const filterKeyword = ref('')
const filterUser = ref('')
const dateRange = ref<[Date, Date] | null>(null)

const categoryOptions = ref<Array<{ name: string; displayName: string }>>([])
const typeOptions = ref<Array<{ name: string; displayName: string }>>([])

const CHANNEL_MAP: Record<string, string> = {
  inapp: '站内信',
  email: '邮件',
  webhook: 'Webhook',
}

const LEVEL_MAP: Record<string, string> = {
  info: '提示',
  warning: '警告',
  critical: '严重',
}

function channelLabel(channel: string): string {
  return CHANNEL_MAP[channel] || channel
}

function channelTag(channel: string): 'primary' | 'success' | 'warning' | 'info' | '' {
  const map: Record<string, 'primary' | 'success' | 'warning' | 'info'> = {
    inapp: 'primary',
    email: 'success',
    webhook: 'warning',
  }
  return map[channel] || 'info'
}

function levelLabel(level: string): string {
  return LEVEL_MAP[level] || level
}

function levelTag(level: string): 'info' | 'warning' | 'danger' | '' {
  const map: Record<string, 'info' | 'warning' | 'danger'> = {
    info: 'info',
    warning: 'warning',
    critical: 'danger',
  }
  return map[level] || ''
}

function typeLabel(type: string): string {
  const found = typeOptions.value.find((t) => t.name === type)
  return found?.displayName || type
}

function recipientLabel(row: AdminNotification): string {
  if (row.user_name) return row.user_name
  if (row.user_email) return row.user_email
  return '-'
}

function deliveryLabel(row: AdminNotification): string {
  const map: Record<string, string> = {
    delivered: '已投递',
    failed: '失败',
    pending: '待处理',
  }
  const label = map[row.delivery_status] || row.delivery_status
  return row.attempts > 1 ? `${label}(${row.attempts})` : label
}

function deliveryTag(status: string): 'success' | 'danger' | 'info' | '' {
  const map: Record<string, 'success' | 'danger' | 'info'> = {
    delivered: 'success',
    failed: 'danger',
    pending: 'info',
  }
  return map[status] || 'info'
}

function deliveryTooltip(row: AdminNotification): string {
  if (row.last_error) return `错误: ${row.last_error}`
  return ''
}

function formatFullTime(time: string): string {
  const date = new Date(time)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

async function loadItems() {
  loading.value = true
  try {
    const params: Record<string, string | number> = {
      page: currentPage.value,
      page_size: pageSize.value,
    }
    if (filterCategory.value) params.category = filterCategory.value
    else if (filterType.value) params.type = filterType.value
    if (filterChannel.value) params.channel = filterChannel.value
    if (filterLevel.value) params.level = filterLevel.value
    if (filterKeyword.value) params.keyword = filterKeyword.value
    if (filterUser.value) params.user = filterUser.value
    if (dateRange.value) {
      params.start_time = dateRange.value[0].toISOString()
      params.end_time = dateRange.value[1].toISOString()
    }

    const result = await getAdminNotificationHistory(params)
    items.value = result.items
    total.value = result.total
  } catch {
    ElMessage.error('加载通知历史失败')
  } finally {
    loading.value = false
  }
}

async function loadTypes() {
  try {
    const metadata = await getNotificationConfigMetadata()
    categoryOptions.value = metadata.categories || []
    typeOptions.value = metadata.types
  } catch {
    categoryOptions.value = []
    typeOptions.value = []
  }
}

function handleCategoryChange() {
  // 分类与类型是同一筛选维度的粗细粒度：选分类时清空类型。
  if (filterCategory.value) filterType.value = ''
  currentPage.value = 1
  loadItems()
}

function handleTypeChange() {
  // 选类型时清空分类，二者互斥。
  if (filterType.value) filterCategory.value = ''
  currentPage.value = 1
  loadItems()
}

function handleFilterChange() {
  currentPage.value = 1
  loadItems()
}

function handleSizeChange() {
  currentPage.value = 1
  loadItems()
}

onMounted(() => {
  Promise.all([loadTypes(), loadItems()])
})
</script>

<style scoped>
.notification-history-page {
  padding: 0;
}

.page-header {
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0;
}

.filter-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.content-cell {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

/* Mobile responsive */
@media (max-width: 768px) {
  .notification-history-page {
    overflow-x: auto;
  }

  .page-title {
    font-size: 16px;
  }

  .filter-bar {
    flex-direction: column;
    align-items: stretch;
  }

  .filter-bar .el-input,
  .filter-bar .el-select,
  .filter-bar .el-date-picker,
  .filter-bar :deep(.el-date-editor--datetimerange) {
    width: 100% !important;
  }

  .desktop-table {
    display: none;
  }

  .mobile-cards {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .pagination-wrapper {
    justify-content: center;
  }
}

.mobile-cards {
  display: none;
}

.notif-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.notif-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.notif-card__title {
  font-weight: 600;
  font-size: 14px;
  color: var(--el-text-color-primary);
  word-break: break-all;
}

.notif-card__row {
  display: flex;
  gap: 10px;
  font-size: 13px;
  line-height: 1.5;
}

.notif-card__label {
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
  min-width: 60px;
}
</style>
