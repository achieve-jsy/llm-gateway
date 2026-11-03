'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { useAuth } from '@/lib/auth-context'
import Sidebar from '@/components/layout/Sidebar'
import Topbar from '@/components/layout/Topbar'
import { Spin } from '@douyinfe/semi-ui'

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const { accessToken, isLoading } = useAuth()
  const router = useRouter()

  useEffect(() => {
    if (!isLoading && !accessToken) router.replace('/login')
  }, [isLoading, accessToken, router])

  if (isLoading) {
    return (
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        height: '100vh', background: '#f8f9fc', flexDirection: 'column', gap: 16,
      }}>
        <div style={{
          width: 48, height: 48, borderRadius: 12,
          background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: 24, boxShadow: '0 8px 20px rgba(99,102,241,0.3)',
        }}>⚡</div>
        <Spin size="middle" />
      </div>
    )
  }

  if (!accessToken) return null

  return (
    <div className="app-shell">
      <Sidebar />
      <div className="main-area">
        <Topbar />
        <main className="page-content">
          {children}
        </main>
      </div>
    </div>
  )
}
