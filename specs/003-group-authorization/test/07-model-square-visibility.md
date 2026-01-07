# 测试任务 07: 模型广场按角色过滤

## 设计目标

`/model-square` 是所有用户（含 normal_user）的**通用入口页**。本测试验证其在新交互架构下的**角色感知展示**：

> **核心原则**：用户看不到「自己用不了」的东西。
> normal_user 看到的是「我能用的所有模型」，完全不知道还有受控组存在。
> manager 角色能看到「我管辖范围内可操作的模型」，并能在广场内**直接操作开通**。

## 模型广场设计（新）

卡片统一外观（每张卡片结构相同）：
- 卡片标题：组名
- 卡片顶部：能力 tag 行（推理/代码/对话/长文本 等）
- 卡片主体：开放模式 pill（🟢 公开 / 🟠 受控）+ 状态描述
- 卡片 footer：按角色不同出现不同操作按钮

卡片可见性（按角色）：

| 角色 | 看到什么 | 看不到什么 |
|------|---------|----------|
| normal_user | public 组 + 本部门已开通 controlled 组 | **完全不知道**还有其他受控组存在 |
| dept_admin | public 组 + 本**工作室已授权**的 controlled 组（含"已开通"和"待开通"两种状态） | 本工作室未被授权的 controlled 组 |
| studio_admin | public 组 + **本工作室已授权**的 controlled 组 + 开通统计 | 本工作室未被授权的 controlled 组 |
| admin | 全部（含 disabled 组，标灰） | 无 |

## 前置数据

| 组 | 状态 |
|----|------|
| `deepseek-chat` | `open_mode=public, enabled=true` |
| `claude-opus` | `open_mode=controlled, enabled=true`，已授权给 studio A，D1 部门已开通 |
| `gpt-4` | `open_mode=controlled, enabled=true`，已授权给 studio B（非 studio A） |
| `gemini-pro` | `open_mode=controlled, enabled=true`，未授权任何工作室 |
| `qwen-max` | `open_mode=public, enabled=false`（已禁用的 public 组） |

---

## 第一部分：normal_user 视角（7.1 - 7.5）

### 测试 7.1: normal_user 只看到 public 组 + 本部门已开通组

**步骤**:
1. normal_user 登录 → `/model-square`
2. 观察页面上所有卡片

**预期结果**:
- ✅ 看到 `deepseek-chat` 卡片（public，本部门可见）
- ✅ 看到 `claude-opus` 卡片（controlled，本部门 D1 已开通）
- ❌ **不显示** `gpt-4`（controlled，本部门未开通）
- ❌ **不显示** `gemini-pro`（controlled，无任何授权）
- ❌ **不显示** `qwen-max`（public 但 disabled，全员不可见）
- 页面**没有**任何「你还有以下模型未开通」提示
- 页面**没有**「申请开通」「联系管理员」等链接

### 测试 7.2: normal_user 卡片状态标识

**步骤**: 观察每个卡片的 pill 和状态描述

**预期结果**:
- `deepseek-chat`: 🟢 pill「公开」+ 「全员可用」字样
- `claude-opus`: 🟠 pill「受控」+ 「本部门可用」**或** 🟢 pill「本部门可用」（设计待确认，倾向后者）
- 两卡片的视觉外观一致（都是"已开通"状态），无明显"受限"感受

### 测试 7.3: normal_user 卡片无管理操作

**步骤**: 尝试点击卡片各区域

**预期结果**:
- 卡片**无**「开通」「撤销」「管理」等按钮
- 唯一可能的交互：点击卡片进入详情（如有模型详情页）
- 无任何管理入口泄露

### 测试 7.4: normal_user 不能通过 URL 参数触发开通

**步骤**:
```
/model-square?open=gpt-4
```
或 hash：
```
/model-square#entitle=gpt-4
```

**预期结果**:
- URL 参数被忽略
- 不打开任何开通引导
- gpt-4 卡片仍不显示

### 测试 7.5: normal_user 不能通过 API 探测受控组

**步骤**: DevTools Console：
```js
fetch('/api/v1/groups').then(r => r.json()).then(d => console.log(d.items.map(g => g.name)))
```

**预期结果**:
- HTTP 200 或特殊结构
- 返回的列表只含 `deepseek-chat` + `claude-opus`（即用户能用到的）
- **不返回** `gpt-4`、`gemini-pro`、`qwen-max`
- 不返回任何 `open_mode=controlled` 但不可用的组

