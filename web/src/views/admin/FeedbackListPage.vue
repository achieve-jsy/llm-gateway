<template>
  <div class="feedback-page" data-test="feedback-list-page">
    <div class="page-header">
      <div class="header-info">
        <h2 class="page-title">反馈管理</h2>
        <p class="page-desc">查看员工提交的功能建议、问题反馈与使用咨询，并跟进处理状态</p>
      </div>
      <div class="header-actions">
        <el-select
          v-model="filterStatus"
          placeholder="状态"
          clearable
          class="toolbar-field-sm"
          data-test="feedback-filter-status"
          @change="handleFilterChange"
        >
          <el-option v-for="opt in STATUS_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
        <el-select
          v-model="filterType"
          placeholder="类型"
          clearable
          class="toolbar-field-sm"
          data-test="feedback-filter-type"
          @change="handleFilterChange"
        >
          <el-option v-for="opt in TYPE_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
        <el-tooltip content="刷新" placement="top">
          <el-button :icon="Refresh" circle :loading="loading" data-test="feedback-refresh" @click="loadFeedbacks" />
        </el-tooltip>
      </div>
    </div>

    <el-table
      :data="feedbacks"
      v-loading="loading"
      border
      stripe
      row-key="id"
      class="feedback-table"
      data-test="feedback-table"
      @row-click="openDetail"
    >
      <el-table-column label="提交时间" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="类型" width="100" align="center">
        <template #default="{ row }">{{ typeLabel(row.type) }}</template>
      </el-table-column>
      <el-table-column label="提交人" width="140">
        <template #default="{ row }">{{ submitterName(row) }}</template>
      </el-table-column>
      <!-- 内容超长省略，点击整行在详情弹窗中查看全文 -->
      <el-table-column label="内容" min-width="260" show-overflow-tooltip>
        <template #default="{ row }">{{ row.content }}</template>
      </el-table-column>
      <!--
        附件列：懒加载的 blob 缩略图（见下方 prefetchThumbnails 取舍说明），点开大图用
        el-image 的 preview-src-list（preview-teleported 避免被表格裁剪）。
        整列在「本页没有任何附件」时直接不渲染，保持表格清爽。
      -->
      <el-table-column v-if="hasAttachments" label="附件" width="150" align="center">
        <template #default="{ row }">
          <div v-if="row.attachments?.length" class="attachment-cell" @click.stop>
            <template v-for="att in row.attachments" :key="att.id">
              <el-image
                v-if="attachmentUrls[att.id]"
                :src="attachmentUrls[att.id]"
                :preview-src-list="previewList(row)"
                :initial-index="previewIndex(row, att.id)"
                preview-teleported
                fit="cover"
                class="attachment-thumb"
                data-test="feedback-attachment-thumb"
              />
              <button
                v-else-if="attachmentErrors[att.id]"
                type="button"
                class="attachment-thumb attachment-thumb--failed"
                title="缩略图加载失败，点击重试"
                data-test="feedback-attachment-thumb-error"
                @click="loadThumbnail(att.id)"
              >
                重试
              </button>
              <span
                v-else
                class="attachment-thumb attachment-thumb--loading"
                title="缩略图加载中"
                data-test="feedback-attachment-thumb-loading"
              />
            </template>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="联系方式" width="180" show-overflow-tooltip>
        <template #default="{ row }">{{ row.contact || row.user_email || '-' }}</template>
      </el-table-column>
      <el-table-column label="状态" width="100" align="center">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="150" align="center" fixed="right">
        <template #default="{ row }">
          <el-button text type="primary" size="small" @click.stop="openHandle(row)">处理</el-button>
          <el-button text size="small" @click.stop="openDetail(row)">查看详情</el-button>
        </template>
      </el-table-column>

      <template #empty>
        <el-empty description="暂无反馈" :image-size="80" />
      </template>
    </el-table>

    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[20, 50]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="loadFeedbacks"
        @size-change="handleSizeChange"
      />
    </div>

    <!-- 详情弹窗：展示反馈全文与处理信息 -->
    <el-dialog v-model="detailVisible" title="反馈详情" width="600px">
      <div v-if="currentFeedback" class="detail-body">
        <div class="detail-row"><span class="detail-label">提交时间</span>{{ formatTime(currentFeedback.created_at) }}</div>
        <div class="detail-row"><span class="detail-label">类型</span>{{ typeLabel(currentFeedback.type) }}</div>
        <div class="detail-row"><span class="detail-label">提交人</span>{{ submitterName(currentFeedback) }}</div>
        <!-- 用户 ID：定位到具体账号用，列表里不占列宽，放详情 -->
        <div class="detail-row">
          <span class="detail-label">用户 ID</span>
          <span class="detail-user-id" data-test="feedback-detail-user-id">{{ currentFeedback.user_id || '-' }}</span>
        </div>
        <div class="detail-row"><span class="detail-label">联系方式</span>{{ currentFeedback.contact || currentFeedback.user_email || '-' }}</div>
        <div class="detail-row">
          <span class="detail-label">状态</span>
          <el-tag :type="statusTagType(currentFeedback.status)" size="small">{{ statusLabel(currentFeedback.status) }}</el-tag>
        </div>
        <div class="detail-row detail-row--block">
          <span class="detail-label">内容</span>
          <div class="detail-content">{{ currentFeedback.content }}</div>
        </div>
        <!-- 附件：详情里给足尺寸看清单张图，点开大图复用表格那套 preview-src-list -->
        <div v-if="currentFeedback.attachments?.length" class="detail-row detail-row--block">
          <span class="detail-label">附件</span>
          <div class="detail-attachments">
            <div
              v-for="att in currentFeedback.attachments"
              :key="att.id"
              class="detail-attachment"
            >
              <el-image
                v-if="attachmentUrls[att.id]"
                :src="attachmentUrls[att.id]"
                :preview-src-list="previewList(currentFeedback)"
                :initial-index="previewIndex(currentFeedback, att.id)"
                preview-teleported
                fit="cover"
                class="detail-attachment-img"
                data-test="feedback-detail-attachment"
              />
              <button
                v-else-if="attachmentErrors[att.id]"
                type="button"
                class="detail-attachment-img detail-attachment-img--failed"
                title="图片加载失败，点击重试"
                data-test="feedback-detail-attachment-error"
                @click="loadThumbnail(att.id)"
              >
                重试
              </button>
              <span
                v-else
                class="detail-attachment-img detail-attachment-img--loading"
                title="图片加载中"
                data-test="feedback-detail-attachment-loading"
              />
              <span class="detail-attachment-name" :title="att.file_name">{{ att.file_name }}</span>
              <span class="detail-attachment-size">{{ attachmentSize(att) }}</span>
            </div>
          </div>
        </div>
        <div v-if="currentFeedback.handled_at" class="detail-row">
          <span class="detail-label">处理时间</span>{{ formatTime(currentFeedback.handled_at) }}
        </div>
        <div v-if="currentFeedback.handled_by" class="detail-row"><span class="detail-label">处理人</span>{{ currentFeedback.handled_by }}</div>
        <div v-if="currentFeedback.handle_note" class="detail-row detail-row--block">
          <span class="detail-label">处理备注</span>
          <div class="detail-content">{{ currentFeedback.handle_note }}</div>
        </div>
      </div>
      <template #footer>
        <el-button @click="detailVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 处理弹窗：选择目标状态并填写处理备注 -->
    <el-dialog v-model="handleVisible" title="处理反馈" width="520px" :close-on-click-modal="false">
      <el-form label-width="80px" label-position="left" @submit.prevent>
        <el-form-item label="目标状态">
          <el-select v-model="handleForm.status" data-test="feedback-handle-status" style="width: 100%">
            <el-option v-for="opt in STATUS_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="处理备注">
          <el-input
            v-model="handleForm.handle_note"
            data-test="feedback-handle-note"
            type="textarea"
            :rows="3"
            maxlength="2000"
            show-word-limit
            placeholder="记录处理结论或回复内容（可选）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="handleVisible = false">取消</el-button>
        <el-button
          type="primary"
          data-test="feedback-handle-submit"
          :loading="submitting"
          :disabled="submitting"
          @click="submitHandle"
        >
          确定
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { fetchFeedbackAttachmentBlob, listFeedback, updateFeedbackStatus } from '@/api/feedback'
import type { FeedbackAttachment, FeedbackItem, FeedbackStatus, FeedbackType } from '@/api/feedback'

