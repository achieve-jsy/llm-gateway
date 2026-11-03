package vidu_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/vidu"
)

// 测试目的：provider type 常量与控制面 `provider.type` 逐字一致（注册表的键）。
func TestMediaProviderType(t *testing.T) {
	assert.Equal(t, "vidu", vidu.MediaProviderType)
}

// 测试目的：凭据缺失/非法时**必须**返回 error（而不是占位适配器）——占位凭证会让
// 请求真的打到上游并产生费用（providers.MediaAdapterBuilder 冻结约定）。
func TestNewMediaAdapter_RejectsBadCredentials(t *testing.T) {
	cases := []struct {
		name        string
		credentials string
	}{
		{name: "空凭据", credentials: ""},
		{name: "只有空白", credentials: "   \n "},
		{name: "非法 JSON（以 { 开头）", credentials: `{"api_key":`},
		{name: "JSON 缺 api_key", credentials: `{"apikey":"vda_x"}`},
		{name: "JSON api_key 为空串", credentials: `{"api_key":"   "}`},
		{name: "JSON 数组", credentials: `{"api_key":["vda_x"]}`},
		{name: "裸 Key 含空白（疑似贴了 JSON 片段）", credentials: `vda_x {"api_key":"vda_y"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := vidu.NewMediaAdapter(providers.MediaAdapterParams{
				Type:        vidu.MediaProviderType,
				BaseURL:     "https://api.vidu.cn",
				Credentials: tc.credentials,
			})
			require.Error(t, err)
			assert.Nil(t, adapter, "不得返回占位适配器")
		})
	}
}

// 测试目的：合法凭据**两种形态**都要接受。
//
//   - 裸 Key：生产实际形态（控制面 extractAPIKey 会把 {"api_key":...} 抽成纯字符串，
//     见 internal/controlplane/service/credential.go:8-21）。只认 JSON 会导致真机上
//     「凭据解析失败 → 渠道静默消失 → 全部 501」。
//   - JSON 对象：credential_schema 原始形态 / 直连构造。
//
// 同时断言构造器**不发任何网络请求**（探活是 Batch 3 的能力，且必须不产生生成费用）。
func TestNewMediaAdapter_AcceptsValidCredentials(t *testing.T) {
	cases := []struct {
		name        string
		credentials string
	}{
		{name: "裸 API Key（生产形态）", credentials: testCredentialsBare},
		{name: "裸 API Key 带首尾空白", credentials: "  " + testCredentialsBare + "  "},
		{name: "JSON 对象", credentials: testCredentialsJSON},
		{name: "JSON 对象含多余键", credentials: `{"api_key":"` + testAPIKey + `","region":"cn"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)

			adapter := newAdapterWith(t, providers.MediaAdapterParams{
				Type:        vidu.MediaProviderType,
				Name:        "vidu-prod",
				BaseURL:     fake.URL,
				Credentials: tc.credentials,
				Models:      []string{"viduq2"},
			})

			require.NotNil(t, adapter)
			assert.Zero(t, fake.count(), "构造适配器不得发任何网络请求")

			// 再真发一次请求：断言两种凭据形态解析出的是同一个 Key。
			_, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
			require.NoError(t, err)
			assert.Equal(t, "Token "+testAPIKey, fake.last(t).Header.Get("Authorization"),
				"两种凭据形态必须解析出同一个 Key")
		})
	}
}

// 测试目的：endpoint 为空 → 接受（用官方默认地址，不发请求所以不断言其取值）；
// 非 http/https 或缺主机名 → 构造失败。
//
// 为什么构造期就校验：endpoint 写错时宁可在建表阶段失败并打日志（渠道不进注册表，
// 请求得到明确 501），也不要让每次请求都在运行期变成 502。
func TestNewMediaAdapter_BaseURLValidation(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "空 → 官方默认（不发请求，故不断言其取值）", baseURL: ""},
		{name: "只有空白 → 官方默认", baseURL: "   "},
		{name: "https 绝对地址", baseURL: "https://api.vidu.cn"},
		{name: "http 绝对地址（自建代理）", baseURL: "http://vidu-proxy.internal:8080"},
		{name: "缺 scheme", baseURL: "api.vidu.cn", wantErr: true},
		{name: "非 http scheme", baseURL: "ftp://api.vidu.cn", wantErr: true},
		{name: "缺主机名", baseURL: "https://", wantErr: true},
		{name: "非法 URL", baseURL: "https://%zz", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := vidu.NewMediaAdapter(providers.MediaAdapterParams{
				Type:        vidu.MediaProviderType,
				BaseURL:     tc.baseURL,
				Credentials: testCredentialsBare,
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

// 测试目的：endpoint 里**已经带了 /ent/v2** 时不产生重复前缀。
//
// 运维按文档里的调用地址粘贴（https://api.vidu.cn/ent/v2）是很自然的误配，
// 而重复前缀只会得到一个上游 404；构造期归一后必须仍只拼一次。
func TestNewMediaAdapter_BaseURLWithAPIPrefix(t *testing.T) {
	fake := startFakeUpstream(t, nil)

	adapter := newAdapterWith(t, providers.MediaAdapterParams{
		Type:        vidu.MediaProviderType,
		BaseURL:     fake.URL + "/ent/v2",
		Credentials: testCredentialsBare,
	})

	_, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, "/ent/v2/text2video", fake.last(t).Path,
		"带 /ent/v2 的 endpoint 不得产生 /ent/v2/ent/v2 重复前缀")
}

// 测试目的：自建反代带自定义路径前缀时不得丢前缀（不能把非 /ent/v2 的路径也吃掉）。
func TestNewMediaAdapter_BaseURLWithCustomPathPrefix(t *testing.T) {
	fake := startFakeUpstream(t, nil)

	adapter := newAdapterWith(t, providers.MediaAdapterParams{
		Type:        vidu.MediaProviderType,
		BaseURL:     fake.URL + "/vidu-proxy",
		Credentials: testCredentialsBare,
	})

	_, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, "/vidu-proxy/ent/v2/text2video", fake.last(t).Path)
}

// 测试目的：适配器必须实现 core.MediaTaskProvider（编译期断言在包内，这里是行为层面的
// 冒烟：Submit/Query 都能被接口变量调用）。
func TestNewMediaAdapter_ImplementsMediaTaskProvider(t *testing.T) {
	fake := startFakeUpstream(t, nil)

	var provider core.MediaTaskProvider = newTestAdapter(t, fake)

	result, err := provider.Submit(t.Context(), textToVideoRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, upstreamTaskID, result.VendorTaskID)
}
