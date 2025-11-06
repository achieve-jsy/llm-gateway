package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// CheckResult 单次检测结果
type CheckResult struct {
	ProviderID   uuid.UUID `json:"provider_id"`
	ProviderName string    `json:"provider_name"`
	Success      bool      `json:"success"`
	ResponseTime int       `json:"response_time"` // 毫秒
	Error        string    `json:"error,omitempty"`
	AutoDisabled bool      `json:"auto_disabled"` // 本次是否触发了自动禁用
}

// CheckAllResult 批量检测汇总结果
type CheckAllResult struct {
	Total        int           `json:"total"`
	Success      int           `json:"success"`
	Failed       int           `json:"failed"`
	AutoDisabled int           `json:"auto_disabled"`
	Results      []CheckResult `json:"results"`
}

// HealthCheckService 健康检测调度服务
type HealthCheckService struct {
	healthRepo   *repository.HealthCheckRepo
	providerRepo *repository.ProviderRepo
	modelRepo    *repository.ModelRepo

	mu                sync.Mutex
	cancel            context.CancelFunc
	modelCheckCancel  context.CancelFunc
	running           bool
	modelCheckRunning bool
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

	if s.running && s.cancel != nil {
		s.cancel()
		s.running = false
	}
	if s.modelCheckRunning && s.modelCheckCancel != nil {
		s.modelCheckCancel()
		s.modelCheckRunning = false
	}
	slog.Info("health check scheduler stopped")
}

