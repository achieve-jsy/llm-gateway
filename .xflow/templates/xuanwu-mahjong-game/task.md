# [MODULE_NAME] 麻将游戏任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | [CHANGE_ID] |
| **模块名称** | [MODULE_NAME] |
| **模块类型** | xuanwu-mahjong-game |
| **创建时间** | [CREATED_AT] |

---

## 任务格式

```
- [ ] {semantic-prefix}-{NNN} [P] P? {原子动作描述}
  切面: {服务端/客户端切面值}
  文件: {真实文件路径（≤3 个）或 blocked: 原因}
  AC: {AC-XX 或 —}
  depends: {semantic-prefix}-{NNN}, {semantic-prefix}-{NNN}@{other-module}
```

| 标记 | 含义 |
| :--- | :--- |
| `{semantic-prefix}-{NNN}` | 语义编号，前缀由分片固定分配，如 `fgui-001`、`state-001`、`entry-001`、`peri-001`、`server-core-001` |
| `[P]` | 可并行：操作不同文件且无未完成依赖 |
| `P?` | P0（必须）/ P1（重要）/ P2（可选） |
| `切面` | 实现切面标识（如：服务端-C++ 骨架 / 客户端-FGUI / 客户端-核心模块） |
| `文件` | 真实路径；无锚点则 `blocked: 原因` |
| `AC` | 关联验收标准编号；无直接 AC 则 `—` |
| `depends` | 依赖标注（缩进换行）。跨模块带 `@module` |

### 路径与产物规则

- 服务端任务的 `文件` 必须是仓库真实相对路径，并从 `server/game_server/Logic/mahjong_server/src/games/` 起算。
- 公共麻将能力使用 `server/game_server/Logic/mahjong_server/src/games/common_game/...`。
- 玩法私有服务端文件使用 `server/game_server/Logic/mahjong_server/src/games/{server_module_name}/...`。
- 禁止在可执行任务的 `文件` / `write_set` 中使用 `common_game/...`、`{server_module_name}/...`、`hahazhuaji_server/...` 等短路径。
- 生成任务前检查目标文件是否存在：存在则任务为 `modify`，不存在则为 `create_new`；已有骨架或 TODO 占位文件不得标为 `create_new`。
- 客户端协议生成物固定为 `client/assets/{game_id}/lib/proto_cm_protocol.js` 和 `client/assets/{game_id}/lib/proto_cm_protocol.d.ts`；任务不得引用 `proto_{game_keyword}_cli.*`、`proto_{game_keyword}.*` 或其他玩法名派生的客户端 proto 文件。
- FGUI 任务必须同时覆盖 FGUI 工程包与发布声明产物：
  - FGUI 工程包：`client/fgui_game/{fgui_project_name}/assets/{package_name}/`
  - 发布声明产物：`client/assets/{game_id}/fgui_uideclare/{package_name}/`
- 生成 FGUI 任务前必须检查 `client/assets/{game_id}/fgui_uideclare/{package_name}/` 是否存在：存在则生成“修改/补齐声明产物”任务，不存在则生成“新建声明产物目录/文件”任务。
- FGUI 与 `fgui_uideclare` 相关 `.meta` 文件按项目现有资源规范同步处理，不能遗漏。

---

## 阶段 1：服务端基础

**目标**：C++ GameLogic 骨架、Step 状态、配置接入就绪

- [ ] server-core-001 [P] P0 服务端 C++ 骨架（`GameLogic.cpp` 入口 + cmdid 分发）
  切面: 服务端-C++ 骨架
  文件: server/game_server/Logic/mahjong_server/src/games/{server_module_name}/GameLogic.cpp
  AC: —
- [ ] server-core-002 [P] P0 GameData / PlayerData / Step 状态初始化
  切面: 服务端-状态
  文件: server/game_server/Logic/mahjong_server/src/games/{server_module_name}/GameLogic.h, server/game_server/Logic/mahjong_server/src/games/{server_module_name}/GameLogic.cpp
  AC: —
  depends: server-core-001
- [ ] server-service-001 [P] P1 配置字段读取与校验
  切面: 服务端-配置
  文件: server/game_server/Logic/mahjong_server/src/games/{server_module_name}/GameLogic.cpp
  AC: —
  depends: server-core-001

