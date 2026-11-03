package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
)

// 本文件实现「同步图像链路」（Batch 1 · B2/B3/B4/B5/B6）：
//
//	POST /v1/images/generations  （application/json）
//	POST /v1/images/edits        （multipart/form-data，重建）
//
// 处理流程（设计文档 §6.3 的创建时序，同步版）：
//
//	鉴权（v1 组中间件）→ 解析 body 取 model → 直连模型 ID → 403 direct_model_not_allowed
//	→ 组调度（组名 → 上游模型 + 渠道类型，复用 deps.GroupDispatch）
//	→ middleware.EnsureMediaPricing（A2 闸门：无价立即 402，**必须在调上游之前**）
//	→ 适配器（core.ImageProvider；未实现 → 501）→ **原样回写上游响应字节** → 写媒体用量埋点
//
// 关键约束（逐条对应任务书）：
//
//   - **响应原样回写**（B2）：gpt-image 单响的 data[].b64_json 可达数十 MB，
//     禁止重新序列化。只有 Raw 为空（上游没有返回正文）时才退化为结构化序列化。
//   - **multipart 重建**（B3）：boundary 一定变化，但字段顺序、image[] 重复次数与顺序、
//     mask、真实 filename 全部保留；只改写 model 字段。
//   - **日志策略**（B4）：请求体绝不进入本 handler 的日志（base64 不入日志）；
//     multipart 正文不落 call_traces；trace 路径白名单在 TraceCapture 里不含
//     /v1/images/*（请求体根本不采集），DEBUG 级 RequestBodyLogger 也对本路径跳过。
//   - **错误语义**（B5）：ImageGenerationRequest 类型层没有 response_format / style / seed，
//     收到即 400 并给出替代建议（output_format）；上游 4xx 忠实透传、5xx/网络 → 502；
//     401 区分「客户端 Key 无效」（本仓 401）与「上游凭证无效」（透传上游 401）。
//   - **计价**（B6）：唯一接线点是组调度之后、调上游之前的 middleware.EnsureMediaPricing；
//     用量写入 obs.CtxKeyMediaUsage（**精确类型** core.MediaCostInput，禁止别名/指针）。

const (
	// imageJSONBodyMaxBytes 是 generations JSON 请求体的读取上限。
	// 图像请求体本身不可能很大（没有入站图片字节），10MB 是宽松上界；
	// 真正的「大」在**响应**侧（b64），由网关原样回写。
	imageJSONBodyMaxBytes int64 = 10 << 20
	// imageMaxFileBytes 是 edits multipart 中**单个文件 part** 的内存上限。
	// 取 32MB：OpenAI Images edits 允许单张图最大 25MB（png/webp/jpg），
	// 旧的 8MB 会误拒合法请求；同时仍是有界上限，不会把内存打爆。
	imageMaxFileBytes int64 = 32 << 20
	// imageMaxTotalBytes 是 edits 整个请求（所有 part 之和）的内存上限。
	// 一次 edits 最多 16 张图，单张 32MB 时理论峰值 512MB——过大会被 OOM 杀掉，
	// 因此总上限收紧到 128MB（远超正常用法，同时给恶意请求一个硬边界）。
	imageMaxTotalBytes int64 = 128 << 20
	// 图像 part 的字段名（与上游 OpenAI Images edits 的约定一致，也是 provider 侧
	// 重建 multipart 时使用的名字；handler 只做识别，不改写字段名）。
	imageFieldName      = "image"
	imageArrayFieldName = "image[]"
	imageMaskFieldName  = "mask"
	// imageMaxFieldBytes 是 edits multipart 中**单个文本字段**的上限。
	// 文本字段只有 prompt 可能偏长（模型侧本身也有长度限制），1MB 足够。
	imageMaxFieldBytes int64 = 1 << 20
	// imageUpstreamTimeout 是同步图像调用的默认超时。上游生成一张图通常 10–60s；
	// 不做长等待（真正的长耗时任务属于异步视频链路）。
	imageUpstreamTimeout = 120 * time.Second
)

// 本文件对外暴露几个**纯函数**供外部测试包（tests/，防护栏 Q3 要求测试物理分离）
// 直接断言：参数白名单判定、multipart 解析结果、媒体计价桶口径。
// 它们都是无副作用的逻辑函数，不构成额外 API 面。

