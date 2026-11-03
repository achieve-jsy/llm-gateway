package core

import (
	"context"
	"errors"
	"time"
)

// 本文件定义「异步媒体任务」可选能力接口与状态/产物契约。
// 设计依据：docs/media-channels-integration-design.md §2.1（对外协议）、§6.1（core 类型提案）、§6.4（状态机）。
//
// 与 core.Provider 的关系：MediaTaskProvider 是**可选能力接口**，与 NativeBatchProvider /
// NativeFileProvider / PassthroughProvider 同范式 —— **绝不扩展 core.Provider**。
//
// ============================ 三条必须遵守的契约 ============================
//
// 契约 1：业务失败 ≠ error（最重要）
//   - **业务失败**：上游明确返回「任务失败 / 审核拒绝 / 生成失败」等终态失败语义
//     → Query **必须**返回 (status, nil)，其中 status.Status = MediaStatusFailed
//     且 status.FailReason 非空。**绝不允许返回 error**。
//   - **传输/协议失败**：网络不可达、超时、鉴权失败（401/403）、上游 5xx、
//     响应体无法解析、非预期状态码 → **必须**返回 error（可用 %w 包装）。
//   - 为什么必须分开：调用方对「error」的处理是重试/标记 poll_failure，对「业务失败」的
//     处理是推进到终态并**不记账**。把业务失败返回成 error 会被重试放大成重复计费；
//     把传输失败返回成 failed 会让任务被误判为终态失败且不再轮询。
//
// 契约 2：上游未知状态词 → error（协议失败）
//   上游状态词表必须归一（Kling: submitted/processing/succeed/failed；
//   Vidu: created/queueing/processing/success/failed）。遇到**未枚举**的取值时返回 error
//   （调用方归类为 unrecognized 决定重试或放弃），既不得当成 completed，
//   也不得静默当作 in_progress 无限轮询。
//
// 契约 3：对外任务 id 与上游任务 id 分离
//   - 对外 task id 由**网关**生成、由控制面持久化（media_tasks.task_id），
//     适配器既看不到也不需要生成它；
//   - MediaTaskProvider.Submit 返回的 MediaSubmitResult.VendorTaskID 是**上游** id，
//     调用方必须以 vendor_task_id 语义持久化（抄 New API 的 PrivateData.UpstreamTaskID）；
//     **禁止**把它直接作为对外 id 返回给客户端（否则泄露上游渠道细节，且上游轮询地址可被枚举）。
//   - Query 的入参是上游 id（vendorTaskID），不是对外 id。
//
// ==========================================================================

// ErrMediaTaskNotFound 表示上游明确回答「该任务不存在」（HTTP 404 或等价的业务错误码）。
//
// 它既不是业务失败（上游没有给出 failed 终态），也不是普通传输失败，调用方
// （C7 失败分类）需要单独识别：not_found 通常意味着上报的 vendor id 有误或上游已清理，
// 应计入失败分类并停止无意义轮询。适配器返回时应 %w 包装本错误。
var ErrMediaTaskNotFound = errors.New("core: media task not found upstream")

// MediaKind 媒体任务类别，对应 media_tasks.kind。
type MediaKind string

const (
	// MediaKindImage 图像任务（同步链路一般不入任务表；异步图像渠道仍用它）。
	MediaKindImage MediaKind = "image"
	// MediaKindVideo 视频任务（Kling / Vidu / Seedance 等）。
	MediaKindVideo MediaKind = "video"
)

// IsValid 报告 kind 是否在受支持的枚举内。
func (k MediaKind) IsValid() bool {
	switch k {
	case MediaKindImage, MediaKindVideo:
		return true
	default:
		return false
	}
}

// MediaStatus 是对外（OpenAI 语义）任务状态词表。
//
// 契约：无论上游用什么词，适配器**必须**归一到下面四个值（设计文档 §2.1 / §6.4）。
//
// 注意控制面内部还有一个 `expired`（超时 sweeper 置位，视同 failed 但不记账）：
// 它是**存储层**状态，对外响应里必须归一为 MediaStatusFailed（并在 FailReason 说明超时）。
type MediaStatus string

const (
	// MediaStatusQueued 已受理、尚未开始。
	MediaStatusQueued MediaStatus = "queued"
	// MediaStatusInProgress 上游处理中。
	MediaStatusInProgress MediaStatus = "in_progress"
	// MediaStatusCompleted 成功终态，此时 Artifacts 必须非空。
	MediaStatusCompleted MediaStatus = "completed"
	// MediaStatusFailed 失败终态（含审核拒绝、超时归一），此时 FailReason 必须有意义。
	MediaStatusFailed MediaStatus = "failed"
)

// IsValid 报告状态词是否属于对外词表。
func (s MediaStatus) IsValid() bool {
	switch s {
	case MediaStatusQueued, MediaStatusInProgress, MediaStatusCompleted, MediaStatusFailed:
		return true
	default:
		return false
	}
}

// IsTerminal 报告状态是否为终态（完成后不再推进）。
// 状态机只允许前进：queued → in_progress → completed|failed（设计文档 §6.4）。
func (s MediaStatus) IsTerminal() bool {
	return s == MediaStatusCompleted || s == MediaStatusFailed
}

