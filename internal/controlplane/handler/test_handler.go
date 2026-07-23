package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	cpservice "github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// 非 OpenAI 兼容请使用 cpservice.IsNonOpenAICompatible，避免与 service 双 map 漂移。

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
	// Message 成功时的语义说明（如景汐：仅连通+鉴权，非视频创建）
	Message string `json:"message,omitempty"`
	// 转发测试结果
	ChatSuccess      *bool  `json:"chat_success,omitempty"`
	ChatResponseTime int    `json:"chat_response_time,omitempty"`
	ChatModel        string `json:"chat_model,omitempty"`
	ChatError        string `json:"chat_error,omitempty"`
}

// TestProvider POST /api/v1/providers/:id/test
// 对 OpenAI 兼容 provider：GET /models + 可选 POST /chat/completions
// 对非 OpenAI 兼容 provider（如 Bedrock）：走专属检测路径
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

	// 非 OpenAI 兼容 provider 走专属检测路径（与定时健康检查同源判定）
	if cpservice.IsNonOpenAICompatible(provider.DefinitionID) {
		result := h.testNonOpenAIProvider(provider, credJSON, input.TestChat)
		c.JSON(http.StatusOK, gin.H{"data": result})
		return
	}

	// ===== OpenAI 兼容 provider =====
	apiKey := extractAPIKey(credJSON)
	result := h.testOpenAICompatible(provider, apiKey, input.TestChat)
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// testOpenAICompatible 对 OpenAI 兼容 provider 做通用检测：
// 第一步 GET {endpoint}/models（连通性+鉴权），第二步可选 POST {endpoint}/chat/completions（转发）。
func (h *TestHandler) testOpenAICompatible(provider *model.Provider, apiKey string, testChat bool) testResult {
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
		return result
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	result.StatusCode = resp.StatusCode

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Success = false
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		return result
	}

	// 连通性测试成功
	result.Success = true
	h.providerRepo.UpdateResponseTime(provider.ID, int(elapsed))

	// 解析模型列表
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(body, &modelsResp)
	result.ModelCount = len(modelsResp.Data)

	// ========== 第二步：转发测试（POST /chat/completions） ==========
	if testChat {
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

	return result
}

// testNonOpenAIProvider 对非 OpenAI 兼容的 provider 做专属检测。
// 参考 GoModel AvailabilityChecker 设计：各 provider 用自己协议的最轻量级 API 验证连通性和凭证。
func (h *TestHandler) testNonOpenAIProvider(provider *model.Provider, credJSON string, testChat bool) testResult {
	switch provider.DefinitionID {
	case "bedrock":
		return h.testBedrock(provider, credJSON, testChat)
	case "seedance-relay":
		return h.testSeedanceRelay(provider, credJSON, testChat)
	case "seedance-volcengine":
		return h.testSeedanceVolcengine(provider, credJSON, testChat)
	case "azure":
		return h.testAzure(provider, credJSON, testChat)
	case "vertex":
		return h.testVertex(provider, credJSON, testChat)
	default:
		return testResult{
			Success: true,
			Message: fmt.Sprintf("provider type '%s' 暂不支持自动检测（已跳过）", provider.DefinitionID),
		}
	}
}

// testAzure 与定时健康检查共用 Azure v1 探针。
func (h *TestHandler) testAzure(provider *model.Provider, credJSON string, testChat bool) testResult {
	apiKey := extractAPIKey(credJSON)
	res := cpservice.ProbeAzureConnectivity(context.Background(), provider.Endpoint, apiKey)
	out := testResult{
		Success:      res.Success,
		ResponseTime: res.ResponseTime,
		StatusCode:   res.StatusCode,
		Message:      res.Message,
	}
	if !res.Success {
		out.Error = res.Message
		out.Message = ""
		return out
	}
	_ = h.providerRepo.UpdateResponseTime(provider.ID, res.ResponseTime)

	if testChat {
		testModel := provider.TestModel
		if testModel == "" {
			f := false
			out.ChatSuccess = &f
			out.ChatError = "未配置 test_model，跳过转发测试"
			return out
		}
		chatRes := cpservice.ProbeAzureChat(context.Background(), provider.Endpoint, apiKey, testModel)
		out.ChatModel = testModel
		out.ChatResponseTime = chatRes.ResponseTime
		ok := chatRes.Success
		out.ChatSuccess = &ok
		if !chatRes.Success {
			out.ChatError = chatRes.Message
		}
	}
	return out
}

