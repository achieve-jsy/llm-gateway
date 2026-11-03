package kling_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/kling"
)

// ---------------------------------------------------------------------------
// 鉴权：HS256 JWT 必须能被独立验签
// ---------------------------------------------------------------------------

// 测试目的：提交请求带的是「用 secret_key 签的 HS256 JWT」，且 claims 与上游契约逐字一致
// （iss=access_key、exp=now+1800、nbf=now-5、header.typ=JWT）。
//
// 独立验签：用同一个库（github.com/golang-jwt/jwt/v5）以 secret_key 解析并校验签名，
// 再断言用错误密钥验签必定失败——证明签名真的绑定了 secret_key，而不是形似。
func TestSubmit_SignsVerifiableHS256JWT(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	before := time.Now().Unix()
	_, err := adapter.Submit(context.Background(), submitRequest(core.MediaActionTextToVideo, nil, nil))
	require.NoError(t, err)
	after := time.Now().Unix()

	req := fake.last(t)
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "/v1/videos/text2video", req.Path)

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"), "提交必须带 Content-Type")
	assert.Equal(t, "application/json", req.Header.Get("Accept"), "提交必须带 Accept")
	assert.Equal(t, testUserAgent, req.Header.Get("User-Agent"))

	auth := req.Header.Get("Authorization")
	require.True(t, strings.HasPrefix(auth, "Bearer "), "Authorization 必须是 Bearer 前缀，实际 %q", auth)
	rawToken := strings.TrimPrefix(auth, "Bearer ")

	var verified jwt.MapClaims
	parsed, err := jwt.Parse(rawToken, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期签名算法: %v", token.Header["alg"])
		}
		return []byte(testSecretKey), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	require.NoError(t, err, "token 必须能用 secret_key 验签通过")
	require.True(t, parsed.Valid)

	assert.Equal(t, "HS256", parsed.Method.Alg())
	assert.Equal(t, "JWT", parsed.Header["typ"], "上游要求 header.typ=JWT")

	var ok bool
	verified, ok = parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)

	assert.Equal(t, testAccessKey, verified["iss"], "iss 必须是 access_key")

	exp, ok := verified["exp"].(float64)
	require.True(t, ok, "exp 必须是数字")
	assert.GreaterOrEqual(t, int64(exp), before+1800, "exp 必须是签发时刻 +1800s")
	assert.LessOrEqual(t, int64(exp), after+1800, "exp 必须是签发时刻 +1800s")

	nbf, ok := verified["nbf"].(float64)
	require.True(t, ok, "nbf 必须是数字")
	assert.GreaterOrEqual(t, int64(nbf), before-5, "nbf 必须是签发时刻 -5s")
	assert.LessOrEqual(t, int64(nbf), after-5, "nbf 必须是签发时刻 -5s")

	// 负向：换一个密钥必须验签失败（证明签名绑定 secret_key）。
	_, err = jwt.Parse(rawToken, func(*jwt.Token) (any, error) {
		return []byte("wrong-secret-key"), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	require.Error(t, err, "用错误密钥验签必须失败")
}

// 测试目的：查询请求同样带 JWT，且**不带** Content-Type（GET 无请求体，与 New API 一致）。
func TestQuery_SendsAcceptAndJWTWithoutContentType(t *testing.T) {
	fake := startFakeUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		_, _ = w.Write([]byte(queryBody("processing", "", "", "")))
	})
	adapter := newTestAdapter(t, fake)

	vendorTaskID := queryVendorTaskID(t, adapter)
	_, err := adapter.Query(context.Background(), vendorTaskID)
	require.NoError(t, err)

	req := fake.last(t)
	require.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
	assert.Equal(t, testUserAgent, req.Header.Get("User-Agent"))
	assert.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "Bearer "))
	assert.Empty(t, req.Header.Get("Content-Type"), "查询不得带 Content-Type")
}

// ---------------------------------------------------------------------------
// action → 上游路径映射（显式表，不靠字符串相似）
// ---------------------------------------------------------------------------

