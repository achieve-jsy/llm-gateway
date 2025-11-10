package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	"github.com/joho/godotenv"

	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	authhandler "github.com/example/llm-gateway/internal/auth/handler"
	authsvc "github.com/example/llm-gateway/internal/auth/service"

	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/example/llm-gateway/internal/gateway/provider/anthropic"
	"github.com/example/llm-gateway/internal/gateway/provider/bailian"
	"github.com/example/llm-gateway/internal/gateway/provider/bedrock"
	"github.com/example/llm-gateway/internal/gateway/provider/deepseek"
	"github.com/example/llm-gateway/internal/gateway/provider/litellm"
	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/redis/go-redis/v9"
)

type providerLoadResult struct {
	source  string
	version int64
}

func main() {
	// 加载 .env 文件（开发环境，文件不存在时不报错）
	_ = godotenv.Load()

	// 配置 slog JSON 格式
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// 加载配置
	cfg := config.Load()

	// 初始化 Redis
	rdb, err := infra.InitRedis(cfg.Redis)
	if err != nil {
		log.Fatalf("failed to connect Redis: %v", err)
	}

	// 初始化缓存
	cache := infra.NewCache(rdb)

	// 初始化 OTLP Tracer
	shutdown, err := infra.InitTracer(cfg.OTLP)
	if err != nil {
		log.Fatalf("failed to init tracer: %v", err)
	}
	defer shutdown()

	// 初始化 Provider Registry
	registry := provider.NewRegistry()
	state := &provider.RuntimeState{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 尝试从 control-plane 加载 Provider 配置
	controlPlaneURL := cfg.Provider.ControlPlaneURL
	internalToken := cfg.Provider.InternalToken

	if result, err := loadProviders(ctx, rdb, registry, controlPlaneURL, internalToken); err != nil {
		state.MarkLoadFailure(err)
		slog.Warn("failed to load providers from shared sources, falling back to env vars", "error", err)
		registerProvidersFromEnv(registry)
		state.MarkLoadSuccess("env", len(registry.ListAllModels()), 0)
	} else {
		state.MarkLoadSuccess(result.source, len(registry.ListAllModels()), result.version)
	}

	// 启动配置变更订阅（US-06）
	subscriber := infra.NewSubscriber(rdb, func(ev infra.ChangeEvent) {
		loaded, _, _, _, _, currentVersion := state.Snapshot()
		if ev.SnapshotVersion != 0 && loaded && len(registry.ListAllModels()) > 0 && ev.SnapshotVersion == currentVersion {
			slog.Info("config change already applied, skipping reload", "type", ev.Type, "snapshot_version", ev.SnapshotVersion)
			return
		}
		slog.Info("config change received, reloading registry", "type", ev.Type, "resource_type", ev.ResourceType)
		if result, err := loadProviders(ctx, rdb, registry, controlPlaneURL, internalToken); err != nil {
			state.MarkLoadFailure(err)
			slog.Error("failed to reload providers", "error", err)
		} else {
			state.MarkLoadSuccess(result.source, len(registry.ListAllModels()), result.version)
			slog.Info("registry reloaded successfully")
		}
	})

	go reconcileProviders(ctx, rdb, registry, state, controlPlaneURL, internalToken)

	go subscriber.Start(ctx)

	// 处理优雅退出信号
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("received shutdown signal")
		cancel()
	}()

	authClient := infra.NewAuthClient(cfg.Auth.URL, cfg.Auth.Token)

	// Wire auth services (JWT / Key Auth / OIDC)
	authJWTManager, err := acrypto.NewJWTManagerFromEnv()
	if err != nil {
		slog.Warn("JWT manager init failed, JWKS/token endpoints disabled", "error", err)
	}
	authTokenSvc := authsvc.NewTokenService(authJWTManager)

	authHTTPClient := &http.Client{Timeout: 5 * time.Second}
	authKeyAuthSvc := authsvc.NewKeyAuthService(authHTTPClient, rdb, controlPlaneURL, internalToken)

	// Auth handlers
	authJWKSHandler := authhandler.NewJWKSHandler()
	authTokenHandler := authhandler.NewTokenHandler(authTokenSvc, rdb)
	authInternalHandler := authhandler.NewInternalHandler(authKeyAuthSvc)

	// 初始化路由（注入依赖）
	r := server.Setup(registry, cache, state, authClient, controlPlaneURL, internalToken)

	// Register auth routes
	r.GET("/.well-known/jwks.json", authJWKSHandler.GetJWKS)
	r.POST("/auth/refresh", authTokenHandler.Refresh)
	r.POST("/auth/logout", authTokenHandler.Logout)
	r.POST("/internal/v1/auth/verify-key", authInternalHandler.VerifyKey)

	// 启动服务
	slog.Info("gateway starting", "port", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func loadProviders(ctx context.Context, rdb *redis.Client, registry *provider.Registry, controlPlaneURL, token string) (*providerLoadResult, error) {
	if result, err := loadAndRegisterProvidersFromSnapshot(ctx, rdb, registry); err == nil {
		slog.Info("providers loaded from redis snapshot successfully")
		return result, nil
	} else {
		slog.Warn("failed to load providers from redis snapshot", "error", err)
	}

	if controlPlaneURL != "" && token != "" {
		if err := loadAndRegisterProviders(registry, controlPlaneURL, token); err != nil {
			return nil, fmt.Errorf("load providers from control-plane: %w", err)
		}
		slog.Info("providers loaded from control-plane successfully")
		return &providerLoadResult{source: "control-plane", version: 0}, nil
	}

	return nil, fmt.Errorf("no provider snapshot or control-plane source available")
}

func loadAndRegisterProvidersFromSnapshot(ctx context.Context, rdb *redis.Client, registry *provider.Registry) (*providerLoadResult, error) {
	snapshot, err := configsync.LoadProviderSnapshot(ctx, rdb)
	if err != nil {
		return nil, err
	}

	registry.Reload(snapshot.Entries, providerFactory)
	slog.Info("registry reloaded from redis snapshot", "providers", len(snapshot.Entries), "version", snapshot.Version)
	return &providerLoadResult{source: "redis-snapshot", version: snapshot.Version}, nil
}

func reconcileProviders(ctx context.Context, rdb *redis.Client, registry *provider.Registry, state *provider.RuntimeState, controlPlaneURL, token string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			loaded, _, _, _, _, currentVersion := state.Snapshot()
			if snapshot, err := configsync.LoadProviderSnapshot(ctx, rdb); err == nil {
				if loaded && len(registry.ListAllModels()) > 0 && snapshot.Version == currentVersion {
					continue
				}
			}

			if result, err := loadProviders(ctx, rdb, registry, controlPlaneURL, token); err != nil {
				state.MarkLoadFailure(err)
				slog.Warn("periodic provider reconcile failed", "error", err)
				continue
			} else {
				state.MarkLoadSuccess(result.source, len(registry.ListAllModels()), result.version)
			}
		}
	}
}

