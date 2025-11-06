# LLM Gateway 变更历史

## [CHG-20260521-001] 统一接入强兼容主链路 — 2026-05-21

- 新增模块：gateway-api（统一接入转发）、control-plane（渠道/模型治理）、ops-console（管理后台）
- 关键决策：Provider Registry + vendor_options 配置驱动；凭证 JSON 加密 + schema 校验；基于 new-api 重写转发层；模型标签独立表替代 capabilities 枚举
- 覆盖厂商：百炼、DeepSeek、Anthropic Claude、AWS Bedrock + 通用 OpenAI 兼容（15+ 厂商零代码接入）

---
