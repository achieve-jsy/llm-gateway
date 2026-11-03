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
	baseproviders "github.com/example/llm-gateway/internal/providers"
	bedrockprovider "github.com/example/llm-gateway/internal/providers/bedrock"
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
	// Message 成功时的语义说明（如景汐：仅连通+鉴权，非视频创建）
	Message      string `json:"message,omitempty"`
	AutoDisabled bool   `json:"auto_disabled"` // 本次是否触发了自动禁用
	// AutoReenabled 本次是否因检测成功自动恢复（此前 status=3）
	AutoReenabled bool `json:"auto_reenabled,omitempty"`
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

	modelStatusLogSvc *ModelStatusLogService // 模型状态变更日志服务
	snapshotter       snapshotRefresher      // 自动禁用/恢复后刷新 Redis snapshot（可为 nil）

	mu                sync.Mutex
	cancel            context.CancelFunc
	modelCheckCancel  context.CancelFunc
	groupCheckCancel  context.CancelFunc
	running           bool
	modelCheckRunning bool
	groupCheckRunning bool
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

// snapshotRefresher 抽象「自动禁用/恢复后重建 gateway provider 快照」的能力。
// 生产实现是 *ProviderSnapshotRefresher（合并去抖 + 单飞）；抽成接口便于单测注入计数型假实现。
type snapshotRefresher interface {
	// ScheduleRefresh 请求一次异步、合并去抖的全量快照重建
	ScheduleRefresh(ctx context.Context)
}

// SetSnapshotRefresher 注入 provider snapshot 刷新器，自动禁用/恢复后同步通知 gateway
func (s *HealthCheckService) SetSnapshotRefresher(snapshotter *ProviderSnapshotRefresher) {
	if snapshotter == nil {
		s.snapshotter = nil
		return
	}
	s.snapshotter = snapshotter
}

// scheduleSnapshotRefresh 请求一次合并去抖的全量快照重建（未注入 snapshotter 时为空操作）
func (s *HealthCheckService) scheduleSnapshotRefresh(ctx context.Context) {
	if s.snapshotter == nil {
		return
	}
	s.snapshotter.ScheduleRefresh(ctx)
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
	// 重启调度器（provider + model + group）
	s.Stop()
	if config.Enabled {
		s.Start()
	}
	if config.Enabled && config.ModelCheckEnabled {
		s.startModelCheckBackground(context.Background(), config)
	}
	if config.Enabled && config.GroupCheckEnabled {
		s.startGroupCheckBackground(context.Background(), config)
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
			s.runBackgroundModelCheck(modelCtx)
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
				s.runBackgroundModelCheck(modelCtx)
			}
		}
	}()
}

// ─── 失败分类：确定性失败 / 临时性失败 ──────────────────────────────────────
//
// 区分两类失败的目的（2026-10-16 生产事故复盘）：
//   - 临时性上游故障（配额耗尽 429、5xx、超时）不该把模型/渠道永久踢出可用池——
//     事故中 27 个模型因 429/5xx 被自动禁用后 consecutive_recovery_successes 永远为 0、
//     永不恢复；
//   - 确定性故障（模型名不存在 404、鉴权失败 401/403、凭证解不开）重复探测不会自愈，
//     仍需按「连续 N 次失败」的阈值自动禁用，避免把错误持续暴露给线上流量。

// probeFailureKind 健康检测失败类别
type probeFailureKind int

const (
	// probeFailureTransient 临时性失败：上游暂时不可用，不累加连续失败、不触发自动禁用
	probeFailureTransient probeFailureKind = iota
	// probeFailureDeterministic 确定性失败：配置/数据/权限问题，累加连续失败，达阈值自动禁用
	probeFailureDeterministic
)

// transientFailureMarkers 无 HTTP 状态码时的临时性失败兜底判据（全部按小写匹配）。
// 覆盖网络/超时类错误，以及上游限流、服务不可用的常见错误类型名。
var transientFailureMarkers = []string{
	"throttl",                 // ThrottlingException / ThrottlingException
	"too many requests",       // 通用限流文案
	"requestlimitexceeded",    // AWS RequestLimitExceeded
	"toomanyrequests",         // AWS TooManyRequestsException
	"rate limit",              // 通用限流文案
	"serviceunavailable",      // AWS ServiceUnavailableException
	"service unavailable",     // 上游 503 文案
	"temporarily unavailable", // 上游 503 文案
	"internal server error",   // 上游 500 文案
	"internalerror",           // AWS InternalServerError / internal error
	"requesttimeout",          // AWS RequestTimeout
	"timeout",                 // 通用超时
	"timed out",               // 通用超时
	"deadline exceeded",       // context deadline exceeded
	"connection refused",      // 连接被拒
	"connection reset",        // 连接被重置
	"connection closed",       // 连接被关闭
	"reset by peer",           // 对端重置
	"broken pipe",             // 写管道断开
	"no such host",            // DNS 解析失败
	"unexpected eof",          // 上游提前断开
	"i/o timeout",             // 网络超时
	"tls handshake",           // TLS 握手失败（上游临时抖动）
}

