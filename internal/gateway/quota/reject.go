package quota

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/core/quota"
	"github.com/gin-gonic/gin"
)

// WriteQuotaRejected 统一构造 429 响应与结构化日志。
func WriteQuotaRejected(c *gin.Context, v quota.QuotaViolation) {
	retryAfter := 60 // 默认 60s
	if v.Gate == "concurrency" {
		retryAfter = 1 // 并发释放快
	}

	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.Header("X-RateLimit-Limit", strconv.Itoa(v.Limit))
	c.Header("X-RateLimit-Remaining", "0")
	c.Header("X-RateLimit-Reset", strconv.Itoa(retryAfter))

	msg := formatQuotaMessage(v)

	slog.Warn("quota rejected",
		"error_code", errorCodes[v.Gate],
		"quota_layer", v.Layer,
		"quota_gate", v.Gate,
		"quota_subject_type", v.SubjectType,
		"quota_subject_id", v.SubjectID,
		"quota_limit", v.Limit,
	)

	// Prometheus 指标
	RecordRejection(c.Request.Context(), v.Layer, v.Gate, v.SubjectType)

	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error": gin.H{
			"type":                errorType(v.Gate),
			"code":                errorCode(v.Gate),
			"message":             msg,
			"param":               v.Layer,
			"gate":                v.Gate,
			"layer":               v.Layer,
			"subject_type":        v.SubjectType,
			"limit":               v.Limit,
			"retry_after_seconds": retryAfter,
		},
	})
}

var errorCodes = map[string]string{
	"rpm":         "QUOTA_RPM_REJECTED",
	"concurrency": "QUOTA_CONCURRENCY_REJECTED",
}

func errorType(gate string) string {
	if gate == "concurrency" {
		return "concurrency_limit"
	}
	return "rate_limit_error"
}

func errorCode(gate string) string {
	if gate == "concurrency" {
		return "quota_concurrency_exceeded"
	}
	return "quota_rpm_exceeded"
}

// formatQuotaMessage 生成中文可读的配额拒绝说明。
func formatQuotaMessage(v quota.QuotaViolation) string {
	layerName := map[string]string{
		"default": "全局默认",
		"subject": subjectLabel(v.SubjectType),
		"group":   "模型组",
		"model":   "模型",
	}
	gateName := map[string]string{
		"rpm":         "请求频率（RPM）",
		"concurrency": "并发数",
	}
	ln := layerName[v.Layer]
	if ln == "" {
		ln = v.Layer
	}
	gn := gateName[v.Gate]
	if gn == "" {
		gn = v.Gate
	}
	return fmt.Sprintf("速率超限：%s层 %s 已达上限（%d）", ln, gn, v.Limit)
}

func subjectLabel(t string) string {
	if t == "service_key" {
		return "Service Key"
	}
	return "用户"
}