// ValidateImageGenerationParams 判定 generations JSON 正文里是否携带了类型层禁用的参数。
// 返回 nil 表示可以继续；非 nil 时调用方应返回 400（见 WriteUnsupportedImageParams）。
func ValidateImageGenerationParams(body []byte) []UnsupportedImageParam {
	return detectUnsupportedGenerationParams(body)
}

// ValidateImageEditParams 判定 edits 表单里是否携带了类型层禁用的参数。
func ValidateImageEditParams(fields map[string]string) []UnsupportedImageParam {
	return detectUnsupportedEditParams(fields)
}

// WriteUnsupportedImageParams 写 400 + 替代建议（供测试直接调用）。
func WriteUnsupportedImageParams(c *gin.Context, unsupported []UnsupportedImageParam) {
	writeImageUnsupportedParams(c, unsupported)
}

// UnsupportedImageParam 描述一个「类型层故意不存在」的参数及其替代建议。
type UnsupportedImageParam struct {
	// Name 是客户端传入的字段名（如 response_format）。
	Name string
	// Replacement 是替代字段名（如 output_format）；为空表示无等价替代。
	Replacement string
}

// ParseImageEditForm 解析 edits 的 multipart 请求（保序），供路由/handler 与测试共用。
func ParseImageEditForm(r *http.Request) (*ImageEditForm, error) {
	form, err := parseImageEditForm(r)
	if err != nil {
		return nil, err
	}
	return &ImageEditForm{form: form}, nil
}

// ImageEditForm 是解析后的 edits 表单：保序 part 列表 + 文本字段索引。
type ImageEditForm struct {
	form *imageEditForm
}

// Model 返回表单里的 model 原始值（**未经**组名改写）。
func (f *ImageEditForm) Model() string {
	if f == nil || f.form == nil {
		return ""
	}
	return f.form.model
}

// Prompt 返回 prompt 文本。
func (f *ImageEditForm) Prompt() string {
	if f == nil || f.form == nil {
		return ""
	}
	return f.form.prompt
}

// Fields 返回首次出现的文本字段（保序语义由 Parts 承担）。
func (f *ImageEditForm) Fields() map[string]string {
	if f == nil || f.form == nil {
		return nil
	}
	return f.form.fields
}

// ToEditRequest 把表单转成归一请求（model 参数用于替换上游模型 ID）。
func (f *ImageEditForm) ToEditRequest(upstreamModel string) core.ImageEditRequest {
	if f == nil || f.form == nil {
		return core.ImageEditRequest{}
	}
	return f.form.toEditRequest(upstreamModel)
}

// ImageMediaCostInput 返回本次响应对应的媒体计价桶（B6 口径），供测试断言。
func ImageMediaCostInput(resp *core.ImageGenerationResponse) core.MediaCostInput {
	return imageMediaCostInput(resp)
}

// imageModelResolver 把「上游模型 + 渠道类型」解析为具体 provider 实例。
//
// 用最小的函数签名而不是 *providers.Router：便于测试注入假 provider，
// 也避免 handler 直接依赖 Router 的其它入口（图像请求不走 Router 的 chat 方法）。
type imageModelResolver func(model, providerType string) core.Provider

// ImageHandler 同步图像 Handler（gpt-image 生成 / 编辑）。
type ImageHandler struct {
	groupDispatch *orchestrator.GroupAwareDispatcher
	resolveModel  imageModelResolver
}

// ImageHandlerOption 是 ImageHandler 的函数式选项。
type ImageHandlerOption func(*ImageHandler)

// WithImageGroupDispatcher 注入模型组调度器（组名 → 上游模型 + 渠道类型）。
func WithImageGroupDispatcher(d *orchestrator.GroupAwareDispatcher) ImageHandlerOption {
	return func(h *ImageHandler) { h.groupDispatch = d }
}

// WithImageModelResolver 注入「模型 → provider 实例」解析器。
func WithImageModelResolver(r imageModelResolver) ImageHandlerOption {
	return func(h *ImageHandler) { h.resolveModel = r }
}

