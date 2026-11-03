package observability_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/stretchr/testify/require"
)

// call_logs.call_type 的列宽（varchar(64)）；测试里显式写死，用于钉住与落库列宽的契约，
// 避免实现侧把 maxCallTypeLen 悄悄放大到超出列宽（超长会让整行 call_logs INSERT 失败）。
const callTypeColumnWidth = 64

// videoAssetPrefix 与实现约定的素材取值前缀（video.asset.{Action}）。
const videoAssetPrefix = "video.asset."

// TestDeriveCallType 逐条断言「端点 × 方法 → call_type 取值」。
//
// 测什么：现有端点（chat/messages/count_tokens/responses/embeddings/rerank/models/model_info）、
// Seedance 视频与素材端点、本次新增的媒体端点（images/videos）的归一化结果；
// 同时覆盖"路径对但方法不对"、尾部斜杠、超长素材 Action、未知路径四类边界。
// 怎么测：纯函数表驱动，逐条比对返回值。
// 期望：命中端点返回约定取值；方法不匹配 / 路径不认识一律返回空串（不猜测）。
func TestDeriveCallType(t *testing.T) {
	// 恰好填满列宽的最长 Action：64 - len("video.asset.") = 52
	maxAssetAction := strings.Repeat("A", callTypeColumnWidth-len(videoAssetPrefix))
	// 再长 1 个字节就越界，必须退回桶值 video.asset
	tooLongAssetAction := strings.Repeat("B", callTypeColumnWidth-len(videoAssetPrefix)+1)
	// 非 ASCII Action：字节长度按保守口径判断（20 个汉字 = 60 字节，拼接后 72 > 64）
	nonASCIIAssetAction := strings.Repeat("写", 20)

	tests := []struct {
		name        string
		description string
		method      string
		path        string
		want        string
	}{
		// ── 现有端点（正确方法） ────────────────────────────────────────────────
		{
			name: "chat 端点", description: "POST /v1/chat/completions 应归一为 chat",
			method: http.MethodPost, path: "/v1/chat/completions", want: "chat",
		},
		{
			name: "messages 端点", description: "POST /v1/messages 应归一为 messages",
			method: http.MethodPost, path: "/v1/messages", want: "messages",
		},
		{
			name: "messages count_tokens 端点", description: "POST /v1/messages/count_tokens 应归一为 messages.count_tokens（不是 messages）",
			method: http.MethodPost, path: "/v1/messages/count_tokens", want: "messages.count_tokens",
		},
		{
			name: "responses 端点", description: "POST /v1/responses 应归一为 responses",
			method: http.MethodPost, path: "/v1/responses", want: "responses",
		},
		{
			name: "embeddings 端点", description: "POST /v1/embeddings 应归一为 embeddings",
			method: http.MethodPost, path: "/v1/embeddings", want: "embeddings",
		},
		{
			name: "rerank 端点", description: "POST /v1/rerank 应归一为 rerank",
			method: http.MethodPost, path: "/v1/rerank", want: "rerank",
		},
		{
			name: "models 端点", description: "GET /v1/models 应归一为 models",
			method: http.MethodGet, path: "/v1/models", want: "models",
		},
		{
			name: "model_info 端点", description: "GET /v1/model/info 应归一为 model_info",
			method: http.MethodGet, path: "/v1/model/info", want: "model_info",
		},

		// ── Seedance 现有视频 / 素材端点 ───────────────────────────────────────
		{
			name: "Seedance 创建任务", description: "POST /v1/contents/generations/tasks 应归一为 video.create",
			method: http.MethodPost, path: "/v1/contents/generations/tasks", want: "video.create",
		},
		{
			name: "Seedance 查询任务", description: "GET /v1/contents/generations/tasks/{task_id} 应归一为 video.get",
			method: http.MethodGet, path: "/v1/contents/generations/tasks/cgt-20261014-abc123", want: "video.get",
		},
		{
			name: "素材动作 CreateAssetGroup", description: "POST /v1/assets/{Action} 应归一为 video.asset.{Action}",
			method: http.MethodPost, path: "/v1/assets/CreateAssetGroup", want: "video.asset.CreateAssetGroup",
		},
		{
			name: "素材动作 GetAsset", description: "素材 Action 大小写敏感、原样保留（GetAsset 不等于 getasset）",
			method: http.MethodPost, path: "/v1/assets/GetAsset", want: "video.asset.GetAsset",
		},
		{
			name: "素材 Action 恰好填满列宽", description: "拼接后正好 64 字节（列宽边界内）应写入具体 Action",
			method: http.MethodPost, path: "/v1/assets/" + maxAssetAction, want: videoAssetPrefix + maxAssetAction,
		},
		{
			name: "素材 Action 超长退回桶值", description: "拼接后 65 字节超过 varchar(64)，必须退回 video.asset 以保证整行仍能落库",
			method: http.MethodPost, path: "/v1/assets/" + tooLongAssetAction, want: "video.asset",
		},
		{
			name: "素材 Action 非 ASCII 保守兜底", description: "非 ASCII Action 按字节长度判断，超限同样退回 video.asset（避免列宽溢出）",
			method: http.MethodPost, path: "/v1/assets/" + nonASCIIAssetAction, want: "video.asset",
		},
		{
			name: "素材 Action 多段退回桶值", description: "/v1/assets/a/b 不是合法端点（路由只有单个 :action 段），退回 video.asset 桶值",
			method: http.MethodPost, path: "/v1/assets/GetAsset/extra", want: "video.asset",
		},

		// ── 本次新增的媒体端点 ────────────────────────────────────────────────
		{
			name: "图像生成端点", description: "POST /v1/images/generations 应归一为 image.generations",
			method: http.MethodPost, path: "/v1/images/generations", want: "image.generations",
		},
		{
			name: "图像编辑端点", description: "POST /v1/images/edits 应归一为 image.edits",
			method: http.MethodPost, path: "/v1/images/edits", want: "image.edits",
		},
		{
			name: "视频提交端点", description: "POST /v1/videos 应归一为 video.submit",
			method: http.MethodPost, path: "/v1/videos", want: "video.submit",
		},
		{
			name: "视频查询端点", description: "GET /v1/videos/{task_id} 应归一为 video.get（与 Seedance 查询同值，看板可合并统计）",
			method: http.MethodGet, path: "/v1/videos/video_abc123", want: "video.get",
		},
		{
			name: "视频产物下载端点", description: "GET /v1/videos/{task_id}/content 应归一为 video.content",
			method: http.MethodGet, path: "/v1/videos/video_abc123/content", want: "video.content",
		},

		// ── 尾部斜杠容忍 ─────────────────────────────────────────────────────
		{
			name: "尾部斜杠 chat", description: "POST /v1/chat/completions/ 与不带斜杠等价，应仍是 chat",
			method: http.MethodPost, path: "/v1/chat/completions/", want: "chat",
		},
		{
			name: "多尾部斜杠 messages", description: "连续多个尾部斜杠也应被容忍",
			method: http.MethodPost, path: "/v1/messages//", want: "messages",
		},
		{
			name: "尾部斜杠 model_info", description: "GET /v1/model/info/ 应仍是 model_info",
			method: http.MethodGet, path: "/v1/model/info/", want: "model_info",
		},
		{
			name: "尾部斜杠 视频提交", description: "POST /v1/videos/ 应仍是 video.submit",
			method: http.MethodPost, path: "/v1/videos/", want: "video.submit",
		},
		{
			name: "尾部斜杠 视频查询", description: "GET /v1/videos/{task_id}/ 应仍是 video.get",
			method: http.MethodGet, path: "/v1/videos/video_abc123/", want: "video.get",
		},
		{
			name: "尾部斜杠 视频产物", description: "GET /v1/videos/{task_id}/content/ 应仍是 video.content",
			method: http.MethodGet, path: "/v1/videos/video_abc123/content/", want: "video.content",
		},
		{
			name: "尾部斜杠 素材动作", description: "POST /v1/assets/GetAsset/ 应仍是 video.asset.GetAsset（Action 不被斜杠污染）",
			method: http.MethodPost, path: "/v1/assets/GetAsset/", want: "video.asset.GetAsset",
		},
		{
			name: "尾部斜杠 Seedance 查询", description: "GET /v1/contents/generations/tasks/cgt-1/ 应仍是 video.get",
			method: http.MethodGet, path: "/v1/contents/generations/tasks/cgt-1/", want: "video.get",
		},

		// ── 路径对但方法不对（返回空串，不猜测） ──────────────────────────────
		{
			name: "chat 方法不对", description: "GET /v1/chat/completions 不命中路由，不应算作 chat",
			method: http.MethodGet, path: "/v1/chat/completions", want: "",
		},
		{
			name: "models 方法不对", description: "POST /v1/models 不命中路由，不应算作 models",
			method: http.MethodPost, path: "/v1/models", want: "",
		},
		{
			name: "model_info 方法不对", description: "POST /v1/model/info 不命中路由，不应算作 model_info",
			method: http.MethodPost, path: "/v1/model/info", want: "",
		},
		{
			name: "embeddings 方法不对", description: "GET /v1/embeddings 不命中路由，不应算作 embeddings",
			method: http.MethodGet, path: "/v1/embeddings", want: "",
		},
		{
			name: "图像端点方法不对", description: "GET /v1/images/generations 不命中路由，不应算作 image.generations",
			method: http.MethodGet, path: "/v1/images/generations", want: "",
		},
		{
			name: "图像编辑方法不对", description: "GET /v1/images/edits 不命中路由，不应算作 image.edits",
			method: http.MethodGet, path: "/v1/images/edits", want: "",
		},
		{
			name: "视频列表未开放", description: "GET /v1/videos 是列表端点（本次未实现），不应被猜成 video.submit/video.get",
			method: http.MethodGet, path: "/v1/videos", want: "",
		},
		{
			name: "视频查询方法不对", description: "POST /v1/videos/{task_id} 不命中路由，不应算作 video.get",
			method: http.MethodPost, path: "/v1/videos/video_abc123", want: "",
		},
		{
			name: "视频产物方法不对", description: "POST /v1/videos/{task_id}/content 不命中路由，不应算作 video.content",
			method: http.MethodPost, path: "/v1/videos/video_abc123/content", want: "",
		},
		{
			name: "Seedance 查询方法不对", description: "POST /v1/contents/generations/tasks/{task_id} 不应算作 video.get",
			method: http.MethodPost, path: "/v1/contents/generations/tasks/cgt-1", want: "",
		},
		{
			name: "Seedance 列表未开放", description: "GET /v1/contents/generations/tasks（无 task_id）未实现，不应猜成 video.get",
			method: http.MethodGet, path: "/v1/contents/generations/tasks", want: "",
		},
		{
			name: "素材端点方法不对", description: "素材路由只注册 POST，GET /v1/assets/GetAsset 不应算作素材操作",
			method: http.MethodGet, path: "/v1/assets/GetAsset", want: "",
		},
		{
			name: "视频提交方法不对", description: "PUT /v1/videos 不命中路由，不应算作 video.submit",
			method: http.MethodPut, path: "/v1/videos", want: "",
		},

		// ── 未知路径 / 前缀相似路径（返回空串） ─────────────────────────────
		{
			name: "未知路径", description: "未登记的 /v1/unknown 返回空串",
			method: http.MethodPost, path: "/v1/unknown", want: "",
		},
		{
			name: "透传端点未纳入", description: "厂商透传 /v1/p/:provider/*path 本次不归类，返回空串",
			method: http.MethodPost, path: "/v1/p/openai/v1/chat/completions", want: "",
		},
		{
			name: "视频多段子路径", description: "/v1/videos/{id}/other 不是已实现端点，返回空串（不猜成 video.content）",
			method: http.MethodGet, path: "/v1/videos/video_abc123/other", want: "",
		},
		{
			name: "Seedance 任务多段子路径", description: "/v1/contents/generations/tasks/a/b 不是合法 task_id，返回空串",
			method: http.MethodGet, path: "/v1/contents/generations/tasks/a/b", want: "",
		},
		{
			name: "视频内容路径缺 task_id", description: "GET /v1/videos//content 缺 task_id，返回空串",
			method: http.MethodGet, path: "/v1/videos//content", want: "",
		},
		{
			name: "素材缺 Action", description: "/v1/assets（无 Action）识别不出具体操作，返回空串",
			method: http.MethodPost, path: "/v1/assets", want: "",
		},
		{
			name: "素材只有斜杠", description: "/v1/assets/ 只有前缀没有 Action，返回空串而不是猜成桶值",
			method: http.MethodPost, path: "/v1/assets/", want: "",
		},
		{
			name: "前缀相似路径 videosX", description: "/v1/videosX 不应被当成 /v1/videos 前缀",
			method: http.MethodPost, path: "/v1/videosX", want: "",
		},
		{
			name: "前缀相似路径 assetsX", description: "/v1/assetsX/Action 不应被当成素材端点",
			method: http.MethodPost, path: "/v1/assetsX/GetAsset", want: "",
		},
		{
			name: "前缀相似路径 modelsX", description: "/v1/modelsX 不应被当成 models 端点",
			method: http.MethodGet, path: "/v1/modelsX", want: "",
		},
		{
			name: "chat 多余路径段", description: "/v1/chat/completions/extra 不是已登记端点，返回空串",
			method: http.MethodPost, path: "/v1/chat/completions/extra", want: "",
		},
		{
			name: "大小写敏感", description: "路径匹配大小写敏感（与 Gin 一致），/V1/CHAT/COMPLETIONS 返回空串",
			method: http.MethodPost, path: "/V1/CHAT/COMPLETIONS", want: "",
		},
		{
			name: "健康检查路径", description: "/health 不在 /v1 下，返回空串",
			method: http.MethodGet, path: "/health", want: "",
		},
		{
			name: "根路径", description: "根路径 \"/\" 返回空串",
			method: http.MethodGet, path: "/", want: "",
		},
		{
			name: "空路径", description: "路径为空返回空串（防御非法调用）",
			method: http.MethodPost, path: "", want: "",
		},
		{
			name: "空方法", description: "方法为空即不匹配任何端点，返回空串",
			method: "", path: "/v1/chat/completions", want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := observability.DeriveCallType(tt.method, tt.path)
			require.Equal(t, tt.want, got, tt.description)
		})
	}
}

