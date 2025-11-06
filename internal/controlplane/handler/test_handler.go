package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TestProviderRepo TestHandler 依赖的 ProviderRepo 子集
type TestProviderRepo interface {
	GetByID(id uuid.UUID) (*model.Provider, error)
	UpdateResponseTime(id uuid.UUID, ms int) error
}

// TestModelRepo TestHandler 依赖的 ModelRepo 子集（当前未使用，预留）
type TestModelRepo interface {
	ListAvailable() ([]model.Model, error)
}

// TestHandler 渠道测试 Handler
type TestHandler struct {
	providerRepo TestProviderRepo
	modelRepo    TestModelRepo
}

// NewTestHandler 创建 TestHandler
func NewTestHandler(providerRepo TestProviderRepo, modelRepo TestModelRepo) *TestHandler {
	return &TestHandler{providerRepo: providerRepo, modelRepo: modelRepo}
}

// testInput 测试请求体
type testInput struct {
	TestChat bool `json:"test_chat"`
}

// testResult 测试结果
type testResult struct {
	Success      bool   `json:"success"`
	ResponseTime int    `json:"response_time"`
	StatusCode   int    `json:"status_code,omitempty"`
	ModelCount   int    `json:"model_count,omitempty"`
	Error        string `json:"error,omitempty"`
	// 转发测试结果
	ChatSuccess      *bool  `json:"chat_success,omitempty"`
	ChatResponseTime int    `json:"chat_response_time,omitempty"`
	ChatModel        string `json:"chat_model,omitempty"`
	ChatError        string `json:"chat_error,omitempty"`
}

// TestProvider POST /api/v1/providers/:id/test
// 1. 调用厂商 GET /models 验证连通性（不消耗 token）
// 2. 可选：发送最小化 chat 请求验证转发功能（消耗极少 token）
func (h *TestHandler) TestProvider(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": "bad_request"},
		})
		return
	}

	// 解析请求体（可选）
	var input testInput
	c.ShouldBindJSON(&input) // 忽略错误，body 可为空

	// 获取渠道信息
	provider, err := h.providerRepo.GetByID(id)
	if err != nil {
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

	// 解析凭证获取 api_key
	var creds map[string]string
	json.Unmarshal([]byte(credJSON), &creds)
	apiKey := creds["api_key"]
	if apiKey == "" {
		apiKey = credJSON
	}

	client := &http.Client{Timeout: 15 * time.Second}
	result := testResult{}

	// ========== 第一步：连通性测试（GET /models） ==========
	endpoint := strings.TrimRight(provider.Endpoint, "/") + "/models"

	start := time.Now()
	httpReq, _ := http.NewRequest("GET", endpoint, nil)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(httpReq)
	elapsed := time.Since(start).Milliseconds()
	result.ResponseTime = int(elapsed)

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("连接失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"data": result})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	result.StatusCode = resp.StatusCode

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Success = false
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		c.JSON(http.StatusOK, gin.H{"data": result})
		return
	}

	// 连通性测试成功
	result.Success = true
	h.providerRepo.UpdateResponseTime(id, int(elapsed))

	// 解析模型列表
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(body, &modelsResp)
	result.ModelCount = len(modelsResp.Data)

	// ========== 第二步：转发测试（POST /chat/completions） ==========
	if input.TestChat {
		// 确定测试模型：优先用配置的 test_model，否则取模型列表中不是 embedding 的模型
		testModel := provider.TestModel
		if testModel == "" && len(modelsResp.Data) > 0 {
			// 尝试找到第一个不是 embedding 的模型
			for _, model := range modelsResp.Data {
				if !strings.Contains(strings.ToLower(model.ID), "embed") {
					testModel = model.ID
					break
				}
			}
			// 如果都是 embedding 或没有找到，使用第一个
			if testModel == "" {
				testModel = modelsResp.Data[0].ID
			}
		}

		if testModel != "" {
			chatResult := h.testChatWithModel(client, provider, apiKey, testModel)
			result.ChatSuccess = &chatResult.success
			result.ChatResponseTime = chatResult.responseTime
			result.ChatModel = chatResult.model
			result.ChatError = chatResult.err
		} else {
			f := false
			result.ChatSuccess = &f
			result.ChatError = "无可用模型：未配置 test_model 且 /models 返回为空"
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// chatTestResult 转发测试内部结果
type chatTestResult struct {
	success      bool
	responseTime int
	model        string
	err          string
}

// testChatWithModel 发送最小化 chat 请求验证转发
func (h *TestHandler) testChatWithModel(client *http.Client, provider *model.Provider, apiKey string, testModel string) chatTestResult {
	endpoint := strings.TrimRight(provider.Endpoint, "/") + "/chat/completions"

	// 构造最小化请求：1 token prompt，max_tokens=1
	chatReq := map[string]interface{}{
		"model": testModel,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 1,
	}

	reqBody, _ := json.Marshal(chatReq)

	start := time.Now()
	httpReq, _ := http.NewRequest("POST", endpoint, bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(httpReq)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		return chatTestResult{
			success:      false,
			responseTime: int(elapsed),
			model:        testModel,
			err:          fmt.Sprintf("请求失败: %v", err),
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 尝试提取错误信息
		errMsg := extractErrorMessage(body)
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return chatTestResult{
			success:      false,
			responseTime: int(elapsed),
			model:        testModel,
			err:          errMsg,
		}
	}

	// 验证响应格式
	var chatResp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &chatResp); err != nil {
		return chatTestResult{
			success:      false,
			responseTime: int(elapsed),
			model:        testModel,
			err:          "响应格式异常，无法解析为 ChatCompletion",
		}
	}

	if chatResp.ID == "" {
		return chatTestResult{
			success:      false,
			responseTime: int(elapsed),
			model:        testModel,
			err:          "响应缺少 id 字段，可能不是有效的 ChatCompletion 响应",
		}
	}

	return chatTestResult{
		success:      true,
		responseTime: int(elapsed),
		model:        chatResp.Model,
	}
}

// extractErrorMessage 从错误响应体中提取可读错误信息
func extractErrorMessage(body []byte) string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return errResp.Error.Message
	}

	// 尝试直接取 message 字段
	var simple struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &simple) == nil && simple.Message != "" {
		return simple.Message
	}

	return truncate(string(body), 150)
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
