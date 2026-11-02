// 命令 seed 为「真实双进程脱敏 E2E」准备最小数据集：
// 一个 admin 用户、一个固定 API Key、一个 openai 兼容渠道、一个 chat 模型。
//
// 之所以不在测试里用 ORM 现建：providers.encrypted_credentials 必须由控制面的
// KEK/DEK 管理器加密（internal/controlplane/pkg/crypto），否则控制面解密时拿不到 api_key。
//
// 用法（幂等，可重复执行）：
//
//	E2E_DATABASE_URL=postgres://... ENCRYPTION_KEY=... go run ./tests/e2e/privacy/seed
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	ucmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// 固定值，方便 E2E 脚本直接引用；只是一个本地测试 Key。
	userID      = "11111111-1111-1111-1111-111111111111"
	apiKeyID    = "22222222-2222-2222-2222-222222222222"
	providerID  = "33333333-3333-3333-3333-333333333333"
	modelID     = "44444444-4444-4444-4444-444444444444"
	APIKeyPlain = "sk-e2e-privacy-local-0000000000000000"
	ModelName   = "bf-privacy-e2e"
)

func main() {
	dsn := os.Getenv("E2E_DATABASE_URL")
	if dsn == "" {
		fatal("E2E_DATABASE_URL 未设置")
	}
	upstream := os.Getenv("E2E_UPSTREAM_BASE_URL")
	if upstream == "" {
		fatal("E2E_UPSTREAM_BASE_URL 未设置（形如 http://127.0.0.1:19090/v1）")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fatal("连接数据库失败: " + err.Error())
	}
	// 本地 E2E 库由 atlas-migrations 建表；代码 HEAD 里有些列还没有对应迁移
	// （例如 models.quota_rpm），这里对用到的几张表做一次增量 AutoMigrate 补齐，
	// 只加列不删列。生产环境仍以 Atlas 迁移为准。
	if err := db.AutoMigrate(
		&ucmodel.ApiKey{}, &ucmodel.ProviderDefinition{},
		&ucmodel.Provider{}, &ucmodel.ModelCatalog{}, &ucmodel.Model{},
		&crypto.EncryptionKey{},
	); err != nil {
		fatal("补齐本地 E2E 表结构失败: " + err.Error())
	}
	// 凭证加密的 KEK/DEK 表由 control-plane 里的 crypto 包按需写入，
	// 但仓库没有它对应的 Atlas 迁移（历史遗留），新库需要先补这张表。
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS encryption_keys (
		version integer PRIMARY KEY,
		encrypted_key text NOT NULL,
		algorithm varchar(50) NOT NULL DEFAULT '',
		status varchar(20) NOT NULL DEFAULT 'active',
		created_at timestamptz NOT NULL DEFAULT now(),
		retired_at timestamptz
	)`).Error; err != nil {
		fatal("创建 encryption_keys 表失败: " + err.Error())
	}
	if err := crypto.InitFromDB(db); err != nil {
		fatal("初始化凭证加密失败（检查 ENCRYPTION_KEY）: " + err.Error())
	}
	creds, err := crypto.Encrypt(`{"api_key":"sk-e2e-upstream"}`)
	if err != nil {
		fatal("加密渠道凭证失败: " + err.Error())
	}
	sum := sha256.Sum256([]byte(APIKeyPlain))
	keyHash := hex.EncodeToString(sum[:])

	stmts := []struct {
		name string
		sql  string
		args []any
	}{
		{"user", `INSERT INTO users (id, sub, name, email, source, role, created_at, updated_at)
			VALUES (?, 'e2e-admin', 'E2E Admin', 'e2e@local', 'local', 'admin', now(), now())
			ON CONFLICT (id) DO UPDATE SET role = 'admin', updated_at = now()`, []any{userID}},
		{"api_key", `INSERT INTO api_keys (id, user_id, name, key_hash, encrypted_key, key_prefix, key_hint,
				enabled, log_enabled, access_all_groups, user_name, created_at, updated_at)
			VALUES (?, ?, 'e2e-privacy', ?, '', ?, ?, true, true, true, 'E2E Admin', now(), now())
			ON CONFLICT (id) DO UPDATE SET key_hash = EXCLUDED.key_hash, enabled = true, updated_at = now()`,
			[]any{apiKeyID, userID, keyHash, APIKeyPlain[:8], APIKeyPlain[len(APIKeyPlain)-4:]}},
		// provider_definitions 是 providers.definition_id 的外键目标，先建一个最小定义。
		{"provider_definition", `INSERT INTO provider_definitions (id, display_name, adapter, base_url,
				credential_schema, vendor_options, description, default_models, builtin, enabled, created_at, updated_at)
			VALUES ('e2e-openai-def', 'E2E OpenAI', 'openai', 'https://api.openai.com/v1',
				'{}'::jsonb, '{}'::jsonb, 'e2e only', '[]'::jsonb, false, true, now(), now())
			ON CONFLICT (id) DO NOTHING`, nil},
		{"provider", `INSERT INTO providers (id, name, definition_id, type, endpoint, encrypted_credentials,
				enabled, weight, priority, status, max_retries, timeout, created_at, updated_at)
			VALUES (?, 'e2e-openai', 'e2e-openai-def', 'openai', ?, ?, true, 1, 0, 1, 3, 30, now(), now())
			ON CONFLICT (id) DO UPDATE SET endpoint = EXCLUDED.endpoint,
				encrypted_credentials = EXCLUDED.encrypted_credentials, enabled = true, status = 1, updated_at = now()`,
			[]any{providerID, upstream, creds}},
		{"model", `INSERT INTO models (id, name, provider_id, model_type, enabled, upstream_model_id, status,
				context_length, max_output_tokens, input_price, output_price, created_at, updated_at)
			VALUES (?, ?, ?, 'chat', true, ?, 1, 128000, 4096, 0, 0, now(), now())
			ON CONFLICT (id) DO UPDATE SET enabled = true, status = 1, provider_id = EXCLUDED.provider_id, updated_at = now()`,
			// upstream_model_id 与对外模型名保持一致：控制面的「组内成员」和
			// 「available 列表」用的是不同字段，名字不一致会导致
			// providers.Router 查不到 provider/model。
			[]any{modelID, ModelName, providerID, ModelName}},
	}
	for _, s := range stmts {
		if err := db.Exec(s.sql, s.args...).Error; err != nil {
			fatal(fmt.Sprintf("写入 %s 失败: %v", s.name, err))
		}
	}

	fmt.Printf("OK api_key=%s model=%s upstream=%s\n", APIKeyPlain, ModelName, upstream)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "seed: "+msg)
	os.Exit(1)
}
