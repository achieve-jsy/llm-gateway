package gemini

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// cacheMode controls how the gateway decides whether to build an explicit
// Gemini/Vertex CachedContent resource for a request. It is aligned with
// LiteLLM's Vertex AI context caching flow:
//
//   - off    (default): no explicit caching; requests rely on Gemini's
//     automatic implicit prefix caching.
//   - marker: only build a cache resource when the caller marks a contiguous
//     message block with cache_control:{"type":"ephemeral"} (LiteLLM semantics).
//   - auto:   build a cache resource from a conservatively-chosen prefix
//     (system instruction + history ending on a user turn); the tail (the
//     final user turn) is sent inline. Best-effort: if the structure is not
//     valid for Vertex, caching is skipped.
type cacheMode string

const (
	cacheModeOff    cacheMode = "off"
	cacheModeAuto   cacheMode = "auto"
	cacheModeMarker cacheMode = "marker"
)

const (
	// geminiCacheModeEnvVar is the gateway-wide default explicit-caching mode.
	geminiCacheModeEnvVar = "GEMINI_CACHE_MODE"
	// minimumCachedContentTokens mirrors LiteLLM's MINIMUM_PROMPT_CACHE_TOKEN_COUNT.
	minimumCachedContentTokens = 1024
	// cachedContentPageSize is the Vertex cachedContents list page size ceiling.
	cachedContentPageSize = 100
	// cachedContentListPageCap bounds the pagination scan for a matching resource.
	cachedContentListPageCap = 5
)

// cachedContentManager owns the Vertex cachedContents client and the
// create/list logic. It never fails a chat request: errors fall back to no
// explicit cache.
type cachedContentManager struct {
	// client targets the location-level root, so endpoints like
	// /cachedContents resolve against https://aiplatform.googleapis.com/v1/
	// projects/<p>/locations/<l>.
	client *llmclient.Client
	// baseURL is the location-level root used to build the resource model field.
	baseURL string
}

// newCachedContentManager derives the Vertex cachedContents surface from the
// publisher-level native base URL (…/publishers/google). It returns nil when
// the URL is not Vertex-shaped so callers can skip caching safely.
func newCachedContentManager(nativeBaseURL string, client *llmclient.Client) *cachedContentManager {
	root, ok := vertexLocationRoot(nativeBaseURL)
	if !ok || client == nil {
		return nil
	}
	return &cachedContentManager{
		client:  client,
		baseURL: root,
	}
}

// vertexLocationRoot strips the trailing /publishers/google segment from a
// Vertex publisher native URL, yielding the location-level root.
func vertexLocationRoot(nativeBaseURL string) (string, bool) {
	const suffix = "/publishers/google"
	nativeBaseURL = strings.TrimRight(strings.TrimSpace(nativeBaseURL), "/")
	if !strings.HasSuffix(nativeBaseURL, suffix) {
		return "", false
	}
	return strings.TrimSuffix(nativeBaseURL, suffix), true
}

// cacheModeFromEnv reads the gateway-wide default caching mode.
func cacheModeFromEnv() cacheMode {
	return parseCacheMode(os.Getenv(geminiCacheModeEnvVar))
}

// parseCacheMode normalizes a mode string, defaulting to off.
func parseCacheMode(value string) cacheMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto":
		return cacheModeAuto
	case "marker":
		return cacheModeMarker
	default:
		return cacheModeOff
	}
}

// resolveCacheMode combines a per-request override (extra field "cache") with
// the provider-level default from the environment.
func resolveCacheMode(extra core.UnknownJSONFields) cacheMode {
	raw := extra.Lookup("cache")
	if len(raw) > 0 {
		var mode string
		if err := json.Unmarshal(raw, &mode); err == nil && mode != "" {
			return parseCacheMode(mode)
		}
	}
	return cacheModeFromEnv()
}

// cacheableMessages selects the contiguous block of messages to cache for the
// given mode. It mirrors LiteLLM's separate_cached_messages. Returns ok=false
// when no cacheable block exists or the structure is invalid for Vertex.
func cacheableMessages(messages []core.Message, mode cacheMode) (cached, tail []core.Message, ok bool) {
	switch mode {
	case cacheModeMarker:
		return markerCachedMessages(messages)
	case cacheModeAuto:
		return autoCachedMessages(messages)
	default:
		return nil, nil, false
	}
}

