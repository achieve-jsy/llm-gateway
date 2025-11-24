基于所有设计文档生成结构化任务清单——为每个受影响模块生成 task.md。

## 用户输入

```
$ARGUMENTS
```

无需额外参数，直接基于当前活跃变更进行任务拆解。

## 前置条件

- `xspec/constitution.md` 必须存在
- `xspec/changes/` 目录下各模块的 detail 阶段产物完整——具体清单由 `xf-manifest.mjs --phase detail` 驱动，不同 `module_type` 要求不同（如 `systems` 不含 `contracts.md`）
- `xspec/changes/change.yaml` 的 phase 为 `detail` 或 `task`
- **结构不变量校验（硬停）**：执行 `node .xflow/scripts/xf-check-invariants.mjs`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ 结构不变量违反：<脚本输出的报告>

  请按报告建议修复后重试。
  ```
- **phase 入口校验（硬停）**：执行 `node .xflow/scripts/xf-check-phase.mjs --command task`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ phase 入口不合法：<脚本输出的报告>

  请按报告建议处理后重试。
  ```
- **门禁检查（硬停）**：执行 `node .xflow/scripts/xf-gate.mjs --to task`。如返回 `pass:false`，**必须立即终止本命令**，仅输出以下固定提示后结束（不搜索、不分析、不提供替代方案、不教用户手动写 `.gates/` 文件）：
  ```
  ⛔ 门禁未通过：<脚本返回的 reason 原文>

  请在终端执行：xflow approve
  审批通过后重新执行 /xf/task
  ```

---

## 步骤 0：Profile 分支加载（始终先于其他步骤执行）

1. 执行 `node .xflow/scripts/xf-profile.mjs --value-only` 读取当前 `project_profile`
2. 显眼打印一行：`📌 project_profile = <profile>`
3. 如果 `.xflow/profiles/<profile>/task.md` 存在，**读取该文件**并作为本次运行的行为覆写规则优先执行：
   - 指令 `终止整条命令` → 按方言固定输出打印并结束，**不执行下文任何步骤**
   - 指令 `跳过 <步骤>` → 执行到该步骤时直接跳过（例如 xuanwu-activity 下跳过步骤 4 跨模块依赖对齐）
   - 指令 `替换 <步骤>` → 改用方言文件中的替代内容（例如 xuanwu-activity 下替换步骤 2+3 为调用 task-work SKILL）
   - 指令 `追加 <位置>` → 在该位置附加方言文件的内容
4. 方言文件不存在或 profile 为 `general` 时，按下文通用流程执行

> 原则：本文件只维护通用逻辑；领域方言统一在 `.xflow/profiles/<profile>/` 下管理。**禁止**在本文件硬编码 `if profile == xxx` 分支。
> 门禁边界：方言的覆写动词**仅作用于步骤 0 之后的业务步骤**。"前置条件"段落（含 `xf-check-invariants.mjs` 结构校验与 `xf-gate.mjs --to task` 门禁硬停）对方言不可见、不可改——硬停未通过整条命令已在此之上终止，SKILL 不会被调用。方言本身也必须遵守 xflow 的结构不变量。

---

## 步骤 1：加载并分析所有设计文档

对每个模块通过 manifest 查询应加载的文档清单（而非硬编码）：

```bash
node .xflow/scripts/xf-manifest.mjs --module-type <module_type> --all-phases --change-type <added|modified>
```

按 manifest 返回的文件清单逐一读取并抽取要点，典型映射如下：

- `spec.md` / `spec.delta.md` → 提取功能点和验收标准（BDD 格式的 Given/When/Then）
- `hld.md` / `hld.delta.md` → 提取架构方向和技术选型约束
- `design.md` / `design.delta.md` → 提取详细技术实现要点和设计决策
- `data-model.md` / `data-model.delta.md` → 提取数据层任务
- `contracts.md` / `contracts.delta.md` → 提取接口实现任务（仅 manifest 包含此项时）
- `evaluation.md` / `evaluation.delta.md` → 提取 Agent 评测任务（仅 `ai-agent`）
- `research.md` → 从课题索引表提取技术决策约束（如存在；深研究详见 `research/` 子文件）

建立任务与需求的追溯关系：每个功能实现任务必须关联对应的验收标准编号（AC-XX）。



## 步骤 2：按模块拆解任务

对**每个受影响模块**独立拆解任务。

### 任务格式（严格遵守）

```
- [ ] T{NNN} [P?] {优先级} 任务描述（含具体文件路径）
  depends: T{NNN}, T{NNN}@{other-module}
```

| 标记 | 含义 |
| :--- | :--- |
| `T{NNN}` | **模块内唯一**编号，每个模块从 T001 起独立编号 |
| `[P]` | 可并行标记：仅当操作不同文件且无依赖未完成任务时标注 |
| 优先级 | P0（必须）/ P1（重要）/ P2（可选） |
| `depends:` | 依赖标注（缩进换行）。模块内：`T010`；跨模块：`T020@user-auth`。无依赖则不写此行 |

**格式约束**：
- ✅ 有依赖：
  ```
  - [ ] T012 P0 API 接口对接 `src/services/api.ts`
    depends: T010, T004
  ```
- ✅ 有跨模块依赖：
  ```
  - [ ] T050 P1 与后端接口联调验证
    depends: T022, T020@user-auth
  ```
- ✅ 无依赖：
  ```
  - [ ] T001 [P] P0 项目初始化与依赖安装
  ```
- ❌ `- [ ] 创建用户模型`（缺编号、路径）
- ❌ `T001 创建模型`（缺 checkbox）

### 阶段结构

