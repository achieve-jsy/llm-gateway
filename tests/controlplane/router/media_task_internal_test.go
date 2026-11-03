package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/router"
)

// C3：冻结契约（内部 API）的行为验证 —— 走**真实** `router.Setup` 装配。
//
// 覆盖点：
//  1. 防护栏 S1：所有 /internal/v1/video/tasks* 端点**必须**走既有 InternalAuth（X-Internal-Token），
//     无 token / 错 token 一律 403（fail-closed），带正确 token 才可用；
//  2. 冻结契约逐端点逐字段：POST（新增字段 + 201/200 幂等语义）、PATCH /:id（CAS + 状态机 + 响应形状）、
//     PATCH /:id/status（向后兼容）、GET /:id（新增）、GET /:id/provider-type（语义不变）；
//  3. private_data 永不出现在任何 HTTP 响应里（json:"-"）；
//  4. expired 对调用方归一为 failed。
//
// 【效力边界】内存 sqlite + 不可达 Redis；证明的是「装配后的路由/中间件/契约行为」，
// 不是生产 PG 下的行为（后者由迁移演练 + GORM↔SQL 漂移对拍覆盖）。

const mediaInternalInternalToken = "media-internal-contract-token-9f31ab"

const mediaInternalTaskDDL = `
CREATE TABLE media_tasks (
	task_id TEXT PRIMARY KEY,
	api_key_id TEXT NOT NULL,
	api_key_name TEXT,
	api_key_type TEXT,
	user_id TEXT,
	user_name TEXT,
	model TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	provider_type TEXT NOT NULL,
	status TEXT NOT NULL,
	video_url TEXT,
	request_body TEXT,
	response_body TEXT,
	error_message TEXT,
	created_at DATETIME,
	updated_at DATETIME,
	kind TEXT,
	action TEXT,
	vendor_task_id TEXT,
	progress TEXT,
	usage TEXT,
	cost_micro_usd INTEGER,
	billed_at DATETIME,
	started_at DATETIME,
	finished_at DATETIME,
	poll_failures INTEGER,
	idempotency_key TEXT,
	private_data TEXT,
	dept_id TEXT,
	studio_id TEXT
)`

const mediaInternalEventDDL = `
CREATE TABLE media_task_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	event_data TEXT,
	created_at DATETIME
)`

const mediaInternalIdemIndexDDL = `CREATE UNIQUE INDEX uniq_media_tasks_idem ON media_tasks (api_key_id, idempotency_key)`

func mediaInternalDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	for _, stmt := range []string{mediaInternalTaskDDL, mediaInternalIdemIndexDDL, mediaInternalEventDDL} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}

func mediaInternalEngine(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("JWT_SECRET", "media-internal-contract-jwt-secret-3c7d")
	t.Setenv("INTERNAL_TOKEN", mediaInternalInternalToken)
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	return router.Setup(context.Background(), db, rdb)
}

// doInternal 发一次内部请求（默认带 X-Internal-Token）。
func doInternal(t *testing.T, engine *gin.Engine, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	out := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func taskOf(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	task, ok := resp["task"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 task 对象: %v", resp)
	}
	return task
}

// ───────────────────────────── S1：InternalAuth ─────────────────────────────

func TestInternalMediaTaskRoutes_RequireInternalToken(t *testing.T) {
	engine := mediaInternalEngine(t, mediaInternalDB(t))

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/internal/v1/video/tasks"},
		{http.MethodPatch, "/internal/v1/video/tasks/task-x"},
		{http.MethodPatch, "/internal/v1/video/tasks/task-x/status"},
		{http.MethodGet, "/internal/v1/video/tasks/task-x"},
		{http.MethodGet, "/internal/v1/video/tasks/task-x/provider-type"},
	}
	for _, rt := range routes {
		for _, token := range []string{"", "wrong-token"} {
			code, _ := doInternal(t, engine, rt.method, rt.path, map[string]any{}, token)
			if code != http.StatusForbidden {
				t.Errorf("%s %s token=%q → %d，必须被 InternalAuth 拒绝为 403", rt.method, rt.path, token, code)
			}
		}
	}
}

// ───────────────────────────── POST 创建 + 幂等 ─────────────────────────────

