# [MODULE_NAME] 麻将游戏概要设计

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | [CHANGE_ID] |
| **模块名称** | [MODULE_NAME] |
| **模块类型** | xuanwu-mahjong-game |
| **创建时间** | [CREATED_AT] |

### 修订记录

| 版本号 | 修改日期 | 修改内容摘要 | 修改人 |
| :--- | :--- | :--- | :--- |
| v1.0 | [CREATED_AT] | 初稿创建 | [AUTHOR] |

---

## 1. 共享契约

> 双端实现的唯一真相来源——此处字段一旦确定，`hld_server.md` 与 `hld_client.md` 必须与此保持一致，不得各自定义。

| 字段 | 值 | 说明 |
| :--- | :--- | :--- |
| **游戏字符串 key** | `[如：mahjong_xxx]` | 客户端 `GAME_ID` / 源码目录名，全局唯一 |
| **TYPE_ID（数字游戏 ID）** | [int，待平台注册后填入] | 服务端注册用；FGUI 工程目录 `fgui_{TYPE_ID}` 依此确定 |
| **XYID 协议常量前缀** | `[如：XYID_FY_]` | GLCProtocol 中所有协议常量的命名前缀；§5.2 路由表中每条消息必须有对应的 XYID 常量名和数值，不得留空 |
| **game_script** | `[C++ 路径]` | 服务端 C++ 落点 |
| **客户端 FGUI 工程路径** | `client/fgui_game/fgui_{TYPE_ID}/` | TYPE_ID 确认后替换占位符；禁止使用字符串 key 拼接路径 |
| **数据返回结构根字段** | `[字段名列表]` | info 快照公共字段集合 |

---

## 2. 开发模式选择

> 根据游戏类型选择最合适的开发基础。

| 维度 | 选择 | 理由 |
| :--- | :--- | :--- |
| **服务端模式** | [新建独立玩法目录 / 复用 common_game / 扩展已有麻将游戏] | [简述理由] |
| **客户端模式** | [新建独立模块 / 复用现有 View+Module / 扩展已有麻将游戏] | [简述理由] |
| **参考案例** | [类似游戏名称或路径] | [相似点说明] |

---

## 3. 服务端架构概要

### 3.1 C++ 落点

| 元素 | 路径 / 值 |
| :--- | :--- |
| 入口类/文件 | `[真实路径，如：server/game_server/Logic/mahjong_server/src/games/{game}_server/GameLogic.cpp]` |
| 入口接口 | `GameLogic` / `OnGameStart` / `OnUserOutCard` / Step 回调 |
| cmdid 分发方式 | [单入口按 cmd 分发 / 多 handler 分发] |

### 3.2 命令职责

| cmdid | 名称 | 职责描述 |
| :--- | :--- | :--- |
| [cmdid] | [协议名] | [服务端处理逻辑概述] |
| [...] | [...] | [...] |

### 3.3 状态机与服务端状态概要

```
[初始] --触发条件--> [进行中] --完成条件--> [已完成] --领取--> [已领取]
```

| 状态/字段 | 用途 | 生命周期 |
| :--- | :--- | :--- |
| `[key 模式，如：act:{TYPE_ID}:{uid}:daily]` | [用途] | [生命周期，如：单局 / 小局 / 大结算] |
| [...] | [...] | [...] |

### 3.4 外部依赖

| 依赖 | 用途 | 失败时 |
| :--- | :--- | :--- |
| [如：结算/流水服务] | [结算] | [拒绝操作 / 记录异常 / 断线恢复] |
| [...] | [...] | [...] |

---

## 4. 客户端架构概要

### 4.1 模块落点

| 元素 | 路径 |
| :--- | :--- |
| 麻将游戏目录 | `client/assets/{game_key}/`（game_key 为 §1 游戏字符串 key） |
| View 文件 | `[路径，如：XxxView.ts]` |
| Module 文件 | `[路径，如：XxxModule.ts]` |
| Data 文件 | `[路径，如：XxxData.ts]` |
| FGUI 工程 | `client/fgui_game/fgui_{TYPE_ID}/`（TYPE_ID 为 §1 数字游戏 ID，禁止用字符串 key 替代） |
| 运行时 FGUI 包 | `client/assets/{game_key}/fgui` |
| UI 声明 | `client/assets/{game_key}/fgui_uideclare` |

### 4.2 宿主挂载点

| 挂载点 | 类型 | 条件 |
| :--- | :--- | :--- |
| [如：大厅入口] | [图标 / 横幅 / Tab] | [显示条件] |
| [如：强弹] | [弹窗] | [触发条件与频次] |
| [如：红点] | [红点] | [出现条件与消除条件] |

### 4.3 事件表

