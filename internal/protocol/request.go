package protocol

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// DecodeClaudeRequest 将原始 JSON 解码为 relaykit 的 ClaudeRequest DTO。
func DecodeClaudeRequest(rawJSON []byte) (*dto.ClaudeRequest, error) {
	var req dto.ClaudeRequest
	if err := json.Unmarshal(rawJSON, &req); err != nil {
		return nil, fmt.Errorf("decode claude request: %w", err)
	}
	return &req, nil
}

// DecodeResponsesRequest 将原始 JSON 解码为 relaykit 的 OpenAIResponsesRequest DTO。
func DecodeResponsesRequest(rawJSON []byte) (*dto.OpenAIResponsesRequest, error) {
	var req dto.OpenAIResponsesRequest
	if err := json.Unmarshal(rawJSON, &req); err != nil {
		return nil, fmt.Errorf("decode responses request: %w", err)
	}
	return &req, nil
}

// ConvertRequest 将已解码的请求通过 relaykit 转换为 TargetFormat。
// session 提供转换上下文（Meta）；request 为已解码的 DTO（如 *dto.ClaudeRequest）。
func ConvertRequest(session *Session, request any) (*relayconvert.RequestResult, error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	result, err := relayconvert.ConvertRequest(
		context.Background(),
		&session.Values,
		session.TargetFormat,
		request,
	)
	if err != nil {
		return nil, fmt.Errorf("convert request: %w", err)
	}
	return result, nil
}

// ConvertRequestVia 将请求按指定路径逐跳转换。
// path 为中间格式列表，最终到达 TargetFormat。
func ConvertRequestVia(session *Session, request any, path ...types.RelayFormat) (*relayconvert.RequestResult, error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	result, err := relayconvert.ConvertRequestVia(
		context.Background(),
		&session.Values,
		request,
		path...,
	)
	if err != nil {
		return nil, fmt.Errorf("convert request via: %w", err)
	}
	return result, nil
}
