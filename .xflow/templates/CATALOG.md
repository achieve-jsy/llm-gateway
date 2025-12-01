# 模板目录索引（CATALOG）— 类型导读

> **用途**：供人阅读的「module_type 如何选型」指南。
>
> **机器 SSOT**：`CATALOG.yaml`（`module_type → 文件清单` 单一信源）
> **查询接口**：`node .xflow/scripts/xf-manifest.mjs --list-types` / `--module-type <type> [--phase <phase>]`
>
> 命令（split / design / detail / task 等）**不再**从本文件解析文件清单，全部通过 `xf-manifest.mjs` 动态查询。
> 新增或调整 `module_type` / 文件时，**只改 `CATALOG.yaml`**；本文件仅在类型定位或选型建议发生实质变化时同步更新。

---

## 一、已注册的 module_type 及其定位

### `web-backend`

Web 后端服务（REST API、分层架构、Controller / Service / Repository）。适用于 Go / Python / Java / Node.js 等有托管运行时的后端——Go / Python / Java 都归入此类型，开发模型相同，无需单独类型。

典型产物含 `contracts.md`（API 契约必有）。

### `web-frontend`

Web 前端界面（SPA / SSR、页面交互、组件体系）。React / Vue / Svelte / Solid 等声明式框架。

典型产物含 `contracts.md`（记录调用后端的 API 清单，与后端 `contracts.md` 双向对齐）。

### `native-app`

声明式 UI 原生 App：SwiftUI / Jetpack Compose / Flutter / React Native。

典型产物默认**不含** `contracts.md`：原生 App 的网络层通常通过后端模块的 `contracts.md` 定义；若本 App 存在独立对外协议（如自有 gRPC、蓝牙协议），可按需添加（此时 CATALOG.yaml 中 `contracts.md` 标记为 optional，在 detail 阶段由设计者判断是否生成）。

### `ai-agent`

AI Agent 架构（推理循环 + 工具调用 + 记忆管理）。含**专属** `evaluation.md`（Agent 评测方案：指标、数据集、通过阈值、离线评估流程），其他类型不生成。

> 若系统同时包含 HTTP 服务层和 Agent 推理层，应拆分为两个独立模块（`web-backend` + `ai-agent`），各自填写 `module_type`。

### `systems`

系统级后端（Zig / C / Rust），面向有**二进制体积 / RSS / 启动时间**硬约束或主动选择无 GC 运行时的场景。

典型产物**不含** `contracts.md`：系统模块的对外边界通常以二进制 ABI / IPC / 共享内存 / 序列化协议等形式存在，归入 `design.md` 描述；不需要独立的 REST/HTTP 契约文档。

> Go / Python / Java 属于 `web-backend`（托管运行时，开发模型相同），不是 `systems`。

### `xuanwu-activity`

玄武引擎活动模块（服务端 AwardLua + 客户端 FGUI，含完整活动生命周期：玩法规则、资格入口、协议、状态、配置、奖励、UI 交互、红点、强弹、埋点、支付）。

> 此类型一般搭配 `project_profile: xuanwu-activity` 使用（见本文件 §四 Profile 命中机制）。
> 所有模板的提炼规范、内容标准和规则文档存放于 `xwRef/xuanwu/xuanwu-activity/`。
> 专属 SKILL 链：`req-extractor → arch-designer → design-detailer → task-work → code-generator`。

**clarify 阶段（spec）**

