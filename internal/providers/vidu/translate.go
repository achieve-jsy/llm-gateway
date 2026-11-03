package vidu

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/example/llm-gateway/internal/core"
)

// 本文件是 Vidu 的**纯函数翻译层**：action 判定、7 键参数 → 上游报文、上游状态/产物/用量 →
// core 归一形状。不含任何 HTTP 或网络依赖，因此可以在测试里直接断言（见 tests/providers/vidu）。
//
// 上游契约来源：本机 New API 的 Go 实现 `relay/channel/task/vidu/adaptor.go`
// （与设计文档 docs/media-channels-integration-design.md §3.2 对拍一致）：
//
//   - 提交：`POST {base}/ent/v2/{text2video|img2video|start-end2video|reference2video}`
//   - 查询：`GET {base}/ent/v2/tasks/{task_id}/creations`
//   - 鉴权：`Authorization: Token <apiKey>`
//   - 请求体：`{model, images[], prompt, duration(int), resolution, movement_amplitude:"auto"}`
//   - 响应体（提交与查询**同形**、均为顶层字段）：`{task_id, state, err_code, credits?, creations[]}`
//   - 状态枚举：`created|queueing|processing|success|failed`

// ---------------------------------------------------------------------------
// action 判定：图片张数 → 上游端点
// ---------------------------------------------------------------------------

// upstreamAction 是 Vidu 四个提交端点共用的路径尾段。
type upstreamAction string

const (
	upstreamActionText2Video      upstreamAction = "text2video"
	upstreamActionImg2Video       upstreamAction = "img2video"
	upstreamActionStartEnd2Video  upstreamAction = "start-end2video"
	upstreamActionReference2Video upstreamAction = "reference2video"
)

// maxReferenceImages 是判定表里参考生视频区间的上界。
//
// ⚠️ 它**不是**上游的图片张数上限（各模型上限需真上游核对，本轮无凭据无法核实），
// 只是「>2 张」这个开区间的右端占位，取 int 上界表示「不设本地上限」：
// 多传的图片由上游校验，适配器**不发明**未经核实的上限。
const maxReferenceImages = math.MaxInt

// imageCountActionRule 是判定表的一行：`[minImages, maxImages]` 闭区间内 → 某个动作。
type imageCountActionRule struct {
	minImages  int
	maxImages  int
	action     upstreamAction
	coreAction core.MediaAction
}

// imageCountActionRules 是 **action 判定表**（本适配器的核心规则，逐条对齐 D3 任务书）：
//
//	0 张 → text2video（text_to_video）
//	1 张 → img2video（image_to_video）
//	2 张 → start-end2video（first_tail_to_video）
//	>2 张 → reference2video（reference_to_video）
//
// 为什么写成表而不是 if/switch 链：这是**唯一**决定上游端点的规则，端点选错会既扣费又出错片
// （例如把 2 张图当首帧丢掉尾帧）；表驱动让它可以被单测逐行遍历，也不会出现 off-by-one。
// 与 New API 的实现一致（`adaptor.go:94-107` 的 `len(req.Images) == 2` / `> 2` 判定）。
var imageCountActionRules = []imageCountActionRule{
	{0, 0, upstreamActionText2Video, core.MediaActionTextToVideo},
	{1, 1, upstreamActionImg2Video, core.MediaActionImageToVideo},
	{2, 2, upstreamActionStartEnd2Video, core.MediaActionFirstTailToVideo},
	{3, maxReferenceImages, upstreamActionReference2Video, core.MediaActionReferenceToVideo},
}

// planActionForImageCount 按图片张数查出上游端点与对应的 core 归一动作。
//
// ok=false 只可能来自**负数张数**这种本地不可能出现的输入（len() 恒 >= 0）；
// 保留它是为了让调用方不必假设表一定命中，也让将来改表时不影响编译。
func planActionForImageCount(count int) (upstreamAction, core.MediaAction, bool) {
	for _, rule := range imageCountActionRules {
		if count >= rule.minImages && count <= rule.maxImages {
			return rule.action, rule.coreAction, true
		}
	}
	return "", "", false
}

// ---------------------------------------------------------------------------
// vendorTaskID：Vidu **不需要**像 Kling 那样编码 action
// ---------------------------------------------------------------------------

