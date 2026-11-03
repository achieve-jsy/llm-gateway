package core

import (
	"context"
	"encoding/json"
	"errors"
)

// 本文件定义「同步图像」可选能力接口与 generations / edits 契约。
// 设计依据：docs/media-channels-integration-design.md §3.3（gpt-image 上游契约）、§6.1（core 类型提案）。
//
// 与 core.Provider 的关系：ImageProvider 是**可选能力接口**，与 NativeBatchProvider /
// NativeFileProvider / PassthroughProvider 同范式 —— **绝不扩展 core.Provider**。
// 未实现该接口的 provider 由上层（handler）做类型断言后映射为 HTTP 501。
//
// 参数支持矩阵（照设计文档 §3.3，实现时不要放宽）：
//   - generations 支持：background / moderation / n / output_compression / output_format / quality / size / user
//   - edits 额外支持：image（多图为重复字段 image[]）、mask、input_fidelity、response_format
//   - **generations 不支持 response_format**（那是 dall-e 的形状）、也不支持 style（仅 dall-e-3）与 seed；
//     客户端传入不支持参数时由 handler 返回 400（并提示改用 output_format），**不做静默丢弃**。

// ErrMediaNotSupported 表示 provider 未实现相关媒体可选能力。
//
// 语义与映射（务必区分，不要和「上游 4xx 参数错误」混用）：
//   - provider 完全没有图像能力（类型断言失败，或内部无 CompatibleProvider 可委派）→ 返回本错误；
//   - 上层 handler 必须把本错误映射为 HTTP 501 not_implemented，而不是 400/500；
//   - 上游返回的参数错误属于调用方问题，应返回 *GatewayError（400）而不是本错误。
var ErrMediaNotSupported = errors.New("core: media capability not supported by provider")

// gpt-image 的默认参数（设计文档 §3.3）。客户端未提供时，适配器可按这些值补齐；
// 这里只固定「缺省值」，**不固定取值范围**（支持矩阵与白名单由适配器/handler 按上游契约校验）。
const (
	ImageDefaultSize         = "1024x1024"
	ImageDefaultQuality      = "high"
	ImageDefaultOutputFormat = "png"
	// ImageDefaultMaskFileName edits 的 mask 缺省文件名（LiteLLM 固定用 mask.png）。
	ImageDefaultMaskFileName = "mask.png"
	// ImageDefaultImageFileName edits 的 image 缺省文件名（LiteLLM 固定用 image.png）。
	ImageDefaultImageFileName = "image.png"
)

// ImageProvider 同步图像能力（gpt-image 生成 / 编辑）。
//
// 错误语义契约：
//   - 传输/协议失败（网络不可达、超时、上游 5xx、鉴权失败、响应体不合法）→ 返回 error；
//   - 请求本身不被支持（缺 provider 能力/缺上游端点）→ 返回 ErrMediaNotSupported（可 %w 包装）；
//   - 上游返回的 4xx 参数错误 → 返回 *GatewayError（StatusCode=400，保留上游 message），
//     便于 handler 忠实透传，**不要**把它包装成 ErrMediaNotSupported。
//
// 实现者注意：请求翻译（组名已在 handler 侧改写为上游模型 ID）与鉴权都在适配器内完成。
type ImageProvider interface {
	// GenerateImage 执行 POST /v1/images/generations（JSON 同步）。
	GenerateImage(ctx context.Context, req *ImageGenerationRequest) (*ImageGenerationResponse, error)
	// EditImage 执行 POST /v1/images/edits（multipart 同步）。
	EditImage(ctx context.Context, req *ImageEditRequest) (*ImageGenerationResponse, error)
}

// ImageGenerationRequest 是 /v1/images/generations 的归一请求。
//
// 字段与 OpenAI/gpt-image 的 wire 字段同名同义，可直接 JSON 序列化发给上游。
// 注意：这里**没有** ResponseFormat / Style 字段 —— generations 不支持它们（见文件头注释）。
type ImageGenerationRequest struct {
	// Model 是**已完成「组名 → 上游模型」改写**的上游模型 ID。
	// handler 负责组调度，适配器不得再做组名解析。
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	// 以下为可选参数：nil（数值）/ 空串（字符串）表示客户端未提供，
	// 由适配器决定是否按上游默认值补齐（ImageDefault* 只是缺省参考）。
	N                 *int   `json:"n,omitempty"`
	Size              string `json:"size,omitempty"`
	Quality           string `json:"quality,omitempty"`
	Background        string `json:"background,omitempty"`
	Moderation        string `json:"moderation,omitempty"`
	OutputFormat      string `json:"output_format,omitempty"`
	OutputCompression *int   `json:"output_compression,omitempty"`
	User              string `json:"user,omitempty"`
}

