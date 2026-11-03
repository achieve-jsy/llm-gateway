<!-- xflow:context:start -->
# xflow — 规范驱动开发框架

xflow 是一套内部 SDD（Specification-Driven Development）框架，通过结构化的阶段流程将需求从澄清推进到归档，确保设计制品的完整性和可追溯性。

## 核心架构

框架和数据**分离**到两个独立目录：

| 目录 | 定位 | 性质 |
| :--- | :--- | :--- |
| `.xflow/` | 框架（模板 + 脚本） | **只读**，随仓库分发 |
| `xspec/` | 项目数据（模块 + 变更 + 归档） | **读写**，运行时生成 |

```
.xflow/                              # 框架（只读）
├── templates/                       # 文档模板（类型映射 SSOT 见 CATALOG.yaml）
│   ├── CATALOG.yaml                 #   模块类型→文件清单 机器可读 SSOT（xf-manifest.mjs 读取）
│   ├── CATALOG.md                   #   类型导读（人工阅读，说明每个 module_type 的定位与选型）
│   ├── _shared/                     #   跨类型共享模板
│   │   ├── change.yaml              #     变更元数据模板（/xf/brainstorm 或 /xf/clarify 产出）
│   │   ├── research.md              #     技术研究索引（课题汇总 + 浅研究 inline）
│   │   ├── research-full.md         #     深研究子文件模板（≥3 方案或有 benchmark）
│   │   ├── brainstorm.md            #     头脑风暴记录
│   │   ├── integration.md           #     跨模块协调视图（/xf/task 产出）
│   │   ├── changelog.md             #     变更记录
│   │   ├── constitution.md          #     项目宪章
│   │   └── product.md              #     产品定位
│   ├── web-frontend/                #   Web 前端模块模板
│   │   └── spec / hld / design / data-model / contracts / task
│   ├── web-backend/                 #   Web 后端模块模板
│   │   └── spec / hld / design / data-model / contracts / task
│   ├── native-app/                  #   原生 App 模块模板（SwiftUI / Flutter 等）
│   │   └── spec / hld / design / data-model / task
│   ├── ai-agent/                    #   AI Agent 模块模板（推理循环 + 工具 + 记忆）
│   │   └── spec / hld / design / data-model / task / evaluation（专属）
│   ├── systems/                     #   系统级后端模板（Zig / C / Rust）
│   │   └── spec / hld / design / data-model / task
│   └── _default/                    #   通用兜底模板（自定义模块类型无专属模板时自动使用）
│       └── spec / hld / design / data-model / task
├── init.json                        #   项目 init 元数据（含 profiles.active，init 阶段 profile SSOT）
├── profiles/                        #   legacy 方言包（如 xuanwu-activity；yaml 业务 profile 不用此路径）
└── scripts/                         # 辅助脚本（Node.js ESM）
    ├── xf-init.mjs                  #   初始化 xspec/ 目录结构
    ├── xf-scaffold.mjs              #   从模板创建模块骨架文件（按 CATALOG.yaml 铺齐）
    ├── xf-manifest.mjs              #   模块文件清单查询（CATALOG.yaml 消费入口）
    ├── xf-profile.mjs               #   读取 project_profile（宪章 → init.json → general）
    ├── xf-current-change.mjs        #   活跃变更事实查询（active / phase / id）
    ├── xf-check-invariants.mjs      #   changes/ 结构合法性校验
    ├── xf-check-phase.mjs           #   命令 phase 入口校验
    ├── xf-archive.mjs               #   归档（复制新模块 / 搬运到 archive）
    ├── xf-deps.mjs                  #   任务依赖关系查询（--task / --feature / --module）
    ├── xf-gate.mjs                  #   阶段门禁检查（--to design / --to task）
    ├── xf-validate.mjs              #   产出自检脚本（--phase clarify / --phase detail）
    └── lib.mjs                      #   共享工具函数

xspec/                               # 项目数据（读写，运行时生成）
├── constitution.md                  # 项目宪章（全局不可违背的约束）
├── changes/                         # 当前活跃变更（同一时间只有一个，归档后清空）
│   ├── change.yaml                  #   变更元数据（id, phase, scope, affected_modules）
│   ├── .gates/                      #   阶段门禁审批记录（AI 禁止修改）
│   │   └── <stage>.json             #     各阶段审批状态（xflow approve 产出）
│   ├── brainstorm.md                #   头脑风暴记录（可选，/xf/brainstorm 产出）
│   ├── integration.md               #   跨模块协调视图（/xf/task 产出）
│   ├── changelog.md                 #   归档时生成的变更记录
│   └── <module>/                    #   每个受影响模块的设计制品
│       ├── spec.md                  #     模块级需求规范
│       ├── hld.md                   #     概要设计（/xf/design 产出）
│       ├── design.md                #     详细设计（/xf/detail 产出）
│       ├── data-model.md            #     数据模型
│       ├── contracts.md             #     接口契约
│       ├── research.md              #     技术研究索引（可选，xf-research subagent 产出）
│       ├── research/               #     深研究子文件目录（01-topic.md, 02-topic.md...）
│       └── task.md                  #     模块任务清单
├── modules/                         # Source of Truth（归档后持续增长）
│   ├── PRODUCT.md                   #   产品定位 + 模块结构 + 依赖关系
│   ├── CHANGELOG.md                 #   项目迭代记录（每次归档追加）
│   └── <module>/                    #   各模块最新状态
│       ├── module.yaml              #     模块元数据（版本、变更历史）
│       ├── spec.md / hld.md / design.md / data-model.md / contracts.md / task.md
│       ├── research.md              #     技术研究索引
│       ├── research/                #     深研究子文件
│       └──                          #     与 changes/<module>/ 结构一致
└── archive/                         # 历史变更快照
    └── <change-id>/                 #   每次归档保留 changes/ 的完整副本
        └── (与 changes/ 结构一致)

.claude/                             # Claude Code 平台目录（OpenCode 为 .opencode/，Cursor 为 .cursor/）
├── commands/xf/                     # xflow 命令（13 个 .md；business profile 定制已体现在此）
├── agents/xf-research.md            # 框架 subagent
└── agent-memory/xf-research/        # subagent 持久记忆（运行时生成）
```

