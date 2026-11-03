package protocol

import (
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/google/uuid"
)

// Session 封装 relaykit 转换所需的上下文。
// 每个请求对应一个 Session，生命周期贯穿解码 → 转换 → 桥接 → 响应回程。
type Session struct {
	// Values 实现 relaykit 的 convmeta.Meta 接口，为转换器提供运行时上下文。
	Values convmeta.Values

	// SourceFormat 标记请求的源协议格式（如 "claude"）。
	SourceFormat types.RelayFormat

	// TargetFormat 标记目标协议格式（如 "openai"）。
	TargetFormat types.RelayFormat

	// Diagnostics 收集转换过程中产生的诊断信息。
	Diagnostics []types.ConversionDiagnostic

	// ID 唯一标识本次转换会话。
	ID string

	// ToolState 记录 Responses 工具编码信息，供响应回程时恢复。
	ToolState *convmeta.ResponsesToolState
}

// NewSession 创建转换会话。
//
// source/target 为协议格式；opts 为请求级转换选项；model 为原始请求模型名；
// upstreamModel 为上游实际模型名（调度后的）。
func NewSession(source, target types.RelayFormat, opts convmeta.Options, model, upstreamModel string) *Session {
	return &Session{
		SourceFormat: source,
		TargetFormat: target,
		ID:           uuid.New().String(),
		Values: convmeta.Values{
			OriginModelName:   model,
			UpstreamModelName: upstreamModel,
			Options:           &opts,
		},
	}
}