// deterministicFailureMarkers 无 HTTP 状态码时的确定性失败兜底判据（全部按小写匹配）。
var deterministicFailureMarkers = []string{
	"model not exist",             // 上游 404 文案（生产 43 个模型属于此类）
	"model not found",             // 上游 404 文案
	"model_not_found",             // 上游错误码
	"does not exist",              // 通用「不存在」文案
	"invalid model",               // 模型名非法
	"invalid api key",             // 鉴权失败文案
	"invalid_api_key",             // 鉴权失败错误码
	"incorrect api key",           // 鉴权失败文案
	"unauthorized",                // 上游 401 文案
	"authentication",              // 鉴权类文案
	"access denied",               // 上游 403 文案
	"accessdenied",                // AWS AccessDeniedException
	"permission denied",           // 权限类文案
	"forbidden",                   // 上游 403 文案
	"not authorized",              // 权限类文案
	"validationexception",         // AWS 参数/模型校验失败
	"unrecognizedclientexception", // AWS 凭证无效
	"invalidsignatureexception",   // AWS 签名无效
	"expired",                     // 凭证过期
	"解密凭证失败",                      // 本地：EncryptedCredentials 无法解密
	"decrypt credentials failed",  // 本地：EncryptedCredentials 无法解密
	"渠道不存在",                       // 本地：provider 记录缺失
	"provider not found",          // 本地：provider 记录缺失
	"aws 配置加载失败",                  // 本地：AWS 配置加载失败
}

// classifyProbeFailure 把检测失败归类为确定性/临时性。
//
// 判据优先级：
//  1. 探针自己写入的 HTTP 状态码（doProbeRequest / checkConnectivity 统一生成 "HTTP %d: ..."），
//     这是最可靠的机器可读信号，不依赖上游文案：
//     408/425/429/5xx → 临时性；其余 4xx → 确定性；
//  2. 无状态码时按已知错误类型/关键词兜底（见上面两个 marker 列表）；
//  3. 仍无法归类 → 临时性（保守：宁可下一轮再探测一次，也不把可能可用的对象永久剔除）。
func classifyProbeFailure(errMsg string) probeFailureKind {
	if code, ok := probeHTTPStatusCode(errMsg); ok {
		switch {
		case code == http.StatusRequestTimeout || code == http.StatusTooEarly || code == http.StatusTooManyRequests:
			return probeFailureTransient
		case code >= 500:
			return probeFailureTransient
		case code >= 400:
			return probeFailureDeterministic
		default:
			return probeFailureTransient
		}
	}

	lower := strings.ToLower(errMsg)
	for _, marker := range transientFailureMarkers {
		if strings.Contains(lower, marker) {
			return probeFailureTransient
		}
	}
	for _, marker := range deterministicFailureMarkers {
		if strings.Contains(lower, marker) {
			return probeFailureDeterministic
		}
	}
	return probeFailureTransient
}

// probeHTTPStatusCode 提取探针错误文本里自己写入的 HTTP 状态码。
// 形如 "HTTP 502: <body>"，可能带前缀（checkModelForward 会加 "model forward "）。
// 取第一个 "HTTP " 之后的 3 位数字，且必须落在 100..599 之间。
func probeHTTPStatusCode(errMsg string) (int, bool) {
	const marker = "http "
	lower := strings.ToLower(errMsg)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return 0, false
	}
	rest := lower[idx+len(marker):]
	if len(rest) < 3 {
		return 0, false
	}
	code := 0
	for i := 0; i < 3; i++ {
		c := rest[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		code = code*10 + int(c-'0')
	}
	if code < 100 || code > 599 {
		return 0, false
	}
	return code, true
}

// runBackgroundModelCheck 后台执行一轮模型检测。
//
// ctx 为轮次上下文（定时器上下文）：仅用于判断「自动禁用/恢复触发的快照重建」是否还有必要，
// 单次重建本身由 ProviderSnapshotRefresher 用独立的超时上下文执行。
func (s *HealthCheckService) runBackgroundModelCheck(ctx context.Context) {
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

	var total, success, failed, autoDisabled, reenabled, transientFailed int
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
				recovered := s.handleModelCheckSuccess(mod, elapsed, errMsg, recoveryEnabled, recoveryThreshold, ctx)
				mu.Lock()
				success++
				if recovered {
					reenabled++
				}
				mu.Unlock()
				return
			}

			disabled, kind := s.handleModelCheckFailure(mod, elapsed, errMsg, maxFailures, ctx)
			mu.Lock()
			failed++
			if disabled {
				autoDisabled++
			}
			if kind == probeFailureTransient {
				transientFailed++
			}
			mu.Unlock()
		}(m)
	}

	wg.Wait()
	slog.Info("background model check completed",
		"total", total, "success", success, "failed", failed,
		"auto_disabled", autoDisabled, "transient_failed", transientFailed, "recovered", reenabled)
	s.invalidateTimelineCache()
}

