// 对抗性判据：并发租约必须覆盖「运行时生效的上游超时」，而不是覆盖一个常量。
//
// 背景：concurrencyLease 与上游 HTTP 客户端的超时之间原本没有任何耦合校验。
// internal/httpclient/client.go 的 DefaultConfig() 在**构造时**读 HTTP_TIMEOUT /
// HTTP_RESPONSE_HEADER_TIMEOUT，而 getEnvDuration 同时接受「整数秒」与「Go duration」两种写法，
// 因此运行时生效值可以轻易超过租约（例如该文件注释一度推荐的 600s）。
// 一旦「生效超时 > 租约」，合法长请求会在自己的生命周期内被判过期、槽位被别人复用，
// 并发限额被静默削弱。
//
// 本用例断言的是**已被采纳的修复方针（路线 a）**：启动期用
// ValidateLeaseCoversUpstream 对新生效值做校验，不满足即**拒绝并告警**。
// 断言对象始终是 httpclient.DefaultConfig() 的**返回值**与 store 实际使用的租约值，
// 而**不是两个常量之间的关系** —— 常量对常量的比较无法覆盖「运行时配置把生效值放大」这条路径。
//
// 本文件**不带 build tag**：它是常态防线，默认 `go test ./internal/gateway/quota/` 即执行。
// 判据不生效等于没有判据 —— 一份默认不被执行的证据，其有效性为 0。
//
// 运行方式：
//
//	go test ./internal/gateway/quota/ -run TestAdversarial_ -v
package quota

import (
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/httpclient"
)

// runtimeUpstreamTimeout 返回**运行时生效**的上游超时（读取构造函数返回值，而非常量）。
func runtimeUpstreamTimeout(t *testing.T) (timeout, headerTimeout time.Duration) {
	t.Helper()
	cfg := httpclient.DefaultConfig()
	return cfg.Timeout, cfg.ResponseHeaderTimeout
}

// assertLeaseGuardDecides 断言：校验函数对该配置作出的判定与「覆盖性」事实一致。
//
//   - 若生效上游超时已严格小于租约，校验必须**放行**（不得误报）；
//   - 若生效上游超时 ≥ 租约，校验必须**拒绝**，且信息中同时出现两个具体数值。
func assertLeaseGuardDecides(t *testing.T, scenario string) {
	t.Helper()
	timeout, headerTimeout := runtimeUpstreamTimeout(t)
	maxUpstream := timeout
	if headerTimeout > maxUpstream {
		maxUpstream = headerTimeout
	}
	lease := ConcurrencyLease()
	err := ValidateLeaseCoversUpstream(lease, timeout, headerTimeout)

	t.Logf("%s：生效 HTTP_TIMEOUT=%v，生效 HTTP_RESPONSE_HEADER_TIMEOUT=%v，取大=%v；租约=%v；校验结果=%v",
		scenario, timeout, headerTimeout, maxUpstream, lease, err)

	if maxUpstream < lease {
		if err != nil {
			t.Fatalf("%s：租约 %v 已覆盖生效上游超时 %v，校验却拒绝了：%v", scenario, lease, maxUpstream, err)
		}
		return
	}

	// 生效超时 ≥ 租约：这正是本判据要防住的配置，必须被拒绝。
	if err == nil {
		t.Fatalf(
			"%s：租约 %v 未覆盖运行时生效的上游超时 %v（缺口 %v），校验却放行。\n"+
				"合法修复二选一（不要靠临时抬高租约常量绕过）：\n"+
				"  (a) 启动/装配期校验 lease > max(生效 Timeout, 生效 ResponseHeaderTimeout)，不满足则拒绝并告警（当前采纳）；\n"+
				"  (b) 在配置解析处把 HTTP_TIMEOUT / HTTP_RESPONSE_HEADER_TIMEOUT 钳制在租约之下（会波及共享 HTTP 客户端，未采纳）。",
			scenario, lease, maxUpstream, maxUpstream-lease,
		)
	}
	msg := err.Error()
	if !strings.Contains(msg, lease.String()) || !strings.Contains(msg, maxUpstream.String()) {
		t.Fatalf("%s：拒绝信息必须同时给出两个具体数值（租约 %v、生效超时 %v），实际为：%s",
			scenario, lease, maxUpstream, msg)
	}
}

// TestAdversarial_LeaseCoversRuntimeUpstreamTimeout 覆盖「未设置 / 整数秒 / Go duration /
// 响应头超时单列 / 两侧同时放大」五种配置形态。
func TestAdversarial_LeaseCoversRuntimeUpstreamTimeout(t *testing.T) {
	cases := []struct {
		name       string
		timeout    string
		headerTime string
	}{
		{"env_unset_生效值应为默认", "", ""},
		{"HTTP_TIMEOUT=600_注释一度推荐的值", "600", ""},
		{"HTTP_TIMEOUT=20m_Go duration 写法", "20m", ""},
		{"HTTP_RESPONSE_HEADER_TIMEOUT=600_单列放大", "", "600"},
		{"两侧同时放大到600", "600", "600"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 空串对本代码路径等价于「未设置」（getEnvDuration: val == "" → 默认值）。
			t.Setenv("HTTP_TIMEOUT", tc.timeout)
			t.Setenv("HTTP_RESPONSE_HEADER_TIMEOUT", tc.headerTime)
			assertLeaseGuardDecides(t, tc.name)
		})
	}
}

// TestAdversarial_InvalidTimeoutFallsBackToSafeDirection 固化「无法解析」时的现状：
// getEnvDuration 对非法取值**静默**回落到默认值。方向上安全（更短的上游超时不会
// 突破租约），但静默回落会让运维以为配置已生效，故建议同时打一条 warn。
func TestAdversarial_InvalidTimeoutFallsBackToSafeDirection(t *testing.T) {
	t.Setenv("HTTP_TIMEOUT", "abc")
	t.Setenv("HTTP_RESPONSE_HEADER_TIMEOUT", "abc")

	timeout, headerTimeout := runtimeUpstreamTimeout(t)
	t.Logf("非法取值 abc 的生效结果：Timeout=%v，ResponseHeaderTimeout=%v", timeout, headerTimeout)

	if timeout > 300*time.Second || headerTimeout > 300*time.Second {
		t.Fatalf("非法取值的回落方向不安全：应回落到默认 300s，实际 Timeout=%v header=%v", timeout, headerTimeout)
	}
	if err := ValidateLeaseCoversUpstream(ConcurrencyLease(), timeout, headerTimeout); err != nil {
		t.Fatalf("回落之后租约仍未覆盖生效值：%v", err)
	}
}

// TestAdversarial_ConfigIsReadAtRuntime 是判据的**自检**：证明 DefaultConfig() 确实在
// 调用时读取环境变量（因此上面的子用例不是恒真断言）。若这条失败，说明「运行时生效值」
// 这个前提不成立，上面的判据也就失去意义。
func TestAdversarial_ConfigIsReadAtRuntime(t *testing.T) {
	t.Setenv("HTTP_TIMEOUT", "600")
	t.Setenv("HTTP_RESPONSE_HEADER_TIMEOUT", "600")
	timeout, headerTimeout := runtimeUpstreamTimeout(t)
	if timeout != 600*time.Second || headerTimeout != 600*time.Second {
		t.Fatalf("DefaultConfig() 未反映运行时环境变量：Timeout=%v header=%v，期望均为 600s", timeout, headerTimeout)
	}
	t.Logf("自检通过：DefaultConfig() 在调用时读取 env，生效值 = %v / %v", timeout, headerTimeout)
}
