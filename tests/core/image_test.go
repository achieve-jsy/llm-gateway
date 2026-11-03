// Package core_test 测试 internal/core 的同步图像契约类型（ImageProvider 及其请求/响应）。
// 重点锁定两条容易踩坑的契约：generations 不支持 response_format；Raw 字节不得进入 JSON。
package core_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeImageProvider 实现 core.ImageProvider，用于编译期校验接口形状。
type fakeImageProvider struct{}

func (f *fakeImageProvider) GenerateImage(_ context.Context, _ *core.ImageGenerationRequest) (*core.ImageGenerationResponse, error) {
	return &core.ImageGenerationResponse{}, nil
}

func (f *fakeImageProvider) EditImage(_ context.Context, _ *core.ImageEditRequest) (*core.ImageGenerationResponse, error) {
	return &core.ImageGenerationResponse{}, nil
}

// 编译期断言：适配器（providers/<vendor>/）实现的是可选接口，不扩展 core.Provider。
var _ core.ImageProvider = (*fakeImageProvider)(nil)

// 测试目的：generations 请求序列化后**不得**出现 response_format / style / seed
// —— gpt-image 的 generations 不支持它们，结构体里没有字段才能从类型层面挡住误传。
func TestImageGenerationRequest_WireFields(t *testing.T) {
	n := 2
	compression := 80
	req := &core.ImageGenerationRequest{
		Model:             "gpt-image-group",
		Prompt:            "a cat",
		N:                 &n,
		Size:              "1024x1024",
		Quality:           "high",
		Background:        "opaque",
		Moderation:        "low",
		OutputFormat:      "png",
		OutputCompression: &compression,
		User:              "u-1",
	}

	raw, err := json.Marshal(req)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	tests := []struct {
		name        string
		description string
		key         string
		wantPresent bool
	}{
		{name: "model", description: "model 是必填字段，必须序列化", key: "model", wantPresent: true},
		{name: "prompt", description: "prompt 是必填字段，必须序列化", key: "prompt", wantPresent: true},
		{name: "output_format", description: "gpt-image 用 output_format，必须序列化", key: "output_format", wantPresent: true},
		{name: "output_compression", description: "output_compression 为 0 以外的值时必须序列化", key: "output_compression", wantPresent: true},
		{name: "response_format", description: "generations 不支持 response_format（edits 才支持），结构体不得含该字段", key: "response_format", wantPresent: false},
		{name: "style", description: "style 仅 dall-e-3 支持，结构体不得含该字段", key: "style", wantPresent: false},
		{name: "seed", description: "seed 不受支持，结构体不得含该字段", key: "seed", wantPresent: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, present := got[tt.key]
			assert.Equal(t, tt.wantPresent, present, tt.description)
		})
	}
}

// 测试目的：未提供的可选参数必须被 omitempty 省略（不能被序列化成 0 / ""，否则上游会当成显式入参）。
func TestImageGenerationRequest_OmitEmpty(t *testing.T) {
	raw, err := json.Marshal(&core.ImageGenerationRequest{Model: "m", Prompt: "p"})
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	assert.Equal(t, map[string]any{"model": "m", "prompt": "p"}, got,
		"只提供必填字段时，序列化结果必须恰好是 model + prompt（可选参数全部 omitempty）")
	assert.Nil(t, got["n"], "n 未提供时不得序列化为 0")
	assert.Nil(t, got["output_compression"], "output_compression 未提供时不得序列化为 0")
}

// 测试目的：edits 请求必须能携带 mask/image[] 与 edits 独有参数（input_fidelity / response_format）。
func TestImageEditRequest_MultipartInputs(t *testing.T) {
	mask := &core.ImageFile{
		FieldName:   "mask",
		FileName:    "mask.png",
		ContentType: "image/png",
		Data:        []byte{0x89, 'P', 'N', 'G'},
	}
	req := &core.ImageEditRequest{
		Model:  "gpt-image-group",
		Prompt: "add a hat",
		Images: []core.ImageFile{
			{FieldName: "image[]", FileName: "first.png", ContentType: "image/png", Data: []byte{1}},
			{FieldName: "image[]", FileName: "last.png", ContentType: "image/png", Data: []byte{2}},
		},
		Mask:           mask,
		InputFidelity:  "high",
		ResponseFormat: "b64_json",
	}

	require.Len(t, req.Images, 2)
	assert.Equal(t, "first.png", req.Images[0].FileName, "多图顺序即首尾帧语义，必须保序")
	assert.Equal(t, "last.png", req.Images[1].FileName)
	require.NotNil(t, req.Mask)
	assert.Equal(t, core.ImageDefaultMaskFileName, req.Mask.FileName, "mask 缺省文件名必须是 mask.png")
	assert.Equal(t, "high", req.InputFidelity, "edits 支持 input_fidelity")
	assert.Equal(t, "b64_json", req.ResponseFormat, "edits 支持 response_format（generations 不支持）")
}

// 测试目的：Raw（上游原始响应体）永远不得进入客户端 JSON —— 否则会把整份 b64 重复放大一倍。
func TestImageGenerationResponse_RawIsNeverSerialized(t *testing.T) {
	resp := &core.ImageGenerationResponse{
		Created: 1700000000,
		Data: []core.ImageData{
			{B64JSON: "aGVsbG8=", RevisedPrompt: "revised"},
		},
		Usage: &core.ImageUsage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
			InputTokensDetails: &core.ImageInputTokensDetails{
				ImageTokens: 8,
				TextTokens:  2,
			},
		},
		Raw: []byte(`{"poison":"MUST_NOT_LEAK"}`),
	}

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	_, present := got["Raw"]
	assert.False(t, present, "Raw 带 json:\"-\" 标签，绝不能出现在 JSON 里")
	assert.NotContains(t, string(raw), "MUST_NOT_LEAK",
		"Raw 的字节不得出现在结构化序列化结果中")

	assert.Contains(t, got, "usage", "usage 存在时必须序列化（A1/B6 计价依赖它）")
	usage, ok := got["usage"].(map[string]any)
	require.True(t, ok, "usage 必须是 JSON 对象")
	assert.EqualValues(t, 20, usage["output_tokens"], "图像计价优先使用 output_tokens")
	assert.Contains(t, usage, "input_tokens_details", "input_tokens_details 需保留给计价归一为 prompt_tokens_details")
}
