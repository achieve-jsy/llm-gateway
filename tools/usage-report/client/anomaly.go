package client

// anomaly.go —— 方案 X（真反馈闭环）：异常记录上报（upsert）+ 读回真实状态。
// 报告工具零库权限、只走 /api/v1/usage/report/anomalies 与 /api/v1/usage/report/anomaly-status。
// 接口由 be-eng（control-plane）提供；本文件仅负责【客户端调用】，接口未建/不可用时由调用方静默降级。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnomalyUpsert 异常上报载荷（报告识别命中后 upsert 登记，默认「待确认」）。
// 字段与设计稿 docs/anomaly-feedback-loop-design.md §2.1 对齐。
type AnomalyUpsert struct {
	ScopeLevel  string `json:"scope_level"`   // personal | department | studio
	ScopeID     string `json:"scope_id"`      // 范围 ID（user_id / dept_id / studio_id）
	Rule        string `json:"rule"`          // A1..A5
	RuleVersion string `json:"rule_version"`  // 规则版本（v1）
	WindowStart string `json:"window_start"`  // +08:00 RFC3339，分钟精度（命中窗口起点）
	MemberID    string `json:"member_id"`     // 归因 user_id（可为空 = 群体异常）
}

// AnomalyUpsertResult 上报返回：含分配的记录 id（供后续按 id 反馈）。
type AnomalyUpsertResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// AnomalyStatusRow 读回的真实状态行（供报告覆盖 AnomalyHit.Status）。
type AnomalyStatusRow struct {
	Rule        string `json:"rule"`
	WindowStart string `json:"window_start"` // +08:00 RFC3339，分钟精度
	MemberID    string `json:"member_id"`
	Status      string `json:"status"`
}

// PostReportAnomalies 批量上报异常命中（POST /usage/report/anomalies，upsert）。
// 服务端按唯一键 (scope_level,scope_id,rule,window_start,member_id) upsert：
// 已存在不覆盖用户反馈态（仅刷 updated_at），不存在则以「待确认」新建。
// 返回每条 {id,status}；调用方失败须静默降级（上报为尽力而为，不代表报告失败）。
func (c *Client) PostReportAnomalies(ctx context.Context, items []AnomalyUpsert) ([]AnomalyUpsertResult, error) {
	if len(items) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	u := c.base + strings.TrimLeft(strings.TrimPrefix("/usage/report/anomalies", "/api/v1"), "/")
	b, err := c.doBody(ctx, "POST", u, body)
	if err != nil {
		return nil, err
	}
	var resp []AnomalyUpsertResult
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// FetchAnomalyStatus 读回指定范围 + 时间窗的异常真实状态（GET /usage/report/anomaly-status）。
// 供报告按 (rule, window_start, member_id) 三元组覆盖 AnomalyHit.Status。失败时调用方静默降级为「待确认」。
func (c *Client) FetchAnomalyStatus(ctx context.Context, scopeLevel, scopeID, from, to string) ([]AnomalyStatusRow, error) {
	if scopeLevel == "" || scopeID == "" {
		return nil, nil
	}
	q := map[string]string{
		"scope_level": scopeLevel,
		"scope_id":    scopeID,
		"from":        from,
		"to":          to,
	}
	u := c.base + strings.TrimLeft(strings.TrimPrefix("/usage/report/anomaly-status", "/api/v1"), "/") + "?" + encodeQuery(q)
	b, err := c.doBody(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	var rows []AnomalyStatusRow
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// encodeQuery 把 query map 编码为 "k=v&..."（值用包内 urlEncode 做百分号编码）。
func encodeQuery(q map[string]string) string {
	if len(q) == 0 {
		return ""
	}
	parts := make([]string, 0, len(q))
	for k, v := range q {
		parts = append(parts, k+"="+urlEncode(v))
	}
	return strings.Join(parts, "&")
}

// doBody 通用带请求体的 HTTP 调用（POST 带 JSON body；GET 传 nil）。复用 doOnce 的鉴权/错误处理语义，
// 但支持 body（现有 do() 仅支持 GET 无 body）。重试策略与 do() 一致（429/5xx 退避 0.5s/1s）。
func (c *Client) doBody(ctx context.Context, method, u string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
		b, err := c.doBodyOnce(ctx, method, u, body)
		if err == nil {
			return b, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) doBodyOnce(ctx context.Context, method, u string, body []byte) ([]byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bb, _ := io.ReadAll(resp.Body)
		return nil, &httpError{status: resp.StatusCode, path: u, body: trimBytes(bb, 300)}
	}
	bb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(bb) == 0 {
		return nil, &httpError{status: 0, path: u, body: "empty body"}
	}
	return bb, nil
}
