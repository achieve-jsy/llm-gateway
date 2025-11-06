对当前变更的设计制品进行跨文档一致性审计，在实现前发现问题。**只读操作，不修改任何文件。**

## 用户输入

```
$ARGUMENTS
```

**参数解析**：

- **无参数**：审计当前变更下的**全部模块**
- **指定模块名或前缀**（如 `user-auth`、`001`）：仅审计匹配的模块，跨模块检查（步骤 4/5/6）仅涉及该模块与其关联模块

从 `$ARGUMENTS` 中提取关键词（去除首尾空白）。匹配规则：
1. 精确匹配：`xspec/changes/<keyword>/` 目录存在 → 直接使用
2. 前缀匹配：查找 `xspec/changes/` 下以 `<keyword>` 开头的目录（如输入 `001` 匹配 `001-login`）
3. 匹配到唯一目录 → 直接使用；匹配到多个 → 列出让用户选择；无匹配 → 列出所有可用模块后终止

## 前置条件

- `xspec/constitution.md` 必须存在
- 目标模块内 `task.md` 必须存在（设计制品已完成）
- `xspec/changes/change.yaml` 的 phase 为 `task` 或更后阶段

---

## 步骤 1：确定审计范围并加载制品

### 1.1 确定目标模块

- **全量模式**（无参数）：从 `xspec/changes/change.yaml` 的 `affected_modules` 获取所有模块列表
- **单模块模式**（有参数）：仅使用指定的模块名

### 1.2 加载公共制品

- `xspec/constitution.md`（项目宪章）
- `xspec/changes/change.yaml`（变更元数据 + scope 功能范围边界）
- `xspec/changes/brainstorm.md`（头脑风暴记录，如存在）
- `xspec/changes/integration.md`（跨模块协调视图，如存在）

### 1.3 加载模块制品

对每个目标模块，调用 manifest 查询应加载的制品清单（**不要硬编码文件名**——不同 `module_type` 的制品集不同）：

```bash
node .xflow/scripts/xf-manifest.mjs --module-type <module_type> --all-phases --change-type <added|modified>
```

按 manifest 返回的 `files[]` 读取 `xspec/changes/<name>/` 下对应文件。典型覆盖项：

- `spec.md` / `spec.delta.md`（clarify 阶段，所有类型 required）
- `hld.md` / `hld.delta.md`（design 阶段，所有类型 required）
- `design.md` / `design.delta.md`（detail 阶段）
- `data-model.md` / `data-model.delta.md`（detail 阶段）
- `contracts.md` / `contracts.delta.md`（detail 阶段，仅 manifest 包含此项；`systems` / `ai-agent` / `_default` 不含）
- `evaluation.md` / `evaluation.delta.md`（detail 阶段，仅 `ai-agent` 专属）
- `task.md`（task 阶段，所有类型 required）

额外可选读取（与 module_type 无关）：`research.md` + `research/` 子文件。

> 单模块模式下，如果步骤 4/5/6 需要跨模块对比（如前后端 contracts 对齐），额外对关联模块调用 manifest 并加载关键文件。

## 步骤 2：需求覆盖检查

**目标**：确保 `xspec/changes/change.yaml` 的 `scope.include` 中确认的功能点都在模块级 spec 中有对应内容。

1. 从 `xspec/changes/change.yaml` 的 `scope.include` 提取功能点列表
2. 对每个功能点，检查是否在某个模块的 `spec.md` 中有对应章节
3. 输出覆盖率：`N/M (XX%)`

**问题标记**：
- 功能点未在任何模块 spec 中出现 → **高**
- 功能点仅简要提及但无详细描述 → **中**

## 步骤 3：任务覆盖检查

**目标**：确保 spec 中的验收标准都有对应的实现任务。

1. 从所有模块的 `spec.md` 中提取验收标准（AC-XX 编号）
2. 对每个验收标准，检查各模块 `task.md` 中是否有对应任务
3. 输出覆盖率：`N/M (XX%)`

**问题标记**：
- P0 验收标准无对应任务 → **高**
- P1 验收标准无对应任务 → **中**
- P2 验收标准无对应任务 → **低**

## 步骤 4：集成任务检查

**目标**：如存在 `integration.md`，检查跨模块集成任务的完整性。

1. 集成任务中的 `depends: TNNN@module` 引用是否指向存在的模块任务
2. 涉及前后端的变更是否有联调集成任务
3. 质量门禁清单是否完整