// 测试目的：core 归一动作 → 上游端点路径的显式映射；首尾帧走 image2video + image_tail。
//
// 极性陷阱回归：New API 的 TaskActionGenerate 指 image2video、其余是 text2video，
// 本表必须保证 text_to_video→text2video、image_to_video→image2video，不得反向。
func TestSubmit_MapsActionToUpstreamPath(t *testing.T) {
	firstFrame := "https://cdn.example.com/first.png"
	lastFrame := "https://cdn.example.com/last.png"

	cases := []struct {
		name          string
		action        core.MediaAction
		images        []string
		wantPath      string
		wantImage     string
		wantImageTail string
	}{
		{
			name:     "文生视频 → text2video",
			action:   core.MediaActionTextToVideo,
			images:   nil,
			wantPath: "/v1/videos/text2video",
		},
		{
			name:      "图生视频 → image2video",
			action:    core.MediaActionImageToVideo,
			images:    []string{firstFrame},
			wantPath:  "/v1/videos/image2video",
			wantImage: firstFrame,
		},
		{
			name:          "首尾帧 → image2video + image_tail",
			action:        core.MediaActionFirstTailToVideo,
			images:        []string{firstFrame, lastFrame},
			wantPath:      "/v1/videos/image2video",
			wantImage:     firstFrame,
			wantImageTail: lastFrame,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			_, err := adapter.Submit(t.Context(), submitRequest(tc.action, tc.images, nil))
			require.NoError(t, err)

			req := fake.last(t)
			assert.Equal(t, tc.wantPath, req.Path)

			body := decodeBody(t, req.Body)
			if tc.wantImage == "" {
				assert.NotContains(t, body, "image", "文生视频不得出现 image")
			} else {
				assert.Equal(t, tc.wantImage, body["image"], "首帧必须发到 image")
			}
			if tc.wantImageTail == "" {
				assert.NotContains(t, body, "image_tail", "非首尾帧动作不得出现 image_tail")
			} else {
				assert.Equal(t, tc.wantImageTail, body["image_tail"], "尾帧必须发到 image_tail")
			}
		})
	}
}

// 测试目的：不支持的 action（Kling 没有 reference2video 端点）必须前置 400，不打上游。
func TestSubmit_RejectsUnsupportedAction(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionReferenceToVideo,
		[]string{"https://cdn.example.com/a.png", "https://cdn.example.com/b.png", "https://cdn.example.com/c.png"}, nil))

	assertBadRequest(t, err)
	assert.Contains(t, err.Error(), "reference_to_video")
	assert.Zero(t, fake.count(), "参数校验失败不得打上游")
}

// 测试目的：action 与图片数量不一致（静默丢图的入口）必须前置 400。
func TestSubmit_RejectsImageCountMismatch(t *testing.T) {
	cases := []struct {
		name   string
		action core.MediaAction
		images []string
	}{
		{"文生视频带图", core.MediaActionTextToVideo, []string{"https://cdn.example.com/a.png"}},
		{"图生视频无图", core.MediaActionImageToVideo, nil},
		{"图生视频带两图", core.MediaActionImageToVideo, []string{"https://cdn.example.com/a.png", "https://cdn.example.com/b.png"}},
		{"首尾帧只有一张", core.MediaActionFirstTailToVideo, []string{"https://cdn.example.com/a.png"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			_, err := adapter.Submit(t.Context(), submitRequest(tc.action, tc.images, nil))
			assertBadRequest(t, err)
			assert.Contains(t, err.Error(), "张图")
			assert.Zero(t, fake.count())
		})
	}
}

// ---------------------------------------------------------------------------
// 请求体：golden 字段
// ---------------------------------------------------------------------------

// 测试目的：请求体字段名/取值与上游契约逐字一致（golden 报文）。
//
// 关键点：duration 是**字符串**、cfg_scale 固定 0.5、model_name 与 model **同值且都发**、
// aspect_ratio 由 size 推导；`resolution` 是网关归一参数但上游没有该字段，不得出现在报文里。
func TestSubmit_RequestBodyGolden(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, func(params map[string]any) {
		params["prompt"] = "一只猫在弹钢琴"
		params["duration"] = 10
		params["mode"] = "pro"
		params["resolution"] = "1080p"
		params["size"] = "1920x1080"
	}))
	require.NoError(t, err)

	body := fake.last(t).Body
	const want = `{
		"prompt": "一只猫在弹钢琴",
		"mode": "pro",
		"duration": "10",
		"aspect_ratio": "16:9",
		"model_name": "kling-v1-6",
		"model": "kling-v1-6",
		"cfg_scale": 0.5
	}`
	assert.JSONEq(t, want, string(body))

	// 上游没有 resolution 字段，严禁把网关归一参数原样透传出去。
	assert.NotContains(t, string(body), "resolution")
	decoded := decodeBody(t, body)
	for _, forbidden := range []string{
		"negative_prompt", "static_mask", "dynamic_masks", "camera_control",
		"callback_url", "external_task_id", "resolution", "images", "size",
	} {
		assert.NotContains(t, decoded, forbidden)
	}
}

