package service

import "time"

// ── Monitor status constants ──────────────────────────────────────────────────

const (
	MonitorStatusOperational = "operational"
	MonitorStatusDegraded    = "degraded"
	MonitorStatusFailed      = "failed"
	MonitorStatusError       = "error"
)

// ── Provider constants ────────────────────────────────────────────────────────

const (
	MonitorProviderOpenAI    = "openai"
	MonitorProviderAnthropic = "anthropic"
	MonitorProviderGemini    = "gemini"
)

// ── API mode constants ────────────────────────────────────────────────────────

const (
	MonitorAPIModeChatCompletions = "chat_completions"
	MonitorAPIModeResponses       = "responses"
)

// ── Body override mode constants ──────────────────────────────────────────────

const (
	MonitorBodyOverrideModeOff     = "off"
	MonitorBodyOverrideModeMerge   = "merge"
	MonitorBodyOverrideModeReplace = "replace"
)

// ── Timeout & threshold constants ─────────────────────────────────────────────

const (
	monitorRequestTimeout         = 45 * time.Second
	monitorPingTimeout            = 8 * time.Second
	monitorDegradedThreshold      = 6 * time.Second
	monitorHistoryRetentionDays   = 30
	monitorRollupRetentionDays    = 30
	monitorMaintenanceMaxDaysPerRun = 35
	monitorWorkerConcurrency      = 5
	monitorStartupLoadTimeout     = 10 * time.Second
	monitorMinIntervalSeconds     = 15
	monitorMaxIntervalSeconds     = 3600
	monitorMessageMaxBytes        = 500
	monitorResponseMaxBytes       = 64 * 1024
	monitorErrorBodySnippetMaxBytes = 300
	monitorChallengeMin           = 1
	monitorChallengeMax           = 50
	monitorTimelineMaxPoints      = 60
	monitorEndpointResolveTimeout = 5 * time.Second
	monitorAnthropicAPIVersion    = "2023-06-01"
	monitorChallengeMaxTokens     = 50
	monitorRunOneBuffer           = 10 * time.Second
	monitorIdleConnTimeout        = 30 * time.Second
	monitorTLSHandshakeTimeout    = 10 * time.Second
	monitorResponseHeaderTimeout  = 30 * time.Second
	monitorPingDiscardMaxBytes    = 1024
	monitorDialTimeout            = 10 * time.Second
	monitorDialKeepAlive          = 30 * time.Second
)

// ── API paths ─────────────────────────────────────────────────────────────────

const (
	providerOpenAIPath           = "/v1/chat/completions"
	providerOpenAIResponsesPath  = "/v1/responses"
	providerAnthropicPath        = "/v1/messages"
	providerGeminiPathTemplate   = "/v1beta/models/%s:generateContent"
)

// ── History query limits ──────────────────────────────────────────────────────

const (
	MonitorHistoryDefaultLimit = 100
	MonitorHistoryMaxLimit     = 1000
)

// ── Availability windows ──────────────────────────────────────────────────────

const (
	monitorAvailability7Days  = 7
	monitorAvailability15Days = 15
	monitorAvailability30Days = 30
)