// handleModelCheckSuccess 处理模型检测成功：清零连续失败计数、写检测结果，
// 自动禁用态（status=3）的模型再按恢复策略决定是否自动恢复。
// 返回本轮是否发生了自动恢复。
func (s *HealthCheckService) handleModelCheckSuccess(mod *model.Model, elapsed int, errMsg string, recoveryEnabled bool, recoveryThreshold int, ctx context.Context) bool {
	// 成功即归零「连续失败」，保证阈值语义始终是「连续 N 次失败」
	s.modelRepo.ResetModelFailures(mod.ID)
	s.modelRepo.UpdateModelCheckResult(mod.ID, elapsed, errMsg)
	s.recordLog("model", mod.ID, mod.Name, mod.Provider.ID, mod.Provider.Name, true, elapsed, errMsg)

	// 恢复逻辑：自动禁用的模型检测成功时处理恢复条件
	if mod.Status != model.ModelStatusAutoDisabled || !recoveryEnabled {
		return false
	}

	newRecoveryCount := mod.ConsecutiveRecoverySuccesses + 1
	slog.Info("model auto-recovery: success count increased",
		"model_id", mod.ID, "model_name", mod.Name,
		"recovery_successes", newRecoveryCount, "threshold", recoveryThreshold)

	if newRecoveryCount < recoveryThreshold {
		// 未达阈值：递增 recovery_successes
		s.modelRepo.IncrementRecoverySuccesses(mod.ID)
		return false
	}

	slog.Warn("model auto-recovery: threshold reached, re-enabling",
		"model_id", mod.ID, "model_name", mod.Name,
		"recovery_successes", newRecoveryCount)
	if err := s.modelRepo.AutoEnableModel(mod.ID); err != nil {
		return false
	}
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
	// 请求刷新 Redis snapshot（合并去抖），让 gateway 立即感知模型恢复
	s.scheduleSnapshotRefresh(ctx)
	return true
}

// handleModelCheckFailure 处理模型检测失败。
//
// 语义（修复重复禁用 churn + 临时故障被永久淘汰）：
//   - 已处于自动禁用态（status=3）的模型：本轮只探测「能否恢复」，失败时不再累加
//     consecutive_failures、不再重复执行自动禁用与快照重建（幂等短路）；
//   - 临时性失败（429/5xx/超时/网络等）：不计入连续失败、不自动禁用，只写检测结果与日志，
//     上游恢复后下一轮成功即把计数归零；
//   - 确定性失败（404/401/403/凭证错误等）：累加连续失败计数，达到阈值才自动禁用。
//
// 返回：(本轮是否真的触发了自动禁用, 失败类别)。
func (s *HealthCheckService) handleModelCheckFailure(mod *model.Model, elapsed int, errMsg string, maxFailures int, ctx context.Context) (bool, probeFailureKind) {
	kind := classifyProbeFailure(errMsg)
	alreadyAutoDisabled := mod.Status == model.ModelStatusAutoDisabled

	s.modelRepo.UpdateModelCheckResult(mod.ID, elapsed, errMsg)
	s.recordLog("model", mod.ID, mod.Name, mod.Provider.ID, mod.Provider.Name, false, elapsed, errMsg)

	// 失败时重置恢复成功计数（已有计数才写库，避免每轮无谓 UPDATE）
	if alreadyAutoDisabled && mod.ConsecutiveRecoverySuccesses != 0 {
		s.modelRepo.ResetRecoverySuccesses(mod.ID)
		slog.Info("model auto-recovery: reset recovery successes due to failure",
			"model_id", mod.ID, "model_name", mod.Name)
	}

	// 幂等短路：已自动禁用的模型不可能「再次被禁用」，重复累加计数与重复触发快照只会放大 churn
	if alreadyAutoDisabled {
		return false, kind
	}

	if kind == probeFailureTransient {
		slog.Warn("model check: transient failure, skip auto-disable",
			"model_id", mod.ID, "model_name", mod.Name, "error", errMsg)
		return false, kind
	}

	newCount, err := s.modelRepo.IncrementModelFailures(mod.ID)
	if err != nil || newCount < maxFailures {
		return false, kind
	}

	slog.Warn("model check: auto-disabling model",
		"model_id", mod.ID, "model_name", mod.Name,
		"failures", newCount, "threshold", maxFailures)
	if err := s.modelRepo.AutoDisableModel(mod.ID); err != nil {
		return false, kind
	}
	if s.modelStatusLogSvc != nil {
		s.modelStatusLogSvc.LogStatusChange(
			mod.ID, mod.Provider.ID,
			mod.Name, mod.Provider.Name,
			model.ChangeTypeAutoDisabled,
			fmt.Sprintf("连续 %d 次健康检测失败", newCount),
			errMsg, nil, "",
		)
	}
	// 请求刷新 Redis snapshot（合并去抖：一轮 72 次禁用 → 1~2 次全量重建）
	s.scheduleSnapshotRefresh(ctx)
	return true, kind
}