/** 状态筛选项（「全部」由 clearable 表达，不额外占位） */
const STATUS_OPTIONS: { label: string; value: FeedbackStatus }[] = [
  { label: '待处理', value: 'pending' },
  { label: '处理中', value: 'processing' },
  { label: '已解决', value: 'resolved' },
  { label: '已关闭', value: 'closed' },
]

/** 类型筛选项，与提交端保持一致 */
const TYPE_OPTIONS: { label: string; value: FeedbackType }[] = [
  { label: '功能建议', value: 'suggestion' },
  { label: '问题反馈', value: 'bug' },
  { label: '使用咨询', value: 'usage' },
  { label: '其他', value: 'other' },
]

const STATUS_LABEL_MAP: Record<string, string> = {
  pending: '待处理',
  processing: '处理中',
  resolved: '已解决',
  closed: '已关闭',
}

const STATUS_TAG_MAP: Record<string, 'primary' | 'success' | 'warning' | 'info' | 'danger'> = {
  pending: 'warning',
  processing: 'primary',
  resolved: 'success',
  closed: 'info',
}

const TYPE_LABEL_MAP: Record<string, string> = {
  suggestion: '功能建议',
  bug: '问题反馈',
  usage: '使用咨询',
  other: '其他',
}

const feedbacks = ref<FeedbackItem[]>([])
const loading = ref(false)
const submitting = ref(false)
const filterStatus = ref<FeedbackStatus | ''>('')
const filterType = ref<FeedbackType | ''>('')
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const detailVisible = ref(false)
const handleVisible = ref(false)
const currentFeedback = ref<FeedbackItem | null>(null)
const handleForm = ref<{ status: FeedbackStatus; handle_note: string }>({
  status: 'processing',
  handle_note: '',
})