// testVertex Google Vertex (native) 专属检测（与健康检查共用 cpservice.ProbeVertexGenerate）。
// 原生面 endpoint 以 /publishers/google 结尾，走 {endpoint}/models/{model}:generateContent + x-goog-api-key；
// 非原生面（如 OpenAI 兼容层）回退到 testOpenAICompatible 通用检测。
func (h *TestHandler) testVertex(provider *model.Provider, credJSON string, testChat bool) testResult {
	apiKey := extractAPIKey(credJSON)
	endpoint := strings.TrimRight(provider.Endpoint, "/")

	// 非原生面：回退到 OpenAI 兼容检测
	if !strings.HasSuffix(endpoint, "/publishers/google") {
		return h.testOpenAICompatible(provider, apiKey, testChat)
	}

	testModel := strings.TrimSpace(provider.TestModel)
	if testModel == "" {
		testModel = "gemini-1.5-flash"
	}

	res := cpservice.ProbeVertexGenerate(context.Background(), provider.Endpoint, apiKey, testModel)
	out := testResult{
		Success:      res.Success,
		ResponseTime: res.ResponseTime,
		StatusCode:   res.StatusCode,
		Message:      res.Message,
	}
	if !res.Success {
		out.Error = res.Message
		out.Message = ""
		return out
	}
	_ = h.providerRepo.UpdateResponseTime(provider.ID, res.ResponseTime)

	if testChat {
		ok := res.Success
		out.ChatSuccess = &ok
		out.ChatResponseTime = res.ResponseTime
		out.ChatModel = testModel
	}
	return out
}

// testSeedanceRelay 与定时健康检查共用 ProbeSeedanceRelayConnectivity。
// 仅验证连通+鉴权，不创建视频；test_chat 对景汐无意义，返回 skipped 说明。
func (h *TestHandler) testSeedanceRelay(provider *model.Provider, credJSON string, testChat bool) testResult {
	apiKey := extractAPIKey(credJSON)
	res := cpservice.ProbeSeedanceRelayConnectivity(context.Background(), provider.Endpoint, apiKey)
	out := testResult{
		Success:      res.Success,
		ResponseTime: res.ResponseTime,
		StatusCode:   res.StatusCode,
		Message:      res.Message,
	}
	if !res.Success {
		out.Error = res.Message
		out.Message = ""
		return out
	}
	_ = h.providerRepo.UpdateResponseTime(provider.ID, res.ResponseTime)
	if testChat {
		// 不设 chat_success=true，避免前端误读为「转发测试通过」
		out.ChatError = "skipped: seedance-relay probe is connectivity+auth only (no video create)"
	}
	return out
}

// testSeedanceVolcengine 与定时健康检查共用 ProbeSeedanceVolcengineConnectivity。
func (h *TestHandler) testSeedanceVolcengine(provider *model.Provider, credJSON string, testChat bool) testResult {
	cred := cpservice.ParseSeedanceVolcCreds(credJSON)
	if !cred.HasAuth() {
		return testResult{Success: false, Error: "seedance-volcengine: missing credentials for selected auth_type"}
	}
	res := cpservice.ProbeSeedanceVolcengineConnectivityCreds(context.Background(), provider.Endpoint, cred)
	out := testResult{
		Success:      res.Success,
		ResponseTime: res.ResponseTime,
		StatusCode:   res.StatusCode,
		Message:      res.Message,
	}
	if !res.Success {
		out.Error = res.Message
		out.Message = ""
		return out
	}
	_ = h.providerRepo.UpdateResponseTime(provider.ID, res.ResponseTime)
	if testChat {
		out.ChatError = "skipped: seedance-volcengine probe is connectivity+auth only (no video create)"
	}
	return out
}

