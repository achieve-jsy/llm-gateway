export interface Announcement {
  id: string
  title: string
  content: string
  category: 'system' | 'feature' | 'alert'
  level: 'info' | 'warning' | 'critical'
  display_mode: 'banner' | 'popup'
  status: 'draft' | 'published' | 'archived'
  sort_order: number
  created_by: string
  published_at: string | null
  created_at: string
  updated_at: string
}

export interface AnnouncementPayload {
  title: string
  content: string
  category?: string
  level?: string
  display_mode?: string
  sort_order?: number
  created_by?: string
}

export interface UpdateAnnouncementPayload {
  title?: string
  content?: string
  category?: string
  level?: string
  display_mode?: string
  status?: string
  sort_order?: number
}