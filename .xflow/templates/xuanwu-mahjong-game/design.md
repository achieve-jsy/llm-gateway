# [MODULE_NAME] 麻将游戏详细技术设计

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | [CHANGE_ID] |
| **模块名称** | [MODULE_NAME] |
| **模块类型** | xuanwu-mahjong-game |
| **创建时间** | [CREATED_AT] |

---

## 1. 设计索引

> 本文档是详细设计阶段的索引，各子文档路径如下。

| 文档 | 路径 | 说明 |
| :--- | :--- | :--- |
| 服务端状态设计 | `design_server_state.md` | GameData / PlayerData / Step 状态与生命周期 |
| 服务端接口协议 | `design_server_protocol.md` | cmdid、请求/响应结构 |
| 服务端配置契约 | `design_server_config.md` | 配置字段、来源、消费位置 |
| 服务端实现冻结 | `design_server_freeze.md` | 服务端所有冻结约束汇总 |
| 客户端 FGUI 设计 | `design_client_fgui.md` | FGUI 节点树、uideclare |
| 客户端核心模块 | `design_client_core.md` | 常量/DTO/数据存储 |
| 客户端入口弹窗 | `design_client_entry.md` | 入口逻辑与弹窗流程 |
| 客户端 UI 埋点 | `design_client_analytics.md` | 事件名、上报字段 |
| 客户端支付流程 | `design_client_payment.md` | 商品列表、发起支付、回调（有支付时） |
| 客户端实现冻结 | `design_client_freeze.md` | 客户端所有冻结约束汇总 |

---

## 2. 共享契约确认

> 以 `hld.md` 为准，此处仅做冻结确认。如与 hld.md 有差异，以本文为准并注明原因。

| 字段 | 冻结值 | 来源 |
| :--- | :--- | :--- |
| **TYPE_ID** | [int] | hld.md |
| **game_script** | `[路径]` | hld.md / 仓库扫描 |
| **协议前缀** | `[前缀]` | hld.md |
| **info 根字段集合** | `[字段名列表]` | hld.md |

---

## 3. 服务端设计概要

### 3.1 C++ 结构

```
server/game_server/Logic/mahjong_server/src/games/{game}_server/
├── GameLogic.cpp          -- 入口，cmdid 分发
├── handlers/         -- 各命令处理器（如适用）
└── GameLogic.h / rule helper .cpp         -- 公共工具（如适用）
```

### 3.2 命令处理概要

| cmdid | 处理函数 | 核心逻辑 | 关键约束 |
| :--- | :--- | :--- | :--- |
| [cmdid] | [函数名] | [一句话描述] | [幂等/事务/并发约束] |
| [...] | [...] | [...] | [...] |

### 3.3 服务端状态设计概要

| Key 模式 | 数据结构 | 用途 | 生命周期 |
| :--- | :--- | :--- | :--- |
| `act:{TYPE_ID}:{uid}:[用途]` | [String/Hash/List] | [用途] | [生命周期] |
| [...] | [...] | [...] | [...] |

**一致性约束**：服务端状态变更必须由 GameLogic/Step 单点驱动，禁止客户端侧推断最终结算

---

## 4. 客户端设计概要

### 4.1 模块结构

```
[麻将游戏目录]/
├── [MahjongGameName]View.ts       -- UI 控制，继承 BaseGameView
├── [MahjongGameName]Module.ts     -- 业务逻辑，继承 BaseGameModule
├── [MahjongGameName]Data.ts       -- 状态存储，继承 BaseGameData
└── [MahjongGameName]ViewProxy.ts  -- 代理（可选）
```

### 4.2 FGUI 包结构概要

```
[FGUI包名]/
├── [MainPanel]           -- 主界面容器
│   ├── [TaskList]        -- 任务列表
│   └── [RewardPanel]     -- 结算区域
└── [子组件]              -- 按需列出
```

### 4.3 关键事件与消息

| 事件 / 消息 | 触发时机 | 处理方 | 作用 |
| :--- | :--- | :--- | :--- |
| `[EVENT_CONST]` | [触发时机] | [处理方] | [作用] |
| [...] | [...] | [...] | [...] |

---

## 5. 跨端一致性约束

> 以下字段在双端实现中必须完全一致，detail 阶段校验必须通过。

- **协议字段名**：`[字段名列表]`——双端不得各自定义别名
- **状态枚举值**：`[枚举名: 值列表]`——双端使用相同数值
- **事件/消息名**：`[事件名列表]`——不得拼写不一致

---

## 6. 设计决策记录

### 决策 1：[决策标题]

* **问题**：[需要做决策的具体问题]
* **候选方案**：
  * A — [方案描述]：[优点] / [缺点]
  * B — [方案描述]：[优点] / [缺点]
* **决策**：选择方案 [X]
* **理由**：[为什么选择这个方案]