// 时间线缓存 key 与版本号 key。
//
// 缓存失效改为「版本号」：
//   - 旧实现用 SCAN MATCH health:timeline:* (COUNT 取默认 10) + 逐 key DEL，
//     Redis keyspace 较大时会产生成百上千次往返，2s 上限必然 context deadline exceeded，
//     且 key 删不干净（生产日志每轮都报 health timeline cache invalidate scan failed）；
//   - 新实现只 INCR 一个版本号，缓存 key 内嵌版本号：旧 key 不再被读取，
//     由自身 30s TTL 自然回收。失效成本 O(1)，与 keyspace 规模无关。
const (
	timelineCacheVersionKey      = "health:timeline:version"
	groupTimelineCacheVersionKey = "health:group-timeline:version"
)

// timelineCacheKey 生成带版本号的时间线缓存 key
func timelineCacheKey(version int64, lookback, search string) string {
	return fmt.Sprintf("health:timeline:v%d:%s:%s", version, lookback, search)
}

// groupTimelineCacheKey 生成带版本号的模型组时间线缓存 key
func groupTimelineCacheKey(version int64, lookback, search string) string {
	return fmt.Sprintf("health:group-timeline:v%d:%s:%s", version, lookback, search)
}

// timelineCacheVersion 读取时间线缓存版本号（key 不存在或读取失败都按 0 处理）
func (s *HealthCheckService) timelineCacheVersion(key string) int64 {
	if s.rdb == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	version, err := s.rdb.Get(ctx, key).Int64()
	if err != nil {
		if err != redis.Nil {
			slog.Debug("health timeline cache version get failed", "key", key, "error", err)
		}
		return 0
	}
	return version
}

// bumpTimelineCacheVersion 递增缓存版本号，一次性让该命名空间下的所有缓存失效
func (s *HealthCheckService) bumpTimelineCacheVersion(key string) {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.rdb.Incr(ctx, key).Err(); err != nil {
		slog.Warn("health timeline cache invalidate failed", "key", key, "error", err)
	}
}

// invalidateTimelineCache 失效健康监控时间线缓存（O(1)，不再 SCAN 整个 keyspace）
func (s *HealthCheckService) invalidateTimelineCache() {
	s.bumpTimelineCacheVersion(timelineCacheVersionKey)
}

// BedrockProbeMaxTokens 是 Bedrock 探针下发的 max_tokens。
//
// 不能取 1：Bedrock 上 OpenAI GPT 系列 / xAI Grok 系列要求 max_output_tokens >= 16，
// 否则上游返回 ValidationException（integer_below_min_value），探针会把完全正常的
// 模型判为不可用并在连续失败 3 次后自动禁用。16 对所有 Bedrock 模型都是合法值。
// 该常量导出给 handler（渠道测试）与 model_check_public（手动检测）复用，
// 避免同一条探针逻辑在多处各自写死 max_tokens。
const BedrockProbeMaxTokens int32 = 16

