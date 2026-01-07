# 测试任务 06: 级联撤销与跨角色视图联动

## 设计目标

验证新交互架构下的**级联语义**：
1. admin 撤销工作室授权 → 所有相关角色的视图**即时联动**
2. 级联不删除 dept 行 → 重新授权后自动恢复
3. dept_admin 开通/撤销 → normal_user 的模型广场视图联动
4. 全链路无延迟刷新：撤销后 F5 或重新登录仍能看到最新状态

核心原则：**级联撤销不是「删除」，而是「联级判定为失效」**，所以恢复时旧数据还在。

## 前置数据准备

| 数据 | 状态 |
|------|------|
| 受控组 `claude-opus` | `open_mode=controlled`, `enabled=true` |
| 工作室 A（studio_admin_b 所属） | 被 admin 授权 claude-opus |
| 部门 D1（dept_admin_a 所属，归属工作室 A） | dept_admin_a 为本部门开通了 claude-opus |
| 工作室 B（另一个工作室） | 未授权 claude-opus |

执行本测试前需先完成测试 03（授权工作室）和 04（部门开通 4.19），建立上述数据。

---

## 第一部分：admin 撤销 → studio_admin 视图联动（6.1 - 6.4）

### 测试 6.1: admin 撤销工作室授权，观察本卡片的变化

**步骤**:
1. admin 登录 → `/admin/entitlements`
2. 定位 `claude-opus` 卡片
3. 点击「管理授权 →」进入抽屉
4. 「工作室授权」Tab 中，点击 studio A 行的「撤销」按钮
5. popconfirm 出现，确认文案「撤销后下属部门将同时失效，确定撤销？」
6. 点击「确定撤销」

**预期结果**:
- ✅ 抽屉内：studio A 行从表格消失，「工作室授权」Tab 显示「暂无授权工作室」
- ✅ success message：「已撤销工作室授权」
- ❓ **关键观察**：抽屉关闭后，`claude-opus` 卡片如何变化？
  - **理想设计**：卡片顶部 🟠 pill 保留 + 统计数从「1 工作室 / 1 部门」变为「0 工作室 / 0 部门」+ 卡片新增「未授权给任何工作室」提示
  - **或**：卡片整体从 admin 视图的「受控组」区移到「未授权组」区（如果有此区）

### 测试 6.2: studio_admin 视角卡片消失

**前置**: admin 已完成 6.1

**步骤**:
1. studio_admin_b 登录 → `/admin/entitlements`
2. 观察页面卡片

**预期结果**:
- ❌ `claude-opus` 卡片**完全消失**（本工作室不再被授权）
- 页面没有任何提示说「你之前有权限现在没了」
- 其他已获授权的受控组（如有）正常显示

### 测试 6.3: studio_admin 尝试 URL 访问被撤销的组

**前置**: 同 6.2

**步骤**:
1. studio_admin_b 直接导航到带 group id 的 URL（如果设计支持 URL hash 直达抽屉）：
   ```
   /admin/entitlements#group=claude-opus-id
   ```
2. 或构造 fetch：
   ```js
   fetch('/api/v1/groups/{claude-opus-id}/entitlements?scope_type=dept')
   ```

**预期结果**:
- URL 参数被忽略，页面不打开抽屉
- API 调用返回 403 或空列表
- 不泄露 studio A 之前被授权的历史

### 测试 6.4: admin 重新授权 studio A，验证 dept 行仍保留

**步骤**:
1. admin 登录 → `claude-opus` 卡片抽屉
2. 「工作室授权」Tab → 「+ 授权工作室」
3. 选择 studio A，点击「确定授权」
4. 切换到「部门开通」Tab
5. 选择 studio A 筛选

**预期结果**:
- ✅ studio A 重新出现在「工作室授权」Tab 表格中
- ✅ 「部门开通」Tab 中 dept_admin_a 所属部门的行**仍然保留**（因为级联撤销没有删除 dept 行）
- 卡片统计数恢复：「1 工作室授权」/「1 部门开通」

### 测试 6.5: studio_admin 视角卡片恢复

**前置**: admin 已完成 6.4

**步骤**: studio_admin_b 刷新 `/admin/entitlements`

**预期结果**:
- ✅ `claude-opus` 卡片**重新出现**
- ✅ 卡片显示「已开通 1 部门」（因为 dept 行保留自动恢复生效）
- ✅ 卡片上「管理部门开通 →」按钮可用
- 抽屉内「部门开通」Tab 显示 D1 部门的开通记录
- 无任何「你曾经被撤销过」的提示

---

## 第二部分：dept_admin 开通/撤销 → normal_user 视图联动（6.6 - 6.9）

### 测试 6.6: 撤销前 normal_user 能看到 claude-opus

**前置**: 测试 6.4/6.5 状态（studio A 授权恢复，dept_admin 部门 D1 开通已存）

**步骤**:
1. normal_user 登录 → `/model-square`
2. 查看页面

**预期结果**:
- ✅ `deepseek-chat` 卡片可见（public）
- ✅ `claude-opus` 卡片可见（本部门已开通 controlled）
- 卡片顶部有 🟢 状态 tag「已开通」或类似标识

### 测试 6.7: dept_admin 撤销本部门开通，观察 normal_user 视图

