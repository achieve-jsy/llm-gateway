package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	_ "github.com/lib/pq"
)

const (
	dbConnStr      = "postgres://llm_gateway:CHANGE_ME_PASSWORD@192.0.2.10:5433/llm_gateway_prod?sslmode=disable"
	encryptionKey  = "12345678901234567890123456789012"
)

type AWSCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Region          string `json:"region"`
}

func decryptCredential(encryptedText string, key string) (*AWSCredentials, error) {
	// 解析加密文本
	parts := strings.Split(encryptedText, ":")
	if len(parts) != 2 || parts[0] != "v1" {
		return nil, fmt.Errorf("invalid encrypted text format")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("base64 decode failed: %w", err)
	}

	// AES-GCM 解密
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("create cipher failed: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM failed: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed: %w", err)
	}

	// 解析 JSON
	var creds AWSCredentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("json unmarshal failed: %w", err)
	}

	return &creds, nil
}

func testModelID(ctx context.Context, client *bedrockruntime.Client, modelID string) (bool, string) {
	input := &bedrockruntime.InvokeModelInput{
		ModelId:     &modelID,
		ContentType: aws.String("application/json"),
		Body: []byte(`{
			"anthropic_version": "bedrock-2023-05-31",
			"max_tokens": 10,
			"messages": [
				{"role": "user", "content": "Hi"}
			]
		}`),
	}

	_, err := client.InvokeModel(ctx, input)
	if err != nil {
		return false, err.Error()
	}
	return true, "OK"
}

func listAvailableModels(ctx context.Context, client *bedrock.Client) ([]string, error) {
	var modelIDs []string
	paginator := bedrock.NewListFoundationModelsPaginator(client, &bedrock.ListFoundationModelsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, model := range page.ModelSummaries {
			if model.ModelId != nil && strings.Contains(*model.ModelId, "claude") {
				modelIDs = append(modelIDs, *model.ModelId)
			}
		}
	}
	return modelIDs, nil
}

func main() {
	// 连接数据库
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// 获取 AWS provider 凭证
	rows, err := db.Query(`
		SELECT id, name, encrypted_credentials
		FROM providers
		WHERE name ILIKE '%aws%'
	`)
	if err != nil {
		log.Fatalf("Failed to query providers: %v", err)
	}
	defer rows.Close()

	fmt.Println("=== Testing Claude Sonnet 4.6 Model IDs on AWS Bedrock ===\n")

	for rows.Next() {
		var id, name, encryptedCreds string
		if err := rows.Scan(&id, &name, &encryptedCreds); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}

		fmt.Printf("Testing provider: %s (ID: %s)\n", name, id)
		fmt.Println(strings.Repeat("-", 60))

		// 解密凭证
		creds, err := decryptCredential(encryptedCreds, encryptionKey)
		if err != nil {
			fmt.Printf("  ❌ Failed to decrypt credentials: %v\n\n", err)
			continue
		}

		fmt.Printf("  Region: %s\n", creds.Region)

		// 创建 AWS 客户端
		cfg, err := config.LoadDefaultConfig(context.Background(),
			config.WithRegion(creds.Region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				creds.AccessKeyID,
				creds.SecretAccessKey,
				"",
			)),
		)
		if err != nil {
			fmt.Printf("  ❌ Failed to load AWS config: %v\n\n", err)
			continue
		}

		runtimeClient := bedrockruntime.NewFromConfig(cfg)
		bedrockClient := bedrock.NewFromConfig(cfg)

		// 测试各种模型 ID 格式
		modelIDs := []string{
			"global.anthropic.claude-sonnet-4-6-20250514-v1:0",
			"us.anthropic.claude-sonnet-4-6-20250514-v1:0",
			"eu.anthropic.claude-sonnet-4-6-20250514-v1:0",
			"ap.anthropic.claude-sonnet-4-6-20250514-v1:0",
			"anthropic.claude-sonnet-4-6-20250514-v1:0",
			"claude-sonnet-4-6-20250514-v1:0",
			"anthropic.claude-sonnet-4-6-v1:0",
			"claude-sonnet-4-6-v1:0",
			// 尝试不同的日期格式
			"global.anthropic.claude-sonnet-4-6-20250601-v1:0",
			"us.anthropic.claude-sonnet-4-6-20250601-v1:0",
			"global.anthropic.claude-sonnet-4-6-20250615-v1:0",
			"us.anthropic.claude-sonnet-4-6-20250615-v1:0",
		}

		fmt.Printf("\n  Testing %d model ID formats:\n", len(modelIDs))
		for _, modelID := range modelIDs {
			valid, msg := testModelID(context.Background(), runtimeClient, modelID)
			if valid {
				fmt.Printf("  ✅ %s\n", modelID)
			} else {
				// 简化错误信息
				errMsg := msg
				if len(errMsg) > 80 {
					errMsg = errMsg[:80] + "..."
				}
				fmt.Printf("  ❌ %s - %s\n", modelID, errMsg)
			}
		}

		// 列出所有可用的 Claude 模型
		fmt.Printf("\n  Available Claude models in this region:\n")
		availableModels, err := listAvailableModels(context.Background(), bedrockClient)
		if err != nil {
			fmt.Printf("  ⚠️  Failed to list models: %v\n", err)
		} else {
			for _, model := range availableModels {
				if strings.Contains(model, "sonnet-4") {
					fmt.Printf("    - %s\n", model)
				}
			}
		}

		fmt.Println()
	}

	fmt.Println("=== Test Complete ===")
}
