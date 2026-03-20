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

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimetypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go/auth/bearer"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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
	rdb          *redis.Client

	groupCheckSvc *GroupCheckService // 可选：模型组检测（nil 表示未启用）

	modelStatusLogSvc *ModelStatusLogService    // 模型状态变更日志服务
	snapshotter       *ProviderSnapshotRefresher // 自动禁用/恢复后刷新 Redis snapshot

	mu                   sync.Mutex
	cancel               context.CancelFunc
	modelCheckCancel     context.CancelFunc
	groupCheckCancel     context.CancelFunc
	running              bool
	modelCheckRunning    bool
	groupCheckRunning    bool
}

// NewHealthCheckService 创建 HealthCheckService 实例
func NewHealthCheckService(
	healthRepo *repository.HealthCheckRepo,
	providerRepo *repository.ProviderRepo,
	modelRepo *repository.ModelRepo,
	rdb *redis.Client,
) *HealthCheckService {
	return &HealthCheckService{
		healthRepo:   healthRepo,
		providerRepo: providerRepo,
		modelRepo:    modelRepo,
		rdb:          rdb,
	}
}

// SetGroupCheckService 注入模型组检测服务（在 router 中完成依赖装配后调用）
func (s *HealthCheckService) SetGroupCheckService(svc *GroupCheckService) {
	s.groupCheckSvc = svc
}

// SetModelStatusLogService 注入模型状态变更日志服务
func (s *HealthCheckService) SetModelStatusLogService(svc *ModelStatusLogService) {
	s.modelStatusLogSvc = svc
}