// normalizeVendorTaskID 归一上游任务 id。
//
// ⚠️ **为什么 Vidu 的 vendorTaskID 就是逐字的上游 id（与 Kling 的关键差异）**：
//
// Kling 的查询路径是 `GET /v1/videos/{action}/{task_id}`，action 段**必须与提交时一致**，
// 而 `core.MediaTaskProvider.Query` 只拿到 vendorTaskID（接口不传 action），所以 Kling 必须把
// action 编进 vendorTaskID（见 internal/providers/kling/translate.go 的 encodeVendorTaskID：
// 它放弃了包内 map 方案，因为内存无界增长且跨副本/蓝绿切换必然 miss）。
//
// Vidu 的查询路径是 `GET /ent/v2/tasks/{task_id}/creations`——**路径里没有 action 段**，
// 上游用 task_id 就能唯一定位任务（New API 的 FetchTask 也只把 task_id 拼进路径，
// `relay/channel/task/vidu/adaptor.go:192-198`）。因此：
//
//  1. 编码没有信息可承载：加一层 `img2video:` 前缀只会把一个「逐字等于上游 id」的字段
//     变成一个需要解析的自造格式，纯粹增加出错面；
//  2. 编码反而有害：`media_tasks.vendor_task_id` 是排障/对账时与上游控制台对照的字段
//     （控制面明文规定禁止对外暴露，internal/controlplane/model/video_task.go:121），
//     逐字 id 能直接粘贴到上游查，带前缀则不能；
//  3. 两种渠道的结论相同（都必须是「无状态、跨副本可查」），但**代价不同**：
//     Kling 用「自造格式」换来无状态，Vidu 用「逐字 id」天然无状态。这也是为什么本文件
//     没有 encode/decodeVendorTaskID 这一对函数——不是漏写。
//
// 空 id / 纯空白 → error。**刻意不**包装 core.ErrMediaTaskNotFound：我们并不知道上游有没有
// 这个任务（可能是持久化被写坏），谎报 not_found 会让调用方立刻停止轮询并计入失败分类；
// 返回普通协议错误则按可恢复处理并带明确原因放弃。
func normalizeVendorTaskID(vendorTaskID string) (string, error) {
	trimmed := strings.TrimSpace(vendorTaskID)
	if trimmed == "" {
		return "", fmt.Errorf("vidu: vendor task id 为空（无法定位上游任务）")
	}
	return trimmed, nil
}

// ---------------------------------------------------------------------------
// 归一参数（Params）→ 上游请求体
// ---------------------------------------------------------------------------

const (
	// defaultBaseURL 在 media_adapter.go（provider.endpoint 为空时的兜底地址）。
	// defaultModel 是网关未给出上游模型时的兜底（与 New API 一致：
	// `adaptor.go:229` 的 `taskcommon.DefaultString(info.UpstreamModelName, "viduq1")`）。
	defaultModel = "viduq1"
	// movementAmplitude 固定 "auto"：**不接受外部传入**（它不在 7 键冻结参数集里），
	// 与 New API 的硬编码一致（`adaptor.go:235`）。
	movementAmplitude = "auto"
	// maxImageBytes 是单张图片（data URL 解码后）的字节上限 = 15 MiB。
	//
	// 逐字取自设计文档 §3.2 的对外 multipart 约定（文件字段仅 input_reference、
	// 最多 1 个、dataUrl 编码、上限 15728640 字节）。入站 multipart 的解析与张数限制属于
	// gateway handler 层；适配器在**发上游之前**再卡一次同样的字节数，理由是：
	// 网关转发一个 100 MB 的 data URL 上游必然拒绝（浪费一次上游调用与配额），
	// 而且网关自己的响应体上限只有 1 MiB 级别的余量假设（maxResponseBytes）。
	maxImageBytes = 15728640
	// defaultResolution 是未指定 resolution 时的兜底（与 New API 的 "1080p" 一致）。
	defaultResolution = "1080p"
)

// allowedParamKeyList 是 core.MediaSubmitRequest.Params 的**冻结键集**（7 个，字典序）。
//
// 来源：internal/gateway/handler/media_task_handler.go 的 mediaSubmitParams（唯一生产者），
// 它保证这 7 个键**恒定存在**（缺省为零值）。适配器必须 fail-closed：表外键返回 400 类错误，
// **绝不静默忽略**——静默忽略会让用户以为参数生效了（例如把 negative_prompt / seed /
// movement_amplitude 吃掉），而费用照付。
var allowedParamKeyList = []string{
	"aspect_ratio",
	"duration",
	"images",
	"mode",
	"prompt",
	"resolution",
	"size",
}

// resolutionAllowList 是 resolution 的合法档位（逐字对齐任务书与设计文档 §3.2）。
var resolutionAllowList = []string{"360p", "540p", "720p", "1080p"}