// IsRunning 是否在运行
func (s *HealthCheckService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// StartBackground 根据环境变量启动后台定时检测 goroutine
// HEALTH_CHECK_INTERVAL 单位为分钟，默认 5，0 表示禁用
func (s *HealthCheckService) StartBackground(ctx context.Context) {
	intervalStr := os.Getenv("HEALTH_CHECK_INTERVAL")
	intervalMin := 5
	if intervalStr != "" {
		if v, err := strconv.Atoi(intervalStr); err == nil {
			intervalMin = v
		}
	}
	if intervalMin <= 0 {
		slog.Info("health check background disabled (HEALTH_CHECK_INTERVAL=0)")
		return
	}

	// 同步 interval 到 config 表
	config, err := s.healthRepo.GetConfig()
	if err != nil {
		slog.Warn("failed to get health check config for background start", "error", err)
		return
	}
	config.IntervalSeconds = intervalMin * 60
	config.Enabled = true
	s.healthRepo.UpdateConfig(config)

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.cancel = func() {} // 占位，下面覆盖
	s.running = true
	s.mu.Unlock()

	bgCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	// 渠道检测后台 goroutine
	go func() {
		interval := time.Duration(intervalMin) * time.Minute
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		slog.Info("health check background started", "interval_minutes", intervalMin)

		// 首次延迟 30 秒执行，避免启动时立即发请求
		select {
		case <-bgCtx.Done():
			return
		case <-time.After(30 * time.Second):
			s.runBackgroundCheck()
		}

		for {
			select {
			case <-bgCtx.Done():
				s.mu.Lock()
				s.running = false
				s.mu.Unlock()
				return
			case <-ticker.C:
				s.runBackgroundCheck()
			}
		}
	}()

	// 启动独立的模型检测后台 goroutine
	s.startModelCheckBackground(ctx, config)
}

// startModelCheckBackground 启动模型检测独立后台定时器
func (s *HealthCheckService) startModelCheckBackground(ctx context.Context, config *model.HealthCheckConfig) {
	if !config.ModelCheckEnabled {
		slog.Info("model check background disabled (model_check_enabled=false)")
		return
	}

	modelInterval := config.ModelCheckIntervalSeconds
	if modelInterval <= 0 {
		modelInterval = 300 // 默认 5 分钟
	}

	s.mu.Lock()
	if s.modelCheckRunning {
		s.mu.Unlock()
		return
	}
	s.modelCheckRunning = true
	s.mu.Unlock()

	modelCtx, modelCancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.modelCheckCancel = modelCancel
	s.mu.Unlock()

	go func() {
		interval := time.Duration(modelInterval) * time.Second
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		slog.Info("model check background started", "interval_seconds", modelInterval)

		// 首次延迟 45 秒（错开渠道检测的 30 秒）
		select {
		case <-modelCtx.Done():
			return
		case <-time.After(45 * time.Second):
			s.runBackgroundModelCheck()
		}

		for {
			select {
			case <-modelCtx.Done():
				s.mu.Lock()
				s.modelCheckRunning = false
				s.mu.Unlock()
				return
			case <-ticker.C:
				// 每次重新读配置，支持动态关闭
				cfg, err := s.healthRepo.GetConfig()
				if err != nil || !cfg.ModelCheckEnabled {
					slog.Info("model check background stopping (disabled in config)")
					s.mu.Lock()
					s.modelCheckRunning = false
					s.mu.Unlock()
					return
				}
				s.runBackgroundModelCheck()
			}
		}
	}()
}

// runBackgroundModelCheck 后台执行一轮模型检测
func (s *HealthCheckService) runBackgroundModelCheck() {
	config, _ := s.healthRepo.GetConfig()
	maxFailures := 3
	if config != nil && config.ModelCheckFailureThreshold > 0 {
		maxFailures = config.ModelCheckFailureThreshold
	}

	filter := &repository.ModelListFilter{}
	models, _, err := s.modelRepo.List(*filter, 0, 1000)
	if err != nil {
		slog.Error("model check background: failed to list models", "error", err)
		return
	}

	var total, success, failed, autoDisabled int
	var mu sync.Mutex
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for i := range models {
		m := &models[i]
		if m.Enabled == nil || !*m.Enabled {
			continue
		}
		if strings.Contains(strings.ToLower(m.ModelType), "embed") {
			continue
		}

		total++
		wg.Add(1)
		sem <- struct{}{}
		go func(mod *model.Model) {
			defer wg.Done()
			defer func() { <-sem }()

			ok, elapsed, errMsg := s.doModelForwardCheck(mod)
			if ok {
				mu.Lock()
				success++
				mu.Unlock()
				s.modelRepo.ResetModelFailures(mod.ID)
				s.modelRepo.UpdateModelCheckResult(mod.ID, elapsed, "")
				s.recordLog("model", mod.ID, mod.Name, true, elapsed, "")
			} else {
				s.modelRepo.UpdateModelCheckResult(mod.ID, elapsed, errMsg)
				s.recordLog("model", mod.ID, mod.Name, false, elapsed, errMsg)

				newCount, err := s.modelRepo.IncrementModelFailures(mod.ID)
				if err == nil && newCount >= maxFailures {
					slog.Warn("model check: auto-disabling model",
						"model_id", mod.ID, "model_name", mod.Name,
						"failures", newCount, "threshold", maxFailures)
					if err := s.modelRepo.AutoDisableModel(mod.ID); err == nil {
						mu.Lock()
						autoDisabled++
						mu.Unlock()
					}
				}
				mu.Lock()
				failed++
				mu.Unlock()
			}
		}(m)
	}

	wg.Wait()
	slog.Info("background model check completed",
		"total", total, "success", success, "failed", failed, "auto_disabled", autoDisabled)
}

// doModelForwardCheck 对单个模型做 chat/completions 检测
func (s *HealthCheckService) doModelForwardCheck(m *model.Model) (bool, int, string) {
	provider := &m.Provider
	if provider.ID == uuid.Nil {
		p, err := s.providerRepo.GetByID(m.ProviderID)
		if err != nil {
			return false, 0, "渠道不存在"
		}
		provider = p
	}

	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		return false, 0, "解密凭证失败"
	}

	apiKey := extractAPIKey(credJSON)

	modelName := m.UpstreamModelID
	if modelName == "" {
		modelName = m.Name
	}

	success, skipped, elapsed, errMsg := ProbeModelForward(context.Background(), provider.Endpoint, modelName, apiKey)
	if skipped {
		return true, elapsed, "no model to test, skipped"
	}
	return success, elapsed, errMsg
}

// runBackgroundCheck 后台执行一轮检测
func (s *HealthCheckService) runBackgroundCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result := s.CheckAll(ctx)
	slog.Info("background health check completed",
		"total", result.Total,
		"success", result.Success,
		"failed", result.Failed,
		"auto_disabled", result.AutoDisabled,
	)
}

