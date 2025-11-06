定义或修订项目的整体模块结构，将产品功能拆分为独立模块并记录到 PRODUCT.md。

## 用户输入

```
$ARGUMENTS
```

如果用户提供了需要拆分的功能描述，以此作为起点；否则基于现有 PRODUCT.md 进行模块结构审视。

## 前置条件

- `xspec/constitution.md` 必须存在
- `xspec/modules/PRODUCT.md` 必须存在（不存在则提示先运行 `/xf/product`）

---

## 步骤 0：Profile 分支加载（始终先于其他步骤执行）

1. 执行 `node .xflow/scripts/xf-profile.mjs --value-only` 读取当前 `project_profile`
2. 显眼打印一行：`📌 project_profile = <profile>`
3. 如果 `.xflow/profiles/<profile>/split.md` 存在，**读取该文件**并作为本次运行的行为覆写规则优先执行：
   - 指令 `终止整条命令` → 按方言固定输出打印并结束，**不执行下文任何步骤**
   - 指令 `跳过 <步骤>` → 执行到该步骤时直接跳过
   - 指令 `替换 <步骤>` → 改用方言文件中的替代内容
   - 指令 `追加 <位置>` → 在该位置附加方言文件的内容
4. 方言文件不存在或 profile 为 `general` 时，按下文通用流程执行

> 原则：本文件只维护通用逻辑；领域方言统一在 `.xflow/profiles/<profile>/` 下管理。**禁止**在本文件硬编码 `if profile == xxx` 分支。

---

## 场景一：首次模块划分（PRODUCT.md 中模块列表为空或初始状态）

### 步骤 1：加载上下文

1. 读取 `xspec/constitution.md` 了解技术约束
2. 读取 `xspec/modules/PRODUCT.md` 了解产品定位、目标用户、产品边界
3. 扫描项目代码结构推断可能的模块划分

### 步骤 2：提出模块划分方案

基于产品定位和技术约束，提出模块结构：

```
📋 模块划分方案

模块列表：
  - <module-a> (web-backend)：[职责一句话描述]
  - <module-b> (web-frontend)：[职责一句话描述]
  - <module-c> (ai-agent)：[职责一句话描述]

模块依赖：
  - <module-b> → <module-a>（前端调用后端接口）
  - <module-c> → <module-a>（共享数据层）

划分依据：
  - [说明为什么这样划分，边界在哪里]

```

展示后以选项式交互确认。

### 步骤 3：确认细节（1-2 轮）

如用户接受方案，进入步骤 4。如需调整，针对调整点逐项确认：
- 每个需要用户决策的调整点，给出 2-3 个选项 + 推荐 + 最小化
- 模块类型不确定时，给出候选类型 + 推荐（已注册类型导读见 `.xflow/templates/CATALOG.md`，机器可查清单见 `.xflow/templates/CATALOG.yaml` 或执行 `node .xflow/scripts/xf-manifest.mjs --list-types`，支持自定义类型）

### 步骤 4：更新 PRODUCT.md

用户确认后，更新 `xspec/modules/PRODUCT.md` 的以下部分：

1. **模块结构** mermaid 图
2. **模块列表** 表格（名称、类型、职责描述、状态）
3. **模块依赖关系** 表格
4. 递增版本号、更新日期

> ⚠️ **"状态"列语义约定**（避免后续 `/xf/clarify` 误判 added/modified）：
> - `规划中`：模块名已注册，但 `xspec/modules/<name>/spec.md` 尚未物化。
>   首次为它跑 `/xf/clarify` 时仍是 `change_type: added`（这是初次撰写规范，不是变更）。
> - `已实现` / `维护中`：`xspec/modules/<name>/spec.md` 已存在（至少经过一次归档）。
>   后续对它的修改才是 `change_type: modified`。
>
> **判定凭据是 `xspec/modules/<name>/spec.md` 是否物理存在，不是 PRODUCT.md 的状态字面值**——
> 状态列只是给人看的描述，AI 在 clarify 时不要据此推断 added/modified。

---

## 场景二：新增或调整模块（已有模块结构）

### 步骤 1：加载现有结构

1. 读取 `xspec/modules/PRODUCT.md` 获取现有模块列表和依赖
2. 读取 `xspec/constitution.md` 了解约束
3. 扫描 `xspec/modules/` 了解各模块实际状态

### 步骤 2：理解调整意图

如用户在参数中说明了调整内容，直接分析影响。否则展示现有模块结构，并以选项式交互让用户选择调整方式。

### 步骤 3：影响分析

对于模块调整，分析：

1. 新增模块对现有依赖关系的影响
2. 拆分/合并对已有设计文档的影响
3. 是否需要迁移已有模块的内容

### 步骤 4：确认并更新

1. 展示修订对比（原结构 → 新结构）
2. 用户确认后更新 PRODUCT.md
3. 递增版本号

---

## 输出产物

- `xspec/modules/PRODUCT.md`（模块结构部分更新）

## 完成报告

```
✅ 模块结构定义完成

模块总数：X 个（按类型统计）

模块列表：
  - <module-a> (web-backend)
  - <module-b> (web-frontend)
  - <module-c> (ai-agent)

依赖关系：X 条

产品定位文档已更新：xspec/modules/PRODUCT.md

👉 下一步：执行 /xf/brainstorm 或 /xf/clarify 开始一个需求
```

## 行为规则

- 需要用户做选择时，遵循 CLAUDE.md 全局交互规则（选项式 + 推荐 + 最小化）
- 模块划分以产品定位和产品边界为参考基准
- 每个模块必须有明确的职责边界，不交叉
- 模块粒度适中——不要太粗（一个模块包含所有功能）也不要太细（每个 API 一个模块）
- 模块命名默认使用纯语义名（如 `user-auth`、`project-api`）。如用户希望加数字前缀（如 `001-user-auth`），按用户偏好执行，后续新增模块延续编号
- 模块类型优先使用 `.xflow/templates/CATALOG.yaml` 中已注册的类型（当前为 web-frontend / web-backend / native-app / ai-agent / systems）。如遇不在列表中的类型，可自定义类型名称，后续骨架创建时 `xf-scaffold.mjs` 会自动回退到 `_default/` 通用模板
- 所有输出使用中文