// modelFamily 是「按模型收紧参数」的家族标签。
//
// 用**前缀**匹配而不是全等：上游模型名可能带后缀（如 viduq2-pro / viduq1-turbo），
// 而这些后缀只影响画质档位、不改变 duration/resolution 约束矩阵。
type modelFamily string

const (
	// familyVidu20：vidu2.0 系列。duration ∈ {4,8}，且**不支持纯文生**；duration=8 时只允许 720p。
	familyVidu20 modelFamily = "vidu2.0"
	// familyViduQ1：viduq1 系列。duration 只能 5，resolution **强制 1080p**。
	familyViduQ1 modelFamily = "viduq1"
	// familyViduQ2：viduq2 系列。duration ∈ [1,10]。
	familyViduQ2 modelFamily = "viduq2"
	// familyUnlisted：未在约束矩阵内的模型（如 vidu1.5），或未来新增模型。
	familyUnlisted modelFamily = ""
)

// modelProfile 是一个模型家族的参数约束。
type modelProfile struct {
	// family 家族标签（报错文案用）。
	family modelFamily
	// supported 该家族是否被本适配器显式识别（仅用于日志/文案，不改变行为）。
	supported bool
	// allowedDurations 枚举型 duration 白名单（非空时忽略 min/maxDuration）。
	allowedDurations []int
	// minDuration / maxDuration 区间型 duration 约束（仅当 allowedDurations 为空时生效；
	// 两者都为 0 表示「本地不设区间」）。
	minDuration int
	maxDuration int
	// defaultDuration 未提供 duration 时的缺省（**必须在合法集合内**，否则组装出的报文
	// 会被上游拒绝；familyVidu20 的缺省因此是 4 而不是 New API 的通用缺省 5）。
	defaultDuration int
	// defaultResolution 未提供 resolution 时的缺省档位。
	defaultResolution string
	// forcedResolution 非空时表示该家族**只**支持这一档（viduq1 → 1080p）。
	forcedResolution string
	// supportsTextToVideo 是否支持纯文生（vidu2.0 为 false：无图直接 400）。
	supportsTextToVideo bool
}

// modelProfiles 是参数约束矩阵（唯一的本地真值来源）。
//
// 矩阵逐条来自 D3/D4 任务书：
//
//	vidu2.0 : duration ∈ {4,8}；duration=8 只允许 720p；无图（纯文生）直接 400
//	viduq1  : duration 只能 5；resolution 强制 1080p
//	viduq2  : duration ∈ [1,10]
//	未列出  : **不发明约束**（duration 任意正整数、resolution 走全局白名单），
//	          避免在缺凭据无法核实的情况下误杀合法取值（vidu1.5 即属此类）。
var modelProfiles = map[modelFamily]modelProfile{
	familyVidu20: {
		family:              familyVidu20,
		supported:           true,
		allowedDurations:    []int{4, 8},
		defaultDuration:     4,
		defaultResolution:   defaultResolution,
		supportsTextToVideo: false,
	},
	familyViduQ1: {
		family:              familyViduQ1,
		supported:           true,
		allowedDurations:    []int{5},
		defaultDuration:     5,
		defaultResolution:   "1080p",
		forcedResolution:    "1080p",
		supportsTextToVideo: true,
	},
	familyViduQ2: {
		family:              familyViduQ2,
		supported:           true,
		minDuration:         1,
		maxDuration:         10,
		defaultDuration:     5,
		defaultResolution:   defaultResolution,
		supportsTextToVideo: true,
	},
	familyUnlisted: {
		family:              familyUnlisted,
		defaultDuration:     5,
		defaultResolution:   defaultResolution,
		supportsTextToVideo: true,
	},
}

// classifyModel 把上游模型名归到家族（前缀匹配，大小写不敏感）。
func classifyModel(model string) modelFamily {
	lowered := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(lowered, string(familyVidu20)):
		return familyVidu20
	case strings.HasPrefix(lowered, string(familyViduQ1)):
		return familyViduQ1
	case strings.HasPrefix(lowered, string(familyViduQ2)):
		return familyViduQ2
	default:
		return familyUnlisted
	}
}

// profileForModel 取模型对应的约束档（未识别模型回退 familyUnlisted 档，永不返回零值）。
func profileForModel(model string) modelProfile {
	if profile, ok := modelProfiles[classifyModel(model)]; ok {
		return profile
	}
	return modelProfiles[familyUnlisted]
}

