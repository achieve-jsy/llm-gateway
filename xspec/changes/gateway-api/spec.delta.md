# gateway-api 需求变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-api/spec.md (v1.3)

## 变更摘要

每次模型调用完成后，gateway-api 异步构造埋点事件并写入 Redis 队列，供 observability-hub 消费。埋点事件包含：模型标识、输入/输出 Token 数、调用耗时、HTTP 状态码、调用 Key、部门标识。

## 新增需求

### US-TRACK-EMIT-01 — 调用完成后异步埋点 (P0)

**作为**系统，**我想要**每次模型调用完成后自动发出埋点事件，**以便** observability-hub 能消费并建立用量和成本数据链路。

**功能说明**：
- 在模型调用完成（成功或失败）后，构造埋点事件结构体
- 埋点事件包含：模型标识、输入 Token 数、输出 Token 数、调用耗时（毫秒）、HTTP 状态码、调用 Key 标识、部门标识（dept_id）、时间戳、studio_id（预留，当前置空）
- 事件通过 Redis 队列异步发送，不阻塞调用响应返回
- 队列写入失败不影响调用结果（调用已成功返回给业务方）

**异常与边界**：
- Redis 不可达 → 埋点事件丢失，记录告警日志，不阻塞模型调用
- 调用失败（如上游超时）→ Token 数为 0 或从模型响应中获取的部分 Token 数，正常埋点
- 流式调用 → 在流完全结束后（收到 [DONE] 或异常中断）一次性发送埋点事件，Token 数取模型返回的 usage 字段

## 修改需求

无。本次变更仅新增埋点事件发送逻辑，不修改现有调用链路。

## 新增验收标准

| 编号 | 关联 | Given（前置状态） | When（触发动作） | Then（预期结果） | 优先级 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| AC-TE-01 | US-TRACK-EMIT-01 | Given 一次模型调用成功完成，返回了 usage 信息（input_tokens=100, output_tokens=50） | When 调用完成后构造埋点事件 | Then 事件包含模型/Token/耗时/状态/Key/部门字段，写入 Redis 队列成功 | P0 |
| AC-TE-02 | US-TRACK-EMIT-01 | Given Redis 不可达 | When 模型调用完成后尝试发送埋点事件 | Then 调用正常返回，埋点事件发送失败记录告警日志，不抛异常 | P1 |
| AC-TE-03 | US-TRACK-EMIT-01 | Given 一次流式调用正常完成 | When 流结束后 | Then 发送一次埋点事件，Token 数取模型返回的 usage 字段 | P0 |
| AC-TE-04 | US-TRACK-EMIT-01 | Given 一次模型调用因上游超时失败 | When 调用失败 | Then 发送埋点事件，状态码为超时错误码，Token 数为 0 或部分值 | P1 |