export interface User {
  id: string
  name: string
  email: string
  departmentId: string
  departmentName: string
}

export interface LoginResponse {
  accessToken: string
  refreshToken: string
  user: User
  role: 'employee' | 'admin'
}

export interface RefreshResponse {
  accessToken: string
  refreshToken: string
}