// SetSnapshotRefresher 注入 provider snapshot 刷新器，自动禁用/恢复后同步通知 gateway
func (s *HealthCheckService) SetSnapshotRefresher(snapshotter *ProviderSnapshotRefresher) {
	s.snapshotter = snapshotter
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
	// 重启调度器（provider + model）
	s.Stop()
	if config.Enabled {
		s.Start()
	}
	if config.Enabled && config.ModelCheckEnabled {
		s.startModelCheckBackground(context.Background(), config)
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
	if s.groupCheckRunning && s.groupCheckCancel != nil {
		s.groupCheckCancel()
		s.groupCheckRunning = false
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

	// 启动独立的模型组检测后台 goroutine
	s.startGroupCheckBackground(ctx, config)
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

	recoveryEnabled := false
	recoveryThreshold := 2
	if config != nil && config.ModelRecoveryEnabled {
		recoveryEnabled = true
		if config.ModelRecoverySuccessThreshold > 0 {
			recoveryThreshold = config.ModelRecoverySuccessThreshold
		}
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
			// 手动禁用 (status=2) 跳过，自动禁用 (status=3) 继续检测
			if m.Status != model.ModelStatusAutoDisabled {
				continue
			}
			// status=3 的模型继续检测
		}
		if strings.Contains(strings.ToLower(m.ModelType), "audio") {
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
				s.recordLog("model", mod.ID, mod.Name, mod.Provider.ID, mod.Provider.Name, true, elapsed, "")

				// 恢复逻辑：自动禁用的模型检测成功时处理恢复条件
				if mod.Status == model.ModelStatusAutoDisabled && recoveryEnabled {
					newRecoveryCount := mod.ConsecutiveRecoverySuccesses + 1
					slog.Info("model auto-recovery: success count increased",
						"model_id", mod.ID, "model_name", mod.Name,
						"recovery_successes", newRecoveryCount, "threshold", recoveryThreshold)

					if newRecoveryCount >= recoveryThreshold {
						slog.Warn("model auto-recovery: threshold reached, re-enabling",
							"model_id", mod.ID, "model_name", mod.Name,
							"recovery_successes", newRecoveryCount)
				if err := s.modelRepo.AutoEnableModel(mod.ID); err == nil {
						reason := fmt.Sprintf("连续 %d 次健康检测成功", newRecoveryCount)
						if s.modelStatusLogSvc != nil {
							s.modelStatusLogSvc.LogStatusChange(
								mod.ID, mod.Provider.ID,
								mod.Name, mod.Provider.Name,
								model.ChangeTypeAutoRecovered,
								reason, "",
								nil, "",
							)
						}
						// 刷新 Redis snapshot，让 gateway 立即感知模型恢复
						if s.snapshotter != nil {
							go s.snapshotter.RefreshAvailableSnapshot(context.Background())
						}
					}
					} else {
						// 未达阈值：递增 recovery_successes
						s.modelRepo.IncrementRecoverySuccesses(mod.ID)
					}
				}
			} else {
				s.modelRepo.UpdateModelCheckResult(mod.ID, elapsed, errMsg)
				s.recordLog("model", mod.ID, mod.Name, mod.Provider.ID, mod.Provider.Name, false, elapsed, errMsg)

				// 失败时重置恢复成功计数
				if mod.Status == model.ModelStatusAutoDisabled {
					s.modelRepo.ResetRecoverySuccesses(mod.ID)
					slog.Info("model auto-recovery: reset recovery successes due to failure",
						"model_id", mod.ID, "model_name", mod.Name)
				}

				newCount, err := s.modelRepo.IncrementModelFailures(mod.ID)
				if err == nil && newCount >= maxFailures {
					slog.Warn("model check: auto-disabling model",
						"model_id", mod.ID, "model_name", mod.Name,
						"failures", newCount, "threshold", maxFailures)
				if err := s.modelRepo.AutoDisableModel(mod.ID); err == nil {
					if s.modelStatusLogSvc != nil {
						s.modelStatusLogSvc.LogStatusChange(
							mod.ID, mod.Provider.ID,
							mod.Name, mod.Provider.Name,
							model.ChangeTypeAutoDisabled,
							fmt.Sprintf("连续 %d 次健康检测失败", newCount),
							errMsg, nil, "",
						)
					}
					// 刷新 Redis snapshot，让 gateway 立即感知模型禁用
					if s.snapshotter != nil {
						go s.snapshotter.RefreshAvailableSnapshot(context.Background())
					}
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
	s.invalidateTimelineCache()
}

// invalidateTimelineCache 清除所有健康监控时间线缓存
func (s *HealthCheckService) invalidateTimelineCache() {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	iter := s.rdb.Scan(ctx, 0, "health:timeline:*", 0).Iterator()
	for iter.Next(ctx) {
		s.rdb.Del(ctx, iter.Val())
	}
	if err := iter.Err(); err != nil {
		slog.Warn("health timeline cache invalidate scan failed", "error", err)
	}
}

// doModelForwardCheck 对单个模型做转发检测
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

	// Bedrock 模型用 AWS SDK Converse API 检测（而非 /chat/completions）
	if isNonOpenAICompatible(provider.DefinitionID) {
		return s.doBedrockModelCheck(provider, credJSON, m)
	}

	apiKey := extractAPIKey(credJSON)

	modelName := m.UpstreamName()

	success, skipped, elapsed, errMsg := ProbeModelForward(context.Background(), provider.Endpoint, modelName, apiKey, m.ModelType)
	if skipped {
		return true, elapsed, "no model to test, skipped"
	}
	return success, elapsed, errMsg
}

// doBedrockModelCheck 用 AWS SDK Converse(max_tokens=1) 验证具体模型是否可调用。
// 这样可以发现：模型名拼错、账号没有该模型的访问权限等问题。
func (s *HealthCheckService) doBedrockModelCheck(provider *model.Provider, credJSON string, m *model.Model) (bool, int, string) {
	apiKey := extractAPIKey(credJSON)
	region := regionFromBedrockEndpoint(provider.Endpoint)
	if region == "" {
		region = "us-east-1"
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	var runtimeOpts []func(*bedrockruntime.Options)

	if apiKey != "" {
		if strings.HasPrefix(apiKey, "{") {
			var creds struct {
				AccessKeyID     string `json:"access_key_id"`
				SecretAccessKey string `json:"secret_access_key"`
				SessionToken    string `json:"session_token,omitempty"`
			}
			if err := json.Unmarshal([]byte(apiKey), &creds); err == nil && creds.AccessKeyID != "" {
				loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
					credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
				))
			}
		} else {
			// Bedrock API Key (Bearer token) 模式：
			// 使用 AnonymousCredentials 让 SDK 不做 SigV4 签名，也不走默认凭证链
			loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}))
			tokenVal := apiKey
			runtimeOpts = append(runtimeOpts, func(o *bedrockruntime.Options) {
				o.BearerAuthTokenProvider = bearer.TokenProviderFunc(func(_ context.Context) (bearer.Token, error) {
					return bearer.Token{Value: tokenVal}, nil
				})
				o.AuthSchemePreference = []string{"smithy.api#httpBearerAuth"}
			})
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return false, 0, "AWS 配置加载失败: " + err.Error()
	}

	runtime := bedrockruntime.NewFromConfig(awsCfg, runtimeOpts...)

	modelName := m.UpstreamName()

	maxTokens := int32(1)
	userMsg := "hi"
	start := time.Now()

	_, err = runtime.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: &modelName,
		Messages: []bedrockruntimetypes.Message{
			{
				Role: bedrockruntimetypes.ConversationRoleUser,
				Content: []bedrockruntimetypes.ContentBlock{
					&bedrockruntimetypes.ContentBlockMemberText{
						Value: bedrockruntimetypes.ContentBlockMemberText{Value: userMsg}.Value,
					},
				},
			},
		},
		InferenceConfig: &bedrockruntimetypes.InferenceConfiguration{
			MaxTokens: &maxTokens,
		},
	})

	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return false, elapsed, err.Error()
	}
	return true, elapsed, ""
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
	s.invalidateTimelineCache()
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
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, 0, cr.Error)
		s.handleProviderFailure(provider.ID, maxFailures, cr)
		return cr
	}

	// 非 OpenAI 兼容 provider 走专属检测路径
	if isNonOpenAICompatible(provider.DefinitionID) {
		return s.checkNonOpenAIProvider(ctx, provider, credJSON, checkMode, maxFailures, cr)
	}

	apiKey := extractAPIKey(credJSON)

	// 连通性检测
	if checkMode == "connectivity" || checkMode == "both" {
		success, elapsed, errMsg := s.checkConnectivity(ctx, provider, apiKey)
		cr.ResponseTime = elapsed
		if !success {
			cr.Error = errMsg
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, elapsed, errMsg)
			s.handleProviderFailure(provider.ID, maxFailures, cr)
			return cr
		}
		// 仅连通性模式直接返回成功
		if checkMode == "connectivity" {
			cr.Success = true
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, elapsed, "")
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
				s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, errMsg)
				s.providerRepo.UpdateResponseTime(provider.ID, cr.ResponseTime)
				s.providerRepo.ResetFailures(provider.ID)
				return cr
			}
			cr.Error = errMsg
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, cr.ResponseTime, errMsg)
			s.handleProviderFailure(provider.ID, maxFailures, cr)
			return cr
		}
	}

	// 全部通过
	cr.Success = true
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, "")
	s.providerRepo.UpdateResponseTime(provider.ID, cr.ResponseTime)
	s.providerRepo.ResetFailures(provider.ID)
	return cr
}

