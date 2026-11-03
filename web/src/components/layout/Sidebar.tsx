'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useState, useRef } from 'react'
import {
  IconServer, IconBolt, IconGridSquare, IconFlag, IconRefresh,
  IconComment, IconActivity, IconKey, IconSetting, IconHome,
  IconChevronDown, IconChevronRight,
} from '@douyinfe/semi-icons'

const NAV_GROUPS = [
  {
    label: '核心',
    items: [
      { key: 'home',      text: '概览',      icon: IconHome,        href: '/home' },
      { key: 'providers', text: '渠道管理',   icon: IconServer,      href: '/providers' },
      { key: 'models',    text: '模型管理',   icon: IconBolt,        href: '/models' },
      { key: 'groups',    text: '模型组',     icon: IconGridSquare,  href: '/groups' },
    ],
  },
  {
    label: '运营',
    items: [
      { key: 'keys',            text: 'API Keys',    icon: IconKey,      href: '/keys' },
      { key: 'chat-playground', text: '对话测试',     icon: IconComment,  href: '/chat-playground' },
      { key: 'catalog-sync',    text: 'Catalog 同步', icon: IconRefresh,  href: '/catalog-sync' },
      { key: 'tags',            text: '标签管理',     icon: IconFlag,     href: '/tags' },
    ],
  },
  {
    label: '系统',
    items: [
      {
        key: 'health',
        text: '健康监控',
        icon: IconActivity,
        href: '/health-monitor',
        children: [
          { key: 'health-monitor',       text: '模型监控',   href: '/health-monitor' },
          { key: 'group-health-monitor', text: '模型组监控', href: '/group-health-monitor' },
        ],
      },
      { key: 'settings', text: '系统设置', icon: IconSetting, href: '/settings' },
    ],
  },
]

export default function Sidebar() {
  const pathname = usePathname()
  const [expanded, setExpanded] = useState<Record<string, boolean>>({ health: true })
  const [search, setSearch] = useState('')
  const searchRef = useRef<HTMLInputElement>(null)

  function isActive(href: string) {
    return pathname === href || (href !== '/home' && pathname.startsWith(href))
  }

  function isGroupActive(item: any): boolean {
    if (item.children) return item.children.some((c: any) => isActive(c.href))
    return isActive(item.href)
  }

  // 搜索过滤
  const q = search.trim().toLowerCase()

  return (
    <div className="sidebar">
      {/* Logo */}
      <div className="sidebar-logo">
        <div className="sidebar-logo-mark">G</div>
        <span className="sidebar-logo-name">LLM Gateway</span>
        <span className="sidebar-logo-version">v2</span>
      </div>

      {/* Search */}
      <div className="sidebar-search">
        <div className="sidebar-search-wrap">
          <span className="sidebar-search-icon">⌕</span>
          <input
            ref={searchRef}
            className="sidebar-search-input"
            placeholder="搜索菜单..."
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
        </div>
      </div>

      {/* Nav */}
      <div className="sidebar-nav">
        {NAV_GROUPS.map(group => {
          const filtered = group.items.filter(item => {
            if (!q) return true
            if (item.text.includes(q)) return true
            if ('children' in item && item.children) {
              return item.children.some((c: any) => c.text.includes(q))
            }
            return false
          })
          if (filtered.length === 0) return null

          return (
            <div key={group.label}>
              <div className="nav-group-label">{group.label}</div>
              {filtered.map(item => {
                const Icon = item.icon
                const active = isGroupActive(item)
                const hasChildren = 'children' in item && !!item.children

                if (hasChildren && item.children) {
                  const open = expanded[item.key] ?? false
                  return (
                    <div key={item.key}>
                      <div
                        className={`nav-item ${active ? 'active' : ''}`}
                        onClick={() => setExpanded(p => ({ ...p, [item.key]: !open }))}
                        style={{ userSelect: 'none' }}
                      >
                        <span className="nav-item-icon">
                          <Icon size="small" />
                        </span>
                        <span className="nav-item-text" style={{ flex: 1, fontFamily: 'var(--font-body)' }}>
                          {item.text}
                        </span>
                        <span style={{ color: 'var(--color-text-tertiary)', fontSize: 12 }}>
                          {open
                            ? <IconChevronDown size="extra-small" />
                            : <IconChevronRight size="extra-small" />}
                        </span>
                      </div>
                      {open && item.children.map((child: any) => (
                        <Link key={child.key} href={child.href} style={{ textDecoration: 'none' }}>
                          <div className={`nav-item nav-sub-item ${isActive(child.href) ? 'active' : ''}`}>
                            <span className="nav-sub-dot" />
                            <span className="nav-item-text" style={{ fontFamily: 'var(--font-body)', fontSize: 13 }}>
                              {child.text}
                            </span>
                          </div>
                        </Link>
                      ))}
                    </div>
                  )
                }

                return (
                  <Link key={item.key} href={item.href} style={{ textDecoration: 'none' }}>
                    <div className={`nav-item ${active ? 'active' : ''}`}>
                      <span className="nav-item-icon">
                        <Icon size="small" />
                      </span>
                      <span className="nav-item-text" style={{ fontFamily: 'var(--font-body)' }}>
                        {item.text}
                      </span>
                    </div>
                  </Link>
                )
              })}
            </div>
          )
        })}
      </div>

      {/* Footer */}
      <div className="sidebar-footer">
        <div className="sidebar-status">
          <div className="sidebar-status-dot" />
          <span className="sidebar-status-text">系统运行正常</span>
        </div>
      </div>
    </div>
  )
}