> business profile 的定制在平台命令目录（如 `.opencode/commands/xf/`）——**直接执行该目录下的命令**即可。`.xflow/profiles/<profile>/` 仅 legacy 方言（如 `xuanwu-activity`）使用，yaml 业务 profile 勿去该路径找定制。

## 辅助工具

### xf-deps.mjs — 任务依赖关系查询

项目中每个模块的 `task.md` 通过 `depends:` 标注了任务间的依赖关系（包括跨模块引用如 `T002@auth`）。当你需要分析这些依赖时，`xf-deps.mjs` 是一个非常好用的工具——它会自动扫描所有模块的 task.md，解析 depends 标注，构建完整的依赖图，然后以清晰的出站/入站视角展示结果。相比于自己逐个打开 task.md 再人工匹配跨模块引用，这个脚本一条命令就能得到全貌，不容易遗漏。

```bash
# 查模块的全部跨模块依赖（出站 + 入站一目了然）
node .xflow/scripts/xf-deps.mjs --module user-auth

# 查单个任务的上下游链路
node .xflow/scripts/xf-deps.mjs --task T020 --module user-auth

# 按功能组名查依赖（子串匹配，跨模块也能查到）
node .xflow/scripts/xf-deps.mjs --feature "用户认证"

# 从已归档的 modules/ 扫描（默认扫描 changes/）
node .xflow/scripts/xf-deps.mjs --module user-auth --archived
```

什么时候用它比较好：
- 开始实现某个模块前，先跑一下 `--module` 了解它和其他模块的关联，有助于判断执行顺序
- 某个任务卡住时，用 `--task` 看看它的上游依赖是否都已完成
- 做制品审计时，用 `--module` 快速掌握跨模块依赖全景，发现悬空引用

### xf-manifest.mjs — 模块文件清单查询（CATALOG.yaml 消费入口）

xflow 的所有"该生成/读取哪些文件"的决策都从一个地方取：`.xflow/templates/CATALOG.yaml`（机器可读 SSOT）。命令和脚本不再硬编码文件清单，统一通过 `xf-manifest.mjs` 查询。

```bash
# 列出已注册的 module_type
node .xflow/scripts/xf-manifest.mjs --list-types

# 查询某类型在某阶段应生成的文件（含 required / optional / dynamic 分组）
node .xflow/scripts/xf-manifest.mjs --module-type web-backend --phase detail

# 查询完整清单（所有阶段）
node .xflow/scripts/xf-manifest.mjs --module-type ai-agent --all-phases

# 变更模块查 delta 清单
node .xflow/scripts/xf-manifest.mjs --module-type web-frontend --phase detail --change-type modified
```

