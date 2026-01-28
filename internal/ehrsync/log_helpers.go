package ehrsync

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/ehrclient"
	"github.com/google/uuid"
)

// ChangeFromUpsert 把一批 DiffEntry 转成 change_log（changeType=modified）
func ChangeFromUpsert(batchID, source, entity, orgID string, entries []DiffEntry) []model.EhrSyncChangeLog {
	logs := make([]model.EhrSyncChangeLog, 0, len(entries))
	for i := range entries {
		l := entries[i].toChangeLog(batchID, source, "modified")
		l.Entity = entity
		l.OrgID = orgID
		logs = append(logs, l)
	}
	return logs
}

// SkipsFromOverride 把 override 命中的条目转成 skip_log
func SkipsFromOverride(batchID, entity, orgID string, entries []DiffEntry) []model.EhrSyncSkipLog {
	logs := make([]model.EhrSyncSkipLog, 0, len(entries))
	for i := range entries {
		l := entries[i].toSkipLog(batchID, entries[i].LocalOverrideAt, entries[i].LocalOverrideBy)
		l.Entity = entity
		l.OrgID = orgID
		logs = append(logs, l)
	}
	return logs
}

// LogFromClientError 从 ehrclient.ErrHTTP 构造 error_log
// 返回 nil 表示 err 不是 ErrHTTP 类型
func LogFromClientError(batchID, entity, orgID, method, queryParams string, err error, attempt, maxRetries, backoffMs int) *model.EhrSyncErrorLog {
	var httpErr *ehrclient.ErrHTTP
	if !errors.As(err, &httpErr) {
		return nil
	}
	return &model.EhrSyncErrorLog{
		ID:          uuid.New(),
		BatchID:     batchID,
		Entity:      entity,
		OrgID:       orgID,
		Method:      method,
		QueryParams: queryParams,
		StatusCode:  httpErr.StatusCode,
		Response:    truncateStr(httpErr.Body, 2048),
		ErrorMsg:    err.Error(),
		AttemptNum:  attempt,
		MaxRetries:  maxRetries,
		BackoffMs:   backoffMs,
		OccurredAt:  time.Now(),
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

// MustJSON 序列化任意值为 JSON 字符串（失败返回 {}）
func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
