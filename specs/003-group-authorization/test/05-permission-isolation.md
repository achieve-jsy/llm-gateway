# 测试任务 05: 权限隔离与信息最小暴露

## 设计目标

验证新交互架构的**核心安全原则**：每个角色只能看到「对自己有意义的信息」，无法越权探查、无法通过 URL 篡改或 API 探测绕过 UI 限制。

| 原则 | 验证点 |
|------|--------|
| 路由守卫 | 非 admin 不能进 `/admin/groups` |
| UI 隔离 | 不同角色在 `/admin/entitlements` 看到不同卡片集合 |
| API 隔离 | 后端对越权请求返回 403/404，不泄露任何信息 |
| 错误消息不泄露 | 错误提示不包含敏感对象信息（如"工作室 X 不存在"） |
| URL 篡改防御 | 直接构造 URL 参数不能越权访问 |
| Dev 工具探测防御 | 调用未授权 API 不能得到详细错误 |

## 前置条件

- 系统中至少有：
  - studio_admin_b 所属工作室 A
  - 另一个工作室 B（studio_admin_b 不归属）
  - 受控组 `claude-opus` 已授权给工作室 A
  - 受控组 `gpt-4` 已授权给工作室 B
  - 受控组 `gemini-pro` 未授权给任何工作室

---

## 第一部分：路由守卫（5.1 - 5.3）

### 测试 5.1: studio_admin 访问 admin-only 路由被拒

**步骤**: studio_admin_b 登录后，依次直接导航：
1. `/admin/groups`
2. `/admin/org/roles`
3. `/admin/providers`
4. `/admin/models`
5. `/admin/settings`

**预期结果**:
- 每个都跳转到 `/403`
- 页面显示**统一**的 403 提示（如「无权限访问」）
- **不显示**具体「你缺少 X 权限」等技术细节

### 测试 5.2: dept_admin 访问 admin-only 路由被拒

**步骤**: dept_admin_a 登录后重复 5.1 的 URL 列表

**预期结果**: 同 5.1

### 测试 5.3: normal_user 访问所有管理路由被拒

**步骤**: normal_user 登录后依次直接导航：
1. `/admin/entitlements`
2. `/admin/groups`
3. `/admin/org`
4. `/admin/org/roles`
5. `/admin/providers` + `/admin/models` + `/admin/settings`

**预期结果**: 每个都跳到 `/403`

---

## 第二部分：工作室级信息隔离（5.4 - 5.7）

### 测试 5.4: studio_admin 看不到其他工作室的受控组卡片

**步骤**:
1. studio_admin_b 登录 → `/admin/entitlements`
2. 观察页面上所有受控组卡片

**预期结果**:
- ✅ `claude-opus` 卡片**可见**（本工作室已授权）
- ❌ `gpt-4` 卡片**不显示**（本工作室未授权，对 studio_admin 如同不存在）
- ❌ `gemini-pro` 卡片**不显示**（未授权给任何人）
- 页面不应有任何提示说「还有其他受控组你无权查看」

### 测试 5.5: studio_admin 抽屉中看不到其他工作室的数据

**步骤**:
1. studio_admin_b 打开 `claude-opus` 卡片抽屉
2. 在「部门开通」Tab 内，尝试修改工作室筛选器为其他工作室

**预期结果**:
- ✅ 工作室筛选器**disabled / readonly**，锁定为本工作室
- 表格只显示本工作室下的部门
- 不存在切换到其他工作室的 UI 路径

### 测试 5.6: studio_admin 不能通过 API 查到其他工作室的授权

**步骤**:
1. studio_admin_b 登录后，DevTools Console 调用：
   ```js
   fetch('/api/v1/groups/{gpt-4-id}/entitlements?scope_type=studio')
     .then(r => r.json()).then(console.log)
   ```
2. 观察响应

**预期结果**:
- HTTP 403 或 404
- 响应体**不包含**任何工作室列表或错误详情
- 只含统一错误结构 `{ code, message }`，message 为「权限不足」或「无权限」
- **不泄露** gpt-4 是否真的授权给了哪些工作室

### 测试 5.7: studio_admin 不能撤销其他工作室的授权

**步骤**: 尝试调用：
```js
fetch('/api/v1/groups/{claude-opus-id}/entitlements/{工作室B的eid}', {
  method: 'DELETE'
})
```

**预期结果**:
- HTTP 403
- 响应不包含工作室 B 的敏感信息
- 撤销不生效（admin 视角下该记录仍存在）

---

## 第三部分：部门级信息隔离（5.8 - 5.12）

### 测试 5.8: dept_admin 看不到本工作室未获授权的组

**步骤**:
1. dept_admin_a 登录 → `/admin/entitlements`
2. 查看「已可用」+「可开通」两列

**预期结果**:
- ✅ 「已可用」列：public 组 + 本部门已开通的 controlled 组
- ✅ 「可开通」列：本工作室已获授权但本部门未开通的 controlled 组
- ❌ `gpt-4`（授权给其他工作室）**不显示**
- ❌ `gemini-pro`（本工作室未授权）**不显示**
- 两列中**没有任何提示**说「还有 X 个组你无权查看」

### 测试 5.9: dept_admin 看不到本工作室下其他部门的开通记录

**步骤**:
1. dept_admin_a 进入某个受控组的抽屉
2. 切换到「部门开通」Tab

**预期结果**:
- 部门列表**只显示本部门的记录**
- 同一工作室下其他部门的开通记录**不显示**
- 表格中看不到任何其他部门的名称、开通人、开通时间

