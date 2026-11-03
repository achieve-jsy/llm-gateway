package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ContentPart represents a single OpenAI-compatible multimodal chat content part.
type ContentPart struct {
	Type        string             `json:"type"`
	Text        string             `json:"text,omitempty"`
	ImageURL    *ImageURLContent   `json:"image_url,omitempty"`
	InputAudio  *InputAudioContent `json:"input_audio,omitempty"`
	VideoURL    *VideoURLContent   `json:"video_url,omitempty"`
	ExtraFields UnknownJSONFields  `json:"-" swaggerignore:"true"`
}

// ImageURLContent contains an image reference for image_url parts.
type ImageURLContent struct {
	URL         string            `json:"url"`
	Detail      string            `json:"detail,omitempty"`
	MediaType   string            `json:"media_type,omitempty"`
	ExtraFields UnknownJSONFields `json:"-" swaggerignore:"true"`
}

// VideoURLContent 表示视频输入片段：url 与 file_id 二选一，fps 等采样参数可选。
// 与 ImageURLContent 一致，同时兼容 "url 字符串" 与 {"url": ...} 两种 JSON 形态。
type VideoURLContent struct {
	URL         string   `json:"url,omitempty"`
	FileID      string   `json:"file_id,omitempty"`
	FPS         *float64 `json:"fps,omitempty"`
	MaxFrames   *int     `json:"max_frames,omitempty"`
	StartOffset *float64 `json:"start_offset,omitempty"`
	EndOffset   *float64 `json:"end_offset,omitempty"`
	MediaType   string   `json:"media_type,omitempty"`
	// Source 记录目标方言（ark-chat / ark-responses / dashscope / zhipu / kimi / gemini），
	// 只用于出站翻译时决定 fps 等参数的落位，不参与序列化。
	Source      string            `json:"-"`
	ExtraFields UnknownJSONFields `json:"-"`
}

func (v *VideoURLContent) UnmarshalJSON(data []byte) error {
	parsed, err := unmarshalVideoURLContent(data)
	if err != nil {
		return err
	}
	*v = *parsed
	return nil
}

func (v VideoURLContent) MarshalJSON() ([]byte, error) {
	if v.URL == "" && v.FileID == "" {
		return nil, NewContentPartError("video_url", "video_url.url")
	}
	return marshalWithUnknownJSONFields(struct {
		URL         string   `json:"url,omitempty"`
		FileID      string   `json:"file_id,omitempty"`
		FPS         *float64 `json:"fps,omitempty"`
		MaxFrames   *int     `json:"max_frames,omitempty"`
		StartOffset *float64 `json:"start_offset,omitempty"`
		EndOffset   *float64 `json:"end_offset,omitempty"`
		MediaType   string   `json:"media_type,omitempty"`
	}{
		URL:         v.URL,
		FileID:      v.FileID,
		FPS:         v.FPS,
		MaxFrames:   v.MaxFrames,
		StartOffset: v.StartOffset,
		EndOffset:   v.EndOffset,
		MediaType:   v.MediaType,
	}, v.ExtraFields)
}

// InputAudioContent contains inline audio payload metadata.
type InputAudioContent struct {
	Data        string            `json:"data"`
	Format      string            `json:"format"`
	ExtraFields UnknownJSONFields `json:"-" swaggerignore:"true"`
}

func (p *ContentPart) UnmarshalJSON(data []byte) error {
	part, err := unmarshalContentPart(data)
	if err != nil {
		return err
	}
	*p = part
	return nil
}