什么时候需要关心它：
- 实现/扩展新命令时，**不要手写文件名列表**，调用 `xf-manifest.mjs` 动态获取
- 新增或调整模块类型的文件清单时，**只改 CATALOG.yaml**——所有命令、scaffold、validate、archive 都会自动跟随
- 自定义类型（如 `data-pipeline`、`infra`）未在 CATALOG.yaml 中注册时，manifest 自动回退到 `_default` 类型

### 模板与兜底机制

`CATALOG.yaml` 定义了 `module_type → 文件清单（含 status + phase）` 的完整映射，`CATALOG.md` 仅承载"类型定位 / 选型建议"等人阅读内容。当用户在 `/xf/split` 中使用自定义模块类型（如 `data-pipeline`、`infra`、`desktop-app` 等）且该类型未在 `CATALOG.yaml` 中注册时，`xf-manifest.mjs` / `xf-scaffold.mjs` 自动回退到 `_default` 通用模板，确保任何模块类型都能正常走完完整工作流。

### xf-current-change.mjs / xf-check-invariants.mjs / xf-check-phase.mjs — 活跃变更状态、结构合法性、phase 入口

事故防御分三层硬约束，互不重叠、各管一边：

| 脚本 | 管什么 | 防什么事故 |
| :--- | :--- | :--- |
| `xf-current-change.mjs` | 事实查询（active / phase / id / affected_modules） | — 仅是事实来源，不做判断，由其他脚本和 agent 消费 |
| `xf-check-invariants.mjs` | **结构层**：单 yaml 命名、目录与 affected_modules 一致、无 `ARCHIVE.md` 残留 | 防"多变更并存的脏状态"（多 yaml、孤儿目录） |
| `xf-check-phase.mjs` | **流程层**：当前 phase 是否为目标命令的合法入口 | 防"phase 跳跃 / 在 implement 阶段开新 clarify"等流程错乱 |

```bash
# 取当前活跃变更状态（完整 JSON）
node .xflow/scripts/xf-current-change.mjs

# 仅取某字段（裸值，方便 shell 直接判断）
node .xflow/scripts/xf-current-change.mjs --field active   # → true / false
node .xflow/scripts/xf-current-change.mjs --field phase    # → clarify / design / ...

# 校验 changes/ 结构是否合法（多 yaml / 孤儿目录 / ARCHIVE.md 残留）
node .xflow/scripts/xf-check-invariants.mjs
# 退出码：0=合法，2=违规，1=脚本错误

# 校验当前 phase 是否为目标命令的合法入口
node .xflow/scripts/xf-check-phase.mjs --command clarify
# 退出码：0=合法，2=违规，1=脚本错误
```

什么时候用：

- **agent 决定写入 `xspec/changes/` 之前**：必须先跑 `xf-current-change.mjs --field active` 判断是否能开新变更（详见"## 活跃变更保护"章节）
- `/xf/*` 命令在"前置条件"段落（先于方言加载）会自动跑 `xf-check-invariants.mjs` + `xf-check-phase.mjs`，任一违规整条命令立即终止
- 用户问"我这个 xspec 状态健康吗"时直接跑 `xf-check-invariants.mjs` 给报告
- 用户在错误的 phase 想跑命令时（最常见：implement 阶段还没归档、又想 clarify 新需求），`xf-check-phase.mjs` 会立刻挡下并打印诊断报告 + 修复建议

## Profile 机制（project_profile 与业务扩展）

`project_profile` 决定**整个项目** xflow 命令链的行为分支；`module_type` 决定**单个模块** scaffold 用哪套模板。二者**正交**——`profiles.active = retail-checkout` **不会**自动让 `/xf/split` 选用 `retail-checkout` 模块类型，需在 split 或 profile 定制中显式指定。

### SSOT 与统一读取

| 阶段 | SSOT | 说明 |
| :--- | :--- | :--- |
| 宪章已创建（运行时） | `xspec/constitution.md` 的 `project_profile` | 以宪章为准 |
| init 后、宪章未创建 | `.xflow/init.json` 的 `profiles.active` | init 阶段写入 |
| 均不可用 | `general` | legacy 项目或未指定 profile |

所有命令与脚本**禁止**自行解析宪章取 profile，统一调用：

