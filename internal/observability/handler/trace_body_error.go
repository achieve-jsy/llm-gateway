package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/example/llm-gateway/internal/observability/service"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"gorm.io/gorm"
)

// 本文件是点读错误语义 → HTTP 表现的**纯函数映射**（计划 3/4 任务 3）。
//
// 【为什么单独成文件】它是本计划里唯一「纯粹的分类逻辑」——没有 IO、没有 DB、
// 没有 gin.Context ⇒ 可以做到「每条哨兵一个用例、每条变异一个断言红」。
// 因此这里**只做分类**：不查库、不读文件、不写响应、不新增哨兵。
//
// 【错误身份是契约】调用方一律用 `errors.Is` 分类（冻结包 errors.go 的自述），
// 对外包装必须 `%w`；本函数用 `errors.Is` 判定，所以服务层**包装过**的哨兵同样命中。
//
// 【200 的 body 不是本函数的返回体】成功路径由调用方直接写出**正文 JSON 原字节**
// （任务 5 的 `c.Data(http.StatusOK, "application/json", result.Body)`）。
// 本函数对 nil 返回零值诊断体，**不得**把服务层结果（`TraceBodyResult`，含
// `Ref`/`Source`）包一层 json.Marshal —— 那会把定位键泄漏进响应。

// TraceBodyErrorResponse 是对外诊断体（与仓库既有错误形状一致：`{"error": {...}}`）。
type TraceBodyErrorResponse struct {
	Error TraceBodyErrorDetail `json:"error"`
}

// TraceBodyErrorDetail 是诊断体的四个键（键集合是契约：只有这四个）。
//
// `Type` 是粗分类（`not_found` / `storage_error` / `unavailable` / `client_closed` /
// `timeout` / `internal_error`），`Code` 才是机器可读的精确契约（用例逐行断言它）。
// 规格 §8.1 的 `gone`（410）在本计划**不可达**（冻结包无 `ErrPartNotFound` 生产者，
// 见 D53）⇒ 该取值保留但永不出现。
type TraceBodyErrorDetail struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// BodyMissing 仅 404 类为 true（规格 §8.1 要求「正文从未落盘」可被调用方区分）。
	BodyMissing bool `json:"body_missing"`
}

// MapTraceBodyError 把服务层 / 存储层的错误分类成对外表现。纯函数。
//
// 映射表见计划任务 3 的权威判据表；顺序是承重的：具体哨兵在前，`context` 与兜底在后。
// 注意实现形状（每个臂**直接 return**，不得先赋值再统一 return、不得用 if-chain）：
// 任务 3 的变异以这些字面量为定位串。
func MapTraceBodyError(err error) (status int, resp TraceBodyErrorResponse) {
	switch {
	case err == nil:
		// 成功：调用方写正文原字节；这里不产出任何诊断体。
		return http.StatusOK, TraceBodyErrorResponse{}
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "not_found",
			Code:    "trace_not_found",
			Message: "call_traces 中没有该 request_id 的记录",
		}}
	case errors.Is(err, ta.ErrBodyMissing):
		// 标记行：正文在写入时即失败，从未落盘（规格 §8.1 要求 body_missing=true）。
		return http.StatusNotFound, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:        "not_found",
			Code:        "body_missing",
			Message:     "正文在写入时即失败，从未落盘",
			BodyMissing: true,
		}}
	case errors.Is(err, service.ErrTraceBodyInconsistent):
		return http.StatusInternalServerError, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "storage_error",
			Code:    "body_ref_inconsistent",
			Message: "定位键与 body_missing 组合不一致（元数据自身矛盾，需人工核查）",
		}}
	case errors.Is(err, service.ErrTraceBodyDisabled):
		return http.StatusServiceUnavailable, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "unavailable",
			Code:    "trace_body_disabled",
			Message: "读侧未配置（TRACE_BODY_ROOT 为空），但该行带定位键（部署错误）",
		}}
	case errors.Is(err, ta.ErrNotArchived):
		// 【唯一保留底层文本的臂】它覆盖三种可分辨的成因（两文件都不存在 / 行号越界 /
		// 目标偏移未命中行首），运维处置不同 ⇒ 必须原样透出，不得替换成固定文案。
		return http.StatusInternalServerError, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "storage_error",
			Code:    "not_archived",
			Message: err.Error(),
		}}
	case errors.Is(err, ta.ErrFooterCorrupt), errors.Is(err, ta.ErrTrailerCorrupt):
		return http.StatusInternalServerError, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "storage_error",
			Code:    "part_corrupt",
			Message: "归档产物损坏（footer/trailer 结构或校验不符，含半写/位腐）",
		}}
	case errors.Is(err, context.Canceled):
		// 499 语义：客户端断开**不是**存储故障，报 500 会让日志里全是假故障。
		return http.StatusRequestTimeout, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "client_closed",
			Code:    "client_closed",
			Message: "客户端已断开，读取被取消",
		}}
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "timeout",
			Code:    "read_timeout",
			Message: "读取归档超时（大块解压耗时超出预算）",
		}}
	default:
		// 兜底：也承接 `ta.ErrPartNotFound`（本计划不映射任何状态码，410 不可达，见 D53）。
		// message 不含任何内部细节。
		return http.StatusInternalServerError, TraceBodyErrorResponse{Error: TraceBodyErrorDetail{
			Type:    "internal_error",
			Code:    "internal_error",
			Message: "取正文时发生未分类的内部错误",
		}}
	}
}