// testBedrock 使用 AWS SDK 验证 Bedrock 连通性和可用性。
// 参考 GoModel bedrock.CheckAvailability 的实现：
//
// 连通性测试：调用 ListFoundationModels（控制面 API，不消耗推理额度）
//   - 验证网络可达性 + AWS 凭证有效性
//
// 转发测试（testChat=true）：用 Converse API 发送最小化请求（1 token）
//   - 用 provider.TestModel 或配置的模型名作为测试模型
//   - 验证推理端能否正常调用
func (h *TestHandler) testBedrock(provider *model.Provider, credJSON string, testChat bool) testResult {
	start := time.Now()
	result := testResult{}

	// 从 endpoint 中提取 region
	region := regionFromBedrockEndpoint(provider.Endpoint)
	if region == "" {
		region = "us-east-1"
	}

	// 解析凭证
	apiKey := extractAPIKey(credJSON)

	// 构建 AWS 加载选项
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	var bedrockCtrlOpts []func(*bedrock.Options)
	var bedrockRuntimeOpts []func(*bedrockruntime.Options)

	if apiKey != "" {
		if strings.HasPrefix(apiKey, "{") {
			// IAM Access Key JSON 格式
			var creds struct {
				AccessKeyID     string `json:"access_key_id"`
				SecretAccessKey string `json:"secret_access_key"`
				SessionToken    string `json:"session_token,omitempty"`
			}
			if err := json.Unmarshal([]byte(apiKey), &creds); err != nil {
				return testResult{Success: false, Error: "凭证 JSON 格式错误: " + err.Error()}
			}
			if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
				return testResult{Success: false, Error: "凭证缺少 access_key_id 或 secret_access_key"}
			}
			loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
			))
		} else {
			// Bedrock API Key（Bearer token）：使用 AnonymousCredentials 让 SDK 不做 SigV4 签名
			loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}))
			tokenVal := apiKey
			bedrockCtrlOpts = append(bedrockCtrlOpts, func(o *bedrock.Options) {
				o.BearerAuthTokenProvider = bearer.TokenProviderFunc(func(_ context.Context) (bearer.Token, error) {
					return bearer.Token{Value: tokenVal}, nil
				})
				o.AuthSchemePreference = []string{"smithy.api#httpBearerAuth"}
			})
			bedrockRuntimeOpts = append(bedrockRuntimeOpts, func(o *bedrockruntime.Options) {
				o.BearerAuthTokenProvider = bearer.TokenProviderFunc(func(_ context.Context) (bearer.Token, error) {
					return bearer.Token{Value: tokenVal}, nil
				})
				o.AuthSchemePreference = []string{"smithy.api#httpBearerAuth"}
			})
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cpservice.BedrockProbeTimeout)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return testResult{Success: false, Error: "AWS 配置加载失败: " + err.Error()}
	}

	// ===== 连通性测试：ListFoundationModels =====
	bedrockCtrl := bedrock.NewFromConfig(awsCfg, bedrockCtrlOpts...)
	out, err := bedrockCtrl.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{
		ByInferenceType: bedrocktypes.InferenceTypeOnDemand,
	})

	elapsed := int(time.Since(start).Milliseconds())
	result.ResponseTime = elapsed

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.StatusCode = 200
	result.ModelCount = len(out.ModelSummaries)
	h.providerRepo.UpdateResponseTime(mustParseUUID(provider.ID), elapsed)

	// ===== 转发测试：Converse API =====
	if testChat {
		// 确定测试模型：优先用 provider.TestModel，否则用 claude-3-haiku（最轻量）
		testModel := provider.TestModel
		if testModel == "" {
			testModel = "anthropic.claude-3-haiku-20240307-v1:0"
		}

		chatStart := time.Now()
		runtime := bedrockruntime.NewFromConfig(awsCfg, bedrockRuntimeOpts...)

		userMsg := "hi"
		converseOut, err := runtime.Converse(ctx, &bedrockruntime.ConverseInput{
			ModelId: &testModel,
			Messages: []bedrockruntimetypes.Message{
				{
					Role: bedrockruntimetypes.ConversationRoleUser,
					Content: []bedrockruntimetypes.ContentBlock{
						&bedrockruntimetypes.ContentBlockMemberText{
							Value: bedrockruntimetypes.ContentBlockMemberText{
								Value: userMsg,
							}.Value,
						},
					},
				},
			},
			InferenceConfig: &bedrockruntimetypes.InferenceConfiguration{
				MaxTokens: int32Ptr(cpservice.BedrockProbeMaxTokens),
			},
		})

		chatElapsed := int(time.Since(chatStart).Milliseconds())

		if err != nil {
			f := false
			result.ChatSuccess = &f
			result.ChatResponseTime = chatElapsed
			result.ChatModel = testModel
			result.ChatError = err.Error()
		} else {
			t := true
			result.ChatSuccess = &t
			result.ChatResponseTime = chatElapsed
			result.ChatModel = testModel
			_ = converseOut
		}
	}

	return result
}

