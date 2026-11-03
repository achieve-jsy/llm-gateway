package vllm

import (
	"context"

	"github.com/example/llm-gateway/internal/core"
)

// 本文件让 vLLM provider 满足 core.ImageProvider（Batch 1 · B1）。
//
// 为什么需要显式委派：vLLM 用**命名字段**「compatible *openai.CompatibleProvider」复用
// OpenAI 兼容实现（而不是匿名内嵌），Go 的方法提升不会跨越命名字段发生 —— 不写这三行，
// 图像请求会在 handler 的类型断言处退化成 501 not_implemented。
//
// 语义与内嵌形态完全一致：请求翻译、multipart 重建、Raw 原样保留、NoRetry 都由
// *openai.CompatibleProvider 负责（见 internal/providers/openai/image.go）。

// GenerateImage 执行 POST /images/generations。
func (p *Provider) GenerateImage(ctx context.Context, req *core.ImageGenerationRequest) (*core.ImageGenerationResponse, error) {
	return p.compatible.GenerateImage(ctx, req)
}

// EditImage 执行 POST /images/edits（multipart）。
func (p *Provider) EditImage(ctx context.Context, req *core.ImageEditRequest) (*core.ImageGenerationResponse, error) {
	return p.compatible.EditImage(ctx, req)
}
