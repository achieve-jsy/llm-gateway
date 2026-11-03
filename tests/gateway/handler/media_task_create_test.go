package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 创建链路（C5/C6）——含 Params 冻结键集、粗粒度准入、幂等语义
// ─────────────────────────────────────────────────────────────────────────────

// assertFrozenParams 校验 core.MediaSubmitRequest.Params 的**冻结键集与类型**。
//
// 冻结来源：docs/media-channels-integration-design.md §6.1.1（Batch 1 补充通知）。
// D 队适配器是 fail-closed 的：多一个键就 400，所以这里逐键比对，任何新增都会让本测试失败。
func assertFrozenParams(t *testing.T, params map[string]any) {
	t.Helper()
	require.NotNil(t, params)
	allowed := map[string]struct{}{
		"prompt": {}, "images": {}, "duration": {}, "resolution": {},
		"mode": {}, "aspect_ratio": {}, "size": {},
	}
	require.Len(t, params, len(allowed), "Params 必须且只能有 7 个冻结键：%v", params)
	for key := range params {
		_, ok := allowed[key]
		require.True(t, ok, "Params 出现表外键 %q（适配器 fail-closed 会 400）", key)
	}
	_, ok := params["prompt"].(string)
	require.True(t, ok, "prompt 必须是 string，实际 %T", params["prompt"])
	_, ok = params["images"].([]string)
	require.True(t, ok, "images 必须是 []string，实际 %T", params["images"])
	_, ok = params["duration"].(int)
	require.True(t, ok, "duration 必须是 int（秒），实际 %T", params["duration"])
	for _, key := range []string{"resolution", "mode", "aspect_ratio", "size"} {
		_, ok := params[key].(string)
		require.True(t, ok, "%s 必须是 string，实际 %T", key, params[key])
	}
}

func TestMediaTaskCreate_Success_FrozenParamsAndPersistence(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	body := `{
		"model": "kling-group",
		"prompt": "a cat surfing",
		"image": "https://cdn.example.com/cat.png",
		"duration": "5",
		"resolution": "1080p",
		"mode": "pro",
		"size": "1920x1080"
	}`
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos", body, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// ① 上游只调用一次，且是「不重试」上下文（提交路径至多一次语义）。
	require.Equal(t, 1, rig.adapter.submitCalls)
	require.True(t, rig.adapter.submitNoRetry, "提交上游必须使用 llmclient.WithNoRetry")
	require.NotNil(t, rig.adapter.lastSubmit)
	require.Equal(t, core.MediaKindVideo, rig.adapter.lastSubmit.Kind)
	require.Equal(t, core.MediaActionImageToVideo, rig.adapter.lastSubmit.Action)
	require.Equal(t, testMediaUpstream, rig.adapter.lastSubmit.UpstreamModel)
	assertFrozenParams(t, rig.adapter.lastSubmit.Params)
	require.Equal(t, "a cat surfing", rig.adapter.lastSubmit.Params["prompt"])
	require.Equal(t, []string{"https://cdn.example.com/cat.png"}, rig.adapter.lastSubmit.Params["images"])
	require.Equal(t, 5, rig.adapter.lastSubmit.Params["duration"], "duration 必须归一为 int 秒")
	require.Equal(t, "1080p", rig.adapter.lastSubmit.Params["resolution"])
	require.Equal(t, "pro", rig.adapter.lastSubmit.Params["mode"])
	require.Equal(t, "16:9", rig.adapter.lastSubmit.Params["aspect_ratio"], "缺省 aspect_ratio 由 size 推导")
	require.Equal(t, "1920x1080", rig.adapter.lastSubmit.Params["size"])

	// ② 落库请求：对外 id 由网关生成、vendor id 单独存、渠道细节进 private_data。
	create := rig.store.lastCreate(t).req
	require.True(t, strings.HasPrefix(create.TaskID, "task_"), "对外 id 前缀：%s", create.TaskID)
	require.NotEqual(t, "vendor-1", create.TaskID, "禁止把上游 id 当对外 id")
	require.Equal(t, "vendor-1", create.VendorTaskID)
	require.Equal(t, "video", create.Kind)
	require.Equal(t, "image_to_video", create.Action)
	require.Equal(t, "kling", create.ProviderType)
	require.Equal(t, "kling-group", create.Model)
	require.Equal(t, testMediaKeyID, create.APIKeyID)
	require.Equal(t, testMediaUserID, create.UserID)
	require.Empty(t, create.IdempotencyKey, "未带 Idempotency-Key 时不得伪造幂等键")

	private, ok := parsePrivate(t, string(create.PrivateData))
	require.True(t, ok)
	require.Equal(t, testMediaUpstream, private["upstream_model"])
	require.Equal(t, "kling", private["provider_type"])

	// ③ 对外响应：OpenAI 形状，且**不回传** vendor id。
	resp := decodeJSON(t, rec)
	require.Equal(t, create.TaskID, resp["id"])
	require.Equal(t, "video", resp["object"])
	require.Equal(t, "queued", resp["status"])
	require.Equal(t, "kling-group", resp["model"], "对外 model 必须是模型组名")
	require.NotZero(t, resp["created_at"])
	require.NotContains(t, rec.Body.String(), "vendor-1", "响应体不得出现上游任务 id")
}

