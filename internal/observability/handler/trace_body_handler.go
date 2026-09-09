package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/observability/service"
)

// 本文件是规格 §8.1 两条点读端点的接线层（计划 3/4 任务 5）：
//
//	GET /api/v1/trace/:request_id/body          （管理面：既有 JWTAuth + guard(PermUsageViewAll)）
//	GET /internal/v1/trace-body?request_id=…    （内部：既有 InternalAuth）
//
// 【职责只有 HTTP 入参出参与鉴权上下文】取正文、优先级判定与错误分类分别落在
// 服务层（internal/observability/service）与映射层（trace_body_error.go）；本文件
// 不查库、不读文件、不解析 token、**不新建鉴权机制**（规格 §2.2 明文）。
//
// 【200 的响应体是正文原字节】两条端点都直接写服务层返回的字节，**不包壳**：
// 包一层 {"body": …} 就不再是原字节（逐字节契约破裂），并且会把服务层结果上的
// 定位键（Ref）与来源（Source）字段泄漏进响应。

// TraceBodySubjectFunc 反查一条 request_id 的归属（user_id / key_type）。
//
// 生产实现是既有 trace 端点用的**同一个判据**（call_logs 反查、归属为空按 fail closed
// 处理，见 usage_handler.go 的 LookupTraceSubject）；注入成函数是为了让 B 层用例
// 不连 rbac、不造 call_logs 也能测越权。
type TraceBodySubjectFunc func(ctx context.Context, requestID string) (userID string, keyType string, found bool)

// TraceBodyCanViewFunc 判定当前请求者能否看到该归属记录的提示词正文。
// 生产注入既有的 rbac.CanViewPrompt；注入成函数是为了让 B 层用例直接测 403。
type TraceBodyCanViewFunc func(c *gin.Context, ownerUserID string, isProgramKey bool) bool

// TraceBodyHandler 是两条点读端点的 gin handler。
//
// 只持有服务层句柄与两个注入的判定函数：**判定逻辑不在这里各写一套**。
type TraceBodyHandler struct {
	svc     *service.TraceBodyService
	subject TraceBodySubjectFunc
	canView TraceBodyCanViewFunc
}

// NewTraceBodyHandler 构造点读 handler。
//
// router 注入生产实现：`LookupTraceSubject`（与既有 trace 端点同一套归属反查）
// 与 `rbac.CanViewPrompt`（既有可见性判据）。
func NewTraceBodyHandler(svc *service.TraceBodyService, subject TraceBodySubjectFunc, canView TraceBodyCanViewFunc) *TraceBodyHandler {
	return &TraceBodyHandler{svc: svc, subject: subject, canView: canView}
}

// TraceBody GET /api/v1/trace/:request_id/body —— 管理面端点。
//
// 【判定次序是承重的，前两步都必须先于任何存储访问】
//
//	① 入参：缺 request_id ⇒ 400；
//	② 归属：反查不到 ⇒ **404**（按不存在处理，不泄露存在性）；
//	③ 可见性：提示词不可见 ⇒ **403**（本端点返回的就是提示词本身，无法「部分裁剪」；
//	   返回 200 + 空正文会让「无权看」与「正文为空」不可区分）；
//	④ 取正文：错误交映射层分类，成功直接写正文原字节。
func (h *TraceBodyHandler) TraceBody(c *gin.Context) {
	requestID, ok := requireRequestID(c, c.Param("request_id"))
	if !ok {
		return
	}
	userID, keyType, found := h.subject(c.Request.Context(), requestID)
	if !found {
		// 归属未知（无 call_logs 行 / user_id 为空）：按不存在处理。
		// 不能回「这条 trace 存在但你看不了」——那本身就是存在性泄露。
		c.JSON(http.StatusNotFound, gin.H{"error": "trace not found"})
		return
	}
	if !h.canView(c, userID, keyType == obsmodel.KeyTypeService) {
		c.JSON(http.StatusForbidden, gin.H{"error": "prompt not visible"})
		return
	}
	h.getBody(c, requestID)
}

// InternalTraceBody GET /internal/v1/trace-body?request_id=… —— 内部端点。
//
// 服务间调用：无归属判定、不裁剪（沿用既有内部端点惯例）；鉴权由 internalGroup 上的
// 既有 InternalAuth 负责，本 handler 不解析任何凭据。
func (h *TraceBodyHandler) InternalTraceBody(c *gin.Context) {
	requestID, ok := requireRequestID(c, c.Query("request_id"))
	if !ok {
		return
	}
	h.getBody(c, requestID)
}

// getBody 取正文并写出响应；两条端点共用（差异只在鉴权与归属/可见性判定）。
func (h *TraceBodyHandler) getBody(c *gin.Context, requestID string) {
	result, err := h.svc.GetBody(c.Request.Context(), requestID)
	if err != nil {
		writeTraceBodyError(c, err)
		return
	}
	writeTraceBodyResult(c, result)
}

// requireRequestID 是两条端点共用的入参校验：缺 request_id ⇒ 400。
//
// 直接放行空串会让服务层拿 "" 去查库并把入参错误伪装成「记录不存在」（404）。
func requireRequestID(c *gin.Context, requestID string) (string, bool) {
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id required"})
		return "", false
	}
	return requestID, true
}

// writeTraceBodyError 复用任务 3 的映射层：状态码与诊断体都由 MapTraceBodyError 决定，
// 本文件**不另起一套**错误映射（也不改写、不吞掉底层诊断文本）。
func writeTraceBodyError(c *gin.Context, err error) {
	status, resp := MapTraceBodyError(err)
	c.JSON(status, resp)
}

// writeTraceBodyResult 把服务层返回的正文**原字节**写进 200。
//
// 【逐字节契约】body 就是结果里的字节：不重新序列化、不包壳。Content-Length 显式写出，
// 让「body 长度逐字节等于服务层返回值」这条契约能被用例固定（httptest 不会自动补该头）。
func writeTraceBodyResult(c *gin.Context, result *service.TraceBodyResult) {
	c.Header("Content-Length", strconv.Itoa(len(result.Body)))
	c.Data(http.StatusOK, "application/json", result.Body)
}
