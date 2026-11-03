package service_test

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 夹具刻意取自仓库内真实的 LiteLLM catalog 条目
// （internal/controlplane/seed/litellm_catalog.json），key 名与 mode 都不臆测：
//   - input/output_cost_per_image
//   - input/output_cost_per_video_per_second、input/output_cost_per_second、cost_per_second
//   - input_cost_per_request
//   - input/output_cost_per_image_token、input/output_cost_per_pixel
//   - input_cost_per_character
//
// mode 分布（同一份种子）：image_generation 197 / image_edit 31 / video_generation 25 …
const mediaCatalogFixture = `{
  "dall-e-3": {
    "input_cost_per_image": 0.04,
    "litellm_provider": "openai",
    "mode": "image_generation"
  },
  "vertex_ai/imagen-3.0-generate-002": {
    "output_cost_per_image": 0.04,
    "litellm_provider": "vertex_ai-image-models",
    "mode": "image_generation"
  },
  "1024-x-1024/50-steps/bedrock/amazon.nova-canvas-v1:0": {
    "max_input_tokens": 2600,
    "output_cost_per_image": 0.06,
    "litellm_provider": "bedrock",
    "mode": "image_generation"
  },
  "1024-x-1024/dall-e-2": {
    "input_cost_per_pixel": 1.9e-08,
    "output_cost_per_pixel": 0.0,
    "litellm_provider": "openai",
    "mode": "image_generation"
  },
  "runwayml/gen4_turbo": {
    "output_cost_per_video_per_second": 0.05,
    "litellm_provider": "runwayml",
    "mode": "video_generation"
  },
  "openai/sora-2": {
    "output_cost_per_video_per_second": 0.1,
    "mode": "video_generation"
  },
  "gemini/veo-3.1-generate-001": {
    "input_cost_per_video_per_second": 0.4,
    "output_cost_per_video_per_second": 0.4,
    "mode": "video_generation"
  },
  "azure/whisper-1": {
    "input_cost_per_second": 0.0001,
    "output_cost_per_second": 0.0001,
    "mode": "audio_transcription"
  },
  "azure/tts-1": {
    "input_cost_per_character": 1.5e-05,
    "mode": "audio_speech"
  },
  "some-video-by-second": {
    "cost_per_second": 0.02,
    "mode": "video_generation"
  },
  "perplexity/pplx-70b-online": {
    "input_cost_per_request": 0.005,
    "input_cost_per_token": 1e-06,
    "output_cost_per_token": 2e-06,
    "mode": "chat"
  },
  "zero-placeholder": {
    "output_cost_per_image": 0.0,
    "input_cost_per_second": 0,
    "mode": "image_generation"
  },
  "gpt-image-1": {
    "input_cost_per_image_token": 1e-05,
    "output_cost_per_image_token": 4e-05,
    "input_cost_per_token": 5e-06,
    "mode": "image_generation"
  }
}`

func mediaF64(v float64) *float64 { return &v }

func parseMediaCatalog(t *testing.T) map[string]*service.ModelCatalogParsed {
	t.Helper()
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(mediaCatalogFixture), &raw))
	parsed, err := service.ParseLiteLLMJSON(raw)
	require.NoError(t, err)
	byID := make(map[string]*service.ModelCatalogParsed, len(parsed))
	for _, entry := range parsed {
		byID[entry.SourceModelID] = entry
	}
	return byID
}

