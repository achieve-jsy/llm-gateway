# xuanwu-mahjong-game x /xf/design 覆写

> 激活条件：`project_profile: xuanwu-mahjong-game`

## 覆写动作：跳过架构方向交互 + 替换文档生成为 arch-designer SKILL

玄武麻将游戏的架构范式已由 `xuanwu-mahjong-game/tech/bundle.yaml` 固化。客户端 TypeScript + FGUI、服务端 C++、单模块 `mahjong-game` 是既定约束，不需要在命令层再做 A/B/C 架构方向选择。

本 profile 不再执行任何目录预初始化或目标目录初始化。设计阶段的唯一工程规范来源是：

- `xwRef/xuanwu/frameworks/client/xuanwu-mahjong-game/spec-index.md`
- `xwRef/xuanwu/frameworks/server/xuanwu-mahjong-game/spec-index.md`
- 上述索引引用的 `modules/*.md`

### 保留不动

- **前置条件**（含 `xf-gate.mjs --to design` 门禁硬停）：门禁未通过整条命令终止，SKILL 不会被调用
- **步骤 0（Profile 分支加载）**
- **步骤 1（加载上下文）**：仍要读 constitution + change.yaml + spec_*.md + PRODUCT.md
- **步骤 3（技术研究硬规则）**：发现技术不确定项必须自主调 `xf-research` subagent，不允许留 TODO
- **步骤 6（更新 change.yaml.phase = design）**

### 跳过 step 2.2（架构方向探索）

- 架构方向由 `arch-designer` SKILL 读取 `xwRef/xuanwu/xuanwu-mahjong-game/tech/bundle.yaml` 后按玄武双端范式生成。
- 若 SKILL 内部发现必须由人拍板的方向选择，由 SKILL 自行发起询问，不走命令层。

### 跳过 step 2.3（架构方案展示）

由 `arch-designer` SKILL 在 `hld.md` 中完成；命令层不再做独立方案确认轮次。

### 保留 step 2.1（技术范围界定）+ step 2.4（宪章合规检查）

SKILL 调用前仍做轻量技术范围确认。SKILL 调用后对照 `constitution.md` 的核心原则与技术方向做宪章合规校验。

### 追加 step 2.5（命名契约与规范入口校验）

调用 `arch-designer` SKILL 前只校验契约和规范可达性，不复制、不创建、不覆盖任何目标代码目录。

1. 若 `xspec/changes/change.yaml` 缺少以下字段，则向用户询问并写回：
   - `game_id`
   - `game_keyword`
   - `class_prefix`（默认推导：`game_keyword` 大驼峰，如 `fengyun` → `Fengyun`、`hahazhuaji` → `Hahazhuaji`；若推导结果用户确认即可，无需额外询问）
   - `abbr`（默认推导：`game_keyword` 全大写，如 `fengyun` → `FENGYUN`；若推导结果用户确认即可，无需额外询问）
   - `fgui_project_name`（默认 `fgui_<game_id>`）
   - `server_module_name`（默认 `<game_keyword>_server`）
2. 校验以下规范入口存在：
   - `xwRef/xuanwu/frameworks/client/xuanwu-mahjong-game/spec-index.md`
   - `xwRef/xuanwu/frameworks/server/xuanwu-mahjong-game/spec-index.md`
3. 校验双端 `spec-index.md` 引用的 `modules/*.md` 文件存在；任一缺失则停止 `/xf/design`。
4. 设计依据来自上述规范入口和 `modules/*.md`。

### 追加 step 2.6（生成游戏骨架）

在命名契约校验通过后、`arch-designer` SKILL 调用前，运行骨架复制脚本：

```bash
node xwRef/xuanwu/frameworks/scripts/xf-copy-skeleton.mjs \
  --game-id       {change.yaml: game_id} \
  --server-module {change.yaml: server_module_name} \
  --class-prefix  {change.yaml: class_prefix} \
  --abbr          {change.yaml: abbr} \
  --game-keyword  {change.yaml: game_keyword}
```

- 读取 `xspec/changes/change.yaml` 填入上述五个参数。
- 运行命令，捕获其输出（`[OK]` / `[SKIP]` 文件清单）。
- 脚本是幂等的：目标文件已存在则 `[SKIP]`，不覆盖。
- 若脚本退出码非 0，停止命令并向用户报告错误，不进入 `arch-designer`。

