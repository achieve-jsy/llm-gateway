'use client'

import { usePathname } from 'next/navigation'
import { Avatar, Dropdown, Button, Tooltip } from '@douyinfe/semi-ui'
import { IconMoon, IconSun, IconExit } from '@douyinfe/semi-icons'
import { useAuth } from '@/lib/auth-context'
import { useTheme } from '@/hooks/useTheme'

const PAGE_META: Record<string, { title: string; desc: string }> = {
  '/home':                 { title: '概览',          desc: '系统运行状态总览' },
  '/providers':            { title: '渠道管理',       desc: '上游 LLM API 渠道配置' },
  '/models':               { title: '模型管理',       desc: '模型注册与健康状态' },
  '/groups':               { title: '模型组',         desc: '路由策略与负载均衡' },
  '/tags':                 { title: '标签管理',       desc: '模型分类标签' },
  '/catalog-sync':         { title: 'Catalog 同步',  desc: '与上游模型目录同步' },
  '/chat-playground':      { title: '对话测试',       desc: '实时测试模型调用效果' },
  '/health-monitor':       { title: '健康监控',       desc: '模型健康检测时间线' },
  '/group-health-monitor': { title: '模型组监控',     desc: '模型组健康检测时间线' },
  '/keys':                 { title: 'API Keys',      desc: '访问密钥与权限管理' },
  '/settings':             { title: '系统设置',       desc: '品牌、认证与全局配置' },
}

export default function Topbar() {
  const pathname = usePathname()
  const { user, logout } = useAuth()
  const { isDark, toggle } = useTheme()

  const meta = Object.entries(PAGE_META).find(([p]) => pathname.startsWith(p))?.[1]
  const displayName = user?.display_name || user?.username || 'Admin'

  return (
    <div className="topbar">
      {/* Left */}
      <div className="topbar-left">
        <span className="topbar-page-title">{meta?.title}</span>
        {meta?.desc && (
          <>
            <span className="topbar-sep" />
            <span className="topbar-page-desc">{meta.desc}</span>
          </>
        )}
      </div>

      {/* Right */}
      <div className="topbar-right">
        <Tooltip content={isDark ? '浅色模式' : '深色模式'}>
          <Button
            icon={isDark ? <IconSun /> : <IconMoon />}
            theme="borderless"
            type="tertiary"
            onClick={toggle}
            style={{ borderRadius: 'var(--radius-md)' }}
          />
        </Tooltip>

        <div className="topbar-divider" />

        <Dropdown
          trigger="click"
          position="bottomRight"
          menu={[
            {
              node: 'item' as const,
              name: displayName,
              type: 'tertiary' as const,
              disabled: true,
              style: { fontFamily: 'var(--font-body)', fontWeight: 600 },
            },
            { node: 'divider' as const },
            {
              node: 'item' as const,
              name: '退出登录',
              icon: <IconExit />,
              type: 'danger' as const,
              onClick: logout,
            },
          ]}
        >
          <div
            style={{
              display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer',
              padding: '5px 10px', borderRadius: 'var(--radius-md)',
              transition: 'background 0.12s',
            }}
            onMouseEnter={e => (e.currentTarget.style.background = 'var(--color-border-subtle)')}
            onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
          >
            <Avatar
              size="small"
              style={{
                background: 'var(--color-primary)',
                fontSize: 12, fontWeight: 700,
                fontFamily: 'var(--font-headline)',
              }}
            >
              {displayName[0].toUpperCase()}
            </Avatar>
            <span style={{
              fontSize: 13, fontWeight: 500,
              fontFamily: 'var(--font-body)',
              color: 'var(--color-text-primary)',
            }}>
              {displayName}
            </span>
          </div>
        </Dropdown>
      </div>
    </div>
  )
}
