'use client'

import '@douyinfe/semi-ui/react19-adapter'
import React, { createContext, useContext, useEffect, useState, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import type { User } from '@/types/auth'

interface AuthContextValue {
  user: User | null
  accessToken: string | null
  isLoading: boolean
  login: (accessToken: string, refreshToken: string, user: User) => void
  logout: () => void
}

const AuthContext = createContext<AuthContextValue>({
  user: null,
  accessToken: null,
  isLoading: true,
  login: () => {},
  logout: () => {},
})

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter()
  const [user, setUser] = useState<User | null>(null)
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    const token = localStorage.getItem('access_token')
    const userStr = localStorage.getItem('user')
    if (token && userStr) {
      try {
        setAccessToken(token)
        setUser(JSON.parse(userStr))
      } catch {
        localStorage.removeItem('access_token')
        localStorage.removeItem('refresh_token')
        localStorage.removeItem('user')
      }
    }
    setIsLoading(false)
  }, [])

  const login = useCallback((token: string, refreshToken: string, u: User) => {
    localStorage.setItem('access_token', token)
    localStorage.setItem('refresh_token', refreshToken)
    localStorage.setItem('user', JSON.stringify(u))
    setAccessToken(token)
    setUser(u)
  }, [])

  const logout = useCallback(() => {
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    localStorage.removeItem('user')
    setAccessToken(null)
    setUser(null)
    router.push('/login')
  }, [router])

  return (
    <AuthContext.Provider value={{ user, accessToken, isLoading, login, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