**步骤**:
1. dept_admin_a 登录 → `/admin/entitlements`
2. 在「可开通」或「已可用」列中找到 `claude-opus`
3. 撤销本部门开通
4. dept_admin_a 登出
5. normal_user 登录 → `/model-square`

**预期结果**:
- ✅ `claude-opus` 卡片**完全消失**（normal_user 看不到未开通的 controlled 组，如同不存在）
- 页面没有「最近移除」「已撤销」等提示
- 仅显示 public 组 + 其他已开通组

### 测试 6.8: dept_admin 重新开通，normal_user 视图恢复

**步骤**:
1. dept_admin_a 重新为本部门开通 `claude-opus`
2. normal_user 登录 → `/model-square`

**预期结果**:
- ✅ `claude-opus` 卡片**重新出现**
- 卡片状态正常（无"新开通"或"刚恢复"提示）

### 测试 6.9: dept_admin 开通一个全新组，normal_user 视图新增

**步骤**:
1. admin 授权一个新 controlled 组（如 `anthropic-claude-3-5-sonnet`）给 studio A
2. dept_admin_a 进入 `/admin/entitlements` → 「可开通」列 → 开通本部门
3. normal_user 登录 `/model-square`

**预期结果**:
- ✅ 新组卡片出现在 normal_user 的 `/model-square`
- 与之前开通的 controlled 组外观一致

---

## 第三部分：全链路级联验证（6.10 - 6.12）

### 测试 6.10: admin 撤销 → dept_admin / normal_user 同时失去

**前置**: 所有授权都已就绪

**步骤**:
1. admin 撤销 studio A 的 claude-opus 授权
2. dept_admin_a 登录 `/admin/entitlements`：应**看不到** claude-opus 卡片
3. normal_user 登录 `/model-square`：应**看不到** claude-opus 卡片（即使自己之前开通过）

**预期结果**:
- admin 一次撤销，三个角色视图**同步级联**
- 无任何角色能通过刷新恢复对该组的可见性

### 测试 6.11: admin 重新授权 + dept_admin 开通记录自动恢复

**步骤**:
1. admin 重新给 studio A 授权 claude-opus
2. dept_admin_a 登录 `/admin/entitlements`
3. 在「可开通」列还是「已可用」列观察？

**预期结果**（关键设计点）:
- dept_admin 视图上 claude-opus 卡片**出现在「已可用」列**（因为 dept 行在数据库中仍然 enabled，联级判定通过）
- 不需要 dept_admin 重新操作开通

> 这与 6.7 不同：6.7 是 dept_admin **主动撤销** → dept 行被删除/禁用 → 需要重新开通
> 6.10-6.11 是 admin **级联撤销** → dept 行未被修改，仅因 studio 行失效而联级判定不通过 → studio A 恢复后 dept 行自动生效

### 测试 6.12: normal_user 在 6.11 后也自动恢复

**前置**: 6.11 完成

**步骤**: normal_user 登录 `/model-square`

**预期结果**:
- ✅ `claude-opus` 卡片重新出现
- 用户端完全无感知
- 卡片状态与之前一致

---

## 第四部分：持久性与刷新一致性（6.13 - 6.15）

### 测试 6.13: 刷新页面状态一致

**步骤**: admin 撤销 studio A 后，按 F5 刷新浏览器

**预期结果**:
- admin 视图上 `claude-opus` 卡片仍是撤销后状态（统计数为 0）
- 不出现脏数据残留

### 测试 6.14: 重新登录后状态一致

**步骤**: admin 登出再登入 → `/admin/entitlements`

**预期结果**:
- 视图与登出前完全一致
- studio A 不在「工作室授权」Tab 中
- 卡片统计为 0

### 测试 6.15: 另一 admin 也能看到最新状态

**步骤**: 用另一个 admin 账号（如有）登录 → `/admin/entitlements`

**预期结果**:
- 两个 admin 看到的视图完全一致
- 无 session 缓存导致的旧状态

---

## 关键 DOM 选择器提示

- 卡片「未授权给任何工作室」提示: `.entitlement-card__unauthorized-hint`
- 模型广场卡片: `.model-square-card`
- 部门信息卡（dept_admin 视图）: `.dept-info-card`

## 验收标准

| # | 检查项 | 通过条件 |
|---|--------|---------|
| 1 | 撤销文案强调级联 | popconfirm 文案包含「下属部门将同时失效」 |
| 2 | studio_admin 视图即时级联 | admin 撤销后 studio_admin 卡片消失 |
| 3 | dept_admin 视图即时级联 | admin 撤销后 dept_admin 卡片消失 |
| 4 | normal_user 视图即时级联 | 任何撤销链最终都让普通用户看到消失 |
| 5 | 重新授权自动恢复 | dept 行保留 → studio 行恢复 → 自动重新生效 |
| 6 | dept_admin 主动撤销 vs 级联撤销有差异 | 6.7 需重新开通；6.11 自动恢复 |
| 7 | F5 刷新状态一致 | 无脏数据残留 |
| 8 | 重新登录状态一致 | 无缓存问题 |
| 9 | 无敏感提示泄露 | 撤销/恢复时不出现「你曾经有权限」「已被管理员撤销」等文案 |
