# LLM Gateway 变更历史

## [CHG-20260525-001] 模型组 + 智能调度（路由/负载/健康/切换） — 2026-05-26

- 新增模块：scheduler（嵌入 gateway-api 的调度引擎：路由决策、负载均衡、健康探测、故障切换）
- 更新模块：control-plane（模型组 CRUD、组内模型管理、故障切换配置）、gateway-api（组名调用 + scheduler 嵌入）、ops-console（模型组管理页面）
- 关键决策：scheduler 嵌入 gateway-api 而非独立服务；路由策略管理员可配（轮询/优先级/加权）；健康探测双机制（被动毫秒级 + 主动 30s 兜底）；模型组名替代模型名直传

---

## [CHG-20260523-001] SSO 登录 + Key 自助管理 — 2026-05-25

- 新增模块：auth（OIDC 登录、JWT 签发、API Key 鉴权中间件、用户与部门数据写入）
- 更新模块：control-plane（Key CRUD 与 user_id 强绑定）、gateway-api（API Key 鉴权中间件集成 + /v1/me/models）、ops-console（登录回调页、Key 管理页、模型列表页、首页文档）
- 关键决策：Key 采用 sk- 前缀 + UUID v7 生成，AES-256-GCM 加密存储 + SHA-256 hash 快速查找；禁用 Key 即时生效不依赖延迟刷新；鉴权拆分为 auth（SSO/JWT/中间件）与 control-plane（Key CRUD）两模块协作

---

## [CHG-20260521-002] 一键同步模型列表 — 2026-05-23

- 更新模块：control-plane（新增同步模型接口 sync-models + confirm）、ops-console（渠道列表页新增同步按钮 + SyncModelsDialog 弹窗）
- 关键决策：同步接口拆为两步（先拉取对比→再确认注册）；空列表防误禁用；不支持同步的渠道类型返回 400

---

## [CHG-20260521-001] 统一接入强兼容主链路 — 2026-05-21

- 新增模块：gateway-api（统一接入转发）、control-plane（渠道/模型治理）、ops-console（管理后台）
- 关键决策：Provider Registry + vendor_options 配置驱动；凭证 JSON 加密 + schema 校验；基于 new-api 重写转发层；模型标签独立表替代 capabilities 枚举
- 覆盖厂商：百炼、DeepSeek、Anthropic Claude、AWS Bedrock + 通用 OpenAI 兼容（15+ 厂商零代码接入）

---
