'use client'

import { getGroupHealthTimeline, checkAllGroups } from '@/services/control-plane'
import HealthMonitorCore from '../health-monitor/HealthMonitorCore'

const STRATEGY_MAP: Record<string, { label: string; color: string }> = {
  round_robin: { label: '轮询',  color: 'var(--color-primary)' },
  priority:    { label: '优先级', color: 'var(--color-success)' },
  weighted:    { label: '加权',  color: 'var(--color-warning)' },
}

export default function GroupHealthMonitorPage() {
  return (
    <HealthMonitorCore
      title="模型组监控"
      desc="模型组健康检测时间线，每 60 秒自动刷新"
      fetchFn={getGroupHealthTimeline}
      checkAllFn={checkAllGroups}
      renderMeta={(g) => {
        const s = STRATEGY_MAP[g.strategy] || { label: g.strategy || '—', color: 'var(--color-text-tertiary)' }
        return (
          <div style={{ display: 'flex', gap: 8, fontSize: 12, fontFamily: 'var(--font-body)', alignItems: 'center' }}>
            <span style={{
              padding: '1px 6px', borderRadius: '3px',
              background: s.color + '18', color: s.color, fontWeight: 500,
            }}>{s.label}</span>
            <span style={{ color: 'var(--color-text-tertiary)' }}>{g.model_count ?? 0} 模型</span>
          </div>
        )
      }}
    />
  )
}
