/** 通用 API 响应结构 */
export interface ApiResponse<T> {
  data: T
  total?: number
  message?: string
}

export interface PaginatedResponse<T> {
  data: T[]
  total: number
  page: number
  page_size: number
}
