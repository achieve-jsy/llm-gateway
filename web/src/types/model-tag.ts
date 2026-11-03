export interface ModelTag {
  id: string
  name: string
  color: string
  description: string
  created_at: string
  updated_at: string
}

export interface ModelTagPayload {
  name: string
  color?: string
  description?: string
}
