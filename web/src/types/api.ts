/** 通用 API 响应包装 */
export interface ApiResponse<T> {
  /** 状态码（成功时 BE 可能不返回） */
  code?: number
  /** 提示信息（成功时 BE 可能不返回） */
  message?: string
  /** 响应数据 */
  data: T
}

/** 分页响应 */
export interface PaginatedResponse<T> {
  /** 状态码（成功时 BE 可能不返回） */
  code?: number
  /** 提示信息（成功时 BE 可能不返回） */
  message?: string
  /** 数据列表 */
  data: T[]
  /** 总数 */
  total: number
  /** 当前页 */
  page: number
  /** 每页条数 */
  page_size: number
}
