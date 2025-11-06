将变更内容归档合并到 modules（Source of Truth），完成变更生命周期。

## 用户输入

```
$ARGUMENTS
```

无需额外参数，直接对当前活跃变更执行归档。

## 前置条件

- 各模块 `task.md` 中所有任务已完成（全部 `[x]`）
- `xspec/changes/change.yaml` 的 phase 为 `implement` 或 `archive`
- 如有未完成任务，询问用户是否强制归档
- **结构不变量校验（硬停）**：执行 `node .xflow/scripts/xf-check-invariants.mjs`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ 结构不变量违反：<脚本输出的报告>

  请按报告建议修复后重试。
  ```
  > **archive 特别说明**：archive 命令本身就是为了"清场"，但**校验仍然在前置条件跑**——若发现违规说明上一次 archive 没收尾干净，应先重跑或人工修复，而不是在脏状态上叠加本次归档。
- **phase 入口校验（硬停）**：执行 `node .xflow/scripts/xf-check-phase.mjs --command archive`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ phase 入口不合法：<脚本输出的报告>

  请按报告建议处理后重试。
  ```

---

## 步骤 0：Profile 分支加载（始终先于其他步骤执行）

1. 执行 `node .xflow/scripts/xf-profile.mjs --value-only` 读取当前 `project_profile`
2. 显眼打印一行：`📌 project_profile = <profile>`
3. 如果 `.xflow/profiles/<profile>/archive.md` 存在，**读取该文件**并作为本次运行的行为覆写规则优先执行：
   - 指令 `终止整条命令` → 按方言固定输出打印并结束，**不执行下文任何步骤**
   - 指令 `跳过 <步骤>` → 执行到该步骤时直接跳过（例如 xuanwu-activity 下跳过步骤 5 的 copy-new）
   - 指令 `替换 <步骤>` → 改用方言文件中的替代内容
   - 指令 `追加 <位置>` → 在该位置附加方言文件的内容
4. 方言文件不存在或 profile 为 `general` 时，按下文通用流程执行

> 原则：本文件只维护通用逻辑；领域方言统一在 `.xflow/profiles/<profile>/` 下管理。**禁止**在本文件硬编码 `if profile == xxx` 分支。

---

## 步骤 1：推进 phase 至 `archive`（必须先于一切写入步骤）

归档实质性写入要落到 `xspec/modules/` 与 `xspec/archive/`，这两个目录受 protect 插件保护。**插件仅在 `change.yaml` 的 phase 已经是 `archive` 时**才会放行后续合并/复制；如果 phase 还停留在 `implement`，下文步骤 4–11 全部会被拦截，命令直接卡死。

因此进入归档流程的第一步**必须**先把 phase 推到 archive：

1. 编辑 `xspec/changes/change.yaml`：
   - `phase` 字段改为 `"archive"`
   - 同时更新 `updated_at` 为今天日期（`<YYYY-MM-DD>`）
   - 其他字段保持不变
2. 这一步只改这两个字段，**不要**顺手做其他修改
3. 写入 `change.yaml` 不会被任何 protect 规则拦截（`xspec/changes/` 不受保护）

如果 phase 已经是 `archive`（重入归档场景，例如上一次归档没收尾），跳过本步骤。

> 这一步对应原"步骤 9 更新元数据"中 phase 的部分；其余字段（`status: archived` / `archived_at`）仍按原步骤在归档收尾时更新。

> **取消归档怎么回退**：phase 一旦推到 `archive`，后续步骤 2/4 任意一步用户选"取消"或"未完成的任务不强制归档"，归档流程会中止但 phase 字段不会自动回滚——这是有意为之，方便用户**重跑 /xf/archive 继续完成**。如果用户决定彻底放弃本次归档、回到 `implement` 继续做修改：手工把 `change.yaml` 的 `phase` 改回 `"implement"` 即可（写入 `xspec/changes/` 不受 protect 拦截）。

---

## 步骤 2：预检查

