// Package scheduler 模型组智能调度引擎
//
// scheduler 作为 Go package 嵌入 gateway-api 进程内运行，提供：
//   - 组内路由决策（轮询/优先级/加权）
//   - 实时健康探测（被动毫秒级 + 主动定时兜底）
//   - 故障切换（自动排除失败模型重路由）
//
// 通过 Go 函数调用提供服务（非 HTTP），与 gateway-api 共享进程生命周期。
package scheduler
