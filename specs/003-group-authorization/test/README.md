# 模型组授权 — 前端 E2E 测试任务（新交互方案）

## 1. 设计原则（测试基准）

本次测试基于以下**新交互架构**编写，每条用例都是对交互设计的活体验证：

1. **信息最小暴露**：每个角色只看到"对自己有意义的信息"
   - user 看不到不可用的受控组（如同不存在）
   - dept_admin 看不到本工作室外的授权信息
   - studio_admin 看不到未授权给本工作室的受控组
   - admin 拥有一切
2. **入口不藏**：授权管理独立菜单项，manager 角色直接看到
3. **操作即入口**：有权限的角色在模型广场就能直接操作
4. **角色视图差异**：不同角色在同一个页面看到**完全不同的信息架构**

## 2. 测试环境

| 项 | 值 |
|----|-----|
| Base URL | `http://localhost:8090`（nginx 统一入口） |
| 登录入口 | `/login` → 点击「其他登录方式」展开 dev 登录表单 |
| 登录 API | `POST /auth/dev/login` body: `{ username, password }` |

## 3. 测试账号

| 账号 | 密码 | 角色 | 角色范围 |
|------|------|------|---------|
| `admin` | `admin123` | 平台管理员 | 全局，可见所有工作室/部门 |
| `studio_admin_b` | `dev123456` | 工作室负责人 | 仅所属工作室（含其下部门） |
| `dept_admin_a` | `dev123456` | 部门负责人 | 仅所属部门 |
| `normal_user` | `dev123456` | 普通员工 | 无管理权限 |

## 4. 新路由结构

| # | 路由 | 路由守卫 | 菜单可见角色 | 角色视图差异 |
|---|------|---------|-------------|-------------|
| 1 | `/model-square` | 无 | 所有角色 | **数据按角色过滤**（核心测试点） |
| 2 | `/admin/entitlements` | `requiresManager` | manager (admin/studio_admin/dept_admin) | **每个角色看到完全不同的页面** |
| 3 | `/admin/groups` | `requiresAdmin` | admin only | model group CRUD（不含授权管理） |

> **关键变化**：授权管理从「模型组行尾操作列」迁出为**独立路由** `/admin/entitlements`，所有 manager 均可访问，但看到的 UI/数据按角色完全不同。

## 5. 三个页面的角色视图摘要

### `/model-square`（模型广场）

| 角色 | 可见模型组 | 看不到 |
|------|----------|--------|
| normal_user | public 组 + 本部门**已开通**的 controlled 组 | **完全不知道**还有其他受控组（不存在感） |
| dept_admin | public 组 + 本**工作室已获授权**的 controlled 组（不论本部门是否开通） | 本工作室未获授权的组 |
| studio_admin | public 组 + 本工作室已获授权的 controlled 组 + 各部门开通数 | 本工作室未获授权的组 |
| admin | 全部 | 无限制 |

### `/admin/entitlements`（模型组授权）

| 角色 | 可见内容 | 可操作 |
|------|---------|--------|
| admin | 全部受控组卡片（含工作室/部门统计数）+ 全部公开组卡片 | 切换开放模式、授权工作室、开通部门、撤销 |
| studio_admin | 本工作室已获授权的受控组 + 各部门开通状态 | 为自己工作室下的部门开通/撤销 |
| dept_admin | 本工作室已获授权的受控组 + 本部门开通状态 | 为本部门开通/撤销 |

### `/admin/groups`（模型组 CRUD）

| 角色 | 访问 | 说明 |
|------|------|------|
| admin | ✅ | 创建/编辑/删除模型组（不含授权操作） |
| 其他 | ❌ 403 | - |

## 6. 测试任务索引

| # | 文件 | 内容 | 核心角色 |
|---|------|------|---------|
| 01 | `01-login-and-access.md` | 登录 + 侧边栏菜单 + 路由守卫 | 全角色 |
| 02 | `02-open-mode-display.md` | admin 在 `/admin/entitlements` 切换开放模式 | admin |
| 03 | `03-studio-authorization.md` | admin 授权/撤销工作室 | admin |
| 04 | `04-dept-enablement.md` | 三角色的部门开通 / 撤销（含数据隔离） | admin / studio / dept |
| 05 | `05-permission-isolation.md` | 权限隔离 + 跨角色信息不泄露 | 全角色 |
| 06 | `06-cascade-revoke.md` | 级联撤销 + 跨角色视图联动 | admin / studio |
| 07 | `07-model-square-visibility.md` | 模型广场按角色过滤 | 全角色 |

## 7. 通用操作模板

### 登录步骤

```
1. 浏览器导航到 /login
2. 等待 "LLM Gateway" 标题可见
3. 点击 "其他登录方式"（文字链接）
4. 等待表单出现（placeholder 为 "用户名" 的 input 可见）
5. 填充用户名、密码
6. 点击 "账号密码登录" 按钮
7. 等待 URL 不是 /login，页面渲染完成
```

### Element Plus 操作约定

- **popconfirm**：点击触发元素 → 气泡出现 → 点击确认按钮（文字如「确定」「确定撤销」）
- **el-drawer**：滑出动画结束后，标题元素 `.el-drawer__header` 可见
- **el-select** filterable：点击展开下拉 → 可在输入框输入关键词 → 点击选项
- **el-message**：顶部中央出现 `.el-message--success/.el-message--error`，几秒自动消失
- **el-card** 状态色块：通过色块标签的 type (success/warning/danger/info) 判断

## 8. 测试数据预置建议

为了让每个测试用例独立可执行，需有稳定的种子数据：

- **public 公共组** 至少 1 个：如 `deepseek-chat`（`open_mode='public'`, `enabled=true`）
- **controlled 受控组** 至少 2 个：如 `claude-opus`、`gpt-4`
- **工作室授权**：`claude-opus` 已授权给 studio_admin_b 所属工作室
- **部门开通**：dept_admin_a 所属部门已在 `claude-opus` 下被开通
- gpt-4 未授权给任何工作室（用于测试"未授权"视图）

**注意**：每个测试用例文件开头会声明它的数据前置要求，执行时若缺失需先跑数据准备脚本或由测试执行者按前置用例建立数据。