// CheckAll 检测所有启用渠道，并行返回结果
func (s *HealthCheckService) CheckAll(ctx context.Context) *CheckAllResult {
	filter := &repository.ProviderListFilter{}
	providers, _, err := s.providerRepo.List(*filter, 0, 1000)
	if err != nil {
		slog.Error("health check: failed to list providers", "error", err)
		return &CheckAllResult{}
	}

	config, _ := s.healthRepo.GetConfig()
	maxFailures := 3
	checkMode := "connectivity"
	if config != nil {
		if config.FailureThreshold > 0 {
			maxFailures = config.FailureThreshold
		}
		if config.CheckMode != "" {
			checkMode = config.CheckMode
		}
	}

	result := &CheckAllResult{
		Results: make([]CheckResult, 0, len(providers)),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i := range providers {
		p := &providers[i]
		if !p.Enabled {
			continue
		}
		mu.Lock()
		result.Total++
		mu.Unlock()

		wg.Add(1)
		sem <- struct{}{}
		go func(provider *model.Provider) {
			defer wg.Done()
			defer func() { <-sem }()
			cr := s.CheckOne(ctx, provider, maxFailures, checkMode)
			mu.Lock()
			result.Results = append(result.Results, *cr)
			if cr.Success {
				result.Success++
			} else {
				result.Failed++
			}
			if cr.AutoDisabled {
				result.AutoDisabled++
			}
			mu.Unlock()
		}(p)
	}

	wg.Wait()

	s.healthRepo.CleanOldLogs(30)

	return result
}

// CheckOne 检测单个渠道，支持 connectivity / model_forward / both 三种模式
func (s *HealthCheckService) CheckOne(ctx context.Context, provider *model.Provider, maxFailures int, checkMode string) *CheckResult {
	cr := &CheckResult{
		ProviderID:   provider.ID,
		ProviderName: provider.Name,
	}

	// 解密凭证
	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		cr.Error = "decrypt credentials failed"
		s.recordLog("provider", provider.ID, provider.Name, false, 0, cr.Error)
		s.handleProviderFailure(provider.ID, maxFailures, cr)
		return cr
	}

	apiKey := extractAPIKey(credJSON)

	// 连通性检测
	if checkMode == "connectivity" || checkMode == "both" {
		success, elapsed, errMsg := s.checkConnectivity(ctx, provider, apiKey)
		cr.ResponseTime = elapsed
		if !success {
			cr.Error = errMsg
			s.recordLog("provider", provider.ID, provider.Name, false, elapsed, errMsg)
			s.handleProviderFailure(provider.ID, maxFailures, cr)
			return cr
		}
		// 仅连通性模式直接返回成功
		if checkMode == "connectivity" {
			cr.Success = true
			s.recordLog("provider", provider.ID, provider.Name, true, elapsed, "")
			s.providerRepo.UpdateResponseTime(provider.ID, elapsed)
			s.providerRepo.ResetFailures(provider.ID)
			return cr
		}
	}

	// 模型转发检测
	if checkMode == "model_forward" || checkMode == "both" {
		success, elapsed, errMsg := s.checkModelForward(ctx, provider, apiKey)
		if cr.ResponseTime == 0 {
			cr.ResponseTime = elapsed
		} else {
			cr.ResponseTime += elapsed
		}
		if !success {
			// errMsg 为 "skipped" 表示无模型可测，视为通过
			if errMsg == "no model to test, skipped" {
				cr.Success = true
				s.recordLog("provider", provider.ID, provider.Name, true, cr.ResponseTime, errMsg)
				s.providerRepo.UpdateResponseTime(provider.ID, cr.ResponseTime)
				s.providerRepo.ResetFailures(provider.ID)
				return cr
			}
			cr.Error = errMsg
			s.recordLog("provider", provider.ID, provider.Name, false, cr.ResponseTime, errMsg)
			s.handleProviderFailure(provider.ID, maxFailures, cr)
			return cr
		}
	}

	// 全部通过
	cr.Success = true
	s.recordLog("provider", provider.ID, provider.Name, true, cr.ResponseTime, "")
	s.providerRepo.UpdateResponseTime(provider.ID, cr.ResponseTime)
	s.providerRepo.ResetFailures(provider.ID)
	return cr
}

