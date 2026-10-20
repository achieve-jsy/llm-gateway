<template>
  <el-dialog
    v-model="visible"
    data-test="feedback-dialog"
    title="反馈与建议"
    width="520px"
    append-to-body
    :close-on-click-modal="false"
  >
    <el-form label-width="80px" label-position="left" @submit.prevent>
      <el-form-item label="反馈类型">
        <el-select v-model="form.type" data-test="feedback-type" style="width: 100%">
          <el-option v-for="opt in TYPE_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
      </el-form-item>

      <el-form-item label="反馈内容">
        <el-input
          v-model="form.content"
          data-test="feedback-content"
          type="textarea"
          :rows="4"
          maxlength="2000"
          show-word-limit
          placeholder="描述你遇到的问题或期望的效果，例如：模型广场希望支持按上下文长度筛选"
        />
      </el-form-item>

      <el-form-item label="图片">
        <div class="attachment-field">
          <!-- 已选贴图：上传中 / 成功 / 失败三态 + 逐张移除（移除前 popconfirm 确认） -->
          <div v-if="attachments.length" class="attachment-list" data-test="feedback-attachments">
            <div
              v-for="item in attachments"
              :key="item.uid"
              class="attachment-card"
              :data-test="`feedback-attachment-${item.status}`"
            >
              <img class="attachment-thumb" :src="item.previewUrl" :alt="item.file.name" />

              <div
                v-if="item.status === 'uploading'"
                class="attachment-mask"
                data-test="feedback-attachment-uploading"
              >
                <span class="attachment-spinner" aria-hidden="true" />
                <span class="attachment-mask-text">上传中</span>
              </div>
              <div
                v-else-if="item.status === 'error'"
                class="attachment-mask attachment-mask--error"
                data-test="feedback-attachment-error"
              >
                <span class="attachment-mask-text">上传失败</span>
                <el-button
                  link
                  type="primary"
                  size="small"
                  data-test="feedback-attachment-retry"
                  @click="retryUpload(item)"
                >
                  重试
                </el-button>
              </div>

              <el-popconfirm
                title="移除这张图片？"
                confirm-button-text="移除"
                cancel-button-text="取消"
                width="180"
                @confirm="removeAttachment(item)"
              >
                <template #reference>
                  <button
                    type="button"
                    class="attachment-remove"
                    :title="`移除 ${item.file.name}`"
                    data-test="feedback-attachment-remove"
                  >
                    ×
                  </button>
                </template>
              </el-popconfirm>
            </div>
          </div>

          <!-- 未达上限才显示选择入口，避免用户点了才被告知超限 -->
          <el-upload
            v-if="attachments.length < MAX_ATTACHMENTS"
            ref="uploadRef"
            class="attachment-uploader"
            data-test="feedback-uploader"
            list-type="picture-card"
            accept="image/png,image/jpeg,image/webp,image/gif"
            :auto-upload="false"
            :show-file-list="false"
            :multiple="true"
            :on-change="handleFileChange"
          >
            <div class="attachment-uploader-inner" data-test="feedback-upload-trigger">
              <span class="attachment-uploader-plus" aria-hidden="true">+</span>
              <span class="attachment-uploader-text">选择图片</span>
            </div>
          </el-upload>

          <div class="attachment-tip" data-test="feedback-attachment-tip">
            支持 Ctrl/⌘ + V 直接粘贴截图，最多 {{ MAX_ATTACHMENTS }} 张，单张不超过 2 MiB
          </div>
        </div>
      </el-form-item>

      <el-form-item label="提交身份">
        <!-- 只读回显用户名 + 邮箱，二者都来自 JWT。
             后端 CreateFeedbackRequest 里没有身份字段，提交人也只从校验过的 token 取，
             所以既不需要用户填，也不可能替别人提交。
             不展示 user_id：对提交人没有意义，排障用的 ID 在管理端详情里。 -->
        <div class="feedback-identity" data-test="feedback-identity">
          <span class="feedback-identity-name" data-test="feedback-identity-name">
            {{ accountName || '（未提供用户名）' }}
          </span>
          <span class="feedback-identity-mail" data-test="feedback-identity-email">
            {{ accountEmail || '（未提供邮箱）' }}
          </span>
        </div>
      </el-form-item>
    </el-form>

    <div class="feedback-tip">
      {{ accountEmail
        ? `反馈会提交给模型网关维护团队，处理结果会回复到 ${accountEmail}`
        : '反馈会提交给模型网关维护团队，处理结果会通过你的账号联系你' }}
    </div>

    <template #footer>
      <div class="dialog-footer">
        <el-button @click="visible = false">取消</el-button>
        <el-button
          data-test="feedback-submit"
          type="primary"
          :loading="submitting"
          :disabled="submitting"
          @click="handleSubmit"
        >
          提交
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { UploadFile, UploadInstance } from 'element-plus'
import { submitFeedback, uploadFeedbackAttachment } from '@/api/feedback'
import type { FeedbackType } from '@/api/feedback'
import { compressImage } from '@/utils/image-compress'
import { useAuthStore } from '@/stores/auth'