function statusLabel(s: string) { return STATUS_LABEL_MAP[s] || s }
function statusTagType(s: string) { return STATUS_TAG_MAP[s] || 'info' }
function typeLabel(t: string) { return TYPE_LABEL_MAP[t] || t }
function formatTime(t: string) { return t ? new Date(t).toLocaleString('zh-CN') : '-' }

/** 提交人：优先显示 user_name，为空时退化显示 user_id 前 8 位 */
function submitterName(row: FeedbackItem) {
  return row.user_name || (row.user_id ? `${row.user_id.slice(0, 8)}…` : '-')
}

// ─── 附件缩略图（二进制直出接口，必须 axios 拉 blob 再 createObjectURL）──────
// 说明：接口要 JWT，裸 <img src> 不带 Authorization，所以只能 blob URL。
// 生命周期：attachmentUrls 里的每个 URL 在「列表刷新后已不再需要」或「页面卸载」时 revoke。

/** attachmentId → blob URL */
const attachmentUrls = ref<Record<string, string>>({})
/** attachmentId → 是否加载失败（失败态在单元格里给可点击重试，不弹全局 toast） */
const attachmentErrors = ref<Record<string, boolean>>({})
/** 列表批次号：刷新后旧批次迟到的响应直接丢弃，避免写入过期状态 / 产生孤儿 blob URL */
let loadSeq = 0

/** 本页是否存在附件：没有任何附件时整列不渲染，不产生占位噪音 */
const hasAttachments = computed(() =>
  feedbacks.value.some((item) => (item.attachments?.length ?? 0) > 0),
)

function collectAttachmentIds(items: FeedbackItem[]): string[] {
  const ids = new Set<string>()
  items.forEach((item) => {
    ;(item.attachments || []).forEach((att) => {
      if (att?.id) ids.add(att.id)
    })
  })
  return [...ids]
}

/** 释放全部 blob URL（页面卸载时调用） */
function releaseAttachmentUrls() {
  Object.values(attachmentUrls.value).forEach((url) => URL.revokeObjectURL(url))
  attachmentUrls.value = {}
  attachmentErrors.value = {}
}

/** 刷新列表后，只释放「新一页里已经不存在」的 blob URL，避免缩略图整体闪一下 */
function pruneAttachmentUrls(items: FeedbackItem[]) {
  const keep = new Set(collectAttachmentIds(items))
  Object.entries(attachmentUrls.value).forEach(([id, url]) => {
    if (keep.has(id)) return
    URL.revokeObjectURL(url)
    delete attachmentUrls.value[id]
  })
  Object.keys(attachmentErrors.value).forEach((id) => {
    if (!keep.has(id)) delete attachmentErrors.value[id]
  })
}