// checkConnectivity 连通性检测：GET {endpoint}/models
func (s *HealthCheckService) checkConnectivity(ctx context.Context, provider *model.Provider, apiKey string) (bool, int, string) {
	endpoint := strings.TrimRight(provider.Endpoint, "/") + "/models"
	client := &http.Client{Timeout: 10 * time.Second}

	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())

	if err != nil {
		return false, elapsed, fmt.Sprintf("connection failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return false, elapsed, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	return true, elapsed, ""
}

// checkModelForward 模型转发检测：POST {endpoint}/chat/completions（max_tokens=1）
func (s *HealthCheckService) checkModelForward(ctx context.Context, provider *model.Provider, apiKey string) (bool, int, string) {
	// 确定测试模型：优先使用渠道的 test_model
	testModel := provider.TestModel
	if testModel == "" {
		// 从 models 表取第一个属于该渠道且启用的模型
		providerID := provider.ID
		filter := repository.ModelListFilter{ProviderID: &providerID}
		models, _, _ := s.modelRepo.List(filter, 0, 1)
		if len(models) > 0 {
			testModel = models[0].Name
		}
	}
	if testModel == "" {
		// 无模型可测，视为通过
		return false, 0, "no model to test, skipped"
	}

	success, skipped, elapsed, errMsg := ProbeModelForward(ctx, provider.Endpoint, testModel, apiKey)
	if skipped {
		return false, elapsed, "no model to test, skipped"
	}
	if !success && errMsg != "" {
		return false, elapsed, "model forward " + errMsg
	}
	return success, elapsed, errMsg
}

// CheckOneByID 通过 ID 查找渠道并检测
func (s *HealthCheckService) CheckOneByID(ctx context.Context, id uuid.UUID) (*CheckResult, error) {
	provider, err := s.providerRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("provider not found")
	}

	config, _ := s.healthRepo.GetConfig()
	maxFailures := 3
	checkMode := "connectivity"
	if config != nil {
		if config.FailureThreshold > 0 {
			maxFailures = config.FailureThreshold
		}
		if config.CheckMode != "" {
			checkMode = config.CheckMode
		}
	}

	return s.CheckOne(ctx, provider, maxFailures, checkMode), nil
}

// handleProviderFailure 处理渠道失败（递增计数，可能自动禁用）
func (s *HealthCheckService) handleProviderFailure(id uuid.UUID, maxFailures int, cr *CheckResult) {
	newCount, err := s.providerRepo.IncrementFailures(id)
	if err != nil {
		slog.Error("failed to increment failures", "provider_id", id, "error", err)
		return
	}

	if newCount >= maxFailures {
		slog.Warn("auto-disabling provider due to consecutive failures",
			"provider_id", id,
			"failures", newCount,
			"threshold", maxFailures,
		)
		if err := s.providerRepo.AutoDisable(id); err != nil {
			slog.Error("failed to auto-disable provider", "provider_id", id, "error", err)
		} else {
			cr.AutoDisabled = true
		}
	}
}

// RunNow 手动触发一次检测（异步，兼容旧调用）
func (s *HealthCheckService) RunNow() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.CheckAll(ctx)
	}()
}

// runLoop 定时执行健康检测（config 驱动模式）
func (s *HealthCheckService) runLoop(ctx context.Context, config *model.HealthCheckConfig) {
	ticker := time.NewTicker(time.Duration(config.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	// 首次立即执行一轮
	s.runBackgroundCheck()

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
			s.runBackgroundCheck()
		}
	}
}

// checkModels 检测所有启用的模型（保留原有功能）
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
		if strings.Contains(strings.ToLower(m.ModelType), "embed") {
			continue
		}
		if m.Provider.ID == uuid.Nil {
			continue
		}
		s.checkSingleModel(client, &m, config)
	}
}

// checkSingleModel 检测单个模型
func (s *HealthCheckService) checkSingleModel(client *http.Client, m *model.Model, config *model.HealthCheckConfig) {
	provider := &m.Provider
	if provider.ID == uuid.Nil || !provider.Enabled {
		return
	}

	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		s.recordLog("model", m.ID, m.Name, false, 0, "decrypt provider credentials failed")
		return
	}

	apiKey := extractAPIKey(credJSON)

	modelName := m.UpstreamModelID
	if modelName == "" {
		modelName = m.Name
	}
	success, skipped, elapsed, errMsg := ProbeModelForward(context.Background(), provider.Endpoint, modelName, apiKey)
	if skipped {
		s.recordLog("model", m.ID, m.Name, true, elapsed, "skipped")
		return
	}
	if !success {
		s.recordLog("model", m.ID, m.Name, false, elapsed, errMsg)
		s.handleModelFailure("model", m.ID, config)
		return
	}

	s.recordLog("model", m.ID, m.Name, true, elapsed, "")
}

// handleModelFailure 处理模型失败（基于日志记录的连续失败判断）
func (s *HealthCheckService) handleModelFailure(targetType string, targetID uuid.UUID, config *model.HealthCheckConfig) {
	failures, err := s.healthRepo.CountConsecutiveFailures(targetType, targetID)
	if err != nil {
		return
	}

	if failures < config.FailureThreshold {
		return
	}

	if config.FailureAction == "alert_only" {
		slog.Warn("health check threshold reached (alert_only)", "type", targetType, "id", targetID, "failures", failures)
		return
	}

	slog.Warn("auto-disabling model due to health check failures", "id", targetID, "failures", failures)
	enabled := false
	s.modelRepo.UpdateEnabled(targetID, &enabled)
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