// isNonOpenAICompatible 返回该 definition_id 是否为非 OpenAI 兼容协议
func isNonOpenAICompatible(definitionID string) bool {
	switch definitionID {
	case "bedrock":
		return true
	}
	return false
}

// checkNonOpenAIProvider 对非 OpenAI 兼容 provider 做专属健康检测。
// 参考 GoModel AvailabilityChecker 设计模式。
func (s *HealthCheckService) checkNonOpenAIProvider(ctx context.Context, provider *model.Provider, credJSON string, checkMode string, maxFailures int, cr *CheckResult) *CheckResult {
	switch provider.DefinitionID {
	case "bedrock":
		return s.checkBedrockProvider(ctx, provider, credJSON, checkMode, maxFailures, cr)
	default:
		cr.Success = true // 未知类型跳过，不计失败
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, 0, "skipped: unsupported provider type for auto-check")
		return cr
	}
}

// checkBedrockProvider 使用 AWS SDK 对 Bedrock 做健康检测。
//
// 连通性检测：ListFoundationModels（控制面 API，零推理消耗）
//   - 同时验证网络可达 + AWS 凭证有效
//
// 转发检测：Converse API（max_tokens=1，实际调用推理端）
func (s *HealthCheckService) checkBedrockProvider(ctx context.Context, provider *model.Provider, credJSON string, checkMode string, maxFailures int, cr *CheckResult) *CheckResult {
	apiKey := extractAPIKey(credJSON)
	region := regionFromBedrockEndpoint(provider.Endpoint)
	if region == "" {
		region = "us-east-1"
	}

	// 构建 AWS 凭证选项
	loadOpts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	var ctrlOpts []func(*bedrock.Options)
	var runtimeOpts []func(*bedrockruntime.Options)

	if apiKey != "" {
		if strings.HasPrefix(apiKey, "{") {
			var creds struct {
				AccessKeyID     string `json:"access_key_id"`
				SecretAccessKey string `json:"secret_access_key"`
				SessionToken    string `json:"session_token,omitempty"`
			}
			if err := json.Unmarshal([]byte(apiKey), &creds); err == nil && creds.AccessKeyID != "" {
				loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
					credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
				))
			}
		} else {
			// Bearer token 模式：用 AnonymousCredentials 阻止 SDK 走默认凭证链（EC2 IMDS 等）
			loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}))
			tokenVal := apiKey
			ctrlOpts = append(ctrlOpts, func(o *bedrock.Options) {
				o.BearerAuthTokenProvider = bearer.TokenProviderFunc(func(_ context.Context) (bearer.Token, error) {
					return bearer.Token{Value: tokenVal}, nil
				})
				o.AuthSchemePreference = []string{"smithy.api#httpBearerAuth"}
			})
			runtimeOpts = append(runtimeOpts, func(o *bedrockruntime.Options) {
				o.BearerAuthTokenProvider = bearer.TokenProviderFunc(func(_ context.Context) (bearer.Token, error) {
					return bearer.Token{Value: tokenVal}, nil
				})
				o.AuthSchemePreference = []string{"smithy.api#httpBearerAuth"}
			})
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(probeCtx, loadOpts...)
	if err != nil {
		cr.Error = "AWS 配置加载失败: " + err.Error()
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, 0, cr.Error)
		s.handleProviderFailure(provider.ID, maxFailures, cr)
		return cr
	}

	// ===== 连通性检测：ListFoundationModels =====
	if checkMode == "connectivity" || checkMode == "both" {
		start := time.Now()
		bedrockCtrl := bedrock.NewFromConfig(awsCfg, ctrlOpts...)
		out, err := bedrockCtrl.ListFoundationModels(probeCtx, &bedrock.ListFoundationModelsInput{
			ByInferenceType: bedrocktypes.InferenceTypeOnDemand,
		})
		elapsed := int(time.Since(start).Milliseconds())
		cr.ResponseTime = elapsed

		if err != nil {
			cr.Error = err.Error()
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, elapsed, cr.Error)
			s.handleProviderFailure(provider.ID, maxFailures, cr)
			return cr
		}

		_ = out // model count available at out.ModelSummaries if needed
		s.providerRepo.UpdateResponseTime(provider.ID, elapsed)
		s.providerRepo.ResetFailures(provider.ID)

		if checkMode == "connectivity" {
			cr.Success = true
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, elapsed, "")
			return cr
		}
	}

	// ===== 转发检测：Converse API =====
	if checkMode == "model_forward" || checkMode == "both" {
		testModel := provider.TestModel
		if testModel == "" {
			// 从该 provider 的模型表取第一个启用的模型
			providerID := provider.ID
			filter := repository.ModelListFilter{ProviderID: &providerID}
			models, _, _ := s.modelRepo.List(filter, 0, 1)
			if len(models) > 0 {
				testModel = bedrockModelID(models[0].Name)
			}
		}
		if testModel == "" {
			// 无模型可测，跳过转发检测视为通过
			cr.Success = true
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, "no model to test, skipped")
			return cr
		}

		start := time.Now()
		runtime := bedrockruntime.NewFromConfig(awsCfg, runtimeOpts...)
		userMsg := "hi"
		maxTokens := int32(1)
		_, err := runtime.Converse(probeCtx, &bedrockruntime.ConverseInput{
			ModelId: &testModel,
			Messages: []bedrockruntimetypes.Message{
				{
					Role: bedrockruntimetypes.ConversationRoleUser,
					Content: []bedrockruntimetypes.ContentBlock{
						&bedrockruntimetypes.ContentBlockMemberText{
							Value: bedrockruntimetypes.ContentBlockMemberText{Value: userMsg}.Value,
						},
					},
				},
			},
			InferenceConfig: &bedrockruntimetypes.InferenceConfiguration{MaxTokens: &maxTokens},
		})
		elapsed := int(time.Since(start).Milliseconds())
		cr.ResponseTime += elapsed

		if err != nil {
			cr.Error = "model forward: " + err.Error()
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, cr.ResponseTime, cr.Error)
			s.handleProviderFailure(provider.ID, maxFailures, cr)
			return cr
		}
	}

	cr.Success = true
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, "")
	s.providerRepo.UpdateResponseTime(provider.ID, cr.ResponseTime)
	s.providerRepo.ResetFailures(provider.ID)
	return cr
}

