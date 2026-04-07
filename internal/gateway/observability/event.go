package observability

import (
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/gateway/budget"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/google/uuid"
)

// UsageEvent 埋点事件数据契约
// gateway-api 在每次调用完成后构造，异步投递到 Collector
type UsageEvent struct {
	RequestID               string
	Timestamp               time.Time
	BillableReserved        bool
	BillableReservation     *budget.Reservation
	PendingBudgetAccount    *budget.PendingAccount
	BudgetAdmissionFailOpen bool

	// 身份维度（快照）
	KeyID      string
	KeyName    string
	KeyType    string // personal | service，与 api_keys.key_type 对齐
	UserID     string
	UserName   string
	DeptID     string
	DeptName   string
	StudioID   string
	StudioName string

	// 路由/模型
	Model          string
	RoutedModel    string
	GroupName      string
	Provider       string
	RoutedViaGroup bool // 是否经过模型组调度

	// Token 用量
	PromptTokens     int
	CachedTokens     int
	CacheWriteTokens int
	CompletionTokens int
	ReasoningTokens  int
	TotalTokens      int
	UsageMissing     bool

	// Rerank 用量
	SearchUnits float64 // Cohere 按 search_units 计费，默认 1

	// 状态与耗时
	Status          int
	UpstreamStatus  int
	DurationMs      int64
	FirstTokenMs    *int   // 首 token 延迟（毫秒），流式时才有（相对上游建连时刻）
	StageTTFBMs     *int   // 上游 TTFB（毫秒）：从上游请求发出到收到响应头，仅流式
	StageAuthMs     *int16 // auth 阶段耗时（始终记录，不受 log_enabled 影响）
	StageRouteMs    *int16 // 路由阶段耗时
	StageUpstreamMs *int16 // 上游 LLM 阶段耗时
	Stream          bool
	FailoverUsed    bool

	// 请求元信息
	InboundEndpoint string // 入站端点路径，如 /v1/chat/completions
	ReasoningEffort string // low / medium / high
	UserAgent       string
	IPAddress       string

	// 成本（采集时算好并锁价）
	InputPriceSnapshot  float64
	OutputPriceSnapshot float64
	InputCostUSD        float64
	OutputCostUSD       float64
	CacheReadCostUSD    float64
	EstCostUSD          float64

	BudgetTierID                  string
	BudgetTierNameSnapshot        string
	BudgetTierAccountingEligible  bool
	BudgetTierSnapshotRevision    int64
	BudgetTierConstraintsSnapshot []obsmodel.BudgetTierConstraintSnapshot
	BudgetBlockCode               *string
	BudgetBlockDimension          *string
	BudgetBlockScopeType          *string
	BudgetBlockScopeID            *string
	BudgetBlockPeriodType         *string
}

func (e UsageEvent) ToCallLog() (obsmodel.CallLog, error) {
	var tierID *uuid.UUID
	if e.BudgetTierID != "" {
		parsed, err := uuid.Parse(e.BudgetTierID)
		if err != nil {
			return obsmodel.CallLog{}, fmt.Errorf("parse budget tier id: %w", err)
		}
		tierID = &parsed
	}
	return obsmodel.CallLog{
		RequestID: e.RequestID, TS: e.Timestamp, KeyID: e.KeyID, KeyName: e.KeyName,
		KeyType: normalizeKeyType(e.KeyType), UserID: e.UserID, UserName: e.UserName,
		DeptID: e.DeptID, DeptName: e.DeptName, StudioID: e.StudioID, StudioName: e.StudioName,
		Model: e.Model, RoutedModel: e.RoutedModel, GroupName: e.GroupName, Provider: e.Provider,
		PromptTokens: e.PromptTokens, CachedTokens: e.CachedTokens, CacheWriteTokens: e.CacheWriteTokens,
		CompletionTokens: e.CompletionTokens, ReasoningTokens: e.ReasoningTokens, TotalTokens: e.TotalTokens,
		Status: e.Status, UpstreamStatus: e.UpstreamStatus, DurationMs: int(e.DurationMs),
		FirstTokenMs: e.FirstTokenMs, StageTTFBMs: e.StageTTFBMs, StageAuthMs: e.StageAuthMs,
		StageRouteMs: e.StageRouteMs, StageUpstreamMs: e.StageUpstreamMs, Stream: e.Stream,
		FailoverUsed: e.FailoverUsed, RoutedViaGroup: e.RoutedViaGroup, UsageMissing: e.UsageMissing,
		ReasoningEffort: e.ReasoningEffort, UserAgent: e.UserAgent, IPAddress: e.IPAddress,
		InboundEndpoint: e.InboundEndpoint, InputPriceSnapshot: &e.InputPriceSnapshot,
		OutputPriceSnapshot: &e.OutputPriceSnapshot, InputCostUSD: e.InputCostUSD,
		OutputCostUSD: e.OutputCostUSD, CacheReadCostUSD: e.CacheReadCostUSD, EstCostUSD: e.EstCostUSD,
		BudgetTierID: tierID, BudgetTierNameSnapshot: e.BudgetTierNameSnapshot,
		BudgetTierAccountingEligible:  e.BudgetTierAccountingEligible,
		BudgetTierConstraintsSnapshot: e.BudgetTierConstraintsSnapshot,
		BudgetBlockCode:               e.BudgetBlockCode, BudgetBlockDimension: e.BudgetBlockDimension,
		BudgetBlockScopeType: e.BudgetBlockScopeType, BudgetBlockScopeID: e.BudgetBlockScopeID,
		BudgetBlockPeriodType: e.BudgetBlockPeriodType,
	}, nil
}
