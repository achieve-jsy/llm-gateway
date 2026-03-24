package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	cpservice "github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ModelCheckResult 单个模型检测结果
type ModelCheckResult struct {
	ModelID       uuid.UUID `json:"model_id"`
	ModelName     string    `json:"model_name"`
	Success       bool      `json:"success"`
	ResponseTime  int       `json:"response_time"`
	Error         string    `json:"error,omitempty"`
	AutoDisabled  bool      `json:"auto_disabled"`
	AutoReenabled bool      `json:"auto_reenabled"`
}

// ModelCheckAllResult 批量模型检测汇总结果
type ModelCheckAllResult struct {
	Total          int                `json:"total"`
	Success        int                `json:"success"`
	Failed         int                `json:"failed"`
	FailureSummary map[string]int     `json:"failure_summary,omitempty"`
	Results        []ModelCheckResult `json:"results"`
}

// ModelCheckHandler 模型健康检测 Handler
type ModelCheckHandler struct {
	modelRepo    *repository.ModelRepo
	providerRepo *repository.ProviderRepo
	healthRepo   *repository.HealthCheckRepo
}

// NewModelCheckHandler 创建 ModelCheckHandler 实例
func NewModelCheckHandler(modelRepo *repository.ModelRepo, providerRepo *repository.ProviderRepo, healthRepo *repository.HealthCheckRepo) *ModelCheckHandler {
	return &ModelCheckHandler{
		modelRepo:    modelRepo,
		providerRepo: providerRepo,
		healthRepo:   healthRepo,
	}
}

// CheckOne POST /api/v1/models/:id/check — 检测单个模型
func (h *ModelCheckHandler) CheckOne(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid model id"}})
		return
	}

	m, err := h.modelRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "model not found"}})
		return
	}

	result := h.checkModel(c.Request.Context(), m)
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// CheckAll POST /api/v1/models/check-all — 检测所有模型（含 disabled）
func (h *ModelCheckHandler) CheckAll(c *gin.Context) {
	filter := repository.ModelListFilter{}
	models, _, err := h.modelRepo.List(filter, 0, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to list models"}})
		return
	}

	result := &ModelCheckAllResult{
		FailureSummary: make(map[string]int),
		Results:        make([]ModelCheckResult, 0),
	}

	// 并发检测，最多 10 个 goroutine
	var mu sync.Mutex
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for i := range models {
		m := &models[i]

		result.Total++
		wg.Add(1)
		sem <- struct{}{}
		go func(mod *model.Model) {
			defer wg.Done()
			defer func() { <-sem }()

			cr := h.checkModel(c.Request.Context(), mod)
			mu.Lock()
			result.Results = append(result.Results, *cr)
			if cr.Success {
				result.Success++
			} else {
				result.Failed++
				result.FailureSummary[classifyModelCheckError(cr.Error)]++
			}
			mu.Unlock()
		}(m)
	}

	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func classifyModelCheckError(errMsg string) string {
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(lower, "401") || strings.Contains(lower, "invalid api key") || strings.Contains(lower, "invalid key") || strings.Contains(lower, "authentication fails"):
		return "invalid_api_key"
	case strings.Contains(lower, "403") || strings.Contains(lower, "access denied"):
		return "access_denied"
	case strings.Contains(lower, "404") || strings.Contains(lower, "model_not_found") || strings.Contains(lower, "does not exist"):
		return "model_not_found"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "limit_requests"):
		return "rate_limited"
	case strings.Contains(lower, "product is not activated") || strings.Contains(lower, "not activated"):
		return "product_not_activated"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "超时"):
		return "timeout"
	default:
		return "other"
	}
}