**✓ 检查点**：C++ 编译目标可接入，状态流转正常，配置可读取

---

## 阶段 2：服务端核心逻辑

**目标**：按 spec.md 验收标准实现核心业务逻辑

### 切面：[功能名称，如：查询麻将游戏数据]

**对应验收标准**：AC-01

- [ ] server-core-010 P0 实现查询 cmdid 处理逻辑
  切面: 服务端-命令处理
  文件: server/game_server/Logic/mahjong_server/src/games/{server_module_name}/GameLogic.cpp
  AC: AC-01
  depends: server-core-001, server-service-001

### 切面：[功能名称，如：局内动作处理]

**对应验收标准**：AC-02

- [ ] server-core-020 P0 实现局内动作处理逻辑（含幂等）
  切面: 服务端-业务逻辑
  文件: server/game_server/Logic/mahjong_server/src/games/{server_module_name}/GameLogic.cpp
  AC: AC-02
  depends: server-core-002

### 切面：[功能名称，如：结算]

**对应验收标准**：AC-03

- [ ] server-service-030 P0 实现结算 cmdid 处理（含防重领）
  切面: 服务端-结算
  文件: server/game_server/Logic/mahjong_server/src/games/{server_module_name}/GameLogic.cpp
  AC: AC-03
  depends: server-core-020

**✓ 检查点**：服务端核心业务逻辑可独立验证

---

## 阶段 3：客户端基础

**目标**：FGUI 结构、模块骨架、协议接入就绪

- [ ] fgui-001 [P] P0 创建或修改 FGUI 包，并同步发布声明产物
  切面: 客户端-FGUI
  文件: client/fgui_game/{fgui_project_name}/assets/{package_name}/, client/assets/{game_id}/fgui_uideclare/{package_name}/
  AC: —
- [ ] state-001 [P] P0 创建 Module/View/Data 骨架文件
  切面: 客户端-核心模块
  文件: [ModulePath], [ViewPath]
  AC: —
- [ ] state-002 P0 注册麻将游戏协议监听（cmdid 绑定）
  切面: 客户端-协议
  文件: [ModulePath]
  AC: —
  depends: state-001

**✓ 检查点**：客户端模块可注册，协议监听正常

---

## 阶段 4：客户端 UI 功能

**目标**：按 spec.md UI 交互需求实现各 UI 切面

### 切面：主界面展示

**对应验收标准**：AC-01

- [ ] state-010 P0 实现主界面数据绑定与刷新
  切面: 客户端-View
  文件: [ViewPath]
  AC: AC-01
  depends: fgui-001, state-001, state-002

### 切面：入口与强弹

**对应验收标准**：AC-03

- [ ] entry-001 P1 实现入口图标/横幅挂载
  切面: 客户端-入口
  文件: [EntryPath]
  AC: AC-03
  depends: state-001
- [ ] entry-002 P1 实现强弹逻辑（频次控制）
  切面: 客户端-强弹
  文件: [EntryPath]
  AC: —
  depends: entry-001

### 切面：红点

- [ ] peri-001 P2 实现红点注册与消除
  切面: 客户端-红点
  文件: [RedDotPath 或 ModulePath]
  AC: —
  depends: state-002

**✓ 检查点**：UI 各切面可独立验证

---

## 阶段 5：集成与收尾

**目标**：联调、埋点、质量验证

- [ ] client-gap-001 P1 服务端-客户端联调（核心流程 E2E）
  切面: 集成
  文件: —
  AC: AC-01, AC-02, AC-03
  depends: server-service-030, state-010
- [ ] peri-002 [P] P1 埋点上报实现
  切面: 客户端-埋点
  文件: [AnalyticsPath]
  AC: —
  depends: state-010
- [ ] client-gap-002 [P] P2 异常场景验证（网络错误、重复请求）
  切面: 集成
  文件: —
  AC: —
  depends: client-gap-001

**✓ 检查点**：全部 P0/P1 验收标准通过，满足宪章质量标准

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 0 |
| P0（必须） | 0 |
| P1（重要） | 0 |
| P2（可选） | 0 |
| 可并行任务 | 0 |
| 预计工时 | 0h |
