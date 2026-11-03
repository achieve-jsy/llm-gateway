package observability

import "github.com/example/llm-gateway/internal/core"

// 本文件提供「媒体计价准入」的统一判据（A2），供 middleware 的媒体准入闸门调用。
//
// 为什么单独一个文件：判据必须与记账侧（collector.go 的媒体分支 +
// EstimateMediaCost）同源，否则会出现「准入判有价、记账判无价」或反向的错配。
// 放在 observability 包内可以直接复用包内私有的 hasPositiveTokenPrice，
// 不需要改动 collector.go（A11 正在同目录工作，尽量减少触碰）。

// MediaPriced 判定模型是否具备媒体计价能力 —— 准入侧唯一判据。
//
// 语义（对齐 docs/media-channels-integration-design.md §8.3 / D3「无价策略」）：
//
//   - 任一**媒体**单价 > 0 → 有价；
//   - 只有 token 价 > 0、没有任何媒体价 → **也算有价**：终态用量到达后
//     collector 的媒体分支会回退 token 口径（EstimateCost），准入阶段把它判成
//     「无价」会误拒按 token 计费的图像模型（gpt-image 系、dall-e-2 等）；
//   - **字段存在但全是 NULL / 0 占位 → 无价**。这是 model_catalog 的刻意语义
//     （A1 迁移：媒体价列默认 NULL，与 token 价的 0 占位区分），必须被正确识别，
//     不能因为「字段在」就当作有价。
//
// 与 EstimateMediaCost 的分工：
//   - EstimateMediaCost 按**本次实际用量桶**逐桶判定（终态记账用，严格）；
//   - MediaPriced 是**维度无关**的准入判据：准入阶段还拿不到真实产物数/时长，
//     只能判断「这个模型到底有没有配价」。
func MediaPriced(p *core.ModelPricing) bool {
	if p == nil {
		return false
	}
	return hasPositiveMediaPrice(p) || hasPositiveTokenPrice(p)
}

// hasPositiveMediaPrice 判定是否存在正的媒体单价。
//
// 覆盖 ModelPricing 里全部非 token 的媒体字段族（与 core.MediaCostInput 的桶一一
// 对应，另含 EstimateMediaCost 尚未消费、但目录里已有的 per-pixel / per-video-token /
// per-character），避免「配了价却被准入判无价」。
//
// 注意：只看**指针非 nil** 是不够的 —— catalog 里 0 是占位（如 dall-e-3 的
// input_price_per_million = 0），0 元不是价，必须 > 0。
func hasPositiveMediaPrice(p *core.ModelPricing) bool {
	if p == nil {
		return false
	}
	prices := []*float64{
		// 图像：按张
		p.PerImage, p.InputPerImage,
		// 视频/音频：按秒
		p.PerSecondInput, p.PerSecondOutput,
		// 按次
		p.PerRequest,
		// 图像 token / 像素 / 视频 token / 字符
		p.InputPerImageToken, p.OutputPerImageToken,
		p.InputPerPixel, p.OutputPerPixel,
		p.InputPerVideoToken, p.OutputPerVideoToken,
		p.PerCharacterInput,
	}
	for _, price := range prices {
		if price != nil && *price > 0 {
			return true
		}
	}
	return false
}
