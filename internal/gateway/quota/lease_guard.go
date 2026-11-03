package quota

import (
	"fmt"
	"time"
)

// ConcurrencyLease 返回当前生效的并发租约时长。
//
// 租约是一次并发占用的最长有效期：ZSET 成员的 score = 占用时刻 + 租约，
// 到期即自然失效（逐条目到期是唯一的自愈权威）。
func ConcurrencyLease() time.Duration {
	return concurrencyLease
}

// ValidateLeaseCoversUpstream 校验并发租约是否**严格覆盖**运行时生效的上游超时。
//
// 为什么需要这道校验：上游超时由 httpclient.DefaultConfig() 在构造时从
// HTTP_TIMEOUT / HTTP_RESPONSE_HEADER_TIMEOUT 读出，getEnvDuration 同时接受
// 「整数秒」与「Go duration」两种写法，因此生效值可以被轻易放大。
// 一旦「生效上游超时 > 租约」，一个合法的长请求会在自己的生命周期内被判为过期，
// 它仍然占着上游连接，但槽位已被后续请求复用 —— 并发限额被静默削弱，
// 且只会表现为「偶发超限」，极难从现象反推。
//
// 返回 error 表示该配置不被接受。调用方（gateway 启动期）应打出**同时含两个具体数值**的
// 显著告警。本函数刻意不 panic / 不 os.Exit：把配置问题升级成「网关拒绝启动」是可用性故障。
// 同理这里也不钳制 httpclient 的取值 —— DefaultConfig() 是被鉴权、视频任务、绑定
// 三条链路共用的配置，静默改写会扩大影响面。
func ValidateLeaseCoversUpstream(lease, timeout, headerTimeout time.Duration) error {
	maxUpstream := timeout
	if headerTimeout > maxUpstream {
		maxUpstream = headerTimeout
	}
	if maxUpstream <= 0 {
		// 0 值表示该维度未设置，不构成风险。
		return nil
	}
	if lease <= maxUpstream {
		return fmt.Errorf(
			"并发租约 %v 未覆盖运行时生效的上游超时 %v（缺口 %v）；"+
				"合法长请求会在租约到期后仍持有上游连接，槽位被并发复用，并发限额被静默削弱。"+
				"请把 HTTP_TIMEOUT / HTTP_RESPONSE_HEADER_TIMEOUT 调到 %v 以内，或提高 concurrencyLease",
			lease, maxUpstream, maxUpstream-lease, lease,
		)
	}
	return nil
}
