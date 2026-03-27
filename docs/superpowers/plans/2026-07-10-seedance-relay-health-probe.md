# Seedance Relay Health Probe Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `seedance-relay`（景汐）提供唯一的连通+鉴权探针，供定时健康检查与管理面手动检测共用，避免误打 `/models` 导致渠道被自动禁用。

**Architecture:** 在 `internal/controlplane/service` 新增 `ProbeSeedanceRelayConnectivity` 作为唯一 HTTP 判定；`HealthCheckService`、`TestHandler`、模型检测路径按 `definition_id` 分发后调用该函数。语义：成功 = 网络可达 + 非 401/403 + 非 5xx（典型 404）；不创建视频任务。

**Tech Stack:** Go、`net/http/httptest`、现有 control-plane service/handler 模式。

**Spec:** `docs/superpowers/specs/2026-07-10-seedance-relay-health-probe-design.md`

---

## File map

| File | Responsibility |
|------|----------------|
| `internal/controlplane/service/seedance_relay_probe.go` | 共享探针实现 + `SeedanceRelayProbeResult` |
| `internal/controlplane/service/seedance_relay_probe_test.go` | 探针单测 |
| `internal/controlplane/service/health_check_service.go` | `isNonOpenAICompatible` + `checkNonOpenAIProvider` 分发 |
| `internal/controlplane/service/model_probe.go` | video model_type skip |
| `internal/controlplane/handler/test_handler.go` | 手动测试走共享探针 |
| `internal/controlplane/handler/model_check_handler.go` | 景汐不走 Bedrock |
| `internal/controlplane/service/health_check_service.go` (`doModelForwardCheck`) | 按 definition 分发 |
| `internal/controlplane/service/sync_service.go` | seedance 标不支持 `/models` 同步 |
| `internal/controlplane/service/model_check_public.go` | 可选：导出景汐模型检测公共入口 |

---

### Task 1: 共享探针 + 单测

**Files:**
- Create: `internal/controlplane/service/seedance_relay_probe.go`
- Create: `internal/controlplane/service/seedance_relay_probe_test.go`

- [ ] **Step 1: 写失败单测**

```go
package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeSeedanceRelayConnectivity_404IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/seedance/api/v3/contents/generations/tasks/__gateway_health_probe__") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("auth=%s", got)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
	}))
	defer srv.Close()

	res := ProbeSeedanceRelayConnectivity(context.Background(), srv.URL, "sk-test")
	if !res.Success {
		t.Fatalf("want success, got %#v", res)
	}
	if !strings.Contains(res.Message, "connectivity+auth") {
		t.Fatalf("message should declare scope: %s", res.Message)
	}
}

func TestProbeSeedanceRelayConnectivity_401IsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	res := ProbeSeedanceRelayConnectivity(context.Background(), srv.URL, "bad")
	if res.Success {
		t.Fatal("401 must fail")
	}
}

func TestProbeSeedanceRelayConnectivity_503IsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	res := ProbeSeedanceRelayConnectivity(context.Background(), srv.URL, "sk")
	if res.Success {
		t.Fatal("5xx must fail")
	}
}
```

- [ ] **Step 2: 跑测确认失败**

```bash
go test ./internal/controlplane/service/ -run ProbeSeedanceRelay -count=1
```

Expected: FAIL（undefined ProbeSeedanceRelayConnectivity）

- [ ] **Step 3: 实现探针**

`seedance_relay_probe.go`：

- 常量假 ID `__gateway_health_probe__`
- 注释写明：测连通+鉴权；不测视频创建
- `GET {endpoint}/seedance/api/v3/contents/generations/tasks/__gateway_health_probe__`
- Bearer apiKey；Timeout 10s；Proxy: nil（与现有 checkConnectivity 一致）
- 失败：transport err / 401 / 403 / 5xx
- 成功：其余状态码；Message 含 `ok: connectivity+auth only (GET tasks/__gateway_health_probe__; not video create)`

- [ ] **Step 4: 跑测通过**

```bash
go test ./internal/controlplane/service/ -run ProbeSeedanceRelay -count=1
```

Expected: PASS

---

### Task 2: HealthCheckService 分发

**Files:**
- Modify: `internal/controlplane/service/health_check_service.go`