// BedrockProbeTimeout 是 Bedrock Converse 探针的超时。
// 不能取 15s：gpt-6-astra 这类慢模型的探针耗时稳定超过 15s（实测 6 小时内约 1/3 的检查报
// context deadline exceeded，模型因此被误判为不可用并反复自动禁用）。60s 在 5 分钟的检测
// 周期预算内留足余量，同时仍能识别真正挂掉的上游。
const BedrockProbeTimeout = 60 * time.Second

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

	// 非 OpenAI 兼容：按 definition_id 分发（禁止一律走 Bedrock）
	if isNonOpenAICompatible(provider.DefinitionID) {
		switch provider.DefinitionID {
		case "bedrock":
			return s.doBedrockModelCheck(provider, credJSON, m)
		case "seedance-relay":
			apiKey := extractAPIKey(credJSON)
			res := ProbeSeedanceRelayConnectivity(context.Background(), provider.Endpoint, apiKey)
			return res.Success, res.ResponseTime, res.Message
		case "seedance-volcengine":
			cred := ParseSeedanceVolcCreds(credJSON)
			if !cred.HasAuth() {
				return false, 0, "seedance-volcengine: missing credentials for selected auth_type"
			}
			res := ProbeSeedanceVolcengineConnectivityCreds(context.Background(), provider.Endpoint, cred)
			return res.Success, res.ResponseTime, res.Message
		case "azure":
			apiKey := extractAPIKey(credJSON)
			testModel := m.UpstreamName()
			if testModel == "" {
				testModel = provider.TestModel
			}
			if testModel == "" {
				res := ProbeAzureConnectivity(context.Background(), provider.Endpoint, apiKey)
				return res.Success, res.ResponseTime, res.Message
			}
			res := ProbeAzureChat(context.Background(), provider.Endpoint, apiKey, testModel)
			return res.Success, res.ResponseTime, res.Message
		case "anthropic":
			apiKey := extractAPIKey(credJSON)
			testModel := m.UpstreamName()
			if testModel == "" {
				testModel = provider.TestModel
			}
			if testModel == "" {
				return true, 0, "skipped: anthropic forward requires test_model"
			}
			ok, elapsed, msg := ProbeAnthropicMessages(context.Background(), provider.Endpoint, apiKey, testModel)
			return ok, elapsed, msg
		case "gemini":
			apiKey := extractAPIKey(credJSON)
			testModel := m.UpstreamName()
			if testModel == "" {
				testModel = provider.TestModel
			}
			if testModel == "" {
				return true, 0, "skipped: gemini forward requires test_model"
			}
			ok, elapsed, msg := ProbeGeminiGenerate(context.Background(), provider.Endpoint, apiKey, testModel)
			return ok, elapsed, msg
		default:
			return true, 0, "skipped: unsupported provider type for auto-check"
		}
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

	// 走真实 adapter：凭证解析（IAM JSON / Bearer API Key）、region 提取、
	// Converse 请求构造都在 bedrock 包内，探针不再复刻一份。
	//
	// 这样探针不会比转发更宽容：原先这里会给 region 缺失的 endpoint 兜底
	// us-east-1，而 adapter 不兜底，于是「探针通过、线上必然失败」。
	p := bedrockprovider.New(baseproviders.ProviderConfig{
		APIKey:  apiKey,
		BaseURL: provider.Endpoint,
	}, baseproviders.ProviderOptions{})

	ok, elapsed, errMsg := probeViaAdapter(context.Background(), p, m.UpstreamName())
	if !ok {
		return false, elapsed, errMsg
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
		// 启用渠道正常检测；自动禁用 (status=3) 的渠道继续检测以便自动恢复；手动禁用跳过
		if !p.Enabled && p.Status != model.ProviderStatusAutoDisabled {
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
		s.handleProviderFailure(provider, maxFailures, cr)
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
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}
		// 仅连通性模式直接返回成功
		if checkMode == "connectivity" {
			cr.Success = true
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, elapsed, "")
			s.finalizeProviderSuccess(provider, cr)
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
				s.finalizeProviderSuccess(provider, cr)
				return cr
			}
			cr.Error = errMsg
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, cr.ResponseTime, errMsg)
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}
	}

	// 全部通过
	cr.Success = true
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, "")
	s.finalizeProviderSuccess(provider, cr)
	return cr
}

// isNonOpenAICompatible 返回该 definition_id 是否为非 OpenAI 兼容协议。
// 注意：仅为「不要走 GET /models / chat 通用路径」的标记；调用方必须再按 definition_id 分发，
// 禁止把 seedance-relay 等误送进 Bedrock SDK。
func isNonOpenAICompatible(definitionID string) bool {
	switch definitionID {
	case "bedrock", "seedance-relay", "seedance-volcengine", "azure", "vertex":
		return true
	// Anthropic / Gemini 有各自的原生协议：通用探针打 /chat/completions 只会拿到 404，
	// 导致正常渠道被「连续失败」自动禁用，原生旁路渠道因此永远活不过探测周期。
	case "anthropic", "gemini":
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
	case "seedance-relay":
		return s.checkSeedanceRelayProvider(ctx, provider, credJSON, checkMode, maxFailures, cr)
	case "seedance-volcengine":
		return s.checkSeedanceVolcengineProvider(ctx, provider, credJSON, checkMode, maxFailures, cr)
	case "azure":
		return s.checkAzureProvider(ctx, provider, credJSON, checkMode, maxFailures, cr)
	case "vertex":
		return s.checkVertexProvider(ctx, provider, credJSON, checkMode, maxFailures, cr)
	case "anthropic":
		return s.checkNativeProtocolProvider(ctx, provider, credJSON, maxFailures, cr, "anthropic", ProbeAnthropicMessages)
	case "gemini":
		return s.checkNativeProtocolProvider(ctx, provider, credJSON, maxFailures, cr, "gemini", ProbeGeminiGenerate)
	default:
		cr.Success = true // 未知类型跳过，不计失败
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, 0, "skipped: unsupported provider type for auto-check")
		return cr
	}
}