func TestMediaTaskCreate_ActionMatrixDrivenByImageCount(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		action core.MediaAction
		images int
	}{
		{"0 张图文生视频", `{"model":"kling-group","prompt":"p"}`, core.MediaActionTextToVideo, 0},
		{"1 张图图生视频", `{"model":"kling-group","image":"https://x/1.png"}`, core.MediaActionImageToVideo, 1},
		{"2 张图首尾帧", `{"model":"kling-group","images":["https://x/1.png","https://x/2.png"]}`, core.MediaActionFirstTailToVideo, 2},
		{"3 张图参考生视频", `{"model":"kling-group","images":["https://x/1.png","https://x/2.png","https://x/3.png"]}`, core.MediaActionReferenceToVideo, 3},
		{"数组形式的单图", `{"model":"kling-group","image":["https://x/1.png"]}`, core.MediaActionImageToVideo, 1},
		{"对象形式的图片", `{"model":"kling-group","images":[{"url":"https://x/1.png"}]}`, core.MediaActionImageToVideo, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMediaTestRig(t, nil, nil)
			rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos", tc.body, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, tc.action, rig.adapter.lastSubmit.Action)
			images, _ := rig.adapter.lastSubmit.Params["images"].([]string)
			require.Len(t, images, tc.images)
		})
	}
}

func TestMediaTaskCreate_AspectRatioDerivation(t *testing.T) {
	cases := []struct {
		size string
		want string
	}{
		{"1280x720", "16:9"},
		{"720x1280", "9:16"},
		{"1024x1024", "1:1"},
		{"640x480", ""},  // 4:3 不在冻结集合内 → 不填，交给适配器 fail-closed
		{"notasize", ""}, // 无法解析 → 不填
		{"", ""},         // 未提供 → 不填
	}
	for _, tc := range cases {
		t.Run("size="+tc.size, func(t *testing.T) {
			rig := newMediaTestRig(t, nil, nil)
			body := `{"model":"kling-group","prompt":"p"`
			if tc.size != "" {
				body += `,"size":"` + tc.size + `"`
			}
			body += `}`
			rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos", body, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, tc.want, rig.adapter.lastSubmit.Params["aspect_ratio"])
		})
	}
}

func TestMediaTaskCreate_RejectsContradictoryAction(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","action":"text_to_video","image":"https://x/1.png"}`, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Equal(t, "invalid_request_error", errorCode(t, rec))
	require.Zero(t, rig.adapter.submitCalls, "参数非法时不得调用上游")
}

func TestMediaTaskCreate_ValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{"缺 model", `{"prompt":"p"}`, "invalid_request_error"},
		{"缺 prompt 与图", `{"model":"kling-group"}`, "invalid_request_error"},
		{"非法 JSON", `{"model":`, "invalid_request_error"},
		{"空图片数组且无 prompt", `{"model":"kling-group","images":[]}`, "invalid_request_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMediaTestRig(t, nil, nil)
			rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos", tc.body, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Equal(t, tc.code, errorCode(t, rec))
			require.Zero(t, rig.adapter.submitCalls)
			require.Zero(t, rig.store.createCalls)
		})
	}
}

