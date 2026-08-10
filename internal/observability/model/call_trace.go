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

	// 正文定位键（规格 §6）：正文不再写进本表的 jsonb 列，而是落 /datalog 上的 spool，
	// 这里只记录「去哪儿找」。四个分量在写入追加时即可确定，无需封块后回填。
	//
	// 【为什么全用指针】NULL 与零值必须可区分：part 0 是合法序号，
	// 若 BodyRefPart 用 int，则「正文缺失（无定位键）」与「part 0 的第 0 行」同名。
	// 【类型必须是 *time.Time，不能是 *string】`type:date` 的列用 string 往返有损：
	// 写 "2026-09-24" 会读回 "2026-09-24T00:00:00Z"（sqlite 与真实 PG 均如此；存储正确，
	// 是驱动按 date 解码成 time.Time）⇒ 计划 3 拿它拼归档目录路径会拼错。
	// 用 *time.Time 后，列值与目录名都由 config 的 DayStart/DayOf 派生，构造上一致。
	BodyRefDT   *time.Time `gorm:"column:body_ref_dt;type:date" json:"body_ref_dt,omitempty"`
	BodyRefInst *string    `gorm:"column:body_ref_inst;type:text" json:"body_ref_inst,omitempty"`
	BodyRefPart *int       `gorm:"column:body_ref_part;type:integer" json:"body_ref_part,omitempty"`
	BodyRefRow  *int       `gorm:"column:body_ref_row;type:integer" json:"body_ref_row,omitempty"`

	// BodyMissing 正文在写入时即失败（盘满/不可写），从未落盘（规格 §9.1）。
	// NOT NULL DEFAULT false：历史行与「已成功落盘」的行都为 false。
	BodyMissing bool `gorm:"column:body_missing;not null;default:false" json:"body_missing"`

	// BodyBytes 原始正文字节数 = len(request_body) + len(response_body)，**不含 headers**
	//（口径裁定见计划 D42；用途是审计与压缩比核算，不参与业务逻辑）。
	BodyBytes *int `gorm:"column:body_bytes;type:integer" json:"body_bytes,omitempty"`
}

func (CallTrace) TableName() string { return "call_traces" }
