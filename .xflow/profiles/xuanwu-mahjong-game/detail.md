# xuanwu-mahjong-game x /xf/detail 覆写

> 激活条件：`project_profile: xuanwu-mahjong-game`

## 覆写动作：调用 proto-generator + 跳过方案探索交互 + 替换步骤 4 为 design-detailer SKILL

### 保留不动

- 前置条件：`constitution.md`、`change.yaml.phase in {design, detail}`、
  `spec.md`、HLD 制品均须存在。
- detail 命令本身没有人工门禁；门禁在 `/xf/task` 前检查。
- 技术不确定项必须立即由研究解决，不允许在设计制品中留 TODO、待研究、
  或"实现阶段再确认"等延迟处理。
- 完成前运行 `xf-validate.mjs --phase detail` 和语义自检。
- 全部必要产出与自检通过后，才将 `xspec/changes/change.yaml` 的 `phase`
  更新为 `detail`。

---

### 追加 0.5（Proto 文件生成与编译）

在步骤 0（Profile 分支加载）完成后、步骤 1（加载上下文）开始前执行。

读取 [`.opencode/skills/proto-generator/SKILL.md`](.opencode/skills/proto-generator/SKILL.md)，由该 SKILL 按 `xwRef/xuanwu/xuanwu-mahjong-game/proto/bundle.yaml` 匹配 job 并执行：

```text
proto-generator --profile xuanwu-mahjong-game --module mahjong-game --phase detail
```

- 该 SKILL 负责从 HLD 协议总表生成 `xspec/changes/mahjong-game/runtime/cm_protocol.proto`，并使用 `build_proto.mjs --force` 驱动完整编译流水线。
- `cm_protocol.proto` 是唯一设计 proto 产物，与其他 `xspec/` 产出一同提交。
- 客户端 proto 产物名称由 `div-build-protobuf/run.bat` 流水线固定，`design-detailer` 只能引用 `client/assets/{game_id}/lib/proto_cm_protocol.js` 和 `client/assets/{game_id}/lib/proto_cm_protocol.d.ts`。
- 不得使用 `proto_game_protocol.*`、`proto_{game_keyword}.*` 或其他玩法名相关产物名。
- 脚本退出码非 0、产物缺失或命名契约不满足时，按 SKILL 规则返回 `blocked`，停止后续步骤并向用户报告错误。
- 成功后，`design-detailer` 可读取生成的 `client/assets/{game_id}/lib/proto_cm_protocol.js` 和 `server/.../protocol/cm_protocol.pb.h` 作为 XYID 的权威锚点。

---

### FGUI 包与声明规则

详细设计新增或修改任何 FGUI 包时，设计产出必须描述每个 `{package_name}` 的两个目标位置：

- FGUI 工程包：`client/fgui_game/{fgui_project_name}/assets/{package_name}/`
- 客户端声明文件（手动维护）：`client/assets/{game_id}/fgui_uideclare/{package_name}/`

`fgui_uideclare` 目录及其 `.d.ts` 文件**由人工手动维护**，不通过 FGUI 发布流程生成。它们是供实现任务消费的客户端可见产物。对每个 FGUI 包，设计必须说明对应的 `fgui_uideclare` 目录和声明文件是应修改还是新建。任务生成根据仓库实际状态决定最终操作：已存在的声明路径为 `modify`，不存在的为 `create_new`。按现有项目资源规范包含对应的 `.meta` 文件。不得生成调用 `publishFGUI.py` 或任何 FGUI 发布工具产出 `.bin` 文件的任务。

---

### 追加 1.5（契约与规范入口校验）

在步骤 1（加载上下文）完成后、步骤 2 开始前执行。

校验命名契约、框架规范入口及步骤 0.5 产物：

1. 读取 `xspec/changes/change.yaml`，要求存在以下字段：
   - `game_id`
   - `game_keyword`
   - `class_prefix`
   - `fgui_project_name`
   - `server_module_name`
2. 校验以下文件可达：
   - `xwRef/xuanwu/frameworks/client/xuanwu-mahjong-game/spec-index.md`
   - `xwRef/xuanwu/frameworks/server/xuanwu-mahjong-game/spec-index.md`
   - 双端各自至少一个 `modules/*.md`
3. 校验步骤 0.5 产物均已存在：
   - `xspec/changes/mahjong-game/runtime/cm_protocol.proto`（设计产物）
   - `client/assets/{game_id}/lib/proto_cm_protocol.js`
   - `client/assets/{game_id}/lib/proto_cm_protocol.d.ts`
   任一缺失则停止并提示用户重新执行步骤 0.5。
4. 不检查目标源码目录是否存在。目标文件和目录由实现阶段的任务按模块契约创建。

---

### 跳过 3（方案探索）

理由：玄武麻将游戏架构范式已由 `frameworks/**/modules` 固化，双端技术栈与
设计决策由 `design-detailer` SKILL 通过 `tech-design/bundle.yaml` 驱动，
无需命令层的交互式方案探索（步骤 3.1/3.2/3.3 全部跳过）。

---

### 替换 4（逐模块生成技术文档）

改为调用 `design-detailer` SKILL：

```text
design-detailer --profile xuanwu-mahjong-game --module mahjong-game
```

输入：

- `xspec/changes/mahjong-game/hld*.md`
- `xspec/changes/mahjong-game/spec_*.md`
- 双端 `spec-index.md` 及按需引用的 `modules/*.md`
- 生成的客户端协议文件：`client/assets/{game_id}/lib/proto_cm_protocol.js`

输出：

- `xspec/changes/mahjong-game/design.md`
- `xspec/changes/mahjong-game/design_client_freeze.md`
- `xspec/changes/mahjong-game/design_server_freeze.md`
- `xspec/changes/mahjong-game/design_client_tasks.md`
- `xspec/changes/mahjong-game/design_server_tasks.md`
- `xspec/changes/mahjong-game/design_client_self_check.md`
- `xspec/changes/mahjong-game/design_server_self_check.md`
- `xspec/changes/mahjong-game/design-report.md`

每项设计决策必须引用 `modules/*.md` 中的规范锚点。

---

### 覆写完成报告

```text
详细设计完成（xuanwu-mahjong-game）

模块：mahjong-game
命名契约：通过
规范来源：双端 spec-index.md + modules/*.md
Proto 生成：
  设计产物：xspec/changes/mahjong-game/runtime/cm_protocol.proto
Proto 编译流水线：完成
  服务端：server/.../protocol/cm_protocol.pb.cc
          server/.../protocol/cm_protocol.pb.h
  合并产物：server/.../protocol/cm_protocol_merged.proto
  客户端：client/assets/{game_id}/lib/proto_cm_protocol.js
          client/assets/{game_id}/lib/proto_cm_protocol.d.ts
自检：通过 / 失败

下一步：审阅后执行 xflow approve，再运行 /xf/task
```
