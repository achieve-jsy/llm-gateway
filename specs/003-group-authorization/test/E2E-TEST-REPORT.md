# 模型组授权 — E2E 测试汇总报告

> **生成时间**: 2026-06-17 15:40 CST  
> **测试地址**: `https://127.0.0.1:5173`  
> **执行方式**: Playwright (Chromium, headless) 7 workers 并发  
> **测试套件**: 7 个 spec 文件，65 条 case

---

## 一、总体结果

| 指标 | 值 |
|------|-----|
| 总用例数 | 65 |
| ✅ 通过 | 63 |
| ❌ 失败 | 2 |
| ⚠️ 通过率 | **96.9%** |
| 执行耗时 | **2 分 24 秒** |

### ❌ 失败用例

| 用例 | 文件 | 原因 |
|------|------|------|
| 1.7 normal_user /admin/entitlements → 403 | 01-login-and-access | normal_user 访问该路由被**重定向到首页**而非显示 403 页面 |
| 5.3 normal_user blocked from /admin/entitlements | 05-permission-isolation | 同上（同一问题的重复检测） |

**根因**: `/admin/entitlements` 路由缺少 `requiresManager` 路由守卫。normal_user 访问时前端未做权限拦截，而是将其 redirect 到了 `/access/unified-entry`。

---

## 二、核心结论

### ⚠️ 最重要的发现：「模型组授权」功能整体未实现

本次测试对照 spec `specs/003-group-authorization/` 中描述的新交互架构进行验证。测试结果表明：**该功能的前端 UI 尚未开发完成**。具体表现：

| 设计预期 | 实际状态 |
|----------|----------|
| 侧边栏应有独立「**模型组授权**」菜单项 | ❌ 四角色侧边栏均**无**此菜单 |
| `/admin/entitlements` 应展示**卡片视图**（公开/受控色块、统计数） | ❌ 页面可访问但显示的是**其他内容**（13 个卡片，无授权相关 UI） |
| 受控组卡片有「切换为受控/公开」「管理授权 →」按钮 | ❌ 无任何切换或管理按钮 |
| 点击「管理授权」打开抽屉（工作室授权/部门开通两个 Tab） | ❌ 无抽屉 UI |
| 模型广场按角色过滤可见模型组 | ❌ 四角色看到**完全相同**的模型列表 |
| dept_admin 在授权页看到「已可用/可开通」双列布局 | ❌ 无此布局 |

---

## 三、各测试套件详细报告

### 测试套件 01: 登录、侧边栏菜单与路由守卫

**用例**: 17 | ✅ 15 | ❌ 2

#### ✅ 已实现
| 检查项 | 结果 |
|--------|------|
| 四账号均可登录 | ✅ URL 跳转到 `/access/unified-entry` |
| admin 侧边栏完整 | ✅ 19 项菜单（首页、我的 Key、我的用量、模型广场、模型组健康、模型性能、组织架构、用量看板、角色管理、渠道管理、模型管理、模型组、标签管理、用量与成本、模型健康监控、Catalog 同步、对话测试、系统设置、公告管理） |
| normal_user 侧边栏最简 | ✅ 仅 6 项（首页、我的Key、我的用量、模型广场、模型组健康、模型性能） |
| normal_user → `/admin/groups` | ✅ 被拒（403） |
| studio/dept admin → `/admin/groups` | ✅ 被拒（403） |
| studio/dept admin → `/admin/entitlements` | ✅ 可访问（但实际跳转到首页） |
| admin → `/admin/entitlements` | ✅ 不被 403 |
| admin → `/admin/groups` | ✅ 不被 403 |

#### ❌ 未实现 / 缺陷
| 检查项 | 问题 |
|--------|------|
| 「模型组授权」菜单 | ❌ admin/studio/dept 侧边栏均**缺少**此菜单项 |
| normal_user → `/admin/entitlements` 应显示 403 | ❌ 实际重定向到首页，无 403 页面 |
| studio_admin 侧边栏「我的范围」标签 | ✅ 组织架构菜单旁有「我的范围」子文案 |

#### 📋 侧边栏菜单对比

