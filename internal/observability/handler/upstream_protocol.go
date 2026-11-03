package handler

import (
	"time"

	"gorm.io/gorm"
)

// ─── 原生旁路可见性（upstream_protocol 维度）────────────────────────────────
//
// gateway 只在「原生直发」成功时给 gin.Context 打标 upstream_protocol：
// native_chat / native_responses / native_claude / native_gemini；
// 降级到 relaykit 转换路径时不打标，落库为空串 —— **空串即代表「转换」**。
//
// 这里沿用 inbound_endpoint 的筛选约定：查询参数为空 = 不筛选，非空 = 精确匹配，
// 因此同一个 SQL 片段既能给「看原生」也能给「看转换之外的明细」复用。

const (
	// UpstreamProtocolParam 查询参数名 / gin.Context key / 投递 JSON 字段名 / DB 列名，四者同名。
	UpstreamProtocolParam = "upstream_protocol"

	// UpstreamProtocolClause 无表别名的 raw SQL 片段；参数需按 (protocol, protocol) 传入两次。
	UpstreamProtocolClause = "AND (? = '' OR upstream_protocol = ?)"
	// UpstreamProtocolClauseCL 带 call_logs 别名 cl 的版本（GetDashboard 系列 raw SQL）。
	UpstreamProtocolClauseCL = "AND (? = '' OR cl.upstream_protocol = ?)"
)

// UpstreamProtocolStat 单个上游协议的调用分布；UpstreamProtocol 为空串的行代表转换路径。
type UpstreamProtocolStat struct {
	UpstreamProtocol string  `json:"upstream_protocol" gorm:"column:upstream_protocol"`
	Calls            int64   `json:"calls"             gorm:"column:calls"`
	TotalTokens      int64   `json:"total_tokens"      gorm:"column:total_tokens"`
	CostEst          float64 `json:"cost_est"          gorm:"column:cost_est"`
}

// QueryUpstreamProtocolBreakdown 按 upstream_protocol 聚合调用分布。
// 运维据此算原生旁路占比：非空串桶的 calls / 全部 calls（空串桶 = 转换路径）。
// extraWhere 可复用调用方已有的筛选片段（须带 cl. 别名，以 AND 开头，可为空）。
func QueryUpstreamProtocolBreakdown(db *gorm.DB, from, to time.Time, extraWhere string, extraArgs []interface{}) ([]UpstreamProtocolStat, error) {
	args := append([]interface{}{from, to}, extraArgs...)
	rows := make([]UpstreamProtocolStat, 0, 5)
	err := db.Raw(`
		SELECT COALESCE(upstream_protocol, '') AS upstream_protocol,
		       COUNT(*) AS calls,
		       COALESCE(SUM(cl.prompt_tokens + cl.completion_tokens + cl.reasoning_tokens), 0) AS total_tokens,
		       COALESCE(SUM(cl.est_cost_usd), 0) AS cost_est
		FROM call_logs cl
		WHERE cl.ts >= ? AND cl.ts < ? `+extraWhere+`
		GROUP BY 1
		ORDER BY calls DESC`, args...).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