---

## 第二部分：dept_admin 视角（7.6 - 7.11）

### 测试 7.6: dept_admin 看到 public + 本工作室已授权组

**步骤**:
1. dept_admin_a 登录 → `/model-square`
2. 观察所有卡片

**预期结果**:
- ✅ 看到 `deepseek-chat`（public）
- ✅ 看到 `claude-opus`（本工作室 A 已授权，本部门已开通）
- ✅ 看到 `gpt-4`（本工作室 A 已授权，本部门**未开通**）
- ❌ **不显示** `gemini-pro`（本工作室未授权）
- ❌ **不显示** `qwen-max`（public 但 disabled）

### 测试 7.7: dept_admin 卡片区分"已开通"vs"待开通"

**步骤**: 观察 claude-opus 和 gpt-4 卡片的外观差异

**预期结果**:
- `claude-opus`（已开通）：
  - 🟢 pill「本部门已开通」或类似
  - footer **无**操作按钮
- `gpt-4`（待开通）：
  - 🟠 pill「本部门待开通」或类似
  - footer 有 **「开通本部门 →」** 按钮（primary 主要样式）

### 测试 7.8: dept_admin 在模型广场直接一键开通

**步骤**:
1. 点击 `gpt-4` 卡片上的「开通本部门 →」
2. popconfirm：「确定为本部门开通 gpt-4？」
3. 点击「确定开通」

**预期结果**:
- ✅ success message：「已为本部门开通」
- ✅ 卡片即时变化：
  - pill 从🟠变🟢
  - 「开通本部门 →」按钮消失
- ✅ **无需跳转**到 `/admin/entitlements` 完成操作
- 这是「操作即入口」设计的关键验证

### 测试 7.9: dept_admin 开通后卡片稳定

**步骤**: 开通完成后，F5 刷新 `/model-square`

**预期结果**:
- gpt-4 卡片保持「已开通」状态
- 无「刚开通」标记残留

### 测试 7.10: dept_admin 撤销开通

**前置**: gpt-4 已开通（来自 7.8）

**步骤**: 尝试撤销

**预期结果**（设计待确认）:
- **设计 A**：在模型广场卡片上有「撤销开通」按钮（次要按钮），点击后 popconfirm 撤销
- **设计 B**：模型广场只展示开通操作，撤销需跳到 `/admin/entitlements` 双列布局中操作

> 测试执行时按实际实现选择验证。两种设计都可接受，但必须**不能只有开通没有撤销入口**。

### 测试 7.11: dept_admin 不能开通本工作室未授权的组

**步骤**: 尝试在 API 中操作：
```js
fetch('/api/v1/groups/{gemini-pro-id}/entitlements', {
  method: 'POST',
  body: JSON.stringify({ scope_type: 'dept', scope_id: 'D1' })
})
```

**预期结果**: HTTP 403，后端拒绝（前端 UI 根本不给入口）

---

## 第三部分：studio_admin 视角（7.12 - 7.15）

### 测试 7.12: studio_admin 只看到本工作室已授权组

**步骤**: studio_admin_b 登录 → `/model-square`

**预期结果**:
- ✅ 看到 `deepseek-chat`（public）
- ✅ 看到 `claude-opus`（本工作室 A 已授权）
- ✅ 看到 `gpt-4`（本工作室 A 已授权，假设 admin 已授权）
- ❌ **不显示** `gemini-pro`（本工作室未授权）

### 测试 7.13: studio_admin 卡片显示开通统计

**步骤**: 观察 claude-opus 卡片

**预期结果**:
- 🟠 pill「受控」+ **开通统计**：「🏬 已开通 1/3 部门」
- footer 有 **「管理部门开通 →」** 按钮（跳转或打开抽屉）
- 与 dept_admin 视图明显不同（dept_admin 只有单部门维度，studio_admin 有跨部门维度）

### 测试 7.14: studio_admin "管理部门开通"入口

**步骤**: 点击 `claude-opus` 卡片上的「管理部门开通 →」

**预期结果**（设计待确认）:
- **设计 A**: 跳转 `/admin/entitlements` → 自动打开 claude-opus 的抽屉
- **设计 B**: 在模型广场页面原地弹出抽屉（drawer overlay 方式）