// NewImageHandler 创建图像 Handler。
func NewImageHandler(opts ...ImageHandlerOption) *ImageHandler {
	h := &ImageHandler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RouterImageModelResolver 用 *providers.Router 构造解析器（生产接线用）。
func RouterImageModelResolver(getter func() *providers.Router) imageModelResolver {
	return func(model, providerType string) core.Provider {
		if getter == nil {
			return nil
		}
		r := getter()
		if r == nil {
			return nil
		}
		return r.ProviderForModel(model, providerType)
	}
}

// ─── 入口 ────────────────────────────────────────────────────────────────────

// HandleImageGeneration 处理 POST /v1/images/generations。
func (h *ImageHandler) HandleImageGeneration(c *gin.Context) {
	body, err := readImageJSONBody(c.Request.Body)
	if err != nil {
		writeImageError(c, http.StatusBadRequest, "invalid_request_error",
			"failed to read image generation request body: "+err.Error(), "")
		return
	}

	// 参数白名单校验（B5）：先按类型层判定，绝不静默丢弃。
	if unsupported := detectUnsupportedGenerationParams(body); len(unsupported) > 0 {
		writeImageUnsupportedParams(c, unsupported)
		return
	}

	var req core.ImageGenerationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeImageError(c, http.StatusBadRequest, "invalid_request_error",
			"request body is not valid JSON: "+err.Error(), "")
		return
	}

	routed, ok := h.prepareRoutedModel(c, strings.TrimSpace(req.Model))
	if !ok {
		return
	}
	req.Model = routed.model

	provider, ok := h.resolveImageProvider(c, routed)
	if !ok {
		return
	}

	// B6：准入计价闸门必须在「拿到 routedModel/providerType、尚未调上游」之间。
	// 返回 false 时闸门已经写好 402 media_pricing_missing。
	if !ensureImagePricing(c, routed) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), imageUpstreamTimeout)
	defer cancel()
	ctx = llmclient.WithNoRetry(ctx) // 提交类请求：至多一次（A6）

	resp, err := provider.GenerateImage(ctx, &req)
	if err != nil {
		writeImageUpstreamError(c, err)
		return
	}
	writeImageResponse(c, resp)
}

// HandleImageEdit 处理 POST /v1/images/edits（multipart/form-data）。
func (h *ImageHandler) HandleImageEdit(c *gin.Context) {
	form, err := parseImageEditForm(c.Request)
	if err != nil {
		writeImageError(c, http.StatusBadRequest, "invalid_request_error", err.Error(), "")
		return
	}
	if unsupported := detectUnsupportedEditParams(form.fields); len(unsupported) > 0 {
		writeImageUnsupportedParams(c, unsupported)
		return
	}

	routed, ok := h.prepareRoutedModel(c, form.model)
	if !ok {
		return
	}

	req := form.toEditRequest(routed.model)
	if strings.TrimSpace(req.Prompt) == "" {
		writeImageError(c, http.StatusBadRequest, "invalid_request_error",
			"prompt is required for image edits", "prompt")
		return
	}
	if len(req.Images) == 0 {
		writeImageError(c, http.StatusBadRequest, "invalid_request_error",
			"at least one image part is required for image edits", "image")
		return
	}

	provider, ok := h.resolveImageProvider(c, routed)
	if !ok {
		return
	}
	if !ensureImagePricing(c, routed) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), imageUpstreamTimeout)
	defer cancel()
	ctx = llmclient.WithNoRetry(ctx)

	resp, err := provider.EditImage(ctx, &req)
	if err != nil {
		writeImageUpstreamError(c, err)
		return
	}
	writeImageResponse(c, resp)
}

// ─── 调度与解析 ───────────────────────────────────────────────────────────────

// routedImage 是「组调度之后」的图像请求上下文：模型已改写为上游模型 ID。
type routedImage struct {
	// publicModel 是客户端传来的对外模型名（模型组名），用于埋点与 call_logs。
	publicModel string
	// model 是改写后的**上游**模型 ID，发给上游的 model 字段。
	model string
	// providerName 是渠道实例名（用于 c.Set("provider") 与日志）。
	providerName string
	// providerType 是渠道类型（用于价表解析与 provider 实例解析）。
	providerType string
}

