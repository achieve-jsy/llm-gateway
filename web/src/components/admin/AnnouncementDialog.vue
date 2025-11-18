<template>
  <el-dialog
    :model-value="visible"
    :title="isEdit ? '编辑公告' : '新建公告'"
    width="min(900px, 92vw)"
    destroy-on-close
    align-center
    @close="emit('close')"
  >
    <el-form
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-width="90px"
      label-position="left"
    >
      <el-form-item label="公告标题" prop="title">
        <el-input v-model="formData.title" placeholder="请输入公告标题" />
      </el-form-item>

      <el-form-item label="公告内容" prop="content">
        <div class="editor-wrap">
          <div class="editor-toolbar">
            <button type="button"
              class="editor-tab"
              :class="{ active: editorMode === 'edit' }"
              @click="editorMode = 'edit'"
            >编辑</button>
            <button type="button"
              class="editor-tab"
              :class="{ active: editorMode === 'preview' }"
              @click="editorMode = 'preview'"
            >预览</button>
            <button type="button"
              class="editor-tab"
              :class="{ active: editorMode === 'split' }"
              @click="editorMode = 'split'"
            >分栏</button>
            <span class="editor-hint">支持 Markdown 语法</span>
          </div>
          <div class="editor-body" :class="`mode--${editorMode}`">
            <textarea
              v-show="editorMode !== 'preview'"
              class="editor-textarea"
              v-model="formData.content"
              placeholder="请输入公告内容，支持 Markdown 语法..."
            ></textarea>
            <div
              v-show="editorMode !== 'edit'"
              class="editor-preview"
              :class="{ 'preview-empty': !formData.content }"
            >
              <div
                v-if="formData.content"
                class="preview-content md-content"
                v-html="renderMarkdown(formData.content)"
              ></div>
              <div v-else class="preview-placeholder">暂无内容，请在左侧输入...</div>
            </div>
          </div>
        </div>
      </el-form-item>

      <el-form-item label="展示方式">
        <div class="mode-selector">
          <div
            class="mode-card"
            :class="{ active: formData.display_mode === 'banner' }"
            @click="formData.display_mode = 'banner'"
          >
            <div class="mode-card-icon">📌</div>
            <div class="mode-card-body">
              <div class="mode-card-title">横幅 Banner</div>
              <div class="mode-card-desc">顶部非阻断横幅，低打扰，适合持续性提示</div>
            </div>
          </div>
          <div
            class="mode-card"
            :class="{ active: formData.display_mode === 'popup' }"
            @click="formData.display_mode = 'popup'"
          >
            <div class="mode-card-icon">💬</div>
            <div class="mode-card-body">
              <div class="mode-card-title">弹出框 Popup</div>
              <div class="mode-card-desc">登录时弹出，需主动关闭，适合必读公告</div>
            </div>
          </div>
        </div>
      </el-form-item>

      <el-form-item label="公告级别">
        <el-select v-model="formData.level" style="width: 100%">
          <el-option label="普通信息" value="info" />
          <el-option label="重要警告" value="warning" />
          <el-option label="紧急通知" value="critical" />
        </el-select>
      </el-form-item>

      <el-form-item label="公告分类">
        <el-select v-model="formData.category" style="width: 100%">
          <el-option label="🔧 系统公告" value="system" />
          <el-option label="✨ 功能更新" value="feature" />
          <el-option label="📣 重要通知" value="alert" />
        </el-select>
      </el-form-item>

      <el-form-item label="创建人" v-if="isEdit && formData.created_by">
        <el-input :value="formData.created_by" disabled />
      </el-form-item>

      <el-form-item label="排序">
        <el-input-number v-model="formData.sort_order" :min="0" :max="9999" style="width: 160px" />
        <span class="form-hint inline">数值越大越靠前</span>
      </el-form-item>

      <el-form-item label="状态" v-if="isEdit">
        <el-select v-model="formData.status" style="width: 100%">
          <el-option label="草稿" value="draft" />
          <el-option label="已发布" value="published" />
          <el-option label="已归档" value="archived" />
        </el-select>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { createAnnouncement, updateAnnouncement } from '@/services/control-plane'
import { renderMarkdown } from '@/utils/markdown'
import type { Announcement } from '@/types/announcement'

interface FormState {
  title: string
  content: string
  category: string
  level: string
  display_mode: string
  sort_order: number
  status: string
  created_by: string
}

const props = defineProps<{
  visible: boolean
  announcement: Announcement | null
}>()
const emit = defineEmits<{
  close: []
  saved: []
  'update:visible': [val: boolean]
}>()

const formRef = ref<FormInstance>()
const submitting = ref(false)
const isEdit = computed(() => !!props.announcement)
const editorMode = ref<'edit' | 'preview' | 'split'>('edit')

