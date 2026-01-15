package service

import (
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// AuditLogService 审计日志存储实现。
// 通过写 DB 的方式持久化，实现了 audit.Recorder 接口（Record(in audit.Input)）。
// 其他依赖审计的组件（handler/middleware）均持有 audit.Recorder 接口，不直接依赖此类。
type AuditLogService struct {
	repo *repository.AuditLogRepo
}

func NewAuditLogService(repo *repository.AuditLogRepo) *AuditLogService {
	return &AuditLogService{repo: repo}
}

// Record 写入一条审计日志（实现 audit.Recorder 接口）。
// 写库失败仅 slog.Warn，不阻断业务请求。
func (s *AuditLogService) Record(in audit.Input) {
	if s == nil || s.repo == nil {
		return
	}

	details := ""
	if in.Details != nil {
		if b, err := json.Marshal(in.Details); err == nil {
			details = string(b)
		} else {
			slog.Warn("audit: failed to marshal details", "err", err)
		}
	}

	statusCode := in.StatusCode
	if statusCode == 0 {
		statusCode = 200
	}

	ts := in.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	log := &model.AuditLog{
		UserID:       in.UserID,
		UserName:     in.UserName,
		Action:       string(in.Action),
		ResourceType: string(in.ResourceType),
		ResourceID:   in.ResourceID,
		ResourceName: in.ResourceName,
		DeptID:       in.DeptID,
		DeptName:     in.DeptName,
		StatusCode:   statusCode,
		ClientIP:     sanitizeIP(in.ClientIP),
		UserAgent:    truncate(in.UserAgent, 512),
		Details:      truncate(details, 4096),
		CreatedAt:    ts,
	}
	if err := s.repo.Create(log); err != nil {
		slog.Warn("audit: failed to write audit log",
			"action", in.Action,
			"resource_type", in.ResourceType,
			"resource_id", in.ResourceID,
			"err", err)
	}
}

// sanitizeIP 防止写库时客户端伪造超长 IP
func sanitizeIP(ip string) string {
	if ip == "" {
		return ""
	}
	if len(ip) > 64 {
		return ip[:64]
	}
	if parsed := net.ParseIP(ip); parsed == nil {
		return ip
	}
	return ip
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// List 分页查询审计日志（保留供 handler 调用；不属于 Recorder 接口）
func (s *AuditLogService) List(offset, limit int, userID *uuid.UUID, action, resourceType, deptID string, from, to *time.Time) ([]model.AuditLog, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.List(offset, limit, userID, action, resourceType, deptID, from, to)
}
