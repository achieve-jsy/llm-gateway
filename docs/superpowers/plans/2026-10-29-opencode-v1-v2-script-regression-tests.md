# OpenCode V1/V2 脚本下发回归测试 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把已交付的「OpenCode V1/V2 双路径安装脚本」固化成 `tests/` 下可重复运行的回归测试，覆盖模板渲染、mac/PowerShell 两条真实执行路径、能力映射与失败兜底。

**Architecture:** 全部测试放 `tests/controlplane/handler/`、`package handler_test`，只走公开 API（`handler.NewOpencodeConfigHandler` + `GetScript`）。Redis 用 `miniredis`，模型/能力/插件/回调端点用 `httptest.Server` 假网关，脚本在 `t.TempDir()` 造的假 HOME 里真跑一遍，然后断言落盘的 `opencode.json`。缺子进程（bash/python3/curl/pwsh）一律 `t.Skip`。

**Tech Stack:** Go 1.26、Gin、`miniredis`（go.mod 已引入 v2.38.0）、`net/http/httptest`、`os/exec`、`encoding/json`。

**Spec:** [docs/opencode-models-discovery.md](../../opencode-models-discovery.md) §6 与 §6.1（V1/V2 双路径的权威描述）+ [tests/README.md](../../../tests/README.md)（测试分层与命名规范）。生产代码已实现并有手工验证记录，本计划不修改生产代码。

## Global Constraints

- 测试文件一律放 `tests/controlplane/handler/`，包名 `handler_test`（外部测试包，只能访问公开 API）。
- 不连真实 Redis / Postgres：Redis 用 `miniredis`，`SettingsServiceProvider` 用 fake 实现。
- 不出网：`/v1/models`、`/v1/model/info`、`/api/v1/opencode-config/plugin`、`/api/v1/opencode-config/callback` 全部由 `httptest.Server` 提供，端口随机。
- 不碰用户真实配置：脚本一律在 `t.TempDir()` 里执行，`HOME`（mac）/ `USERPROFILE`（Windows）指向临时目录。
- 子进程缺失必须跳过而非失败：`bash`、`python3`、`curl` 缺失 → `t.Skip("缺少 bash/python3/curl")`；PowerShell 探测 `pwsh` 与 `powershell` 都找不到 → `t.Skip`。
- **这是特征化（characterization）测试**：生产代码已交付。测试红了说明已交付行为与设计不符，先报告差异、不要为了让测试变绿而改断言或改生产代码。
- 子测试名与表驱动 `description` 用中文写清「测什么、期望什么」（`tests/README.md` 要求）。
- 完成门槛：`go test ./tests/... -count=1` 全绿；`gofmt -l tests/` 输出为空。
- 提交信息用 Conventional Commits（`test(...)`）。

## Review Focus

1. **同机同时装 V1 与 V2**：`opencode` 在 PATH 里只解析到一份，脚本必须按解析到的那份写配置，且**只写一种形状**，不能两条都写。
2. **老配置里 `provider`/`providers`、`plugin`/`plugins` 并存**（用户升级过或手工改过）：写入时清掉同名旧条目与本插件声明，其它 provider / 插件原样保留。
3. **`/v1/models` 正常但 `/v1/model/info` 挂掉**：仍要写出模型清单（只是缺 capabilities/limit），不能整体失败。
4. **网关不可达或 Key 看不到任何模型**：脚本必须非零退出、不写半截配置、回调 `failed`（否则前端轮询 2 分钟超时）。
5. **路径含空格或中文**（`HOME`、Windows `USERPROFILE`、用户名带空格）：脚本仍能写对文件，不被引号/编码打断。

---

### Task 1: 测试脚手架 + 模板渲染断言

**Files:**
- Create: `tests/controlplane/handler/opencode_config_harness_test.go`
- Create: `tests/controlplane/handler/opencode_config_script_test.go`

