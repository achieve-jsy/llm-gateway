package tracesealer

// 退出码契约（systemd unit、CI、以及将来若改用 oneshot/timer 形态的运维脚本都依赖它）：
//
//	ExitOK     一轮/常驻循环正常结束
//	ExitFatal  启动期致命错误：配置非法、根目录不可用、锁被别的 sealer 持有
//	ExitFailed --once 跑完但本轮存在「可重试」或「永久」失败
//
// 【为什么是纯函数而不是 main 里的 switch】main 里的 switch 只能靠「编译出二进制
// 再跑」端到端验证，而变异批次有 30s 上限（工具用 -test.timeout 30s），编译一次
// 二进制就吃掉了大半预算。搬进纯函数后，三个顶层用例就能钉死它，而且可被变异杀死。
const (
	ExitOK     = 0
	ExitFatal  = 1
	ExitFailed = 2
)

// ExitCode 把「一轮的结果 + 错误」映射成退出码。
//
// 【为什么「有失败」必须退 2 而不是 0】常驻 unit（Type=simple）不看退出码（它会
// Restart），但运维手工跑 `--once`、以及将来若改用 timer 形态时，退出码是脚本**唯一**
// 能看见的信号：把「这一轮有东西没封成」报成 0，等于让「定时跑一次、失败就告警」的
// 脚本永远沉默 —— 而 sealer 的失败恰恰是那种「不喊就不会有人知道」的失败。
func ExitCode(res RoundResult, err error) int {
	if err != nil {
		return ExitFatal
	}
	if res.Failed() > 0 {
		return ExitFailed
	}
	return ExitOK
}