| 文档类型 | 模板文件 | 状态 | 提炼规范 |
| :--- | :--- | :---: | :--- |
| spec（总纲索引） | `xuanwu-activity/spec.md` | 必有 | — |
| spec_overview | `xuanwu-activity/spec_overview.md` | 必有 | `activity/req/rules/overview.md` |
| spec_mechanic | `xuanwu-activity/spec_mechanic.md` | 必有 | `activity/req/rules/mechanic.md` |
| spec_eligibility | `xuanwu-activity/spec_eligibility.md` | 必有 | `activity/req/rules/eligibility.md` |
| spec_protocol | `xuanwu-activity/spec_protocol.md` | 必有 | `activity/req/rules/protocol.md` |
| spec_state-data | `xuanwu-activity/spec_state-data.md` | 必有 | `activity/req/rules/state-data.md` |
| spec_config | `xuanwu-activity/spec_config.md` | 必有 | `activity/req/rules/config.md` |
| spec_reward | `xuanwu-activity/spec_reward.md` | 必有 | `activity/req/rules/reward.md` |
| spec_ui-interaction | `xuanwu-activity/spec_ui-interaction.md` | 必有 | `activity/req/rules/ui-interaction.md` |
| spec_red-dot | `xuanwu-activity/spec_red-dot.md` | 必有 | `activity/req/rules/red-dot.md` |
| spec_force-popup | `xuanwu-activity/spec_force-popup.md` | 必有 | `activity/req/rules/force-popup.md` |
| spec_analytics | `xuanwu-activity/spec_analytics.md` | 必有 | `activity/req/rules/analytics.md` |
| spec_payment | `xuanwu-activity/spec_payment.md` | 按需 | `activity/req/rules/payment.md` |

**design 阶段（hld）**

| 文档类型 | 模板文件 | 状态 | 提炼规范 |
| :--- | :--- | :---: | :--- |
| hld（总纲） | `xuanwu-activity/hld.md` | 必有 | — |
| hld_server | `xuanwu-activity/hld_server.md` | 必有 | `activity/tech/rules/hld_server.md` |
| hld_client | `xuanwu-activity/hld_client.md` | 必有 | `activity/tech/rules/hld_client.md` |

**detail 阶段（design）—— 只做实现冻结，不含任务拆分**

| 文档类型 | 模板文件 | 状态 | 冻结规范 |
| :--- | :--- | :---: | :--- |
| design（总纲） | `xuanwu-activity/design.md` | 必有 | — |
| design_server | `xuanwu-activity/design_server.md` | 必有 | `activity/tech-design/rules/design_server_freeze.md` |
| design_client | `xuanwu-activity/design_client.md` | 必有 | `activity/tech-design/rules/design_client_freeze.md` |

**task 阶段 —— 基于冻结文档拆分微任务卡**

| 文档类型 | 模板文件 | 状态 |
| :--- | :--- | :---: |
| task | `xuanwu-activity/task.md` | 必有 |

> **玄武 SKILL 运行时产物（status: dynamic）**：
> 玄武专属 SKILL（arch-designer / design-detailer 等）在 design / detail 阶段会按需额外产出
> `hld_client_core` / `hld_client_entry-popup` / `hld_client_ui-analytics` / `hld_client_freeze` /
> `hld_server_protocol` / `hld_server_config` / `hld_server_freeze` 等子文档，
> 以及按需产出 `hld_client_fgui` / `hld_client_payment` / `hld_server_redis`；detail 阶段同理由
> design-detailer SKILL 产出 `design_*_freeze` / `design_*_tasks` / `design-report.md` 等。
> 这些 SKILL 运行时产物**已在 `CATALOG.yaml` 中以 `status: dynamic` 注册** —— scaffold 跳过铺骨架、
> validate 不强制其存在，但 manifest 输出会列出，approve 审批时识别为本阶段产出。
> 参考模板位于 `xwRef/xuanwu/xuanwu-activity/tech/publish/`，由 SKILL 内部消费，不归 xflow scaffold 管理。

> **已知局限**：`validate-rules.yaml` 的 semantic_checks 多数面向 web-backend 风格的 spec，
> 对 xuanwu-activity 的索引型 spec.md 和切面拆分的 spec_* 子文档不完全适用。短期内可能产生
> 无效警告或漏检，此局限已记录，后续独立评估是否为 xuanwu-activity 单独定制一套检查规则。

