import http from './http'
import type { LoginResponse, RefreshResponse } from '@/types/auth'

export async function oidcCallback(code: string, state: string): Promise<LoginResponse> {
  const response = await http.post<LoginResponse>('/auth/oidc/callback', { code, state })
  return response.data
}

export async function refreshToken(token: string): Promise<RefreshResponse> {
  const response = await http.post<RefreshResponse>('/auth/refresh', { refreshToken: token })
  return response.data
}

export async function logout(): Promise<void> {
  await http.post('/auth/logout')
}