func int32Ptr(v int32) *int32 { return &v }

// regionFromBedrockEndpoint 从 Bedrock endpoint URL 中提取 region。
// 例：https://bedrock-runtime.us-east-1.amazonaws.com → us-east-1
func regionFromBedrockEndpoint(endpoint string) string {
	// 替换 bedrock-runtime 为 bedrock（控制面）后提取 region
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

func mustParseUUID(id interface{}) uuid.UUID {
	switch v := id.(type) {
	case uuid.UUID:
		return v
	case string:
		u, _ := uuid.Parse(v)
		return u
	}
	return uuid.Nil
}

// chatTestResult 转发测试内部结果
type chatTestResult struct {
	success      bool
	responseTime int
	model        string
	err          string
}

// testChatWithModel 发送最小化 chat 请求验证转发（OpenAI 兼容 provider 专用）
func (h *TestHandler) testChatWithModel(client *http.Client, provider *model.Provider, apiKey string, testModel string) chatTestResult {
	endpoint := strings.TrimRight(provider.Endpoint, "/") + "/chat/completions"

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
	elapsed := int(time.Since(start).Milliseconds())

	if err != nil {
		return chatTestResult{success: false, responseTime: elapsed, model: testModel,
			err: fmt.Sprintf("请求失败: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMsg := extractErrorMessage(body)
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return chatTestResult{success: false, responseTime: elapsed, model: testModel, err: errMsg}
	}

	var chatResp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return chatTestResult{success: false, responseTime: elapsed, model: testModel,
			err: "响应格式异常，无法解析为 ChatCompletion"}
	}
	if chatResp.ID == "" {
		return chatTestResult{success: false, responseTime: elapsed, model: testModel,
			err: "响应缺少 id 字段，可能不是有效的 ChatCompletion 响应"}
	}
	return chatTestResult{success: true, responseTime: elapsed, model: chatResp.Model}
}

// extractErrorMessage 从错误响应体中提取可读错误信息
func extractErrorMessage(body []byte) string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return errResp.Error.Message
	}
	var simple struct{ Message string `json:"message"` }
	if json.Unmarshal(body, &simple) == nil && simple.Message != "" {
		return simple.Message
	}
	return truncate(string(body), 150)
}

// extractAPIKey 从凭证 JSON 中提取 api_key 字段，若不是 JSON 则原样返回。
// auth_type=aksk 时保留完整 JSON，避免丢失 AK/SK。
func extractAPIKey(credentialJSON string) string {
	var m map[string]string
	if json.Unmarshal([]byte(credentialJSON), &m) == nil {
		switch strings.ToLower(strings.TrimSpace(m["auth_type"])) {
		case "aksk", "iam", "hmac":
			return credentialJSON
		}
		if key, ok := m["api_key"]; ok && key != "" {
			return key
		}
	}
	return credentialJSON
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