// ImageEditRequest 是 /v1/images/edits 的归一请求（multipart/form-data）。
//
// handler（B3）负责解析原始 multipart 并**重建**：image[] 重复字段、mask、保留真实 filename。
// 本结构没有 json tag —— 编辑请求以 multipart 上行，字段名由适配器按上游契约拼装
// （image 多值为重复字段 `image[]`，mask 为 `mask`）。
type ImageEditRequest struct {
	// Model 同 ImageGenerationRequest.Model：已由 handler 完成组名改写。
	Model  string
	Prompt string
	// Images 是 multipart 的 image / image[] 部分，至少 1 个，**顺序必须保持**（首尾帧语义依赖顺序）。
	Images []ImageFile
	// Mask 是可选的多张蒙版（multipart 字段 mask）；缺省文件名用 ImageDefaultMaskFileName。
	Mask *ImageFile
	// 与 generations 共有的可选参数。
	N                 *int
	Size              string
	Quality           string
	Background        string
	Moderation        string
	OutputFormat      string
	OutputCompression *int
	User              string
	// edits 独有参数：input_fidelity 与 response_format（generations 不支持后者）。
	InputFidelity  string
	ResponseFormat string
}

// ImageFile 表示 multipart 请求中的一个文件部分。
//
// 适配器应自行拼装 multipart 请求体（boundary 会变，禁止把原始 boundary 透传给上游），
// 但必须保留 FieldName / FileName / ContentType，否则上游可能拒收多图或蒙版。
type ImageFile struct {
	// FieldName 是原始字段名："image" / "image[]" / "mask"。
	FieldName string
	// FileName 是客户端真实文件名；为空时用 ImageDefaultImageFileName / ImageDefaultMaskFileName。
	FileName string
	// ContentType 是客户端声明的 MIME（上游对图片类型敏感，不要改写）。
	ContentType string
	// Data 是完整文件字节。
	Data []byte
}

// ImageGenerationResponse 是图像接口的归一响应。
//
// 原样回写契约（设计文档 §6.1 / B2）：gpt-image 只回 b64_json，单响可达数十 MB，
// 因此适配器**必须**把上游响应体原封不动放进 Raw；handler 在 Raw 非空时**必须**直接
// 写出这些字节（不重新序列化），仅在 Raw 为空时才回退到按下面的结构化字段序列化。
type ImageGenerationResponse struct {
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`
	Usage   *ImageUsage `json:"usage,omitempty"`
	// Raw 上游原始响应体；json:"-" 保证它永远不会被序列化进客户端响应。
	Raw []byte `json:"-"`
}

// ImageData 是单张产物的 OpenAI 形状数据。
// gpt-image 只回 B64JSON；URL 形状（dall-e 等）保留以兼容。
type ImageData struct {
	B64JSON                string          `json:"b64_json,omitempty"`
	URL                    string          `json:"url,omitempty"`
	RevisedPrompt          string          `json:"revised_prompt,omitempty"`
	ProviderSpecificFields json.RawMessage `json:"provider_specific_fields,omitempty"`
}

// ImageUsage 是图像生成的用量（设计文档 §3.3 的 ImageUsage）。
//
// 计费口径（A1/B6）：优先用 OutputTokens 等令牌口径；缺失时退化为
// len(Data) × per_image 单价。InputTokensDetails 对应上游 input_tokens_details，
// 归一后可映射为 prompt_tokens_details（缺字段补 0）。
type ImageUsage struct {
	InputTokens        int                      `json:"input_tokens,omitempty"`
	OutputTokens       int                      `json:"output_tokens,omitempty"`
	TotalTokens        int                      `json:"total_tokens,omitempty"`
	InputTokensDetails *ImageInputTokensDetails `json:"input_tokens_details,omitempty"`
}

// ImageInputTokensDetails 是图像输入令牌的细分（text/image 两类）。
type ImageInputTokensDetails struct {
	ImageTokens int `json:"image_tokens,omitempty"`
	TextTokens  int `json:"text_tokens,omitempty"`
}
