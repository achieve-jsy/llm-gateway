# LLM Gateway 前端开发指南

> 本文档供前端设计 Hermes 使用，包含访问地址、账号、项目位置等关键信息

---

## 1. 网关在线地址

**生产环境**: https://aimeter.xk-devops.com
- IP: 192.0.2.10
- 浏览器会提示"不安全"（自签证书），点击"继续访问"即可

**测试环境**: http://192.0.2.10:9080
- 本地反向代理: http://192.0.2.10:9081 → 192.0.2.10:9080
- WireGuard 访问: http://10.8.0.8:9081

---

## 2. Admin 账号密码

**Ops Console 管理员**:
- 用户名: `admin`
- 密码: `admin123`

**注意**: 这是 Ops Console（管理后台）的账号，不是普通用户的 OAuth 登录。

---

## 3. 登录方式

### 3.1 普通用户（员工）
- **OAuth 登录**: 通过飞书（Feishu/Lark）OAuth 授权
- 访问 https://aimeter.xk-devops.com 后点击"飞书登录"
- 需要飞书管理员提前授权用户

### 3.2 管理员（Ops Console）
- **账号密码登录**: admin / admin123
- 访问路径: 首页 → 管理 → 各管理页面
- 或直接访问: https://aimeter.xk-devops.com/admin/

### 3.3 API Key 认证
- 用于 API 调用（非 Web UI）
- 格式: `sk-` 前缀的密钥
- 在"我的 Key"页面创建和管理

---

## 4. 截图/访问方式

### 4.1 浏览器直接访问
```bash
# 生产环境
https://aimeter.xk-devops.com

# 测试环境
http://192.0.2.10:9080

# 本地代理（推荐，避免证书问题）
http://192.0.2.10:9081
```

### 4.2 命令行截图（Puppeteer/Playwright）
如需自动化截图，可使用：
```bash
# Puppeteer 示例
node screenshot.js https://aimeter.xk-devops.com/my/usage

# Playwright 示例
npx playwright screenshot --full-page https://aimeter.xk-devops.com/my/usage
```

### 4.3 主要页面路径
| 页面 | 路径 | 说明 |
|------|------|------|
| 我的用量 | `/my/usage` | 个人用量统计、花费趋势 |
| 我的 Key | `/my/keys` | API Key 管理 |
| 模型广场 | `/my/models` | 可用模型列表 |
| 我的预算 | `/my/budget` | 预算管理和告警 |
| 模型组健康 | `/my/health` | 模型组可用性监控 |
| 模型性能 | `/my/performance` | 延迟、TTFT 等性能指标 |
| **管理后台** | | |
| 组织架构 | `/admin/org` | 用户、部门、工作室管理 |
| 用量看板 | `/admin/dashboard` | 全局用量统计 |
| 预算管理 | `/admin/budget` | 预算配置和审批 |
| Service Key | `/admin/service-keys` | 服务账号 Key 管理 |
| 模型组授权 | `/admin/groups` | 模型组权限配置 |
| 审计日志 | `/admin/audit` | 操作审计记录 |

---

## 5. 本地项目地址

**前端代码位置**:
```
/home/hgf/llm_gateway/web/
```

**目录结构**:
```
web/
├── src/
│   ├── views/              # 页面组件
│   │   ├── MyUsagePage.vue         # 我的用量
│   │   ├── MyKeysPage.vue          # 我的 Key
│   │   ├── admin/                  # 管理后台页面
│   │   │   ├── OrgStructurePage.vue
│   │   │   ├── UsageDashboardPage.vue
│   │   │   ├── BudgetManagementPage.vue
│   │   │   └── ...
│   ├── components/         # 通用组件
│   ├── api/                # API 调用
│   ├── stores/             # Pinia 状态管理
│   ├── router/             # 路由配置
│   └── utils/              # 工具函数
├── package.json
└── vite.config.ts
```

**本地开发**:
```bash
cd /home/hgf/llm_gateway/web
npm install
npm run dev  # 启动开发服务器，默认 http://localhost:5173
```

**构建**:
```bash
npm run build  # 构建生产版本到 dist/
```

---

## 6. 源码地址

**GitLab 仓库**:
- 地址: https://git.example.com/honggangfeng/llm_gateway
- 项目 ID: 10254
- PAT (Personal Access Token): `glpat-REDACTED`

**Git 克隆**:
```bash
git clone https://git.example.com/honggangfeng/llm_gateway.git
cd llm_gateway
```

**分支策略**:
- `main`: 生产分支（稳定版本）
- `test`: 测试分支（部署到测试环境）
- `feature/*`: 功能分支（从 main 拉取，合并到 test）

**当前版本**: v0.11.11（2026-06-26 部署）

**CI/CD**:
- 推送到 `test` 分支自动触发测试环境部署
- 推送到 `main` 分支并打 tag 触发生产环境部署
- Pipeline 查看: https://git.example.com/honggangfeng/llm_gateway/-/pipelines

---

## 7. 技术栈

- **前端框架**: Vue 3 + TypeScript
- **UI 组件库**: Element Plus
- **状态管理**: Pinia
- **构建工具**: Vite
- **图表库**: ECharts
- **CSS 预处理器**: SCSS

---

## 8. 关键文件

| 文件 | 说明 |
|------|------|
| `web/src/views/MyUsagePage.vue` | 我的用量页面（最近修改：筛选联动 + KEY 分布视图 + 时间精确到小时） |
| `web/src/views/admin/ops/OpsDashboardPage.vue` | Ops Console 主页面 |
| `web/src/api/ops.ts` | Ops API 调用封装 |
| `web/src/router/index.ts` | 路由配置 |
| `web/src/stores/settings.ts` | 全局设置状态 |

---

## 9. 开发注意事项

1. **不要直接修改 main 分支**，所有开发从 main 拉 feature 分支
2. **测试环境验证**：合并到 test 分支后自动部署到 http://192.0.2.10:9080
3. **生产部署**：需要手动合并 test → main，打 tag 触发 CI/CD
4. **时间格式**：所有时间戳使用北京时间（UTC+8），前端显示时注意转换
5. **API 调用**：使用 `web/src/api/` 下的封装函数，不要直接写 fetch/axios

---

## 10. 联系方式

如有问题，联系：
- 洪岗峰（hgf）
- 飞书：ou_1aa9e08a184d0ea5d90df499de3e112e

---

**文档版本**: v1.0 (2026-06-26)
**最后更新**: hermes-coder