func TestInternalCreateMediaTask_NewFieldsAndIdempotency(t *testing.T) {
	db := mediaInternalDB(t)
	engine := mediaInternalEngine(t, db)
	const path = "/internal/v1/video/tasks"

	body := map[string]any{
		"task_id":       "task-1",
		"api_key_id":    "key-a",
		"api_key_name":  "主 Key",
		"api_key_type":  "personal",
		"user_id":       "user-a",
		"model":         "kling-group",
		"provider_name": "kling",
		"provider_type": "kling",
		"request_body":  `{"prompt":"一只猫"}`,
		// C1 delta
		"kind":            "video",
		"action":          "text_to_video",
		"vendor_task_id":  "vendor-1",
		"progress":        "0",
		"idempotency_key": "idem-1",
		"private_data":    map[string]any{"upstream_token": "secret-should-never-leak"},
		"dept_id":         "dept-1",
		"studio_id":       "studio-1",
	}

	code, resp := doInternal(t, engine, http.MethodPost, path, body, mediaInternalInternalToken)
	if code != http.StatusCreated {
		t.Fatalf("新建必须返回 201，实际 %d body=%v", code, resp)
	}
	if resp["ok"] != true || resp["created"] != true {
		t.Fatalf("响应形状不符: %v", resp)
	}
	task := taskOf(t, resp)
	for field, want := range map[string]any{
		"task_id": "task-1", "kind": "video", "action": "text_to_video",
		"vendor_task_id": "vendor-1", "provider_type": "kling",
		"idempotency_key": "idem-1", "dept_id": "dept-1", "studio_id": "studio-1",
		// 带 kind 且未给 status → 默认 queued（旧链路不给 kind 才是 submitted）
		"status": "queued", "progress": "0",
	} {
		if task[field] != want {
			t.Errorf("task.%s=%v want %v", field, task[field], want)
		}
	}
	if _, leaked := task["private_data"]; leaked {
		t.Error("private_data 绝不允许出现在 HTTP 响应里（json:\"-\"）")
	}
	raw, _ := json.Marshal(resp)
	if bytes.Contains(raw, []byte("secret-should-never-leak")) {
		t.Error("private_data 内容泄漏到响应体里了")
	}
	// private_data 确实落库了（只是不外发）
	var stored model.VideoTask
	require.NoError(t, db.Where("task_id = ?", "task-1").First(&stored).Error)
	require.NotNil(t, stored.PrivateData)
	if stored.PrivateData["upstream_token"] != "secret-should-never-leak" {
		t.Errorf("private_data 未落库: %+v", stored.PrivateData)
	}

	// 幂等命中：同 (api_key_id, idempotency_key) → 200 + 既有任务，不新建
	dup := map[string]any{}
	for k, v := range body {
		dup[k] = v
	}
	dup["task_id"] = "task-2" // 连 task_id 都不同，仍必须命中幂等
	code, resp = doInternal(t, engine, http.MethodPost, path, dup, mediaInternalInternalToken)
	if code != http.StatusOK {
		t.Fatalf("幂等命中必须返回 200，实际 %d body=%v", code, resp)
	}
	if resp["created"] != false {
		t.Fatalf("幂等命中必须 created=false: %v", resp)
	}
	if got := taskOf(t, resp)["task_id"]; got != "task-1" {
		t.Fatalf("幂等命中必须返回既有 task-1，实际 %v", got)
	}
	var count int64
	require.NoError(t, db.Model(&model.VideoTask{}).Count(&count).Error)
	if count != 1 {
		t.Fatalf("幂等命中后总行数应为 1，实际 %d", count)
	}

	// 无幂等键的两次提交都必须成功（空键落 NULL，不撞唯一索引）
	noIdem := map[string]any{
		"task_id": "task-n1", "api_key_id": "key-a", "model": "m",
		"provider_name": "kling", "provider_type": "kling", "kind": "video",
	}
	if code, resp = doInternal(t, engine, http.MethodPost, path, noIdem, mediaInternalInternalToken); code != http.StatusCreated {
		t.Fatalf("无幂等键的新建应 201，实际 %d %v", code, resp)
	}
	noIdem["task_id"] = "task-n2"
	if code, resp = doInternal(t, engine, http.MethodPost, path, noIdem, mediaInternalInternalToken); code != http.StatusCreated {
		t.Fatalf("同一 Key 的第二个无幂等键任务应 201（NULL 互不相等），实际 %d %v", code, resp)
	}
}

