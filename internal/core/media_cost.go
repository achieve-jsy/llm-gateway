package core

// MediaCostInput 媒体计价输入：按「实际产物数 / 实际时长 / 实际 token 数」记账，
// 不按请求参数。
//
// 与 ModelPricing 的非 token 字段族一一对应（能映射既有字段的一律复用）：
//
//	Images            → PerImage                （output_cost_per_image；缺失时回退 InputPerImage）
//	InputImages       → InputPerImage           （input_cost_per_image，如 images/edits 的输入图）
//	InputSeconds      → PerSecondInput          （input_cost_per_video_per_second / input_cost_per_second）
//	OutputSeconds     → PerSecondOutput         （output_cost_per_video_per_second / output_cost_per_second）
//	Requests          → PerRequest              （input_cost_per_request）
//	InputImageTokens  → InputPerImageToken      （input_cost_per_image_token，gpt-image 系）
//	OutputImageTokens → OutputPerImageToken     （output_cost_per_image_token）
//
// 计费口径见 docs/media-channels-integration-design.md §8.1：
// 终态才记账、失败/超时不记账；调用方只在拿到上游真实用量后构造本结构。
//
// 调用方只填**本次实际使用的计价桶**：填了用量却没有对应单价（NULL 或 0）的桶，
// 会让整体判定为「无价」（missing=true），这是刻意的保守语义 —— 宁可拒绝计费，
// 也不静默少收钱。
type MediaCostInput struct {
	Images            int     // 生成的图像张数
	InputImages       int     // 输入的图像张数
	InputSeconds      float64 // 输入时长（秒）
	OutputSeconds     float64 // 输出时长（秒）
	Requests          int     // 按次计费的次数
	InputImageTokens  int     // 输入图像 token 数
	OutputImageTokens int     // 输出图像 token 数
}

// HasUsage 是否携带任何可计价的媒体用量。
//
// 计价层用它区分「这是媒体请求」与「这是 token 请求」，避免把媒体请求
// 误判成 token 计价（InputPerMtok != nil 的旧判定缺陷）。
func (m MediaCostInput) HasUsage() bool {
	return m.Images > 0 || m.InputImages > 0 || m.InputSeconds > 0 || m.OutputSeconds > 0 ||
		m.Requests > 0 || m.InputImageTokens > 0 || m.OutputImageTokens > 0
}