/** 反馈类型选项（与后端 type 枚举一致） */
const TYPE_OPTIONS: { label: string; value: FeedbackType }[] = [
  { label: '功能建议', value: 'suggestion' },
  { label: '问题反馈', value: 'bug' },
  { label: '使用咨询', value: 'usage' },
  { label: '其他', value: 'other' },
]

/** 贴图张数上限 */
const MAX_ATTACHMENTS = 3
/** 单张图片字节上限：2 MiB（后端同样按此校验） */
const MAX_ATTACHMENT_BYTES = 2 * 1024 * 1024
/** 类型白名单，与后端 png|jpeg|webp|gif 一致 */
const ACCEPTED_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif']

/** 待提交贴图：本地预览 URL + 上传三态 */
interface PendingAttachment {
  uid: string
  /** 压缩后的文件（重试复用同一份，避免重复压缩） */
  file: File
  /** 本地 blob URL（预览用），移除 / 重置时 revoke */
  previewUrl: string
  width: number
  height: number
  status: 'uploading' | 'done' | 'error'
  /** 上传成功后的后端附件 id，提交时进 attachment_ids */
  attachmentId?: string
}

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const submitting = ref(false)
const form = ref({
  type: 'suggestion' as FeedbackType,
  content: '',
})

const auth = useAuthStore()
/**
 * 提交身份三项，全部来自 JWT（经 auth store 解析，非模板硬编码）：
 * user_name / user_email / user_id 后端本来就会随反馈落库，
 * 让用户再手打一遍联系方式纯属白做功，也给不出「替别人提交」这种能力——
 * 提交人只从校验过的 token 取。仅回显，不参与提交载荷。
 */
const accountName = computed(() => (auth.user?.name || '').trim())
const accountEmail = computed(() => (auth.user?.email || '').trim())
const attachments = ref<PendingAttachment[]>([])
let uidSeed = 0

/** 清空已选贴图并释放全部 blob URL，防内存泄漏 */
function releaseAttachments() {
  attachments.value.forEach((item) => URL.revokeObjectURL(item.previewUrl))
  attachments.value = []
}

/** 清空表单，回到初始状态 */
function resetForm() {
  releaseAttachments()
  form.value = { type: 'suggestion', content: '' }
  submitting.value = false
}

// 用户取消 / 点 X 关闭时也清空，避免下次打开看到上一次的残留内容；
// 仅提交失败时保留内容（catch 分支不触发这里，因为失败不改变 modelValue）
watch(visible, (v) => {
  if (v) {
    attachPasteListener()
  } else {
    detachPasteListener()
    resetForm()
  }
}, { immediate: true })

onBeforeUnmount(detachPasteListener)

// ─── 粘贴上传（本需求的核心体验）─────────────────────────────────
// 反馈弹窗是 append-to-body 的模态框，焦点可能停在 textarea、也可能是弹窗标题栏，
// 所以监听挂在 document 上（仅弹窗打开期间挂载），保证「弹窗开着时 Ctrl+V」都能命中。

/** 从剪贴板里挑出图片文件（非图片内容不拦截，让 textarea 正常粘贴文本） */
function extractImageFiles(e: ClipboardEvent): File[] {
  const items = e.clipboardData?.items
  if (!items) return []
  const files: File[] = []
  for (let i = 0; i < items.length; i += 1) {
    const item = items[i]
    if (!item || !item.type || !item.type.startsWith('image/')) continue
    const file = item.getAsFile?.()
    if (file) files.push(file)
  }
  return files
}

