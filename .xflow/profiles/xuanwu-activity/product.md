# xuanwu-activity × /xf/product 覆写

> 激活条件：`project_profile: xuanwu-activity`

## 覆写动作：scaffold 前置检查 + 简化问答 + 模块结构锁定 + 双端代码位置写入 PRODUCT.md

玄武活动项目的产品定位不需要完整的"多模块规划"：模块结构天然就是单模块 `activity`（type=xuanwu-activity）。`/xf/product` 在本 profile 下：

1. 首先做 scaffold 前置检查，确保 `client/` + `lua/` 双端目录已经初始化
2. 用 3 个必答项收集业务级信息
3. 直接锁定单模块结构和代码位置，不询问

### 追加：scaffold 前置检查（在通用流程任何询问之前执行）

玄武双端工程依赖 `client/`（FGUI）+ `lua/`（AwardLua）目录承载代码。`/xf/product` 是 xuanwu-activity 链路里**第一个会问"代码位置"且后续 SKILL（arch-designer / code-generator）依赖代码目录的命令**，因此 scaffold 前置检查在此命令落地最合适（宪章是项目级原则约束，不该承担业务环境检查职责）。

1. 检测仓库根目录是否同时存在 `client/` 和 `lua/` 两个目录
2. 若有缺失，向用户提示：

   ```
   📦 玄武活动工程目录尚未初始化：
      <列出缺失的目录：client/ → FGUI 客户端代码目录；lua/ → AwardLua 服务端代码目录>

   请在终端执行以下之一完成 scaffold 后再重试本命令：
      macOS / Linux:  sh scaffold.sh
      Windows:        .\scaffold.bat

   scaffold 完成后将自动具备 client/ 与 lua/ 双端目录，下游 arch-designer / code-generator
   等 SKILL 依赖此目录布局。
   ```

3. 检查失败时**终止本命令**，不进入下方任何步骤，不写入 PRODUCT.md
4. 检查通过后继续执行下方"替换通用问答式需求采集"

### 替换通用 product.md 的所有问答式需求采集，改为：

1. 询问用户（3 个必答项）：
   - Q1：本次玄武项目的业务名/代号是什么？（例："春节签到"、"夏日嘉年华招财猫"）
   - Q2：面向玩家的核心价值一句话？（例："登录签到拿奖励，7 天累签送限定皮肤"）
   - Q3：需求文档位置？（默认 `doc/需求文档.md`；支持本地 Markdown / Word 路径，或飞书 URL。后续 `/xf/clarify` 会把此路径写入 `change.yaml.source_doc`，由 `req-extractor` SKILL 消费）
2. 模块结构**直接写死**，不询问：
   ```markdown
   ## 模块结构

   本项目为玄武游戏活动工程，采用单模块架构：

   | 模块 | 类型 | 职责 |
   | :--- | :--- | :--- |
   | activity | xuanwu-activity | 当前活动的全部服务端 + 客户端实现 |

   > 客户端代码位于仓库根目录 `client/`（FGUI），服务端代码位于 `lua/`（AwardLua）。
   > 代码位置由 scaffold 流程固定，不可在 PRODUCT.md 中改动。下游 SKILL（arch-designer /
   > code-generator）会按此约定读写代码目录。
   >
   > 玄武活动项目是"一仓库 = 一个活动"的串行工程。活动归档后，下一个活动会在相同的
   > `activity` 模块目录下重新开始，历史活动通过 `xspec/archive/<change-id>/` 查阅。
   ```
3. 其余章节按通用 product.md 模板生成，但**跳过**：
   - 多模块依赖关系 mermaid 图（直接省略该段）
   - 模块粒度讨论（已经锁死）

### 输出产物

- `xspec/modules/PRODUCT.md`（精简版）

### 完成报告（替换通用版本）

```
✅ 玄武活动项目产品定位完成

业务名：<用户输入>
核心价值：<用户输入>
需求文档：<用户输入>（默认 doc/需求文档.md）
模块结构：单模块 activity（类型 xuanwu-activity，已锁定）
代码位置：client/（FGUI 客户端）+ lua/（AwardLua 服务端）

产品定位文档：xspec/modules/PRODUCT.md

👉 下一步：执行 /xf/brainstorm 或 /xf/clarify 开始采集当前活动的具体需求
```

### 理由

- 玄武活动的模块结构是 profile 已经决定的常量，不应在 product 阶段重新讨论
- 让 product 阶段只采集"这是什么活动"这种真正需要用户输入的信息，体验更顺
- 保留业务名 + 核心价值是为了给后续 clarify 和归档提供上下文
- scaffold 前置检查放在 product 而非 constitution：宪章是项目级原则约束，不应携带"业务环境就绪与否"这类业务相关检查；product 是项目里第一个真正消费 client/lua 双端结构的命令，在此卡住既符合"使用前检查"的语义，也保持了宪章的纯粹性
- 双端代码路径只在 PRODUCT.md 中体现（不进宪章）：路径属于产品/工程结构层面，宪章只关心"原则"
