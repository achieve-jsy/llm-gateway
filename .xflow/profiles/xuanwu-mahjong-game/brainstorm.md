# xuanwu-mahjong-game × /xf/brainstorm 覆写

> 激活条件：`project_profile: xuanwu-mahjong-game`

## 覆写动作：收敛阶段锁定单模块 mahjong-game

`/xf/brainstorm` 分两阶段：**发散**（开放式引导）和**收敛**（产出 change.yaml + spec 骨架）。本 profile 只覆写**收敛阶段**，发散阶段保持原生的开放式激发创意的风格（CLAUDE.md 交互规则对 brainstorm 第一阶段有豁免）。

### 执行指令

#### 发散阶段：不覆写

保持原 brainstorm.md 的自由发散流程。允许 AI 以开放式问题帮用户探索麻将游戏玩法、奖励形式、节日主题等。

> 仅追加一条提示（在发散前打印一次）：
> ```
> 🎮 当前 profile = xuanwu-mahjong-game。发散完成后，产物会锁定为单模块 mahjong-game。
> 如需换用多模块发散（例如还不确定是不是麻将游戏项目），请先在 constitution 中把 project_profile 改回 general。
> ```

#### 收敛阶段：套用 clarify 方言的产物规则

收敛环节产出 `change.yaml` 和 spec 骨架时，按 `profiles/xuanwu-mahjong-game/clarify.md` 的 **step 1.7.2 / 1.7.3** 覆写规则执行（这两步在 clarify 方言里是"SKILL 提取前的基础设施就位"）：

1. `change.yaml` 的 `affected_modules` 恒为 `[mahjong-game]`，类型 `xuanwu-mahjong-game`，`change_type: added`
2. `project_profile` 字段强制写入 `xuanwu-mahjong-game`
3. `name` 字段写入本次麻将游戏的业务名（发散阶段用户已经表达过，AI 提取即可）
4. 不要询问用户"影响哪些模块"——直接写死
5. scaffold 命令：`node .xflow/scripts/xf-scaffold.mjs --module mahjong-game --type xuanwu-mahjong-game`

**本阶段不调用 `req-extractor` SKILL**。SKILL 的触发放在后续 `/xf/clarify` 的 step 1.7.4 统一处理，避免双入口触发 SKILL 导致的重复提取 / 输出覆盖问题。`/xf/clarify` 重入 step 1.7 时对 `change.yaml` 和 scaffold 产物是幂等操作，不会丢失 brainstorm 的产出。

#### 覆写完成报告

```
✅ 头脑风暴收敛完成（玄武麻将游戏模式）

业务名：<name>
创意亮点：<1-3 条>
模块：mahjong-game (xuanwu-mahjong-game) — added
spec 骨架：<N> 个文件已 scaffold 到 xspec/changes/mahjong-game/（spec_*.md 尚未填充内容）

👉 下一步：执行 /xf/clarify
  clarify 会自动：
    1) 运行 req-extractor SKILL 从需求文档提取 spec_*.md
    2) 基于未明确项向你针对性提问
    3) 把你的回答事务式回写到 spec_*.md
```

### 理由

- 发散阶段保留开放式风格是 xflow 的既定设计，不应被 profile 推翻
- 收敛阶段的产物必须和 clarify 一致，否则两条入口路径产出的 change.yaml 结构不一致，后续命令会分叉
- 复用 clarify 方言的 1.7.2 / 1.7.3 规则，避免重复描述
- SKILL 触发只挂在 clarify 单入口：防止 brainstorm → clarify 串跑时 SKILL 被调用两次（第二次会清空目录并重新生成），也让"何时需要需求文档"这件事只在 clarify 里关心
