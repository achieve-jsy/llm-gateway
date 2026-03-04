package quota

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/core/quota"
	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/gin-gonic/gin"
)

// WriteQuotaRejected 统一构造 429 响应与结构化日志。
// v0.16: 使用 errors.WriteError 统一渲染，替代手写 gin.H。
func WriteQuotaRejected(c *gin.Context, v quota.QuotaViolation) {
	retryAfter := 60 // 默认 60s
	if v.Gate == "concurrency" {
		retryAfter = 1 // 并发释放快
	}

	// X-RateLimit-Reset 返回 Unix 时间戳（非相对值），客户端可直接比对当前时间。
	resetAt := time.Now().Unix() + int64(retryAfter)

	msg := formatQuotaMessage(v)
	code := errorCode(v.Gate)

	// 在 WriteError 之前设置限流响应头，避免 WriteError 内部的 c.JSON 提交后
	// 再设置 header 无效。注意：不传 WithRetryAfter 给 WriteError，
	// 因为 WriteError 内部使用 http.StatusText(retryAfter) 存在 bug，
	// 会将数值转为 HTTP 状态文本而非数字字符串。
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.Header("X-RateLimit-Limit", strconv.Itoa(v.Limit))
	c.Header("X-RateLimit-Remaining", "0")
	c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

	slog.Warn("quota rejected",
		"error_code", code,
		"quota_layer", v.Layer,
		"quota_gate", v.Gate,
		"quota_subject_type", v.SubjectType,
		"quota_subject_id", v.SubjectID,
		"quota_limit", v.Limit,
	)

	// Prometheus 指标
	RecordRejection(c.Request.Context(), v.Layer, v.Gate, v.SubjectType)

	gatewayerrors.WriteError(c, code,
		gatewayerrors.WithMessage(msg),
		gatewayerrors.WithDetails(map[string]any{
			"gate":                v.Gate,
			"layer":               v.Layer,
			"subject_type":        v.SubjectType,
			"limit":               v.Limit,
			"retry_after_seconds": retryAfter,
		}),
	)
}

func errorCode(gate string) string {
	switch gate {
	case "concurrency":
		return "quota_concurrency_exceeded"
	case "rpm":
		return "quota_rpm_exceeded"
	default:
		return "quota_exceeded"
	}
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