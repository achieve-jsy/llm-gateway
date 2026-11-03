package kling

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/example/llm-gateway/internal/core"
)

// 上游路径里的 action 段（Kling 只有两个视频端点）。
type upstreamAction string

const (
	upstreamActionText2Video  upstreamAction = "text2video"
	upstreamActionImage2Video upstreamAction = "image2video"
)

// vendorTaskIDSeparator 是 vendorTaskID 里「action 段」与「上游任务 id」的分隔符。
const vendorTaskIDSeparator = ":"

// actionPath 是 **显式** 映射表：core 归一动作 → 上游路径 action 段。
//
// ⚠️ 极性陷阱（本任务最容易做错的地方）：New API 的 `constant.TaskActionGenerate`
// 指的是 **image2video**，其余（TaskActionTextGenerate）才是 text2video
// （`relay/channel/task/kling/adaptor.go:122` 与 `:212`）。因此这里**只按
// core.MediaAction 查表**，绝不做「路径名与动作名相似度」之类的猜测。
//
// 为什么 reference_to_video 不在表里：New API 的 Kling 适配器只有这两个上游端点，
// Kling 的参考生视频走的是另一族端点与另一组参数。宁可在网关前置 400，
// 也不把用户的参考图静默降级成「首帧+尾帧」图生视频（既有费用又有内容偏差）。
var actionPath = map[core.MediaAction]upstreamAction{
	core.MediaActionTextToVideo:      upstreamActionText2Video,
	core.MediaActionImageToVideo:     upstreamActionImage2Video,
	core.MediaActionFirstTailToVideo: upstreamActionImage2Video,
}

// upstreamActionFor 查表；ok=false 表示该 core 动作在 Kling 上不被支持。
func upstreamActionFor(action core.MediaAction) (upstreamAction, bool) {
	path, ok := actionPath[action]
	return path, ok
}

// knownUpstreamAction 报告 action 段是否是本适配器会生成的两个取值之一。
func knownUpstreamAction(action upstreamAction) bool {
	return action == upstreamActionText2Video || action == upstreamActionImage2Video
}

// encodeVendorTaskID 把「上游 action 段」与「上游任务 id」编成一个可再次解析的
// vendorTaskID，形如 `image2video:890072193272676418`。
//
// ============================ 为什么要把 action 编进 vendorTaskID ============================
// core.MediaTaskProvider.Query 只拿到 vendorTaskID（接口不传 action），而 Kling 的查询路径
// `GET /v1/videos/{action}/{task_id}` 里的 action 段**必须与提交时一致**
// （New API 的 FetchTask 是靠调用方把 action 一起塞进 body 才绕开的：adaptor.go:203-213）。
// 因此适配器必须自己记住 action。两种方案的取舍：
//
//  1. 包内 `map[vendorTaskID]action` + 互斥锁：
//     - 内存随累计任务数**无界增长**（Query 之后没有任何回调能触发清理）；
//     - 更致命的是**跨进程不可用**：网关是蓝绿/多副本部署（deploy/blue-green 会同时跑
//     blue 与 green），任务在 A 实例 Submit、在 B 实例 Retrieve 时 map 必然 miss，
//     该任务从此永远轮询不到，只能等到超时被判 failed。
//  2. 把 action 编码进 vendorTaskID（**本实现的选择**）：完全无状态，
//     跨进程/重启/多副本/蓝绿切换都成立，不需要任何锁，也没有内存增长边界问题。
//
// 代价与边界：vendorTaskID 不再是逐字的上游 id。但它**只由本适配器生产、只由本适配器消费**，
// 控制面把它当不透明字符串持久化（media_tasks.vendor_task_id，varchar(191)），
// 且明文规定「禁止对外暴露」（internal/controlplane/model/video_task.go:121）。长度上
// "image2video:" 12 字节 + 上游 task id（Kling 是数字串），远低于 varchar(191) 上限
// （控制面在 internal/controlplane/handler/video_task_handler.go:298-301 会校验）。
func encodeVendorTaskID(action upstreamAction, upstreamTaskID string) string {
	return string(action) + vendorTaskIDSeparator + upstreamTaskID
}