// bedrockModelID 从模型名称中提取实际的 Bedrock model ID。
// 模型名称在 gateway 内部格式为 "{provider}/{model_id}"（如 "bedrock/global.anthropic.claude-opus-4-6-v1"），
// 但 AWS Bedrock API 只接受 "/" 之后的部分（如 "global.anthropic.claude-opus-4-6-v1"）。
// 若不含 "/"，原样返回。
func bedrockModelID(name string) string {
	if idx := strings.Index(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// regionFromBedrockEndpoint 从 Bedrock endpoint URL 中提取 AWS region
func regionFromBedrockEndpoint(endpoint string) string {
	ep := strings.Replace(endpoint, "bedrock-runtime.", "bedrock.", 1)
	idx := strings.Index(ep, "://")
	if idx < 0 {
		return ""
	}
	host := ep[idx+3:]
	if slash := strings.IndexByte(host, '/'); slash >= 0 {
		host = host[:slash]
	}
	parts := strings.Split(host, ".")
	for i, part := range parts {
		if part == "bedrock" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// checkConnectivity 连通性检测：GET {endpoint}/models
func (s *HealthCheckService) checkConnectivity(ctx context.Context, provider *model.Provider, apiKey string) (bool, int, string) {
	endpoint := strings.TrimRight(provider.Endpoint, "/") + "/models"
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}

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

// checkModelForward 模型转发检测：按模型类型选择对应探针接口
func (s *HealthCheckService) checkModelForward(ctx context.Context, provider *model.Provider, apiKey string) (bool, int, string) {
	// 确定测试模型：优先使用渠道的 test_model
	testModel := provider.TestModel
	testModelType := "chat" // 默认 chat
	if testModel == "" {
		// 从 models 表取第一个属于该渠道且启用的模型
		providerID := provider.ID
		filter := repository.ModelListFilter{ProviderID: &providerID}
		models, _, _ := s.modelRepo.List(filter, 0, 1)
		if len(models) > 0 {
			testModel = models[0].Name
			testModelType = models[0].ModelType
		}
	}
	if testModel == "" {
		// 无模型可测，视为通过
		return false, 0, "no model to test, skipped"
	}

	success, skipped, elapsed, errMsg := ProbeModelForward(ctx, provider.Endpoint, testModel, apiKey, testModelType)
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
	go s.runBackgroundModelCheck()
	if s.groupCheckSvc != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			s.groupCheckSvc.CheckAll(ctx)
			s.invalidateGroupTimelineCache()
		}()
	}
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
		if strings.Contains(strings.ToLower(m.ModelType), "audio") {
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
		s.recordLog("model", m.ID, m.Name, provider.ID, provider.Name, false, 0, "decrypt provider credentials failed")
		return
	}

	apiKey := extractAPIKey(credJSON)

	modelName := m.UpstreamName()
	success, skipped, elapsed, errMsg := ProbeModelForward(context.Background(), provider.Endpoint, modelName, apiKey, m.ModelType)
	if skipped {
		s.recordLog("model", m.ID, m.Name, provider.ID, provider.Name, true, elapsed, "skipped")
		return
	}
	if !success {
		s.recordLog("model", m.ID, m.Name, provider.ID, provider.Name, false, elapsed, errMsg)
		s.handleModelFailure("model", m.ID, config)
		return
	}

	s.recordLog("model", m.ID, m.Name, provider.ID, provider.Name, true, elapsed, "")
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
func (s *HealthCheckService) recordLog(targetType string, targetID uuid.UUID, targetName string, providerID uuid.UUID, providerName string, success bool, responseTimeMs int, errMsg string) {
	log := &model.HealthCheckLog{
		TargetType:     targetType,
		TargetID:       targetID,
		TargetName:     targetName,
		ProviderID:     providerID,
		ProviderName:   providerName,
		Success:        success,
		ResponseTimeMs: responseTimeMs,
		ErrorMessage:   errMsg,
	}
	if err := s.healthRepo.CreateLog(log); err != nil {
		slog.Error("failed to create health check log", "error", err)
	}
	if !success {
		slog.Warn("health check failed", "type", targetType, "name", targetName, "provider", providerName, "error", errMsg)
	}
}

// TimelineEntry 时间线聚合桶（最多 100 个桶）
type TimelineEntry struct {
	CheckedAt      time.Time `json:"checked_at"`
	Success        bool      `json:"success"`
	ResponseTimeMs int       `json:"response_time_ms"`
	ErrorMessage   string    `json:"error_message,omitempty"`
}

// TimelineGroup 按模型+渠道分行的监控组
type TimelineGroup struct {
	TargetName    string          `json:"target_name"`
	TargetType    string          `json:"target_type"`
	ProviderName  string          `json:"provider_name"`
	HealthPercent int             `json:"health_percent"`
	TotalChecks   int             `json:"total_checks"`
	SuccessChecks int             `json:"success_checks"`
	Timeline      []TimelineEntry `json:"timeline"`
}

// TimelineResult 时间线监控结果
type TimelineResult struct {
	Lookback string          `json:"lookback"`
	Total    int             `json:"total"`
	Groups   []TimelineGroup `json:"groups"`
}

const timelineBuckets = 100

// GetTimeline 获取健康监控时间线数据
func (s *HealthCheckService) GetTimeline(lookback string, search string, offset int, limit int) (*TimelineResult, error) {
	cacheKey := fmt.Sprintf("health:timeline:%s:%s", lookback, search)
	if s.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if val, err := s.rdb.Get(ctx, cacheKey).Result(); err == nil {
			var full TimelineResult
			if json.Unmarshal([]byte(val), &full) == nil {
				slog.Debug("health timeline cache hit", "key", cacheKey, "total", full.Total)
				return paginateTimelineResult(&full, offset, limit), nil
			}
		} else if err != redis.Nil {
			slog.Warn("health timeline redis get failed", "error", err)
		}
	}

	duration := parseLookback(lookback)
	since := time.Now().Add(-duration)

	rows, err := s.healthRepo.GetTimelineBuckets(since, search, timelineBuckets)
	if err != nil {
		return nil, err
	}

	type aggKey struct{ targetID, providerID string }
	type agg struct {
		targetName    string
		providerName  string
		totalChecks   int
		successChecks int
		timeline      []TimelineEntry
	}
	groupMap := make(map[aggKey]*agg)
	keyOrder := make([]aggKey, 0)

	for _, r := range rows {
		k := aggKey{targetID: r.TargetID, providerID: r.ProviderID}
		a, ok := groupMap[k]
		if !ok {
			a = &agg{targetName: r.TargetName, providerName: r.ProviderName}
			groupMap[k] = a
			keyOrder = append(keyOrder, k)
		}
		a.totalChecks += r.TotalCount
		a.successChecks += r.OkCount
		a.timeline = append(a.timeline, TimelineEntry{
			CheckedAt:      r.BucketStart,
			Success:        r.TotalCount == r.OkCount,
			ResponseTimeMs: r.AvgRtt,
			ErrorMessage:   r.FirstErr,
		})
	}

	groups := make([]TimelineGroup, 0, len(groupMap))
	for _, k := range keyOrder {
		a := groupMap[k]
		g := TimelineGroup{
			TargetName:    a.targetName,
			TargetType:    "model",
			ProviderName:  a.providerName,
			TotalChecks:   a.totalChecks,
			SuccessChecks: a.successChecks,
			Timeline:      a.timeline,
		}
		if g.TotalChecks > 0 {
			g.HealthPercent = g.SuccessChecks * 100 / g.TotalChecks
		} else {
			g.HealthPercent = 100
		}
		groups = append(groups, g)
	}
	sortTimelineGroupsByHealth(groups)

	full := &TimelineResult{Lookback: lookback, Total: len(groups), Groups: groups}

	if s.rdb != nil {
		if data, err := json.Marshal(full); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := s.rdb.Set(ctx, cacheKey, data, 30*time.Second).Err(); err != nil {
				slog.Warn("health timeline redis set failed", "error", err)
			}
			cancel()
		}
	}

	return paginateTimelineResult(full, offset, limit), nil
}

func paginateTimelineResult(full *TimelineResult, offset int, limit int) *TimelineResult {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = len(full.Groups)
	}
	total := len(full.Groups)
	if offset >= total {
		return &TimelineResult{Lookback: full.Lookback, Total: total, Groups: []TimelineGroup{}}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return &TimelineResult{Lookback: full.Lookback, Total: total, Groups: full.Groups[offset:end]}
}

	func parseLookback(lookback string) time.Duration {
	switch lookback {
	case "12h":
		return 12 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 6 * time.Hour
	}
}

func sortTimelineGroupsByHealth(groups []TimelineGroup) {
	for i := 0; i < len(groups); i++ {
		for j := i + 1; j < len(groups); j++ {
			if groups[i].HealthPercent > groups[j].HealthPercent {
				groups[i], groups[j] = groups[j], groups[i]
			}
		}
	}
}

// GroupTimelineGroup 按模型组聚合的监控组
type GroupTimelineGroup struct {
	GroupName     string          `json:"group_name"`
	Strategy      string          `json:"strategy"`
	ModelCount    int             `json:"model_count"`
	HealthPercent int             `json:"health_percent"`
	TotalChecks   int             `json:"total_checks"`
	SuccessChecks int             `json:"success_checks"`
	Timeline      []TimelineEntry `json:"timeline"`
}

// GroupTimelineResult 模型组时间线监控结果
type GroupTimelineResult struct {
	Lookback string               `json:"lookback"`
	Total    int                  `json:"total"`
	Groups   []GroupTimelineGroup `json:"groups"`
}

// GetGroupTimeline 获取按模型组聚合的健康监控时间线数据
func (s *HealthCheckService) GetGroupTimeline(lookback string, search string, offset int, limit int) (*GroupTimelineResult, error) {
	cacheKey := fmt.Sprintf("health:group-timeline:%s:%s", lookback, search)
	if s.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if val, err := s.rdb.Get(ctx, cacheKey).Result(); err == nil {
			var full GroupTimelineResult
			if json.Unmarshal([]byte(val), &full) == nil {
				slog.Debug("health group timeline cache hit", "key", cacheKey, "total", full.Total)
				return paginateGroupTimelineResult(&full, offset, limit), nil
			}
		} else if err != redis.Nil {
			slog.Warn("health group timeline redis get failed", "error", err)
		}
	}

	duration := parseLookback(lookback)
	since := time.Now().Add(-duration)

	rows, metaMap, err := s.healthRepo.GetGroupTimelineBuckets(since, search, timelineBuckets)
	if err != nil {
		return nil, err
	}

	type agg struct {
		totalChecks   int
		successChecks int
		timeline      []TimelineEntry
	}
	groupMap := make(map[string]*agg)

	for _, r := range rows {
		a, ok := groupMap[r.GroupName]
		if !ok {
			a = &agg{}
			groupMap[r.GroupName] = a
		}
		a.totalChecks += r.TotalCount
		a.successChecks += r.OkCount
		a.timeline = append(a.timeline, TimelineEntry{
			CheckedAt:      r.BucketStart,
			Success:        r.TotalCount == r.OkCount,
			ResponseTimeMs: r.AvgRtt,
			ErrorMessage:   r.FirstErr,
		})
	}

	groups := make([]GroupTimelineGroup, 0, len(metaMap))

	// 先从日志数据填充有检测记录的组
	for name, a := range groupMap {
		g := GroupTimelineGroup{
			GroupName:     name,
			TotalChecks:   a.totalChecks,
			SuccessChecks: a.successChecks,
			Timeline:      a.timeline,
		}
		if g.TotalChecks > 0 {
			g.HealthPercent = g.SuccessChecks * 100 / g.TotalChecks
		} else {
			g.HealthPercent = 100
		}
		if meta, ok := metaMap[name]; ok {
			g.Strategy = meta.Strategy
			g.ModelCount = meta.ModelCount
		}
		groups = append(groups, g)
	}

	// 补充没有检测日志的组（显示为暂无数据）
	for name, meta := range metaMap {
		if _, exists := groupMap[name]; !exists {
			groups = append(groups, GroupTimelineGroup{
				GroupName:     name,
				Strategy:      meta.Strategy,
				ModelCount:    meta.ModelCount,
				HealthPercent: 100,
				TotalChecks:   0,
				SuccessChecks: 0,
				Timeline:      []TimelineEntry{},
			})
		}
	}
	sortGroupTimelineByHealth(groups)

	full := &GroupTimelineResult{Lookback: lookback, Total: len(groups), Groups: groups}

	if s.rdb != nil {
		if data, err := json.Marshal(full); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := s.rdb.Set(ctx, cacheKey, data, 30*time.Second).Err(); err != nil {
				slog.Warn("health group timeline redis set failed", "error", err)
			}
			cancel()
		}
	}

	return paginateGroupTimelineResult(full, offset, limit), nil
}

