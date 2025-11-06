# xuanwu-activity × /xf/archive 覆写

> 激活条件：`project_profile: xuanwu-activity`

## 覆写动作：跳过 copy-new + 归档命名体现业务名

玄武活动项目是"一仓库串行多活动"模型：当前活动归档后，下一个活动复用同名 `activity` 模块目录。为防止上一个活动的内容被当作下一活动的增量基线，**归档时不把 `xspec/changes/activity/` 沉淀到 `xspec/modules/activity/`**。

脚本层已实现（见 `.xflow/scripts/xf-archive.mjs`）：xuanwu profile 下 `copy-new` 自动跳过。命令层只需按此编排步骤即可。

### 执行指令

#### 步骤 1：加载上下文 + 预检

1. 调用 `node .xflow/scripts/xf-profile.mjs` 确认 profile = xuanwu-activity
2. 读取 `xspec/changes/change.yaml`，校验：
   - `affected_modules` 必须恰好是 `[activity]`，否则警示用户数据异常并中止
   - `change.yaml.name` 应有业务名；缺失时提示用户补充后再归档
3. 读取 `xspec/modules/` 当前状态：
   - 理想情况：`xspec/modules/activity/` 不存在或为空（因为上次归档已跳过 copy-new）
   - 若存在且非空：打印警示（可能是历史 general 模式遗留）并询问是否删除 / 备份 / 中止

#### 步骤 2：生成 changelog

正常生成 `xspec/changes/changelog.md`，在首行追加：

```
> 活动业务名：<change.yaml.name>
> 项目 profile：xuanwu-activity（单模块串行活动工程）
```

#### 步骤 3：调用 xw-archiver SKILL

读取 [`.opencode/skills/xw-archiver/SKILL.md`](.opencode/skills/xw-archiver/SKILL.md)，由当前主 Agent 执行：

```text
xw-archiver --profile xuanwu-activity --module activity
```

#### 步骤 4：调用 archive 脚本

**跳过 copy-new**（脚本层即便被调用也会短路，但命令层显式跳过更清晰）：

```bash
# 直接调 finalize，不调 copy-new
node .xflow/scripts/xf-archive.mjs --step finalize
```

#### 步骤 5：归档后校验

- `xspec/archive/<change-id>/` 存在且包含完整 `activity/` 子目录
- `xspec/changes/` 已清空（只保留 `.gitkeep`）
- `xspec/modules/activity/` 保持**不存在**或为空（这是玄武模式的关键不变量）

#### 覆写完成报告

```
✅ 活动归档完成（玄武活动模式）

业务名：<name>
change_id：<id>
归档路径：xspec/archive/<id>/（完整含 spec / hld / design / task / changelog）
xspec/modules/ 状态：保持空（玄武模式不沉淀历史活动）

历史查询：直接浏览 xspec/archive/ 下对应 change-id 目录
下一活动启动：
  1. （可选）确认 constitution.md 的 project_profile 仍为 xuanwu-activity
  2. 执行 /xf/brainstorm 或 /xf/clarify 开启新活动
```

### 注意事项

- **禁止**在本 profile 下手动创建 `xspec/modules/activity/`，这会破坏串行隔离假设
- 若用户确实需要把某个历史活动作为后续活动的基线参考，应从 `xspec/archive/<old-id>/activity/` 手动复制文件到新 change 的 `xspec/changes/activity/` 中，而不是让 `xspec/modules/` 承担基线职责

### 理由

- 单模块名 + 多次活动的模型下，xspec/modules/ 扮演"持续演进的单一真理"会导致内容混淆，必须通过不沉淀彻底隔离
- 命令层显式跳 copy-new 比依赖脚本内部短路更健壮，也让行为对读命令文件的人更透明