> **章节结构校验职责划分（`validations.skip_required_sections: true`）**：
> xuanwu-activity 下所有内容文档（spec* / hld* / design* / task）均由玄武 SKILL 按
> `xwRef/xuanwu/xuanwu-activity/*/bundle.yaml` 指向的玄武模板生成，章节结构的 SSOT 在
> 玄武 bundle 侧，不在 `.xflow/templates/xuanwu-activity/` 的骨架里。且 SKILL 自身已在
> Step 6 通过 reviewer Agent 按 `bundle.validation.checklists` 完成语义级校验。
> 因此 `CATALOG.yaml` 在本类型下声明 `validations.skip_required_sections: true`，
> `xf-validate` 跳过机械的 required-sections 字符串对比，保留 file-exists / no-placeholder /
> no-empty-section / delta-baseline 四条机械检查。章节合规性完全交回玄武 SKILL 负责。

> **框架约束引用**：
> - 客户端：`xwRef/xuanwu/frameworks/client/spec-index.md`
> - 服务端：`xwRef/xuanwu/frameworks/server/award-lua/spec-index.md`

### `_default`（通用兜底）

当用户指定的 `module_type` 未在 `CATALOG.yaml` 中注册时（如 `data-pipeline`、`infra`、`desktop-app`、`cli-tool` 等），`xf-manifest.mjs` 和 `xf-scaffold.mjs` 自动回退到 `_default`。通用模板提供类型无关的最小骨架（`spec` / `hld` / `design` / `task`；`data-model` 为 optional），确保任意模块类型都能正常走完工作流。

<!-- xflow:profile-types-begin -->
> 业务 profile `llm-gateway` 注册的 module_type（由 `xflow sync` 自动维护，请勿手工编辑本块）：

> （当前未注册任何业务类型）
<!-- xflow:profile-types-end -->

## 二、选型建议

| 如果你的模块…… | 建议类型 |
| :--- | :--- |
| 对外提供 HTTP/REST/RPC API，有托管运行时（Node / Go / Python / Java） | `web-backend` |
| 是浏览器端 SPA / SSR 前端 | `web-frontend` |
| 是手机端原生 App（iOS / Android / 跨端框架） | `native-app` |
| 围绕 LLM 推理循环、工具调用、记忆管理构建 | `ai-agent` |
| 用 Zig / C / Rust 编写，有体积 / RSS / 启动硬约束 | `systems` |
| **在玄武引擎上开发的游戏活动**（AwardLua 服务端 + FGUI 客户端，含完整活动生命周期） | `xuanwu-activity` |
| 是数据管道、基础设施、CLI、桌面端等其他类型 | 自定义类型（自动走 `_default`） |

---

## 三、通用模板（跨模块 / 变更级 / 项目级）

下列模板存放于 `.xflow/templates/_shared/`，不归属于任何 `module_type`，由对应命令按需使用：

| 文档 | 使用场景 |
| :--- | :--- |
| `change.yaml` | 变更元数据（`/xf/brainstorm` 或 `/xf/clarify` 产出） |
| `research.md` | 技术研究索引（课题汇总 + 浅研究 inline，xf-research subagent 产出） |
| `research-full.md` | 单课题完整深度分析（≥3 方案或有 benchmark 时拆至 `research/` 子文件） |
| `brainstorm.md` | 头脑风暴记录 |
| `integration.md` | 跨模块协调视图（`/xf/task` 产出） |
| `changelog.md` | 变更归档记录 |
| `constitution.md` | 项目宪章 |
| `product.md` | 产品定位 |

---

## 四、Profile 命中机制（项目级方言包）

`module_type` 解决的是**单个模块用哪套模板**的问题，`project_profile` 解决的是**整个项目的命令行为分支**问题。二者正交协作，共同构成 xflow 的领域方言能力。

### 核心约定

- **SSOT（运行时）**：`xspec/constitution.md` 里的 `project_profile` 字段（见 `_shared/constitution.md` 模板"项目配置"小节）；宪章创建后以此为准
- **SSOT（init 阶段）**：`.xflow/init.json` 的 `profiles.active`——`xflow init --profile` 写入；宪章尚未创建时，命令与 `xf-profile.mjs` 回退读取此值
- **取值**：`general`（默认）| `xuanwu-activity` | 其他自定义 profile
- **统一读取**：所有命令和脚本通过 `node .xflow/scripts/xf-profile.mjs` 获取当前 profile，不自行解析
- **方言包位置**：`.xflow/profiles/<profile>/<command-name>.md` 承载该 profile 下某命令的行为覆写

