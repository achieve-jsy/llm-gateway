package handler

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
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ModelCheckResult 单个模型检测结果
type ModelCheckResult struct {
	ModelID      uuid.UUID `json:"model_id"`
	ModelName    string    `json:"model_name"`
	Success      bool      `json:"success"`
	ResponseTime int       `json:"response_time"`
	Error        string    `json:"error,omitempty"`
	AutoDisabled bool      `json:"auto_disabled"`
}

// ModelCheckAllResult 批量模型检测汇总结果
type ModelCheckAllResult struct {
	Total   int                `json:"total"`
	Success int                `json:"success"`
	Failed  int                `json:"failed"`
	Results []ModelCheckResult `json:"results"`
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

// CheckAll POST /api/v1/models/check-all — 检测所有启用模型
func (h *ModelCheckHandler) CheckAll(c *gin.Context) {
	filter := repository.ModelListFilter{}
	models, _, err := h.modelRepo.List(filter, 0, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to list models"}})
		return
	}

	result := &ModelCheckAllResult{
		Results: make([]ModelCheckResult, 0),
	}

	// 并发检测，最多 10 个 goroutine
	var mu sync.Mutex
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for i := range models {
		m := &models[i]
		if m.Enabled == nil || !*m.Enabled {
			continue
		}
		// 跳过 embedding 类型
		if strings.Contains(strings.ToLower(m.ModelType), "embed") {
			continue
		}

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
			}
			mu.Unlock()
		}(m)
	}

	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"data": result})
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

	var creds map[string]string
	json.Unmarshal([]byte(credJSON), &creds)
	apiKey := creds["api_key"]
	if apiKey == "" {
		apiKey = credJSON
	}

	// 确定上游模型名（优先使用 upstream_model_id）
	modelName := m.UpstreamModelID
	if modelName == "" {
		modelName = m.Name
	}

	// 发送检测请求
	success, responseTime, errMsg := doModelCheck(ctx, provider.Endpoint, modelName, apiKey)
	cr.ResponseTime = responseTime

	if success {
		// 检测成功：重置失败计数 + 更新响应时间
		cr.Success = true
		h.modelRepo.ResetModelFailures(m.ID)
		h.modelRepo.UpdateModelCheckResult(m.ID, responseTime, "")
	} else {
		// 检测失败：递增失败计数 + 保存错误
		cr.Error = errMsg
		h.modelRepo.UpdateModelCheckResult(m.ID, responseTime, errMsg)
		h.handleModelFailure(m.ID, cr)
	}

	return cr
}

// handleModelFailure 处理模型检测失败（递增计数，可能自动禁用）
func (h *ModelCheckHandler) handleModelFailure(id uuid.UUID, cr *ModelCheckResult) {
	maxFailures := 3
	config, err := h.healthRepo.GetConfig()
	if err == nil && config != nil {
		// 优先使用模型检测独立阈值
		if config.ModelCheckFailureThreshold > 0 {
			maxFailures = config.ModelCheckFailureThreshold
		} else if config.FailureThreshold > 0 {
			maxFailures = config.FailureThreshold
		}
	}

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

// doModelCheck 向上游发送 chat/completions 请求检测模型可用性
func doModelCheck(ctx context.Context, endpoint string, modelName string, apiKey string) (success bool, responseTime int, errMsg string) {
	url := strings.TrimRight(endpoint, "/") + "/chat/completions"
	chatReq := map[string]interface{}{
		"model":      modelName,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 1,
	}
	body, _ := json.Marshal(chatReq)

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())

	if err != nil {
		return false, elapsed, fmt.Sprintf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return false, elapsed, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateModelCheckStr(string(respBody), 200))
	}

	// 验证 choices 非空
	respBody, _ := io.ReadAll(resp.Body)
	var chatResp struct {
		Choices []interface{} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return false, elapsed, fmt.Sprintf("响应体解析失败: %v", err)
	}
	if len(chatResp.Choices) == 0 {
		return false, elapsed, "响应 choices 为空"
	}

	return true, elapsed, ""
}

// truncateModelCheckStr 截断字符串
func truncateModelCheckStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