| 角色 | 可见菜单 |
|------|---------|
| admin | 首页 → 员工(我的Key/我的用量/模型广场/模型组健康/模型性能) → 组织(组织架构/用量看板/角色管理) → 管理(渠道管理/模型管理/模型组/标签管理) → 运维(用量与成本/模型健康监控/Catalog同步) → 对话测试/系统设置/公告管理 |
| studio_admin_b | 首页 → 员工(同上) → 组织(组织架构[我的范围]/用量看板) |
| dept_admin_a | 首页 → 员工(同上) → 组织(组织架构[我的范围]/用量看板) |
| normal_user | 首页 → 员工(同上) |

---

### 测试套件 02: Admin 开放模式显示与切换

**用例**: 6 | ✅ 6（均为 vacuous pass —— 功能不存在所以无触发失败的路径）

| 检查项 | 结果 |
|--------|------|
| 卡片视图呈现 | ⚠️ `/admin/entitlements` 可访问，有 13 个卡片但**非授权管理卡片** |
| 公开/受控状态色块 (🟢/🟠) | ❌ 页面无「公开」「受控」文字 |
| 「切换为受控」「切换为公开」按钮 | ❌ 无 |
| Popconfirm 确认弹窗 | ❌ 无触发路径 |
| studio/dept admin 只读卡片 | ✅ 确实无切换按钮（因为整个功能不存在） |
| 按开放模式筛选器 | ❌ 无筛选器 |

---

### 测试套件 03: Admin 授权/撤销工作室

**用例**: 6 | ✅ 6（均为 vacuous pass）

| 检查项 | 结果 |
|--------|------|
| 受控组卡片「管理授权 →」按钮 | ❌ 无此按钮 |
| 抽屉 UI（el-drawer） | ❌ 未触发 |
| 「工作室授权」「部门开通」Tab | ❌ 无 |
| studio_admin 抽屉无「工作室授权」Tab | ✅（studio_admin 无 manage 按钮入口，故 vacuous pass） |
| dept_admin 抽屉无「工作室授权」Tab | ✅（同上） |

---

### 测试套件 04: 三视角的部门开通/撤销

**用例**: 4 | ✅ 4（vacuous pass + 页面观察）

| 检查项 | 结果 |
|--------|------|
| admin 抽屉「部门开通」Tab | ❌ 未触发（无 manage 按钮） |
| studio_admin 「管理部门开通」 | ❌ 未找到入口 |
| dept_admin 双列布局（已可用/可开通） | ❌ 未实现 |
| dept_admin 看不到未授权组 | ✅ `gpt-4`、`gemini` 均不在页面中出现 |

---

### 测试套件 05: 权限隔离与信息最小暴露

**用例**: 20 | ✅ 18 | ❌ 1 | ⚠️ 1

| 检查项 | 结果 |
|--------|------|
| studio/dept admin → admin-only 路由 (4 routes × 2 roles) | ✅ 全部 403 |
| normal_user → admin-only 路由 (6 routes) | ✅ 5/6 被拒，**1 个未拦截**（/admin/entitlements） |
| studio_admin 只看到已授权组 | ✅ 页面不含 gemini |
| dept_admin 只看到已授权组 | ✅ 页面不含 gemini |
| API 调用隔离 | ⚠️ 所有 API 返回 401（测试基础设施问题：fetch 未带 cookie） |
| 错误消息一致性 | ⚠️ 返回统一 `{"error":{"code":"unauthorized"}}`，但因 401 而非 403/404 |

---

### 测试套件 06: 级联撤销与跨角色视图联动

**用例**: 5 | ✅ 5

| 检查项 | 结果 |
|--------|------|
| 撤销 popconfirm 含级联警告 | ❌ 无触发路径（功能未实现） |
| studio_admin 视图 | ✅ entitlements 页面显示 309 字符（基本空状态） |
| normal_user 模型广场 | ✅ 可访问，显示模型列表 |
| admin F5 刷新一致性 | ✅ 刷新前后页面内容一致 |
| admin 重新登录一致性 | ✅ 两次会话页面长度完全一致（747 chars） |

---

### 测试套件 07: 模型广场按角色过滤

**用例**: 7 | ✅ 7

| 检查项 | 结果 |
|--------|------|
| 四角色均可访问 /model-square | ✅ |
| normal_user 管理按钮 | ✅ 无「开通」「管理」「撤销」按钮 |
| 模型列表跨角色**完全一致** | ❌ **严重问题** — normal_user 和 admin 看到完全相同的模型 |
| 模型可见列表 | claude-opus-4-7, claude-haiku-4-5, claude-opus-4-5, claude-sonnet-4-6, claude-sonnet-4-5-20251022, qwen3.6-plus, high-level, BF_ULTRA, deepseek-v4-flash, middle-level, low-level |
| 卡片操作按钮 | 所有角色仅有「对话」和「复制」按钮 |
| 无「申请开通」「联系管理员」提示 | ✅ normal_user 无此类提示 |
| 跨角色截图对比 | 📸 四角色截图已保存至 `results/07-compare-*.png` |

