<template>
  <div class="vtt">
    <div v-if="!controlled" class="vtt-toolbar">
      <div class="vtt-filters">
        <el-select
          v-model="localStatus"
          clearable
          placeholder="任务状态"
          style="width: 140px"
          @change="reload"
        >
          <el-option label="排队中" value="submitted" />
          <el-option label="生成中" value="running" />
          <el-option label="成功" value="succeeded" />
          <el-option label="失败" value="failed" />
        </el-select>
        <el-input
          v-model="localModel"
          clearable
          placeholder="模型"
          style="width: 200px"
          @clear="reload"
          @keyup.enter="reload"
        />
        <el-button type="primary" :loading="loading" @click="reload">查询</el-button>
      </div>
    </div>

    <el-table
      v-loading="loading"
      :data="rows"
      stripe
      style="width: 100%"
      :header-cell-style="TABLE_HEADER"
      empty-text="暂无视频任务"
    >
      <el-table-column label="时间" min-width="160">
        <template #default="{ row }">{{ fmtDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="任务 ID" prop="task_id" min-width="180" show-overflow-tooltip />
      <el-table-column label="Key" min-width="110" show-overflow-tooltip>
        <template #default="{ row }">{{ resolveKeyLabel(row) }}</template>
      </el-table-column>
      <el-table-column v-if="!mine" label="用户" min-width="100" show-overflow-tooltip>
        <template #default="{ row }">{{ row.user_name || '—' }}</template>
      </el-table-column>
      <el-table-column label="模型组" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ resolveGroupAndModel(row).groupName || '—' }}</template>
      </el-table-column>
      <el-table-column label="模型" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">{{ resolveGroupAndModel(row).modelName || '—' }}</template>
      </el-table-column>
      <el-table-column label="渠道" prop="provider_name" min-width="120" show-overflow-tooltip />
      <el-table-column label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="生成耗时" width="110">
        <template #default="{ row }">{{ formatGenDuration(row) }}</template>
      </el-table-column>
      <el-table-column label="详情" width="80" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openDetail(row)">查看</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="total > 0" class="vtt-pagination">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        small
        @current-change="load"
        @size-change="onSizeChange"
      />
    </div>

    <VideoTaskDetail v-model="detailVisible" :task="detailTask" :mine="mine" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { videoTasksAPI, type VideoTask } from '@/api/videoTasks'
import VideoTaskDetail from '@/components/usage/VideoTaskDetail.vue'
import { fmtDateTime } from '@/utils/time'
import { resolveGroupAndModel, resolveKeyLabel } from '@/utils/videoTaskDisplay'

const props = withDefaults(defineProps<{
  refreshToken?: number
  mine?: boolean
  /** 由页级筛选条控制时隐藏表内筛选项 */
  controlled?: boolean
  status?: string
  modelName?: string
  providerName?: string
}>(), {
  mine: false,
  controlled: false,
})

const loading = ref(false)
const rows = ref<VideoTask[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const localStatus = ref('')
const localModel = ref('')

const detailVisible = ref(false)
const detailTask = ref<VideoTask | null>(null)

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
}

function statusType(status: string): 'info' | 'warning' | 'success' | 'danger' {
  if (status === 'succeeded') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'warning'
  return 'info'
}

function formatGenDuration(row: VideoTask): string {
  if (!row.updated_at || !row.created_at) return '—'
  if (row.status !== 'succeeded' && row.status !== 'failed') return '—'
  const ms = new Date(row.updated_at).getTime() - new Date(row.created_at).getTime()
  if (!Number.isFinite(ms) || ms < 0) return '—'
  const sec = ms / 1000
  if (sec < 60) return `${sec.toFixed(1)}s`
  return `${Math.floor(sec / 60)}m${Math.round(sec % 60)}s`
}

function openDetail(row: VideoTask) {
  detailTask.value = { ...row }
  detailVisible.value = true
}

async function load() {
  loading.value = true
  try {
    const status = props.controlled ? (props.status || undefined) : (localStatus.value || undefined)
    const modelName = props.controlled ? (props.modelName || undefined) : (localModel.value || undefined)
    const res = await videoTasksAPI.list({
      page: page.value,
      page_size: pageSize.value,
      status,
      model_name: modelName,
      provider_name: props.controlled ? (props.providerName || undefined) : undefined,
    }, { mine: props.mine })
    rows.value = res.data || []
    total.value = res.total || 0
  } catch {
    rows.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function onSizeChange() {
  page.value = 1
  load()
}

watch(
  () => [props.refreshToken, props.status, props.modelName, props.providerName, props.controlled, props.mine],
  () => load(),
)
onMounted(load)

defineExpose({ reload: load })
</script>

<style scoped>
.vtt-toolbar {
  display: flex;
  justify-content: space-between;
  margin-bottom: 12px;
}
.vtt-filters {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.vtt-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
</style>
