# LLM Gateway 项目变更记录

## 1.0.0 — 基线 (2026-06-03)

初始基线，记录 v1.0 已实现的全部模块能力。

| 模块 | 类型 | 内容 |
| :--- | :--- | :--- |
| gateway-core | web-backend | 请求鉴权、模型组调度、流式转发、Provider 热加载 |
| provider-adapters | web-backend | 17+ 厂商 adapter、Router/Registry、熔断重试、Fallback |
| control-plane | web-backend | 渠道/模型/Key/模型组 CRUD、Catalog 同步、配置下发、SSO |
| ops-console | web-frontend | 管理后台 SPA：渠道/模型/组/Key/Catalog/健康/设置 |
| opencode-plugin | _default | OpenCode 模型发现插件：一键安装、自动发现、自更新 |