func TestInternalCreateMediaTask_LegacyCompatibilityAndValidation(t *testing.T) {
	db := mediaInternalDB(t)
	engine := mediaInternalEngine(t, db)
	const path = "/internal/v1/video/tasks"

	// 旧网关 client 的报文（无 kind / 无 status）必须逐字兼容：默认 submitted、200/201 都可
	legacy := map[string]any{
		"task_id": "legacy-1", "api_key_id": "key-l", "api_key_name": "k", "api_key_type": "service",
		"user_id": "u", "user_name": "n", "model": "seedance-2.0",
		"provider_name": "seedance", "provider_type": "seedance-relay",
		"request_body": `{"prompt":"p"}`,
	}
	code, resp := doInternal(t, engine, http.MethodPost, path, legacy, mediaInternalInternalToken)
	if code != http.StatusCreated {
		t.Fatalf("新建应 201，实际 %d %v", code, resp)
	}
	if got := taskOf(t, resp)["status"]; got != "submitted" {
		t.Fatalf("不带 kind 的旧链路必须保持历史默认 submitted，实际 %v", got)
	}

	// kind 非法 → 400
	bad := map[string]any{
		"task_id": "bad-1", "api_key_id": "key-a", "model": "m",
		"provider_name": "p", "provider_type": "pt", "kind": "audio",
	}
	if code, _ = doInternal(t, engine, http.MethodPost, path, bad, mediaInternalInternalToken); code != http.StatusBadRequest {
		t.Fatalf("非法 kind 应 400，实际 %d", code)
	}
	// 必填缺失 → 400（既有校验不变）
	if code, _ = doInternal(t, engine, http.MethodPost, path, map[string]any{"task_id": "x"}, mediaInternalInternalToken); code != http.StatusBadRequest {
		t.Fatalf("缺必填字段应 400，实际 %d", code)
	}
	// 超长幂等键 → 400（提前拦住，不让 DB 抛 500）
	long := strings.Repeat("k", 192)
	tooLong := map[string]any{
		"task_id": "bad-2", "api_key_id": "key-a", "model": "m",
		"provider_name": "p", "provider_type": "pt", "idempotency_key": long,
	}
	if code, _ = doInternal(t, engine, http.MethodPost, path, tooLong, mediaInternalInternalToken); code != http.StatusBadRequest {
		t.Fatalf("超长幂等键应 400，实际 %d", code)
	}
}

// ───────────────────────────── PATCH /:id：CAS + 状态机 ─────────────────────────────