// normalizeReferenceModel 归一参考生视频使用的模型名。
//
// New API 的硬编码（`adaptor.go:124-129`）：参考图生视频只能用 **viduq2 基础模型**，
// 不能带 pro/turbo 后缀，否则上游拒绝。因此这里把 viduq2 家族的后缀去掉，
// 其它模型（如 vidu2.0 走参考生视频）**原样透传**，由上游判定——不在这里发明替换规则。
func normalizeReferenceModel(action upstreamAction, model string) string {
	if action != upstreamActionReference2Video {
		return model
	}
	if classifyModel(model) == familyViduQ2 && !strings.EqualFold(model, string(familyViduQ2)) {
		slog.Debug("vidu: reference2video 只支持基础 viduq2，已去掉模型后缀",
			"model", model, "normalized", string(familyViduQ2))
		return string(familyViduQ2)
	}
	return model
}

// viduRequestPayload 是上游提交请求体。
//
// 字段与 json tag 逐字对齐 New API 的 requestPayload（`adaptor.go:29-40`）里**我们能从
// 冻结 7 键安全推导**的部分：
//
//   - 不发 seed / bgm / payload / callback_url：它们不在 7 键里，用户想传这些键会在
//     validateParamKeys 拿到 400，而不是被无声丢掉（fail-closed）；
//   - `images` 用 omitempty：纯文生（0 张图）时**不发** `"images":[]`，避免上游把它当成
//     「给了空图」而走图片分支。New API 会发 `"images":null`（无 omitempty），这是
//     本适配器刻意收紧的一处（保守取舍，无凭据核实上游对空数组的处理）。
//   - `duration` / `movement_amplitude` 无 omitempty：两者都恒有确定取值（duration 由
//     模型档兜底、amplitude 固定 auto），显式发出去比依赖上游默认值更可预测。
type viduRequestPayload struct {
	Model             string   `json:"model"`
	Images            []string `json:"images,omitempty"`
	Prompt            string   `json:"prompt,omitempty"`
	Duration          int      `json:"duration"`
	Resolution        string   `json:"resolution,omitempty"`
	MovementAmplitude string   `json:"movement_amplitude"`
}

// submitPlan 是一次 Submit 的翻译结果（端点 + 报文）。
// 抽成返回值是为了让「参数 → 上游报文」这层纯函数能被直接断言，不必解析 HTTP 报文。
type submitPlan struct {
	action  upstreamAction
	payload viduRequestPayload
}

// buildSubmitPlan 把归一请求翻译成上游端点与请求体，并完成所有前置校验。
//
// 校验顺序（每一步都不发网络请求，因此失败时**上游一定没被调用**）：
// 表外参数 → 参数类型 → action ↔ 图片张数一致性 → 模型家族 → 纯文生支持性
// → duration → resolution（含组合约束）。
func buildSubmitPlan(req *core.MediaSubmitRequest) (*submitPlan, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("vidu: 提交请求为空", nil)
	}

	params := req.Params
	if err := validateParamKeys(params); err != nil {
		return nil, err
	}

	prompt, err := paramString(params, "prompt")
	if err != nil {
		return nil, err
	}
	images, err := paramImages(params)
	if err != nil {
		return nil, err
	}

	// ① action 判定：**由图片张数决定**，请求里的 action 只做一致性校验。
	// 静默按数量纠正（例如 action=text_to_video 却带了 3 张图）会让用户以为参数生效了，
	// 所以不一致时直接 400（任务书明确要求）。
	action, wantAction, ok := planActionForImageCount(len(images))
	if !ok {
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("vidu: 无法由 images 数量 %d 判定动作", len(images)), nil)
	}
	if req.Action != wantAction {
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("vidu: action=%q 与 images 数量 %d 不一致（%d 张图必须使用 action=%s）",
				string(req.Action), len(images), len(images), string(wantAction)), nil)
	}

	// ② 上游模型（网关已完成「模型组 → 上游模型」改写；为空兜底 viduq1，与 New API 一致）。
	model := strings.TrimSpace(req.UpstreamModel)
	if model == "" {
		model = defaultModel
	}
	profile := profileForModel(model)

	// ③ vidu2.0 系列不支持纯文生：无图提前 400（任务书硬约束）。
	if action == upstreamActionText2Video && !profile.supportsTextToVideo {
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("vidu: 模型 %s 不支持纯文生（需至少 1 张图片）", model), nil)
	}
	// 纯文生必须有 prompt：上游对文生是硬要求，提前 400 比让上游报错更可诊断
	// （图生/首尾帧/参考图的 prompt 是否必填无法在无凭据下核实，故**只**在文生收紧）。
	if action == upstreamActionText2Video && strings.TrimSpace(prompt) == "" {
		return nil, core.NewInvalidRequestError("vidu: 纯文生（text_to_video）必须提供 prompt", nil)
	}

	model = normalizeReferenceModel(action, model)

	// ④ duration（按模型家族收紧，越界提前 400）。
	duration, err := resolveDuration(params, profile, model)
	if err != nil {
		return nil, err
	}

	// ⑤ resolution（全局白名单 + 家族强制档 + vidu2.0×8s 组合约束）。
	resolution, err := resolveResolution(params, profile, duration, model)
	if err != nil {
		return nil, err
	}

	// ⑥ 冻结键集里 Vidu 无对应上游字段的键：**不静默忽略**，留一条可观测日志。
	// 为什么不 400：这 7 个键是跨渠道的归一键集，gateway handler 恒定填充（aspect_ratio
	// 甚至会由 size 自动推导），对 Vidu 请求 400 会把正常流量全部打死；而 Vidu 的上游
	// 报文里没有 mode/aspect_ratio/size 三个字段（见 viduRequestPayload 注释）。
	// 取舍与 Kling 一致（Kling 同样不消费 resolution），差异只在「是否留痕」。
	warnIgnoredParams(params)

	return &submitPlan{
		action: action,
		payload: viduRequestPayload{
			Model:             model,
			Images:            images,
			Prompt:            prompt,
			Duration:          duration,
			Resolution:        resolution,
			MovementAmplitude: movementAmplitude,
		},
	}, nil
}

