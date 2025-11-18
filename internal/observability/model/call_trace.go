package model

import (
	"time"

	"gorm.io/datatypes"
)

// CallTrace 记录单次请求的完整 prompt/response 正文及各阶段耗时
type CallTrace struct {
	RequestID string         `gorm:"type:text;primaryKey"                json:"request_id"`
	TS        time.Time      `gorm:"not null;index:idx_call_traces_ts"   json:"ts"`

	// 各阶段耗时（ms，从请求抵达开始计算）
	StageAuthMs     *int16 `gorm:"column:stage_auth_ms"     json:"stage_auth_ms,omitempty"`
	StageRouteMs    *int16 `gorm:"column:stage_route_ms"    json:"stage_route_ms,omitempty"`
	StageUpstreamMs *int16 `gorm:"column:stage_upstream_ms" json:"stage_upstream_ms,omitempty"`
	StageTTFBMs     *int   `gorm:"column:stage_ttfb_ms"     json:"stage_ttfb_ms,omitempty"`
	StageTTFTMs     *int   `gorm:"column:stage_ttft_ms"     json:"stage_ttft_ms,omitempty"`
	StageTotalMs    *int   `gorm:"column:stage_total_ms"    json:"stage_total_ms,omitempty"`

	// 请求头（JSONB，已过滤 Authorization/Cookie）
	RequestHeaders datatypes.JSON `gorm:"type:jsonb" json:"request_headers,omitempty"`
	// 请求/响应正文（JSONB）
	RequestBody  datatypes.JSON `gorm:"type:jsonb" json:"request_body,omitempty"`
	ResponseBody datatypes.JSON `gorm:"type:jsonb" json:"response_body,omitempty"`

	// 错误信息
	ErrorCode    string `gorm:"type:text" json:"error_code,omitempty"`
	ErrorMessage string `gorm:"type:text" json:"error_message,omitempty"`
}

func (CallTrace) TableName() string { return "call_traces" }