---

### 追加 step 2.7（Proto 产物命名契约与协议锚点校验）

骨架复制脚本成功后、`arch-designer` SKILL 调用前，读取 [`.opencode/skills/proto-generator/SKILL.md`](.opencode/skills/proto-generator/SKILL.md)，由该 SKILL 按 `xwRef/xuanwu/xuanwu-mahjong-game/proto/bundle.yaml` 匹配 job 并执行：

```text
proto-generator --profile xuanwu-mahjong-game --module mahjong-game --phase design
```

- HLD 和后续 detail 输入必须使用该 SKILL 固化的固定客户端 proto 产物：
  - `client/assets/{game_id}/lib/proto_cm_protocol.js`
  - `client/assets/{game_id}/lib/proto_cm_protocol.d.ts`
- 不得在 HLD 或 detail 输入中引入玩法名相关的客户端 proto 产物名，例如 `proto_{game_keyword}_cli.*`、`proto_{game_keyword}.*` 或 `proto_hahazhuaji_cli.*`。
- 该 SKILL 校验骨架生成后的协议与阶段枚举锚点，使 `arch-designer` 可以基于真实骨架文件生成 HLD。
- 成功后，`arch-designer` SKILL 可读取 `proto-generator` 返回的骨架锚点（如 `client/assets/{game_id}/protocol/GLCProtocol.ts`、`client/assets/{game_id}/config/GameDefine.ts`）作为协议和阶段枚举的权威依据，而不是自行发明。

### 替换 step 5（生成概要设计文档）

读取 [`.opencode/skills/arch-designer/SKILL.md`](.opencode/skills/arch-designer/SKILL.md)，按该 SKILL 指令执行：

```text
arch-designer --profile xuanwu-mahjong-game --module mahjong-game
```

- **输入**：`xspec/changes/mahjong-game/spec_*.md` + `xspec/constitution.md` + 双端 `spec-index.md`
- **输出**：
  - 主索引：`xspec/changes/mahjong-game/hld.md`
  - 服务端领域文档：`xspec/changes/mahjong-game/hld_server_<id>.md`
  - 客户端领域文档：`xspec/changes/mahjong-game/hld_client_<id>.md`
  - 技术架构报告：`xspec/changes/mahjong-game/技术架构报告.md`
- HLD 必须记录“目标目录由实现阶段按 modules 规范创建或修改”，并记录目标路径、命名契约与规范锚点。
- 严格按 SKILL 内部并行规则执行。

### 更新 change.yaml phase

SKILL 调用和宪章合规校验完成后，将 `xspec/changes/change.yaml` 的 `phase` 字段更新为 `design`。

### 覆写完成报告

```text
✅ 概要设计完成（玄武麻将游戏模式）

模块：mahjong-game (xuanwu-mahjong-game)
技术范围：<step 2.1 确认结果>
命名契约：
  game_id：<game_id>
  game_keyword：<game_keyword>
  class_prefix：<class_prefix>
  abbr：<abbr>
  fgui_project_name：<fgui_project_name>
  server_module_name：<server_module_name>
骨架生成：completed（<x> OK / <y> SKIP）
规范入口：
  客户端：xwRef/xuanwu/frameworks/client/xuanwu-mahjong-game/spec-index.md
  服务端：xwRef/xuanwu/frameworks/server/xuanwu-mahjong-game/spec-index.md
架构输出：
  主索引：xspec/changes/mahjong-game/hld.md
  其他领域文档：<SKILL 返回列表>
  架构报告：xspec/changes/mahjong-game/技术架构报告.md

宪章合规校验：✅ 通过 / ❌ 冲突列表
技术研究：<xf-research 课题数量或"无不确定项">

👉 下一步：执行 /xf/detail
```

### 理由

- 玄武麻将游戏应由 `frameworks/**/modules` 承载架构与实现规则，确保设计与实现规则稳定一致。
- 目标代码目录是否存在是实现阶段问题，不是设计阶段前置条件。
- 骨架在 design 阶段提前生成，使 `arch-designer` 能以真实的 cm_protocol XYID 值和阶段枚举为锚点，避免 HLD 重复发明骨架中已有的协议定义。