func (p ContentPart) MarshalJSON() ([]byte, error) {
	switch p.Type {
	case "text", "input_text":
		if p.Text == "" {
			return nil, fmt.Errorf("text part is missing text")
		}
		return marshalWithUnknownJSONFields(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			Type: "text",
			Text: p.Text,
		}, p.ExtraFields)
	case "image_url", "input_image":
		if p.ImageURL == nil || p.ImageURL.URL == "" {
			return nil, fmt.Errorf("image_url part is missing image_url.url")
		}
		return marshalWithUnknownJSONFields(struct {
			Type     string           `json:"type"`
			ImageURL *ImageURLContent `json:"image_url"`
		}{
			Type:     "image_url",
			ImageURL: p.ImageURL,
		}, p.ExtraFields)
	case "input_audio":
		if p.InputAudio == nil || p.InputAudio.Data == "" || p.InputAudio.Format == "" {
			return nil, fmt.Errorf("input_audio part is missing data or format")
		}
		return marshalWithUnknownJSONFields(struct {
			Type       string             `json:"type"`
			InputAudio *InputAudioContent `json:"input_audio"`
		}{
			Type:       "input_audio",
			InputAudio: p.InputAudio,
		}, p.ExtraFields)
	case "video_url", "input_video", "video":
		if p.VideoURL == nil || (p.VideoURL.URL == "" && p.VideoURL.FileID == "") {
			return nil, NewContentPartError("video_url", "video_url.url")
		}
		return marshalWithUnknownJSONFields(struct {
			Type     string           `json:"type"`
			VideoURL *VideoURLContent `json:"video_url"`
		}{
			Type:     "video_url",
			VideoURL: p.VideoURL,
		}, p.ExtraFields)
	default:
		return nil, NewContentPartError(p.Type, "")
	}
}

func (c *ImageURLContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("image_url part is missing image_url.url")
	}

	if trimmed[0] == '"' {
		var url string
		if err := json.Unmarshal(trimmed, &url); err != nil {
			return err
		}
		if url == "" {
			return fmt.Errorf("image_url part is missing image_url.url")
		}
		c.URL = url
		c.Detail = ""
		c.MediaType = ""
		c.ExtraFields = UnknownJSONFields{}
		return nil
	}

	var raw struct {
		URL       string `json:"url"`
		Detail    string `json:"detail,omitempty"`
		MediaType string `json:"media_type,omitempty"`
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return err
	}
	if raw.URL == "" {
		return fmt.Errorf("image_url part is missing image_url.url")
	}
	extraFields, err := extractUnknownJSONFields(trimmed,
		"url",
		"detail",
		"media_type",
	)
	if err != nil {
		return err
	}

	c.URL = raw.URL
	c.Detail = raw.Detail
	c.MediaType = raw.MediaType
	c.ExtraFields = extraFields
	return nil
}

func (c ImageURLContent) MarshalJSON() ([]byte, error) {
	if c.URL == "" {
		return nil, fmt.Errorf("image_url part is missing image_url.url")
	}
	return marshalWithUnknownJSONFields(struct {
		URL       string `json:"url"`
		Detail    string `json:"detail,omitempty"`
		MediaType string `json:"media_type,omitempty"`
	}{
		URL:       c.URL,
		Detail:    c.Detail,
		MediaType: c.MediaType,
	}, c.ExtraFields)
}

func (a *InputAudioContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("input_audio part is missing data or format")
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("input_audio must be an object")
	}

	var raw struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return err
	}
	if raw.Data == "" || raw.Format == "" {
		return fmt.Errorf("input_audio part is missing data or format")
	}
	extraFields, err := extractUnknownJSONFields(trimmed,
		"data",
		"format",
	)
	if err != nil {
		return err
	}

	a.Data = raw.Data
	a.Format = raw.Format
	a.ExtraFields = extraFields
	return nil
}

func (a InputAudioContent) MarshalJSON() ([]byte, error) {
	if a.Data == "" || a.Format == "" {
		return nil, fmt.Errorf("input_audio part is missing data or format")
	}
	return marshalWithUnknownJSONFields(struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	}{
		Data:   a.Data,
		Format: a.Format,
	}, a.ExtraFields)
}

