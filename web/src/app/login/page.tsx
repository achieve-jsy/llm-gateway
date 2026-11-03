'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { Form, Button, Divider, Toast } from '@douyinfe/semi-ui'
import { IconUser, IconLock } from '@douyinfe/semi-icons'
import { useAuth } from '@/lib/auth-context'
import http from '@/lib/http'

export default function LoginPage() {
  const router = useRouter()
  const { login, accessToken } = useAuth()
  const [loading, setLoading] = useState(false)
  const [publicSettings, setPublicSettings] = useState<{
    site_name: string; oidc_enabled: boolean; oidc_provider_name: string
  } | null>(null)

  useEffect(() => {
    if (accessToken) router.replace('/providers')
    http.get('/api/v1/settings/public').then(r => setPublicSettings(r.data.data)).catch(() => {})
  }, [accessToken, router])

  async function handleLogin(values: { username: string; password: string }) {
    setLoading(true)
    try {
      const r = await http.post('/auth/dev/login', values)
      const { accessToken: token, refreshToken, user } = r.data
      login(token, refreshToken, user)
      router.replace('/providers')
    } catch (e: any) {
      Toast.error(e?.response?.data?.error?.message || '用户名或密码错误')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-split">
      {/* 左：品牌面板 */}
      <div className="login-brand-panel">
        <div className="login-brand-content">
          <div className="login-brand-mark">⚡</div>
          <h1 className="login-brand-title">
            {publicSettings?.site_name || 'LLM Gateway'}
          </h1>
          <p className="login-brand-desc">
            统一管理所有上游大模型 API 渠道，让团队安全、高效地接入 AI 能力。
          </p>
          <div className="login-features">
            {[
              '多渠道统一接入，支持 OpenAI / Claude / Gemini 等',
              '模型组路由策略：轮询、优先级、加权',
              '实时健康监控与自动故障转移',
              'API Key 权限隔离，精细化访问控制',
            ].map((f, i) => (
              <div key={i} className="login-feature">
                <span className="login-feature-dot" />
                <span>{f}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* 右：登录表单 */}
      <div className="login-form-panel">
        <div className="login-form-inner">
          <h2 className="login-form-title">登录账号</h2>
          <p className="login-form-sub">请输入您的用户名和密码以继续</p>

          {/* OIDC */}
          {publicSettings?.oidc_enabled && (
            <>
              <Button
                block
                size="large"
                onClick={() => { window.location.href = '/auth/oidc/login' }}
                style={{
                  borderRadius: 'var(--radius-md)',
                  height: 44,
                  fontFamily: 'var(--font-body)',
                  fontWeight: 500,
                  marginBottom: 8,
                  border: '1px solid var(--color-border)',
                  background: 'var(--color-surface)',
                  color: 'var(--color-text-primary)',
                }}
              >
                通过 {publicSettings.oidc_provider_name || 'SSO'} 登录
              </Button>
              <Divider style={{ margin: '20px 0', borderColor: 'var(--color-border)' }}>
                <span style={{
                  fontSize: 12, color: 'var(--color-text-tertiary)',
                  fontFamily: 'var(--font-body)',
                }}>
                  或使用账号密码
                </span>
              </Divider>
            </>
          )}

          <Form onSubmit={handleLogin} layout="vertical">
            <Form.Input
              field="username"
              label="用户名"
              prefix={<IconUser size="small" style={{ color: 'var(--color-text-tertiary)' }} />}
              rules={[{ required: true, message: '请输入用户名' }]}
              placeholder="admin"
              size="large"
            />
            <Form.Input
              field="password"
              label="密码"
              mode="password"
              prefix={<IconLock size="small" style={{ color: 'var(--color-text-tertiary)' }} />}
              rules={[{ required: true, message: '请输入密码' }]}
              placeholder="••••••••"
              size="large"
            />
            <Button
              htmlType="submit"
              block
              theme="solid"
              type="primary"
              size="large"
              loading={loading}
              style={{
                marginTop: 8,
                height: 46,
                borderRadius: 'var(--radius-md)',
                fontFamily: 'var(--font-body)',
                fontWeight: 600,
                fontSize: 15,
                background: 'var(--color-primary)',
                border: 'none',
              }}
            >
              登录
            </Button>
          </Form>

          <p style={{
            textAlign: 'center', marginTop: 24,
            fontSize: 12, color: 'var(--color-text-tertiary)',
            fontFamily: 'var(--font-body)',
          }}>
            LLM Gateway · 安全运营控制台
          </p>
        </div>
      </div>
    </div>
  )
}