// prepareRoutedModel 执行「直连模型 ID → 403」与「组名 → 上游模型」改写，并写入
// 埋点所需的 gin context 键。
//
// 语义与 chat/embeddings 一致（复用 orchestrator.GroupAwareDispatcher）：
//   - 组名：Dispatch 返回 decision，用 decision.ModelName 作为上游模型；
//   - 非组名：Dispatch 返回 (nil, nil) 或 model.ErrDirectModelNotAllowed。
//     **图像链路只接受模型组名**（docs/media-api.md §1.2）→ 一律 403，
//     避免绕过计费与组能力聚合（也不泄露「该模型是否存在于上游」）。
func (h *ImageHandler) prepareRoutedModel(c *gin.Context, publicModel string) (routedImage, bool) {
	if publicModel == "" {
		writeImageError(c, http.StatusBadRequest, "invalid_request_error",
			"model is required and must be a model group name", "model")
		return routedImage{}, false
	}

	routed := routedImage{publicModel: publicModel, model: publicModel}
	if h.groupDispatch == nil {
		// 未启用模型组：图像链路没有可用的调度语义 → 明确拒绝，不要静默直连上游。
		slog.Error("image request rejected: group dispatch is not configured",
			"request_id", c.GetString("request_id"), "model", publicModel)
		writeImageError(c, http.StatusForbidden, "direct_model_not_allowed",
			"image endpoints only accept model group names, but model groups are not enabled", "model")
		return routedImage{}, false
	}

	decision, err := h.groupDispatch.Dispatch(c, publicModel, nil)
	if err != nil {
		// ErrDirectModelNotAllowed 在这里是**预期**结果（客户端直连模型 ID）；
		// 其它错误（组不存在 / 无可用模型 / 未授权）按 chat 的既有映射处理。
		if errors.Is(err, model.ErrDirectModelNotAllowed) {
			writeImageError(c, http.StatusForbidden, "direct_model_not_allowed",
				"image endpoints only accept model group names; direct model IDs are not allowed", "model")
			return routedImage{}, false
		}
		respondGroupImageError(c, err)
		return routedImage{}, false
	}
	if decision == nil {
		writeImageError(c, http.StatusForbidden, "direct_model_not_allowed",
			"image endpoints only accept model group names; direct model IDs are not allowed", "model")
		return routedImage{}, false
	}

	if name := strings.TrimSpace(decision.ModelName); name != "" {
		routed.model = name
	}
	if ct := strings.TrimSpace(decision.ChannelType); ct != "" {
		routed.providerType = ct
	}
	if pn := strings.TrimSpace(decision.ProviderName); pn != "" {
		routed.providerName = pn
	}

	// 埋点/call_logs：model 用**对外模型名**（组名），routed_model 用上游模型 ID。
	// 不写伪模型后缀（C 队任务书既有事实 6：本分支现状会误导读侧过滤）。
	c.Set("model", publicModel)
	if routed.model != "" {
		c.Set("routed_model", routed.model)
	}
	if routed.providerName != "" {
		c.Set("provider", routed.providerName)
	}
	return routed, true
}

// resolveImageProvider 解析 provider 实例并做 core.ImageProvider 能力断言。
// 断言失败（或解析不到实例）→ 501 not_implemented（core.ErrMediaNotSupported 语义）。
func (h *ImageHandler) resolveImageProvider(c *gin.Context, routed routedImage) (core.ImageProvider, bool) {
	if h.resolveModel == nil {
		writeImageNotImplemented(c, routed, "model resolver is not configured")
		return nil, false
	}
	provider := h.resolveModel(routed.model, routed.providerType)
	if provider == nil {
		writeImageNotImplemented(c, routed, "no provider instance resolved for the routed model")
		return nil, false
	}
	imageProvider, ok := provider.(core.ImageProvider)
	if !ok {
		writeImageNotImplemented(c, routed, "provider does not implement the synchronous image capability")
		return nil, false
	}
	return imageProvider, true
}

// ensureImagePricing 是 A2 媒体准入闸门的唯一接线点（B6 既有事实 5）。
//
// 时机：组调度拿到 routedModel/providerType 之后、**调用上游之前**。
// 「查任务/取产物」类端点不调用它；图像的两条端点都产生新用量，因此都要调用。
func ensureImagePricing(c *gin.Context, routed routedImage) bool {
	providerKey := routed.providerType
	if providerKey == "" {
		providerKey = routed.providerName
	}
	// 返回 false 时闸门已写 402 media_pricing_missing（契约见 docs/media-api.md §6）。
	return middleware.EnsureMediaPricing(c, routed.model, providerKey)
}

