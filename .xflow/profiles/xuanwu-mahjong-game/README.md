# xuanwu-mahjong-game Profile 方言包

本目录定义 `project_profile: xuanwu-mahjong-game`（玄武麻将游戏项目）下各 `/xf/*` 命令相对于通用 profile 的行为差异。

## 触发条件

当 `xspec/constitution.md` 中 `project_profile` 字段为 `xuanwu-mahjong-game` 时，以下命令在执行到"profile 分支加载"触发点会读取本目录下对应的同名 `.md` 文件并应用其中的覆写指令。

命令读取 profile 的统一入口：

```bash
node .xflow/scripts/xf-profile.mjs --value-only
```

## 文件清单

| 方言文件 | 对应命令 | 覆写目的 |
| :--- | :--- | :--- |
| `constitution.md` | `/xf/constitution` | **仅首次创建场景**覆写：跳过 step 2 的 3 轮交互式问答，直接读 `xwRef/xuanwu/xuanwu-mahjong-game/constitution.md` 作候选宪章给用户确认（A. 直接用 / B. 基于此改）。场景二（修订）不覆写 |
| `product.md` | `/xf/product` | scaffold 前置检查（client/+server/game_server/Logic/mahjong_server/ 缺失则终止）+ 简化 PRODUCT.md 生成：单模块 `mahjong-game` 锁定、新增需求文档位置字段（Q3）、双端代码路径写入 PRODUCT.md（不进宪章） |
| `split.md` | `/xf/split` | 终止整条命令：单模块架构下无需拆分 |
| `clarify.md` | `/xf/clarify` | **时序重排**：追加 step 1.7（change.yaml + scaffold + 调 `req-extractor` SKILL）放到提问之前；step 2 的提问改由 **SKILL 未明确项**驱动（必答，受 SKILL ≤8 间接约束）+ AI 补充题 ≤3；跳过模块影响确认题；step 4 改为"答案事务式批量回写 spec_*.md" |
| `brainstorm.md` | `/xf/brainstorm` | 同 clarify 的锁定规则，收敛阶段直接写入单模块结构 |
| `design.md` | `/xf/design` | 跳过 step 2.2 架构方向探索 + step 2.3 架构方案；追加 `proto-generator` 的 proto 命名契约与锚点校验；替换 step 5 为调用 `arch-designer` SKILL |
| `detail.md` | `/xf/detail` | 追加 step 0.5 调用 `proto-generator` 生成 `cm_protocol.proto` 并编译；追加 step 1.5（契约与 proto 产物校验）；跳过 step 3（方案探索全部交互）；替换 step 4 为调用 `design-detailer` SKILL |
| `task.md` | `/xf/task` | 替换 step 2+3 任务拆解为调用 `task-work` SKILL；step 4 跨模块对齐跳过；step 5 生成极简 N/A 版 `integration.md` |
| `implement.md` | `/xf/implement` | 替换 step 1.5 验证命令探测、step 3 逐任务执行、step 5+8 验证为调用 `code-generator` SKILL；step 7 跨模块集成跳过 |
| `archive.md` | `/xf/archive` | `copy-new` 跳过（不沉淀到 modules/）；归档时以业务名在 change.yaml.name 体现 |

共 **10 个方言文件 + 本 README = 11 个文件**，覆盖 xflow 全链条命令。

> **设计权衡：宪章方言的作用边界**。原则上宪章只承载项目级原则约束，不该携带业务技术栈 / 代码路径 / scaffold 流程等内容（这些归 `/xf/product` 方言）。但玄武麻将游戏项目的三条核心原则（协议一致 / 麻将游戏隔离 / 安全优先）+ 双端技术栈（TypeScript + FGUI / C++）**已由行业实践固化**，逐条追问用户徒增交互成本且信息量低。因此 `constitution.md` 方言**仅在首次创建场景**发挥作用：用预置模板替代 3 轮问答，允许用户一键确认或在模板上做局部调整；场景二（修订）不覆写，避免 reference 模板覆盖用户既有宪章。scaffold 前置检查与 client/server 路径语义仍归 `/xf/product` 方言（业务环境检查归业务命令负责）。