const formData = ref<FormState>({
  title: '', content: '', category: 'system', level: 'info', display_mode: 'banner', sort_order: 0, status: 'draft', created_by: ''
})

const resetForm = () => {
  formData.value = { title: '', content: '', category: 'system', level: 'info', display_mode: 'banner', sort_order: 0, status: 'draft', created_by: '' }
  editorMode.value = 'edit'
  formRef.value?.resetFields()
}

// 每次弹窗打开时重新初始化表单
watch(() => props.visible, (visible) => {
  if (!visible) return
  if (props.announcement) {
    const val = props.announcement
    formData.value = {
      title: val.title,
      content: val.content,
      category: val.category || 'system',
      level: val.level,
      display_mode: val.display_mode || 'banner',
      sort_order: val.sort_order,
      status: val.status || 'draft',
      created_by: val.created_by || ''
    }
    editorMode.value = 'edit'
  } else {
    resetForm()
  }
})

const rules: FormRules = {
  title: [{ required: true, message: '请输入公告标题', trigger: 'blur' }],
  content: [{ required: true, message: '请输入公告内容', trigger: 'blur' }]
}

async function handleSubmit() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload = {
      title: formData.value.title,
      content: formData.value.content,
      category: formData.value.category,
      level: formData.value.level,
      display_mode: formData.value.display_mode,
      status: isEdit.value ? formData.value.status : undefined,
      sort_order: formData.value.sort_order
    }
    if (isEdit.value && props.announcement) {
      await updateAnnouncement(props.announcement.id, payload)
      ElMessage.success('更新成功')
    } else {
      await createAnnouncement(payload)
      ElMessage.success('创建成功')
    }
    emit('saved')
  } catch {
    ElMessage.error(isEdit.value ? '更新失败' : '创建失败')
  } finally {
    submitting.value = false
  }
}
</script>

<style>
/* 覆盖弹窗内全局绿色文字 */
.el-dialog .el-form-item__label { color: #374151 !important; }
.el-dialog .el-input__inner,
.el-dialog .el-textarea__inner,
.el-dialog .el-select .el-input__inner { color: #1a1a1a !important; }
.el-dialog .el-radio__label,
.el-dialog .el-checkbox__label { color: #374151 !important; }
.el-dialog .el-dialog__title { color: #1a1a1a !important; }
</style>

<style scoped>
/* ─── 编辑器 ──────────────────────────────── */
.editor-wrap {
  width: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  overflow: hidden;
}

.editor-toolbar {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 6px 10px;
  background: var(--el-fill-color-light);
  border-bottom: 1px solid var(--el-border-color-light);
}

.editor-tab {
  padding: 4px 12px;
  border: none;
  background: transparent;
  border-radius: 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  transition: all 0.15s;
}

.editor-tab:hover { background: var(--el-fill-color); color: var(--el-text-color-primary); }

.editor-tab.active {
  background: #fff;
  color: var(--el-color-primary);
  font-weight: 600;
  box-shadow: 0 1px 4px rgba(0,0,0,0.08);
}

.editor-hint {
  margin-left: auto;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.editor-body {
  display: flex;
  min-height: 300px;
}

.editor-body.mode--edit .editor-textarea { width: 100%; }
.editor-body.mode--preview .editor-preview { width: 100%; }
.editor-body.mode--split .editor-textarea { width: 50%; border-right: 1px solid var(--el-border-color-light); }
.editor-body.mode--split .editor-preview { width: 50%; }

.editor-textarea {
  flex-shrink: 0;
  padding: 14px 16px;
  border: none;
  outline: none;
  resize: vertical;
  font-size: 14px;
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, monospace;
  line-height: 1.7;
  color: #1a1a1a;
  background: #fff;
  min-height: 300px;
}

.editor-preview {
  flex-shrink: 0;
  padding: 14px 16px;
  overflow-y: auto;
  background: var(--el-fill-color-light);
  min-height: 300px;
}

.preview-content { font-size: 14px; color: #334155; }

.preview-placeholder {
  color: var(--el-text-color-placeholder);
  font-size: 14px;
  padding: 20px 0;
}

/* ─── 展示方式卡片 ─────────────────────────── */
.mode-selector { display: flex; gap: 10px; width: 100%; }

.mode-card {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border: 1.5px solid var(--el-border-color);
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.15s;
}

.mode-card:hover { border-color: var(--el-color-primary-light-5); background: var(--el-color-primary-light-9); }
.mode-card.active { border-color: var(--el-color-primary); background: var(--el-color-primary-light-9); }

.mode-card-icon { font-size: 20px; flex-shrink: 0; }
.mode-card-body { flex: 1; min-width: 0; }
.mode-card-title { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); margin-bottom: 2px; }
.mode-card-desc { font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.4; }
</style>