package middleware

// contextKey is a custom string type to prevent context key collisions
// when used with Gin's c.Set() / c.Get() across middleware boundaries.
type contextKey string

func (k contextKey) String() string { return string(k) }

// ---- Identity keys ----

// KeyIDKey holds the API key identifier from the auth service.
const KeyIDKey contextKey = "key_id"

// UserIDKey holds the authenticated user identifier.
const UserIDKey contextKey = "user_id"

// DeptIDKey holds the department identifier from the user's identity.
const DeptIDKey contextKey = "dept_id"

// StudioIDKey holds the studio/workspace identifier.
const StudioIDKey contextKey = "studio_id"

// ---- Latency keys ----

// TTFTKey holds the time-to-first-token duration in milliseconds.
const TTFTKey contextKey = "ttft_ms"

// DurationKey holds the total request duration in milliseconds.
const DurationKey contextKey = "duration_ms"

// ProviderDurationKey holds the upstream provider response duration in milliseconds.
const ProviderDurationKey contextKey = "provider_duration_ms"

// ---- Token bucket keys ----

// PromptTokensKey holds the number of prompt (input) tokens consumed.
const PromptTokensKey contextKey = "prompt_tokens"

// CompletionTokensKey holds the number of completion (output) tokens generated.
const CompletionTokensKey contextKey = "completion_tokens"

// ReasoningTokensKey holds the number of reasoning tokens (e.g. chain-of-thought).
const ReasoningTokensKey contextKey = "reasoning_tokens"

// CachedTokensKey holds the number of cached tokens (prompt cache hits).
const CachedTokensKey contextKey = "cached_tokens"

// ---- Provider routing keys ----

// ProviderTypeKey holds the upstream provider type name (e.g. "openai", "anthropic").
const ProviderTypeKey contextKey = "provider_type"

// ProviderModelIDKey holds the specific model ID used on the upstream provider side.
const ProviderModelIDKey contextKey = "provider_model_id"

// ---- Metadata keys ----

// IsStreamKey indicates whether the request uses streaming (SSE) mode.
const IsStreamKey contextKey = "is_stream"

// SessionIDKey holds the client-provided or auto-generated conversation session identifier.
const SessionIDKey contextKey = "session_id"

// ModelKey holds the resolved model/group name for the request.
const ModelKey contextKey = "model"

// ---- Request identity ----

// RequestIDKey holds the unique request identifier.
const RequestIDKey contextKey = "request_id"

// ---- Metadata keys for call_payloads ----

// MessagesKey holds the request messages for call_payloads logging.
const MessagesKey contextKey = "messages"

// ResponseTextKey holds the assistant's response text for call_payloads logging.
const ResponseTextKey contextKey = "response_text"