// ─── multipart 解析与重建（B3）───────────────────────────────────────────────

// imageEditForm 是解析后的 edits 表单：保序的原始 part 列表 + 文本字段索引。
type imageEditForm struct {
	model  string
	prompt string
	fields map[string]string
	parts  []imageFormPart
}

// imageFormPart 是一个 multipart part 的原始形态（文件或文本字段）。
type imageFormPart struct {
	name     string
	fileName string
	file     bool
	// contentType 仅文件 part 有意义（客户端声明的 MIME，原样保留）。
	contentType string
	value       string
}

// isImageFilePart 判定一个 multipart part 是否为文件。
//
// 判定规则刻意保守：**有 filename 就是文件**（所有主流客户端 curl -F /
// requests files= / openai SDK 都会带 filename）；没有 filename 时，只有
// 字段名是已知的图像字段（image / image[] / mask）才按文件处理——避免把
// 恰好带了 Content-Type 的文本字段误判成文件而破坏「只改写 model」的语义。
func isImageFilePart(name, fileName string) bool {
	if fileName != "" {
		return true
	}
	switch name {
	case imageFieldName, imageArrayFieldName, imageMaskFieldName:
		return true
	default:
		return false
	}
}

// parseImageEditForm 流式解析入站 multipart，**保序**收集 part。
//
// 为什么不用 c.MultipartForm()：需要「保序重建」——image[] 的重复次数与顺序是
// 上游语义（首尾帧），FormFile/Value 都是 map，顺序会丢。
func parseImageEditForm(r *http.Request) (*imageEditForm, error) {
	if r == nil || r.Body == nil {
		return nil, errors.New("request body is required")
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return nil, errors.New("image edits require multipart/form-data")
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("multipart boundary is missing")
	}

	reader := multipart.NewReader(r.Body, boundary)
	form := &imageEditForm{fields: make(map[string]string)}
	var totalBytes int64
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("failed to parse multipart form: " + err.Error())
		}

		name := part.FormName()
		fileName := part.FileName()
		contentType := part.Header.Get("Content-Type")
		if isImageFilePart(name, fileName) {
			// 文件 part：按上限读入内存（超出即拒绝，避免把内存打爆）。
			data, err := io.ReadAll(io.LimitReader(part, imageMaxFileBytes+1))
			_ = part.Close()
			if err != nil {
				return nil, errors.New("failed to read multipart file part: " + err.Error())
			}
			if int64(len(data)) > imageMaxFileBytes {
				return nil, errors.New("multipart file part exceeds the " +
					humanBytes(imageMaxFileBytes) + " limit")
			}
			totalBytes += int64(len(data))
			if totalBytes > imageMaxTotalBytes {
				return nil, errors.New("multipart request exceeds the " +
					humanBytes(imageMaxTotalBytes) + " total limit")
			}
			form.parts = append(form.parts, imageFormPart{
				name: name, fileName: fileName, file: true, contentType: contentType, value: string(data),
			})
			continue
		}

		data, err := io.ReadAll(io.LimitReader(part, imageMaxFieldBytes+1))
		_ = part.Close()
		if err != nil {
			return nil, errors.New("failed to read multipart field: " + err.Error())
		}
		if int64(len(data)) > imageMaxFieldBytes {
			return nil, errors.New("multipart field exceeds the " +
				humanBytes(imageMaxFieldBytes) + " limit")
		}
		value := string(data)
		form.parts = append(form.parts, imageFormPart{name: name, value: value})
		if name == "" {
			continue
		}
		if _, exists := form.fields[name]; !exists {
			form.fields[name] = value
		}
		switch name {
		case "model":
			form.model = strings.TrimSpace(value)
		case "prompt":
			form.prompt = value
		}
	}
	return form, nil
}