// decodeVendorTaskID 解析 encodeVendorTaskID 的产物。
//
// 不是本适配器生成的 id（缺前缀 / 前缀未知 / 上游 id 为空）→ 返回 error。
// 这里**刻意不**包装 core.ErrMediaTaskNotFound：我们并不知道上游有没有这个任务，
// 谎报 not_found 会让调用方（C7 失败分类）立刻放弃轮询并把它计入失败分类；
// 返回普通协议错误则会按「可恢复」重试并在达到上限后带明确原因放弃。
func decodeVendorTaskID(vendorTaskID string) (upstreamAction, string, error) {
	prefix, rest, found := strings.Cut(strings.TrimSpace(vendorTaskID), vendorTaskIDSeparator)
	if !found {
		return "", "", fmt.Errorf("kling: vendor task id %q 缺少 action 前缀（不是本适配器生成的 id，无法确定查询路径）", vendorTaskID)
	}
	action := upstreamAction(prefix)
	if !knownUpstreamAction(action) {
		return "", "", fmt.Errorf("kling: vendor task id %q 的 action 前缀 %q 未知（只支持 %s/%s）",
			vendorTaskID, prefix, upstreamActionText2Video, upstreamActionImage2Video)
	}
	upstreamTaskID := strings.TrimSpace(rest)
	if upstreamTaskID == "" {
		return "", "", fmt.Errorf("kling: vendor task id %q 缺少上游任务 id 段", vendorTaskID)
	}
	return action, upstreamTaskID, nil
}

// ---------------------------------------------------------------------------
// 归一参数（Params）→ 上游请求体
// ---------------------------------------------------------------------------

const (
	// defaultMode 是 Kling mode 的缺省值（New API 与设计文档均以 std 兜底）。
	defaultMode = "std"
	// modeStd / modePro 是 Kling 支持的两个生成模式。
	modeStd = "std"
	modePro = "pro"
	// modelV2Master 不支持 std 模式：给 std 上游会直接报错，必须前置 400。
	modelV2Master = "kling-v2-master"
	// defaultModel 在网关未给出上游模型时兜底（与 New API 的 convertToRequestPayload 一致）。
	defaultModel = "kling-v1"
	// cfgScale 固定 0.5（逐字对齐 New API 的硬编码）：不从 Params 透传厂商私有字段。
	cfgScale = 0.5
	// defaultDurationSeconds / minDurationSeconds / maxDurationSeconds 是时长缺省与合法区间（秒）。
	defaultDurationSeconds = 5
	minDurationSeconds     = 1
	maxDurationSeconds     = 3600
)

// allowedParamKeyList 是 core.MediaSubmitRequest.Params 的**冻结键集**（7 个，字典序）。
//
// 来源：internal/gateway/handler/media_task_handler.go 的 mediaSubmitParams（唯一生产者）。
// 适配器必须 fail-closed：表外键返回 400，**绝不静默忽略**——静默忽略会让用户以为
// 参数生效了（例如把 negative_prompt / camera_control 吃掉），而费用照付。
var allowedParamKeyList = []string{
	"aspect_ratio",
	"duration",
	"images",
	"mode",
	"prompt",
	"resolution",
	"size",
}

// sizeAspectRatios 是 size → aspect_ratio 的显式映射表（逐字对齐 New API
// getAspectRatio 的已知取值，adaptor.go:277-288）。
//
// ⚠️ 与 New API 的唯一差别：**表外 size 返回空串**而不是 New API 的兜底 "1:1"。
// 瞎猜 1:1 会把竖屏需求静默变成方屏，留空让上游取默认更忠实（`Params["aspect_ratio"]`
// 非空时优先用它，见 resolveAspectRatio）。
var sizeAspectRatios = map[string]string{
	"1024x1024": "1:1",
	"512x512":   "1:1",
	"1280x720":  "16:9",
	"1920x1080": "16:9",
	"720x1280":  "9:16",
	"1080x1920": "9:16",
}