**Interfaces:**
- Consumes: `handler.NewOpencodeConfigHandler(rdb *redis.Client, settingsSvc handler.SettingsServiceProvider) *OpencodeConfigHandler`、方法 `GetScript(c *gin.Context)`。
- Produces（后续任务直接用）:
  - `const testScriptToken = "tok-test"`
  - `type tokenFixture struct { ProviderID, ProviderName, BaseURL, APIKey, Status string }`
  - `type fakeSettings struct { Name string }`，方法 `GetSupplierName(ctx context.Context) string`
  - `func newScriptEngine(t *testing.T, f tokenFixture) (*gin.Engine, *miniredis.Miniredis)` — 起 miniredis、把 token 记录写进 `opencode_cfg_token:` + `testScriptToken`、挂 `GET /api/v1/opencode-config/script`
  - `func fetchScript(t *testing.T, r *gin.Engine, osType, ver string) (statusCode int, body string)`

- [ ] **Step 1: 写脚手架**

`tests/controlplane/handler/opencode_config_harness_test.go`：上面 Interfaces 里的五个符号。token 记录按 handler 实际读的 JSON 结构落 Redis（`{"key_id","provider_id","provider_name","base_url","api_key","status"}`），`BASE_URL` 用调用方传入的假网关地址。`fetchScript` 用 `net/url.Values` 拼 `token`/`os`，`ver` 非空才带上。

- [ ] **Step 2: 写渲染断言测试**

`tests/controlplane/handler/opencode_config_script_test.go`：

```go
func TestGetScript_RendersBothVersionBranches(t *testing.T) {
	// 测什么：两个模板都同时带 V1 与 V2 分支骨架，且注入了 token/基址/密钥
	// 期望：mac 有 MODE="v1" 与 providers；windows 有 $MODE = "v1" 与 providers
}

func TestGetScript_ForceVersion(t *testing.T) {
	// 表驱动：("", "")  ("v1","v1")  ("v2","v2")  ("V2","v2")  ("3","")  ("../x","")
	// 期望：mac 输出 FORCE_VERSION="<want>"，windows 输出 $FORCE_VERSION    = "<want>"
}

func TestGetScript_UnknownToken(t *testing.T) { /* 期望 401 + "token" */ }
```

- [ ] **Step 3: 运行**

Run: `go test ./tests/controlplane/handler -run 'TestGetScript_' -count=1 -v`
Expected: 全部 PASS。红了先按 Global Constraints 报告行为差异。

- [ ] **Step 4: 提交**

```bash
gofmt -l tests/controlplane/handler/ | tee /dev/stderr | wc -l   # 期望 0
git add tests/controlplane/handler/opencode_config_harness_test.go tests/controlplane/handler/opencode_config_script_test.go
git commit -m "test(opencode): 覆盖配置脚本的 V1/V2 模板渲染与版本覆盖参数"
```

---

### Task 2: V2 端到端 —— 能力映射与旧配置清理

**Files:**
- Modify: `tests/controlplane/handler/opencode_config_harness_test.go`（追加假网关与执行辅助）
- Create: `tests/controlplane/handler/opencode_config_script_v2_test.go`

**Interfaces:**
- Consumes: Task 1 的 `newScriptEngine` / `fetchScript` / `tokenFixture`。
- Produces（Task 3/4/5 复用）:
  - `type fakeGateway struct { Server *httptest.Server; Models []string; Info map[string]map[string]any; InfoStatus int; Callbacks []map[string]any }`
  - `func newFakeGateway(t *testing.T) *fakeGateway`（`t.Cleanup` 里关闭；`Models` 默认 `middle-level`/`vision-pro`/`embed-small`/`plain-chat`）
  - `func writeFakeOpencode(t *testing.T, dir, version string) string` — 写 `<dir>/bin/opencode`（可执行 sh 脚本，`echo <version>`），返回 bin 目录
  - `func runMacScript(t *testing.T, scriptText, home string, pathDirs ...string) (stdout string, exitCode int)`
  - `func readOpencodeConfig(t *testing.T, home string) map[string]any`

- [ ] **Step 1: 写假网关与执行辅助**

`fakeGateway` 的 handler 按下表响应；`runMacScript` 把 `bash`/`python3`/`curl` 所在目录 + `pathDirs` 组成 PATH，覆盖 `HOME`，`bash <scriptfile>` 执行，返回合并后的 stdout 与退出码。