function handlePaste(e: ClipboardEvent) {
  const files = extractImageFiles(e)
  if (!files.length) return
  // 命中图片才阻止默认行为，避免浏览器把图片当路径/HTML 插进 textarea
  e.preventDefault()
  void addImageFiles(files)
}

function attachPasteListener() {
  document.addEventListener('paste', handlePaste)
}

function detachPasteListener() {
  document.removeEventListener('paste', handlePaste)
}

// ─── 选文件上传（el-upload，auto-upload=false，统一走 compress + upload 流程）───
const uploadRef = ref<UploadInstance>()

/**
 * el-upload 在 picture-card 模式下会自己给每个文件造一个 object URL 存在内部列表里，
 * 而 show-file-list=false 时这个列表永远不会被清理（弹窗在 App.vue 里常驻，会越积越多）。
 * 我们只用自己管理的 previewUrl，所以这里把 el-upload 内部那份 URL 释放并清空列表项。
 */
function discardUploaderInternalFile(uploadFile: UploadFile | undefined) {
  const url = uploadFile?.url
  if (typeof url === 'string' && url.startsWith('blob:')) URL.revokeObjectURL(url)
  try {
    uploadRef.value?.clearFiles()
  } catch {
    /* 内部列表清理失败不影响上传主流程 */
  }
}

function handleFileChange(uploadFile: UploadFile | undefined) {
  const raw = uploadFile?.raw
  discardUploaderInternalFile(uploadFile)
  if (!raw) return
  void addImageFiles([raw])
}

/** 依次处理一批图片：串行处理让张数上限的判断严格可靠 */
async function addImageFiles(files: File[]) {
  for (const file of files) {
    if (attachments.value.length >= MAX_ATTACHMENTS) {
      ElMessage.warning(`最多只能上传 ${MAX_ATTACHMENTS} 张图片`)
      return
    }
    await addOneImage(file)
  }
}

/** 单张图片：校验类型 → 压缩 → 校验体积 → 占位上传 */
async function addOneImage(raw: File) {
  if (!ACCEPTED_IMAGE_TYPES.includes(raw.type)) {
    ElMessage.error('仅支持 PNG / JPEG / WebP / GIF 格式的图片')
    return
  }

  // compressImage 内部已兜底，任何异常都会原样返回文件
  const compressed = await compressImage(raw)
  const file = compressed.file

  if (file.size > MAX_ATTACHMENT_BYTES) {
    ElMessage.error(`图片超过 2 MiB（${(file.size / 1024 / 1024).toFixed(1)} MiB），请换一张更小的图片`)
    return
  }

  // 压缩是异步的，这里再判一次，避免并发粘贴越过上限
  if (attachments.value.length >= MAX_ATTACHMENTS) {
    ElMessage.warning(`最多只能上传 ${MAX_ATTACHMENTS} 张图片`)
    return
  }

  uidSeed += 1
  const item = reactive<PendingAttachment>({
    uid: `att-${uidSeed}`,
    file,
    previewUrl: URL.createObjectURL(file),
    width: compressed.width,
    height: compressed.height,
    status: 'uploading',
  })
  attachments.value.push(item)
  await uploadItem(item)
}

/** 上传单张：失败置 error（可重试 / 可移除），绝不静默 */
async function uploadItem(item: PendingAttachment) {
  item.status = 'uploading'
  try {
    const meta = item.width > 0 && item.height > 0 ? { width: item.width, height: item.height } : undefined
    const result = await uploadFeedbackAttachment(item.file, meta)
    // 用户已在上传过程中移除，丢弃结果即可（后台会清理未绑定的附件）
    if (!attachments.value.includes(item)) return
    item.attachmentId = result.id
    item.status = 'done'
  } catch {
    if (!attachments.value.includes(item)) return
    item.status = 'error'
    ElMessage.error('图片上传失败，请重试或移除该图片')
  }
}

async function retryUpload(item: PendingAttachment) {
  if (item.status === 'uploading') return
  await uploadItem(item)
}