func TestMediaTaskCreate_RejectsDirectModelID(t *testing.T) {
	// 组调度返回 (nil, nil) = 不是组名（直连上游模型 ID）→ 403。
	rig := newMediaTestRig(t, nil, func(cfg *handler.MediaTaskHandlerConfig) {
		cfg.GroupDispatch = &fakeGroupDispatcher{decision: nil}
	})
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-v1-6","prompt":"p"}`, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Equal(t, "direct_model_not_allowed", errorCode(t, rec))
	require.Zero(t, rig.adapter.submitCalls)
}

func TestMediaTaskCreate_RejectsUnpricedModel(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	// 闸门在请求时读进程级默认值，因此可以在装配之后再切换成「无价拒绝」。
	rejectMediaPricing(t)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusPaymentRequired, rec.Code, rec.Body.String())
	require.Equal(t, "media_pricing_missing", errorCode(t, rec))
	require.Zero(t, rig.adapter.submitCalls, "无价必须在调用上游之前拒绝")
	require.Zero(t, rig.store.createCalls, "无价拒绝时不得落库")
}

func TestMediaTaskCreate_AdapterNotWiredReturns501(t *testing.T) {
	// 渠道未注册（D 队未接线）：必须 501 not_implemented，且不打上游、不落库。
	rig := newMediaTestRig(t, nil, func(cfg *handler.MediaTaskHandlerConfig) {
		empty := providers.NewMediaAdapterRegistry()
		cfg.Registry = empty
	})
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusNotImplemented, rec.Code, rec.Body.String())
	require.Equal(t, "not_implemented", errorCode(t, rec))
	require.Zero(t, rig.store.createCalls)
}

func TestMediaTaskCreate_StoreUnwiredReturns503(t *testing.T) {
	rig := newMediaTestRig(t, nil, func(cfg *handler.MediaTaskHandlerConfig) {
		cfg.Store = nil
	})
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Equal(t, "service_unavailable", errorCode(t, rec))
}

func TestMediaTaskCreate_StoreWriteFailureIs5xx(t *testing.T) {
	// 同步落库失败必须 5xx：绝不能 fire-and-forget 后返回 200（否则客户端拿到幽灵任务）。
	rig := newMediaTestRig(t, nil, nil)
	rig.store.createErr = &infra.ControlPlaneError{Op: "create_media_task", Status: http.StatusInternalServerError, Body: "boom"}
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Equal(t, "upstream_error", errorCode(t, rec))
}

func TestMediaTaskCreate_StoreWriteFailureClassifiesContractDrift(t *testing.T) {
	// 控制面 4xx（字段类型/必填约束不一致）也必须 5xx，且错误里带上控制面响应片段便于定位。
	rig := newMediaTestRig(t, nil, nil)
	rig.store.createErr = &infra.ControlPlaneError{Op: "create_media_task", Status: http.StatusBadRequest, Body: `{"error":"private_data must be object"}`}
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "private_data must be object")
}

func TestMediaTaskCreate_SubmitFailure(t *testing.T) {
	adapter := &fakeMediaAdapter{submitErr: errors.New("upstream 500")}
	rig := newMediaTestRig(t, adapter, nil)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Equal(t, "upstream_error", errorCode(t, rec))
}

func TestMediaTaskCreate_EmptyVendorTaskIDIsProtocolFailure(t *testing.T) {
	adapter := &fakeMediaAdapter{submitResult: &core.MediaSubmitResult{VendorTaskID: "  "}}
	rig := newMediaTestRig(t, adapter, nil)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Zero(t, rig.store.createCalls, "协议失败时不得落库（否则产生永远查不到上游的幽灵任务）")
}

