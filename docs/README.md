# docs/ 索引与分类规范

> `AGENTS.md` 是目录入口，`docs/` 是事实源 (System of Record)。新增文档须在下表登记。

## 文档目录

| 文档 | 职责 | 业务相关 |
|------|------|:--------:|
| [README.md](README.md) | docs 索引与分类规范（本文件） | 否 |
| [architecture.md](architecture.md) | 系统架构、服务边界、分层与依赖方向 | 否 |
| [deployment.md](deployment.md) | CI/CD、环境拓扑、容器部署、nginx 路由 | 否 |
| [database.md](database.md) | 数据库部署位置、迁移机制与规范、Expand-Contract | 否 |
| [configuration.md](configuration.md) | 配置体系、参数注入 (YAML/env/CI Variables)、本地开发注入 | 否 |
| [observability.md](observability.md) | 日志、指标、链路追踪规范 | 否 |
| [guardrails.md](guardrails.md) | 项目防护栏铁律（架构/数据库/配置/质量/流程） | 否 |
| [frontend-design-guidelines.md](frontend-design-guidelines.md) | 前端 UI/UX 设计规范（Vue3 + Element Plus），含设计 token、三态、危险操作确认、动作密度等 | 否 |
| [opencode-models-discovery.md](opencode-models-discovery.md) | OpenCode 模型发现插件架构 | 是 |
| [media-api.md](media-api.md) | **媒体能力对外协议**：`/v1/images/generations`、`/v1/images/edits`、`/v1/videos` 三件套的请求/响应契约、错误码与异步任务语义（对接方入口） | 是 |
| [media-channels-integration-design.md](media-channels-integration-design.md) | 三渠道（gpt-image / Kling / Vidu）媒体链路接入设计：总体架构、上游契约、数据模型、计费口径 | 是 |
| [media-channels-implementation-plan.md](media-channels-implementation-plan.md) | 媒体链路分批实施计划、批次闸门与交付记录 | 是 |
| [image-generation-relay-analysis.md](image-generation-relay-analysis.md) | LiteLLM / New API 图像转发实现对比分析（接入选型依据） | 否 |
| [video-integration-comparison.md](video-integration-comparison.md) | 视频链路对比（Seedance 现状 / New API / LiteLLM）与演进建议 | 是 |

## 约定

1. **业务无关的工程基建文档**（上表"否"）统一进 `docs/`
2. **业务/模块规格**放 `xspec/`，不进 `docs/`
3. **命名**：小写连字符，一个主题一个文件
4. **时区**归 `deployment.md`；**参数注入**归 `configuration.md`
5. **新增文档**同步更新本表与 `AGENTS.md` 目录