### 测试 5.10: dept_admin 不能开通其他部门

**步骤**: 尝试通过 API：
```js
fetch(`/api/v1/groups/{claude-opus-id}/entitlements`, {
  method: 'POST',
  body: JSON.stringify({ scope_type: 'dept', scope_id: 'other_dept_id' })
})
```

**预期结果**:
- HTTP 403
- 不生效（admin 视角下其他部门的开通列表无变化）

### 测试 5.11: dept_admin 不能撤销其他部门的开通

**步骤**: 尝试删除其他部门的 entitlement：
```js
fetch(`/api/v1/groups/{claude-opus-id}/entitlements/{其他部门的eid}`, {
  method: 'DELETE'
})
```

**预期结果**: HTTP 403，不生效

### 测试 5.12: dept_admin 看不到本工作室未获授权的组（反向）

**步骤**: dept_admin 直接调用未授权组的授权列表 API：
```js
fetch('/api/v1/groups/{gpt-4-id}/entitlements')
```

**预期结果**: HTTP 403/404，不泄露任何信息

---

## 第四部分：员工级信息隔离（5.13 - 5.15）

### 测试 5.13: normal_user 看不到未开通的受控组

**步骤**:
1. normal_user 登录 → `/model-square`
2. 观察所有模型组卡片

**预期结果**:
- ✅ 显示 public 组（如 `deepseek-chat`）
- ✅ 显示本部门**已开通**的 controlled 组
- ❌ 本部门**未开通**的 controlled 组**不显示**
- ❌ 任何部门的 controlled 组如果未开通，则**完全不存在**
- 页面**没有任何**「你还没有开通 / 需申请开通」的提示

### 测试 5.14: normal_user 不能通过 API 列出受控组

**步骤**: 调用：
```js
fetch('/api/v1/groups?open_mode=controlled')
```

**预期结果**:
- HTTP 200 但返回的列表**只含**该用户能访问的 controlled 组
- 或 HTTP 403（取决于 API 设计）
- 不返回完整的 controlled 列表

### 测试 5.15: normal_user 不能调用任何授权 API

**步骤**: 依次调用：
```js
fetch('/api/v1/groups/{gpt-4-id}/entitlements') // GET
fetch('/api/v1/groups/{claude-opus-id}/entitlements', {method:'POST', body: '...'}) // POST
fetch('/api/v1/groups/{claude-opus-id}/entitlements/{eid}', {method:'DELETE'}) // DELETE
```

**预期结果**: 全部 401/403，无敏感信息泄露

---

## 第五部分：错误消息不泄露（5.16 - 5.17）

### 测试 5.16: 越权访问时错误消息统一

**步骤**: studio_admin_b 直接请求其他工作室的 entitlement：
```js
fetch('/api/v1/groups/{gpt-4-id}/entitlements/{某eid}')
```

**预期结果**:
- HTTP 403 或 404
- **message 统一为「权限不足」或「资源不存在」**
- **不应出现**：「该 entitlement 不属于你的工作室」「gpt-4 授权给了工作室 B」

### 测试 5.17: 访问不存在的 ID 时与越权消息一致

**步骤**: studio_admin 请求一个不存在的 group_id 或 entitlement_id：
```js
fetch('/api/v1/groups/999999/entitlements')
fetch('/api/v1/groups/{claude-opus-id}/entitlements/00000000-0000-0000-0000-000000000000')
```

**预期结果**:
- 返回的错误码 / 错误消息**与越权时一致**
- 这防止了通过枚举 ID 判断哪些资源存在（Timing/Oracle 攻击）

---

## 第六部分：URL 篡改防御（5.18 - 5.19）

### 测试 5.18: studio_admin 不能通过 URL query 参数访问其他工作室

**步骤**: 尝试：
```
/admin/entitlements?studio=other_studio_id
/admin/entitlements?group=gpt-4-id
```

**预期结果**:
- URL 参数被忽略
- 页面仍然只显示本工作室的数据
- 或者 URL 参数导致路由守卫直接 403

### 测试 5.19: dept_admin 不能通过 URL hash 进入管理抽屉

**步骤**: 尝试直接构造 URL + hash 打开抽屉：
```
/admin/entitlements#manage=gpt-4
```

**预期结果**:
- hash 参数被忽略
- 页面不尝试打开 gpt-4 的抽屉
- 或触发权限检查返回 403

---

## 关键 DOM 选择器提示

- 403 页面: `.forbidden-page`、`.empty-state`，内容包含「无权限」
- 卡片视图容器: `.entitlement-page__cards`
- 工作室筛选器: `.el-select` placeholder 「按工作室筛选」
- 部门信息卡（dept_admin）: `.dept-info-card`，包含部门名和工作室名

## 验收标准

| # | 检查项 | 通过条件 |
|---|--------|---------|
| 1 | 路由守卫生效 | admin-only 路由非 admin 都跳到 403 |
| 2 | 视图隔离 | studio_admin 看不到其他工作室的卡片和数据 |
| 3 | 部门隔离 | dept_admin 只能看到本部门数据 |
| 4 | 员工隔离 | normal_user 完全不知道有受控组（除非已开通） |
| 5 | API 不泄露 | 后端对越权返回统一错误，不含敏感详情 |
| 6 | 错误消息一致 | 越权和不存在的资源返回同类错误 |
| 7 | URL 篡改无效 | query / hash 参数不能绕过权限 |
| 8 | Dev 工具探测失败 | 直接调用 API 不能越权获取数据 |
