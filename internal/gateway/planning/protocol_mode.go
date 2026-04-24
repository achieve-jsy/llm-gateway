package planning

// ProtocolMode 表示 gateway 与上游交互时采用的协议形态。
type ProtocolMode string

const (
	ProtocolAuto              ProtocolMode = "auto"
	ProtocolOpenAIChat        ProtocolMode = "openai_chat"
	ProtocolOpenAIResponses   ProtocolMode = "openai_responses"
	ProtocolAnthropicMessages ProtocolMode = "anthropic_messages"
	ProtocolGeminiNative      ProtocolMode = "gemini_native"
	ProtocolBedrockConverse   ProtocolMode = "bedrock_converse"
	ProtocolOpenAICompatible  ProtocolMode = "openai_compatible"
	ProtocolPassthrough       ProtocolMode = "passthrough"
)

// ParamProfile 表示针对某类上游协议/模型族的参数适配画像。
type ParamProfile string

const (
	ParamProfileAuto                ParamProfile = "auto"
	ParamProfileOpenAIStandard      ParamProfile = "openai_standard"
	ParamProfileOpenAIReasoningChat ParamProfile = "openai_reasoning_chat"
	ParamProfileOpenAIResponses     ParamProfile = "openai_responses"
	ParamProfileDeepSeekReasoning   ParamProfile = "deepseek_reasoning"
	ParamProfileGeminiThinking      ParamProfile = "gemini_thinking"
	ParamProfileAnthropicThinking   ParamProfile = "anthropic_thinking"
	ParamProfileBedrockConverse     ParamProfile = "bedrock_converse"
	ParamProfilePassthrough         ParamProfile = "passthrough"
)

// UnsupportedParamPolicy 描述遇到上游不支持参数时的处理策略。
type UnsupportedParamPolicy string

const (
	UnsupportedParamPolicyPreserve UnsupportedParamPolicy = "preserve"
	UnsupportedParamPolicyDrop     UnsupportedParamPolicy = "drop"
	UnsupportedParamPolicyReject   UnsupportedParamPolicy = "reject"
)

// RequestKind 表示 canonical request 的粗粒度请求类型。
type RequestKind string

const (
	RequestKindChat       RequestKind = "chat"
	RequestKindResponses  RequestKind = "responses"
	RequestKindEmbeddings RequestKind = "embeddings"
	RequestKindRerank     RequestKind = "rerank"
)

// RequestBodyProfile/ResponseProfile/StreamProfile 是 shadow plan 中用于审计的
// 轻量协议画像；Phase 2 不执行转换，只记录计划。
type RequestBodyProfile string

type ResponseProfile string

type StreamProfile string

const (
	RequestBodyProfileOpenAIChat      RequestBodyProfile = "openai_chat"
	RequestBodyProfileOpenAIResponses RequestBodyProfile = "openai_responses"
	RequestBodyProfilePassthrough     RequestBodyProfile = "passthrough"

	ResponseProfileOpenAIChat      ResponseProfile = "openai_chat"
	ResponseProfileOpenAIResponses ResponseProfile = "openai_responses"
	ResponseProfilePassthrough     ResponseProfile = "passthrough"

	StreamProfileOpenAIChat      StreamProfile = "openai_chat_sse"
	StreamProfileOpenAIResponses StreamProfile = "openai_responses_sse"
	StreamProfilePassthrough     StreamProfile = "passthrough"
)