// ignoredParamKeys 是冻结 7 键里 Vidu 上游报文**不消费**的键。
//
// `resolution`/`duration`/`prompt`/`images` 会被消费；`mode` 是 Kling 的画质档位（Vidu 无此字段）；
// `aspect_ratio`/`size` 在 New API 的 Vidu 报文里也不存在（Vidu 以 resolution 表达画幅档位）。
var ignoredParamKeys = []string{"aspect_ratio", "mode", "size"}

// warnIgnoredParams 对「用户显式给了值但 Vidu 不消费」的参数留痕（Debug 级，不刷 Error）。
func warnIgnoredParams(params map[string]any) {
	var present []string
	for _, key := range ignoredParamKeys {
		if value, ok := params[key]; ok && value != nil {
			if text, isString := value.(string); isString && strings.TrimSpace(text) == "" {
				continue
			}
			present = append(present, key)
		}
	}
	if len(present) == 0 {
		return
	}
	slog.Debug("vidu: 参数在该渠道没有对应上游字段，已忽略（不影响计费口径）",
		"ignored_params", present)
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
		fmt.Sprintf("vidu: 不支持的参数 %s（Vidu 只接受 %s）",
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
			fmt.Sprintf("vidu: 参数 %s 必须是字符串，收到 %T", key, raw), nil)
	}
	return value, nil
}

// paramImages 取归一图片列表（顺序即语义：首帧在前、尾帧在后；参考图为并列关系）。
//
// 接受 []string（gateway handler 的唯一形态）与 []any（JSON 解码形态，便于单测与将来的
// 直连调用方）；其余类型一律 400。每张图额外做 data URL 字节上限校验（见 maxImageBytes）。
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
					fmt.Sprintf("vidu: images[%d] 必须是字符串，收到 %T", i, item), nil)
			}
			items = append(items, value)
		}
	default:
		return nil, core.NewInvalidRequestError(
			fmt.Sprintf("vidu: 参数 images 必须是字符串数组，收到 %T", raw), nil)
	}

	out := make([]string, 0, len(items))
	for i, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			return nil, core.NewInvalidRequestError(
				fmt.Sprintf("vidu: images[%d] 为空", i), nil)
		}
		if err := validateImagePayload(i, value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

// validateImagePayload 校验单张图片的本地可判定约束。
//
// 只看 data URL（`data:<mime>[;base64],<payload>`）：只有它能在本地算出字节数。
// 普通 http(s) URL 不做长度校验——URL 长度与产物体积无关（网关也不去抓取它，
// 抓取会引入 SSRF 面且拖长提交路径）。
func validateImagePayload(index int, value string) error {
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return nil
	}
	size, err := dataURLDecodedSize(value)
	if err != nil {
		return core.NewInvalidRequestError(
			fmt.Sprintf("vidu: images[%d] 不是合法 data URL: %v", index, err), nil)
	}
	if size > maxImageBytes {
		return core.NewInvalidRequestError(
			fmt.Sprintf("vidu: images[%d] 解码后 %d 字节，超过上限 %d 字节（约 %d MiB）",
				index, size, maxImageBytes, maxImageBytes>>20), nil)
	}
	return nil
}

