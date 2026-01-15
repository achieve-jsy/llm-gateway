package middleware

import (
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditMiddleware 全局审计中间件
// 挂在 adminGroup 上，记录所有写操作（POST/PATCH/PUT/DELETE）的审计日志。
// 依赖 audit.Recorder 接口，不与具体 service 实现耦合。
// 若 handler 已通过 audit.MarkRecorded(c) 标记手动记录过，则跳过自动记录。
func AuditMiddleware(recorder audit.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isWriteMethod(c.Request.Method) {
			c.Next()
			return
		}

		c.Next()

		if audit.IsRecorded(c) {
			return
		}

		path := c.Request.URL.Path
		method := c.Request.Method
		action := inferAction(method, path)
		resourceType := inferResourceType(path)
		resourceID := inferResourceID(c)
		statusCode := c.Writer.Status()

		if action == "" {
			action = audit.Action(method + ":" + c.FullPath())
		}
		if resourceType == "" {
			resourceType = string(audit.ResourceUnknown)
		}

		var userID uuid.UUID
		if uidStr := c.GetString("user_id"); uidStr != "" {
			if uid, err := uuid.Parse(uidStr); err == nil {
				userID = uid
			}
		}

		recorder.Record(audit.Input{
			UserID:       userID,
			UserName:     c.GetString("user_name"),
			Action:       action,
			ResourceType: audit.Resource(resourceType),
			ResourceID:   resourceID,
			ResourceName: audit.GetResourceNameCtx(c),
			DeptID:       c.GetString("dept_id"),
			DeptName:     c.GetString("dept_name"),
			ClientIP:     extractClientIP(c),
			UserAgent:    c.GetHeader("User-Agent"),
			StatusCode:   statusCode,
			Details: map[string]any{
				"method":     method,
				"path":       path,
				"full_route": c.FullPath(),
			},
		})
	}
}

func isWriteMethod(method string) bool {
	switch method {
	case "POST", "PATCH", "PUT", "DELETE":
		return true
	}
	return false
}

func extractClientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if xri := c.GetHeader("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return c.ClientIP()
}

// inferAction 根据 HTTP 方法 + 路由路径推断操作类型（如 provider.create）
// 返回空字符串表示未知，上层会 fallback 到 "POST:/path"
func inferAction(method, path string) audit.Action {
	if !isWriteMethod(method) {
		return ""
	}

	segments := splitPath(path)

	resourceStart := -1
	for i := 0; i < len(segments)-1; i++ {
		if (segments[i] == "api" || segments[i] == "internal") &&
			segments[i+1] == "v1" &&
			i+2 < len(segments) {
			resourceStart = i + 2
			break
		}
	}
	if resourceStart < 0 || resourceStart >= len(segments) {
		return ""
	}

	resourceSegs := segments[resourceStart:]
	baseResource := singularize(segments[resourceStart])

	rest := resourceSegs[1:]

	subResource := ""
	if len(rest) == 1 {
		if isIDSegment(rest[0]) {
			// /providers/:id
		} else {
			subResource = singularize(rest[0])
		}
	} else if len(rest) >= 2 {
		if isIDSegment(rest[0]) && !isIDSegment(rest[1]) {
			subResource = singularize(rest[1])
		}
	}

	verb := methodToVerb(method)
	if subResource != "" {
		return audit.Action(baseResource + "." + subResource + "." + verb)
	}
	return audit.Action(baseResource + "." + verb)
}

func methodToVerb(method string) string {
	switch method {
	case "POST":
		return "create"
	case "PATCH":
		return "update"
	case "PUT":
		return "update"
	case "DELETE":
		return "delete"
	}
	return strings.ToLower(method)
}

// singularize 简单去复数后缀（保留 "ss"/"us"/"is" 等不变形）
func singularize(s string) string {
	if len(s) < 2 {
		return s
	}
	last2 := s[len(s)-2:]
	if last2 == "ss" || last2 == "us" || last2 == "is" {
		return s
	}
	if s[len(s)-1] == 's' {
		return s[:len(s)-1]
	}
	return s
}

func inferResourceType(path string) string {
	segments := splitPath(path)
	for i, seg := range segments {
		if (seg == "api" || seg == "internal") && i+1 < len(segments) {
			if segments[i+1] == "v1" && i+2 < len(segments) {
				return segments[i+2]
			}
		}
	}
	return ""
}

func inferResourceID(c *gin.Context) string {
	if id := c.Param("id"); id != "" {
		return id
	}
	if id := c.Param("model_id"); id != "" {
		return id
	}
	if id := c.Param("eid"); id != "" {
		return id
	}
	return ""
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func isIDSegment(s string) bool {
	if _, err := uuid.Parse(s); err == nil {
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