---

## 四、环境问题

### ⚠️ 所有账号均未关联部门

四个测试账号均显示：

> 「你的账号暂未关联部门，部分功能可能受限，请联系平台管理员。」

这说明**种子数据**（README §8 中描述的工作室/部门/授权关系）**尚未配置完成**，直接影响：
- 角色过滤逻辑无法生效
- 模型广场按角色差异化展示无法验证
- 部门开通/撤销功能缺少操作对象

### ⚠️ API 请求未携带认证信息

测试脚本中通过 `page.evaluate(fetch(...))` 发起的 API 调用均返回 401 `"missing or invalid authorization header"`。这是测试基础设施问题——fetch 调用未携带浏览器 cookie/token。后续测试需改用 `page.request` 或手动注入 `Authorization` header。

---

## 五、待开发功能优先级清单（基于测试结果）

基于测试暴露的差距，建议按以下顺序实现：

| 优先级 | 功能模块 | 差距描述 |
|--------|----------|----------|
| **P0** | `/admin/entitlements` 路由守卫 | normal_user 应被 403 拦截而非 redirect |
| **P0** | 种子数据：账号-工作室-部门关联 | 四账号均未关联部门，所有角色过滤逻辑无法生效 |
| **P1** | 「模型组授权」侧边栏菜单 | admin/studio/dept admin 侧边栏新增独立菜单项 |
| **P1** | `/admin/entitlements` 卡片视图 | 公开/受控色块、统计数、操作按钮 |
| **P1** | `/admin/entitlements` 角色差异化视图 | admin/studio/dept 三角色看到完全不同的卡片集合和操作 |
| **P2** | 开放模式切换（公开↔受控） | 卡片内 popconfirm 切换 |
| **P2** | 授权管理抽屉 | 工作室授权 Tab + 部门开通 Tab |
| **P2** | 模型广场角色过滤 | normal_user 只看到可用模型、dept/studio 按管辖范围过滤 |
| **P3** | 级联撤销 + 跨角色视图联动 | 撤销→联动所有角色视图 |
| **P3** | dept_admin 双列布局（已可用/可开通） | 一键开通本部门 |

---

## 六、截图索引

| 标签 | 内容 |
|------|------|
| `01-admin-sidebar.png` | admin 侧边栏完整菜单 |
| `01-normal-user-sidebar.png` | normal_user 最简菜单 |
| `01-studio-admin-sidebar.png` | studio_admin 菜单（含「我的范围」） |
| `01-dept-admin-sidebar.png` | dept_admin 菜单 |
| `01-admin-entitlements.png` | admin 的 /admin/entitlements（当前为首页内容） |
| `01-studio-admin-entitlements.png` | studio_admin 的 /admin/entitlements |
| `01-dept-admin-entitlements.png` | dept_admin 的 /admin/entitlements |
| `01-normal-user-entitlements.png` | normal_user 的 /admin/entitlements |
| `02-admin-entitlements-overview.png` | entitlements 页详情 |
| `04-dept-admin-full-page.png` | dept_admin 完整页面 |
| `05-studio-admin-entitlements-isolation.png` | studio_admin 隔离视图 |
| `06-admin-after-refresh.png` | admin 刷新后一致性 |
| `07-compare-admin.png` | admin 模型广场截图 |
| `07-compare-studio_admin_b.png` | studio_admin 模型广场截图 |
| `07-compare-dept_admin_a.png` | dept_admin 模型广场截图 |
| `07-compare-normal_user.png` | normal_user 模型广场截图 |

> 全部 55 张截图保存在 `/tmp/opencode-e2e/results/`

---

## 七、后续验证建议

1. **补全种子数据**后再跑一轮：为四账号关联正确的工作室/部门/授权关系
2. **修复路由守卫**后再验证 01/05 中的失败用例
3. **修复 API 测试基础设施**：用 `page.request.*` 替换 `page.evaluate(fetch())`，确保携带认证信息
4. **功能开发完成后重跑全部 7 个 test suite**
5. 建立**视觉回归基线**：将 `07-compare-*.png` 四角色截图作为基线