**阶段 1：环境与基础**
- 项目结构初始化、依赖、配置
- 公共组件/中间件/工具函数
- ⚠️ 此阶段完成前不启动后续工作

**阶段 2：数据层**
- 数据库迁移脚本（后端）/ TypeScript 类型定义（前端）
- 基础 CRUD / API 层封装

**阶段 3：功能实现**
- **按 spec.md 中的用户故事（US-XX）组织**，每个故事为一个独立子阶段
- 用户故事按优先级排列：P0 故事在前，完成即为 MVP
- 每个故事子阶段须包含：
  - **目标**（一句话）
  - **独立验证方式**（如何证明这个故事独立可用）
  - **对应 AC 编号**
- 故事间尽量解耦：后续故事不应破坏已完成故事的功能
- 每个故事完成后设置 **检查点**，标注"可独立验证和演示"

**阶段 4：集成与收尾**
- 前后端联调（通过 `depends: TNNN@other-module` 标注跨模块依赖）
- 性能优化、安全加固、代码审查

### 宪章驱动任务

根据 constitution.md 自动补充：
- 测试覆盖率要求 → 添加测试任务
- Code Review 要求 → 添加 Review 任务
- 性能基线 → 添加性能验证任务
- 安全要求 → 添加安全检查任务

## 步骤 3：生成模块级 task.md

为每个模块基于对应类型模板填充 `xspec/changes/<name>/task.md`。

模板查找规则：调用 `xf-manifest.mjs --module-type <module_type> --phase task` 确认该类型在 task 阶段应生成的文件（当前所有类型均为 `task.md`），并取 manifest 返回的 `resolved_type` 作为模板目录名（自定义类型自动回退 `_default`）。

## 步骤 4：跨模块依赖对齐检查

所有模块的 task.md 生成完毕后，执行一致性检查：

1. 遍历所有模块 task.md 中的 `depends: TNNN@module` 跨模块引用
2. 检查目标模块中对应编号的任务是否存在
3. 输出检查结果：

```
🔗 跨模块依赖检查

✅ login-page T050 → user-auth T020（认证接口）：存在
✅ login-page T012 → user-auth T010（用户数据模型）：存在
❌ login-page T035 → payment T015：目标任务不存在

结果：2 通过 / 1 不一致
```

如有不一致，提示用户确认是否需要调整（可能是编号笔误或遗漏任务）。

也可使用 `xf-deps.mjs` 脚本查询完整依赖关系：

```bash
node .xflow/scripts/xf-deps.mjs --module <module-name>
```

## 步骤 5：生成变更级 integration.md

基于 `.xflow/templates/_shared/integration.md` 模板，生成 `xspec/changes/integration.md`（跨模块协调视图）：

1. **模块任务索引**：列出所有受影响模块及其 `task.md` 的相对路径链接，不复制任务内容
2. **跨模块执行顺序**：用 mermaid 图表示模块间依赖和推荐启动顺序
3. **跨模块集成任务**：仅收录跨越模块边界的集成任务（前后端联调、API 契约验证、E2E 测试等）
   - 编号从 I001 起，与模块内 T 编号区分
   - 依赖标注使用 `depends: TNNN@module` 指向具体模块任务
   - **不包含任何模块内部任务**
4. **质量门禁**：任务完成度、联调通过、验收覆盖、E2E 测试、代码审查、部署前检查

> `integration.md` 是「跨模块协调视图」，不是「任务汇总表」。
> 更细粒度的任务依赖关系可通过 `xf-deps.mjs` 脚本动态查询。

## 步骤 6：验证任务质量

检查每个任务是否满足：

| 维度 | 要求 |
| :--- | :--- |
| 可执行性 | 描述具体，包含文件路径，AI 可直接执行 |
| 粒度 | 单个任务不超过 2 小时工作量 |
| 产出物 | 每个任务有明确的可验证产出 |
| 追溯性 | 功能实现任务关联验收标准编号 |
| 并行标注 | 可并行的任务标了 `[P]`，不可并行的没有误标 |
| 独立交付性 | 每个用户故事子阶段完成后可独立验证，不依赖后续故事 |
| 依赖标注 | 有前置依赖的任务标了 `depends:`，跨模块引用带 `@module` |
| 检查点 | 每个阶段/功能组后有 ✓ 检查点 |
| 格式 | 严格遵守 checkbox + 编号 + 标记 + 描述 + 路径 |

## 步骤 7：更新元数据

将 `xspec/changes/change.yaml` 的 `phase` 更新为 `task`

---

## 输出产物

- `xspec/changes/<name>/task.md`（每个模块一份）
- `xspec/changes/integration.md`（跨模块协调视图）

## 完成报告

```
✅ 任务拆解完成

变更编号：<change-id>

模块任务：
  <module-a>：X 个任务（P0: X / P1: X / P2: X）
  <module-b>：X 个任务（P0: X / P1: X / P2: X）

集成任务（integration.md）：
  跨模块集成任务：X 个
  质量门禁：X 项

跨模块依赖：X 处（全部对齐 / N 处不一致）

👉 建议下一步：执行 /xf/analyze 进行制品审计（推荐）
   或直接执行 /xf/implement 开始实现
```

## 行为规则

- 任务必须可执行——AI 拿到任务描述应能直接动手
- 每个任务包含具体文件路径
- 单个任务不超过 2 小时工作量
- 功能点任务必须关联验收标准
- 每个阶段/功能组后设置检查点
- 前端按用户场景/页面流组织，后端按功能模块/业务流程组织
- 标注可并行任务减少工期
- 依赖标注使用 `depends:` 缩进换行格式，跨模块带 `@module` 后缀
- 编号为模块内唯一，各模块从 T001 独立起编
- 所有输出使用中文
