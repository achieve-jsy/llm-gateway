package observability

import (
	"net/http"
	"strings"
)

// call_type 取值约定（供 call_logs.call_type 落库，列宽 varchar(64)）：
//
//	chat                   POST /v1/chat/completions
//	messages               POST /v1/messages
//	messages.count_tokens  POST /v1/messages/count_tokens
//	responses              POST /v1/responses
//	embeddings             POST /v1/embeddings
//	rerank                 POST /v1/rerank
//	models                 GET  /v1/models
//	model.info             GET  /v1/model/info
//	video.create           POST /v1/contents/generations/tasks（Seedance 原生创建）
//	video.get              GET  /v1/contents/generations/tasks/{task_id}（Seedance 原生查询）
//	image.generations      POST /v1/images/generations
//	image.edits            POST /v1/images/edits
//	video.submit           POST /v1/videos（OpenAI 兼容创建）
//	video.get              GET  /v1/videos/{task_id}（OpenAI 兼容查询，与 Seedance 查询同值）
//	video.content          GET  /v1/videos/{task_id}/content（产物下载）
//	video.asset.{Action}   POST /v1/assets/{Action}（如 video.asset.CreateAssetGroup）
//	video.asset            POST /v1/assets 下 Action 过长/多段时的兜底桶值
//
// 背景（姊妹调研 docs/video-integration-comparison.md §6）：视频调用此前只能靠 model 字符串
// 后缀区分（`组名:CreateTask` / `组名:GetTask` / `seedance-asset:{Action}`），看板只能靠
// `LIKE '%:CreateTask'` 这类约定匹配——命名约定一变就静默漏统计。归一化为独立字段后，
// 统计/筛选基于结构化数据，新增端点只需在下表登记。
//
// 判定原则：**不做猜测**。路径与方法不匹配、或路径不属于任何已登记端点时返回空串；
// 空值由读侧按 inbound_endpoint 兜底，也不会污染看板的分组统计。
const (
	callTypeChat               = "chat"
	callTypeMessages           = "messages"
	callTypeMessagesCountToken = "messages.count_tokens"
	callTypeResponses          = "responses"
	callTypeEmbeddings         = "embeddings"
	callTypeRerank             = "rerank"
	callTypeModels             = "models"
	callTypeModelInfo          = "model.info"
	callTypeImageGenerations   = "image.generations"
	callTypeImageEdits         = "image.edits"
	callTypeVideoCreate        = "video.create"
	callTypeVideoSubmit        = "video.submit"
	callTypeVideoGet           = "video.get"
	callTypeVideoContent       = "video.content"
	callTypeVideoAsset         = "video.asset"
	// callTypeVideoAssetPrefix 与具体 Action 拼接后即为素材操作的取值（video.asset.CreateAsset）
	callTypeVideoAssetPrefix = callTypeVideoAsset + "."
)

// maxCallTypeLen 与调用链落库列 call_logs.call_type 的列宽 varchar(64) 对齐。
//
// 素材 Action 直接取自 URL 路径段、长度不受调用方约束，而 varchar(64) 超长会让**整行**
// call_logs INSERT 失败（连带整条调用明细丢失）。因此拼接后超限必须退回短取值。
// 修改本常量前必须先确认列宽（迁移里 call_logs.call_type 的 varchar 长度）。
const maxCallTypeLen = 64

// 动态端点（含路径参数）的前缀常量。
const (
	videoTasksPath     = "/v1/contents/generations/tasks"
	videoAssetsPath    = "/v1/assets/"
	videosPath         = "/v1/videos"
	videoContentSuffix = "/content"
)