// TestDeriveCallType_NeverExceedsColumnWidth 断言任何输入下返回值都不超过 call_logs.call_type
// 的列宽 varchar(64)——超长会让整行 call_logs INSERT 失败（连带丢失调用明细）。
//
// 测什么：把所有动态端点入口喂给「超长 Action / 超长 task_id / 多段路径」，
// 并对每种 HTTP 方法求值，检查返回值长度。
// 期望：长度 ≤ 64 字节（按字节判断比 varchar 的字符计数更严格，天然安全）。
func TestDeriveCallType_NeverExceedsColumnWidth(t *testing.T) {
	hugeSegment := strings.Repeat("x", 4096)
	paths := []string{
		"/v1/assets/" + hugeSegment,
		"/v1/assets/" + hugeSegment + "/extra",
		"/v1/assets/" + strings.Repeat("写", 200),
		"/v1/contents/generations/tasks/" + hugeSegment,
		"/v1/videos/" + hugeSegment,
		"/v1/videos/" + hugeSegment + "/content",
		"/v1/videos/" + hugeSegment + "/" + hugeSegment,
		"/v1/chat/completions/" + hugeSegment,
	}
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, ""}

	for _, path := range paths {
		for _, method := range methods {
			got := observability.DeriveCallType(method, path)
			require.LessOrEqual(t, len(got), callTypeColumnWidth,
				"method=%s path(len=%d) 的 call_type 取值超长，会让 call_logs 整行写入失败", method, len(path))
		}
	}
}