无论哪种：抽屉内容与测试 04 第二部分一致（studio_admin 视角抽屉只有部门开通 Tab，工作室筛选锁定为本工作室）

### 测试 7.15: studio_admin 不能在广场开通未授权组

**步骤**: 尝试查找 `gemini-pro`（本工作室未授权）

**预期结果**:
- 卡片根本不存在，无"开通"动作路径
- API 调用同样返回 403

---

## 第四部分：admin 视角（7.16 - 7.19）

### 测试 7.16: admin 看到全部模型组（含 disabled）

**步骤**: admin 登录 → `/model-square`

**预期结果**:
- ✅ `deepseek-chat` / `claude-opus` / `gpt-4` / `gemini-pro` 都显示
- ✅ `qwen-max`（禁用 public 组）：灰色 / 带「已禁用」标记 / 出现在「未启用」区
- admin 视图信息最完整

### 测试 7.17: admin 卡片含丰富统计

**步骤**: 观察 `claude-opus` 卡片

**预期结果**:
- 🟠 pill「受控」
- 统计：「🏢 2 工作室授权」「🏬 5 部门开通」
- footer 有两个按钮：「切换开放模式」+「管理授权」
- 一眼看出全局授权情况

### 测试 7.18: admin 切换开放模式

**步骤**: 点击 `claude-opus` 卡片上「切换开放模式」→ popconfirm → 确定

**预期结果**:
- ✅ 卡片状态变化
- 与测试 02 一致

### 测试 7.19: admin "管理授权"→进入抽屉

**步骤**: 点击「管理授权」按钮

**预期结果**:
- 打开测试 03/04 描述的抽屉
- 抽屉内有「工作室授权」+「部门开通」两个 Tab

---

## 第五部分：跨角色视图对比（7.20）

### 测试 7.20: 同一张卡片在四角色视图下的外观对比（视觉回归）

**步骤**: 对 `claude-opus` 卡片，用四账号依次登录 `/model-square`，截图对比

| 角色 | pill | 状态描述 | footer 按钮 |
|------|------|---------|------------|
| admin | 🟠 pill「受控」| 🏢 2 工作室 / 🏬 5 部门 | 「切换为公开」「管理授权」 |
| studio_admin | 🟠 pill「受控」| 🏬 已开通 1/3 部门（本工作室范围） | 「管理部门开通 →」 |
| dept_admin | 🟢 pill「已开通」或 🟠 pill「待开通」 | 对应文案 | 「撤销」或「开通本部门」 |
| normal_user | 🟢 pill「本部门可用」或无 pill | 「可用」 | **无按钮** |

**验收**: 四角截图保存至 `specs/003-group-authorization/test/snapshots/07-model-square-{role}.png`，作为后续视觉回归基线

---

## 关键 DOM 选择器提示

- 模型广场页面容器: `.model-square-page`
- 卡片组件: `.model-square-card`
- pill: `.status-pill`，type 属性区分 public/controlled
- 卡片 footer: `.model-square-card__actions`
- 「开通本部门」按钮: `button[data-action="dept-open"]`
- 「管理部门开通」按钮: `button[data-action="manage-dept-open"]`
- 「管理授权」按钮: `button[data-action="manage-entitlements"]`
- 「切换开放模式」按钮: `button[data-action="toggle-open-mode"]`
- 统计行: `.model-square-card__stats`

## 验收标准

| # | 检查项 | 通过条件 |
|---|--------|---------|
| 1 | normal_user 无信息泄露 | 看不到任何不可用组，无任何"未开通"提示 |
| 2 | dept_admin 可见范围正确 | 只看 public + 本工作室授权组 |
| 3 | dept_admin 开通一键完成 | 不需跳转到其他页面 |
| 4 | studio_admin 可见范围正确 | 只看 public + 本工作室授权组 |
| 5 | studio_admin 有管理入口 | 「管理部门开通」按钮跳转或打开抽屉 |
| 6 | admin 看到全貌 | 含 disabled 组，含完整统计 |
| 7 | 卡片结构统一 | 四角色卡片 HTML 结构相同，仅 pill/文案/按钮不同 |
| 8 | API 返回按角色过滤 | 后端列表只含角色可见组 |
| 9 | 视觉回归基线 | 保存四角色截图作为后续对比基准 |