1. **读取变更 ID**：从 `xspec/changes/change.yaml` 读取 `id` 字段
2. **任务完成度检查**：解析各模块 `task.md`，统计已完成和未完成任务。如有未完成任务，显示列表并询问是否强制归档
3. **制品完整性检查**：扫描 `xspec/changes/` 下每个模块目录，检查必要制品是否存在（至少有 spec.md 或 spec.delta.md）

> **用户选"不强制归档"时**：phase 已在步骤 1 推到 `archive` 但归档没继续。如实告知用户当前状态：「phase 已推到 archive 但归档未完成；想完成归档就把任务做完后重跑 `/xf/archive`，想回到 implement 继续修改就把 `change.yaml` 的 `phase` 字段改回 `"implement"`」，由用户决定。**不要**自行回滚 phase。

## 步骤 3：变更影响分析

读取 `xspec/changes/change.yaml` 获取受影响模块列表，按 `change_type` 分析归档类型：

- **新增模块**（change_type: added）：`xspec/changes/<name>/` 完整制品创建到 `xspec/modules/<name>/`
- **变更模块**（change_type: modified）：delta 文件与原文件合并
- **移除模块**（change_type: removed）：标记模块为废弃状态

输出归档计划供用户确认。对每个模块调用 manifest 获取该类型应该归档的文件清单：

```bash
# 新增模块：查询完整产物清单
node .xflow/scripts/xf-manifest.mjs --module-type <module_type> --all-phases --change-type added

# 变更模块：查询 delta 文件清单
node .xflow/scripts/xf-manifest.mjs --module-type <module_type> --all-phases --change-type modified
```

然后据此生成归档计划（示例：`web-backend` 含 `contracts.md`，`systems` 不含）：

```
📋 归档计划

新增模块（从 xspec/changes/<name>/ → xspec/modules/<name>/）：
  - <module-a> (web-backend)：spec.md, hld.md, design.md, data-model.md, contracts.md, task.md, research.md, research/*
  - <module-c> (systems)：spec.md, hld.md, design.md, data-model.md, task.md（无 contracts.md）

变更模块：
  - <module-b>：<以该模块 manifest 返回的 .delta.md 清单为准>

确认执行归档？
```

> **注意**：`research.md` 和 `research/` 子文件与 `module_type` 无关，按实际存在与否追加。

## 步骤 4：冲突检测

对每个变更模块：
1. 读取 delta 文件中的「修改内容」
2. 读取现有模块对应文件
3. 检查 delta 引用的「当前内容」与实际文件是否一致
4. 如发现冲突，展示详情并提供选项：覆盖 / 合并 / 跳过 / 取消

> **用户选"取消"时**：与步骤 2 同——phase 已经是 `archive` 但归档没继续，向用户复述：「重跑 `/xf/archive` 可继续；想回 implement 改改再来就手工改 `change.yaml` 的 `phase` 为 `"implement"`」。**不要**自行回滚 phase。

## 步骤 5：执行归档 — 新增模块（脚本处理）

```bash
node .xflow/scripts/xf-archive.mjs --step copy-new --modules <逗号分隔的新增模块名>
```

脚本自动：
- 将 `xspec/changes/<name>/` 复制到 `xspec/modules/<name>/`
- 创建 `module.yaml`（含版本、时间、来源变更 ID）

## 步骤 6：执行归档 — 变更模块（AI 智能合并）

对每个 `change_type: modified` 的模块：

1. 读取现有文件（如 `xspec/modules/<name>/spec.md`）
2. 读取 delta 文件（如 `xspec/changes/<name>/spec.delta.md`）
3. 按 delta 指示执行新增/修改/删除
4. 保持文档结构和格式一致
5. **合并 research**（如本次变更产出了 research）：
   - `research.md`：将 `xspec/changes/<name>/research.md` 中的新课题行追加到 `xspec/modules/<name>/research.md` 的索引表，保留已有课题不覆盖
   - `research/` 子文件：将 `xspec/changes/<name>/research/` 中的新增子文件复制到 `xspec/modules/<name>/research/`（序号不冲突则直接复制；如冲突则重新编号）