| 事件常量 | 方向 | 触发时机 |
| :--- | :--- | :--- |
| `[EVENT_NAME]` | Module→View | [触发场景] |
| `[EVENT_NAME]` | [...] | [...] |

---

## 5. 双端协议总表

> **SSoT 原则**：proto 文件是消息结构与 XYID 值的唯一来源。XYID 值 = proto 中每条 message 内的 `MSGID { ID = xxx; }` 枚举值，由开发者在 proto 中自行定义，双端共用同一份文件，**不存在"服务端分配"环节**。`hld_client.md` 与 `hld_server.md` 均引用此处，不得各自重新定义字段或编号。

**proto 文件路径**：`[如：xspec/changes/{change}/proto/{game_key}.proto]`（实现时同步放入服务端协议目录）
**GLCProtocol 文件**：`client/assets/{game_key}/protocol/GLCProtocol.ts`（XYID 常量读自 `proto.{namespace}.msgXxx.MSGID.ID`）
**命名空间**：`[如：mahjong_fengyun_cli]`（与游戏字符串 key 对应）
**全局 ID 枚举**：`CMXY_[PREFIX]_XYID`（起始 ID 需与平台全局 ID 注册表确认无冲突）

### 5.1 XYID 机制说明

XYID 值内嵌于 proto message 的 `MSGID` 枚举，客户端和服务端均从同一份 proto 读取，天然一致：

```proto
// proto 文件中每条 message 的结构模式
enum CMXY_[PREFIX]_XYID {
    CMXY_[PREFIX]_NONE  = 0;
    CMXY_[PREFIX]_FIRST = [起始值]; // 需与平台全局 ID 注册表确认

    CMXY_[PREFIX]_XXX          = [起始值+1]; // S2C 消息
    CMXY_[PREFIX]_CLIENT_XXX   = [起始值+N]; // C2S 消息

    CMXY_[PREFIX]_LAST = [上限值]; // 预留扩展空间
}

message msg[PREFIX]Xxx {
    enum MSGID {
        NONE = 0;
        ID   = [起始值+1]; // 与全局枚举值一致
    }
    int32          field_a = 1;
    repeated int32 field_b = 2;
}

message msg[PREFIX]ClientXxx {
    enum MSGID {
        NONE = 0;
        ID   = [起始值+N];
    }
    int32 field_a = 1;
}
```

客户端读取方式：
```typescript
XYID_[PREFIX]_XXX = cm_protocol.msg[PREFIX]Xxx.MSGID.ID  // 直接从 proto 读
```

### 5.2 XYID 路由表

> XYID 值直接来自 proto 文件的 `MSGID.ID`，本表与 proto 文件保持同步。

| 方向 | XYID 常量（客户端） | XYID 值 | proto message | 状态影响 |
| :--- | :--- | :---: | :--- | :--- |
| S2C | `XYID_[PREFIX]_XXX` | [int] | `msg[PREFIX]Xxx` | [说明] |
| C2S | `XYID_[PREFIX]_CLIENT_XXX` | [int] | `msg[PREFIX]ClientXxx` | [说明] |

---

## 6. 待研究事项

| 编号 | 待研究问题 | 优先级 | 影响范围 |
| :--- | :--- | :--- | :--- |
| R-01 | [不确定的技术点] | 高 / 中 / 低 | [影响哪些设计决策] |

---

## 7. 风险与对策

| 风险 | 概率 | 影响 | 对策 |
| :--- | :--- | :--- | :--- |
| [风险描述] | 高/中/低 | 高/中/低 | [应对策略] |

---

## 8. 附属文档索引

| 文档 | 路径 | 说明 |
| :--- | :--- | :--- |
| 服务端技术概要 | `hld_server.md` | C++ 入口、Step 阶段机、协议、结算与 common_game 复用 |
| 客户端技术概要 | `hld_client.md` | 模块划分、FGUI 结构、宿主挂载点 |

---

## 9. 概要设计确认

- [ ] 共享契约字段已确定（游戏字符串 key / TYPE_ID / XYID 协议前缀 / FGUI 工程路径 / 返回结构根字段）
- [ ] 独立 proto 文件已创建（含 `CMXY_[PREFIX]_XYID` 全局枚举 + 全部 message 的 `MSGID.ID` 定义）
- [ ] proto ID 段起始值已与平台全局 ID 注册表确认无冲突
- [ ] §5.2 XYID 路由表中每条消息的常量名和数值均已填入（值来自 proto MSGID.ID，不得留空进入详设阶段）
- [ ] 开发模式已选择并记录理由
- [ ] 服务端 C++ 落点已确定
- [ ] 客户端模块落点已确定
- [ ] 待研究事项已识别

👉 确认后进入详细设计阶段（`/xf/detail`）