// checkAzureProvider Azure OpenAI v1 健康检测（与管理面「测试连接」共用探针）。
func (s *HealthCheckService) checkAzureProvider(ctx context.Context, provider *model.Provider, credJSON string, checkMode string, maxFailures int, cr *CheckResult) *CheckResult {
	apiKey := extractAPIKey(credJSON)

	if checkMode == "connectivity" || checkMode == "both" {
		res := ProbeAzureConnectivity(ctx, provider.Endpoint, apiKey)
		cr.ResponseTime = res.ResponseTime
		if !res.Success {
			cr.Error = res.Message
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, res.ResponseTime, res.Message)
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}
		if checkMode == "connectivity" {
			cr.Success = true
			cr.Message = res.Message
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
			s.finalizeProviderSuccess(provider, cr)
			return cr
		}
		cr.Message = res.Message
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
	}

	if checkMode == "model_forward" || checkMode == "both" {
		testModel := provider.TestModel
		if testModel == "" {
			skipMsg := "skipped: azure forward requires test_model"
			cr.Success = true
			if cr.Message != "" {
				cr.Message = cr.Message + "; " + skipMsg
			} else {
				cr.Message = skipMsg
			}
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, skipMsg)
			s.finalizeProviderSuccess(provider, cr)
			return cr
		}
		res := ProbeAzureChat(ctx, provider.Endpoint, apiKey, testModel)
		cr.ResponseTime = res.ResponseTime
		if !res.Success {
			cr.Error = res.Message
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, res.ResponseTime, res.Message)
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}
		cr.Success = true
		if cr.Message != "" {
			cr.Message = cr.Message + "; " + res.Message
		} else {
			cr.Message = res.Message
		}
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
		s.finalizeProviderSuccess(provider, cr)
		return cr
	}

	cr.Success = true
	s.finalizeProviderSuccess(provider, cr)
	return cr
}

// checkVertexProvider Google Vertex (native) 健康检测（与管理面「测试连接」共用 ProbeVertexGenerate）。
// 原生面走 /models/{model}:generateContent + x-goog-api-key；
// 非原生面（未以 /publishers/google 结尾）跳过，不判失败，避免误标记 provider 下线。
func (s *HealthCheckService) checkVertexProvider(ctx context.Context, provider *model.Provider, credJSON string, checkMode string, maxFailures int, cr *CheckResult) *CheckResult {
	endpoint := strings.TrimRight(provider.Endpoint, "/")
	if !strings.HasSuffix(endpoint, "/publishers/google") {
		cr.Success = true
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, 0, "skipped: vertex non-native endpoint, use admin test")
		return cr
	}
	apiKey := extractAPIKey(credJSON)
	testModel := strings.TrimSpace(provider.TestModel)
	if testModel == "" {
		testModel = "gemini-1.5-flash"
	}
	res := ProbeVertexGenerate(ctx, provider.Endpoint, apiKey, testModel)
	cr.ResponseTime = res.ResponseTime
	if !res.Success {
		cr.Error = res.Message
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, res.ResponseTime, res.Message)
		s.handleProviderFailure(provider, maxFailures, cr)
		return cr
	}
	cr.Success = true
	cr.Message = res.Message
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
	s.finalizeProviderSuccess(provider, cr)
	return cr
}

// checkSeedanceRelayProvider 景汐中转健康检测（与管理面「测试连接」共用 ProbeSeedanceRelayConnectivity）。
//
// 连通性：GET .../tasks/__gateway_health_probe__ —— 仅验证网络可达 + Bearer 鉴权（+ 非 5xx）。
// 转发：本期 skip（不创建视频任务，不消耗生成额度）。
func (s *HealthCheckService) checkSeedanceRelayProvider(ctx context.Context, provider *model.Provider, credJSON string, checkMode string, maxFailures int, cr *CheckResult) *CheckResult {
	apiKey := extractAPIKey(credJSON)

	if checkMode == "connectivity" || checkMode == "both" {
		res := ProbeSeedanceRelayConnectivity(ctx, provider.Endpoint, apiKey)
		cr.ResponseTime = res.ResponseTime
		if !res.Success {
			cr.Error = res.Message
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, res.ResponseTime, res.Message)
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}
		if checkMode == "connectivity" {
			cr.Success = true
			cr.Message = res.Message
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
			s.finalizeProviderSuccess(provider, cr)
			return cr
		}
		// both：连通已通过，继续转发段（skip）
		cr.Message = res.Message
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
	}

	if checkMode == "model_forward" || checkMode == "both" {
		skipMsg := "skipped: seedance-relay probe is connectivity+auth only (no video create)"
		if checkMode == "model_forward" {
			// 仅转发模式：仍跑一次连通+鉴权探针（与「检测=探测」一致），不做视频创建
			res := ProbeSeedanceRelayConnectivity(ctx, provider.Endpoint, apiKey)
			cr.ResponseTime = res.ResponseTime
			if !res.Success {
				cr.Error = res.Message
				s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, res.ResponseTime, res.Message)
				s.handleProviderFailure(provider, maxFailures, cr)
				return cr
			}
			cr.Success = true
			cr.Message = res.Message + "; " + skipMsg
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, cr.Message)
			s.finalizeProviderSuccess(provider, cr)
			return cr
		}
		// both：连通已成功，转发 skip 记成功
		cr.Success = true
		if cr.Message != "" {
			cr.Message = cr.Message + "; " + skipMsg
		} else {
			cr.Message = skipMsg
		}
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, cr.Message)
		s.finalizeProviderSuccess(provider, cr)
		return cr
	}

	cr.Success = true
	if cr.Message == "" {
		cr.Message = "ok: connectivity+auth only"
	}
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, cr.Message)
	s.finalizeProviderSuccess(provider, cr)
	return cr
}

