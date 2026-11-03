'use client'

import { useState, useEffect, useRef } from 'react'
import {
  Tabs, TabPane, Form, Button, Switch, Toast, Input, InputNumber,
  Select, Typography, Divider, Spin, Upload, Avatar, Banner,
} from '@douyinfe/semi-ui'
import { IconSave, IconUpload, IconDownload } from '@douyinfe/semi-icons'
import {
  getSystemSettings, updateSystemSettings, testWebhook,
  exportSettings, importSettings,
} from '@/services/control-plane'
import type { SystemSettings } from '@/types/settings'
import s from '@/styles/page.module.css'

const { Text, Title } = Typography

const WEBHOOK_EVENTS = [
  { label: '健康检测失败', value: 'health_check_failed' },
  { label: '健康检测恢复', value: 'health_check_recovered' },
  { label: 'Key 即将过期', value: 'key_expiring' },
  { label: 'Key 已过期', value: 'key_expired' },
]

export default function SettingsPage() {
  const [form, setForm] = useState<Partial<SystemSettings>>({})
  const [initialForm, setInitialForm] = useState<Partial<SystemSettings>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testingWebhook, setTestingWebhook] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [importing, setImporting] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const formChanged = JSON.stringify(form) !== JSON.stringify(initialForm)

  useEffect(() => {
    getSystemSettings().then(s => {
      setForm(s)
      setInitialForm(s)
    }).catch(() => Toast.error('加载设置失败'))
      .finally(() => setLoading(false))
  }, [])

  function setField<K extends keyof SystemSettings>(key: K, value: any) {
    setForm(prev => ({ ...prev, [key]: value }))
  }

  async function handleSave() {
    setSaving(true)
    try {
      await updateSystemSettings(form as SystemSettings)
      setInitialForm({ ...form })
      Toast.success('设置已保存')
    } catch { Toast.error('保存失败') }
    finally { setSaving(false) }
  }

  async function handleTestWebhook() {
    setTestingWebhook(true)
    try { await testWebhook(); Toast.success('测试通知已发送') }
    catch { Toast.error('发送失败') }
    finally { setTestingWebhook(false) }
  }

  async function handleExport() {
    setExporting(true)
    try {
      const data = await exportSettings()
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `llm-gateway-settings-${new Date().toISOString().slice(0, 10)}.json`
      a.click()
      URL.revokeObjectURL(url)
      Toast.success('已导出')
    } catch { Toast.error('导出失败') }
    finally { setExporting(false) }
  }

  async function handleImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setImporting(true)
    try {
      const text = await file.text()
      await importSettings(JSON.parse(text))
      Toast.success('导入成功，即将刷新...')
      setTimeout(() => window.location.reload(), 800)
    } catch { Toast.error('导入失败，请检查文件格式') }
    finally { setImporting(false); if (fileInputRef.current) fileInputRef.current.value = '' }
  }

  // Logo 处理
  function handleLogoUpload(file: File) {
    const reader = new FileReader()
    reader.onload = e => setField('site_logo', e.target?.result as string)
    reader.readAsDataURL(file)
  }

  if (loading) return (
    <div style={{ display: 'flex', justifyContent: 'center', padding: 60 }}>
      <Spin size="large" />
    </div>
  )

  const webhookEvents = typeof (form as any).webhook_events === 'string'
    ? (form as any).webhook_events.split(',').filter(Boolean)
    : []

  return (
    <div className={s.settingsPage}>
      <div className={s.pageHeader}>
        <div className={s.pageHeaderLeft}>
          <h1 className={s.pageTitle}>系统设置</h1>
          <p className={s.pageDesc}>配置站点品牌、认证方式和全局参数</p>
        </div>
      </div>

      <Tabs type="line">
        {/* ─── 站点品牌 ─── */}
        <TabPane tab="站点品牌" itemKey="brand">
          <div style={{ paddingTop: 20 }}>
            <div className={s.settingsSection}>
              <h3 className={s.sectionTitle}>品牌设置</h3>
              {/* Logo 预览+上传 */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 20 }}>
                <Avatar
                  size="extra-large"
                  src={form.site_logo || undefined}
                  style={{ background: 'var(--color-primary)' }}
                >
                  {!form.site_logo && (form.site_name?.[0] || 'L')}
                </Avatar>
                <div>
                  <label className={s.fieldLabel}>站点 Logo</label>
                  <Button
                    icon={<IconUpload />}
                    size="small"
                    onClick={() => {
                      const input = document.createElement('input')
                      input.type = 'file'
                      input.accept = 'image/*'
                      input.onchange = (e: any) => handleLogoUpload(e.target.files[0])
                      input.click()
                    }}
                  >上传图片</Button>
                  {form.site_logo && (
                    <Button size="small" type="tertiary" theme="borderless"
                      onClick={() => setField('site_logo', '')}
                      style={{ marginLeft: 8 }}>移除</Button>
                  )}
                  <Text type="tertiary" size="small" style={{ display: 'block', marginTop: 4 }}>
                    支持 PNG / JPG / SVG，建议 64×64px 以上
                  </Text>
                </div>
              </div>
              <label className={s.fieldLabel}>站点名称</label>
              <Input value={form.site_name || ''} onChange={v => setField('site_name', v)}
                style={{ marginBottom: 16 }} placeholder="LLM Gateway" />
              <label className={s.fieldLabel}>站点副标题</label>
              <Input value={(form as any).site_subtitle || ''} onChange={v => setField('site_subtitle' as any, v)}
                placeholder="统一管理大模型 API" />
            </div>
          </div>
        </TabPane>

        {/* ─── 接入配置 ─── */}
        <TabPane tab="接入配置" itemKey="access">
          <div style={{ paddingTop: 20 }}>
            <div className={s.settingsSection}>
              <h3 className={s.sectionTitle}>API 接入</h3>
              <label className={s.fieldLabel}>API 基础 URL</label>
              <Input value={(form as any).api_base_url || ''}
                onChange={v => setField('api_base_url' as any, v)}
                style={{ fontFamily: 'var(--font-mono)', marginBottom: 8 }}
                placeholder="留空则使用当前域名" />
              <Text type="tertiary" size="small">
                接入地址预览：{(form as any).api_base_url || (typeof window !== 'undefined' ? window.location.origin : '')}/v1
              </Text>
            </div>
          </div>
        </TabPane>

        {/* ─── 并发管控 ─── */}
        <TabPane tab="并发管控" itemKey="concurrency">
          <div style={{ paddingTop: 20 }}>
            <div className={s.settingsSection}>
              <h3 className={s.sectionTitle}>请求限制</h3>
              <div className={s.twoCol}>
                <div>
                  <label className={s.fieldLabel}>每分钟请求上限</label>
                  <InputNumber value={(form as any).rate_limit_per_minute ?? 0}
                    onChange={v => setField('rate_limit_per_minute' as any, v)}
                    min={0} max={10000} style={{ width: '100%' }} />
                  <Text type="tertiary" size="small">0 = 不限制</Text>
                </div>
                <div>
                  <label className={s.fieldLabel}>单 Key 最大并发</label>
                  <InputNumber value={(form as any).max_concurrency_per_key ?? 0}
                    onChange={v => setField('max_concurrency_per_key' as any, v)}
                    min={0} max={1000} style={{ width: '100%' }} />
                  <Text type="tertiary" size="small">0 = 不限制</Text>
                </div>
              </div>
            </div>
          </div>
        </TabPane>

        {/* ─── OIDC ─── */}
        <TabPane tab="OIDC 单点登录" itemKey="oidc">
          <div style={{ paddingTop: 20 }}>
            <div className={s.settingsSection}>
              <div className={s.sectionHeader}>
                <div className={s.sectionHeaderInfo}>
                  <Title heading={6} style={{ margin: 0 }}>OIDC 配置</Title>
                  <Text type="tertiary" size="small">启用后支持通过第三方 SSO 登录</Text>
                </div>
                <Switch checked={form.oidc_enabled || false} onChange={v => setField('oidc_enabled', v)} />
              </div>
              {form.oidc_enabled && (
                <div className={s.twoCol}>
                  {([
                    { key: 'oidc_issuer_url', label: 'Issuer URL', placeholder: 'https://accounts.example.com' },
                    { key: 'oidc_client_id', label: 'Client ID', placeholder: 'your-client-id' },
                    { key: 'oidc_client_secret', label: 'Client Secret', placeholder: '••••••••', password: true },
                    { key: 'oidc_redirect_url', label: 'Redirect URL', placeholder: 'https://your-domain/auth/callback' },
                  ] as const).map(({ key, label, placeholder, password }: any) => (
                    <div key={key}>
                      <label className={s.fieldLabel}>{label}</label>
                      <Input value={(form as any)[key] || ''} onChange={v => setField(key, v)}
                        mode={password ? 'password' : undefined}
                        style={{ fontFamily: 'var(--font-mono)' }} placeholder={placeholder} />
                    </div>
                  ))}
                  <div style={{ gridColumn: '1 / -1' }}>
                    <label className={s.fieldLabel}>Scopes</label>
                    <Input value={(form as any).oidc_scopes || ''} onChange={v => setField('oidc_scopes' as any, v)}
                      style={{ fontFamily: 'var(--font-mono)' }} placeholder="openid email profile" />
                  </div>
                </div>
              )}
            </div>
          </div>
        </TabPane>

        {/* ─── Webhook ─── */}
        <TabPane tab="Webhook" itemKey="webhook">
          <div style={{ paddingTop: 20 }}>
            <div className={s.settingsSection}>
              <div className={s.sectionHeader}>
                <div className={s.sectionHeaderInfo}>
                  <Title heading={6} style={{ margin: 0 }}>Webhook 通知</Title>
                  <Text type="tertiary" size="small">系统事件时推送通知到指定地址</Text>
                </div>
                <Switch checked={form.webhook_enabled || false} onChange={v => setField('webhook_enabled', v)} />
              </div>
              {form.webhook_enabled && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                  <div>
                    <label className={s.fieldLabel}>Webhook URL</label>
                    <Input value={form.webhook_url || ''} onChange={v => setField('webhook_url', v)}
                      style={{ fontFamily: 'var(--font-mono)' }} placeholder="https://hooks.example.com/..." />
                  </div>
                  <div>
                    <label className={s.fieldLabel}>签名密钥（可选）</label>
                    <Input value={form.webhook_secret || ''} onChange={v => setField('webhook_secret', v)}
                      mode="password" style={{ fontFamily: 'var(--font-mono)' }} />
                  </div>
                  <div>
                    <label className={s.fieldLabel}>触发事件</label>
                    <Select multiple style={{ width: '100%' }}
                      value={webhookEvents}
                      onChange={v => setField('webhook_events' as any, (v as string[]).join(','))}
                      optionList={WEBHOOK_EVENTS} />
                  </div>
                  <Button loading={testingWebhook} onClick={handleTestWebhook} style={{ alignSelf: 'flex-start' }}>
                    发送测试通知
                  </Button>
                </div>
              )}
            </div>
          </div>
        </TabPane>

        {/* ─── 故障转移 ─── */}
        <TabPane tab="故障转移" itemKey="failover">
          <div style={{ paddingTop: 20 }}>
            <div className={s.settingsSection}>
              <h3 className={s.sectionTitle}>默认故障转移配置</h3>
              <div className={s.twoCol}>
                <div>
                  <label className={s.fieldLabel}>最大切换次数</label>
                  <InputNumber value={(form as any).default_max_switches ?? 2}
                    onChange={v => setField('default_max_switches' as any, v)}
                    min={1} max={10} style={{ width: '100%' }} />
                </div>
                <div>
                  <label className={s.fieldLabel}>同模型重试次数</label>
                  <InputNumber value={(form as any).default_same_model_retries ?? 1}
                    onChange={v => setField('default_same_model_retries' as any, v)}
                    min={0} max={5} style={{ width: '100%' }} />
                </div>
                <div>
                  <label className={s.fieldLabel}>冷却时间（秒）</label>
                  <InputNumber value={(form as any).default_failover_cooldown_seconds ?? 60}
                    onChange={v => setField('default_failover_cooldown_seconds' as any, v)}
                    min={10} max={600} style={{ width: '100%' }} />
                  <Text type="tertiary" size="small">故障模型冷却后自动恢复参与路由</Text>
                </div>
              </div>
            </div>
          </div>
        </TabPane>

        {/* ─── 数据管理 ─── */}
        <TabPane tab="数据管理" itemKey="data">
          <div style={{ paddingTop: 20 }}>
            <div className={s.settingsSection}>
              <h3 className={s.sectionTitle}>导出与导入</h3>
              <div className={s.syncCards}>
                <div className={s.syncCard}>
                  <Title heading={6} style={{ marginBottom: 6 }}>导出配置</Title>
                  <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 12 }}>
                    将所有系统配置导出为 JSON 文件
                  </Text>
                  <Button loading={exporting} icon={<IconDownload />} onClick={handleExport}>
                    导出 JSON
                  </Button>
                </div>
                <div className={s.syncCard}>
                  <Title heading={6} style={{ marginBottom: 6 }}>导入配置</Title>
                  <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 12 }}>
                    从 JSON 文件合并导入，导入后页面将自动刷新
                  </Text>
                  <Button loading={importing} icon={<IconUpload />}
                    onClick={() => fileInputRef.current?.click()}>
                    导入 JSON
                  </Button>
                  <input ref={fileInputRef} type="file" accept=".json" style={{ display: 'none' }}
                    onChange={handleImport} />
                </div>
              </div>
            </div>
          </div>
        </TabPane>
      </Tabs>

      {/* 底部保存栏 */}
      <div className={s.actionBar}>
        {formChanged ? (
          <>
            <Text type="warning" size="small" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span className={s.changeDot} style={{ background: 'var(--color-warning)' }} />
              有未保存的更改
            </Text>
            <Button onClick={() => setForm({ ...initialForm })}>重置</Button>
            <Button loading={saving} type="primary" theme="solid" icon={<IconSave />} onClick={handleSave}>
              保存设置
            </Button>
          </>
        ) : (
          <Text type="success" size="small" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span className={s.changeDot} style={{ background: 'var(--color-success)' }} />
            所有更改已保存
          </Text>
        )}
      </div>
    </div>
  )
}