// dataURLDecodedSize 计算 data URL 载荷解码后的字节数。
//
// 只支持标准形态：`data:<meta>,<payload>`；`<meta>` 含 `;base64` 时按 base64 计算，
// 否则按百分号编码后的原始字节数估算（len(payload)）。
//
// base64 长度用 `len/4*3 - padding` 精确计算而**不**真解码：一张 15 MiB 的图解码会分配
// 20 MB 内存，而这里只需要字节数。刻意**不**因非法字符预先拒绝（上游更权威），
// 只保证「估算值不小于真实值」不会发生——标准 padding 形态下即为精确值。
func dataURLDecodedSize(value string) (int, error) {
	comma := strings.Index(value, ",")
	if comma < 0 {
		return 0, fmt.Errorf("缺少 `,` 分隔的头/载荷")
	}
	meta := strings.ToLower(value[:comma])
	payload := value[comma+1:]

	if !strings.Contains(meta, ";base64") {
		// 非 base64：载荷是百分号编码的文本/二进制形态，按原始长度估（保守取上界）。
		return len(payload), nil
	}

	// 去掉换行/空白后按标准 base64 计算。
	compact := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, payload)
	if compact == "" {
		return 0, fmt.Errorf("base64 载荷为空")
	}
	if len(compact)%4 != 0 {
		return 0, fmt.Errorf("base64 载荷长度 %d 不是 4 的倍数", len(compact))
	}
	padding := 0
	for i := len(compact) - 1; i >= 0 && compact[i] == '='; i-- {
		padding++
	}
	if padding > 2 {
		return 0, fmt.Errorf("base64 载荷 padding 非法（%d 个 '='）", padding)
	}
	return len(compact)/4*3 - padding, nil
}

// resolveDuration 解析并校验时长（秒）。
//
// 归一约定：`Params["duration"]` 是 **int 秒**（gateway handler 的 mediaRequestDuration），
// 缺省或 <=0 表示「未提供」→ 用该模型家族的缺省值。
//
// 约束按家族收紧（任务书矩阵），越界**提前 400**：
//   - vidu2.0：枚举 {4,8}；
//   - viduq1：只能是 5；
//   - viduq2：区间 [1,10]；
//   - 未列出模型：不设本地区间（只要求正整数）。
func resolveDuration(params map[string]any, profile modelProfile, model string) (int, error) {
	raw, ok := params["duration"]
	if !ok || raw == nil {
		return profile.defaultDuration, nil
	}
	seconds, ok := asSeconds(raw)
	if !ok {
		return 0, core.NewInvalidRequestError(
			fmt.Sprintf("vidu: duration 必须是整数秒，收到 %T", raw), nil)
	}
	if seconds <= 0 {
		// 与 handler 的 mediaRequestDuration 语义一致：0/负数等价于「未提供」。
		return profile.defaultDuration, nil
	}

	if len(profile.allowedDurations) > 0 {
		if !containsInt(profile.allowedDurations, seconds) {
			return 0, core.NewInvalidRequestError(
				fmt.Sprintf("vidu: 模型 %s 的 duration 只支持 %s 秒，收到 %d",
					model, joinInts(profile.allowedDurations), seconds), nil)
		}
		return seconds, nil
	}

	if profile.minDuration > 0 && seconds < profile.minDuration {
		return 0, core.NewInvalidRequestError(
			fmt.Sprintf("vidu: 模型 %s 的 duration 必须在 %d–%d 秒之间，收到 %d",
				model, profile.minDuration, profile.maxDuration, seconds), nil)
	}
	if profile.maxDuration > 0 && seconds > profile.maxDuration {
		return 0, core.NewInvalidRequestError(
			fmt.Sprintf("vidu: 模型 %s 的 duration 必须在 %d–%d 秒之间，收到 %d",
				model, profile.minDuration, profile.maxDuration, seconds), nil)
	}
	return seconds, nil
}