// 测试目的：首尾帧的 golden 报文（image + image_tail 同时出现）。
func TestSubmit_FirstTailRequestBodyGolden(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionFirstTailToVideo,
		[]string{"https://cdn.example.com/first.png", "https://cdn.example.com/last.png"},
		func(params map[string]any) {
			params["mode"] = "std"
			params["duration"] = 5
			params["size"] = "720x1280"
			params["prompt"] = ""
		}))
	require.NoError(t, err)

	const want = `{
		"image": "https://cdn.example.com/first.png",
		"image_tail": "https://cdn.example.com/last.png",
		"mode": "std",
		"duration": "5",
		"aspect_ratio": "9:16",
		"model_name": "kling-v1-6",
		"model": "kling-v1-6",
		"cfg_scale": 0.5
	}`
	assert.JSONEq(t, want, string(fake.last(t).Body))
}

// 测试目的：duration 缺省/非正数 → "5"；越界 → 400。
func TestSubmit_DurationBounds(t *testing.T) {
	cases := []struct {
		name         string
		duration     any
		wantDuration string
		wantErr      bool
	}{
		{name: "缺省（0 视为未提供）", duration: 0, wantDuration: "5"},
		{name: "负数视为未提供", duration: -3, wantDuration: "5"},
		{name: "最小值 1", duration: 1, wantDuration: "1"},
		{name: "最大值 3600", duration: 3600, wantDuration: "3600"},
		{name: "浮点写法", duration: float64(7), wantDuration: "7"},
		{name: "字符串写法", duration: "9", wantDuration: "9"},
		{name: "超上界", duration: 3601, wantErr: true},
		{name: "非数字字符串", duration: "abc", wantErr: true},
		{name: "非法类型", duration: []string{"5"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, func(params map[string]any) {
				params["duration"] = tc.duration
			}))
			if tc.wantErr {
				assertBadRequest(t, err)
				assert.Zero(t, fake.count())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantDuration, decodeBody(t, fake.last(t).Body)["duration"])
		})
	}
}

// 测试目的：aspect_ratio 优先用显式取值，其次由 size 显式映射，推不出则留空由上游取默认
// （**绝不**瞎猜 1:1）。
func TestSubmit_AspectRatioResolution(t *testing.T) {
	cases := []struct {
		name       string
		size       string
		aspect     string
		wantAspect string
		wantAbsent bool
	}{
		{name: "size 1024x1024 → 1:1", size: "1024x1024", wantAspect: "1:1"},
		{name: "size 512x512 → 1:1", size: "512x512", wantAspect: "1:1"},
		{name: "size 1280x720 → 16:9", size: "1280x720", wantAspect: "16:9"},
		{name: "size 1920x1080 → 16:9", size: "1920x1080", wantAspect: "16:9"},
		{name: "size 720x1280 → 9:16", size: "720x1280", wantAspect: "9:16"},
		{name: "size 1080x1920 → 9:16", size: "1080x1920", wantAspect: "9:16"},
		{name: "大小写归一", size: "1280X720", wantAspect: "16:9"},
		{name: "表外 size 留空（不猜 1:1）", size: "1024x768", wantAbsent: true},
		{name: "无 size 留空", size: "", wantAbsent: true},
		{name: "显式 aspect_ratio 优先于 size", size: "1280x720", aspect: "9:16", wantAspect: "9:16"},
		{name: "只有显式 aspect_ratio", aspect: "16:9", wantAspect: "16:9"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, func(params map[string]any) {
				params["size"] = tc.size
				params["aspect_ratio"] = tc.aspect
			}))
			require.NoError(t, err)

			body := decodeBody(t, fake.last(t).Body)
			if tc.wantAbsent {
				assert.NotContains(t, body, "aspect_ratio", "推不出宽高比时必须留空交给上游默认")
				return
			}
			assert.Equal(t, tc.wantAspect, body["aspect_ratio"])
		})
	}
}

