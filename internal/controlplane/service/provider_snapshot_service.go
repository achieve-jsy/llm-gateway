package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	controlmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	controlpricing "github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/example/llm-gateway/internal/core"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/redis/go-redis/v9"
)

// GroupNameLister 提供 enabled=true 的模型组名，用于随 provider 快照一起下发。
// 实现者：internal/controlplane/repository.GroupRepo。
type GroupNameLister interface {
	ListEnabledGroupNames() ([]string, error)
}

// ProviderSnapshotRefresher rebuilds the shared gateway provider snapshot.
type ProviderSnapshotRefresher struct {
	modelRepo ModelRepoInterface
	rdb       *redis.Client
	// groupLister 为可选依赖：未注入时快照不带组名（行为与改造前一致）。
	groupLister GroupNameLister
}

// NewProviderSnapshotRefresher creates a new snapshot refresher.
func NewProviderSnapshotRefresher(modelRepo ModelRepoInterface, rdb *redis.Client) *ProviderSnapshotRefresher {
	return &ProviderSnapshotRefresher{modelRepo: modelRepo, rdb: rdb}
}

// SetGroupNameLister 注入组名来源（刻意用 setter，避免改 NewProviderSnapshotRefresher 的签名
// 破坏既有调用点与测试 fake）。nil 安全。
func (r *ProviderSnapshotRefresher) SetGroupNameLister(l GroupNameLister) {
	if r == nil {
		return
	}
	r.groupLister = l
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

	// 组名是网关冷启动的预热数据，但 provider 快照本身是可用性关键路径：
	// 读取组名失败只能告警，绝不能让快照写不出去（否则网关连模型都拉不到）。
	if r.groupLister != nil {
		if names, err := r.groupLister.ListEnabledGroupNames(); err != nil {
			slog.Warn("failed to list enabled group names for provider snapshot", "error", err)
		} else {
			snapshot.GroupNames = names
		}
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
				ModelsV2:    make([]gatewaymodel.ModelEntry, 0, 4),
			}
			providerMap[providerID] = entry
			providerOrder = append(providerOrder, providerID)
		}

		// 保留完整模型名（含 "{provider}/" 前缀）。
		// 某些上游（如 ZHIPU）期望收到带前缀的完整模型名；
		// Bedrock 等需要剥前缀的 provider 在各自的 adapter 中处理。
		entry.Models = append(entry.Models, m.Name)
		modelEntry := gatewaymodel.ModelEntry{Name: m.Name, UpstreamModelID: m.UpstreamModelID}
		if pricing := snapshotModelPricing(m); len(pricing.FieldSources("database")) > 0 {
			modelEntry.Pricing = &pricing
		}
		entry.ModelsV2 = append(entry.ModelsV2, modelEntry)
	}

	entries := make([]gatewaymodel.ProviderEntry, 0, len(providerOrder))
	for _, providerID := range providerOrder {
		entry := providerMap[providerID]
		sort.Strings(entry.Models)
		sort.Slice(entry.ModelsV2, func(i, j int) bool { return entry.ModelsV2[i].Name < entry.ModelsV2[j].Name })
		entries = append(entries, *entry)
	}

	return entries, nil
}

func snapshotModelPricing(m controlmodel.Model) core.ModelPricing {
	var inputPricePerMillion, outputPricePerMillion *float64
	var cacheReadPrice, cacheWritePrice *float64
	var tierThreshold *int64
	var inputPriceAboveTier, outputPriceAboveTier *float64
	var rawPayload string
	if m.Catalog != nil {
		inputPricePerMillion = &m.Catalog.InputPricePerMillion
		outputPricePerMillion = &m.Catalog.OutputPricePerMillion
		cacheReadPrice = m.Catalog.CacheReadPrice
		cacheWritePrice = m.Catalog.CacheWritePrice
		tierThreshold = m.Catalog.TierThreshold
		inputPriceAboveTier = m.Catalog.InputPriceAboveTier
		outputPriceAboveTier = m.Catalog.OutputPriceAboveTier
		rawPayload = m.Catalog.RawPayload
	}
	pricing := controlpricing.ResolveModelPricing(
		m.PriceMode, m.InputPrice, m.OutputPrice,
		m.CacheReadPrice, m.CacheWritePrice,
		inputPricePerMillion, outputPricePerMillion,
		cacheReadPrice, cacheWritePrice,
		tierThreshold, inputPriceAboveTier, outputPriceAboveTier,
	)
	usagePricing := controlpricing.ResolveUsagePricing(controlpricing.UsagePricingInput{
		ModelType: m.ModelType, PriceMode: m.PriceMode,
		InputPrice: m.InputPrice, CatalogRawPayload: rawPayload,
	})
	if usagePricing.RerankPerSearchUnit != nil {
		pricing.RerankPerSearchUnit = usagePricing.RerankPerSearchUnit
	}
	return pricing
}

func providerTypeForGateway(p controlmodel.Provider) string {
	if p.Type != "" {
		return p.Type
	}
	return p.DefinitionID
}