func TestMediaTaskCreate_IdempotencyHitDoesNotCallUpstream(t *testing.T) {
	// 冻结契约：命中 (api_key_id, idempotency_key) → 200 + 既有任务，**不打上游、不重复计费**。
	rig := newMediaTestRig(t, nil, nil)
	rig.store.seed(&infra.MediaTask{
		TaskID:       "task_existing",
		Kind:         "video",
		Status:       "in_progress",
		VendorTaskID: "vendor-existing",
		Model:        testMediaGroup,
		ProviderType: testMediaProviderType,
		APIKeyID:     testMediaKeyID,
		CreatedAt:    time.Now().UTC(),
	})
	rig.store.idempotentOn = true

	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`,
		map[string]string{"Idempotency-Key": "idem-1"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Zero(t, rig.adapter.submitCalls, "幂等命中绝不能调用上游")
	require.Zero(t, rig.store.updateCount(), "幂等命中不得改任务")

	resp := decodeJSON(t, rec)
	require.Equal(t, "task_existing", resp["id"])
	require.Equal(t, "in_progress", resp["status"])

	first := rig.store.creates[0].req
	require.Equal(t, "idem-1", first.IdempotencyKey, "幂等键必须随创建请求上报（控制面据此去重）")
}

func TestMediaTaskCreate_IdempotencyReservationThenBackfill(t *testing.T) {
	// 带 Idempotency-Key 且未命中：先占位（queued、无 vendor id）→ 提交上游 → 同步回填。
	rig := newMediaTestRig(t, nil, nil)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`,
		map[string]string{"Idempotency-Key": "idem-2"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, rig.adapter.submitCalls)

	require.Equal(t, 1, rig.store.createCalls, "预占只应创建一次")
	require.Equal(t, "queued", rig.store.creates[0].req.Status)
	require.Empty(t, rig.store.creates[0].req.VendorTaskID, "预占时还不知道上游 id")

	require.Equal(t, 1, rig.store.updateCount(), "预占路径只应回填一次（不经过 CAS 推进）")
	backfill := rig.store.lastUpdate(t).req
	require.Equal(t, "vendor-1", backfill.VendorTaskID)
	require.Equal(t, string(core.MediaStatusQueued), backfill.Status)

	task := rig.store.task(rig.store.lastCreate(t).req.TaskID)
	require.Equal(t, "vendor-1", task.VendorTaskID, "回填必须真的落库")
}

func TestMediaTaskCreate_IdempotencySubmitFailureMarksTaskFailed(t *testing.T) {
	// 预占成功但上游提交失败：必须尽力把占位行标成 failed，否则幂等键会永久卡在 queued。
	adapter := &fakeMediaAdapter{submitErr: errors.New("upstream timeout")}
	rig := newMediaTestRig(t, adapter, nil)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p"}`,
		map[string]string{"Idempotency-Key": "idem-3"})
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	stored := rig.store.firstTask()
	require.NotNil(t, stored)
	require.Equal(t, string(core.MediaStatusFailed), stored.Status)
}

func TestMediaTaskCreate_RequestTooLarge(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	huge := `{"model":"kling-group","prompt":"p","image":"data:image/png;base64,` + strings.Repeat("A", 70*1024) + `"}`
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos", huge, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Equal(t, "invalid_request_error", errorCode(t, rec))
}

// parsePrivate 解析 private_data JSON 文本。
func parsePrivate(t *testing.T, raw string) (map[string]any, bool) {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &out), "private_data 必须是 JSON 对象：%s", raw)
	return out, true
}

func TestMediaTaskCreate_IdempotencyKeyFromBodyClientRequestID(t *testing.T) {
	// 设计文档 §6.3 允许用 body 内 client_request_id 代替 Idempotency-Key 头。
	rig := newMediaTestRig(t, nil, nil)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p","client_request_id":"req-from-body"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "req-from-body", rig.store.lastCreate(t).req.IdempotencyKey)

	// client_request_id 不得透传给适配器（Params 是冻结键集）。
	assertFrozenParams(t, rig.adapter.lastSubmit.Params)
	_, leaked := rig.adapter.lastSubmit.Params["client_request_id"]
	require.False(t, leaked)
}