| 路径 | 响应 |
|---|---|
| `GET /v1/models` | `{"data":[{"id":...}]}` |
| `GET /v1/model/info` | `{"data":[{"model_name":...,"model_info":{...}}]}`；`InfoStatus != 0` 时用该状态码 |
| `GET /api/v1/opencode-config/plugin` | `// fake plugin\n` |
| `POST /api/v1/opencode-config/callback` | 记录 body 到 `Callbacks`，返回 `{"code":0}` |

- [ ] **Step 2: 写 V2 端到端测试**

```go
func TestMacScript_V2WritesNativeShape(t *testing.T) {
	// 测什么：假的 opencode 2.0.3 + 预置一份"旧 V1 形状 + 两套 plugin 声明 + 另一个 provider"
	// 期望：
	//  - providers["biangfeng-gateway"].package == "aisdk:@ai-sdk/openai-compatible"
	//  - settings.baseURL / settings.apiKey 等于脚本入参
	//  - 模型 3 个（embed-small 被跳过）
	//  - middle-level: capabilities.tools==true, input==["text","image"],
	//    reasoning.effort==["low","medium","high"], limit.context==1000000, limit.output==65536
	//  - vision-pro: input==["text","image"]（源数据只有 image 时补 text）
	//  - plain-chat: input==["text"]，无 tools、无 limit
	//  - 顶层 provider 键消失；providers["other"] 原样保留
	//  - plugin 数组只剩 "keep-me"；plugins 数组只剩 {"package":"keep-me-2"}
	//  - plugins/opencode-models-discovery.js 被删除
	//  - 退出码 0，回调收到 status=="success"
}
```

- [ ] **Step 3: 追加热路径用例（Review Focus 第 5 条）**

```go
func TestMacScript_HomePathWithSpacesAndUnicode(t *testing.T) {
	// 测什么：HOME 指向 filepath.Join(t.TempDir(), "用户 目录")（含中文和空格）
	// 期望：同样写出 3 个模型、退出码 0，文件落在 <HOME>/.config/opencode/opencode.json
}
```

- [ ] **Step 4: 运行**

Run: `go test ./tests/controlplane/handler -run TestMacScript_V2 -count=1 -v`
Expected: PASS（缺 bash/python3/curl 时为 SKIP，先在本机确认不是跳过）。

- [ ] **Step 5: 提交**

```bash
git add tests/controlplane/handler/opencode_config_harness_test.go tests/controlplane/handler/opencode_config_script_v2_test.go
git commit -m "test(opencode): 校验 V2 脚本的原生配置形状、旧配置清理与含空格路径"
```

---

### Task 3: V2 降级与失败路径

**Files:**
- Create: `tests/controlplane/handler/opencode_config_script_v2_fail_test.go`

**Interfaces:**
- Consumes: Task 2 的 `newFakeGateway` / `fakeGateway.InfoStatus` / `runMacScript` / `readOpencodeConfig` / `writeFakeOpencode`。

- [ ] **Step 1: 写三个子测试**

```go
func TestMacScript_V2ModelInfoFailure(t *testing.T) {
	// InfoStatus = 500：仍应写出 3 个模型，但每个 entry 只有 name，
	// 不能出现 capabilities / limit；退出码 0，回调 success。
}

func TestMacScript_V2GatewayUnreachable(t *testing.T) {
	// 关掉假网关后再执行：退出码非 0；预置的 opencode.json 内容与执行前逐字节相同；
	// 回调收到 status=="failed"（回调地址也指向已关闭端口时，至少断言配置文件未变）。
}

func TestMacScript_V2EmptyModelList(t *testing.T) {
	// Models 为空：退出码非 0、opencode.json 不存在、回调 status=="failed"。
}
```

- [ ] **Step 2: 运行**

Run: `go test ./tests/controlplane/handler -run 'TestMacScript_V2(ModelInfoFailure|GatewayUnreachable|EmptyModelList)' -count=1 -v`
Expected: PASS。

- [ ] **Step 3: 提交**

```bash
git add tests/controlplane/handler/opencode_config_script_v2_fail_test.go
git commit -m "test(opencode): 覆盖 V2 脚本的降级与失败路径"
```

---

### Task 4: V1 端到端 —— 插件下发与形状不串味

**Files:**
- Create: `tests/controlplane/handler/opencode_config_script_v1_test.go`

**Interfaces:**
- Consumes: Task 2 的 `newFakeGateway` / `runMacScript` / `writeFakeOpencode` / `readOpencodeConfig`；Task 1 的 `newScriptEngine` / `fetchScript`。