/** 移除只影响待提交列表：附件在提交反馈前还没和任何反馈绑定，无需调后端删除接口 */
function removeAttachment(item: PendingAttachment) {
  const index = attachments.value.indexOf(item)
  if (index === -1) return
  attachments.value.splice(index, 1)
  URL.revokeObjectURL(item.previewUrl)
}

async function handleSubmit() {
  // 提交中禁止重复提交
  if (submitting.value) return

  // 图片还在上传：明确拦下，避免「点提交时图丢了」
  if (attachments.value.some((a) => a.status === 'uploading')) {
    ElMessage.warning('图片正在上传，请稍候再提交')
    return
  }
  // 有上传失败的图片：拦下并要求重试/移除，避免静默丢图
  if (attachments.value.some((a) => a.status === 'error')) {
    ElMessage.error('有图片上传失败，请重试或移除后再提交')
    return
  }

  const content = form.value.content.trim()
  if (content.length < 5) {
    ElMessage.warning('反馈内容至少 5 个字，请补充说明')
    return
  }

  const attachmentIds = attachments.value
    .map((a) => a.attachmentId)
    .filter((id): id is string => Boolean(id))

  submitting.value = true
  try {
    await submitFeedback({
      type: form.value.type,
      content,
      // 不再提交 contact：提交人身份（user_name / user_email / user_id）由后端从 token 落库，
      // 这是唯一来源，前端再传一份只会产生可能不一致的第二份数据
      ...(attachmentIds.length ? { attachment_ids: attachmentIds } : {}),
    })
    ElMessage.success('反馈已提交，感谢你的反馈')
    // 成功后关闭弹窗并清空表单（含贴图 blob URL）
    emit('update:modelValue', false)
    resetForm()
  } catch (e: any) {
    // 失败保留已填内容、不关闭弹窗，便于用户修改后重试
    ElMessage.error(e?.response?.data?.error?.message || '反馈提交失败，请稍后重试')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
/* 提交身份：只读回显用户名 + 邮箱，不给输入框 */
.feedback-identity {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
  font-size: 13px;
  line-height: 1.6;
}
.feedback-identity-name {
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.feedback-identity-mail {
  color: var(--el-text-color-regular);
  word-break: break-all;
}
.feedback-tip {
  margin-top: 4px;
  padding-left: 80px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}

/* ─── 贴图区 ─────────────────────────────────── */
.attachment-field {
  width: 100%;
}

.attachment-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}

.attachment-card {
  position: relative;
  width: 80px;
  height: 80px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  overflow: hidden;
  background-color: var(--el-fill-color-light);
}

.attachment-thumb {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.attachment-mask {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  /* 与 el-loading 同源的遮罩色，浅色/暗色下自动反相，保证文字可读 */
  background-color: var(--el-mask-color);
  color: var(--el-text-color-primary);
}

.attachment-mask--error {
  color: var(--el-color-danger);
}

.attachment-mask-text {
  font-size: 12px;
}

.attachment-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--el-border-color);
  border-top-color: var(--el-color-primary);
  border-radius: 50%;
  animation: attachment-spin 0.8s linear infinite;
}

@keyframes attachment-spin {
  to {
    transform: rotate(360deg);
  }
}

.attachment-remove {
  position: absolute;
  top: 0;
  right: 0;
  width: 18px;
  height: 18px;
  line-height: 1;
  padding: 0;
  border: none;
  border-radius: 0 0 0 4px;
  cursor: pointer;
  font-size: 14px;
  color: var(--el-color-white);
  background-color: var(--el-color-danger);
}

.attachment-remove:hover {
  background-color: var(--el-color-danger-dark-2);
}

.attachment-uploader-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  color: var(--el-text-color-secondary);
}

.attachment-uploader-plus {
  font-size: 20px;
  line-height: 1;
}

.attachment-uploader-text {
  font-size: 12px;
}

.attachment-tip {
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}

/* 只保留一个 80x80 的「picture-card」入口，去掉 el-upload 默认的虚线大格子 */
:deep(.attachment-uploader .el-upload--picture-card) {
  width: 80px;
  height: 80px;
  border-radius: 6px;
  border: 1px dashed var(--el-border-color);
  background-color: var(--el-fill-color-blank);
}

:deep(.attachment-uploader .el-upload--picture-card:hover) {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}
</style>