// checkSeedanceVolcengineProvider 官方 Seedance 健康检测（Bearer/HMAC 连通+鉴权，不创建视频）。
func (s *HealthCheckService) checkSeedanceVolcengineProvider(ctx context.Context, provider *model.Provider, credJSON string, checkMode string, maxFailures int, cr *CheckResult) *CheckResult {
	cred := ParseSeedanceVolcCreds(credJSON)
	if !cred.HasAuth() {
		cr.Error = "seedance-volcengine: missing credentials for selected auth_type"
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, 0, cr.Error)
		s.handleProviderFailure(provider, maxFailures, cr)
		return cr
	}

	runProbe := func() SeedanceRelayProbeResult {
		return ProbeSeedanceVolcengineConnectivityCreds(ctx, provider.Endpoint, cred)
	}
	skipMsg := "skipped: seedance-volcengine probe is connectivity+auth only (no video create)"

	if checkMode == "connectivity" || checkMode == "both" {
		res := runProbe()
		cr.ResponseTime = res.ResponseTime
		if !res.Success {
			cr.Error = res.Message
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, res.ResponseTime, res.Message)
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}
		cr.Message = res.Message
		if checkMode == "connectivity" {
			cr.Success = true
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
			s.finalizeProviderSuccess(provider, cr)
			return cr
		}
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, res.Message)
	}

	if checkMode == "model_forward" || checkMode == "both" {
		if checkMode == "model_forward" {
			res := runProbe()
			cr.ResponseTime = res.ResponseTime
			if !res.Success {
				cr.Error = res.Message
				s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, res.ResponseTime, res.Message)
				s.handleProviderFailure(provider, maxFailures, cr)
				return cr
			}
			cr.Success = true
			cr.Message = res.Message + "; " + skipMsg
			s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, res.ResponseTime, cr.Message)
			s.finalizeProviderSuccess(provider, cr)
			return cr
		}
		cr.Success = true
		if cr.Message != "" {
			cr.Message = cr.Message + "; " + skipMsg
		} else {
			cr.Message = skipMsg
		}
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, cr.Message)
		s.finalizeProviderSuccess(provider, cr)
		return cr
	}

	cr.Success = true
	if cr.Message == "" {
		cr.Message = "ok: connectivity+auth only"
	}
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, cr.Message)
	s.finalizeProviderSuccess(provider, cr)
	return cr
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

	probeCtx, cancel := context.WithTimeout(ctx, BedrockProbeTimeout)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(probeCtx, loadOpts...)
	if err != nil {
		cr.Error = "AWS 配置加载失败: " + err.Error()
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, 0, cr.Error)
		s.handleProviderFailure(provider, maxFailures, cr)
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
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}

		_ = out // model count available at out.ModelSummaries if needed
		cr.ResponseTime = elapsed
		s.finalizeProviderSuccess(provider, cr)

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
		maxTokens := BedrockProbeMaxTokens
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
			s.handleProviderFailure(provider, maxFailures, cr)
			return cr
		}
	}

	cr.Success = true
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, cr.ResponseTime, "")
	s.finalizeProviderSuccess(provider, cr)
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

// finalizeProviderSuccess 渠道检测成功收尾：更新耗时、清失败计数；
// 若此前被自动禁用（status=3）则立即恢复并刷新 snapshot（连通探针较稳，一次成功即可恢复）。
func (s *HealthCheckService) finalizeProviderSuccess(provider *model.Provider, cr *CheckResult) {
	s.providerRepo.UpdateResponseTime(provider.ID, cr.ResponseTime)
	s.providerRepo.ResetFailures(provider.ID)
	if provider.Status != model.ProviderStatusAutoDisabled {
		return
	}
	if err := s.providerRepo.AutoEnable(provider.ID); err != nil {
		slog.Error("provider auto-recovery: failed to re-enable",
			"provider_id", provider.ID, "error", err)
		return
	}
	cr.AutoReenabled = true
	provider.Enabled = true
	provider.Status = model.ProviderStatusEnabled
	slog.Warn("provider auto-recovery: re-enabled after successful health check",
		"provider_id", provider.ID, "provider_name", provider.Name)
	// 请求刷新 Redis snapshot（合并去抖），让 gateway 立即感知渠道恢复
	s.scheduleSnapshotRefresh(context.Background())
}