**问题标记**：
- 跨模块依赖指向不存在的任务 → **高**
- 前后端变更缺少联调任务 → **中**
- 质量门禁缺失关键检查项 → **低**

如 `integration.md` 不存在（单模块变更），跳过此检查。

## 步骤 5：前后端 Contracts 对齐

**目标**：如果变更同时涉及前端和后端模块，确保接口定义一致。

**前置判断**：分别对前端和后端模块调用 manifest，检查 `contracts.md` 是否在各自的 files 列表中。**只要任一端的 manifest 不含 `contracts.md`，跳过此检查**（例如 `systems` 后端）。

1. 从后端模块 `contracts.md` 提取接口列表（方法、路径、参数、响应、错误码）
2. 从前端模块 `contracts.md` 提取调用列表
3. 逐接口对比路径、参数、响应、错误码

**问题标记**：
- 前端调用的接口后端未定义 → **高**
- 参数或响应字段不匹配 → **高**
- 后端有接口但前端未调用 → **中**
- 错误码处理不一致 → **中**

如变更仅涉及单端，或任一端 manifest 不含 `contracts.md`，跳过此检查。

## 步骤 6：数据模型对齐

**目标**：确保前后端的数据结构定义匹配。

**前置判断**：检查前后端模块的 manifest 是否均含 `data-model.md`（当前所有注册类型均含，但自定义类型回退 `_default` 时此文件为 optional，需确认实际存在）。

1. 从后端 `data-model.md` 提取表结构和字段
2. 从前端 `data-model.md` 提取 TypeScript 接口/类型
3. 对比字段、类型兼容性、命名风格

**问题标记**：
- 后端响应字段在前端类型中缺失 → **高**
- 类型不兼容 → **中**
- 命名风格不一致且未说明转换规则 → **低**

## 步骤 7：宪章合规检查

**目标**：确保所有设计制品符合 `constitution.md` 的要求。

逐板块检查：
1. **核心原则**：设计中是否有违背
2. **技术约束**：design.md 中的技术选型是否在宪章允许范围内
3. **质量标准**：tasks 中是否有满足宪章要求的测试/性能任务
4. **安全与合规**：contracts 中的认证方式是否符合

**问题标记**：
- 违反核心原则/技术约束 → **高**
- 质量标准/安全要求未满足 → **中**

## 步骤 8：术语一致性检查

**目标**：发现跨文档的术语不一致。

检查同一概念在不同文档中是否使用不同称呼（如"用户" vs "账户"）。

**问题标记**：
- 同一概念不同业务术语 → **中**
- 仅命名风格差异（snake_case vs camelCase）→ **低**

## 步骤 9：歧义检测

**目标**：发现需求描述中的模糊表述。

扫描所有 `spec.md` 和 `hld.md`，检测模糊词汇：
"等"、"若干"、"适当"、"合理"、"如有必要"、"尽可能"，以及无具体数值的性能描述。

**问题标记**：
- 验收标准中的模糊表述 → **中**
- 功能描述中的模糊表述 → **低**

---

## 输出

不生成任何文件。直接向用户输出审计报告：

```
━━━━━━━━━━━━━━━━━━━━━━
  制品审计报告
  变更：<change-id> <change-name>
  范围：全部模块 / 仅 <module-name>
━━━━━━━━━━━━━━━━━━━━━━

▎覆盖率
  需求 → Spec      ：XX% (N/M)
  Spec → Tasks     ：XX% (N/M)
  前后端 Contracts  ：XX%
  前后端 Data Model ：XX%

▎问题清单（共 N 项：X 高 / Y 中 / Z 低）

  [高] <位置>：<问题描述>
  [中] <位置>：<问题描述>
  [低] <位置>：<问题描述>

▎宪章合规
  <通过 / 列出违规项>

▎建议
  - 有高优先级问题：建议修复后再 /xf/implement
  - 仅中/低问题：可酌情处理，或直接 /xf/implement
  - 无问题：可直接 /xf/implement
```

## 行为规则

- **绝对只读**：不修改任何文件，不创建任何文件
- 报告问题时给出具体位置（文件路径 + 章节）
- 不过度报警——信息项标为低
- 单端变更跳过前后端对齐检查
- 没有 research.md 正常跳过，不报为问题
- **单模块模式**：需求覆盖检查（步骤 2）仅检查该模块相关的 scope 功能点；跨模块检查（步骤 4/5/6）仅涉及该模块与其直接关联模块
- 所有输出使用中文
