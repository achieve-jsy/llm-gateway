package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// HealthCheckService 健康检测调度服务
type HealthCheckService struct {
	healthRepo   *repository.HealthCheckRepo
	providerRepo *repository.ProviderRepo
	modelRepo    *repository.ModelRepo

	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
}

// NewHealthCheckService 创建 HealthCheckService 实例
func NewHealthCheckService(
	healthRepo *repository.HealthCheckRepo,
	providerRepo *repository.ProviderRepo,
	modelRepo *repository.ModelRepo,
) *HealthCheckService {
	return &HealthCheckService{
		healthRepo:   healthRepo,
		providerRepo: providerRepo,
		modelRepo:    modelRepo,
	}
}

// GetConfig 获取配置
func (s *HealthCheckService) GetConfig() (*model.HealthCheckConfig, error) {
	return s.healthRepo.GetConfig()
}

// UpdateConfig 更新配置并重启调度
func (s *HealthCheckService) UpdateConfig(config *model.HealthCheckConfig) error {
	if err := s.healthRepo.UpdateConfig(config); err != nil {
		return err
	}
	// 重启调度器
	s.Stop()
	if config.Enabled {
		s.Start()
	}
	return nil
}

// GetLogs 获取检测记录
func (s *HealthCheckService) GetLogs(limit int) ([]model.HealthCheckLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.healthRepo.GetRecentLogs(limit)
}

// Start 启动调度器
func (s *HealthCheckService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	config, err := s.healthRepo.GetConfig()
	if err != nil || !config.Enabled {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true

	go s.runLoop(ctx, config)
	slog.Info("health check scheduler started", "interval", config.IntervalSeconds)
}

// Stop 停止调度器
func (s *HealthCheckService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
	slog.Info("health check scheduler stopped")
}

// IsRunning 是否在运行
func (s *HealthCheckService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// runLoop 定时执行健康检测
func (s *HealthCheckService) runLoop(ctx context.Context, config *model.HealthCheckConfig) {
	ticker := time.NewTicker(time.Duration(config.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	// 首次立即执行一轮
	s.runCheck(config)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 每次重新读取配置（支持动态调整间隔）
			newConfig, err := s.healthRepo.GetConfig()
			if err != nil || !newConfig.Enabled {
				s.mu.Lock()
				s.running = false
				s.mu.Unlock()
				return
			}
			s.runCheck(newConfig)
		}
	}
}

// runCheck 执行一轮完整检测
func (s *HealthCheckService) runCheck(config *model.HealthCheckConfig) {
	slog.Info("health check round started")

	client := &http.Client{Timeout: time.Duration(config.TimeoutSeconds) * time.Second}

	if config.CheckProviders {
		s.checkProviders(client, config)
	}

	if config.CheckModels {
		s.checkModels(client, config)
	}

	// 清理 30 天前的日志
	s.healthRepo.CleanOldLogs(30)
}

// checkProviders 检测所有启用的渠道
func (s *HealthCheckService) checkProviders(client *http.Client, config *model.HealthCheckConfig) {
	filter := &repository.ProviderListFilter{}
	providers, _, err := s.providerRepo.List(*filter, 0, 1000)
	if err != nil {
		slog.Error("health check: failed to list providers", "error", err)
		return
	}

	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		s.checkSingleProvider(client, &provider, config)
	}
}

// checkSingleProvider 检测单个渠道
func (s *HealthCheckService) checkSingleProvider(client *http.Client, provider *model.Provider, config *model.HealthCheckConfig) {
	// 解密凭证
	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		s.recordLog("provider", provider.ID, provider.Name, false, 0, "decrypt credentials failed")
		return
	}

	var creds map[string]string
	json.Unmarshal([]byte(credJSON), &creds)
	apiKey := creds["api_key"]
	if apiKey == "" {
		apiKey = credJSON
	}

	// GET /models 连通性检测
	endpoint := strings.TrimRight(provider.Endpoint, "/") + "/models"
	start := time.Now()
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())

	if err != nil {
		s.recordLog("provider", provider.ID, provider.Name, false, elapsed, fmt.Sprintf("connection failed: %v", err))
		s.handleFailure("provider", provider.ID, config)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		s.recordLog("provider", provider.ID, provider.Name, false, elapsed, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200)))
		s.handleFailure("provider", provider.ID, config)
		return
	}

	// 成功
	s.recordLog("provider", provider.ID, provider.Name, true, elapsed, "")
	s.providerRepo.UpdateResponseTime(provider.ID, elapsed)
}

