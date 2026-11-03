package kling_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/kling"
)

// 测试目的：provider type 常量与控制面 `provider.type` 逐字一致（注册表的键）。
func TestMediaProviderType(t *testing.T) {
	assert.Equal(t, "kling", kling.MediaProviderType)
}

// 测试目的：凭据缺失/非法时**必须**返回 error（而不是占位适配器）——占位凭证会让
// 请求真的打到上游并产生费用（providers.MediaAdapterBuilder 冻结约定）。
func TestNewMediaAdapter_RejectsBadCredentials(t *testing.T) {
	cases := []struct {
		name        string
		credentials string
	}{
		{name: "空凭据", credentials: ""},
		{name: "只有空白", credentials: "   "},
		{name: "非法 JSON", credentials: "accessKey|secretKey"},
		{name: "缺 secret_key", credentials: `{"access_key":"ak"}`},
		{name: "缺 access_key", credentials: `{"secret_key":"sk"}`},
		{name: "access_key 为空串", credentials: `{"access_key":"  ","secret_key":"sk"}`},
		{name: "JSON 数组", credentials: `["ak","sk"]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := kling.NewMediaAdapter(providers.MediaAdapterParams{
				Type:        kling.MediaProviderType,
				BaseURL:     "https://api.klingai.com",
				Credentials: tc.credentials,
			})
			require.Error(t, err)
			assert.Nil(t, adapter, "不得返回占位适配器")
		})
	}
}

// 测试目的：合法凭据（含多余键）可以构造；构造器不发任何网络请求（探活是 Batch 3 的能力，
// 且必须不产生生成费用）。
func TestNewMediaAdapter_AcceptsValidCredentials(t *testing.T) {
	fake := startFakeUpstream(t, nil)

	adapter := newAdapterWith(t, providers.MediaAdapterParams{
		Type:        kling.MediaProviderType,
		Name:        "kling-prod",
		BaseURL:     fake.URL,
		Credentials: `{"access_key":" ak ","secret_key":" sk ","region":"cn"}`,
		Models:      []string{"kling-v1"},
	})

	require.NotNil(t, adapter)
	assert.Zero(t, fake.count(), "构造适配器不得发任何网络请求")
}

// 测试目的：endpoint 为空 → 接受（用官方默认地址）；非 http/https 或缺主机名 → 构造失败。
//
// 为什么构造期就校验：endpoint 写错时宁可在建表阶段失败并打日志（渠道不进注册表，
// 请求得到明确 501），也不要让每次请求都在运行期变成 502。
func TestNewMediaAdapter_BaseURLValidation(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "空 → 官方默认", baseURL: ""},
		{name: "只有空白 → 官方默认", baseURL: "   "},
		{name: "https 绝对地址", baseURL: "https://api.klingai.com"},
		{name: "http 绝对地址（自建代理）", baseURL: "http://kling-proxy.internal:8080"},
		{name: "带路径前缀", baseURL: "https://proxy.example.com/kling"},
		{name: "缺 scheme", baseURL: "api.klingai.com", wantErr: true},
		{name: "非 http scheme", baseURL: "ftp://api.klingai.com", wantErr: true},
		{name: "缺主机名", baseURL: "https://", wantErr: true},
		{name: "非法 URL", baseURL: "https://%zz", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := kling.NewMediaAdapter(providers.MediaAdapterParams{
				Type:        kling.MediaProviderType,
				BaseURL:     tc.baseURL,
				Credentials: testCredentials,
			})
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, adapter)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, adapter)
		})
	}
}

// 测试目的：带路径前缀的 endpoint 拼接不得丢前缀（自建反代场景）。
func TestNewMediaAdapter_BaseURLWithPathPrefix(t *testing.T) {
	fake := startFakeUpstream(t, nil)

	adapter := newAdapterWith(t, providers.MediaAdapterParams{
		Type:        kling.MediaProviderType,
		BaseURL:     fake.URL + "/kling-proxy",
		Credentials: testCredentials,
	})

	_, err := adapter.Submit(t.Context(), submitRequest("text_to_video", nil, nil))
	require.NoError(t, err)
	assert.Equal(t, "/kling-proxy/v1/videos/text2video", fake.last(t).Path)
}