// resolveResolution 解析并校验画幅档位。
//
// 判定顺序（顺序本身就是契约，不要重排）：
//  1. 用户显式取值 → 归一为小写并做**全局白名单**校验（不在 {360p,540p,720p,1080p} 内 → 400）；
//  2. 家族强制档（viduq1 → 1080p）：显式给了别的档 → 400 而**不是静默改写**。
//     理由与任务书对 7 键 fail-closed 的要求同源：静默改写会让用户以为参数生效了、
//     费用照付；400 让「viduq1 只用 1080p」这条产品约束可被调用方看见。
//     未显式给值时不报错，直接落到强制档（有效取值恒为 1080p）。
//  3. 组合约束：`vidu2.0` 且 `duration=8` → **只允许 720p**。显式给了别的档 → 400；
//     没给 → 兜底 720p（这是本矩阵里唯一一处缺省值随 duration 变化的地方，
//     也是最容易漏掉的一条）。
//  4. 其余情况：缺省 1080p。
func resolveResolution(params map[string]any, profile modelProfile, duration int, model string) (string, error) {
	declared, err := paramString(params, "resolution")
	if err != nil {
		return "", err
	}
	resolution := strings.ToLower(strings.TrimSpace(declared))

	if resolution != "" && !containsString(resolutionAllowList, resolution) {
		return "", core.NewInvalidRequestError(
			fmt.Sprintf("vidu: resolution 只支持 %s，收到 %q",
				strings.Join(resolutionAllowList, "/"), declared), nil)
	}

	if profile.forcedResolution != "" {
		if resolution != "" && resolution != profile.forcedResolution {
			return "", core.NewInvalidRequestError(
				fmt.Sprintf("vidu: 模型 %s 只支持 resolution=%s，收到 %q",
					model, profile.forcedResolution, declared), nil)
		}
		return profile.forcedResolution, nil
	}

	if profile.family == familyVidu20 && duration == 8 {
		if resolution != "" && resolution != "720p" {
			return "", core.NewInvalidRequestError(
				fmt.Sprintf("vidu: 模型 %s 在 duration=8 时只支持 resolution=720p，收到 %q",
					model, declared), nil)
		}
		return "720p", nil
	}

	if resolution != "" {
		return resolution, nil
	}
	return profile.defaultResolution, nil
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

// containsInt 报告枚举里是否含 value。
func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// containsString 报告枚举里是否含 value。
func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// joinInts 把枚举渲染成 "4/8" 形态的报错文案。
func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, "/")
}

// ---------------------------------------------------------------------------
// 上游响应 → core 归一状态
// ---------------------------------------------------------------------------

// viduTaskResponse 是提交与查询**共用**（且字段同形）的响应体。
//
// 上游把 task_id / state / err_code / creations 直接放在**顶层**（不是 data 信封里），
// 与 Kling 的 `{code,data:{...}}` 不同——证据：New API 的 responsePayload
// （`adaptor.go:42-55`，用于解析提交响应）与 taskResultResponse
// （`adaptor.go:57-63`，用于解析查询响应）都是顶层结构。
type viduTaskResponse struct {
	// TaskID 上游任务 id（提交响应必给；查询响应也会回带）。
	TaskID string `json:"task_id"`
	// State 上游状态词，必须逐字命中 upstreamTaskStatuses。
	State string `json:"state"`
	// ErrCode 失败原因（仅 failed 有意义）。
	ErrCode string `json:"err_code"`
	// Credits 上游扣减的 credits。用 RawMessage 是为了能区分「上游没给」与「上游给了 0」
	// ——**不伪造用量**是本渠道的硬要求（见 buildUsage）。
	Credits json.RawMessage `json:"credits"`
	// Duration 上游回带的时长（数字或数字字符串都出现过）。
	Duration json.RawMessage `json:"duration"`
	// Resolution 上游回带的画幅档位。
	Resolution string `json:"resolution"`
	// Progress 上游进度（数字百分比或阶段名）。设计文档的 handler 注释明确记录
	// 「Vidu 报百分比、Kling 报阶段名」（internal/gateway/handler/media_task_handler.go:1126），
	// 因此这里原样透传字符串，由响应层做 0-100 归一。无法本地核实字段类型，
	// 故用 RawMessage 兼容数字/字符串两种形态；缺省留空（不猜测进度）。
	Progress json.RawMessage `json:"progress"`
	// Creations 产物列表（**多产物全取**，不是只取首个，见 buildArtifacts）。
	Creations []viduCreation `json:"creations"`
}

// viduCreation 是 creations[] 的元素（`adaptor.go:65-69`）。
type viduCreation struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	CoverURL string `json:"cover_url"`
}

// upstreamTaskStatuses 是**上游真实返回**的状态词表（逐字对齐 `adaptor.go:253-270`，
// 以及设计文档 §3.2 的状态枚举）。
//
// ⚠️ 易错点：上游的排队态拼写是 `queueing`（双 e），**不是** queued/queuing；
// 这张表按逐字匹配，不做大小写折叠、不做近义词兜底。
var upstreamTaskStatuses = map[string]core.MediaStatus{
	"created":    core.MediaStatusQueued,
	"queueing":   core.MediaStatusQueued,
	"processing": core.MediaStatusInProgress,
	"success":    core.MediaStatusCompleted,
	"failed":     core.MediaStatusFailed,
}