// handleProviderFailure 处理渠道检测失败。
//
// 语义与 handleModelCheckFailure 对齐：
//   - 已处于自动禁用态（status=3）的渠道：幂等短路，失败不再累加计数、不再重复禁用；
//   - 临时性失败（429/5xx/超时/网络/上游限流）：不计入连续失败、不自动禁用；
//   - 确定性失败（4xx/凭证解密失败等）：累加连续失败，达到阈值才自动禁用。
func (s *HealthCheckService) handleProviderFailure(provider *model.Provider, maxFailures int, cr *CheckResult) {
	if provider == nil {
		return
	}
	if provider.Status == model.ProviderStatusAutoDisabled {
		// 已自动禁用：继续检测只为自动恢复，失败无需再记账（避免 consecutive_failures 无限增长）
		return
	}
	if classifyProbeFailure(cr.Error) == probeFailureTransient {
		slog.Warn("provider check: transient failure, skip auto-disable",
			"provider_id", provider.ID, "provider_name", provider.Name, "error", cr.Error)
		return
	}

	newCount, err := s.providerRepo.IncrementFailures(provider.ID)
	if err != nil {
		slog.Error("failed to increment failures", "provider_id", provider.ID, "error", err)
		return
	}

	if newCount >= maxFailures {
		slog.Warn("auto-disabling provider due to consecutive failures",
			"provider_id", provider.ID,
			"failures", newCount,
			"threshold", maxFailures,
		)
		if err := s.providerRepo.AutoDisable(provider.ID); err != nil {
			slog.Error("failed to auto-disable provider", "provider_id", provider.ID, "error", err)
			return
		}
		cr.AutoDisabled = true
	}
}

// RunNow 手动触发一次检测（异步，兼容旧调用）
func (s *HealthCheckService) RunNow() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.CheckAll(ctx)
	}()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.runBackgroundModelCheck(ctx)
	}()
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
	version := s.timelineCacheVersion(timelineCacheVersionKey)
	cacheKey := timelineCacheKey(version, lookback, search)
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
	version := s.timelineCacheVersion(groupTimelineCacheVersionKey)
	cacheKey := groupTimelineCacheKey(version, lookback, search)
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

// startGroupCheckBackground 启动模型组检测独立后台定时器（使用独立间隔配置）
func (s *HealthCheckService) startGroupCheckBackground(ctx context.Context, config *model.HealthCheckConfig) {
	if s.groupCheckSvc == nil {
		return
	}
	if !config.GroupCheckEnabled {
		slog.Info("group check background disabled (group_check_enabled=false)")
		return
	}

	intervalSec := config.GroupCheckIntervalSeconds
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
				if err != nil || !cfg.GroupCheckEnabled {
					s.mu.Lock()
					s.groupCheckRunning = false
					s.mu.Unlock()
					slog.Info("group check background stopping (disabled in config)")
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

// invalidateGroupTimelineCache 失效模型组时间线缓存（与 invalidateTimelineCache 同机制）
func (s *HealthCheckService) invalidateGroupTimelineCache() {
	s.bumpTimelineCacheVersion(groupTimelineCacheVersionKey)
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

// checkNativeProtocolProvider 对原生协议渠道（anthropic / gemini）做模型前向探测。
//
// 通用探针走 /chat/completions，这两类渠道必然 404，会被误判成「连续失败」后自动禁用。
// 探针按各家原生协议发最小请求，成功/失败仍走统一的 handleProviderFailure 计数，
// 因此只有真实故障才会触发禁用。
func (s *HealthCheckService) checkNativeProtocolProvider(
	ctx context.Context, provider *model.Provider, credJSON string, maxFailures int, cr *CheckResult,
	protocol string,
	probe func(ctx context.Context, endpoint, apiKey, model string) (bool, int, string),
) *CheckResult {
	apiKey := extractAPIKey(credJSON)
	testModel := strings.TrimSpace(provider.TestModel)
	if testModel == "" {
		// 没配 test_model 时无法构造原生请求，跳过且不计失败（与 azure 分支语义一致）。
		cr.Success = true
		msg := "skipped: " + protocol + " forward requires test_model"
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, 0, msg)
		return cr
	}

	ok, elapsed, msg := probe(ctx, provider.Endpoint, apiKey, testModel)
	cr.ResponseTime = elapsed
	if !ok {
		cr.Error = msg
		s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, false, elapsed, msg)
		s.handleProviderFailure(provider, maxFailures, cr)
		return cr
	}

	cr.Success = true
	cr.Message = msg
	s.recordLog("provider", provider.ID, provider.Name, provider.ID, provider.Name, true, elapsed, msg)
	s.finalizeProviderSuccess(provider, cr)
	return cr
}
