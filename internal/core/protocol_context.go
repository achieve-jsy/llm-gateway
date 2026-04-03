package core

import (
	"context"
	"strings"
)

type upstreamProtocolKey struct{}

// UpstreamProtocolChat / UpstreamProtocolResponsesBridge 用于可观测标注。
const (
	UpstreamProtocolChat            = "chat"
	UpstreamProtocolResponsesBridge = "responses_bridge"
)

// WithUpstreamProtocol 标记本次上游实际走的协议路径。
func WithUpstreamProtocol(ctx context.Context, protocol string) context.Context {
	if ctx == nil || strings.TrimSpace(protocol) == "" {
		return ctx
	}
	return context.WithValue(ctx, upstreamProtocolKey{}, protocol)
}

// GetUpstreamProtocol 读取上游协议标记；未设置时返回空串。
func GetUpstreamProtocol(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(upstreamProtocolKey{}).(string)
	return v
}
