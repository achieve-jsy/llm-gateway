# [MODULE_NAME] 活动任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | [CHANGE_ID] |
| **模块名称** | [MODULE_NAME] |
| **模块类型** | xuanwu-activity |
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
| `切面` | 实现切面标识（如：服务端-脚本骨架 / 客户端-FGUI / 客户端-核心模块） |
| `文件` | 真实路径；无锚点则 `blocked: 原因` |
| `AC` | 关联验收标准编号；无直接 AC 则 `—` |
| `depends` | 依赖标注（缩进换行）。跨模块带 `@module` |

---

## 阶段 1：服务端基础

**目标**：脚本骨架、Redis 结构、配置接入就绪

- [ ] server-core-001 [P] P0 服务端脚本骨架（`main.lua` 入口 + cmdid 分发）
  切面: 服务端-脚本骨架
  文件: [脚本路径/main.lua]
  AC: —
- [ ] server-core-002 [P] P0 Redis Key 初始化与幂等锁
  切面: 服务端-Redis
  文件: [脚本路径/utils.lua 或 main.lua]
  AC: —
  depends: server-core-001
- [ ] server-service-001 [P] P1 配置字段读取与校验
  切面: 服务端-配置
  文件: [脚本路径/main.lua]
  AC: —
  depends: server-core-001

**✓ 检查点**：脚本可加载，Redis 操作正常，配置可读取

---

## 阶段 2：服务端核心逻辑

**目标**：按 spec.md 验收标准实现核心业务逻辑

### 切面：[功能名称，如：查询活动数据]

**对应验收标准**：AC-01

- [ ] server-core-010 P0 实现查询 cmdid 处理逻辑
  切面: 服务端-命令处理
  文件: [脚本路径/main.lua]
  AC: AC-01
  depends: server-core-001, server-service-001

### 切面：[功能名称，如：完成任务计数]

**对应验收标准**：AC-02

- [ ] server-core-020 P0 实现完成任务计数逻辑（含幂等）
  切面: 服务端-业务逻辑
  文件: [脚本路径/main.lua]
  AC: AC-02
  depends: server-core-002

### 切面：[功能名称，如：领取奖励]

**对应验收标准**：AC-03

- [ ] server-service-030 P0 实现领取奖励 cmdid 处理（含防重领）
  切面: 服务端-发奖
  文件: [脚本路径/main.lua]
  AC: AC-03
  depends: server-core-020

**✓ 检查点**：服务端核心业务逻辑可独立验证

---

## 阶段 3：客户端基础

**目标**：FGUI 结构、模块骨架、协议接入就绪

- [ ] fgui-001 [P] P0 创建 FGUI 包与 uideclare.d.ts
  切面: 客户端-FGUI
  文件: [FGUI包路径], [uideclare路径]
  AC: —
- [ ] state-001 [P] P0 创建 Module/View/Data 骨架文件
  切面: 客户端-核心模块
  文件: [ModulePath], [ViewPath]
  AC: —
- [ ] state-002 P0 注册活动协议监听（cmdid 绑定）
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
