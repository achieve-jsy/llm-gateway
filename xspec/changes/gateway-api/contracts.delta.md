# gateway-api 接口契约变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-api/contracts.md (v1.3.0)

## 变更摘要

本次变更不新增或修改对外 API 接口。gateway-api 的内部变更仅涉及：
1. 调用完成后异步写入埋点事件（内部行为，对调用方透明）
2. AuthMiddleware 身份解析增强（内部行为，鉴权结果不变）

## 无接口变更

本次变更的所有组件（Collector、batch writer、流式 usage 回填、身份解析）均为内部行为，不暴露新的 HTTP 端点，也不修改现有端点的请求/响应格式。

对外接口保持不变：
- POST /v1/chat/completions — 不变
- GET /v1/models — 不变
- 所有鉴权端点 — 不变