### profile 与 module_type 的关系（以 xuanwu-activity 为例）

- `project_profile: xuanwu-activity` → 锁定项目整体行为：xflow 链上 10 个命令有对应方言覆写（`/xf/constitution` → `/xf/archive`），其中 5 个命令（`clarify` / `design` / `detail` / `task` / `implement`）的关键产物生成步骤被替换为调用玄武 SKILL（`req-extractor` / `arch-designer` / `design-detailer` / `task-work` / `code-generator`），`/xf/split` 整条终止，`/xf/archive` 跳过 copy-new，`/xf/constitution` 首次创建用预置模板替代交互问答
- 对应的 `module_type: xuanwu-activity` → 锁定模块文件清单：scaffold 按 `CATALOG.yaml` 生成玄武模板骨架，SKILL 按各自的 `bundle.yaml` 填充内容（实际文件集由 bundle 决定，可能多于骨架）

典型组合：`xflow init --profile xflow:<name>` 在 init 阶段锁定 profile 并写入 `init.json`；用户首次执行 `/xf/constitution` 时**跳过步骤 1.5 交互**，直接将 init 选定的 profile 写入宪章。legacy 项目（init 未写 profile）仍走步骤 1.5 交互选择。以 `xuanwu-activity` 为例：选定后步骤 1.6（或 step 0 预加载）加载方言并展示预置宪章供用户 A/B 确认；从 `/xf/product` 起所有后续命令通过步骤 0 自动加载方言。

> **`/xf/constitution` 方言的作用边界**：原则上宪章只承载项目级原则约束，不该携带业务技术栈 / scaffold 流程等内容（这些归 `/xf/product` 方言）。**但**对于核心原则与技术方向已由行业实践固化的业务 profile（如 `xuanwu-activity`），允许用预置宪章模板替代 3 轮交互式问答。此类方言**仅覆写首次创建场景**——修订场景仍走通用流程，避免预置模板覆盖用户既有宪章。

门禁边界：方言的覆写动词**仅作用于命令步骤 0 之后的业务步骤**，各命令前置条件里的 `xf-gate.mjs` 门禁对方言不可见、不可改——门禁未通过 SKILL 不会被调用。

### 新增 profile 操作规则

1. 在 `.xflow/profiles/<new-profile>/` 下创建 `README.md` 说明定位，以及需要覆写的 command 对应的 `.md` 文件（如 `clarify.md` / `split.md`）
2. 覆写文件采用"步骤覆写"格式（操作：跳过 / 替换 / 补充；内容；理由）
3. 更新本文件 §四，在选型建议表（§二）或本节补充 profile 的选型指引
4. 不需要改动任何 command 文件或脚本（profile 机制通过 xf-profile.mjs + 统一触发点自动生效）

---

## 五、新增 module_type 操作规则

1. **编辑 `CATALOG.yaml`（必做）**：新增顶层键，补全 `description` 和 `files` 映射（每个文件标注 `status` / `phase`）
2. **创建模板目录（可选）**：在 `.xflow/templates/<type>/` 下放置对应的 `.md` 模板文件——若未创建，`xf-manifest.mjs` 会自动回退到 `_default` 模板
3. **更新本文件（可选）**：如新类型有独特定位或选型建议，在 §一 / §二 中补充说明；纯文件清单差异**不需要**在此文件同步
4. 验证：`node .xflow/scripts/xf-manifest.mjs --module-type <新类型> --all-phases` 能正确返回期望的文件清单
5. 运行测试：`node --test .xflow/scripts/__tests__/xf-manifest.test.mjs`

> 原则：CATALOG.yaml 是机器消费的 SSOT；CATALOG.md 仅承载"人需要理解的语义"。凡文件清单、必有/按需/专属标记等"结构化信息"，一律只在 YAML 中维护，不在 MD 中重复。
