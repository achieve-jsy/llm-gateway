package providers

import "github.com/example/llm-gateway/internal/core"

// MediaAdapterParams 是构造异步媒体任务适配器（core.MediaTaskProvider）的渠道无关入参。
//
// 为什么不直接传 gateway 的 ProviderEntry：分层方向是 **gateway → providers**，
// internal/providers/** 不得反向依赖 internal/gateway/model。渠道包只依赖本类型与 core，
// 因此「新增一个渠道」不需要改动任何共享文件——这也是把 Batch 2 拆成 Kling / Vidu
// 两支队伍并行、零文件重叠的前提。
type MediaAdapterParams struct {
	// Type 是 provider type（如 "kling" / "vidu"），同时也是适配器注册进
	// MediaAdapterRegistry 的键，必须与渠道的 provider_type 完全一致。
	Type string
	// Name 是渠道显示名（来自 provider.name），用于日志与对外展示。
	Name string
	// BaseURL 是上游 base URL（来自 provider.endpoint）；为空表示用渠道默认地址。
	BaseURL string
	// Credentials 是**原始凭证文本**（provider_credentials）。
	// 各渠道自行解析自己的 schema（如 Kling 的 access_key/secret_key），
	// 网关不解释其内部结构——渠道之间因此互不耦合。
	//
	// ⚠️ 形态不一定是 JSON！控制面 internal/controlplane/service/credential.go 的
	// extractAPIKey 会按 auth_type 决定下发形态：
	//   - auth_type ∈ {aksk, iam, hmac} → **保留完整 JSON**（如 Kling 的
	//     access_key/secret_key 双列，因为既无 auth_type 也无 api_key，也走这条）；
	//   - 否则若含 api_key 字段 → **抽成裸字符串**（单 Key 渠道，如 Vidu）；
	//   - 两者都不满足 → 原样返回。
	// 因此**单 Key 渠道的适配器必须同时接受裸字符串与 JSON 两种形态**，否则真机上会
	// 「凭据解析失败 → 渠道静默从注册表消失 → 全部请求 501」。已由 providers/vidu 验证。
	//
	// 反面注意：若在双列凭据（如 Kling）里加上 api_key 字段，它会被抽成裸字符串并
	// 导致该渠道静默失效——双列渠道的凭据 JSON 里**不要**出现 api_key 键。
	Credentials string
	// Models 是该渠道声明的模型名列表（可空，仅用于日志/能力提示）。
	Models []string
}

// MediaAdapterBuilder 是渠道侧提供的适配器构造器。
//
// 冻结约定（渠道实现必须遵守）：
//
//   - **凭证缺失/非法时返回 error，不要返回占位适配器。** 占位凭证会让请求真的打到
//     上游并可能产生费用；返回 error 后该渠道不出现在注册表里，请求得到明确的
//     501 not_implemented，既安全又可观测。
//   - 构造失败不得 panic，也不得返回 (nil, nil)。
//   - 构造器只做「解析配置 + 建 HTTP client」，**不得发任何网络请求**
//     （探活是 Batch 3 的独立能力，且必须不产生生成费用）。
type MediaAdapterBuilder func(params MediaAdapterParams) (core.MediaTaskProvider, error)
