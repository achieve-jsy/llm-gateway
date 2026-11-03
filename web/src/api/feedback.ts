import http from '@/services/http'

/**
 * 用户反馈 API（FE↔BE 契约见 tests/contract/contracts.json 的 user_feedback 条目）。
 *
 * 五条端点都挂在既有 adminGroup 之下（/api/v1/feedback*），因此统一带 JWT：
 *   POST  /api/v1/feedback               任意已登录用户提交
 *   GET   /api/v1/feedback               管理员查看（guard PermFeedbackManage）
 *   PATCH /api/v1/feedback/:id           管理员更新状态（guard PermFeedbackManage）
 *   POST  /api/v1/feedback/attachments   上传贴图（multipart，文件字段名固定为 file）
 *   GET   /api/v1/feedback/attachments/:id  读贴图**字节**（二进制直出，仅上传者本人或 PermFeedbackManage）
 *
 * 响应形状：后端常规返回 `{ code, data }` 包裹，这里统一解出 data 后按契约类型返回；
 * 若后端直接返回裸对象（无 code 包裹），`res.data.data ?? res.data` 同样兼容。
 * 附件字节接口是例外：它直接返回图片二进制，没有 JSON 包裹。
 */

/** 反馈类型：功能建议 / 问题反馈 / 使用咨询 / 其他 */
export type FeedbackType = 'suggestion' | 'bug' | 'usage' | 'other'

/**
 * 反馈贴图元数据（POST /attachments 的返回体，也是列表 item.attachments 的元素）。
 * 只含元数据，不含图片字节；字节要另走 fetchFeedbackAttachmentBlob。
 * `feedback_id` 仅出现在列表 item 的附件里（上传成功那一刻还没绑定反馈），故为可选。
 */
export interface FeedbackAttachment {
  id: string
  /** 所属反馈 id；上传接口返回时通常为空，列表返回时必填 */
  feedback_id?: string
  file_name: string
  /** 图片 mime，如 image/webp */
  mime: string
  size_bytes: number
  width: number
  height: number
  created_at: string
}

/** 反馈处理状态：待处理 / 处理中 / 已解决 / 已关闭 */
export type FeedbackStatus = 'pending' | 'processing' | 'resolved' | 'closed'

/** 反馈条目（管理员列表项 / 更新状态返回体），对应表 feedbacks */
export interface FeedbackItem {
  id: string
  type: FeedbackType
  content: string
  /** 联系方式，可为空字符串 */
  contact: string
  status: FeedbackStatus
  user_id: string
  user_name: string
  user_email: string
  created_at: string
  /** 处理时间，未处理时为 null */
  handled_at: string | null
  /** 处理人，未处理时为 null */
  handled_by: string | null
  /** 处理备注，未处理时为 null */
  handle_note: string | null
  /** 贴图附件元数据（不含字节），无附件时为 undefined / 空数组 */
  attachments?: FeedbackAttachment[]
}

/** 提交反馈的入参：type 缺省 suggestion，content 去空格后 5..2000 字符，contact 可选 <=128 字符 */
export interface SubmitFeedbackPayload {
  type: FeedbackType
  content: string
  contact?: string
  /** 已上传成功的贴图 id 列表，最多 3 个（见 uploadFeedbackAttachment） */
  attachment_ids?: string[]
}

/** 提交反馈的返回值 */
export interface SubmitFeedbackResult {
  id: string
  status: FeedbackStatus
  created_at: string
}

/** 管理员列表查询参数 */
export interface ListFeedbackParams {
  status?: string
  type?: string
  page?: number
  page_size?: number
}

/** 管理员列表返回值 */
export interface ListFeedbackResult {
  items: FeedbackItem[]
  total: number
  page: number
  page_size: number
}

/** 管理员更新状态的入参 */
export interface UpdateFeedbackStatusPayload {
  status: FeedbackStatus
  handle_note?: string
}

/**
 * 提交反馈 POST /api/v1/feedback（任意已登录用户）。
 * 成功 201 返回 { id, status, created_at }；content 非法 400 validation_error。
 */
export async function submitFeedback(payload: SubmitFeedbackPayload): Promise<SubmitFeedbackResult> {
  const res = await http.post('/api/v1/feedback', payload)
  return (res.data?.data ?? res.data) as SubmitFeedbackResult
}

/**
 * 管理员查看反馈列表 GET /api/v1/feedback（guard PermFeedbackManage）。
 * 支持 status / type / page / page_size 过滤，返回分页结构。
 */
export async function listFeedback(params: ListFeedbackParams = {}): Promise<ListFeedbackResult> {
  const { status, type, page = 1, page_size = 20 } = params
  const res = await http.get('/api/v1/feedback', { params: { status, type, page, page_size } })
  return (res.data?.data ?? res.data) as ListFeedbackResult
}

/**
 * 管理员更新反馈状态 PATCH /api/v1/feedback/:id（guard PermFeedbackManage）。
 * 状态变为 resolved / closed 时后端写入 handled_at / handled_by / handle_note；
 * 记录不存在返回 404。
 */
export async function updateFeedbackStatus(
  id: string,
  payload: UpdateFeedbackStatusPayload
): Promise<FeedbackItem> {
  const res = await http.patch(`/api/v1/feedback/${id}`, payload)
  return (res.data?.data ?? res.data) as FeedbackItem
}

/**
 * 上传反馈贴图 POST /api/v1/feedback/attachments（任意已登录用户）。
 *
 * multipart/form-data，文件字段名**必须**是 `file`（与后端契约一致），
 * `width` / `height` 为可选整型表单字段（前端压缩后回传真实尺寸，便于后端记录）。
 * 成功 201 返回附件元数据；400（空文件 / >2MiB / 类型不在 png|jpeg|webp|gif 白名单）、
 * 401、500 走 axios 统一错误处理，调用方负责给用户提示。
 */
export async function uploadFeedbackAttachment(
  file: File,
  meta?: { width?: number; height?: number }
): Promise<FeedbackAttachment> {
  const form = new FormData()
  form.append('file', file)
  // 尺寸未知（降级压缩）时不提交，避免后端拿到 0
  if (meta && Number.isFinite(meta.width) && (meta.width as number) > 0) {
    form.append('width', String(Math.round(meta.width as number)))
  }
  if (meta && Number.isFinite(meta.height) && (meta.height as number) > 0) {
    form.append('height', String(Math.round(meta.height as number)))
  }
  const res = await http.post('/api/v1/feedback/attachments', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return (res.data?.data ?? res.data) as FeedbackAttachment
}

/**
 * 读取反馈贴图字节 GET /api/v1/feedback/attachments/:id。
 *
 * ⚠️ 这是二进制直出接口，不是 JSON。鉴权走 Authorization 头（仅上传者本人或
 * PermFeedbackManage 可读），浏览器不会给裸 `<img src>` 带 JWT，因此必须在这里用
 * `responseType: 'blob'` 拉取，再由调用方 `URL.createObjectURL` 生成 blob URL，
 * 并在组件卸载 / 列表刷新时 `URL.revokeObjectURL` 释放。
 */
export async function fetchFeedbackAttachmentBlob(id: string): Promise<Blob> {
  const res = await http.get(`/api/v1/feedback/attachments/${id}`, { responseType: 'blob' })
  return res.data as Blob
}