// 测试目的：mode 缺省 std；只接受 std/pro。
func TestSubmit_ModeDefaultsAndValidation(t *testing.T) {
	cases := []struct {
		name     string
		mode     any
		wantMode string
		wantErr  bool
	}{
		{name: "缺省 std", mode: "", wantMode: "std"},
		{name: "空白缺省 std", mode: "   ", wantMode: "std"},
		{name: "std", mode: "std", wantMode: "std"},
		{name: "pro", mode: "pro", wantMode: "pro"},
		{name: "未知 mode", mode: "turbo", wantErr: true},
		{name: "类型错误", mode: 1, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, func(params map[string]any) {
				params["mode"] = tc.mode
			}))
			if tc.wantErr {
				assertBadRequest(t, err)
				assert.Zero(t, fake.count())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantMode, decodeBody(t, fake.last(t).Body)["mode"])
		})
	}
}

// 测试目的：kling-v2-master 不支持 std → 必须在 Submit 里提前 400，**不发上游**
// （发给上游会直接报错且可能已扣配额）。
func TestSubmit_RejectsV2MasterWithStd(t *testing.T) {
	cases := []struct {
		name string
		mode any
	}{
		{name: "显式 std", mode: "std"},
		{name: "缺省（std）", mode: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			req := submitRequest(core.MediaActionTextToVideo, nil, func(params map[string]any) {
				params["mode"] = tc.mode
			})
			req.UpstreamModel = "kling-v2-master"

			_, err := adapter.Submit(t.Context(), req)
			assertBadRequest(t, err)
			assert.Contains(t, err.Error(), "kling-v2-master")
			assert.Contains(t, err.Error(), "pro")
			assert.Zero(t, fake.count(), "必须提前失败，不得打上游")
		})
	}

	t.Run("pro 放行", func(t *testing.T) {
		fake := startFakeUpstream(t, nil)
		adapter := newTestAdapter(t, fake)

		req := submitRequest(core.MediaActionTextToVideo, nil, func(params map[string]any) {
			params["mode"] = "pro"
		})
		req.UpstreamModel = "kling-v2-master"

		_, err := adapter.Submit(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "kling-v2-master", decodeBody(t, fake.last(t).Body)["model"])
	})
}

// ---------------------------------------------------------------------------
// Params fail-closed（冻结 7 键）
// ---------------------------------------------------------------------------

// 测试目的：表外参数必须 400（fail-closed），严禁静默忽略——静默忽略会让用户以为
// 参数生效了却照付费用。
func TestSubmit_RejectsParamsOutsideFrozenKeySet(t *testing.T) {
	cases := []string{
		"negative_prompt",
		"camera_control",
		"static_mask",
		"dynamic_masks",
		"callback_url",
		"external_task_id",
		"client_request_id",
		"cfg_scale",
		"model",
		"Duration", // 大小写不同也不认
		"",
	}

	for _, key := range cases {
		t.Run("表外键 "+key, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, func(params map[string]any) {
				params[key] = "whatever"
			}))
			assertBadRequest(t, err)
			if key != "" {
				assert.Contains(t, err.Error(), key)
			}
			assert.Zero(t, fake.count(), "表外参数不得打上游")
		})
	}
}

// 测试目的：冻结 7 键齐备时全部被接受，且每个键都被处理（resolution 是网关侧参数，
// 上游无对应字段，故不出现在报文里；其余键必须体现在报文或校验上）。
func TestSubmit_AcceptsAllFrozenKeys(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionImageToVideo,
		[]string{"https://cdn.example.com/a.png"}, func(params map[string]any) {
			params["prompt"] = "让画面动起来"
			params["duration"] = 8
			params["resolution"] = "720p"
			params["mode"] = "pro"
			params["aspect_ratio"] = ""
			params["size"] = "1280x720"
		}))
	require.NoError(t, err)

	body := decodeBody(t, fake.last(t).Body)
	assert.Equal(t, "让画面动起来", body["prompt"])
	assert.Equal(t, "8", body["duration"])
	assert.Equal(t, "pro", body["mode"])
	assert.Equal(t, "16:9", body["aspect_ratio"])
	assert.Equal(t, "https://cdn.example.com/a.png", body["image"])
}

