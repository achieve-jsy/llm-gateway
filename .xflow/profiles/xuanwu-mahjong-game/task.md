# xuanwu-mahjong-game x /xf/task 覆写

> 激活条件：`project_profile: xuanwu-mahjong-game`

## 覆写动作：task-work 负责任务生成

任务生成委托给 `task-work` skill 执行。它消费详细设计冻结文档和客户端/服务端模块规范。命令层不要求目标代码目录预先存在。

### 保留不动

- `/xf/task` 门禁检查保持必须执行。`xf-gate.mjs --to task` 失败时立即终止。
- 保留任务质量校验和 `change.yaml.phase = task` 更新。

### 步骤 1.5：命名契约与模块可达性校验

调用 `task-work` 之前：

1. 读取 `xspec/changes/change.yaml`，要求存在以下字段：
   - `game_id`
   - `game_keyword`
   - `class_prefix`
   - `fgui_project_name`
   - `server_module_name`
2. 校验以下规范入口可访问：
   - `xwRef/xuanwu/frameworks/client/xuanwu-mahjong-game/spec-index.md`
   - `xwRef/xuanwu/frameworks/server/xuanwu-mahjong-game/spec-index.md`
3. 不要求目标源码目录在任务生成前存在，但 task-work 必须检查每个输出路径。已存在的目标文件为 `modify`；不存在的为 `create_new`。已存在的骨架或 TODO 桩文件仍为 `modify` / `fill_stub`，不得用 `create_new`。
4. 将 `/xf/detail` 产出的 proto 文件视为已存在的生成输入。`/xf/task` 不得创建以运行 `build_proto.mjs`、`protoc` 或部署 proto 文件为目的的客户端或服务端任务。

### 替换任务生成

读取 `.opencode/skills/task-work/SKILL.md` 并执行：

```text
task-work --profile xuanwu-mahjong-game --module mahjong-game
```

输入：

- `xspec/changes/mahjong-game/design_*.md`
- `xspec/changes/mahjong-game/spec_*.md`
- `xspec/constitution.md`
- 客户端/服务端 `spec-index.md` 及所引用的 `modules/*.md`

输出：

- `xspec/changes/mahjong-game/task-handoff.json`
- `xspec/changes/mahjong-game/task-report.md`
- 客户端分片：`data`、`cmd`、`controller`、`fgui_resource`、`view_render`、`peripheral`
- 服务端分片：`xyid_message`、`step_flow`、`common_game`、`rule`、`settlement`、`relink_config`

任务约束：

- 每个任务必须包含规范锚点字段。首选字段标签为 `规范锚点`；`参考锚点` 或 `spec_anchor` 仅在指向客户端/服务端 xuanwu-mahjong-game 模块路径时接受，不得指向目标源码。
- 新文件用 `create_new`；已有文件用 `modify`；生成产物用 `verify_generated`。
- 服务端可执行任务路径必须是真实的仓库相对路径，以 `server/game_server/Logic/mahjong_server/src/games/` 为根。`common_game/...`、`{server_module_name}/...`、`hahazhuaji_server/...` 等短路径只能出现在说明性注释中，不得出现在 `文件`、`write_set`、task handoff 条目或可执行 IU 路径中。
- common_game 的规范路径为 `server/game_server/Logic/mahjong_server/src/games/common_game/...`。
- 私有玩法服务端的规范路径为 `server/game_server/Logic/mahjong_server/src/games/{server_module_name}/...`，其中 `{server_module_name}` 来自 `xspec/changes/change.yaml`。
- Proto 源文件和生成产物在 `/xf/detail` 阶段产出；任务生成可将其作为锚点引用，或在冻结项明确要求校验时添加 `verify_generated` 检查，但不得生成 `proto_build` / `proto_deploy` 实现任务或对其建立依赖。
- 客户端 proto 产物名称固定，因为 `/xf/detail` 以 `--client-name cm_protocol` 调用 `build_proto.mjs`。任务生成只能引用 `client/assets/{game_id}/lib/proto_cm_protocol.js` 和 `client/assets/{game_id}/lib/proto_cm_protocol.d.ts` 作为客户端 proto 输入，不得发明 `proto_{game_keyword}_cli.*` 或 `proto_{game_keyword}.*` 等特定玩法名称。
- FGUI 任务必须同时覆盖 FGUI 工程包和手动维护的客户端声明文件。`fgui_uideclare` 下的 `.d.ts` 文件由人工手写维护，不通过任何 FGUI 发布工具生成；不得生成调用 `publishFGUI.py` 或产出 `.bin` 文件的任务。对每个 `{package_name}`，task-work 须检查 `client/assets/{game_id}/fgui_uideclare/{package_name}/` 是否存在：存在则为 `modify`，不存在则为 `create_new`。按现有项目资源规范包含对应的 `.meta` 文件。
- 没有真实目标路径或规范锚点的任务标记为 `blocked`。
- 一个任务最多改动两个文件。
- 客户端任务必须将生成的 `SKELETON_CONTRACT.md` 作为层边界来源。已有骨架文件为 `modify` 任务；只有骨架中不存在的玩法动作才可新建 Cmd/View/Data 文件。
- 客户端分片必须优先覆盖最小循环：`GP_SEND_CARD`、`GP_DRAW_CARD`、`GP_PLAY_POWER`、`GP_OUT_CARD`、`GP_GAME_END` 和 `sendMsgOutCard`。
- `client-peripheral` 保留给复盘、重连、托管等外围行为，不得拥有发牌/摸牌/出牌/结束基础链路。
- 服务端 `common_game` 分片负责 common_game 复用、GameLogic 入口连接和 Step 注册，这些项目默认不得留给 gap-scan 扫描。
- 服务端 `step_flow` 分片负责 Step4～Step9 内部流程和协议接缝。Step7/Step8/Step9 必须由分片自身覆盖、标记为 blocked 或 intentionally_merged，不得留给 gap-scan。

### 集成视图

由于此 profile 只有一个模块，生成最简 `xspec/changes/integration.md`：

```markdown
# <change-id> 集成视图

模块: mahjong-game
跨模块依赖: N/A
集成任务: N/A

任务执行完整描述见 `mahjong-game/task-handoff.json` 及其声明的任务分片文件。
```

### 完成报告

```text
任务生成完成（xuanwu-mahjong-game）

模块: mahjong-game
规范校验: 通过
Handoff: xspec/changes/mahjong-game/task-handoff.json
集成视图: xspec/changes/integration.md

下一步: /xf/implement
```