// checkModel 执行单个模型的健康检测
func (h *ModelCheckHandler) checkModel(ctx context.Context, m *model.Model) *ModelCheckResult {
	cr := &ModelCheckResult{
		ModelID:   m.ID,
		ModelName: m.Name,
	}

	provider := &m.Provider
	if provider.ID == uuid.Nil {
		// 需要加载 provider
		p, err := h.providerRepo.GetByID(m.ProviderID)
		if err != nil {
			cr.Error = "渠道不存在"
			h.handleModelFailure(m.ID, cr)
			return cr
		}
		provider = p
	}

	// 解密凭证
	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		cr.Error = "解密凭证失败"
		h.handleModelFailure(m.ID, cr)
		return cr
	}

	// Bedrock 等非 OpenAI 兼容的 provider 走健康检测服务的专用路径
	if cpservice.IsNonOpenAICompatible(provider.DefinitionID) {
		success, responseTime, errMsg := cpservice.DoBedrockModelCheckPublic(provider, credJSON, m)
		cr.ResponseTime = responseTime
		if success {
			cr.Success = true
			h.modelRepo.ResetModelFailures(m.ID)
			h.modelRepo.UpdateModelCheckResult(m.ID, responseTime, "")
			if shouldAutoReenableModel(m, h.getModelFailureThreshold()) {
				enabled := true
				h.modelRepo.UpdateEnabled(m.ID, &enabled)
				cr.AutoReenabled = true
			}
		} else {
			cr.Error = errMsg
			h.modelRepo.UpdateModelCheckResult(m.ID, responseTime, errMsg)
			h.handleModelFailure(m.ID, cr)
		}
		return cr
	}

	apiKey := cpservice.ExtractAPIKey(credJSON)
	modelName := m.UpstreamName()

	// 发送检测请求（按模型类型选择探针接口）
	success, skipped, responseTime, errMsg := cpservice.ProbeModelForward(ctx, provider.Endpoint, modelName, apiKey, m.ModelType)
	cr.ResponseTime = responseTime

	if success {
		// 检测成功：重置失败计数 + 更新响应时间 + 如果之前被自动禁用则恢复启用
		cr.Success = true
		h.modelRepo.ResetModelFailures(m.ID)
		h.modelRepo.UpdateModelCheckResult(m.ID, responseTime, "")
		if shouldAutoReenableModel(m, h.getModelFailureThreshold()) {
			enabled := true
			h.modelRepo.UpdateEnabled(m.ID, &enabled)
			cr.AutoReenabled = true
		}
	} else if skipped {
		// 不适合标准 chat 探针的模型不计失败，也不保留错误状态。
		cr.Success = true
		h.modelRepo.ResetModelFailures(m.ID)
		h.modelRepo.UpdateModelCheckResult(m.ID, 0, "")
	} else {
		// 检测失败：递增失败计数 + 保存错误
		cr.Error = errMsg
		h.modelRepo.UpdateModelCheckResult(m.ID, responseTime, errMsg)
		h.handleModelFailure(m.ID, cr)
	}

	return cr
}

func (h *ModelCheckHandler) getModelFailureThreshold() int {
	maxFailures := 3
	config, err := h.healthRepo.GetConfig()
	if err == nil && config != nil {
		if config.ModelCheckFailureThreshold > 0 {
			maxFailures = config.ModelCheckFailureThreshold
		} else if config.FailureThreshold > 0 {
			maxFailures = config.FailureThreshold
		}
	}
	return maxFailures
}

func shouldAutoReenableModel(m *model.Model, threshold int) bool {
	if m == nil || m.Enabled == nil || *m.Enabled {
		return false
	}
	return m.ConsecutiveFailures >= threshold
}

// handleModelFailure 处理模型检测失败（递增计数，可能自动禁用）
func (h *ModelCheckHandler) handleModelFailure(id uuid.UUID, cr *ModelCheckResult) {
	maxFailures := h.getModelFailureThreshold()

	newCount, err := h.modelRepo.IncrementModelFailures(id)
	if err != nil {
		slog.Error("递增模型失败计数出错", "model_id", id, "error", err)
		return
	}

	if newCount >= maxFailures {
		slog.Warn("模型连续失败达阈值，自动禁用",
			"model_id", id,
			"failures", newCount,
			"threshold", maxFailures,
		)
		if err := h.modelRepo.AutoDisableModel(id); err != nil {
			slog.Error("自动禁用模型失败", "model_id", id, "error", err)
		} else {
			cr.AutoDisabled = true
		}
	}
}