// toEditRequest 把解析后的表单转成归一请求（model 已由调用方改写为上游模型 ID）。
func (f *imageEditForm) toEditRequest(upstreamModel string) core.ImageEditRequest {
	req := core.ImageEditRequest{
		Model:          upstreamModel,
		Prompt:         f.prompt,
		Size:           f.fields["size"],
		Quality:        f.fields["quality"],
		Background:     f.fields["background"],
		Moderation:     f.fields["moderation"],
		OutputFormat:   f.fields["output_format"],
		User:           f.fields["user"],
		InputFidelity:  f.fields["input_fidelity"],
		ResponseFormat: f.fields["response_format"],
	}
	req.N = parseOptionalInt(f.fields["n"])
	req.OutputCompression = parseOptionalInt(f.fields["output_compression"])

	for _, part := range f.parts {
		if !part.file {
			continue
		}
		file := core.ImageFile{
			FieldName:   part.name,
			FileName:    part.fileName,
			ContentType: part.contentType,
			Data:        []byte(part.value),
		}
		// mask 只在**确实叫 mask** 时进 Mask；其余文件 part（image / image[] / 其它别名）
		// 一律进 Images 且**保持出现顺序**。
		if part.name == "mask" && req.Mask == nil {
			mask := file
			req.Mask = &mask
			continue
		}
		req.Images = append(req.Images, file)
	}
	return req
}

// ─── 不支持的参数（B5）────────────────────────────────────────────────────────

// detectUnsupportedGenerationParams 检查 generations 的类型层禁用参数。
//
// core.ImageGenerationRequest **没有** response_format / style / seed 字段 —— 这不是疏漏，
// 而是照设计文档 §3.3 的 gpt-image 支持矩阵：generations 不接受 response_format（那是
// dall-e 的形状）、style 仅 dall-e-3、seed 不支持。因为结构体没有字段，json.Unmarshal
// 会**静默忽略**它们；本函数在反序列化前显式探测，把「静默丢弃」改成 400 + 替代建议
// （设计文档 §8.4：默认严格）。
//
// 注意：null 值不算「传了参数」（客户端 SDK 常把未设置的字段序列化成 null）。
func detectUnsupportedGenerationParams(body []byte) []UnsupportedImageParam {
	raw, ok := decodeRawObject(body)
	if !ok {
		return nil
	}
	return collectUnsupported(raw, false)
}

// detectUnsupportedEditParams 检查 edits 的禁用参数。
//
// edits **支持** response_format（设计文档 §3.3：edits 额外支持 response_format），
// 因此只拒绝 style / seed。
func detectUnsupportedEditParams(fields map[string]string) []UnsupportedImageParam {
	var out []UnsupportedImageParam
	for _, name := range []string{"style", "seed"} {
		if _, exists := fields[name]; exists {
			out = append(out, UnsupportedImageParam{Name: name})
		}
	}
	return out
}