- [ ] **Step 1: 扩展 `isNonOpenAICompatible`**

```go
func isNonOpenAICompatible(definitionID string) bool {
	switch definitionID {
	case "bedrock", "seedance-relay":
		return true
	}
	return false
}
```

- [ ] **Step 2: `checkNonOpenAIProvider` 增加 case**

```go
case "seedance-relay":
	return s.checkSeedanceRelayProvider(ctx, provider, credJSON, checkMode, maxFailures, cr)
```

实现 `checkSeedanceRelayProvider`：

- extractAPIKey → `ProbeSeedanceRelayConnectivity`
- 失败：recordLog + handleProviderFailure
- 成功：Success=true，ResetFailures；message 用探针 Message
- `model_forward` / `both`：连通段用探针；转发段 skip，文案  
  `skipped: seedance-relay probe is connectivity+auth only (no video create)`

- [ ] **Step 3: 修 `doModelForwardCheck`**

`isNonOpenAICompatible` 后不要一律 `doBedrockModelCheck`：

```go
switch provider.DefinitionID {
case "bedrock":
	return s.doBedrockModelCheck(provider, credJSON, m)
case "seedance-relay":
	apiKey := extractAPIKey(credJSON)
	res := ProbeSeedanceRelayConnectivity(context.Background(), provider.Endpoint, apiKey)
	return res.Success, res.ResponseTime, res.Message
default:
	return true, 0, "skipped: unsupported provider type for auto-check"
}
```

- [ ] **Step 4: 编译**

```bash
go build ./internal/controlplane/...
```

---

### Task 3: TestHandler 对齐

**Files:**
- Modify: `internal/controlplane/handler/test_handler.go`

- [ ] **Step 1: 用 service 判定或扩展 map**

优先：

```go
if cpservice.IsNonOpenAICompatible(provider.DefinitionID) {
```

并删掉本地仅含 bedrock 的重复 map（或保留 map 但与 service 同步且加 seedance-relay）。

- [ ] **Step 2: `testNonOpenAIProvider`**

```go
case "seedance-relay":
	return h.testSeedanceRelay(provider, credJSON)
```

`testSeedanceRelay`：Decrypt 已完成，extract key，调 `ProbeSeedanceRelayConnectivity`，映射到现有 `testResult`（ConnectivityOK / Error / Message 含 scope）。`TestChat` 对景汐忽略或返回 skipped 说明。

- [ ] **Step 3: 编译**

```bash
go build ./internal/controlplane/...
```

---

### Task 4: 模型检测不走 Bedrock

**Files:**
- Modify: `internal/controlplane/handler/model_check_handler.go`
- Modify: `internal/controlplane/service/model_probe.go`
- Modify: `internal/controlplane/service/sync_service.go`（可选同 task）

- [ ] **Step 1: model_check_handler**

将：

```go
if cpservice.IsNonOpenAICompatible(...) {
	success, ..., := cpservice.DoBedrockModelCheckPublic(...)
}
```

改为 switch：

- bedrock → DoBedrockModelCheckPublic
- seedance-relay → ProbeSeedanceRelayConnectivity（或新建 DoSeedanceRelayModelCheckPublic 薄包装）
- default → skip success + message

- [ ] **Step 2: ProbeModelForward video skip**

```go
case lower == "video" || lower == "video_generation":
	return false, true, 0, "video model: skipped (use seedance-relay connectivity+auth probe)"
```

- [ ] **Step 3: sync_service `isUnsupportedAdapter`**

加入 `seedance-relay`, `seedance-volcengine`。

- [ ] **Step 4: 跑相关测试**

```bash
go test ./internal/controlplane/service/ -count=1
go test ./internal/controlplane/handler/ -count=1
go build ./...
```

---

### Task 5: 验收自检

- [ ] 确认无新的 `/models` 路径用于 seedance-relay 健康/测试
- [ ] 文档 spec 状态改为「已确认 / 实现中」或「已实现」
- [ ] （人工）曾被误禁用的景汐渠道需手动 re-enable——在 PR/提交说明中写一句

**不自动 commit**（除非用户明确要求）。

---

## 完成定义

- 共享探针单测绿
- `go build ./...` 通过
- 景汐定时检查与手动测试同源
- 模型检测不调用 Bedrock Converse