func TestParseLiteLLMJSON_MediaPrices(t *testing.T) {
	entries := parseMediaCatalog(t)

	tests := []struct {
		name            string
		sourceModelID   string
		wantMode        string
		wantPerImage    *float64
		wantInputImage  *float64
		wantPerSecIn    *float64
		wantPerSecOut   *float64
		wantPerRequest  *float64
		wantImgTokenIn  *float64
		wantImgTokenOut *float64
		wantPixelIn     *float64
		wantPixelOut    *float64
		wantCharIn      *float64
		wantInputPerM   float64
		wantOutputPerM  float64
	}{
		{
			name:           "input_cost_per_image → input_per_image（dall-e-3）",
			sourceModelID:  "dall-e-3",
			wantMode:       "image_generation",
			wantInputImage: mediaF64(0.04),
		},
		{
			name:          "output_cost_per_image → per_image（imagen）",
			sourceModelID: "vertex_ai/imagen-3.0-generate-002",
			wantMode:      "image_generation",
			wantPerImage:  mediaF64(0.04),
		},
		{
			name:          "output_cost_per_image → per_image（bedrock nova-canvas，种子原值 0.06）",
			sourceModelID: "1024-x-1024/50-steps/bedrock/amazon.nova-canvas-v1:0",
			wantMode:      "image_generation",
			wantPerImage:  mediaF64(0.06),
		},
		{
			name:          "input/output_cost_per_pixel → 像素价（dall-e-2，output 为 0 占位）",
			sourceModelID: "1024-x-1024/dall-e-2",
			wantMode:      "image_generation",
			wantPixelIn:   mediaF64(1.9e-08),
			// output_cost_per_pixel = 0.0 → 保持 nil
		},
		{
			name:          "output_cost_per_video_per_second → per_second_output（runway）",
			sourceModelID: "runwayml/gen4_turbo",
			wantMode:      "video_generation",
			wantPerSecOut: mediaF64(0.05),
		},
		{
			name:          "output_cost_per_video_per_second → per_second_output（sora-2，上游样例 0.1）",
			sourceModelID: "openai/sora-2",
			wantMode:      "video_generation",
			wantPerSecOut: mediaF64(0.1),
		},
		{
			name:          "input/output_cost_per_video_per_second → per_second_input / per_second_output（veo）",
			sourceModelID: "gemini/veo-3.1-generate-001",
			wantMode:      "video_generation",
			wantPerSecIn:  mediaF64(0.4),
			wantPerSecOut: mediaF64(0.4),
		},
		{
			name:          "input/output_cost_per_second → per_second_input / per_second_output（whisper）",
			sourceModelID: "azure/whisper-1",
			wantMode:      "audio_transcription",
			wantPerSecIn:  mediaF64(0.0001),
			wantPerSecOut: mediaF64(0.0001),
		},
		{
			name:          "cost_per_second（无 input/output 前缀）→ 同时兜底输入与输出秒价",
			sourceModelID: "some-video-by-second",
			wantMode:      "video_generation",
			wantPerSecIn:  mediaF64(0.02),
			wantPerSecOut: mediaF64(0.02),
		},
		{
			name:          "input_cost_per_character → per_character_input（tts）",
			sourceModelID: "azure/tts-1",
			wantMode:      "audio_speech",
			wantCharIn:    mediaF64(1.5e-05),
		},
		{
			name:           "input_cost_per_request → per_request（perplexity）",
			sourceModelID:  "perplexity/pplx-70b-online",
			wantMode:       "chat",
			wantPerRequest: mediaF64(0.005),
			// token 价照旧乘 1e6；媒体价不缩放，两者在同一 entry 上互不影响。
			wantInputPerM:  1.0,
			wantOutputPerM: 2.0,
		},
		{
			name:          "0.0 占位视为无价（NULL 语义）",
			sourceModelID: "zero-placeholder",
			wantMode:      "image_generation",
		},
		{
			name:            "图像 token 价（gpt-image-1）",
			sourceModelID:   "gpt-image-1",
			wantMode:        "image_generation",
			wantImgTokenIn:  mediaF64(1e-05),
			wantImgTokenOut: mediaF64(4e-05),
			// 同一 entry 的普通 token 价照旧 ×1e6。
			wantInputPerM: 5.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := entries[tt.sourceModelID]
			require.Truef(t, ok, "fixture entry %s 未解析出来", tt.sourceModelID)

			assert.Equal(t, tt.wantMode, entry.Mode, "mode")
			assert.Equal(t, tt.wantPerImage, entry.PerImage, "per_image")
			assert.Equal(t, tt.wantInputImage, entry.InputPerImage, "input_per_image")
			assert.Equal(t, tt.wantPerSecIn, entry.PerSecondInput, "per_second_input")
			assert.Equal(t, tt.wantPerSecOut, entry.PerSecondOutput, "per_second_output")
			assert.Equal(t, tt.wantPerRequest, entry.PerRequest, "per_request")
			assert.Equal(t, tt.wantImgTokenIn, entry.InputPerImageToken, "input_per_image_token")
			assert.Equal(t, tt.wantImgTokenOut, entry.OutputPerImageToken, "output_per_image_token")
			assert.Equal(t, tt.wantPixelIn, entry.InputPerPixel, "input_per_pixel")
			assert.Equal(t, tt.wantPixelOut, entry.OutputPerPixel, "output_per_pixel")
			assert.Equal(t, tt.wantCharIn, entry.PerCharacterInput, "per_character_input")
			assert.InDelta(t, tt.wantInputPerM, entry.InputPricePerMillion, 1e-9, "input_price_per_million")
			assert.InDelta(t, tt.wantOutputPerM, entry.OutputPricePerMillion, 1e-9, "output_price_per_million")
		})
	}
}

// TestParseLiteLLMJSON_MediaPriceNotScaled 媒体价是「每张/每秒/每次」的美元原值，
// 不能复用 tokenPriceScale（×1e6），否则单价会被放大一百万倍。
func TestParseLiteLLMJSON_MediaPriceNotScaled(t *testing.T) {
	entries := parseMediaCatalog(t)

	dallE := entries["dall-e-3"]
	require.NotNil(t, dallE.InputPerImage)
	assert.Equal(t, 0.04, *dallE.InputPerImage)
	assert.NotEqual(t, 0.04*1_000_000.0, *dallE.InputPerImage)

	runway := entries["runwayml/gen4_turbo"]
	require.NotNil(t, runway.PerSecondOutput)
	assert.Equal(t, 0.05, *runway.PerSecondOutput)
}