// MediaAction 归一后的任务动作，对应 media_tasks.action。
//
// 它是**渠道无关**的动作词汇：各适配器负责把它映射到上游路径
// （Vidu: /ent/v2/{text2video,img2video,start-end2video,reference2video}；
// Kling: /v1/videos/{text2video,image2video}）。
type MediaAction string

const (
	// MediaActionTextToVideo 文生视频（Vidu 0 张图 / Kling text2video）。
	MediaActionTextToVideo MediaAction = "text_to_video"
	// MediaActionImageToVideo 图生视频（Vidu 1 张图 / Kling image2video）。
	MediaActionImageToVideo MediaAction = "image_to_video"
	// MediaActionFirstTailToVideo 首尾帧生视频（Vidu start-end2video，2 张图）。
	MediaActionFirstTailToVideo MediaAction = "first_tail_to_video"
	// MediaActionReferenceToVideo 参考生视频（Vidu reference2video，>2 张图）。
	MediaActionReferenceToVideo MediaAction = "reference_to_video"
)

// IsValid 报告 action 是否在已知词汇内。
func (a MediaAction) IsValid() bool {
	switch a {
	case MediaActionTextToVideo, MediaActionImageToVideo,
		MediaActionFirstTailToVideo, MediaActionReferenceToVideo:
		return true
	default:
		return false
	}
}

// MediaArtifact 是任务产物（图片/视频）的归一描述。
//
// 产物 URL 一律直跳上游（credentialless）；Phase 5 才改为网关代理
// （GET /v1/videos/{id}/content 302 直跳）。
type MediaArtifact struct {
	// Kind 产物类别（image / video）。
	Kind MediaKind `json:"kind"`
	// URL 上游产物地址（可能带签名且会过期，见 ExpiresAt）。
	URL string `json:"url"`
	// MimeType 可空；上游不给时由网关按扩展名推断。
	MimeType string `json:"mime_type,omitempty"`
	// ExpiresAt 可空；上游签名链接的过期时间（nil = 未知，不承诺长期有效）。
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// MediaSubmitRequest 是提交上游任务的归一请求。
//
// 契约：请求翻译（归一参数 → 厂商字段）在**适配器内**完成——这与
// core/video.VideoProvider 的「纯直通」不同，是本方案的核心增量。
type MediaSubmitRequest struct {
	// Kind 任务类别。
	Kind MediaKind
	// Action 归一动作（适配器据此选上游路径）。
	Action MediaAction
	// UpstreamModel 已由网关完成「模型组 → 上游模型」改写的上游模型 ID。
	UpstreamModel string
	// Params 已经过前置校验的归一参数（prompt / duration / resolution / images / mode ...）。
	// 适配器不得原样透传未知字段（不支持参数应在准入层返回 400）。
	Params map[string]any
}

// MediaSubmitResult 是提交上游任务的结果。
type MediaSubmitResult struct {
	// VendorTaskID 上游任务 id；调用方以 vendor_task_id 语义持久化（见文件头契约 3）。
	VendorTaskID string
	// Status 上游返回的初始状态；零值视为 MediaStatusQueued。
	Status MediaStatus
}

// MediaTaskStatus 是查询上游任务后的归一状态。
//
// 成功/失败的表达方式见文件头契约 1：业务失败返回 (MediaTaskStatus{Status: failed}, nil)。
type MediaTaskStatus struct {
	// Status 归一后的对外状态，必须是 MediaStatus 的四个取值之一。
	Status MediaStatus
	// Progress 进度描述："0-100" 或阶段名，可空（不同上游语义不同，只做透传展示）。
	Progress string
	// Artifacts 产物列表；Status=completed 时必须非空（可多产物，禁止只取首个）。
	Artifacts []MediaArtifact
	// FailReason 失败原因（Kling data.task_status_msg / Vidu err_code）；
	// Status=failed 时必须非空。
	FailReason string
	// Usage 上游原始用量（如 credits / units / duration / final_unit_deduction），
	// 用于终态记账；缺省 nil 表示上游未提供（此时按请求参数退化口径计价）。
	Usage map[string]any
}

// MediaTaskProvider 异步媒体任务能力（可选接口，不扩 core.Provider）。
//
// 语义契约见文件头：业务失败返回状态、传输/协议失败返回 error、上游 id 与对外 id 分离。
type MediaTaskProvider interface {
	// Submit 翻译并提交上游任务，返回上游任务 id（不放对外 id）。
	Submit(ctx context.Context, req *MediaSubmitRequest) (*MediaSubmitResult, error)
	// Query 查询并归一状态。
	//   - 业务失败（上游 state=failed）→ 返回 (*MediaTaskStatus{Status: failed}, nil)，**不返回 error**；
	//   - 任务不存在（上游 404）→ 返回 (nil, 包装了 ErrMediaTaskNotFound 的 error)；
	//   - 其它传输/协议失败 / 未知状态词 → 返回 (nil, error)。
	Query(ctx context.Context, vendorTaskID string) (*MediaTaskStatus, error)
}