// UnmarshalMessageContent decodes supported chat message content payloads.
// Chat content accepts plain strings, null, or arrays of supported content parts.
func UnmarshalMessageContent(data []byte) (any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	switch trimmed[0] {
	case '"':
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		return text, nil
	case '[':
		var rawParts []json.RawMessage
		if err := json.Unmarshal(trimmed, &rawParts); err != nil {
			return nil, err
		}

		parts := make([]ContentPart, len(rawParts))
		for i, rawPart := range rawParts {
			part, err := unmarshalContentPart(rawPart)
			if err != nil {
				return nil, wrapContentPartError(i, err)
			}
			parts[i] = part
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("must be a string or array of content parts")
	}
}

// NormalizeMessageContent validates dynamic content and returns its canonical form.
func NormalizeMessageContent(content any) (any, error) {
	switch c := content.(type) {
	case nil:
		return "", nil
	case string:
		return c, nil
	case []ContentPart:
		parts := make([]ContentPart, len(c))
		for i, part := range c {
			normalized, err := normalizeTypedContentPart(part)
			if err != nil {
				return nil, wrapContentPartError(i, err)
			}
			parts[i] = normalized
		}
		return parts, nil
	case []any:
		parts := make([]ContentPart, len(c))
		for i, part := range c {
			normalized, err := normalizeContentPartValue(part)
			if err != nil {
				return nil, wrapContentPartError(i, err)
			}
			parts[i] = normalized
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("must be a string or array of content parts")
	}
}

// ExtractTextContent returns the textual portion of request content.
// Structured content parts are reduced to their text components only.
func ExtractTextContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []ContentPart:
		return joinTextParts(partsText(c))
	case []any:
		return joinTextParts(interfacePartsText(c))
	default:
		return ""
	}
}

// HasStructuredContent reports whether the content uses the array form.
func HasStructuredContent(content any) bool {
	switch c := content.(type) {
	case []ContentPart:
		return len(c) > 0
	case []any:
		return len(c) > 0
	default:
		return false
	}
}

// HasNonTextContent reports whether the content contains image/audio parts.
func HasNonTextContent(content any) bool {
	parts, ok := NormalizeContentParts(content)
	if !ok {
		return false
	}
	for _, part := range parts {
		if part.Type != "text" {
			return true
		}
	}
	return false
}

// NormalizeContentParts converts dynamic JSON-decoded content into typed parts.
func NormalizeContentParts(content any) ([]ContentPart, bool) {
	normalized, err := NormalizeMessageContent(content)
	if err != nil {
		return nil, false
	}
	parts, ok := normalized.([]ContentPart)
	if !ok || len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

func joinTextParts(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	return strings.Join(texts, " ")
}

func partsText(parts []ContentPart) []string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text", "input_text":
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	return texts
}

func interfacePartsText(parts []any) []string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		partType, _ := partMap["type"].(string)
		switch partType {
		case "text", "input_text":
			if text, ok := partMap["text"].(string); ok && text != "" {
				texts = append(texts, text)
			}
		}
	}
	return texts
}