// markerCachedMessages keeps only a single contiguous run of messages that
// carry a cache_control:ephemeral marker, exactly like LiteLLM.
func markerCachedMessages(messages []core.Message) (cached, tail []core.Message, ok bool) {
	marked := make([]int, 0, len(messages))
	for i, msg := range messages {
		if messageCarriesCacheMarker(msg) {
			marked = append(marked, i)
		}
	}
	if len(marked) == 0 {
		return nil, nil, false
	}

	// Require a single continuous block.
	first := marked[0]
	last := first
	for _, idx := range marked[1:] {
		if idx != last+1 {
			// Two disjoint cached blocks: skip caching to stay predictable.
			return nil, nil, false
		}
		last = idx
	}
	cached = messages[first : last+1]
	tail = append(append([]core.Message{}, messages[:first]...), messages[last+1:]...)
	if !cachedBlockEndsOnSupportedTurn(cached) {
		return nil, nil, false
	}
	return cached, tail, true
}

// autoCachedMessages selects the largest prefix that ends on a user turn and is
// followed by more messages (the final user turn stays as the tail). This
// guarantees the cached block ends on a user turn, which Vertex requires.
func autoCachedMessages(messages []core.Message) (cached, tail []core.Message, ok bool) {
	lastUser := -1
	for i, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			lastUser = i
		}
	}
	if lastUser <= 0 {
		return nil, nil, false
	}

	// The cached block ends at the user turn just before the final user turn.
	end := lastUser - 1
	for end >= 0 && !strings.EqualFold(strings.TrimSpace(messages[end].Role), "user") {
		end--
	}
	if end < 0 {
		// No system + user prefix before the final user turn.
		return nil, nil, false
	}
	cached = append([]core.Message{}, messages[:end+1]...)
	tail = append([]core.Message{}, messages[end+1:]...)
	if !cachedBlockEndsOnSupportedTurn(cached) {
		return nil, nil, false
	}
	return cached, tail, true
}

// cachedBlockEndsOnSupportedTurn mirrors LiteLLM: the cachedContents API
// rejects contents ending on a model turn (assistant/tool/function).
func cachedBlockEndsOnSupportedTurn(cached []core.Message) bool {
	if len(cached) == 0 {
		return false
	}
	last := cached[len(cached)-1]
	role := strings.ToLower(strings.TrimSpace(last.Role))
	switch role {
	case "assistant", "tool", "function", "model":
		return false
	default:
		return true
	}
}

// messageCarriesCacheMarker reports whether a message opts into caching via
// cache_control:{"type":"ephemeral", ...} on any content part or the message.
func messageCarriesCacheMarker(msg core.Message) bool {
	for _, part := range msg.ContentSchema {
		if cacheControlMarked(part.ExtraFields) {
			return true
		}
	}
	return cacheControlMarked(msg.ExtraFields)
}

func cacheControlMarked(fields core.UnknownJSONFields) bool {
	raw := fields.Lookup("cache_control")
	if len(raw) == 0 {
		return false
	}
	var cc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &cc); err != nil {
		return false
	}
	return strings.EqualFold(cc.Type, "ephemeral")
}

// estimatedContentTokens is a conservative token estimate used only to guard
// the minimum-cacheable-token check. It is intentionally coarse; the create
// call is best-effort so an underestimate merely loses cache value.
func estimatedContentTokens(messages []core.Message) int {
	total := 0
	for _, msg := range messages {
		switch c := msg.Content.(type) {
		case string:
			total += len(c)/4 + 1
		default:
			for _, part := range msg.ContentSchema {
				if part.Text != "" {
					total += len(part.Text)/4 + 1
				}
			}
		}
	}
	return total
}

// cachedContentCacheKey derives a deterministic dedupe key from the cached
// message prefix, tools, and model, mirroring LiteLLM's cache_key.
func cachedContentCacheKey(messages []core.Message, model string, tools []map[string]any) string {
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte{0})
	for _, msg := range messages {
		h.Write([]byte(msg.Role))
		h.Write([]byte{0})
		if s, ok := msg.Content.(string); ok {
			h.Write([]byte(s))
		}
		h.Write([]byte{0})
	}
	if len(tools) > 0 {
		h.Write([]byte("|tools|"))
	}
	// A stable digest of the tool JSON ensures key changes when the toolset changes.
	h.Write([]byte(toolFingerprint(tools)))
	return hex.EncodeToString(h.Sum(nil))
}

