// Package protocol 是 relaykit ↔ core 的唯一桥接层。
//
// 本包负责：
//   - 管理协议转换会话（Session），封装 relaykit 所需的转换上下文
//   - 加载和管理路由策略（Policy），控制 legacy/shadow/active 三种模式
//   - 提供请求/响应的解码、转换和桥接能力
//
// 依赖方向：core 不反引 protocol；protocol 不引 handler/providers/Gin。
// relaykit 模块路径：github.com/QuantumNous/new-api/relaykit
package protocol