```bash
node .xflow/scripts/xf-profile.mjs              # JSON：profile + source
node .xflow/scripts/xf-profile.mjs --value-only   # 仅 profile 名
```

`/xf/constitution` 首次创建时：若 `init.json` 已有 `profiles.active`（含 `general` 与任意 business profile），**跳过步骤 1.5 交互**，直接将 active 写入宪章；仅 legacy 项目（无 init.json 或无 `profiles.active`）才弹 profile 选择题。

### 两条并行的定制路径（不要混用）

| 路径 | 适用 | Agent 怎么消费 |
| :--- | :--- | :--- |
| **Yaml + Compose**（新业务 profile） | business profile 项目 | **直接读并执行**平台命令目录（如 `.opencode/commands/xf/`）里的命令文件 |
| **Legacy 方言** | 框架内置（如 `xuanwu-activity`） | 各命令**步骤 0**（前置条件与门禁之后）读 `.xflow/profiles/<profile>/<command>.md` 覆写后续步骤 |

yaml business profile：**以平台命令目录为准**；步骤 0 对 `.xflow/profiles/` 的查找通常是空操作，勿误以为定制「漏装」。

### Agent 禁令与常见误判

- **禁止**用 `profiles.active` 或 `project_profile` **推断**应用哪个 `module_type`——二者独立。
- **禁止**对 yaml business profile 去 `.xflow/profiles/<name>/` 找定制——已在平台命令目录里。
- **禁止**在步骤 0 之前执行方言覆写——前置条件（`xf-check-invariants.mjs` / `xf-check-phase.mjs` / `xf-gate.mjs`）**先于**步骤 0，方言不可绕过门禁与结构校验。
- legacy profile（如 `xuanwu-activity`）的步骤 0 方言加载**仍然有效**；`/xf/constitution` 首次创建时步骤 1.6 可补加载 legacy 预置宪章模板。

选型与 `module_type` 细节见 `.xflow/templates/CATALOG.md` §四。

## 阶段门禁

xflow 在关键阶段交接点设置了人工审批门禁，确保 AI 不能跳过人工审查直接推进流程：

```
clarify/brainstorm --> [自检] --> [人工审批] --> design
detail             --> [自检] --> [人工审批] --> task
```

**禁令（不可违反）：**
- **禁止** 直接创建、修改或删除 `xspec/changes/.gates/` 下的任何文件
- **禁止** 执行 `xflow approve` 命令
- **禁止** 向用户提供手动创建、修改 `.gates/` 文件的命令或方法（如 `echo ... > .gates/xxx.json`、`mkdir .gates` 等）。唯一合法的审批路径是用户自行在终端执行 `xflow approve`
- **禁止** 在门禁未通过时搜索、读取、分析 `.gates/` 目录或门禁脚本源码来试图理解或绕过机制
- 门禁检查未通过时 **必须立即终止当前命令**，仅输出下方固定提示，**不做任何后续操作**（不搜索、不分析原因、不提供替代方案）：

```
⛔ 门禁未通过：<脚本返回的 reason 原文>

请在终端执行：xflow approve
审批通过后重新执行当前命令。
```

门禁检查脚本：`node .xflow/scripts/xf-gate.mjs --to <target-stage>`。在 `/xf/design` 和 `/xf/task` 的前置条件中会调用此脚本。

## 产出自检

在每个人工门禁之前，AI 必须先执行产出自检（`xf-validate.mjs` 脚本检查 + 语义检查），确保输出符合模板结构和 xflow 规范。自检配置定义在 `.xflow/validate-rules.yaml` 中。

自检时间点：
- **clarify/brainstorm 完成后**：执行 `--phase clarify` 检查，通过后提示人工审批
- **detail 完成后**：执行 `--phase detail` 检查，通过后提示人工审批

**禁令（不可违反）：**
- **禁止** 修改 `.xflow/validate-rules.yaml` 文件
- **禁止** 跳过自检步骤直接输出完成报告
- **禁止** 在自检未通过时仍报告"全部通过"

## 设计阶段研究规则

xflow 的核心原则是**设计先行、研究先行**——在写一行代码之前消除技术不确定性。

**禁令（不可违反）：**
- 在 `/xf/design` 和 `/xf/detail` 阶段，**禁止**将技术不确定项标记为「待研究」「TODO」「留到实现阶段再确认」等任何形式的延迟处理
- 发现技术不确定项时，**必须**自主调用 `xf-research` subagent 当场研究解决，**不得询问用户**「是否需要研究」
- 研究结论必须回写到设计制品中（hld.md / design.md），不能只存在于 research.md 而设计文档仍留空

