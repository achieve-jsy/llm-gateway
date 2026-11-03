package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 本文件 = 任务书要求的「IDOR 反面案例」单元测试：
// 用**同一个越权 Key**分别访问既有 Seedance 查询链路与新任务平台链路，固化两条链路的差异。
//
// 背景（docs/media-channels-integration-design.md §1.3）：
// 既有 `/v1/contents/generations/tasks/:task_id` 是「纯直通」链路 —— 只按 task_id
// 去上游取结果，既不校验归属，存储层 `VideoTaskMeta` 也没有 key/user 维度字段，
// 因此**任何一个有效 Key 只要拿到（或猜到）别人的 task_id，就能读到完整上游响应**。
// 这是既有缺陷、不在本次任务范围内修复（B 队负责既有链路），但它正是新链路
// 必须 404 而不是 403 的原因。

// fakeLegacyVideoProvider 是 core/video.VideoProvider 的最小 fake（直通语义）。
type fakeLegacyVideoProvider struct {
	body json.RawMessage
	err  error
}

func (f *fakeLegacyVideoProvider) CreateTask(_ context.Context, _ json.RawMessage) (string, error) {
	return "upstream-task-id", f.err
}

func (f *fakeLegacyVideoProvider) GetTask(_ context.Context, _ string) (json.RawMessage, error) {
	return f.body, f.err
}

// fakeLegacyTaskStore 是 handler.VideoTaskStore 的 fake：只提供路由元数据，无归属信息。
type fakeLegacyTaskStore struct {
	meta *handler.VideoTaskMeta
}

func (s *fakeLegacyTaskStore) Record(_ context.Context, _ handler.VideoTaskRecord) error {
	return nil
}

func (s *fakeLegacyTaskStore) LookupProviderType(_ context.Context, _ string) (string, error) {
	if s.meta == nil {
		return "", nil
	}
	return s.meta.ProviderType, nil
}

func (s *fakeLegacyTaskStore) LookupTaskMeta(_ context.Context, _ string) (*handler.VideoTaskMeta, error) {
	return s.meta, nil
}

func (s *fakeLegacyTaskStore) Update(_ context.Context, _ string, _ handler.VideoTaskUpdate) error {
	return nil
}

func (s *fakeLegacyTaskStore) UpdateStatus(_ context.Context, _, _ string) error {
	return nil
}

// TestLegacyVideoTaskGet_IDOR_Characterization 固化既有缺陷：越权 Key 能读到他人任务。
//
// 这是一个**特性固化（characterization）测试**，断言的是「当前（有缺陷的）行为」，
// 目的是让缺陷可被度量、且一旦被修复能被立即发现（修复者请把断言改成 404 并删掉本注释）。
// 新链路 `/v1/videos/:task_id` 的正确行为见本文件第二个测试（必须 404）。
func TestLegacyVideoTaskGet_IDOR_Characterization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamPayload := json.RawMessage(`{"id":"vendor-secret-task","status":"succeed","video_url":"https://cdn.example.com/victim.mp4"}`)
	legacy := handler.NewVideoHandler(
		&fakeLegacyVideoProvider{body: upstreamPayload},
		nil,
		handler.WithTaskStore(&fakeLegacyTaskStore{meta: &handler.VideoTaskMeta{
			Model:        "seedance-group",
			ProviderType: "seedance-relay",
			ProviderName: "Seedance",
		}}),
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		// 越权调用方：另一个 Key / 另一个用户，与任务毫无关系。
		c.Set("key_id", "key-attacker")
		c.Set("user_id", "user-attacker")
		c.Next()
	})
	router.GET("/v1/contents/generations/tasks/:task_id", legacy.GetTask)

	rec := doJSON(t, router, http.MethodGet, "/v1/contents/generations/tasks/vendor-secret-task", "", nil)
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusForbidden {
		t.Fatalf("既有 Seedance 查询链路似乎已加入归属校验（HTTP %d）。"+
			"请把本特性固化测试改为断言 404/403，并同步更新 docs/media-channels-integration-design.md §1.3 的缺陷记录。", rec.Code)
	}
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "victim.mp4",
		"既有链路当前会把他人任务的完整上游响应返回给任意有效 Key（已知缺陷，见设计文档 §1.3）")

	// 对照：新链路对**同一次越权访问**必须 404（不是 403）——403 会泄露 task_id 存在性。
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_victim",
		Status:       "completed",
		VendorTaskID: "vendor-secret-task",
		VideoURL:     "https://cdn.example.com/victim.mp4",
		APIKeyID:     "key-victim",
	})
	newRec := rig.retrieve(t, "task_victim")
	require.Equal(t, http.StatusNotFound, newRec.Code, newRec.Body.String())
	require.Equal(t, "not_found_error", errorCode(t, newRec))
	require.NotContains(t, newRec.Body.String(), "victim.mp4")
	require.NotContains(t, newRec.Body.String(), "vendor-secret-task", "对外响应永不回传上游 id")
}

// TestLegacyAndNewRoutesCoexist 确认两条链路的**路由形状**不冲突（同一个 gin 引擎同时注册）。
//
// 真实的并存由 router.go 在启动时一次性注册保证（gin 路径冲突会 panic）；
// 这里用最小引擎固化「不同前缀 + 不同参数名不影响注册」这一事实。
func TestLegacyAndNewRoutesCoexist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := handler.NewVideoHandler(
		&fakeLegacyVideoProvider{body: json.RawMessage(`{"status":"processing"}`)},
		nil,
		handler.WithTaskStore(&fakeLegacyTaskStore{}),
	)
	rig := newMediaTestRig(t, nil, nil)
	rig.adapter.queryStatus = &core.MediaTaskStatus{Status: core.MediaStatusInProgress}

	// 同一个引擎：先注册既有链路，再注册新链路（顺序与实际启动一致）。
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("key_id", testMediaKeyID)
		c.Set("user_id", testMediaUserID)
		c.Next()
	})
	router.GET("/v1/contents/generations/tasks/:task_id", legacy.GetTask)
	router.POST("/v1/videos", rig.handler.Create)
	router.GET("/v1/videos/:task_id", rig.handler.Retrieve)
	router.GET("/v1/videos/:task_id/content", rig.handler.Content)

	w := doJSON(t, router, http.MethodGet, "/v1/contents/generations/tasks/t1", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "processing")

	rig.seedMediaTask(newQueuedTask("task_coexist"))
	w2 := doJSON(t, router, http.MethodGet, "/v1/videos/task_coexist", "", nil)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
	require.Equal(t, "task_coexist", decodeJSON(t, w2)["id"])
}