/**
 * 正在请求中的附件 id。
 *
 * 没有这道闸，同一张图会被重复拉取：预取还在飞时点开详情（openDetail 会对未就绪的图补拉）
 * 就会发第二次请求，而两次 createObjectURL 里先建的那个 URL 字符串会被后一次覆盖，
 * 再也不会被 revoke —— 那是实打实的泄漏。去重后所有调用方共享同一次请求。
 */
const inflightThumbnails = new Set<string>()

/** 拉一张缩略图：失败只标记该单元格，可点击重试（不静默，也不刷屏） */
async function loadThumbnail(id: string) {
  if (!id || attachmentUrls.value[id] || inflightThumbnails.has(id)) return
  inflightThumbnails.add(id)
  const seq = loadSeq
  try {
    const blob = await fetchFeedbackAttachmentBlob(id)
    // 列表已刷新：丢弃结果，此时还没建 URL，直接返回不会泄漏
    if (seq !== loadSeq) return
    attachmentUrls.value[id] = URL.createObjectURL(blob)
    delete attachmentErrors.value[id]
  } catch {
    if (seq !== loadSeq) return
    attachmentErrors.value[id] = true
  } finally {
    inflightThumbnails.delete(id)
  }
}

/**
 * 缩略图懒加载取舍：
 * - 不在挂载时全量拉取（那会在首屏一次性打几十个请求）；
 * - 也不用 IntersectionObserver「滚到才拉」——表格在滚动中异步出图/出错，体验与排错都差；
 * - 折中：列表数据到位后异步分批预取，并发上限 3，不阻塞表格渲染；失败即降级为可重试占位。
 *   这样滚动时不会再有网络请求，也就不会「滚着滚着报错」。
 */
async function prefetchThumbnails(items: FeedbackItem[]) {
  const queue = collectAttachmentIds(items).filter(
    (id) => !attachmentUrls.value[id] && !attachmentErrors.value[id],
  )
  const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
    for (;;) {
      const id = queue.shift()
      if (!id) return
      await loadThumbnail(id)
    }
  })
  await Promise.all(workers)
}

/** 该行已加载成功的缩略图 URL 列表，作为 el-image 的 preview-src-list（点开大图） */
function previewList(row: FeedbackItem): string[] {
  return (row.attachments || [])
    .map((att) => attachmentUrls.value[att.id])
    .filter((url): url is string => Boolean(url))
}

/** 当前缩略图在大图列表中的下标，保证点哪张看哪张 */
function previewIndex(row: FeedbackItem, id: string): number {
  const index = previewList(row).indexOf(attachmentUrls.value[id])
  return index < 0 ? 0 : index
}

