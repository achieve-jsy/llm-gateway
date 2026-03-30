package server

import (
	"log/slog"

	"github.com/example/llm-gateway/internal/core/video"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	seedancerelay "github.com/example/llm-gateway/internal/providers/seedance/relay"
	seedancevolcengine "github.com/example/llm-gateway/internal/providers/seedance/volcengine"
)

// seedanceAdapters 从 snapshot entries 构建景汐/官方视频与素材适配器。
type seedanceAdapters struct {
	relay      *seedancerelay.Adapter
	volcengine *seedancevolcengine.Adapter
	relayName  string
	volcName   string
}

func buildSeedanceAdapters(entries []gwmodel.ProviderEntry) seedanceAdapters {
	out := seedanceAdapters{
		relayName: "seedance-relay",
		volcName:  "seedance-volcengine",
	}
	if entry := findProviderByType(entries, "seedance-relay"); entry != nil {
		slog.Info("loading seedance-relay adapter from provider config", "endpoint", entry.Endpoint, "name", entry.Name)
		out.relay = seedancerelay.NewAdapter(entry.Name, entry.Endpoint, entry.Credentials)
		out.relayName = entry.Name
	}
	if entry := findProviderByType(entries, "seedance-volcengine"); entry != nil {
		slog.Info("loading seedance-volcengine adapter from provider config", "endpoint", entry.Endpoint, "name", entry.Name)
		cred := seedancevolcengine.ParseCreds(entry.Credentials)
		if !cred.HasAuth() {
			slog.Warn("seedance-volcengine credentials incomplete, using placeholder", "auth_type", cred.AuthType)
			cred = seedancevolcengine.Creds{AuthType: seedancevolcengine.AuthAPIKey, APIKey: "placeholder-api-key"}
		}
		out.volcengine = seedancevolcengine.NewAdapterFromCreds(entry.Name, entry.Endpoint, cred)
		out.volcName = entry.Name
	}
	if out.relay == nil {
		slog.Warn("seedance-relay adapter not configured, using placeholder credentials")
		out.relay = seedancerelay.NewAdapter("seedance-relay", "http://115.190.78.58:8000", "placeholder-api-key")
	}
	if out.volcengine == nil {
		slog.Warn("seedance-volcengine adapter not configured, using placeholder credentials")
		out.volcengine = seedancevolcengine.NewBearerAdapter("seedance-volcengine", "https://ark.cn-beijing.volces.com", "placeholder-api-key")
	}
	return out
}

func (a seedanceAdapters) asVideoProviders() (video.VideoProvider, video.VideoProvider) {
	return a.relay, a.volcengine
}

func (a seedanceAdapters) asAssetProviders() (video.AssetProvider, video.AssetProvider) {
	return a.relay, a.volcengine
}

// findProviderByType 从 ProviderEntry 列表中按 Type 查找
func findProviderByType(entries []gwmodel.ProviderEntry, providerType string) *gwmodel.ProviderEntry {
	for i := range entries {
		if entries[i].Type == providerType {
			return &entries[i]
		}
	}
	return nil
}