## 活跃变更保护

xflow 同一时间只允许一个活跃变更：`xspec/changes/` 下只装一份 `change.yaml` + 它声明的 `affected_modules` 子目录。这条不变量保护"agent 在前变更未归档时叠加新工作"这类事故（典型后果是 changes/ 堆多份 yaml、modules/ 永远为空、归档残留物滞留）。

**禁令（不可违反）：**

- **禁止**在 `xspec/changes/change.yaml` 已存在且 `status ≠ archived` 时直接覆写它开新变更。任何新变更必须先用 `/xf/archive` 归档当前变更
- **禁止**把过期 `change.yaml` 重命名为 `change-XXX.yaml`、`change.backup.yaml` 等"备份"——`changes/` 根目录的 yaml 必须叫且只叫 `change.yaml`
- **禁止**在 `xspec/changes/<module>/` 下创建未在 `change.yaml.affected_modules` 中声明的模块目录。要新增模块，**先**在 `affected_modules` 声明，**再**建目录
- **禁止**手动创建或保留 `ARCHIVE.md`——这是 `/xf/archive` 的中间产物，由命令完整流程消化
- **禁止**在结构不变量校验（`xf-check-invariants.mjs`）或 phase 入口校验（`xf-check-phase.mjs`）报错时绕过或"先做一部分"。校验失败必须立即终止当前操作，把诊断报告原样输出给用户，提示按报告建议修复后重试
- **禁止**在 `phase ∈ {design, detail, task, implement, archive}` 时把"新模块"并入当前 `affected_modules`。新模块属于新需求，应当：先归档当前变更（`/xf/archive`），再开新变更走 clarify。把不连贯的工作硬塞进同一个 changelog 是**严重反模式**

**写操作前必读**：准备写入 `xspec/changes/` 任何文件之前（覆写、新建、改名都算），先调用一次：

```bash
node .xflow/scripts/xf-current-change.mjs --field active
node .xflow/scripts/xf-current-change.mjs --field phase
```

- `active=false` → 可以正常开启新变更
- `active=true` 且 `phase ∈ {brainstorm, clarify}` → 同一变更内的 clarify 迭代是允许的
- `active=true` 且 `phase ∈ {design, detail, task, implement, archive}` → **禁止**直接开新变更，**也禁止**"并入"新模块到当前变更。必须先 `/xf/archive` 归档

`/xf/*` 命令的"前置条件"段落（先于方言加载）已通过 `xf-check-invariants.mjs` + `xf-check-phase.mjs` 把"结构合法 + phase 流程合法"做成了硬约束，但 agent 在自由对话 / 非 `/xf/*` 操作中决定动手前，仍须主动调用 `xf-current-change.mjs` 看清状态——硬约束是兜底层，主动核查才是第一道防线。

## Source of Truth 保护（xspec/modules/ 等只读目录）

xflow 通过 IDE 插件 hook（Cursor `preToolUse` / OpenCode `tool.execute.before`）实时拦截 agent 对受保护路径的写入，**阻止 agent 直接修改 SoT 文件**。这一层是工具调用拦截，独立于 `/xf/*` 命令前置条件，agent 即使在自由对话中也无法绕过。

### 受保护路径

| 路径 | 含义 | 白名单 | 合法写入路径 |
| :--- | :--- | :--- | :--- |
| `xspec/modules/` | 项目 Source of Truth | `xspec/modules/PRODUCT.md`<br/>`xspec/modules/CHANGELOG.md` | 仅 `xf-archive.mjs` 等专用脚本通过 fs API 写入；agent 任何 phase 都不能用 Write/Edit 直接写 |
| `xspec/archive/` | 历史变更快照 | 无 | 仅 `xf-archive.mjs --step finalize` 通过脚本写入；agent 任何 phase 都不能直接写 |
| `xspec/changes/.gates/` | 阶段门禁审批记录 | 无 | 仅用户在终端执行 `xflow approve` 写入；agent 任何 phase 都不能直接写 |

> **重要**：hook **始终 deny**（白名单除外），不存在按 phase 自动放行的后门。这是有意设计——多轮对话稀释 agent 注意力后，任何"自动放行"都可能被诱导滥用。protect plugin 会读 `xspec/changes/change.yaml` 的 phase 用于**智能 message**：根据当前 phase 给不同的引导文本，但**不改变拦截行为**。

