'use client'

import { getHealthTimeline, checkAllProviders } from '@/services/control-plane'
import HealthMonitorCore from './HealthMonitorCore'

export default function HealthMonitorPage() {
  return (
    <HealthMonitorCore
      title="健康监控"
      desc="模型健康检测时间线，每 60 秒自动刷新"
      fetchFn={getHealthTimeline}
      checkAllFn={checkAllProviders}
      renderMeta={(g) => (
        <div style={{ display: 'flex', gap: 12, fontSize: 12, color: 'var(--color-text-tertiary)', fontFamily: 'var(--font-body)' }}>
          <span>{g.provider_count ?? 0} 渠道</span>
          <span>{g.key_count ?? 0} 密钥</span>
        </div>
      )}
    />
  )
}
