// Package vidu 实现生数科技 Vidu 异步视频任务适配器。
//
// 本文件是 Batch 0/1 由 lead 建立的**接缝骨架**：它让「渠道 → 适配器 → 注册表 →
// /v1/videos」这条链路先可编译、可注册、可观测，渠道实现（Batch 2 · D 队）只需
// 替换本文件里的构造器实现，**不需要改任何共享文件**。
package vidu

import (
	"fmt"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// MediaProviderType 是 Vidu 渠道的 provider type，同时是媒体适配器注册表的键。
// 必须与控制面 `provider.type` 的取值逐字一致。
const MediaProviderType = "vidu"

// NewMediaAdapter 构造 Vidu 异步视频任务适配器（core.MediaTaskProvider）。
//
// ⚠️ 当前返回「未实现」：这是刻意的骨架状态，保证未完成的渠道**不会**用占位凭证
// 打真实上游（会产生生成费用），而是让请求得到一个明确且可观测的错误。
//
// Batch 2 · D3/D4/D5/D6 在此包内实现：
//   - 鉴权 `Authorization: Token <apiKey>`；
//   - Submit 翻译到 POST {base}/ent/v2/{text2video|img2video|start-end2video|reference2video}
//     （**action 由 images 数组长度决定**：0→文生、1→图生、2→首尾帧、>2→参考）；
//   - Query 翻译到 GET {base}/ent/v2/tasks/{task_id}/creations；
//   - 状态枚举 {created, queueing, processing, success, failed} 逐字对齐、未知值报错。
//
// 入参只依赖 providers.MediaAdapterParams（见该类型注释），凭据自行解析。
func NewMediaAdapter(params providers.MediaAdapterParams) (core.MediaTaskProvider, error) {
	_ = params
	return nil, fmt.Errorf("vidu: 媒体适配器尚未实现（Batch 2 · D3 待接入）: %w", core.ErrMediaNotSupported)
}
