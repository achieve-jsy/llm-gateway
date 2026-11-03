'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { Button, Input, Select, Toast, Tooltip, Skeleton, Typography, Tag, Space } from '@douyinfe/semi-ui'
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons'
import type { TimelineEntry } from '@/services/control-plane'
import s from '@/styles/page.module.css'

const { Text } = Typography

const PAGE_SIZE = 10
const LOOKBACK_OPTIONS = [
  { label: '6 小时',  value: '6h' },
  { label: '12 小时', value: '12h' },
  { label: '24 小时', value: '24h' },
  { label: '7 天',    value: '7d' },
]

function TimelineBar({ entries }: { entries: TimelineEntry[] }) {
  if (!entries?.length)
    return <Text type="tertiary" size="small">无数据</Text>
  return (
    <div className={s.timelineBar}>
      {entries.slice(-100).map((e, i) => (
        <Tooltip key={i} content={
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>
            {new Date(e.checked_at).toLocaleTimeString('zh-CN')}<br />
            {e.success ? `✓ ${e.response_time_ms}ms` : `✗ ${e.error_message || '失败'}`}
          </span>
        }>
          <div
            className={s.timelineCell}
            style={{
              background: e.success ? 'var(--color-success)' : 'var(--color-error)',
              opacity: e.success ? 0.75 : 1,
            }}
          />
        </Tooltip>
      ))}
    </div>
  )
}

function healthTag(pct: number) {
  if (pct >= 95) return <Tag color="green" size="small">{pct.toFixed(1)}%</Tag>
  if (pct >= 80) return <Tag color="orange" size="small">{pct.toFixed(1)}%</Tag>
  return <Tag color="red" size="small">{pct.toFixed(1)}%</Tag>
}

export interface HealthMonitorCoreProps {
  title: string
  desc: string
  fetchFn: (lookback: string, search: string, offset: number, limit: number) => Promise<{ groups: any[]; total: number }>
  renderMeta: (group: any) => React.ReactNode
  checkAllFn?: () => Promise<any>
}

export default function HealthMonitorCore({ title, desc, fetchFn, renderMeta, checkAllFn }: HealthMonitorCoreProps) {
  const [groups, setGroups] = useState<any[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [checkingAll, setCheckingAll] = useState(false)
  const [lookback, setLookback] = useState('6h')
  const [search, setSearch] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const offsetRef = useRef(0)

  const loadData = useCallback(async (reset: boolean) => {
    if (reset) { setLoading(true); offsetRef.current = 0 }
    else setLoadingMore(true)
    try {
      const res = await fetchFn(lookback, search, offsetRef.current, PAGE_SIZE)
      if (reset) setGroups(res.groups)
      else setGroups(prev => [...prev, ...res.groups])
      setTotal(res.total)
      offsetRef.current += res.groups.length
    } catch { if (reset) Toast.error('加载失败') }
    finally { setLoading(false); setLoadingMore(false) }
  }, [fetchFn, lookback, search])

  useEffect(() => { loadData(true) }, [lookback])

  useEffect(() => {
    timerRef.current = setInterval(() => loadData(true), 60000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [loadData])

  function handleScroll() {
    const el = scrollRef.current
    if (!el || loadingMore || groups.length >= total) return
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 60) loadData(false)
  }

  async function handleCheckAll() {
    if (!checkAllFn) return
    setCheckingAll(true)
    try {
      const r = await checkAllFn()
      Toast.success(`检测完成：${r.success}/${r.total}`)
      loadData(true)
    } catch { Toast.error('检测失败') }
    finally { setCheckingAll(false) }
  }

  return (
    <div>
      <div className={s.pageHeader}>
        <div className={s.pageHeaderLeft}>
          <h1 className={s.pageTitle}>{title}</h1>
          <p className={s.pageDesc}>{desc}</p>
        </div>
        {checkAllFn && (
          <Button loading={checkingAll} icon={<IconRefresh />} onClick={handleCheckAll}>
            立即检测全部
          </Button>
        )}
      </div>

      <div className={s.toolbar}>
        <Input prefix={<IconSearch />} placeholder="搜索名称..."
          value={search}
          onChange={v => { setSearch(v); if (!v) loadData(true) }}
          onEnterPress={() => loadData(true)}
          style={{ width: 220 }} showClear />
        <Select value={lookback} onChange={v => setLookback(v as string)}
          style={{ width: 120 }} optionList={LOOKBACK_OPTIONS} />
        <Button icon={<IconRefresh />} theme="borderless" type="tertiary"
          onClick={() => loadData(true)} loading={loading} />
        <div className={s.toolbarSpacer} />
        <Text type="tertiary" size="small">共 {total} 条</Text>
      </div>

      <div ref={scrollRef} onScroll={handleScroll}
        style={{ maxHeight: 'calc(100vh - 280px)', overflowY: 'auto', scrollbarWidth: 'thin' }}>
        {loading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {[1, 2, 3, 4, 5].map(i => (
              <Skeleton key={i} active loading
                placeholder={<Skeleton.Image style={{ height: 80, borderRadius: 8, width: '100%' }} />} />
            ))}
          </div>
        ) : groups.length === 0 ? (
          <div className={s.emptyState}>
            <div className={s.emptyIcon}>📊</div>
            <div className={s.emptyTitle}>暂无监控数据</div>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {groups.map((g, idx) => {
              const pct = g.health_percent ?? 0
              return (
                <div key={idx} className={s.monitorItem}>
                  <div className={s.monitorInfo}>
                    <Text strong style={{ display: 'block', marginBottom: 4 }}>
                      {g.target_name || g.group_name}
                    </Text>
                    {renderMeta(g)}
                  </div>
                  <div className={s.monitorStat}>
                    {healthTag(pct)}
                    <Text type="tertiary" size="small" style={{ display: 'block', marginTop: 4 }}>
                      {g.success_checks}/{g.total_checks}
                    </Text>
                  </div>
                  <div className={s.monitorTimeline}>
                    <TimelineBar entries={g.timeline ?? []} />
                  </div>
                </div>
              )
            })}
            <div style={{ textAlign: 'center', padding: '12px 0' }}>
              {loadingMore
                ? <Text type="tertiary" size="small">加载更多...</Text>
                : groups.length >= total
                  ? <Text type="tertiary" size="small">已加载全部 {total} 条</Text>
                  : null}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