// toolFingerprint produces a stable byte digest of the normalized tool list.
func toolFingerprint(tools []map[string]any) string {
	if len(tools) == 0 {
		return ""
	}
	b, err := json.Marshal(tools)
	if err != nil {
		return ""
	}
	return string(b)
}

// ensure returns an existing or newly-created cachedContent resource name for
// the given prefix. It is best-effort: any error returns "".
func (m *cachedContentManager) ensure(ctx context.Context, messages []core.Message, tools []map[string]any, model, ttl string) (string, error) {
	model = normalizeGeminiModelID(model)
	key := cachedContentCacheKey(messages, model, tools)

	if name, ok := m.findByName(ctx, key); ok {
		return name, nil
	}

	body, err := m.buildResourceBody(messages, tools, model, key, ttl)
	if err != nil {
		return "", err
	}
	var created cachedContentResource
	if err := m.client.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/cachedContents",
		Body:     body,
	}, &created); err != nil {
		return "", err
	}
	if created.Name == "" {
		return "", fmt.Errorf("vertex cachedContents create returned empty name")
	}
	return created.Name, nil
}

// findByName lists existing cachedContents and returns the name whose
// displayName equals the cache key. It scans a bounded number of pages.
func (m *cachedContentManager) findByName(ctx context.Context, displayName string) (string, bool) {
	pageToken := ""
	for page := 0; page < cachedContentListPageCap; page++ {
		endpoint := fmt.Sprintf("/cachedContents?pageSize=%d", cachedContentPageSize)
		if pageToken != "" {
			endpoint += "&pageToken=" + pageToken
		}
		var resp cachedContentListResponse
		if err := m.client.Do(ctx, llmclient.Request{
			Method:   http.MethodGet,
			Endpoint: endpoint,
		}, &resp); err != nil {
			return "", false
		}
		for _, item := range resp.CachedContents {
			if item.DisplayName == displayName {
				return item.Name, true
			}
		}
		pageToken = resp.NextPageToken
		if pageToken == "" {
			return "", false
		}
	}
	return "", false
}

// buildResourceBody converts the cached message prefix into a Vertex
// cachedContents create request. Tools and the system instruction are baked
// into the cached resource (LiteLLM drops them from the live request).
func (m *cachedContentManager) buildResourceBody(messages []core.Message, tools []map[string]any, model, key, ttl string) (any, error) {
	// Reuse the gateway's message→gemini conversion on a sub-request so the
	// resource contents and system_instruction stay consistent with the model.
	sub := &core.ChatRequest{Model: model, Messages: messages, Tools: tools}
	converted, err := convertChatRequestToGemini(sub)
	if err != nil {
		return nil, err
	}

	modelName := "publishers/google/models/" + model
	if strings.HasPrefix(m.baseURL, "https://generativelanguage.googleapis.com") {
		modelName = "models/" + model
	}

	out := map[string]any{
		"model":       modelName,
		"contents":    converted.Contents,
		"displayName": key,
	}
	if converted.SystemInstruction != nil {
		out["system_instruction"] = converted.SystemInstruction
	}
	if len(converted.Tools) > 0 {
		out["tools"] = converted.Tools
	}
	if ttl != "" {
		out["ttl"] = ttl
	}
	return out, nil
}

// clearIncompatibleCachedContentFields drops system_instruction/tools/toolConfig
// from the live request when cachedContent is set, because Vertex rejects them
// alongside cachedContent (LiteLLM's same rule).
func clearIncompatibleCachedContentFields(body *geminiGenerateContentRequest) {
	if body == nil || body.CachedContent == "" {
		return
	}
	body.SystemInstruction = nil
	body.Tools = nil
	body.ToolConfig = nil
}

type cachedContentResource struct {
	Name        string `json:"name"`
	Model       string `json:"model"`
	DisplayName string `json:"displayName"`
	CreateTime  string `json:"createTime"`
	ExpireTime  string `json:"expireTime"`
}

type cachedContentListResponse struct {
	CachedContents []cachedContentResource `json:"cachedContents"`
	NextPageToken  string                  `json:"nextPageToken"`
}
