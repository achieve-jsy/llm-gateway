# xuanwu-mahjong-game × /xf/split 覆写

> 激活条件：`project_profile: xuanwu-mahjong-game`

## 覆写动作：终止整条命令

玄武麻将游戏项目是"一仓库一麻将游戏"的单模块工程，模块名永远是 `mahjong-game`，类型永远是 `xuanwu-mahjong-game`，不存在"拆分"的语义。`/xf/split` 在本 profile 下应直接终止。

### 执行指令

当检测到 `project_profile = xuanwu-mahjong-game` 时：

1. **不要**执行 split.md 的任何"场景一"或"场景二"步骤
2. **不要**询问用户模块划分方案
3. **不要**更新 `xspec/modules/PRODUCT.md`（其模块结构应由 `/xf/product` 直接写死，见 `product.md` 方言）
4. 立即打印下方固定提示并结束命令

### 固定输出

```
ℹ️  当前项目 project_profile = xuanwu-mahjong-game

玄武麻将游戏项目是单模块工程（模块名恒为 mahjong-game，类型恒为 xuanwu-mahjong-game），
不需要运行 /xf/split。

如果你的意图是：
  - 启动新麻将游戏需求 → 执行 /xf/brainstorm 或 /xf/clarify
  - 查看当前模块状态 → 执行 /xf/status
  - 改为通用多模块项目 → 编辑 xspec/constitution.md 将 project_profile 改为 general
```

### 理由

`/xf/split` 的语义是"将产品功能拆分为独立模块"。在单麻将游戏工程里，模块边界已经由仓库边界完全等价地决定，强行运行 split 只会产生无意义的确认问答和潜在的误修 `PRODUCT.md`。

### 硬性约束（不开后门）

本方言的"终止"是 xuanwu-mahjong-game 的**硬性约束**，不提供 `--force` 或任何其他参数级绕过。如果用户真的需要在本仓库里做多模块拆分，唯一合法路径是：

1. 先在 `xspec/constitution.md` 将 `project_profile` 从 `xuanwu-mahjong-game` 改为 `general`（`/xf/constitution` 修订场景会弹二次确认）
2. 再执行 `/xf/split`

这样可以保证"切 profile"这个决定被正式记录在宪章里，而不是在 split 命令里偷偷翻转。
