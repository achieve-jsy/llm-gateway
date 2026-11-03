package service

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// OpsService 运维看板轻量状态聚合
type OpsService struct {
	db               *gorm.DB
	healthCheckRepo  *repository.HealthCheckRepo
	healthCheckSvc   *HealthCheckService
}

// NewOpsService 构造 OpsService
func NewOpsService(db *gorm.DB, healthCheckRepo *repository.HealthCheckRepo) *OpsService {
	return &OpsService{db: db, healthCheckRepo: healthCheckRepo}
}

// SetHealthCheckService 注入健康检测服务（用于获取运行状态）
func (s *OpsService) SetHealthCheckService(svc *HealthCheckService) {
	s.healthCheckSvc = svc
}

// StatusCounts 单类资源的总数/启用/禁用
type StatusCounts struct {
	Total    int `json:"total"`
	Enabled  int `json:"enabled"`
	Disabled int `json:"disabled"`
}

// GroupCounts 组统计（含 public/controlled 拆分）
type GroupCounts struct {
	Total      int `json:"total"`
	Public     int `json:"public"`
	Controlled int `json:"controlled"`
}

// HealthCheckStatus 健康检测调度状态
type HealthCheckStatus struct {
	Enabled bool     `json:"enabled"`
	Running bool     `json:"running"`
	Recent  []RecentCheckEntry `json:"recent"`
}

// RecentCheckEntry 最近检测条目（失败/成功）
type RecentCheckEntry struct {
	TargetName   string    `json:"target_name"`
	TargetType   string    `json:"target_type"`
	Success      bool      `json:"success"`
	ResponseTime int       `json:"response_time_ms"`
	Error        string    `json:"error,omitempty"`
	CheckedAt    time.Time `json:"checked_at"`
}

// OpsStatusResponse 运维状态聚合响应
type OpsStatusResponse struct {
	Providers    StatusCounts      `json:"providers"`
	Models       StatusCounts      `json:"models"`
	Keys         StatusCounts      `json:"keys"`
	Groups       GroupCounts       `json:"groups"`
	HealthCheck  HealthCheckStatus `json:"health_check"`
	GeneratedAt  time.Time         `json:"generated_at"`
}

// GetStatus 聚合系统状态（轻量 DB 查询）
func (s *OpsService) GetStatus() (*OpsStatusResponse, error) {
	resp := &OpsStatusResponse{GeneratedAt: time.Now()}

	// Provider counts
	var provTotal, provEnabled, provDisabled int64
	s.db.Model(&cpmodel.Provider{}).Count(&provTotal)
	s.db.Model(&cpmodel.Provider{}).Where("enabled = ?", true).Count(&provEnabled)
	s.db.Model(&cpmodel.Provider{}).Where("enabled = ?", false).Count(&provDisabled)
	resp.Providers = StatusCounts{Total: int(provTotal), Enabled: int(provEnabled), Disabled: int(provDisabled)}

	// Model counts
	var modTotal, modEnabled, modDisabled int64
	s.db.Model(&cpmodel.Model{}).Count(&modTotal)
	s.db.Model(&cpmodel.Model{}).Where("enabled = ?", true).Count(&modEnabled)
	s.db.Model(&cpmodel.Model{}).Where("enabled = ?", false).Count(&modDisabled)
	resp.Models = StatusCounts{Total: int(modTotal), Enabled: int(modEnabled), Disabled: int(modDisabled)}

	// Key counts
	var keyTotal, keyEnabled, keyDisabled int64
	s.db.Model(&cpmodel.ApiKey{}).Count(&keyTotal)
	s.db.Model(&cpmodel.ApiKey{}).Where("enabled = ?", true).Count(&keyEnabled)
	s.db.Model(&cpmodel.ApiKey{}).Where("enabled = ?", false).Count(&keyDisabled)
	resp.Keys = StatusCounts{Total: int(keyTotal), Enabled: int(keyEnabled), Disabled: int(keyDisabled)}

	// Group counts
	var grpTotal, grpPublic, grpControlled int64
	s.db.Model(&cpmodel.Group{}).Where("deleted_at IS NULL").Count(&grpTotal)
	s.db.Model(&cpmodel.Group{}).Where("deleted_at IS NULL AND open_mode = ?", "public").Count(&grpPublic)
	s.db.Model(&cpmodel.Group{}).Where("deleted_at IS NULL AND open_mode = ?", "controlled").Count(&grpControlled)
	resp.Groups = GroupCounts{Total: int(grpTotal), Public: int(grpPublic), Controlled: int(grpControlled)}

	// Health check config + recent
	config, err := s.healthCheckRepo.GetConfig()
	enabled := false
	if err == nil && config != nil {
		enabled = config.Enabled
	}
	running := false
	if s.healthCheckSvc != nil {
		running = s.healthCheckSvc.IsRunning()
	}
	resp.HealthCheck.Enabled = enabled
	resp.HealthCheck.Running = running

	logs, err := s.healthCheckRepo.GetRecentLogs(10)
	recent := make([]RecentCheckEntry, 0, len(logs))
	if err == nil {
		for _, l := range logs {
			recent = append(recent, RecentCheckEntry{
				TargetName:   l.TargetName,
				TargetType:   l.TargetType,
				Success:      l.Success,
				ResponseTime: l.ResponseTimeMs,
				Error:        l.ErrorMessage,
				CheckedAt:    l.CheckedAt,
			})
		}
	}
	resp.HealthCheck.Recent = recent

	return resp, nil
}
