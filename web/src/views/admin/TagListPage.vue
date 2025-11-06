<template>
  <div class="tag-page">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <h2 class="page-title">标签管理</h2>
      <div class="header-actions">
        <el-button type="primary" @click="openCreateDialog">新建标签</el-button>
      </div>
    </div>

    <!-- 标签卡片网格 -->
    <div v-if="tags.length" class="tag-grid">
      <div
        v-for="tag in tags"
        :key="tag.id"
        class="tag-card"
        :style="{ borderLeftColor: tag.color }"
      >
        <div class="tag-card-body">
          <div class="tag-card-header">
            <span class="color-dot" :style="{ backgroundColor: tag.color }"></span>
            <span class="tag-name">{{ tag.name }}</span>
          </div>
          <p class="tag-desc">{{ tag.description || '暂无描述' }}</p>
        </div>
        <div class="tag-card-footer">
          <span class="tag-sort">排序: {{ tag.sort_order }}</span>
          <div class="tag-actions">
            <el-button text type="primary" size="small" @click="openEditDialog(tag)">编辑</el-button>
            <el-popconfirm
              title="确定删除该标签？"
              confirm-button-text="确定"
              cancel-button-text="取消"
              @confirm="handleDelete(tag)"
            >
              <template #reference>
                <el-button text type="danger" size="small">删除</el-button>
              </template>
            </el-popconfirm>
          </div>
        </div>
      </div>
    </div>

    <!-- 空状态 -->
    <el-empty v-else description="暂无标签，点击上方按钮创建" />

    <!-- 创建/编辑对话框 -->
    <TagDialog
      v-model:visible="dialogVisible"
      :tag="editingTag"
      @saved="onDialogSaved"
      @close="dialogVisible = false"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getModelTags, deleteModelTag } from '@/services/control-plane'
import type { ModelTag } from '@/types/model-tag'
import TagDialog from '@/components/admin/TagDialog.vue'

const tags = ref<ModelTag[]>([])
const dialogVisible = ref(false)
const editingTag = ref<ModelTag | null>(null)

async function loadTags() {
  try {
    tags.value = await getModelTags()
  } catch {
    ElMessage.error('加载标签列表失败')
  }
}

function openCreateDialog() {
  editingTag.value = null
  dialogVisible.value = true
}

function openEditDialog(tag: ModelTag) {
  editingTag.value = tag
  dialogVisible.value = true
}

async function handleDelete(tag: ModelTag) {
  try {
    await deleteModelTag(tag.id)
    ElMessage.success('删除成功')
    loadTags()
  } catch {
    ElMessage.error('删除失败')
  }
}

function onDialogSaved() {
  dialogVisible.value = false
  loadTags()
}

onMounted(() => {
  loadTags()
})
</script>

<style scoped>
.tag-page {
  padding: 0;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0;
}

.header-actions {
  display: flex;
  align-items: center;
}

.tag-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
}

.tag-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-left: 4px solid #409EFF;
  border-radius: 10px;
  padding: 14px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  transition: transform 0.2s, box-shadow 0.2s;
}

.tag-card:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 16px rgba(0, 128, 128, 0.08);
}

.tag-card-body {
  flex: 1;
}

.tag-card-header {
  display: flex;
  align-items: center;
  margin-bottom: 6px;
}

.color-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  margin-right: 8px;
  flex-shrink: 0;
}

.tag-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.tag-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin: 0 0 10px;
  line-height: 1.5;
}

.tag-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 8px;
}

@media (max-width: 900px) {
  .page-header {
    align-items: flex-start;
    flex-direction: column;
    gap: 12px;
  }
}

.tag-sort {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.tag-actions {
  display: flex;
  gap: 4px;
}
</style>