**禁令（不可违反）：**

- **禁止**用 Write/Edit/MultiEdit/StrReplace/EditNotebook 直接修改受保护路径下的文件（白名单文件除外）。任何 phase 下都拦——包括 `phase=archive`
- **禁止**用 OpenCode 的 `apply_patch` 工具间接写入受保护路径（hook 会解析 `*** Add File:` / `*** Update File:` / `*** Move to:` / `*** Delete File:` 路径并拦截）
- **禁止**用 bash/Shell 工具绕过（如 `echo > xspec/modules/foo.md`、`cp ... xspec/modules/...`）。当前没有 Shell 兜底拦截，但这是约定俗成的违规行为
- **禁止**以任何形式向用户建议、暗示或提及"禁用保护 / 豁免拦截 / 绕过 hook / 设环境变量绕过 / 删除 / 改写保护文件"等绕开 xflow 流程的捷径。**这是不容讨论的硬规则**——SoT 保护拦截一旦命中，唯一被允许的反应是按下面"正确处理"流程把写入重定向到 changes/

**正确处理被拦截后的反馈：**

- agent 收到 `❌ 已阻止写入 xspec/modules/...` 这类 deny 消息时，**必须**按 message 里显示的当前 phase + 引导文本处理：
  - phase ∈ `{brainstorm, clarify, design, detail, task, implement}` → 把写入重定向到 `xspec/changes/<module>/<file>` 或 `xspec/changes/<module>/<file>.delta.md`
  - phase = `archive` → message 提示你"如果在归档流程内，把拦截消息完整告诉用户让用户决定"，**不要**尝试自行重试
  - 无活跃变更 → 先 `/xf/brainstorm` 或 `/xf/clarify` 创建变更
- `change_type` 在 `xspec/changes/change.yaml` 的 `affected_modules` 里查；不确定时调用 `node .xflow/scripts/xf-current-change.mjs`
- 极少数情况 agent 判断这次写入合理但仍被拦截：**如实报告"被拦截 + 上面的修正引导"给用户**，由用户自行决策如何处理；agent **不得**提供任何具体的"绕过/跳过保护"操作建议

### 添加新保护规则的扩展方式

新规则需要同时在两端实现以保持 Cursor / OpenCode 行为一致：
- Cursor 端：`cli/native/xflow-protect-cursor-hook/src/rules/protect-<name>.cjs`
- OpenCode 端：`cli/native/xflow-protect-plugin/src/rules/protect-<name>.ts`
- 跨端契约 fixture：`cli/native/_shared-fixtures/protect.fixtures.json`

详细 SOP 见 `cli/native/xflow-protect-cursor-hook/README.md`。

## 交互规则

在所有 `/xf/*` 命令中，当存在**多个可选方向**需要用户做选择时（无论是技术决策、模块划分、还是信息确认），应遵循以下引导原则以提升用户体验：

1. **能自主决策的不要问用户**：如果宪章、概要设计或技术约束已明确指向唯一合理方案，直接采用并记录理由，不要询问。只有存在**多个可行方案且各有实质性取舍**时才需要用户决策。
2. **给出 2–3 个具体选项**，每个选项用一句话说清含义。
3. **标明 1 个推荐项**，并说明推荐理由（1–2 句）。
4. **包含 1 个最小化选项**：最简单、最省事、维持现状或跳过的路径，降低用户决策成本。
5. **优先使用平台原生选择工具**：如果当前平台/IDE 提供了结构化选择工具（如 Cursor 的 `AskQuestion`、或其他 IDE 的等价选择 UI），**优先使用该工具**让用户通过点击或上下箭头选择，避免手动输入。
6. 当问题本身是**收集用户自有信息**（如「你的项目原则是什么」「产品解决什么问题」）而非多选决策时，自然提问即可，不必强行套选项格式。

**豁免**：`/xf/brainstorm` **第一阶段（发散）** 需要开放式引导激发创意，不受此规则约束。

## 命令与 Subagent

所有命令以 `/xf:` 为前缀，定义在 `.opencode/commands/xf/` 下。工作流：`constitution → product → split → brainstorm/clarify → design → detail → task → [analyze] → implement → archive`。辅助命令：`status`、`explore`。
<!-- xflow:context:end -->
