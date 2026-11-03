// Package kling 实现快手可灵（Kling）异步视频任务适配器。
//
// 本文件是 Batch 0/1 由 lead 建立的**接缝骨架**：它让「渠道 → 适配器 → 注册表 →
// /v1/videos」这条链路先可编译、可注册、可观测，渠道实现（Batch 2 · D 队）只需
// 替换本文件里的构造器实现，**不需要改任何共享文件**。
package kling

import (
	"fmt"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// MediaProviderType 是 Kling 渠道的 provider type，同时是媒体适配器注册表的键。
// 必须与控制面 `provider.type` 的取值逐字一致。
const MediaProviderType = "kling"

// NewMediaAdapter 构造 Kling 异步视频任务适配器（core.MediaTaskProvider）。
//
// ⚠️ 当前返回「未实现」：这是刻意的骨架状态，保证未完成的渠道**不会**用占位凭证
// 打真实上游（会产生生成费用），而是让请求得到一个明确且可观测的错误。
//
// Batch 2 · D1/D2/D5/D6 在此包内实现：
//   - 鉴权 HS256 JWT（iss=accessKey、exp=now+1800、nbf=now-5，签名密钥=secretKey）；
//   - Submit 翻译到 POST {base}/v1/videos/{text2video|image2video}；
//   - Query 翻译到 GET {base}/v1/videos/{op}/{task_id}（op 必须与提交时一致）；
//   - 状态枚举 {submitted, processing, succeed, failed} 逐字对齐、未知值报错。
//
// 入参只依赖 providers.MediaAdapterParams（见该类型注释），凭据自行解析。
func NewMediaAdapter(params providers.MediaAdapterParams) (core.MediaTaskProvider, error) {
	_ = params
	return nil, fmt.Errorf("kling: 媒体适配器尚未实现（Batch 2 · D1 待接入）: %w", core.ErrMediaNotSupported)
}