func unmarshalContentPart(data []byte) (ContentPart, error) {
	var raw struct {
		Type       string          `json:"type"`
		Text       *string         `json:"text,omitempty"`
		ImageURL   json.RawMessage `json:"image_url,omitempty"`
		InputAudio json.RawMessage `json:"input_audio,omitempty"`
		VideoURL   json.RawMessage `json:"video_url,omitempty"`
		// Responses 方言把 file_id / url / fps 放在 part 顶层
		FPS    json.RawMessage `json:"fps,omitempty"`
		FileID string          `json:"file_id,omitempty"`
		URL    string          `json:"url,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ContentPart{}, err
	}
	extraFields, err := extractUnknownJSONFields(data,
		"type",
		"text",
		"image_url",
		"input_audio",
		"video_url",
		"fps",
		"file_id",
		"url",
	)
	if err != nil {
		return ContentPart{}, err
	}

	switch raw.Type {
	case "text", "input_text":
		if raw.Text == nil || *raw.Text == "" {
			return ContentPart{}, fmt.Errorf("text part is missing text")
		}
		return ContentPart{
			Type:        "text",
			Text:        *raw.Text,
			ExtraFields: extraFields,
		}, nil
	case "image_url", "input_image":
		imageURL, err := unmarshalImageURLContent(raw.ImageURL)
		if err != nil {
			return ContentPart{}, err
		}
		return ContentPart{
			Type:        "image_url",
			ImageURL:    imageURL,
			ExtraFields: extraFields,
		}, nil
	case "input_audio":
		audio, err := unmarshalInputAudioContent(raw.InputAudio)
		if err != nil {
			return ContentPart{}, err
		}
		return ContentPart{
			Type:        "input_audio",
			InputAudio:  audio,
			ExtraFields: extraFields,
		}, nil
	case "video_url", "input_video", "video":
		video, err := unmarshalVideoURLContent(raw.VideoURL)
		if err != nil {
			// Responses 方言：file_id / url 位于 part 顶层
			if raw.FileID == "" && raw.URL == "" {
				return ContentPart{}, err
			}
			video = &VideoURLContent{URL: raw.URL, FileID: raw.FileID}
		}
		if len(raw.FPS) > 0 && video.FPS == nil {
			var fps float64
			if err := json.Unmarshal(raw.FPS, &fps); err == nil {
				video.FPS = &fps
			}
		}
		return ContentPart{
			Type:        "video_url",
			VideoURL:    video,
			ExtraFields: extraFields,
		}, nil
	case "thinking", "redacted_thinking":
		// Anthropic extended thinking blocks are passed back verbatim in multi-turn
		// conversations. They carry an encrypted signature that Anthropic validates
		// on subsequent requests; we preserve all fields via ExtraFields so the
		// Anthropic provider can reconstruct the exact block on the way out.
		// The text field (if present) is stored in ContentPart.Text for convenience.
		text := ""
		if raw.Text != nil {
			text = *raw.Text
		}
		return ContentPart{
			Type:        raw.Type,
			Text:        text,
			ExtraFields: extraFields,
		}, nil
	default:
		return ContentPart{}, NewContentPartError(raw.Type, "")
	}
}

func normalizeTypedContentPart(part ContentPart) (ContentPart, error) {
	switch part.Type {
	case "text", "input_text":
		if part.Text == "" {
			return ContentPart{}, fmt.Errorf("text part is missing text")
		}
		return ContentPart{
			Type:        "text",
			Text:        part.Text,
			ExtraFields: CloneUnknownJSONFields(part.ExtraFields),
		}, nil
	case "image_url", "input_image":
		if part.ImageURL == nil || part.ImageURL.URL == "" {
			return ContentPart{}, fmt.Errorf("image_url part is missing image_url.url")
		}
		return ContentPart{
			Type: "image_url",
			ImageURL: &ImageURLContent{
				URL:         part.ImageURL.URL,
				Detail:      part.ImageURL.Detail,
				MediaType:   part.ImageURL.MediaType,
				ExtraFields: CloneUnknownJSONFields(part.ImageURL.ExtraFields),
			},
			ExtraFields: CloneUnknownJSONFields(part.ExtraFields),
		}, nil
	case "input_audio":
		if part.InputAudio == nil || part.InputAudio.Data == "" || part.InputAudio.Format == "" {
			return ContentPart{}, fmt.Errorf("input_audio part is missing data or format")
		}
		return ContentPart{
			Type: "input_audio",
			InputAudio: &InputAudioContent{
				Data:        part.InputAudio.Data,
				Format:      part.InputAudio.Format,
				ExtraFields: CloneUnknownJSONFields(part.InputAudio.ExtraFields),
			},
			ExtraFields: CloneUnknownJSONFields(part.ExtraFields),
		}, nil
	case "video_url", "input_video", "video":
		if part.VideoURL == nil || (part.VideoURL.URL == "" && part.VideoURL.FileID == "") {
			return ContentPart{}, fmt.Errorf("video_url part is missing url or file_id")
		}
		cloned := *part.VideoURL
		cloned.ExtraFields = CloneUnknownJSONFields(part.VideoURL.ExtraFields)
		return ContentPart{
			Type:        "video_url",
			VideoURL:    &cloned,
			ExtraFields: CloneUnknownJSONFields(part.ExtraFields),
		}, nil
	case "thinking", "redacted_thinking":
		// Pass through verbatim — all payload fields live in ExtraFields.
		return ContentPart{
			Type:        part.Type,
			Text:        part.Text,
			ExtraFields: CloneUnknownJSONFields(part.ExtraFields),
		}, nil
	default:
		return ContentPart{}, NewContentPartError(part.Type, "")
	}
}

func normalizeContentPartValue(part any) (ContentPart, error) {
	switch v := part.(type) {
	case ContentPart:
		return normalizeTypedContentPart(v)
	case map[string]any:
		return normalizeContentPartMap(v)
	default:
		return ContentPart{}, fmt.Errorf("content part must be an object")
	}
}

func normalizeContentPartMap(partMap map[string]any) (ContentPart, error) {
	rawPart, err := json.Marshal(partMap)
	if err != nil {
		return ContentPart{}, fmt.Errorf("content part must be an object")
	}
	return unmarshalContentPart(rawPart)
}

func unmarshalImageURLContent(data []byte) (*ImageURLContent, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("image_url part is missing image_url.url")
	}

	switch trimmed[0] {
	case '"':
		var url string
		if err := json.Unmarshal(trimmed, &url); err != nil {
			return nil, err
		}
		if url == "" {
			return nil, fmt.Errorf("image_url part is missing image_url.url")
		}
		return &ImageURLContent{URL: url}, nil
	case '{':
		var imageURL ImageURLContent
		if err := json.Unmarshal(trimmed, &imageURL); err != nil {
			return nil, err
		}
		if imageURL.URL == "" {
			return nil, fmt.Errorf("image_url part is missing image_url.url")
		}
		return &imageURL, nil
	default:
		return nil, fmt.Errorf("image_url must be a string or object")
	}
}

// unmarshalVideoURLContent 解析 video_url：支持 "url 字符串" 与对象两种形态。
func unmarshalVideoURLContent(data []byte) (*VideoURLContent, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, NewContentPartError("video_url", "video_url.url")
	}

	switch trimmed[0] {
	case '"':
		var url string
		if err := json.Unmarshal(trimmed, &url); err != nil {
			return nil, err
		}
		if url == "" {
			return nil, NewContentPartError("video_url", "video_url.url")
		}
		return &VideoURLContent{URL: url}, nil
	case '{':
		var raw struct {
			URL         string   `json:"url"`
			FileID      string   `json:"file_id"`
			FPS         *float64 `json:"fps"`
			MaxFrames   *int     `json:"max_frames"`
			StartOffset *float64 `json:"start_offset"`
			EndOffset   *float64 `json:"end_offset"`
			MediaType   string   `json:"media_type"`
		}
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, err
		}
		if raw.URL == "" && raw.FileID == "" {
			return nil, NewContentPartError("video_url", "video_url.url")
		}
		extra, err := extractUnknownJSONFields(trimmed,
			"url", "file_id", "fps", "max_frames", "start_offset", "end_offset", "media_type")
		if err != nil {
			return nil, err
		}
		return &VideoURLContent{
			URL:         raw.URL,
			FileID:      raw.FileID,
			FPS:         raw.FPS,
			MaxFrames:   raw.MaxFrames,
			StartOffset: raw.StartOffset,
			EndOffset:   raw.EndOffset,
			MediaType:   raw.MediaType,
			ExtraFields: extra,
		}, nil
	default:
		return nil, NewContentPartError("video_url", "video_url")
	}
}

// ParseVideoURLContent 供 provider 层复用双形态解析（map/字符串均可）。
func ParseVideoURLContent(raw any) (*VideoURLContent, bool) {
	if raw == nil {
		return nil, false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	video, err := unmarshalVideoURLContent(encoded)
	if err != nil {
		return nil, false
	}
	return video, true
}

// HasVideoContent 判断消息列表里是否含视频片段（调度期能力过滤使用）。
func HasVideoContent(msgs []Message) bool {
	for _, msg := range msgs {
		parts, ok := NormalizeContentParts(msg.Content)
		if !ok {
			continue
		}
		for _, part := range parts {
			if part.Type == "video_url" {
				return true
			}
		}
	}
	return false
}

func unmarshalInputAudioContent(data []byte) (*InputAudioContent, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("input_audio part is missing data or format")
	}

	if trimmed[0] != '{' {
		return nil, fmt.Errorf("input_audio must be an object")
	}

	var audio InputAudioContent
	if err := json.Unmarshal(trimmed, &audio); err != nil {
		return nil, err
	}
	if audio.Data == "" || audio.Format == "" {
		return nil, fmt.Errorf("input_audio part is missing data or format")
	}
	return &audio, nil
}