func TestInternalUpdateMediaTask_CASStateMachineAndResponseShape(t *testing.T) {
	db := mediaInternalDB(t)
	engine := mediaInternalEngine(t, db)
	createPath := "/internal/v1/video/tasks"
	patchPath := "/internal/v1/video/tasks/task-cas"

	code, _ := doInternal(t, engine, http.MethodPost, createPath, map[string]any{
		"task_id": "task-cas", "api_key_id": "key-a", "model": "m",
		"provider_name": "kling", "provider_type": "kling", "kind": "video",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusCreated, code)

	// 前进 + CAS 命中：updated=true，且 started_at 被写
	code, resp := doInternal(t, engine, http.MethodPatch, patchPath, map[string]any{
		"expected_status": "queued",
		"status":          "in_progress",
		"progress":        "42",
		"vendor_task_id":  "vendor-9",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["updated"] != true {
		t.Fatalf("CAS 命中应 updated=true: %v", resp)
	}
	task := taskOf(t, resp)
	if task["status"] != "in_progress" || task["progress"] != "42" || task["vendor_task_id"] != "vendor-9" {
		t.Fatalf("字段未生效: %v", task)
	}
	if task["started_at"] == nil {
		t.Error("进入 in_progress 必须由服务端写 started_at")
	}

	// CAS 不匹配：updated=false + 当前快照（不是错误）
	code, resp = doInternal(t, engine, http.MethodPatch, patchPath, map[string]any{
		"expected_status": "queued",
		"status":          "completed",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["updated"] != false {
		t.Fatalf("CAS 不匹配必须 updated=false: %v", resp)
	}
	if got := taskOf(t, resp)["status"]; got != "in_progress" {
		t.Fatalf("CAS 不匹配不得改状态，实际 %v", got)
	}

	// 终态：usage（对象）/ cost_micro_usd / billed_at(RFC3339) / poll_failures 全部写入
	billed := time.Now().UTC().Format(time.RFC3339)
	code, resp = doInternal(t, engine, http.MethodPatch, patchPath, map[string]any{
		"expected_status": "in_progress",
		"status":          "completed",
		"usage":           map[string]any{"credits": 3.5, "duration": 5},
		"cost_micro_usd":  125000,
		"billed_at":       billed,
		"poll_failures":   1,
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["updated"] != true {
		t.Fatalf("终态更新应 updated=true: %v", resp)
	}
	task = taskOf(t, resp)
	usage, ok := task["usage"].(map[string]any)
	if !ok || usage["credits"] != 3.5 {
		t.Fatalf("usage 未按 JSON 对象写入/返回: %v", task["usage"])
	}
	if task["cost_micro_usd"] != float64(125000) {
		t.Errorf("cost_micro_usd=%v want 125000（整数微美元，不得用浮点金额）", task["cost_micro_usd"])
	}
	if task["billed_at"] == nil || task["billed_at"] != billed {
		t.Errorf("billed_at 必须按 RFC3339 往返，got %v want %s", task["billed_at"], billed)
	}
	if task["finished_at"] == nil {
		t.Error("进入终态必须由服务端写 finished_at")
	}

	// 终态不回退
	code, resp = doInternal(t, engine, http.MethodPatch, patchPath, map[string]any{
		"status": "in_progress",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["updated"] != false || taskOf(t, resp)["status"] != "completed" {
		t.Fatalf("终态回退必须被拒且状态不变: %v", resp)
	}

	// 任务不存在 → 404（诚实失败，不静默 200）
	code, _ = doInternal(t, engine, http.MethodPatch, "/internal/v1/video/tasks/absent", map[string]any{
		"status": "in_progress",
	}, mediaInternalInternalToken)
	if code != http.StatusNotFound {
		t.Fatalf("缺失任务应 404，实际 %d", code)
	}
}

func TestInternalUpdateMediaTask_LegacyStatusEndpointSemanticsUnchanged(t *testing.T) {
	db := mediaInternalDB(t)
	engine := mediaInternalEngine(t, db)

	code, _ := doInternal(t, engine, http.MethodPost, "/internal/v1/video/tasks", map[string]any{
		"task_id": "legacy-1", "api_key_id": "key-l", "model": "seedance-2.0",
		"provider_name": "seedance", "provider_type": "seedance-relay",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusCreated, code)

	// 旧链路的 submitted → running → succeeded 必须原样工作（不被新状态机拦下）
	for _, status := range []string{"running", "succeeded"} {
		code, resp := doInternal(t, engine, http.MethodPatch, "/internal/v1/video/tasks/legacy-1",
			map[string]any{"status": status}, mediaInternalInternalToken)
		require.Equal(t, http.StatusOK, code)
		if resp["updated"] != true {
			t.Fatalf("legacy 链路 %s 应生效: %v", status, resp)
		}
		if got := taskOf(t, resp)["status"]; got != status {
			t.Fatalf("legacy 状态应为 %s，实际 %v", status, got)
		}
	}

	// PATCH /:id/status 保持旧语义：legacy 四件套（status/video_url/response_body/error_message）
	// 必须原样生效，C1 新字段在此路径上被忽略（它与旧版一样只服务旧链路）
	code, resp := doInternal(t, engine, http.MethodPatch, "/internal/v1/video/tasks/legacy-1/status",
		map[string]any{
			"status":        "failed",
			"video_url":     "https://cdn.example.com/v.mp4",
			"response_body": `{"state":"failed"}`,
			"error_message": "upstream 报错",
			"progress":      "77", // 新字段：旧端点不认
		}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["ok"] != true {
		t.Fatalf("旧 /status 端点必须保持 {ok:true}: %v", resp)
	}
	var legacyRow model.VideoTask
	require.NoError(t, db.Where("task_id = ?", "legacy-1").First(&legacyRow).Error)
	if legacyRow.Status != "failed" || legacyRow.VideoURL != "https://cdn.example.com/v.mp4" ||
		legacyRow.ResponseBody != `{"state":"failed"}` || legacyRow.ErrorMessage != "upstream 报错" {
		t.Fatalf("旧 /status 的字段映射被改变: %+v", legacyRow)
	}
	if legacyRow.Progress == "77" {
		t.Error("旧 /status 不应接受 C1 新字段（语义需与改名前的转发实现逐字一致）")
	}

	// PATCH /:id/status 保持旧语义：{ok:true} + 200，即使任务不存在也 200（不引入 404）
	code, resp = doInternal(t, engine, http.MethodPatch, "/internal/v1/video/tasks/legacy-1/status",
		map[string]any{"status": "running"}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["ok"] != true {
		t.Fatalf("旧 /status 端点必须保持 {ok:true}: %v", resp)
	}
	code, resp = doInternal(t, engine, http.MethodPatch, "/internal/v1/video/tasks/not-there/status",
		map[string]any{"status": "running"}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["ok"] != true {
		t.Fatalf("旧 /status 端点对缺失任务必须仍是 200 {ok:true}（语义不变）: %v", resp)
	}
}

// ───────────────────────────── GET /:id 与 provider-type ─────────────────────────────

func TestInternalGetMediaTask_AndProviderTypeUnchanged(t *testing.T) {
	db := mediaInternalDB(t)
	engine := mediaInternalEngine(t, db)

	code, _ := doInternal(t, engine, http.MethodPost, "/internal/v1/video/tasks", map[string]any{
		"task_id": "task-get", "api_key_id": "key-a", "model": "kling-group",
		"provider_name": "kling", "provider_type": "kling-v2", "kind": "video",
		"action": "image_to_video", "vendor_task_id": "vendor-77",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusCreated, code)

	code, resp := doInternal(t, engine, http.MethodGet, "/internal/v1/video/tasks/task-get", nil, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	task := taskOf(t, resp)
	for field, want := range map[string]any{
		"task_id": "task-get", "provider_type": "kling-v2", "model": "kling-group",
		"kind": "video", "action": "image_to_video", "vendor_task_id": "vendor-77",
		"status": "queued",
	} {
		if task[field] != want {
			t.Errorf("GET task.%s=%v want %v", field, task[field], want)
		}
	}
	if _, leaked := task["private_data"]; leaked {
		t.Error("GET 不得返回 private_data")
	}

	// 缺失任务 → 404
	if code, _ = doInternal(t, engine, http.MethodGet, "/internal/v1/video/tasks/absent", nil, mediaInternalInternalToken); code != http.StatusNotFound {
		t.Fatalf("缺失任务 GET 应 404，实际 %d", code)
	}

	// 既有 provider-type 端点语义不变
	code, resp = doInternal(t, engine, http.MethodGet, "/internal/v1/video/tasks/task-get/provider-type", nil, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["provider_type"] != "kling-v2" || resp["model"] != "kling-group" || resp["provider_name"] != "kling" {
		t.Fatalf("provider-type 响应语义被改变: %v", resp)
	}
}

// ───────────────────────────── expired → failed 归一 ─────────────────────────────

func TestInternalGetMediaTask_NormalizesExpiredToFailed(t *testing.T) {
	db := mediaInternalDB(t)
	engine := mediaInternalEngine(t, db)

	code, _ := doInternal(t, engine, http.MethodPost, "/internal/v1/video/tasks", map[string]any{
		"task_id": "task-exp", "api_key_id": "key-a", "model": "m",
		"provider_name": "kling", "provider_type": "kling", "kind": "video",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusCreated, code)

	// sweeper 把它置为存储层终态 expired（这里直接改库模拟）
	require.NoError(t, db.Model(&model.VideoTask{}).Where("task_id = ?", "task-exp").
		Update("status", model.MediaTaskStatusExpired).Error)

	code, resp := doInternal(t, engine, http.MethodGet, "/internal/v1/video/tasks/task-exp", nil, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if got := taskOf(t, resp)["status"]; got != "failed" {
		t.Fatalf("对调用方 expired 必须归一为 failed，实际 %v", got)
	}

	// 且调用方用归一后的 failed 做 CAS 也必须命中（否则会陷入永远 CAS 失败）
	code, resp = doInternal(t, engine, http.MethodPatch, "/internal/v1/video/tasks/task-exp", map[string]any{
		"expected_status": "failed",
		"status":          "expired",
		"progress":        "100",
	}, mediaInternalInternalToken)
	require.Equal(t, http.StatusOK, code)
	if resp["updated"] != true {
		t.Fatalf("expected_status=failed 应能匹配 DB 里的 expired: %v", resp)
	}
	if got := taskOf(t, resp)["status"]; got != "failed" {
		t.Fatalf("归一后仍须是 failed，实际 %v", got)
	}
}
