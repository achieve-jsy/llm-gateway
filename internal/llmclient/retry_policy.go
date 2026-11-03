package llmclient

import "context"

// 本文件实现 per-call「禁止重试」策略（媒体通道 A6）。
//
// 背景：上游的创建类接口（POST /v1/videos/...、POST /ent/v2/text2video、
// POST /v1/images/generations 等）没有幂等键，重发会在上游产生**第二个任务并重复计费**。
// 因此这类「提交」请求必须**至多发出一次**；重复提交的兜底只能靠网关自己落库的任务表
// （用任务 ID 去重），不能靠 HTTP 层重发。
//
// 两个等价入口，任一为真即生效；两者都不设置时行为与既有版本逐字节一致：
//
//  1. 调用级：WithNoRetry(ctx) —— 面向 media handler。handler 只持有 ctx、不构造
//     llmclient.Request，标记会随 ctx 传到 llmclient（仓库内 providers 均原样透传 ctx，
//     仅在 core.WithUpstreamProtocol 等处派生，不丢 value）。
//  2. 请求级：Request.NoRetry —— 面向 provider adapter。adapter 自己构造
//     llmclient.Request，可对已知的提交类端点直接声明。
//
// 覆盖范围：Do / DoRaw / DoPassthrough 的全部重试来源（429、5xx、非超时网络错误）。
// 其它重试相关路径的既有语义不变：
//   - DoStream 本来就不重试（见 client.go DoStream 注释）；
//   - 上游超时在 DoRaw/DoPassthrough 中本来就是终止性分支（isClientTimeoutGatewayError），
//     无论有没有本选项都只发一次；
//   - 熔断器**不会**产生重发：熔断打开时请求在真正发出前就被拒绝（返回 503），
//     只会更少发请求，因此无需额外处理。

// noRetryContextKey 是私有 key 类型，避免与其它包的 context value 冲突。
type noRetryContextKey struct{}

// WithNoRetry 返回一个标记了「本次调用至多发出一次上游请求」的 context。
//
// 该标记只影响由这个 ctx（及其派生 ctx）发起的 llmclient 调用。典型用法是媒体提交：
//
//	ctx = llmclient.WithNoRetry(ctx)
//	resp, err := provider.CreateImage(ctx, req) // 内部 429/5xx 都不再重发
//
// 而后续的查询/轮询请求应使用**不带**该标记的 ctx，以便继续享受 GET 重试（退避 3 次）。
// 传 nil 时原样返回 nil。
func WithNoRetry(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, noRetryContextKey{}, true)
}

// IsNoRetry 报告 ctx 是否携带「禁止重试」标记。
// context.WithValue / WithTimeout / WithCancel 派生出的 ctx 会保留该标记。
func IsNoRetry(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(noRetryContextKey{}).(bool)
	return v
}

// retryDisabled 汇总两个入口，判断本次调用是否必须「至多一次」。
// 任一路径为真即生效；都不设置时返回 false，等价于既有重试语义。
func retryDisabled(ctx context.Context, req Request) bool {
	return req.NoRetry || IsNoRetry(ctx)
}
