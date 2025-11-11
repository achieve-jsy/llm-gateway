package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	cpservice "github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SyncServiceInterface 同步服务抽象
// 实现者：internal/controlplane/service.SyncService
type SyncServiceInterface interface {
	SyncModels(providerID uuid.UUID) (*service.SyncResult, error)
	ConfirmSync(providerID uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error)
}

// SyncHandler 模型同步 Handler
type SyncHandler struct {
	svc          SyncServiceInterface
	providerRepo *repository.ProviderRepo
}

// NewSyncHandler 创建 SyncHandler
func NewSyncHandler(svc SyncServiceInterface, providerRepo *repository.ProviderRepo) *SyncHandler {
	return &SyncHandler{svc: svc, providerRepo: providerRepo}
}

// SyncModels POST /api/v1/providers/:id/sync-models
func (h *SyncHandler) SyncModels(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": CodeBadRequest},
		})
		return
	}

	result, err := h.svc.SyncModels(id)
	if err != nil {
		msg := err.Error()
		errType := "internal_error"
		code := CodeInternalError
		status := http.StatusInternalServerError

		switch {
		case msg == "provider not found":
			status = http.StatusNotFound
			errType = "not_found"
			code = CodeProviderNotFound
		case msg == "provider is disabled":
			status = http.StatusBadRequest
			errType = "invalid_request"
			code = CodeProviderDisabled
		case len(msg) > 9 && msg[:9] == "upstream ":
			status = http.StatusBadGateway
			errType = CodeUpstreamError
			code = CodeUpstreamError
		}

		c.JSON(status, gin.H{
			"error": gin.H{"type": errType, "message": msg, "code": code},
		})
		return
	}

	if result.Unsupported {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "upstream adapter does not support model sync",
				"code":    CodeSyncNotSupported,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// ConfirmSync POST /api/v1/providers/:id/sync-models/confirm
func (h *SyncHandler) ConfirmSync(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": CodeBadRequest},
		})
		return
	}

	var input service.ConfirmSyncInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	result, err := h.svc.ConfirmSync(id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// checkModelsInput POST /api/v1/providers/:id/check-models 请求体
type checkModelsInput struct {
	ModelNames []string `json:"model_names" binding:"required"`
}

// ModelAvailabilityItem 单个模型可用性结果
type ModelAvailabilityItem struct {
	Name         string `json:"name"`
	Available    bool   `json:"available"`
	Skipped      bool   `json:"skipped"`
	ResponseTime int    `json:"response_time"`
	Error        string `json:"error,omitempty"`
}

// CheckModels POST /api/v1/providers/:id/check-models
// 对指定渠道的一批模型名执行可用性探测（并发，最多 5 个 goroutine）
func (h *SyncHandler) CheckModels(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": CodeBadRequest},
		})
		return
	}

	var input checkModelsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	provider, err := h.providerRepo.GetByID(id)
	if err != nil || provider == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "provider not found", "code": CodeProviderNotFound},
		})
		return
	}

	// 解密凭证
	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to decrypt credentials", "code": CodeInternalError},
		})
		return
	}
	var creds map[string]string
	json.Unmarshal([]byte(credJSON), &creds)
	apiKey := creds["api_key"]
	if apiKey == "" {
		apiKey = credJSON
	}

	results := make([]ModelAvailabilityItem, len(input.ModelNames))
	var mu sync.Mutex
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup

	for i, name := range input.ModelNames {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, modelName string) {
			defer wg.Done()
			defer func() { <-sem }()

			success, skipped, responseTime, errMsg := cpservice.ProbeModelForward(
				context.Background(), provider.Endpoint, modelName, apiKey, "chat",
			)

			item := ModelAvailabilityItem{
				Name:         modelName,
				Available:    success || skipped,
				Skipped:      skipped,
				ResponseTime: responseTime,
				Error:        errMsg,
			}
			mu.Lock()
			results[idx] = item
			mu.Unlock()
		}(i, name)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"models": results}})
}