// loadAndRegisterProviders 从 control-plane 加载配置并使用 Reload 原子替换 registry
func loadAndRegisterProviders(registry *provider.Registry, controlPlaneURL, token string) error {
	entries, err := infra.LoadFromControlPlane(controlPlaneURL, token)
	if err != nil {
		return err
	}

	registry.Reload(entries, providerFactory)
	slog.Info("registry reloaded from control-plane", "providers", len(entries))
	return nil
}

// providerFactory 根据 ProviderEntry 创建对应的 Provider 实例
func providerFactory(entry model.ProviderEntry) provider.Provider {
	switch entry.Type {
	case "anthropic":
		return anthropic.New(anthropic.Config{
			APIKey:  entry.Credentials,
			BaseURL: entry.Endpoint,
			Models:  entry.Models,
		})
	case "bedrock":
		// bedrock 凭证格式: JSON {"access_key_id": "...", "secret_access_key": "...", "region": "..."}
		var creds struct {
			AccessKeyID    string `json:"access_key_id"`
			SecretAccessKey string `json:"secret_access_key"`
			Region         string `json:"region"`
		}
		if err := json.Unmarshal([]byte(entry.Credentials), &creds); err != nil {
			slog.Error("failed to parse bedrock credentials", "provider", entry.Name, "error", err)
			// fallback: 使用整个 credentials 字符串作为 access key
			return bedrock.New(bedrock.Config{
				AccessKeyID:    entry.Credentials,
				SecretAccessKey: "",
				Region:         "us-east-1",
				Models:         entry.Models,
			})
		}
		return bedrock.New(bedrock.Config{
			AccessKeyID:    creds.AccessKeyID,
			SecretAccessKey: creds.SecretAccessKey,
			Region:         creds.Region,
			Models:         entry.Models,
		})
	case "deepseek":
		return deepseek.New(deepseek.Config{
			APIKey:  entry.Credentials,
			BaseURL: entry.Endpoint,
			Models:  entry.Models,
		})
	case "litellm":
		return litellm.New(litellm.Config{
			Endpoint: entry.Endpoint,
			APIKey:   entry.Credentials,
			Models:   entry.Models,
		})
	default:
		// dashscope, openai-compatible 等都走 bailian adapter（OpenAI 兼容模式）
		return bailian.New(bailian.Config{
			APIKey:  entry.Credentials,
			BaseURL: entry.Endpoint,
			Models:  entry.Models,
		})
	}
}