async function loadFeedbacks() {
  loading.value = true
  const seq = ++loadSeq
  try {
    const result = await listFeedback({
      status: filterStatus.value || undefined,
      type: filterType.value || undefined,
      page: currentPage.value,
      page_size: pageSize.value,
    })
    if (seq !== loadSeq) return
    feedbacks.value = result.items || []
    total.value = result.total || 0
    pruneAttachmentUrls(feedbacks.value)
    // 不 await：缩略图慢慢补，不拖慢列表渲染
    void prefetchThumbnails(feedbacks.value)
  } catch {
    if (seq !== loadSeq) return
    ElMessage.error('加载反馈列表失败')
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

function handleFilterChange() { currentPage.value = 1; loadFeedbacks() }
function handleSizeChange() { currentPage.value = 1; loadFeedbacks() }

/**
 * 附件尺寸信息：宽×高 · 体积（缺的项自动省略）。
 * 详情里光看图不知道多大，管理员排障时要用；文件名单独成行，见模板。
 */
function attachmentSize(att: FeedbackAttachment): string {
  const parts: string[] = []
  if (att.width && att.height) parts.push(`${att.width}×${att.height}`)
  if (att.size_bytes > 0) {
    const kb = att.size_bytes / 1024
    parts.push(kb >= 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${Math.max(1, Math.round(kb))} KB`)
  }
  return parts.join(' · ')
}

/** 点击行/查看详情：打开全文弹窗 */
function openDetail(row: FeedbackItem) {
  currentFeedback.value = row
  detailVisible.value = true
  // 预取是异步分批的，且刷新后会被 prune；详情打开时对还没就绪的图补一次拉取，
  // 避免"表格里能看到、详情里空白"这种不一致（已加载/已失败的会被 loadThumbnail 自己跳过）
  ;(row.attachments || []).forEach((att) => {
    if (!attachmentUrls.value[att.id] && !attachmentErrors.value[att.id]) void loadThumbnail(att.id)
  })
}

/** 打开处理弹窗，默认沿用当前状态，避免误改 */
function openHandle(row: FeedbackItem) {
  currentFeedback.value = row
  handleForm.value = { status: row.status, handle_note: row.handle_note || '' }
  handleVisible.value = true
}

async function submitHandle() {
  if (!currentFeedback.value || submitting.value) return
  submitting.value = true
  try {
    await updateFeedbackStatus(currentFeedback.value.id, {
      status: handleForm.value.status,
      handle_note: handleForm.value.handle_note.trim() || undefined,
    })
    ElMessage.success('反馈状态已更新')
    handleVisible.value = false
    loadFeedbacks()
  } catch {
    ElMessage.error('更新反馈状态失败')
  } finally {
    submitting.value = false
  }
}

onMounted(() => { loadFeedbacks() })

// 页面卸载时释放所有缩略图 blob URL，防内存泄漏
onBeforeUnmount(releaseAttachmentUrls)
</script>

<style scoped>
.feedback-page {
  width: 100%;
}

/* ─── Page Header（与其他管理页一致的结构）──────────── */
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
  gap: 16px;
  flex-wrap: wrap;
}

.header-info {
  flex: 1;
  min-width: 200px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size, 20px);
  font-weight: var(--font-title-weight, 600);
  margin: 0 0 4px 0;
}

.page-desc {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0;
  line-height: 1.5;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.toolbar-field-sm {
  width: 140px;
}

/* ─── 表格 ─────────────────────────────────────── */
.feedback-table {
  margin-bottom: 8px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

/* ─── 附件缩略图 ────────────────────────────────── */
.attachment-cell {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 4px;
  cursor: zoom-in;
}

.attachment-thumb {
  width: 36px;
  height: 36px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  background-color: var(--el-fill-color-light);
  object-fit: cover;
  display: block;
}

.attachment-thumb--loading {
  /* 加载中占位：浅色骨架，暗色下随 --el-fill-color-light 自动反相 */
  background-color: var(--el-fill-color-light);
}

.attachment-thumb--failed {
  padding: 0;
  border-color: var(--el-color-danger-light-5);
  color: var(--el-color-danger);
  font-size: 11px;
  line-height: 1;
  cursor: pointer;
}

/* ─── 详情弹窗 ──────────────────────────────────── */
.detail-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.detail-row {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.detail-row--block {
  align-items: flex-start;
}

/* 详情里的附件：比表格缩略图大得多，够看清截图内容；点开仍是同一套大图预览 */
.detail-attachments {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  min-width: 0;
}
.detail-attachment {
  display: flex;
  flex-direction: column;
  gap: 4px;
  /* 136px 是「1600×918 · 82 KB」在 12px 下不折行的最小宽度 */
  width: 136px;
}
.detail-attachment-img {
  display: block;
  width: 136px;
  height: 96px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  background-color: var(--el-fill-color-light);
  object-fit: cover;
  cursor: zoom-in;
}
.detail-attachment-img--loading {
  cursor: default;
}
.detail-attachment-img--failed {
  padding: 0;
  border-color: var(--el-color-danger-light-5);
  color: var(--el-color-danger);
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
}
.detail-user-id {
  font-family: var(--el-font-family, monospace);
  font-size: 12px;
  word-break: break-all;
}
.detail-attachment-name {
  overflow: hidden;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.detail-attachment-size {
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
}

.detail-label {
  flex-shrink: 0;
  width: 72px;
  color: var(--el-text-color-secondary);
}

.detail-content {
  flex: 1;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.6;
}

/* Mobile responsive */
@media (max-width: 768px) {
  .feedback-page {
    overflow-x: auto;
  }

  .header-actions {
    width: 100%;
    justify-content: flex-start;
  }

  :deep(.el-table) {
    min-width: 900px;
  }

  .pagination-wrapper {
    justify-content: center;
  }
}
</style>
