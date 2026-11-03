package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

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
	modelCheckCancel  context.CancelFunc
	groupCheckCancel  context.CancelFunc
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
	// 渠道级检测已移除，只剩模型级与组级需要重启。
	// 必须显式停掉再起：两者各有自己的 running 标志，
	// 不停就会直接 return，新配置根本不生效。
	s.stopModelCheckBackground()
	s.stopGroupCheckBackground()
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

// 两个后台检测的首跑延迟。
//
// 错开 15 秒是刻意的：两类检测共用同一条上游链路，同时启动会把上游的
// 启动期抖动放大成一批失败，进而误触发自动禁用。
// 之前这三个数字分别硬编码在各自的 goroutine 里，看不出是同一套节奏。
const (
	modelCheckStartDelay = 30 * time.Second
	groupCheckStartDelay = 45 * time.Second
)

// StartBackground 启动后台定时检测（模型级 + 组级）。
//
// 渠道级定时检测已移除：渠道是否可用由它下面的模型体现，逐模型检测已经覆盖，
// 再用一个代表模型复测整个渠道是冗余的。渠道的启用/禁用改由人工决定，
// 需要验证连通性时仍可调 POST /providers/:id/test。
//
// 开关完全由 health_check_configs.enabled 控制，不再读 HEALTH_CHECK_INTERVAL：
// 环境变量每次启动都会覆盖配置表，反而让界面上改的设置失效。
func (s *HealthCheckService) StartBackground(ctx context.Context) {
	config, err := s.healthRepo.GetConfig()
	if err != nil {
		slog.Warn("failed to get health check config for background start", "error", err)
		return
	}

	// 尊重配置表里的总开关：用户关掉自动检测后，重启不应该把它偷偷开起来。
	if !config.Enabled {
		slog.Info("health check background disabled (config.enabled=false)")
		return
	}

	s.startModelCheckBackground(ctx, config)
	s.startGroupCheckBackground(ctx, config)
}

// IsRunning 是否有后台检测在运行。
//
// 渠道级检测移除后，这个状态只反映模型级与组级——两者任一在跑即为 true。
// 管理界面用它显示「检测进行中」，语义保持不变。
func (s *HealthCheckService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modelCheckRunning || s.groupCheckRunning
}

// stopModelCheckBackground 停掉模型级后台检测。
//
// UpdateConfig 里改完配置必须走这一趟：startModelCheckBackground 见到
// modelCheckRunning 为 true 会直接 return，不停就永远用不上新配置。
func (s *HealthCheckService) stopModelCheckBackground() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.modelCheckRunning && s.modelCheckCancel != nil {
		s.modelCheckCancel()
		s.modelCheckRunning = false
	}
}

// stopGroupCheckBackground 停掉组级后台检测，语义同 stopModelCheckBackground。
func (s *HealthCheckService) stopGroupCheckBackground() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groupCheckRunning && s.groupCheckCancel != nil {
		s.groupCheckCancel()
		s.groupCheckRunning = false
	}
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

		// 首次延迟 30 秒（与组检测的 45 秒错开）
		select {
		case <-modelCtx.Done():
			return
		case <-time.After(modelCheckStartDelay):
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

// modelDetectableUnderProvider 判断某模型是否应在本轮模型级检测中被探测。
//
// usable 是「渠道 ID -> 渠道当前可用」的映射，由调用方每轮取一次填充。
// 已知且不可用的渠道下的模型跳过；映射里没有该渠道（查询失败或渠道已删）
// 时按可用处理——宁可多探一次，也不要因为一次查询失败把所有模型当死。
func modelDetectableUnderProvider(providerID uuid.UUID, usable map[uuid.UUID]bool) bool {
	u, known := usable[providerID]
	return !known || u
}

// runBackgroundModelCheck 后台执行一轮模型检测。
//
// ctx 为轮次上下文（定时器上下文）：仅用于判断「自动禁用/恢复触发的快照重建」是否还有必要，
// 单次重建本身由 ProviderSnapshotRefresher 用独立的超时上下文执行。
func (s *HealthCheckService) runBackgroundModelCheck(ctx context.Context) {
	config, _ := s.healthRepo.GetConfig()
	maxFailures := model.DefaultModelCheckFailureThreshold
	if config != nil && config.ModelCheckFailureThreshold > 0 {
		maxFailures = config.ModelCheckFailureThreshold
	}

	recoveryEnabled := false
	recoveryThreshold := model.DefaultModelRecoverySuccessThreshold
	if config != nil && config.ModelRecoveryEnabled {
		recoveryEnabled = true
		if config.ModelRecoverySuccessThreshold > 0 {
			recoveryThreshold = config.ModelRecoverySuccessThreshold
		}
	}

	// 渠道可用性：渠道被自动禁用期间，其下每个模型的请求都必然失败。
	// 原实现对此一无所知，仍逐个模型探测，制造两样东西：
	//   1. 大量必然失败的日志与无效上游请求；
	//   2. 「模型恢复成功、但渠道仍不可用」的矛盾状态 —— 模型从禁用变回启用，
	//      而它所在的渠道还在 status=3，请求照旧 503。
	// 渠道能否恢复由渠道级检测负责，模型级在这一层不重复判断。
	providerUsable := make(map[uuid.UUID]bool)
	if allProviders, _, err := s.providerRepo.List(repository.ProviderListFilter{}, 0, 1000); err == nil {
		for i := range allProviders {
			p := &allProviders[i]
			providerUsable[p.ID] = p.Enabled && p.Status != model.ProviderStatusAutoDisabled
		}
	} else {
		// 查询失败时不做过滤（宁可多探，也不要把全部模型当成不可用）
		slog.Warn("model check background: failed to list providers, skip provider filter", "error", err)
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
		// 所属渠道不可用时跳过（渠道级检测会负责它能否恢复）
		if !modelDetectableUnderProvider(m.ProviderID, providerUsable) {
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

// RunNow 手动触发一次检测（异步，兼容旧调用）
func (s *HealthCheckService) RunNow() {
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
		case <-time.After(groupCheckStartDelay):
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
