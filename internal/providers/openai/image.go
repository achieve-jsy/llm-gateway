package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// 本文件为 OpenAI 兼容渠道补上「同步图像」可选能力（Batch 1 · B1）。
//
// 契约见 internal/core/image.go：ImageProvider 是**可选能力接口**，不扩展 core.Provider。
// 因此这里的实现方式是「能力注入」：
//
//   - *CompatibleProvider 实现 GenerateImage / EditImage → 匿名内嵌它的 provider
//     （openai、azure、openrouter…）通过方法提升**自动获得**该能力；
//   - 用**命名字段**持有 *CompatibleProvider 的 provider（litellm、vllm、minimax、
//     oracle、cohere、bailian）无法靠提升获得，需各自显式委派三行（见各包 image.go）；
//   - 两者都不满足的 provider（gemini / anthropic / bedrock / deepseek / groq / xai /
//     vertex / ollama / seedance…）**不实现**该接口 → handler 的类型断言失败，
//     映射为 HTTP 501 not_implemented（core.ErrMediaNotSupported 的语义）。
//
// 为什么不在 CompatibleProvider 内部按 providerName 白名单决定「支不支持」：
// OpenAI 兼容渠道是否提供 /images/* 由**上游部署**决定（自建 LiteLLM/vLLM/New API
// 通常会转发），网关无法静态判定；硬编码白名单会把「上游支持、网关不支持」变成
// 静默 501。这里一律转发，由上游返回它自己的 4xx/5xx，handler 再忠实透传出去。

const (
	// imageGenerationsEndpoint 与 imageEditsEndpoint 是 OpenAI Images 的两个同步端点。
	imageGenerationsEndpoint = "/images/generations"
	imageEditsEndpoint       = "/images/edits"
	// imageUploadFieldName 是 edits 的图片字段名。设计文档 §3.3：多图为**重复字段**
	// image[]（LiteLLM 亦兼容单值 image）；这里保留客户端原始字段名，只在缺失时兜底。
	imageUploadFieldName   = "image"
	imageUploadArrayName   = "image[]"
	imageMaskFieldName     = "mask"
	imageFormFieldMaxBytes = 1 << 20
)

// GenerateImage 执行 POST {base}/images/generations（gpt-image 同步生成）。
//
// 请求以 RawBody 发出并带 NoRetry：图像生成没有幂等键，重发会在上游**再出一张图并
// 重复计费**（A6 出站策略；llmclient 的 Request.NoRetry 与 WithNoRetry(ctx) 等价）。
//
// 组名 → 上游模型的改写由 handler（B2）完成，本方法只负责把归一请求翻译成上游 wire 格式。
func (p *CompatibleProvider) GenerateImage(ctx context.Context, req *core.ImageGenerationRequest) (*core.ImageGenerationResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("image generation request is required", nil)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, core.NewInvalidRequestError("model is required", nil).WithParam("model")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, core.NewInvalidRequestError("prompt is required", nil).WithParam("prompt")
	}

	body, err := json.Marshal(imageGenerationBody(req))
	if err != nil {
		return nil, core.NewInvalidRequestError("failed to marshal image generation request: "+err.Error(), err)
	}
	return p.doImage(ctx, imageGenerationsEndpoint, body, "")
}

// EditImage 执行 POST {base}/images/edits（multipart 同步编辑）。
//
// multipart **重建**（B3）：boundary 一定变化（不复用入站 boundary），但字段顺序、
// image[] 的重复次数与顺序、mask 以及**每个文件的真实 filename / Content-Type** 必须保留
// ——上游对多图顺序（首尾帧语义）与文件名敏感。
//
// 注意：入站 multipart 里除 model 之外的值**原样重建**；model 由 handler 改写为上游模型 ID。
func (p *CompatibleProvider) EditImage(ctx context.Context, req *core.ImageEditRequest) (*core.ImageGenerationResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("image edit request is required", nil)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, core.NewInvalidRequestError("model is required", nil).WithParam("model")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, core.NewInvalidRequestError("prompt is required", nil).WithParam("prompt")
	}
	if len(req.Images) == 0 {
		return nil, core.NewInvalidRequestError("at least one image is required for image edits", nil).WithParam("image")
	}

	body, contentType, err := buildImageEditMultipart(req)
	if err != nil {
		return nil, err
	}
	return p.doImage(ctx, imageEditsEndpoint, body, contentType)
}

