package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimetypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go/auth/bearer"
	"github.com/example/llm-gateway/internal/controlplane/model"
)

// IsNonOpenAICompatible 返回该 definition_id 是否为非 OpenAI 兼容协议（公共函数）
func IsNonOpenAICompatible(definitionID string) bool {
	return isNonOpenAICompatible(definitionID)
}

// ExtractAPIKey 从凭证 JSON 中提取 api_key（公共函数）
func ExtractAPIKey(credentialJSON string) string {
	return extractAPIKey(credentialJSON)
}

// DoBedrockModelCheckPublic 用 AWS SDK Converse(max_tokens=1) 验证具体模型是否可调用（公共函数）。
// 与 HealthCheckService.doBedrockModelCheck 逻辑完全一致。
func DoBedrockModelCheckPublic(provider *model.Provider, credJSON string, m *model.Model) (bool, int, string) {
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
				AccessKeyID    string `json:"access_key_id"`
				SecretAccessKey string `json:"secret_access_key"`
				SessionToken   string `json:"session_token,omitempty"`
			}
			if err := json.Unmarshal([]byte(apiKey), &creds); err == nil && creds.AccessKeyID != "" {
				loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
					credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
				))
			}
		} else {
			// Bedrock API Key (Bearer token) 模式：
			// 需要提供匿名凭证避免 SDK 走默认凭证链（IMDS 超时）
			loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider("anonymous", "anonymous", ""),
			))
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

	modelName := m.UpstreamModelID
	if modelName == "" {
		modelName = m.Name
	}

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