// checkModels 检测所有启用的模型
func (s *HealthCheckService) checkModels(client *http.Client, config *model.HealthCheckConfig) {
	filter := &repository.ModelListFilter{}
	models, _, err := s.modelRepo.List(*filter, 0, 1000)
	if err != nil {
		slog.Error("health check: failed to list models", "error", err)
		return
	}

	for _, m := range models {
		if m.Enabled == nil || !*m.Enabled {
			continue
		}
		// 跳过 embedding 类型
		if strings.Contains(strings.ToLower(m.ModelType), "embed") {
			continue
		}
		if m.Provider == nil {
			continue
		}
		s.checkSingleModel(client, &m, config)
	}
}

// checkSingleModel 检测单个模型
func (s *HealthCheckService) checkSingleModel(client *http.Client, m *model.Model, config *model.HealthCheckConfig) {
	provider := m.Provider
	if provider == nil || !provider.Enabled {
		return
	}

	// 解密凭证
	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		s.recordLog("model", m.ID, m.Name, false, 0, "decrypt provider credentials failed")
		return
	}

	var creds map[string]string
	json.Unmarshal([]byte(credJSON), &creds)
	apiKey := creds["api_key"]
	if apiKey == "" {
		apiKey = credJSON
	}

	// POST /chat/completions 最小请求
	endpoint := strings.TrimRight(provider.Endpoint, "/") + "/chat/completions"
	chatReq := map[string]interface{}{
		"model":      m.Name,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 1,
	}
	body, _ := json.Marshal(chatReq)

	start := time.Now()
	req, _ := http.NewRequest("POST", endpoint, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())

	if err != nil {
		s.recordLog("model", m.ID, m.Name, false, elapsed, fmt.Sprintf("request failed: %v", err))
		s.handleFailure("model", m.ID, config)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		errMsg := extractErrMsg(respBody)
		s.recordLog("model", m.ID, m.Name, false, elapsed, errMsg)
		s.handleFailure("model", m.ID, config)
		return
	}

	// 成功
	s.recordLog("model", m.ID, m.Name, true, elapsed, "")
}

// recordLog 记录检测日志
func (s *HealthCheckService) recordLog(targetType string, targetID uuid.UUID, targetName string, success bool, responseTimeMs int, errMsg string) {
	log := &model.HealthCheckLog{
		TargetType:     targetType,
		TargetID:       targetID,
		TargetName:     targetName,
		Success:        success,
		ResponseTimeMs: responseTimeMs,
		ErrorMessage:   errMsg,
	}
	if err := s.healthRepo.CreateLog(log); err != nil {
		slog.Error("failed to create health check log", "error", err)
	}
	if !success {
		slog.Warn("health check failed", "type", targetType, "name", targetName, "error", errMsg)
	}
}

// handleFailure 处理失败（根据阈值和策略）
func (s *HealthCheckService) handleFailure(targetType string, targetID uuid.UUID, config *model.HealthCheckConfig) {
	failures, err := s.healthRepo.CountConsecutiveFailures(targetType, targetID)
	if err != nil {
		return
	}

	if failures < config.FailureThreshold {
		slog.Info("health check failure below threshold", "type", targetType, "id", targetID, "failures", failures, "threshold", config.FailureThreshold)
		return
	}

	if config.FailureAction == "alert_only" {
		slog.Warn("health check threshold reached (alert_only)", "type", targetType, "id", targetID, "failures", failures)
		return
	}

	// auto_disable: 自动禁用
	slog.Warn("auto-disabling due to health check failures", "type", targetType, "id", targetID, "failures", failures)
	if targetType == "provider" {
		s.providerRepo.UpdateStatus(targetID, false)
	} else if targetType == "model" {
		enabled := false
		s.modelRepo.UpdateEnabled(targetID, &enabled)
	}
}

// RunNow 手动触发一次检测
func (s *HealthCheckService) RunNow() {
	config, err := s.healthRepo.GetConfig()
	if err != nil {
		return
	}
	go s.runCheck(config)
}

// truncateStr 截断字符串
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractErrMsg 从响应体提取错误信息
func extractErrMsg(body []byte) string {
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &resp) == nil && resp.Error.Message != "" {
		return resp.Error.Message
	}
	return truncateStr(string(body), 150)
}