// exactCallTypes 登记"路径即唯一确定调用类型"的端点（等值匹配，方法必须一致）。
//
// 视频任务端点（/v1/contents/generations/tasks）同一路径按方法区分：本表只登记 POST（创建），
// 查询走 deriveDynamicCallType。方法不匹配（如 GET /v1/chat/completions）返回空串——
// 这类请求本来就不会命中路由，不应被算进任何操作类型。
var exactCallTypes = []struct {
	method   string
	path     string
	callType string
}{
	{http.MethodPost, "/v1/chat/completions", callTypeChat},
	{http.MethodPost, "/v1/messages", callTypeMessages},
	{http.MethodPost, "/v1/messages/count_tokens", callTypeMessagesCountToken},
	{http.MethodPost, "/v1/responses", callTypeResponses},
	{http.MethodPost, "/v1/embeddings", callTypeEmbeddings},
	{http.MethodPost, "/v1/rerank", callTypeRerank},
	{http.MethodGet, "/v1/models", callTypeModels},
	{http.MethodGet, "/v1/model/info", callTypeModelInfo},
	{http.MethodPost, "/v1/images/generations", callTypeImageGenerations},
	{http.MethodPost, "/v1/images/edits", callTypeImageEdits},
	{http.MethodPost, videoTasksPath, callTypeVideoCreate},
	{http.MethodPost, videosPath, callTypeVideoSubmit},
}

// DeriveCallType 按 HTTP 方法与入站路径归一化调用类型；无法识别时返回空串（不做猜测）。
// method 用 net/http 的大写方法常量比对，path 为请求路径（不含 query）。
//
// 导出原因：Q3 要求测试与业务代码物理分离（tests/gateway/observability），外部测试包只能访问
// 公开 API，故导出；同时为后续写库接线（collector 落 call_logs.call_type）提供唯一入口。
//
// 尾部斜杠会被容忍（/v1/messages/ 等价于 /v1/messages），路径匹配大小写敏感（与 Gin 一致）。
func DeriveCallType(method, path string) string {
	if path == "" {
		return ""
	}
	// 容忍尾部斜杠；根路径 "/" 本身保留（TrimRight 后为空会退化为 ""）
	trimmed := path
	if len(trimmed) > 1 {
		trimmed = strings.TrimRight(trimmed, "/")
		if trimmed == "" {
			trimmed = "/"
		}
	}

	for _, entry := range exactCallTypes {
		if entry.method == method && entry.path == trimmed {
			return entry.callType
		}
	}
	return deriveDynamicCallType(method, trimmed)
}

// deriveDynamicCallType 处理带路径参数的端点（素材 Action、视频任务 id）。
func deriveDynamicCallType(method, path string) string {
	// 素材库：POST /v1/assets/{Action}（Action 大小写敏感，原样透传给上游）
	if action, ok := strings.CutPrefix(path, videoAssetsPath); ok {
		if method != http.MethodPost {
			return ""
		}
		if action == "" {
			// 只有前缀、没有 Action：识别不出具体操作，返回空串而不是猜测
			return ""
		}
		if strings.Contains(action, "/") {
			// 多段不是合法端点（路由只有一个 :action 段），退回桶值
			return callTypeVideoAsset
		}
		if len(callTypeVideoAssetPrefix)+len(action) > maxCallTypeLen {
			// 列宽兜底：超长 Action 退回桶值，保证整行 call_logs 仍能写入
			return callTypeVideoAsset
		}
		return callTypeVideoAssetPrefix + action
	}

	// Seedance 原生任务查询：GET /v1/contents/generations/tasks/{task_id}
	if taskID, ok := strings.CutPrefix(path, videoTasksPath+"/"); ok {
		if method == http.MethodGet && isSingleSegment(taskID) {
			return callTypeVideoGet
		}
		return ""
	}

	// OpenAI 兼容视频：GET /v1/videos/{task_id} 与 GET /v1/videos/{task_id}/content
	if rest, ok := strings.CutPrefix(path, videosPath+"/"); ok {
		if method != http.MethodGet {
			return ""
		}
		if isSingleSegment(rest) {
			return callTypeVideoGet
		}
		if taskID, ok := strings.CutSuffix(rest, videoContentSuffix); ok && isSingleSegment(taskID) {
			return callTypeVideoContent
		}
		return ""
	}

	return ""
}

// isSingleSegment 判断 s 是否恰为单个非空路径段（不含 "/"，也不为空）。
func isSingleSegment(s string) bool {
	return s != "" && !strings.Contains(s, "/")
}