## 核心差异（相对 general profile）

| 维度 | general | xuanwu-mahjong-game |
| :--- | :--- | :--- |
| 仓库 ↔ 业务关系 | 一仓库多模块 | 一仓库 = 一个麻将游戏（串行） |
| 模块数量 | 按业务划分多模块 | 恒定单模块，目录名 `mahjong-game` |
| 模块类型 | 用户在 split 选择 | 锁定 `xuanwu-mahjong-game` |
| `/xf/split` | 首轮 & 后续调整必走 | 终止 |
| `/xf/product` | 完整产品定位 + 模块结构 | 简化定位（不含模块规划） |
| `/xf/clarify` spec 填充 | 通用模板直接填（AI 凭 PRODUCT/宪章揣测歧义后才问） | 先由 `req-extractor` SKILL 按 `req/bundle.yaml` 提取 spec + 未明确项清单，**再基于提取结果**向用户针对性提问 |
| `/xf/design` 产物 | 单文件 `hld.md` | 由 `arch-designer` SKILL 按 `tech/bundle.yaml` 产出双端冻结文档集 |
| `/xf/detail` 产物 | `design.md / data-model.md / contracts.md` | 由 `design-detailer` SKILL 按 `tech-design/bundle.yaml` 产出双端冻结 + 任务拆分 + 自检 + 报告 |
| `/xf/task` 产物 | 通用数字任务格式 + integration.md | 由 `task-work` SKILL 按玄武语义 task id 规范产出；integration.md 为 N/A 占位 |
| `/xf/implement` 执行 | 逐任务 ReadLints + npm 验证 | 由 `code-generator` SKILL 接管 DAG/波次/write set/client+server checker |
| modules/ 归档 | copy-new 沉淀历史 | 跳过 copy-new（麻将游戏不沉淀） |
| archive/ 归档 | 按 change-id | 按 change-id（业务名在 change.yaml.name） |

## SKILL 链路（xflow 阶段 ↔ xuanwu SKILL）

```
/xf/constitution（步骤 1.5 询问 profile → 步骤 1.6 延迟加载方言 → 首次创建用预置宪章模板；修订不覆写）
        → /xf/product   (追加 scaffold 前置检查，缺 client/+server/game_server/Logic/mahjong_server/ 则提示 scaffold.sh 后终止)
        → /xf/clarify   (追加 step 1.7)   → req-extractor → 基于未明确项针对性提问 → 事务式回写
        → [门禁: clarify → design]
        → /xf/design    (追加 proto 锚点校验；替换 step 5) → proto-generator → arch-designer
        → /xf/detail    (追加 proto 生成；替换 step 4)     → proto-generator → design-detailer
        → [门禁: detail → task]
        → /xf/task      (替换 step 2+3)   → task-work
        → /xf/implement (替换 step 3)     → code-generator
        → /xf/archive   (跳过 copy-new)
```

门禁**永远在方言之前执行**：方言的覆写动词仅作用于步骤 0 之后的业务步骤；`/xf/design` / `/xf/task` 前置条件里的 `xf-gate.mjs` 门禁硬停对方言不可见、不可改——门禁未通过则整条命令终止，SKILL 不会被调用。

## 覆写指令格式约定

每个方言文件采用"步骤覆写"格式，每条指令明确三要素：

1. **操作**：`跳过 <步骤>` / `替换 <步骤>` / `追加 <位置>` / `终止整条命令`
2. **内容**：具体改为什么或加什么
3. **理由**：一句话说明为什么这样做

## 新增 profile

参考 `.xflow/templates/CATALOG.md` §四 "Profile 命中机制"。
