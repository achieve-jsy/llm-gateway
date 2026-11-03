export interface User {
  id: string
  username: string
  email: string
  display_name: string
  avatar_url: string
  role: string
}

export interface LoginResponse {
  access_token: string
  refresh_token: string
  expires_in: number
  user: User
}
