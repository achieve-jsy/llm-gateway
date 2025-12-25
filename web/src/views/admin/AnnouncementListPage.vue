<template>
  <div class="announcement-page">
    <div class="page-header">
      <h2 class="page-title">公告管理</h2>
      <div class="header-actions">
        <el-select v-model="filterStatus" placeholder="状态筛选" clearable style="width: 140px" @change="handleFilterChange">
          <el-option label="草稿" value="draft" />
          <el-option label="已发布" value="published" />
          <el-option label="已归档" value="archived" />
        </el-select>
        <el-button type="primary" @click="openCreateDialog">新建公告</el-button>
      </div>
    </div>

    <el-table :data="announcements" border stripe v-loading="loading">
      <el-table-column label="标题" min-width="200">
        <template #default="{ row }">
          <div class="announce-title-cell">
            <span class="level-tag" :class="`level--${row.level}`">{{ levelLabel(row.level) }}</span>
            <span>{{ row.title }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="90" align="center">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="分类" width="100" align="center">
        <template #default="{ row }">{{ categoryLabel(row.category) }}</template>
      </el-table-column>
      <el-table-column label="排序" width="70" align="center" prop="sort_order" />
      <el-table-column label="创建者" width="120" align="center" prop="created_by" />
      <el-table-column label="创建时间" width="170" align="center">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="280" align="center" fixed="right">
        <template #default="{ row }">
          <el-button text type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
          <el-button
            v-if="row.status === 'draft'"
            text type="success" size="small" @click="handlePublish(row)"
          >发布</el-button>
          <el-button
            v-if="row.status === 'published'"
            text type="warning" size="small" @click="handleArchive(row)"
          >归档</el-button>
          <el-popconfirm
            title="确定删除该公告？"
            confirm-button-text="确定"
            cancel-button-text="取消"
            @confirm="handleDelete(row)"
          >
            <template #reference>
              <el-button text type="danger" size="small">删除</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="loadAnnouncements"
        @size-change="handleSizeChange"
      />
    </div>

    <AnnouncementDialog
      v-model:visible="dialogVisible"
      :announcement="editingAnnouncement"
      @saved="onDialogSaved"
      @close="dialogVisible = false"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getAnnouncements, deleteAnnouncement, publishAnnouncement, archiveAnnouncement } from '@/services/control-plane'
import type { Announcement } from '@/types/announcement'
import AnnouncementDialog from '@/components/admin/AnnouncementDialog.vue'

const announcements = ref<Announcement[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const editingAnnouncement = ref<Announcement | null>(null)
const filterStatus = ref('')
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const statusTypeMap: Record<string, string> = { draft: 'info', published: 'success', archived: '' }
const statusLabelMap: Record<string, string> = { draft: '草稿', published: '已发布', archived: '已归档' }
const levelLabelMap: Record<string, string> = { info: '信息', warning: '警告', critical: '紧急' }
const categoryLabelMap: Record<string, string> = { system: '🔧 系统', feature: '✨ 功能', alert: '📣 通知' }

function statusType(s: string) { return statusTypeMap[s] || 'info' }
function statusLabel(s: string) { return statusLabelMap[s] || s }
function levelLabel(l: string) { return levelLabelMap[l] || l }
function categoryLabel(c: string) { return categoryLabelMap[c] || c }
function formatTime(t: string) { return t ? new Date(t).toLocaleString('zh-CN') : '-' }

async function loadAnnouncements() {
  loading.value = true
  try {
    const result = await getAnnouncements({
      status: filterStatus.value || undefined,
      page: currentPage.value,
      page_size: pageSize.value
    })
    announcements.value = result.data
    total.value = result.total
  } catch {
    ElMessage.error('加载公告列表失败')
  } finally {
    loading.value = false
  }
}

function handleFilterChange() { currentPage.value = 1; loadAnnouncements() }
function handleSizeChange() { currentPage.value = 1; loadAnnouncements() }

function openCreateDialog() {
  editingAnnouncement.value = null
  dialogVisible.value = true
}

function openEditDialog(row: Announcement) {
  editingAnnouncement.value = row
  dialogVisible.value = true
}

function onDialogSaved() {
  dialogVisible.value = false
  loadAnnouncements()
}

async function handlePublish(row: Announcement) {
  try {
    await publishAnnouncement(row.id)
    ElMessage.success('公告已发布')
    loadAnnouncements()
  } catch {
    ElMessage.error('发布失败')
  }
}

async function handleArchive(row: Announcement) {
  try {
    await archiveAnnouncement(row.id)
    ElMessage.success('公告已归档')
    loadAnnouncements()
  } catch {
    ElMessage.error('归档失败')
  }
}

async function handleDelete(row: Announcement) {
  try {
    await deleteAnnouncement(row.id)
    ElMessage.success('删除成功')
    loadAnnouncements()
  } catch {
    ElMessage.error('删除失败')
  }
}

onMounted(() => { loadAnnouncements() })
</script>

<style scoped>
.announce-title-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.level-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  flex-shrink: 0;
}

.level--info { background-color: #E8F6F5; color: #2BA8A2; }
.level--warning { background-color: #FFF3E0; color: #E65100; }
.level--critical { background-color: #FFEBEE; color: #C62828; }

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

/* Mobile responsive */
@media (max-width: 768px) {
  .announcement-page {
    overflow-x: auto;
  }

  .page-header {
    flex-direction: column;
    gap: 10px;
  }

  .header-actions {
    width: 100%;
    flex-wrap: wrap;
    gap: 8px;
  }

  .header-actions .el-select {
    width: 100%;
  }

  .header-actions .el-button {
    width: 100%;
    justify-content: center;
  }

  :deep(.el-table) {
    min-width: 600px;
  }

  .pagination-wrapper {
    justify-content: center;
  }
}
</style>