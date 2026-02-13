export type ChartState = 'loading' | 'empty' | 'ready'

export type {
  OpsSeverity,
  OpsQueryMode,
  AlertEventStatus,
  MetricType,
  Operator,

  OpsOverview,
  OpsSystemMetrics,
  OpsTraffic,
  OpsSLA,
  OpsLatency,
  OpsErrors,
  OpsJobHeartbeat,

  OpsTrafficPoint,
  OpsErrorPoint,
  OpsLatencyBucket,
  OpsErrorDistributionItem,
  OpsSwitchRatePoint,
  OpsThroughputByProvider,
  OpsThroughputByGroup,

  AlertRule,
  AlertEvent,
  AlertSilenceCreate,

  OpsSystemLog,
  OpsSystemLogSinkHealth,
  OpsRuntimeLogConfig,

  OpsMetricThresholds,
  OpsDashboardSettings,
  OpsAdvancedSettings,

  OpsUpstreamContext,
  OpsLatencyBreakdown,

  EmailNotificationConfig,
  OpsAlertRuntimeSettings,
  OpsErrorLogEntry
} from '@/api/ops'