// doImage 是所有图像请求的统一出站路径：RawBody + 显式 Content-Type + 禁重试。
//
// contentType 为空表示 JSON（由 llmclient 的 Request.Body 语义之外自行序列化的字节，
// 需要显式声明 application/json）。
func (p *CompatibleProvider) doImage(ctx context.Context, endpoint string, body []byte, contentType string) (*core.ImageGenerationResponse, error) {
	if contentType == "" {
		contentType = "application/json"
	}
	headers := http.Header{}
	headers.Set("Content-Type", contentType)

	resp, err := p.client.DoRaw(ctx, p.prepareRequest(llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: endpoint,
		RawBody:  body,
		Headers:  headers,
		// 提交类非幂等请求：429/5xx/网络错误都不重发（A6）。
		NoRetry: true,
	}))
	if err != nil {
		return nil, err
	}

	out := &core.ImageGenerationResponse{Raw: resp.Body}
	// Raw 是**原样回写**的事实源（handler 必须直接写这串字节，禁止重新序列化：
	// b64 单响可达数十 MB）。这里额外解析出结构化字段只为 B6 算用量，
	// 解析失败不影响转发（Raw 仍然完整）。
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			out.Created = 0
			out.Data = nil
			out.Usage = nil
		}
	}
	return out, nil
}

// imageGenerationBody 把归一请求翻译成上游 JSON 字段。
//
// 只输出 core.ImageGenerationRequest 里声明过的字段（**不含** response_format / style /
// seed —— 那些由 handler 在更早的阶段以 400 拒绝，不做静默丢弃）。
func imageGenerationBody(req *core.ImageGenerationRequest) map[string]any {
	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
	}
	if req.N != nil {
		body["n"] = *req.N
	}
	setIfNotEmpty(body, "size", req.Size)
	setIfNotEmpty(body, "quality", req.Quality)
	setIfNotEmpty(body, "background", req.Background)
	setIfNotEmpty(body, "moderation", req.Moderation)
	setIfNotEmpty(body, "output_format", req.OutputFormat)
	setIfNotEmpty(body, "user", req.User)
	if req.OutputCompression != nil {
		body["output_compression"] = *req.OutputCompression
	}
	return body
}

// buildImageEditMultipart 重建 edits 的 multipart 请求体。
//
// 返回的 contentType 带**新** boundary（不复用入站 boundary）；文件顺序与 filename 保持。
func buildImageEditMultipart(req *core.ImageEditRequest) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model", req.Model); err != nil {
		return nil, "", imageMultipartError(err)
	}
	if err := writer.WriteField("prompt", req.Prompt); err != nil {
		return nil, "", imageMultipartError(err)
	}
	writeOptionalIntField(writer, "n", req.N)
	writeOptionalIntField(writer, "output_compression", req.OutputCompression)
	writeOptionalStringField(writer, "size", req.Size)
	writeOptionalStringField(writer, "quality", req.Quality)
	writeOptionalStringField(writer, "background", req.Background)
	writeOptionalStringField(writer, "moderation", req.Moderation)
	writeOptionalStringField(writer, "output_format", req.OutputFormat)
	writeOptionalStringField(writer, "user", req.User)
	writeOptionalStringField(writer, "input_fidelity", req.InputFidelity)
	writeOptionalStringField(writer, "response_format", req.ResponseFormat)

	for i := range req.Images {
		if err := writeImageFilePart(writer, req.Images[i], imageUploadFieldName, core.ImageDefaultImageFileName); err != nil {
			return nil, "", err
		}
	}
	if req.Mask != nil {
		if err := writeImageFilePart(writer, *req.Mask, imageMaskFieldName, core.ImageDefaultMaskFileName); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", imageMultipartError(err)
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}

// writeImageFilePart 写一个文件部分，保留客户端原始字段名与 filename。
func writeImageFilePart(writer *multipart.Writer, file core.ImageFile, defaultField, defaultFileName string) error {
	fieldName := strings.TrimSpace(file.FieldName)
	if fieldName == "" {
		fieldName = defaultField
	}
	fileName := strings.TrimSpace(file.FileName)
	if fileName == "" {
		fileName = defaultFileName
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipartFileDisposition(fieldName, fileName))
	if ct := strings.TrimSpace(file.ContentType); ct != "" {
		header.Set("Content-Type", ct)
	} else {
		header.Set("Content-Type", "application/octet-stream")
	}

	part, err := writer.CreatePart(header)
	if err != nil {
		return imageMultipartError(err)
	}
	if _, err := part.Write(file.Data); err != nil {
		return imageMultipartError(err)
	}
	return nil
}

// multipartFileDisposition 生成文件部分的 Content-Disposition。
// 用 multipart 包内部的转义规则（Go 标准库的 escapeQuotes 语义）手工拼装，
// 以便 filename 里的引号/反斜杠不会破坏 header。
func multipartFileDisposition(fieldName, fileName string) string {
	quoted := strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(fileName)
	return `form-data; name="` + fieldName + `"; filename="` + quoted + `"`
}

func writeOptionalStringField(writer *multipart.Writer, name, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	_ = writer.WriteField(name, value)
}

func writeOptionalIntField(writer *multipart.Writer, name string, value *int) {
	if value == nil {
		return
	}
	_ = writer.WriteField(name, strconv.Itoa(*value))
}

func setIfNotEmpty(body map[string]any, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	body[key] = value
}

func imageMultipartError(err error) error {
	return core.NewInvalidRequestError("failed to build image edit multipart body: "+err.Error(), err)
}