// klingRequestPayload 是上游 `POST /v1/videos/{text2video|image2video}` 的请求体。
//
// 字段名与 json tag **逐字对齐** New API 的 requestPayload（adaptor.go:56-72），
// 包括 `model_name` 与 `model` **同值且都要发**（部分上游只认其中一个）。
//
// 刻意**不**包含 New API payload 里的厂商私有字段：negative_prompt、static_mask、
// dynamic_masks、camera_control、callback_url、external_task_id。它们不在冻结的 7 键
// 参数集里，无法从归一参数安全推导，按 fail-closed 约定不透传（用户想传这些键会在
// validateParamKeys 拿到 400，而不是被无声丢掉）。
type klingRequestPayload struct {
	Prompt      string  `json:"prompt,omitempty"`
	Image       string  `json:"image,omitempty"`
	ImageTail   string  `json:"image_tail,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	Duration    string  `json:"duration,omitempty"`
	AspectRatio string  `json:"aspect_ratio,omitempty"`
	ModelName   string  `json:"model_name,omitempty"`
	Model       string  `json:"model,omitempty"`
	CfgScale    float64 `json:"cfg_scale,omitempty"`
}

// submitPlan 是一次 Submit 的翻译结果（动作路径 + 请求体）。
// 抽成返回值是为了让「参数 → 上游报文」这层纯函数可以被直接断言，不必解析 HTTP 报文。
type submitPlan struct {
	action  upstreamAction
	payload klingRequestPayload
}

// buildSubmitPlan 把归一请求翻译成上游动作与请求体，并完成所有前置校验。
//
// 校验顺序：表外参数 → action 支持性 → 参数类型 → action/图片数量一致性
// → mode 合法性 → v2-master 不支持 std（提前 400，不发给上游）。
func buildSubmitPlan(req *core.MediaSubmitRequest) (*submitPlan, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("kling: 提交请求为空", nil)
	}

	params := req.Params
	if err := validateParamKeys(params); err != nil {
		return nil, err
	}

	action, ok := upstreamActionFor(req.Action)
	if !ok {
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("kling: 不支持 action %q（支持 %s / %s / %s）",
				string(req.Action),
				string(core.MediaActionTextToVideo),
				string(core.MediaActionImageToVideo),
				string(core.MediaActionFirstTailToVideo)),
			nil)
	}

	prompt, err := paramString(params, "prompt")
	if err != nil {
		return nil, err
	}
	images, err := paramImages(params)
	if err != nil {
		return nil, err
	}
	mode, err := paramString(params, "mode")
	if err != nil {
		return nil, err
	}
	aspectRatio, err := resolveAspectRatio(params)
	if err != nil {
		return nil, err
	}
	duration, err := paramDurationSeconds(params)
	if err != nil {
		return nil, err
	}

	// action 与图片数量必须自洽：静默丢图（如 first_tail 只发首帧）会既收钱又出错的成片。
	if err := validateImageCount(req.Action, len(images)); err != nil {
		return nil, err
	}

	// mode：缺省 std；只接受 std/pro（其余值上游不认，提前 400 比让上游报错更可诊断）。
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = defaultMode
	}
	if mode != modeStd && mode != modePro {
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("kling: mode 只支持 %s 或 %s，收到 %q", modeStd, modePro, mode), nil)
	}

	// 上游模型：网关已完成「模型组 → 上游模型」改写；为空时兜底 kling-v1（同 New API）。
	model := strings.TrimSpace(req.UpstreamModel)
	if model == "" {
		model = defaultModel
	}
	// ⚠️ kling-v2-master 只支持 pro：给 std 上游会直接报错（且已扣调用配额），
	// 必须在网关**提前**返回 400，绝不发给上游。
	if model == modelV2Master && mode == modeStd {
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("kling: 模型 %s 不支持 mode=%s，请使用 mode=%s", modelV2Master, modeStd, modePro), nil)
	}

	payload := klingRequestPayload{
		Prompt:      prompt,
		Mode:        mode,
		Duration:    strconv.Itoa(duration),
		AspectRatio: aspectRatio,
		ModelName:   model,
		Model:       model,
		CfgScale:    cfgScale,
	}
	if len(images) > 0 {
		// first_tail_to_video：images[0]=首帧（image）、images[1]=尾帧（image_tail）。
		payload.Image = images[0]
	}
	if len(images) > 1 {
		payload.ImageTail = images[1]
	}

	return &submitPlan{action: action, payload: payload}, nil
}

// requiredImageCount 是每个 core 动作需要的图片张数。
//
// 这是「action ↔ 图片数量」这条不变量的显式表：文生 0 张、图生 1 张、首尾帧 2 张
// （首帧 → image、尾帧 → image_tail）。表外动作（如 reference_to_video）在
// upstreamActionFor 处已被拒，这里查不到即视为无需校验。
var requiredImageCount = map[core.MediaAction]int{
	core.MediaActionTextToVideo:      0,
	core.MediaActionImageToVideo:     1,
	core.MediaActionFirstTailToVideo: 2,
}

// validateImageCount 校验 action 与图片数量自洽。
//
// 注意这与 gateway handler 的 mediaTaskAction（按图片数量推 action）是同一条不变量的
// 两端：handler 保证「推出来的 action」自洽，适配器则保证「直接调进来的 action」也自洽。
func validateImageCount(action core.MediaAction, count int) error {
	want, ok := requiredImageCount[action]
	if !ok {
		return nil
	}
	if count != want {
		return core.NewInvalidRequestError(
			fmt.Sprintf("kling: action=%s 需要 %d 张图，收到 %d 张", string(action), want, count), nil)
	}
	return nil
}

// validateParamKeys 冻结键集校验（fail-closed）。
func validateParamKeys(params map[string]any) error {
	var unknown []string
	for key := range params {
		if !allowedParamKey(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	// 排序保证报错文案稳定（便于单测与线上排障）。
	sort.Strings(unknown)
	return core.NewInvalidRequestError(
		fmt.Sprintf("kling: 不支持的参数 %s（Kling 只接受 %s）",
			strings.Join(unknown, ", "), strings.Join(allowedParamKeyList, ", ")), nil)
}

// allowedParamKey 报告 key 是否在冻结键集内。
func allowedParamKey(key string) bool {
	for _, allowed := range allowedParamKeyList {
		if key == allowed {
			return true
		}
	}
	return false
}

// paramString 取字符串参数；缺省/nil 视为空串，类型不符返回 400。
// 不做 TrimSpace：prompt 等自由文本原样透传（枚举类取值在调用点各自 TrimSpace）。
func paramString(params map[string]any, key string) (string, error) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", core.NewInvalidRequestError(
			fmt.Sprintf("kling: 参数 %s 必须是字符串，收到 %T", key, raw), nil)
	}
	return value, nil
}

// paramImages 取归一图片列表（顺序即语义：首帧在前、尾帧在后）。
//
// 接受 []string（gateway handler 的唯一形态）与 []any（JSON 解码形态，便于单测与将来
// 的直连调用方）；其余类型一律 400。
func paramImages(params map[string]any) ([]string, error) {
	raw, ok := params["images"]
	if !ok || raw == nil {
		return nil, nil
	}

	var items []string
	switch typed := raw.(type) {
	case []string:
		items = typed
	case []any:
		items = make([]string, 0, len(typed))
		for i, item := range typed {
			value, ok := item.(string)
			if !ok {
				return nil, core.NewInvalidRequestError(
					fmt.Sprintf("kling: images[%d] 必须是字符串，收到 %T", i, item), nil)
			}
			items = append(items, value)
		}
	default:
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("kling: 参数 images 必须是字符串数组，收到 %T", raw), nil)
	}

	out := make([]string, 0, len(items))
	for i, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			return nil, core.NewInvalidRequestError(
				fmt.Sprintf("kling: images[%d] 为空", i), nil)
		}
		out = append(out, value)
	}
	return out, nil
}

// paramDurationSeconds 取时长（秒）。
//
// 归一约定：`Params["duration"]` 是 **int 秒**（gateway handler 的 mediaRequestDuration），
// 缺省或 <=0 表示「未提供」→ 用默认 5 秒；上游要的是**字符串**（如 "5"），由
// buildSubmitPlan 用 strconv.Itoa 转换。合法区间 1–3600，越界提前 400。
func paramDurationSeconds(params map[string]any) (int, error) {
	raw, ok := params["duration"]
	if !ok || raw == nil {
		return defaultDurationSeconds, nil
	}
	seconds, ok := asSeconds(raw)
	if !ok {
		return 0, core.NewInvalidRequestError(
			fmt.Sprintf("kling: duration 必须是整数秒，收到 %T", raw), nil)
	}
	if seconds <= 0 {
		// 与 handler 的 mediaRequestDuration 语义一致：0/负数等价于「未提供」。
		return defaultDurationSeconds, nil
	}
	if seconds > maxDurationSeconds {
		return 0, core.NewInvalidRequestError(
			fmt.Sprintf("kling: duration 必须在 %d–%d 秒之间，收到 %d",
				minDurationSeconds, maxDurationSeconds, seconds), nil)
	}
	return seconds, nil
}

// asSeconds 把参数值强制成整数秒，覆盖 JSON 解码会出现的几种数值形态与数字字符串。
func asSeconds(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		if i, err := value.Int64(); err == nil {
			return int(i), true
		}
		if f, err := value.Float64(); err == nil {
			return int(f), true
		}
		return 0, false
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

// resolveAspectRatio 解析宽高比。
//
// 优先级：
//  1. `Params["aspect_ratio"]` 非空则**原样透传**。它是冻结键集里的一等参数，gateway
//     handler 已把它归一到 {1:1,16:9,9:16}（mediaKnownAspectRatios）或保留用户显式取值；
//     忽略它等于把用户参数无声吃掉。
//  2. 否则由 `Params["size"]` 查 sizeAspectRatios 得到。
//  3. 都拿不到 → 空串，由上游取默认（**不**瞎猜 1:1）。
func resolveAspectRatio(params map[string]any) (string, error) {
	declared, err := paramString(params, "aspect_ratio")
	if err != nil {
		return "", err
	}
	if ratio := strings.TrimSpace(declared); ratio != "" {
		return ratio, nil
	}

	size, err := paramString(params, "size")
	if err != nil {
		return "", err
	}
	key := strings.ToLower(strings.TrimSpace(size))
	if key == "" {
		return "", nil
	}
	return sizeAspectRatios[key], nil
}

// ---------------------------------------------------------------------------
// 上游响应 → core 归一状态
// ---------------------------------------------------------------------------

// klingEnvelope 是提交与查询共用（且字段同形）的响应信封。
type klingEnvelope struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    klingTaskData `json:"data"`
}

// klingTaskData 对应 `data` 段。
type klingTaskData struct {
	TaskID        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	TaskStatusMsg string `json:"task_status_msg"`
	TaskResult    struct {
		Videos []klingVideo `json:"videos"`
	} `json:"task_result"`
	// FinalUnitDeduction 是上游实际扣减的用量（数字或字符串都出现过），仅终态有意义。
	// 用 RawMessage 保留原始形态，避免把字符串 "1.0" 解析失败后丢成 0。
	FinalUnitDeduction json.RawMessage `json:"final_unit_deduction"`
}

// klingVideo 是 data.task_result.videos[] 的元素。
type klingVideo struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Duration string `json:"duration"`
}

// upstreamTaskStatuses 是**上游真实返回**的状态词表（逐字对齐 New API
// ParseTaskResult adaptor.go:332-345，以及 kling 插件 plugin.js:292-294）。
//
// ⚠️ 陷阱：插件里那张 `NOT_START/SUBMITTED/QUEUED/IN_PROGRESS/SUCCESS/FAILURE` 表是
// **插件反向渲染**（内部态 → 对外 Kling 形状），**不是上游返回值**，方向别抄反。
var upstreamTaskStatuses = map[string]core.MediaStatus{
	"submitted":  core.MediaStatusQueued,
	"processing": core.MediaStatusInProgress,
	"succeed":    core.MediaStatusCompleted,
	"failed":     core.MediaStatusFailed,
}

// mapTaskStatus 把上游状态词归一到对外词表。
//
// 未知值（含大小写变体、空串）一律返回 error：这是 core/media_task.go 契约 2 的协议失败语义，
// 既不得当成 completed（会漏计费/给出错的成片），也不得静默当 in_progress（会无限轮询）。
// 刻意不做大小写折叠——折叠会把上游新增的未知词吞成已知词。
func mapTaskStatus(raw string) (core.MediaStatus, error) {
	status := strings.TrimSpace(raw)
	if status == "" {
		return "", fmt.Errorf("kling: 上游响应缺少 data.task_status（协议失败）")
	}
	mapped, ok := upstreamTaskStatuses[status]
	if !ok {
		return "", fmt.Errorf(
			"kling: 未知的上游任务状态 %q（协议失败：既不当成功也不无限轮询）", status)
	}
	return mapped, nil
}

// buildArtifacts 把 `data.task_result.videos[]` **全量**转成产物列表。
//
// New API 只取 videos[0]（adaptor.go:346-349、370-378）是它的缺陷：多产物模型会丢掉
// 其余成片。这里全取，并跳过 URL 为空的条目（缺 URL 的条目对调用方无意义）。
func buildArtifacts(videos []klingVideo) []core.MediaArtifact {
	artifacts := make([]core.MediaArtifact, 0, len(videos))
	for _, video := range videos {
		url := strings.TrimSpace(video.URL)
		if url == "" {
			continue
		}
		artifacts = append(artifacts, core.MediaArtifact{
			Kind:     core.MediaKindVideo,
			URL:      url,
			MimeType: videoMimeType(url),
		})
	}
	return artifacts
}

// videoMimeType 尽力从产物 URL 的扩展名推断 MIME 类型。
//
// 上游不给 mime 字段（New API 也没有），能确定就填、不确定留空——core.MediaArtifact
// 的注释允许 MimeType 为空（由网关按扩展名推断）。
func videoMimeType(rawURL string) string {
	path := rawURL
	if idx := strings.IndexAny(path, "?#"); idx >= 0 {
		path = path[:idx]
	}
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return ""
	}
	switch strings.ToLower(path[idx+1:]) {
	case "mp4":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	case "webm":
		return "video/webm"
	case "m4v":
		return "video/x-m4v"
	default:
		return ""
	}
}

// buildUsage 抽取终态用量（供控制面记账与对账）。
//
// 优先级：`data.final_unit_deduction`（上游**实际**扣减，最权威）与
// `videos[].duration`（用于 core.MediaCostInput.OutputSeconds 的兜底口径）。
// 一个都拿不到时返回 nil——**绝不伪造 0**（伪造 0 会被当成「已计价 0 元」）。
func buildUsage(data klingTaskData) map[string]any {
	usage := make(map[string]any, 2)

	if value, ok := decodeNumberOrString(data.FinalUnitDeduction); ok {
		usage["final_unit_deduction"] = value
	}
	for _, video := range data.TaskResult.Videos {
		duration := strings.TrimSpace(video.Duration)
		if duration == "" {
			continue
		}
		// 上游把 duration 放在 videos[] 里且是字符串；归一成数字便于记账口径复用
		// （core.MediaCostInput.OutputSeconds 就取 usage 里的 duration/duration_seconds）。
		usage["duration"] = numericOrText(duration)
		break
	}

	if len(usage) == 0 {
		return nil
	}
	return usage
}

// decodeNumberOrString 解析可能是数字也可能是字符串的原始 JSON 值。
//
// 归一规则：JSON 数字 → float64；数字字符串（如 `"1.0"`）→ float64；
// 无法解析的字符串原样保留（**绝不**改写成 0）；空串/null 视为「上游没给」。
func decodeNumberOrString(raw json.RawMessage) (any, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, false
	}

	var asNumber float64
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return asNumber, true
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		value := strings.TrimSpace(asString)
		if value == "" {
			return nil, false
		}
		return numericOrText(value), true
	}
	return nil, false
}

// numericOrText 能解析成数字就返回 float64，否则原样返回字符串。
func numericOrText(value string) any {
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number
	}
	return value
}
