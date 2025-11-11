/** 模型标签 */
export interface ModelTag {
  id: string
  name: string
  color: string
  description: string
  sort_order: number
  created_at: string
  updated_at: string
}

/** 创建/编辑标签的请求体 */
export interface ModelTagPayload {
  name: string
  color: string
  description?: string
  sort_order?: number
}