// mapTaskStatus 把上游状态词归一到对外词表。
//
// 未知值（含大小写变体、空串、New API 隐式兜底的任何词）一律返回 error：这是
// core/media_task.go 契约 2 的协议失败语义——既不得当成 completed（会漏计费/交付错产物），
// 也不得静默当 in_progress（会无限轮询）。New API 在这里是 `default: return error`
// （`adaptor.go:268-270`），我们保持一致，并比它多拒绝空串。
func mapTaskStatus(raw string) (core.MediaStatus, error) {
	status := strings.TrimSpace(raw)
	if status == "" {
		return "", fmt.Errorf("vidu: 上游响应缺少 state（协议失败）")
	}
	mapped, ok := upstreamTaskStatuses[status]
	if !ok {
		return "", fmt.Errorf(
			"vidu: 未知的上游任务状态 %q（协议失败：既不当成功也不无限轮询）", status)
	}
	return mapped, nil
}

// buildArtifacts 把 `creations[]` **全量**转成产物列表。
//
// New API 只取 creations[0]（`adaptor.go:260-262`、`288-290`）是它的缺陷：多产物任务会丢掉
// 其余成片（且这是任务书点名要修的）。这里全取，并跳过 URL 为空的条目
// （缺 URL 的条目对调用方无意义，但**保留**其余条目而不是整体放弃）。
func buildArtifacts(creations []viduCreation) []core.MediaArtifact {
	artifacts := make([]core.MediaArtifact, 0, len(creations))
	for _, creation := range creations {
		url := strings.TrimSpace(creation.URL)
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

// videoMimeType 尽力从产物 URL 的扩展名推断 MIME 类型（上游不给 mime 字段）。
//
// 能确定就填、不确定留空——core.MediaArtifact 的注释允许 MimeType 为空（由网关推断）。
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

// progressText 把上游 progress 归一成字符串（数字 → 十进制文本；字符串 → 去空白原文）。
//
// 认不出/缺省 → 空串（**绝不猜测**：猜 100 会让调用方以为任务完成了）。
func progressText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var asNumber float64
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return strconv.FormatFloat(asNumber, 'f', -1, 64)
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	return ""
}

// buildUsage 抽取终态用量（供控制面记账与对账）。
//
// ============================ 用量口径（本渠道的硬要求） ============================
// **上游没给就留 nil，绝不伪造。** 这里只搬运响应里**确实存在**的字段：
//
//   - `credits`：上游扣减的 credits（New API 的 taskResultResponse 里有这个字段，
//     但设计文档 §3.2 记录 usageExamples 的 credits 全为 0、真实值由上游返回）。
//     本适配器不假设它一定存在——缺了就缺了。
//   - `duration` / `resolution`：上游回带的实际时长与档位（用于 core.MediaCostInput
//     的 output_seconds 退化口径；缺省时由调用方按**请求参数**退化计价）。
//
// 为什么不像 Kling 那样总有东西可采集：Kling 的 `data.final_unit_deduction` 是上游**实际扣减**
// 的权威数字，所以 Kling 终态必定能采到；Vidu 没有等价字段。
//
// 🔴 **Vidu 的 resolution 档位单价产品尚未给出**（设计文档 §7.2 billing_unit/unit_price_usd
// 待 E 队落地），因此本渠道今天的真实计价口径只能是「按请求参数退化」近似
// ——这一点已在报告里显式标注，**不要**用估算值冒充上游用量。
//
// 三个键都拿不到 → 返回 nil（调用方据此走请求参数退化口径）。绝不返回空 map
// （空 map 非 nil，会让调用方以为「上游给了用量」）。
func buildUsage(resp viduTaskResponse) map[string]any {
	usage := make(map[string]any, 3)

	if value, ok := decodeNumberOrString(resp.Credits); ok {
		usage["credits"] = value
	}
	if value, ok := decodeNumberOrString(resp.Duration); ok {
		usage["duration"] = value
	}
	if resolution := strings.TrimSpace(resp.Resolution); resolution != "" {
		usage["resolution"] = resolution
	}

	if len(usage) == 0 {
		return nil
	}
	return usage
}

// decodeNumberOrString 解析可能是数字也可能是字符串的原始 JSON 值（Kling 侧同款工具）。
//
// 归一规则：JSON 数字 → float64；数字字符串（如 `"1.0"`）→ float64；
// 无法解析的字符串原样保留（**绝不**改写成 0）；空串/null/非法 JSON 视为「上游没给」。
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