// registerProvidersFromEnv 从环境变量注册 Provider（向后兼容）
func registerProvidersFromEnv(registry *provider.Registry) {
	// 注册百炼 Provider
	bailianAPIKey := os.Getenv("BAILIAN_API_KEY")
	bailianModels := os.Getenv("BAILIAN_MODELS")
	if bailianAPIKey != "" {
		models := parseModelList(bailianModels)
		bailianProvider := bailian.New(bailian.Config{
			APIKey: bailianAPIKey,
			Models: models,
		})
		registry.Register("bailian", bailianProvider)
		for _, m := range models {
			registry.RegisterModel(m, "bailian")
		}
		slog.Info("百炼 Provider 已注册", "models", models)
	}

	// 注册 DeepSeek Provider
	deepseekAPIKey := os.Getenv("DEEPSEEK_API_KEY")
	deepseekModels := os.Getenv("DEEPSEEK_MODELS")
	if deepseekAPIKey != "" {
		models := parseModelList(deepseekModels)
		if len(models) == 0 {
			models = []string{"deepseek-chat", "deepseek-reasoner"}
		}
		deepseekProvider := deepseek.New(deepseek.Config{
			APIKey: deepseekAPIKey,
			Models: models,
		})
		registry.Register("deepseek", deepseekProvider)
		for _, m := range models {
			registry.RegisterModel(m, "deepseek")
		}
		slog.Info("DeepSeek Provider 已注册", "models", models)
	}

	// 注册 litellm 兜底 Provider
	litellmEndpoint := os.Getenv("LITELLM_ENDPOINT")
	litellmAPIKey := os.Getenv("LITELLM_API_KEY")
	litellmModels := os.Getenv("LITELLM_MODELS")
	if litellmEndpoint != "" {
		models := parseModelList(litellmModels)
		litellmProvider := litellm.New(litellm.Config{
			Endpoint: litellmEndpoint,
			APIKey:   litellmAPIKey,
			Models:   models,
		})
		registry.Register("litellm", litellmProvider)
		for _, m := range models {
			registry.RegisterModel(m, "litellm")
		}
		slog.Info("LiteLLM Provider 已注册", "models", models)
	}
}

// parseModelList 解析逗号分隔的模型列表
func parseModelList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	models := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			models = append(models, p)
		}
	}
	return models
}
