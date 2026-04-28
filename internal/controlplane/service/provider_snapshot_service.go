package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	controlmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/redis/go-redis/v9"
)

// ProviderSnapshotRefresher rebuilds the shared gateway provider snapshot.
type ProviderSnapshotRefresher struct {
	modelRepo ModelRepoInterface
	rdb       *redis.Client
}

// NewProviderSnapshotRefresher creates a new snapshot refresher.
func NewProviderSnapshotRefresher(modelRepo ModelRepoInterface, rdb *redis.Client) *ProviderSnapshotRefresher {
	return &ProviderSnapshotRefresher{modelRepo: modelRepo, rdb: rdb}
}

// RefreshAvailableSnapshot rebuilds and stores the latest available providers snapshot.
func (r *ProviderSnapshotRefresher) RefreshAvailableSnapshot(ctx context.Context) (int64, error) {
	if r == nil || r.modelRepo == nil || r.rdb == nil {
		return 0, nil
	}

	models, err := r.modelRepo.ListAvailable()
	if err != nil {
		return 0, fmt.Errorf("list available models: %w", err)
	}

	entries, err := buildProviderEntries(models)
	if err != nil {
		return 0, err
	}

	version := time.Now().UnixNano()
	snapshot := &configsync.ProviderSnapshot{
		Version:   version,
		UpdatedAt: time.Now().UTC(),
		Entries:   entries,
	}

	if err := configsync.SaveProviderSnapshot(ctx, r.rdb, snapshot); err != nil {
		return 0, fmt.Errorf("save provider snapshot: %w", err)
	}

	return version, nil
}

func buildProviderEntries(models []controlmodel.Model) ([]gatewaymodel.ProviderEntry, error) {
	providerMap := make(map[string]*gatewaymodel.ProviderEntry)
	providerOrder := make([]string, 0)

	for _, m := range models {
		providerID := m.ProviderID.String()
		entry, ok := providerMap[providerID]
		if !ok {
			credentials, err := crypto.Decrypt(m.Provider.EncryptedCredentials)
			if err != nil {
				return nil, fmt.Errorf("decrypt provider credentials for %s: %w", providerID, err)
			}

			// api_version 从 ProviderDefinition.VendorOptions 继承，per-instance 可覆盖（暂不开放）
			var apiVersion string
			if def := m.Provider.Definition; def != nil {
				apiVersion = def.VendorOptions.APIVersion
			}

			entry = &gatewaymodel.ProviderEntry{
				ID:          providerID,
				Name:        m.Provider.Name,
				Type:        providerTypeForGateway(m.Provider),
				Endpoint:    m.Provider.Endpoint,
				Credentials: extractAPIKey(credentials),
				APIVersion:  apiVersion,
				Enabled:     true,
				Models:      make([]string, 0, 4),
			}
			providerMap[providerID] = entry
			providerOrder = append(providerOrder, providerID)
		}

		// 保留完整模型名（含 "{provider}/" 前缀）。
		// 某些上游（如 ZHIPU）期望收到带前缀的完整模型名；
		// Bedrock 等需要剥前缀的 provider 在各自的 adapter 中处理。
		entry.Models = append(entry.Models, m.Name)
	}

	entries := make([]gatewaymodel.ProviderEntry, 0, len(providerOrder))
	for _, providerID := range providerOrder {
		entry := providerMap[providerID]
		sort.Strings(entry.Models)
		entries = append(entries, *entry)
	}

	return entries, nil
}

func providerTypeForGateway(p controlmodel.Provider) string {
	if p.Type != "" {
		return p.Type
	}
	return p.DefinitionID
}