func paginateGroupTimelineResult(full *GroupTimelineResult, offset int, limit int) *GroupTimelineResult {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = len(full.Groups)
	}
	total := len(full.Groups)
	if offset >= total {
		return &GroupTimelineResult{Lookback: full.Lookback, Total: total, Groups: []GroupTimelineGroup{}}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return &GroupTimelineResult{Lookback: full.Lookback, Total: total, Groups: full.Groups[offset:end]}
}

// startGroupCheckBackground 启动模型组检测独立后台定时器（与模型检测间隔相同）
func (s *HealthCheckService) startGroupCheckBackground(ctx context.Context, config *model.HealthCheckConfig) {
	if s.groupCheckSvc == nil {
		return
	}
	if !config.ModelCheckEnabled {
		return
	}

	intervalSec := config.ModelCheckIntervalSeconds
	if intervalSec <= 0 {
		intervalSec = 300
	}

	s.mu.Lock()
	if s.groupCheckRunning {
		s.mu.Unlock()
		return
	}
	s.groupCheckRunning = true
	s.mu.Unlock()

	groupCtx, groupCancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.groupCheckCancel = groupCancel
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer ticker.Stop()
		slog.Info("group check background started", "interval_seconds", intervalSec)

		// 首次延迟 60 秒（错开渠道/模型检测）
		select {
		case <-groupCtx.Done():
			return
		case <-time.After(60 * time.Second):
			s.runBackgroundGroupCheck(groupCtx)
		}

		for {
			select {
			case <-groupCtx.Done():
				s.mu.Lock()
				s.groupCheckRunning = false
				s.mu.Unlock()
				return
			case <-ticker.C:
				cfg, err := s.healthRepo.GetConfig()
				if err != nil || !cfg.ModelCheckEnabled {
					s.mu.Lock()
					s.groupCheckRunning = false
					s.mu.Unlock()
					return
				}
				s.runBackgroundGroupCheck(groupCtx)
			}
		}
	}()
}

// runBackgroundGroupCheck 后台执行一轮模型组检测
func (s *HealthCheckService) runBackgroundGroupCheck(ctx context.Context) {
	if s.groupCheckSvc == nil {
		return
	}
	result := s.groupCheckSvc.CheckAll(ctx)
	slog.Info("background group check completed",
		"total", result.Total, "success", result.Success, "failed", result.Failed)
	s.invalidateGroupTimelineCache()
}

// invalidateGroupTimelineCache 清除模型组时间线 Redis 缓存
func (s *HealthCheckService) invalidateGroupTimelineCache() {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	iter := s.rdb.Scan(ctx, 0, "health:group-timeline:*", 0).Iterator()
	for iter.Next(ctx) {
		s.rdb.Del(ctx, iter.Val())
	}
	if err := iter.Err(); err != nil {
		slog.Warn("group timeline cache invalidate scan failed", "error", err)
	}
}


func sortGroupTimelineByHealth(groups []GroupTimelineGroup) {
	for i := 0; i < len(groups); i++ {
		for j := i + 1; j < len(groups); j++ {
			if groups[i].HealthPercent > groups[j].HealthPercent {
				groups[i], groups[j] = groups[j], groups[i]
			}
		}
	}
}


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
