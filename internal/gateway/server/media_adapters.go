package server

import (
	"log/slog"

	"github.com/example/llm-gateway/internal/core"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/kling"
	"github.com/example/llm-gateway/internal/providers/vidu"
)

// mediaAdapterBuilders 是「provider type → 适配器构造器」表。
//
// 新增一个媒体渠道 = 在自己的包内实现 providers.MediaAdapterBuilder，然后**只在这一处**
// 加一行。之所以做成表而不是 switch providerType：Batch 2 的 Kling 与 Vidu 由两支队伍
// 并行开发，各自只动自己包内的文件；这张表由 lead 在接缝建立时一次性写好，
// 两队都不需要碰它，因此**零文件重叠、无合并冲突**。
var mediaAdapterBuilders = []struct {
	ProviderType string
	Build        providers.MediaAdapterBuilder
}{
	{ProviderType: kling.MediaProviderType, Build: kling.NewMediaAdapter},
	{ProviderType: vidu.MediaProviderType, Build: vidu.NewMediaAdapter},
}

// BuildMediaAdapters 按 provider snapshot 构建整张媒体适配器表。
//
// 语义（与 MediaAdapterRegistry.Swap 一致）：**整表重建**，不是增量合并。
// 某个渠道未被配置、Disabled、或构造失败 → 它不出现在返回的表里，下一次 Swap 后
// 该渠道的旧适配器会真的消失（渠道删除/停用必须立即生效，否则会继续用旧凭证打上游）。
//
// 构造失败的渠道只记 Error 日志并跳过，不影响其它渠道，也不 panic。
func BuildMediaAdapters(entries []gwmodel.ProviderEntry) (map[string]core.MediaTaskProvider, map[string]string) {
	adapters := make(map[string]core.MediaTaskProvider, len(mediaAdapterBuilders))
	names := make(map[string]string, len(mediaAdapterBuilders))

	for _, builder := range mediaAdapterBuilders {
		entry := findProviderByType(entries, builder.ProviderType)
		if entry == nil {
			continue
		}
		// Enabled 必须与 chat 路径（loader/provider_loader.go 的 `if !entry.Enabled { continue }`）
		// 保持一致：运维停用渠道时，两种链路都要停。否则会出现「chat 已停用、媒体任务仍打到
		// 该渠道」这种按渠道而非按功能的半停用状态——渠道被停用往往正是因为凭证泄露或
		// 费用失控，此时继续给它派发媒体任务是危险的。
		//
		// 现状说明：控制面产出 snapshot 的两条路径目前都硬编码 Enabled: true
		// （provider_snapshot_service.go 与 gateway/infra/config_loader.go），且
		// modelRepo.ListAvailable() 已按 providers.enabled = true 过滤，所以本条今天
		// 不会命中；它是为了在 snapshot 的 Enabled 语义将来变真实时，两条链路不会静默分叉。
		if !entry.Enabled {
			slog.Warn("media adapter skipped: provider disabled",
				"provider_type", builder.ProviderType, "provider", entry.Name)
			continue
		}
		adapter, err := builder.Build(providers.MediaAdapterParams{
			Type:        entry.Type,
			Name:        entry.Name,
			BaseURL:     entry.Endpoint,
			Credentials: entry.Credentials,
			Models:      mediaAdapterModels(entry),
		})
		if err != nil {
			slog.Error("media adapter build failed, channel skipped",
				"provider_type", builder.ProviderType,
				"provider", entry.Name,
				"error", err,
			)
			continue
		}
		if adapter == nil {
			// 防御：构造器不得返回 (nil, nil)（见 providers.MediaAdapterBuilder 约定）。
			slog.Error("media adapter builder returned nil adapter without error, channel skipped",
				"provider_type", builder.ProviderType, "provider", entry.Name)
			continue
		}
		adapters[builder.ProviderType] = adapter
		names[builder.ProviderType] = entry.Name
	}
	return adapters, names
}

// mediaAdapterModels 取渠道声明的模型名，兼容 models 与 models_v2 两种字段
// （旧版本快照只有前者，见 gwmodel.ProviderEntry 的注释）。
func mediaAdapterModels(entry *gwmodel.ProviderEntry) []string {
	if len(entry.ModelsV2) > 0 {
		out := make([]string, 0, len(entry.ModelsV2))
		for _, m := range entry.ModelsV2 {
			if m.Name != "" {
				out = append(out, m.Name)
			}
		}
		return out
	}
	return entry.Models
}