func collectUnsupported(raw map[string]json.RawMessage, allowResponseFormat bool) []UnsupportedImageParam {
	candidates := []UnsupportedImageParam{
		{Name: "response_format", Replacement: "output_format"},
		{Name: "style"},
		{Name: "seed"},
	}
	var out []UnsupportedImageParam
	for _, candidate := range candidates {
		if candidate.Name == "response_format" && allowResponseFormat {
			continue
		}
		value, exists := raw[candidate.Name]
		if !exists || isJSONNull(value) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func decodeRawObject(body []byte) (map[string]json.RawMessage, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, false
	}
	return raw, true
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// writeImageUnsupportedParams 返回 400 + 每个不支持参数的具体替代建议。
func writeImageUnsupportedParams(c *gin.Context, unsupported []UnsupportedImageParam) {
	parts := make([]string, 0, len(unsupported))
	param := unsupported[0].Name
	for _, item := range unsupported {
		if item.Replacement != "" {
			parts = append(parts, item.Name+" is not supported by this endpoint, use "+item.Replacement+" instead")
			continue
		}
		parts = append(parts, item.Name+" is not supported by this endpoint")
	}
	message := "unsupported parameter(s): " + strings.Join(parts, "; ")
	if len(unsupported) == 1 && unsupported[0].Name == "response_format" {
		// 最常见的误用单独给出完整说明（对接方最容易踩，LiteLLM 上会 500）。
		message = "response_format is not supported by /v1/images/generations: the model only returns b64_json; " +
			"use output_format instead (e.g. output_format=png)"
	}
	writeImageError(c, http.StatusBadRequest, "invalid_request_error", message, param)
}

// ─── 响应与错误 ───────────────────────────────────────────────────────────────

// writeImageResponse **原样回写**上游响应字节（B2 的硬要求）。
//
// 只有 Raw 为空时才退化为结构化序列化（正常上游一定会返回正文，这条分支只为
// 「上游 200 但空 body」的异常情形兜底，保证不会 panic 或返回空响应）。
func writeImageResponse(c *gin.Context, resp *core.ImageGenerationResponse) {
	if resp == nil {
		writeImageError(c, http.StatusBadGateway, "upstream_error",
			"upstream returned an empty image response", "")
		return
	}
	// B6：先把媒体用量交给采集器（无价时由准入闸门在调上游前就拒了，
	// 这里只负责「用得对」——精确类型、只在成功路径写）。
	applyImageMediaUsage(c, resp)
	if len(resp.Raw) > 0 {
		// 不 Unmarshal、不 Marshal、不 sanitize：字节级透传。
		c.Data(http.StatusOK, imageResponseContentType(), resp.Raw)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// imageResponseContentType 固定为 application/json：OpenAI Images 的两个端点都返回 JSON
// （b64_json / url 都在 JSON 里）。刻意不看上游 Content-Type、也刻意不解析 body
// ——解析就是重新序列化的第一步，而 b64 响应可能是数十 MB。
func imageResponseContentType() string {
	return "application/json; charset=utf-8"
}

// writeImageNotImplemented 返回 501 not_implemented（core.ErrMediaNotSupported 的 HTTP 映射）。
func writeImageNotImplemented(c *gin.Context, routed routedImage, detail string) {
	slog.WarnContext(c.Request.Context(), "image capability not implemented by provider",
		"request_id", c.GetString("request_id"),
		"model", routed.model,
		"provider_type", routed.providerType,
		"detail", detail,
	)
	writeImageError(c, http.StatusNotImplemented, "not_implemented",
		"this provider does not implement the synchronous image capability", "model")
}

// writeImageUpstreamError 把上游错误映射为对外错误（B5）。
//
//	上游 4xx  → 忠实透传状态码 + 上游 JSON 错误体（参数错误是调用方问题）
//	上游 401  → 透传 401（上游凭证问题；客户端 Key 的 401 由鉴权中间件在更早阶段返回）
//	上游 5xx / 网络 / 超时 / 协议错误 → 502 upstream_error
//
// context 取消/超时单独归一为 504/499 语义（沿用 chat 的既有映射习惯）。
func writeImageUpstreamError(c *gin.Context, err error) {
	if errors.Is(err, core.ErrMediaNotSupported) {
		writeImageError(c, http.StatusNotImplemented, "not_implemented",
			"this provider does not implement the synchronous image capability", "model")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeImageError(c, http.StatusGatewayTimeout, "upstream_timeout",
			"upstream image generation timed out", "")
		return
	}

	var gwErr *core.GatewayError
	if errors.As(err, &gwErr) {
		status := gwErr.HTTPStatusCode()
		c.Set("upstream_status", status)
		// 上游 4xx：原样透传（含 400 参数错误与 401 凭证错误的语义差异）。
		if status >= 400 && status < 500 {
			if len(gwErr.Body) > 0 {
				c.Data(status, "application/json; charset=utf-8", gwErr.Body)
				return
			}
			writeImageErrorCode(c, status, imageErrorTypeForStatus(status), gwErr.Message,
				imageParamName(gwErr), imageErrorCode(gwErr))
			return
		}
		// 上游 5xx：网关主动重映射为 502（上游不可达 / 响应非法，契约 docs/media-api.md §6）。
		writeImageError(c, http.StatusBadGateway, "upstream_error",
			"upstream image service returned an error: "+gwErr.Message, imageParamName(gwErr))
		return
	}

	writeImageError(c, http.StatusBadGateway, "upstream_error",
		"failed to call upstream image service: "+err.Error(), "")
}

func imageErrorTypeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusNotFound:
		return "not_found_error"
	default:
		return "invalid_request_error"
	}
}

func imageParamName(err *core.GatewayError) string {
	if err == nil || err.Param == nil {
		return ""
	}
	return *err.Param
}

func imageErrorCode(err *core.GatewayError) string {
	if err == nil || err.Code == nil {
		return ""
	}
	return *err.Code
}

// respondGroupImageError 复用 chat 的组错误映射（组不存在 404 / 无可用模型 503 / 未授权 403），
// 但**不**走 respondError 的 passthrough 分支：图像链路固定回 OpenAI 形状错误信封。
func respondGroupImageError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrDirectModelNotAllowed) {
		writeImageError(c, http.StatusForbidden, "direct_model_not_allowed",
			"image endpoints only accept model group names; direct model IDs are not allowed", "model")
		return
	}
	var ue *model.UnifiedError
	if errors.As(err, &ue) {
		writeImageErrorCode(c, ue.HTTPStatus, string(ue.Type), ue.Message, "", ue.Code)
		return
	}
	if errors.Is(err, model.ErrModelGroupUnavailable) {
		writeImageErrorCode(c, http.StatusServiceUnavailable, "model_unavailable",
			"the requested model group is temporarily unavailable", "", "model_unavailable")
		return
	}
	writeImageError(c, http.StatusInternalServerError, "internal_error", err.Error(), "")
}