- [ ] **Step 1: 写 V1 端到端测试**

```go
func TestMacScript_V1KeepsDynamicDiscovery(t *testing.T) {
	// 假 opencode 1.19.5：写出的 provider.<id>.npm=="@ai-sdk/openai-compatible"，
	// options.modelsDiscovery.enabled==true，且没有顶层 providers 键；
	// 插件文件下载到 .config/opencode/plugins/opencode-models-discovery.js；
	// plugin 数组里的本插件声明被移除、其它插件保留；退出码 0，回调 success。
}

func TestMacScript_ForceV1OnV2Binary(t *testing.T) {
	// 假 opencode 2.0.3 + 脚本 URL 带 ver=v1：输出仍是 V1 形状（逃生口生效），
	// 且不出现顶层 providers 键。
}
```

- [ ] **Step 2: 追加版本探测兜底用例**

```go
func TestMacScript_DetectionFallback(t *testing.T) {
	// 表驱动：
	//  - PATH 里没有 opencode、也没有 cli.json  → V1 形状（provider 单数），退出码 0
	//  - PATH 里没有 opencode，但 <HOME>/.config/opencode/cli.json 存在 → V2 形状（providers 复数）
	// 两种都必须有且只有一种形状。
}
```

- [ ] **Step 3: 运行**

Run: `go test ./tests/controlplane/handler -run 'TestMacScript_(V1|ForceV1|DetectionFallback)' -count=1 -v`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add tests/controlplane/handler/opencode_config_script_v1_test.go
git commit -m "test(opencode): 校验 V1 脚本保持动态发现、不被 V2 形状污染及版本探测兜底"
```

---

### Task 5: Windows 脚本端到端（有 pwsh 才跑）

**Files:**
- Modify: `tests/controlplane/handler/opencode_config_harness_test.go`（追加 `runWinScript`）
- Create: `tests/controlplane/handler/opencode_config_script_windows_test.go`

**Interfaces:**
- Consumes: Task 2 的 `newFakeGateway` / `writeFakeOpencode` / `readOpencodeConfig`。
- Produces: `func runWinScript(t *testing.T, scriptText, userProfile string, pathDirs ...string) (stdout string, exitCode int)`；内部先 `exec.LookPath("pwsh")`，找不到再 `exec.LookPath("powershell")`，都没有就 `t.Skip("缺少 PowerShell（CI 镜像通常没有）")`。执行方式 `<shell> -NoProfile -ExecutionPolicy Bypass -File <scriptfile>`，环境覆盖 `USERPROFILE`。

- [ ] **Step 1: 写 Windows 双模式测试**

```go
func TestWinScript_V2WritesNativeShape(t *testing.T) {
	// 假 opencode 2.0.3：断言 providers.<id>.package / settings / 3 个模型 /
	// middle-level.capabilities.tools==true / 插件文件被删 / 退出码 0。
}

func TestWinScript_V1KeepsDynamicDiscovery(t *testing.T) {
	// 假 opencode 1.19.5：断言 provider.<id>.options.modelsDiscovery.enabled==true、
	// 插件文件存在、无顶层 providers 键、退出码 0。
}

func TestWinScript_UserProfileWithSpaces(t *testing.T) {
	// USERPROFILE 指向 <tmp>/用户 目录（含空格与中文）：V2 路径仍能写对文件，退出码 0。
}
```

- [ ] **Step 2: 运行**

Run: `go test ./tests/controlplane/handler -run 'TestWinScript_' -count=1 -v`
Expected: 本机有 pwsh 时 PASS；没有时 SKIP（`-v` 输出里确认 skip 原因）。

- [ ] **Step 3: 提交**

```bash
git add tests/controlplane/handler/opencode_config_harness_test.go tests/controlplane/handler/opencode_config_script_windows_test.go
git commit -m "test(opencode): 覆盖 Windows 脚本的 V1/V2 双模式"
```

---

## 收尾检查（不属于任何单个任务）

```bash
gofmt -l tests/ | tee /dev/stderr | wc -l    # 期望 0
go test ./tests/... -count=1                 # 期望 ok
go vet ./tests/controlplane/handler/
```
