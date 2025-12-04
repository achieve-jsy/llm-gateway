export interface User {
  id: string
  name: string
  email: string
  departmentId: string
  departmentName: string
}

export type DeptStatus = 'active' | 'pending_studio' | 'none' | 'unknown'

export interface LoginResponse {
  accessToken: string
  refreshToken: string
  user: User
  role: 'employee' | 'admin'
  dept_status?: DeptStatus
  is_bootstrap_admin?: boolean
}

export interface RefreshResponse {
  accessToken: string
  refreshToken: string
}