6. 更新 `module.yaml`：递增 version、更新 last_updated、添加 change_history 条目
7. 在被修改的文件末尾添加变更历史

## 步骤 7：完整性验证

1. 检查每个模块目录下必要文件是否完整
2. 检查 module.yaml 数据正确性
3. 检查文档内部交叉引用是否有效

## 步骤 8：生成变更记录

1. 读取 `.xflow/templates/_shared/changelog.md` 模板
2. 填充变更概述、涉及模块、关键决策、制品清单
3. 如存在模块级 `research.md`，从课题索引表读取结论摘要，在「关键决策」部分引用
4. 输出到 `xspec/changes/changelog.md`

## 步骤 9：更新项目迭代记录

在 `xspec/modules/CHANGELOG.md` **顶部**追加本次变更条目：

```markdown
## [<change-id>] <change-name> — <YYYY-MM-DD>

- <新增/更新模块列表及简要说明>
- 关键决策：<本次变更中做出的关键决策>

---
```

## 步骤 10：更新元数据（收尾）

更新 `xspec/changes/change.yaml`：`status: "archived"`, `archived_at: "<YYYY-MM-DD>"`, `updated_at: "<YYYY-MM-DD>"`。

> `phase: "archive"` 已在步骤 1 推过，无需再写一次（如果不一致请保留为 `archive`）。

## 步骤 11：搬运归档 + 清理（脚本处理）

```bash
node .xflow/scripts/xf-archive.mjs --step finalize
```

脚本自动：将 `xspec/changes/` 全部内容（包括 `.gates/` 审批记录）复制到 `xspec/archive/<change-id>/`，然后清空 `xspec/changes/`

> `.gates/` 目录会一起归档到 `xspec/archive/<change-id>/.gates/`，保留完整审批历史供后续追溯。归档后 `xspec/changes/.gates/` 被清空，新变更从头走审批流程。

---

## 合并策略

合并策略按**文件类别**统一应用，具体要合并哪些文件由 manifest 决定（不再硬编码枚举）。

| 文件类别 | 新模块 | 变更模块 |
| :--- | :--- | :--- |
| manifest 返回的所有 `.md` 制品（spec / hld / design / data-model / contracts / evaluation / task 等） | 直接复制 | 对应 `.delta.md` 合并到原文件 |
| `research.md`（与 manifest 无关，按需产出） | 直接复制 | 索引表合并追加新课题行 |
| `research/*.md` 子文件 | 直接复制 | 新增子文件直接复制，序号冲突时重编号 |

**统一规则**：
- 新模块：遍历 manifest (`--change-type added`) 返回的文件，逐个从 `xspec/changes/<name>/` 复制到 `xspec/modules/<name>/`
- 变更模块：遍历 manifest (`--change-type modified`) 返回的 `.delta.md` 文件，对每个 delta 执行智能合并

若 CATALOG.yaml 新增了类型专属文件（如 `ai-agent` 的 `evaluation.md`），上述策略会自动覆盖，无需修改本命令。

## 完成报告

```
✅ 归档完成

变更编号：<change-id>

归档摘要：
  新增模块：
    - <module-a> (v1.0)
  更新模块：
    - <module-b> (v1.0 → v1.1)

已归档到：xspec/archive/<change-id>/
xspec/changes/ 目录已清空，可开始下一个变更

🎉 变更 <change-id> 生命周期完成
```

## 行为规则

- 归档前必须验证任务完成度
- 冲突必须经用户确认后才能处理
- 不删除变更记录，复制到 xspec/archive/ 保留历史后再清空 xspec/changes/
- 合并后必须验证文件完整性
- `integration.md` 是变更级视图，**不归入模块目录**（`xspec/modules/<name>/`），仅随 `xspec/changes/` 整体归档到 `xspec/archive/<change-id>/`
- 所有输出使用中文