// writeImageError 写 OpenAI 形状错误信封（docs/media-api.md §6）。
//
// 信封字段固定为 type / message / param / code：param 为空输出 null
// （契约要求字段存在且可为 null），code 缺省退化为 type。
func writeImageError(c *gin.Context, status int, errType, message, param string) {
	writeImageErrorCode(c, status, errType, message, param, "")
}

// writeImageErrorCode 与 writeImageError 相同，但允许覆盖 machine-readable code
// （例如上游返回的 invalid_size 要原样透传，而不是退化成 type）。
func writeImageErrorCode(c *gin.Context, status int, errType, message, param, code string) {
	if c.IsAborted() && c.Writer.Written() {
		return
	}
	if strings.TrimSpace(code) == "" {
		code = errType
	}
	errObj := gin.H{
		"type":    errType,
		"message": message,
		"param":   nil,
		"code":    code,
	}
	if strings.TrimSpace(param) != "" {
		errObj["param"] = param
	}
	c.AbortWithStatusJSON(status, gin.H{"error": errObj})
}

// ─── 用量与埋点（B6/B4）──────────────────────────────────────────────────────

// applyImageMediaUsage 把本次图像用量写入 gin context，供采集器按媒体口径计价。
//
// 口径（设计文档 §8.1 / core.ImageUsage 注释）：
//
//	优先 image token：usage.output_tokens / usage.input_tokens（gpt-image 系）；
//	退化按张：len(data) × per_image（并把 Requests 记为 1，兼容按次计价的模型）。
//
// ⚠️ 类型必须是 **core.MediaCostInput 值类型**（不是别名、不是指针）：
// observability 的采集器对该类型做精确类型断言，传别名/指针会静默丢失用量 → 静默 0 元。
func applyImageMediaUsage(c *gin.Context, resp *core.ImageGenerationResponse) {
	if c == nil || resp == nil {
		return
	}
	c.Set(gwobs.CtxKeyMediaUsage, imageMediaCostInput(resp))
}

// imageMediaCostInput 计算媒体计价桶。导出给测试直接断言口径。
func imageMediaCostInput(resp *core.ImageGenerationResponse) core.MediaCostInput {
	in := core.MediaCostInput{}
	if resp == nil {
		return in
	}
	if usage := resp.Usage; usage != nil && usage.TotalTokens > 0 {
		in.InputImageTokens = usage.InputTokens
		in.OutputImageTokens = usage.OutputTokens
		if details := usage.InputTokensDetails; details != nil {
			in.InputImageTokens = details.ImageTokens
		}
		return in
	}
	in.Images = len(resp.Data)
	if in.Images > 0 {
		in.Requests = 1
	}
	return in
}

// ─── 小工具 ───────────────────────────────────────────────────────────────────

func readImageJSONBody(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, errors.New("request body is required")
	}
	raw, err := io.ReadAll(io.LimitReader(body, imageJSONBodyMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > imageJSONBodyMaxBytes {
		return nil, errors.New("request body exceeds the " + humanBytes(imageJSONBodyMaxBytes) + " limit")
	}
	return raw, nil
}

// humanBytes 用于错误文案（如「exceeds the 8MB limit」）。
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatInt(n>>20, 10) + "MB"
	case n >= 1<<10:
		return strconv.FormatInt(n>>10, 10) + "KB"
	default:
		return strconv.FormatInt(n, 10) + "B"
	}
}

// parseOptionalInt 解析可选整数字段；非法值按「未提供」处理（不阻断请求，
// 与 OpenAI 对可选参数的宽容语义一致）。
func parseOptionalInt(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &v
}
