'use client'

import { Suspense, useEffect } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { Spin, Typography, Toast } from '@douyinfe/semi-ui'
import { useAuth } from '@/lib/auth-context'
import http from '@/lib/http'

const { Text } = Typography

function CallbackContent() {
  const router = useRouter()
  const params = useSearchParams()
  const { login } = useAuth()

  useEffect(() => {
    const code = params.get('code')
    const state = params.get('state')
    if (!code) {
      Toast.error('OAuth 回调参数缺失')
      router.replace('/login')
      return
    }
    http.post('/auth/oidc/callback', { code, state })
      .then(r => {
        const { access_token, refresh_token, user } = r.data
        login(access_token, refresh_token, user)
        router.replace('/providers')
      })
      .catch(() => {
        Toast.error('登录失败，请重试')
        router.replace('/login')
      })
  }, [params, login, router])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16 }}>
      <Spin size="large" />
      <Text type="tertiary">正在完成登录...</Text>
    </div>
  )
}

export default function AuthCallbackPage() {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}>
      <Suspense fallback={<Spin size="large" />}>
        <CallbackContent />
      </Suspense>
    </div>
  )
}