// ---------------------------------------------------------------------------
// 提交响应处理
// ---------------------------------------------------------------------------

// 测试目的：提交成功 → 返回**上游任务 id**（编码了 action，见 Query 复用 action 用例），
// 且提交返回的初始状态同样走状态映射。
func TestSubmit_ReturnsUpstreamTaskIDAndInitialStatus(t *testing.T) {
	cases := []struct {
		name           string
		upstreamStatus string
		wantStatus     core.MediaStatus
		wantErr        bool
	}{
		{name: "submitted → queued", upstreamStatus: "submitted", wantStatus: core.MediaStatusQueued},
		{name: "空状态 → queued（零值语义）", upstreamStatus: "", wantStatus: core.MediaStatusQueued},
		{name: "processing → in_progress", upstreamStatus: "processing", wantStatus: core.MediaStatusInProgress},
		{name: "未知状态 → 报错", upstreamStatus: "STARTED", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(http.StatusOK, submitBody(upstreamTaskID, tc.upstreamStatus)))
			adapter := newTestAdapter(t, fake)

			result, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, nil))
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tc.wantStatus, result.Status)
			// vendorTaskID 必须是「action 前缀 + 上游 id」（不含任何对外 id）。
			assert.Equal(t, "text2video:"+upstreamTaskID, result.VendorTaskID)
		})
	}
}

// 测试目的：上游业务拒绝（code != 0）、非 2xx、缺 task_id、非法 JSON 都必须返回 error
// 且**不得**给出 vendorTaskID。
func TestSubmit_UpstreamFailures(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		wantInErr  string
	}{
		{
			name:       "上游业务错误码",
			statusCode: http.StatusOK,
			body:       `{"code":1201,"message":"prompt is required","data":{}}`,
			wantInErr:  "1201",
		},
		{
			name:       "上游 500",
			statusCode: http.StatusInternalServerError,
			body:       `{"code":500,"message":"internal"}`,
			wantInErr:  "500",
		},
		{
			name:       "上游 401",
			statusCode: http.StatusUnauthorized,
			body:       `{"code":1002,"message":"auth failed"}`,
			wantInErr:  "401",
		},
		{
			name:       "缺 task_id",
			statusCode: http.StatusOK,
			body:       `{"code":0,"message":"SUCCEED","data":{"task_status":"submitted"}}`,
			wantInErr:  "task_id",
		},
		{
			name:       "非法 JSON",
			statusCode: http.StatusOK,
			body:       `<html>bad gateway</html>`,
			wantInErr:  "JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(tc.statusCode, tc.body))
			adapter := newTestAdapter(t, fake)

			result, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, nil))
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.wantInErr)
		})
	}
}

// 测试目的：首次提交返回 null 参数 / 空请求不得 panic（防御）。
func TestSubmit_NilRequest(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	result, err := adapter.Submit(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Zero(t, fake.count())
}

// 测试目的：上游模型为空时兜底 kling-v1（与 New API 一致），保证请求体不缺 model。
func TestSubmit_DefaultsUpstreamModelWhenEmpty(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	req := submitRequest(core.MediaActionTextToVideo, nil, nil)
	req.UpstreamModel = ""

	_, err := adapter.Submit(t.Context(), req)
	require.NoError(t, err)

	body := decodeBody(t, fake.last(t).Body)
	assert.Equal(t, "kling-v1", body["model"])
	assert.Equal(t, "kling-v1", body["model_name"])
}

// 测试目的：base URL 结尾多一个 '/' 不得产生双斜杠路径（构造期归一）。
func TestSubmit_NormalizesBaseURLTrailingSlash(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newAdapterWith(t, providers.MediaAdapterParams{
		Type:        kling.MediaProviderType,
		BaseURL:     fake.URL + "///",
		Credentials: testCredentials,
	})

	_, err := adapter.Submit(t.Context(), submitRequest(core.MediaActionTextToVideo, nil, nil))
	require.NoError(t, err)
	assert.Equal(t, "/v1/videos/text2video", fake.last(t).Path)
}
