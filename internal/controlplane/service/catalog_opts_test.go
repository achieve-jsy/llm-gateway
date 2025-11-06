package service

// catalog_opts_test.go ─ TDD: CATALOG_REMOTE_URL 环境变量驱动的配置行为
//
// 问题背景：
//   默认远程 URL 指向 raw.githubusercontent.com，部署服务器（192.0.2.10）
//   无法访问 GitHub，导致每次 SyncFromRemote 都走 bundled fallback，
//   catalog 数据永远停在内置快照版本，无法自动跟随 LiteLLM 上游更新。
//
// 修复目标：
//   1. 支持通过 CATALOG_REMOTE_URL 环境变量覆盖默认 URL；
//   2. 支持通过 CATALOG_HTTP_TIMEOUT 覆盖默认超时（默认 15s，内网镜像可调短）；
//   3. CatalogSyncOpts.RemoteURL 为空时才读环境变量（代码显式传入优先）；
//   4. 环境变量也为空时，回退到原有 defaultLiteLLMURL。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCatalogOpts_EnvURL_OverridesDefault 当 CATALOG_REMOTE_URL 设置时，
// SyncFromRemote 应访问该 URL 而非默认的 GitHub URL。
//
// 在无法访问 GitHub 的内网环境下，设置这个变量指向内网镜像或代理即可解决问题。
func TestCatalogOpts_EnvURL_OverridesDefault(t *testing.T) {
	visited := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visited <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	t.Setenv("CATALOG_REMOTE_URL", ts.URL+"/custom-path")

	// 关键：传空 RemoteURL，让实现去读环境变量
	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{
		HTTPTimeout: 2 * time.Second,
	})

	_, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err)

	select {
	case path := <-visited:
		assert.Equal(t, "/custom-path", path,
			"CATALOG_REMOTE_URL 应覆盖默认的 GitHub URL")
	case <-time.After(3 * time.Second):
		t.Fatal("SyncFromRemote 没有访问 CATALOG_REMOTE_URL 指定的地址")
	}
}

// TestCatalogOpts_ExplicitURL_TakesPrecedenceOverEnv 代码里显式传入 RemoteURL 时，
// 不应被环境变量覆盖（显式 > 环境变量）。
func TestCatalogOpts_ExplicitURL_TakesPrecedenceOverEnv(t *testing.T) {
	explicitVisited := make(chan struct{}, 1)
	envVisited := make(chan struct{}, 1)

	explicit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		explicitVisited <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer explicit.Close()

	envServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envVisited <- struct{}{}
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer envServer.Close()

	t.Setenv("CATALOG_REMOTE_URL", envServer.URL)

	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{
		RemoteURL:   explicit.URL, // 显式传入，应优先
		HTTPTimeout: 2 * time.Second,
	})

	_, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err)

	select {
	case <-explicitVisited:
		// 正确
	case <-time.After(3 * time.Second):
		t.Fatal("没有访问显式指定的 URL")
	}

	select {
	case <-envVisited:
		t.Fatal("显式 URL 存在时不应访问 CATALOG_REMOTE_URL 环境变量指定的地址")
	default:
		// 正确：env server 没被访问
	}
}

// TestCatalogOpts_EnvTimeout_OverridesDefault CATALOG_HTTP_TIMEOUT 覆盖默认 15s 超时。
//
// 内网镜像访问很快，可以把超时调短（如 5s），减少卡在 GitHub 上的等待时间。
func TestCatalogOpts_EnvTimeout_OverridesDefault(t *testing.T) {
	// 用一个会延迟 200ms 的 server 验证超时参数确实被采用
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	t.Setenv("CATALOG_REMOTE_URL", ts.URL)
	t.Setenv("CATALOG_HTTP_TIMEOUT", "5s") // 5s > 200ms，应该成功

	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{})
	_, err := svc.SyncFromRemote(context.Background())
	require.NoError(t, err, "CATALOG_HTTP_TIMEOUT=5s 应允许 200ms 的响应通过")
}

// TestCatalogOpts_EnvTimeout_ShortEnoughCausesTimeout 超时设置比响应时间短时，
// 应触发 timeout 错误（回退 bundled）。
func TestCatalogOpts_EnvTimeout_ShortEnoughCausesTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // 故意慢
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalRemoteJSON))
	}))
	defer ts.Close()

	t.Setenv("CATALOG_REMOTE_URL", ts.URL)
	t.Setenv("CATALOG_HTTP_TIMEOUT", "100ms") // 比 500ms 短，必须超时

	// 无 bundled → 彻底失败，让我们能看到 error
	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{
		BundledBytes: []byte{},
	})
	_, err := svc.SyncFromRemote(context.Background())
	require.Error(t, err, "超时应触发错误（无 bundled fallback 时彻底失败）")
}

// TestCatalogOpts_NoEnv_FallsBackToDefaultURL 环境变量未设时，使用默认 GitHub URL。
// 这个 case 不发真实网络请求——只验证 URL 被正确设置到 opts 里。
func TestCatalogOpts_NoEnv_FallsBackToDefaultURL(t *testing.T) {
	// 确保测试隔离
	t.Setenv("CATALOG_REMOTE_URL", "")
	t.Setenv("CATALOG_HTTP_TIMEOUT", "")

	svc := NewCatalogSyncService(newFakeCatalogRepo(), nil, CatalogSyncOpts{})
	assert.Equal(t, defaultLiteLLMURL, svc.opts.RemoteURL,
		"无环境变量时 RemoteURL 应等于 defaultLiteLLMURL")
}
