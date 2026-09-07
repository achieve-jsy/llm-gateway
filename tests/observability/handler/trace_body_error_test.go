package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"testing"

	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 本文件是计划 3/4 任务 3 的 B 层用例：`MapTraceBodyError` 的**纯函数映射表**。
// 不碰文件系统 / DB / HTTP / gin：被测函数只有 (err) → (status, 诊断体)。
// 每条映射行都断言 status + code 两个**契约字段**；message 只断言「非空 + 关键子串」
// （不做逐字断言，避免文案漂移把变异变脆）。
//
// 断言不依赖宿主机状态：所有输入都是自造哨兵 / 包装错误，无 IO、无时钟、无环境变量。

// TestMapTraceBodyError 是步骤 1 的表驱动映射（本计划的权威判据表逐行对应）。
func TestMapTraceBodyError(t *testing.T) {
	tests := []struct {
		name        string
		in          error
		wantStatus  int
		wantCode    string
		wantMissing bool
	}{
		{name: "nil 不产生错误", in: nil, wantStatus: http.StatusOK, wantCode: ""},
		{name: "记录缺失", in: gorm.ErrRecordNotFound, wantStatus: http.StatusNotFound, wantCode: "trace_not_found"},
		{name: "body_missing 标记行", in: ta.ErrBodyMissing, wantStatus: http.StatusNotFound, wantCode: "body_missing", wantMissing: true},
		{name: "定位键不一致", in: obssvc.ErrTraceBodyInconsistent, wantStatus: http.StatusInternalServerError, wantCode: "body_ref_inconsistent"},
		{name: "读侧未配置", in: obssvc.ErrTraceBodyDisabled, wantStatus: http.StatusServiceUnavailable, wantCode: "trace_body_disabled"},
		{name: "未归档", in: ta.ErrNotArchived, wantStatus: http.StatusInternalServerError, wantCode: "not_archived"},
		{name: "footer 损坏", in: ta.ErrFooterCorrupt, wantStatus: http.StatusInternalServerError, wantCode: "part_corrupt"},
		{name: "trailer 损坏", in: ta.ErrTrailerCorrupt, wantStatus: http.StatusInternalServerError, wantCode: "part_corrupt"},
		// D53：`ErrPartNotFound` 全仓无生产者，规格 §8.1 的 410 行**不可达** ⇒
		// 本计划不映射任何状态码，落到 default 的 500/internal_error（绝不反推 410）。
		{name: "ErrPartNotFound 无生产者不得映射成410", in: ta.ErrPartNotFound, wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
		{name: "客户端断开", in: context.Canceled, wantStatus: http.StatusRequestTimeout, wantCode: "client_closed"},
		{name: "读超时", in: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantCode: "read_timeout"},
		{name: "未知错误兜底", in: errors.New("boom"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := obshandler.MapTraceBodyError(tt.in)
			require.Equal(t, tt.wantStatus, status, "status 是契约的一部分")
			require.Equal(t, tt.wantCode, resp.Error.Code, "code 是机器可读契约")
			require.Equal(t, tt.wantMissing, resp.Error.BodyMissing, "body_missing 只对标记行为 true（规格 §8.1）")

			if tt.wantCode == "" {
				// 成功路径（nil）：**不得**产出任何诊断体 ——
				// 200 的 body 是服务层返回的正文原字节（任务 5 的 c.Data），
				// 不是本函数的返回体。此断言防「把服务层结果包壳」。
				require.Equal(t, obshandler.TraceBodyErrorResponse{}, resp, "nil 必须是零值诊断体")
				return
			}
			require.NotEmpty(t, resp.Error.Type, "错误行必须有机器可读的 type")
			require.NotEmpty(t, resp.Error.Message, "错误行必须有 message")
			if tt.wantCode != "body_missing" {
				require.False(t, resp.Error.BodyMissing, "只有 body_missing 行可以置 true")
			}
		})
	}
}

// TestMapTraceBodyErrorRequiresWrapping 是【承重】断言：每一行都必须用 `%w` 包装后的
// `errors.Is` 判定，而不是 `==` 比较 —— 否则服务层包装过的哨兵会全部落进兜底。
func TestMapTraceBodyErrorRequiresWrapping(t *testing.T) {
	tests := []struct {
		name       string
		in         error
		wantStatus int
		wantCode   string
	}{
		{name: "包装后的未归档", in: fmt.Errorf("wrap: %w", ta.ErrNotArchived), wantStatus: http.StatusInternalServerError, wantCode: "not_archived"},
		{name: "包装后的 body_missing", in: fmt.Errorf("wrap: %w", ta.ErrBodyMissing), wantStatus: http.StatusNotFound, wantCode: "body_missing"},
		{name: "包装后的 footer 损坏", in: fmt.Errorf("wrap: %w", ta.ErrFooterCorrupt), wantStatus: http.StatusInternalServerError, wantCode: "part_corrupt"},
		{name: "包装后的 trailer 损坏", in: fmt.Errorf("wrap: %w", ta.ErrTrailerCorrupt), wantStatus: http.StatusInternalServerError, wantCode: "part_corrupt"},
		{name: "包装后的定位键不一致", in: fmt.Errorf("wrap: %w", obssvc.ErrTraceBodyInconsistent), wantStatus: http.StatusInternalServerError, wantCode: "body_ref_inconsistent"},
		{name: "包装后的读侧未配置", in: fmt.Errorf("wrap: %w", obssvc.ErrTraceBodyDisabled), wantStatus: http.StatusServiceUnavailable, wantCode: "trace_body_disabled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := obshandler.MapTraceBodyError(tt.in)
			require.Equal(t, tt.wantStatus, status)
			require.Equal(t, tt.wantCode, resp.Error.Code)
		})
	}
}

// TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText 是【承重】断言：
// `ErrNotArchived` 的三种成因（冻结包 errors.go 自述）必须在 message 里**可分辨**，
// 因为运维处置不同 ⇒ 服务层原样 `%w` 包裹，映射层原样 `err.Error()`。
//
// 三个向量逐字按冻结包 reader.go 的实际拼装形态构造：
//
//	① reader.go:139  两文件都不存在   "%w: %s"
//	② reader.go:183  行号越界          "%w: %s（块内共 %d 行）"
//	③ reader.go:266  目标偏移未命中行首 "%w: %s（偏移 %d 未命中行首）"
func TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText(t *testing.T) {
	const ref = "2026-10-08/blue/part-000000#row=7"

	tests := []struct {
		name           string
		in             error
		wantSubstrings []string
	}{
		{
			name:           "两种副本都不存在",
			in:             fmt.Errorf("%w: %s", ta.ErrNotArchived, ref),
			wantSubstrings: []string{ref, "产物与暂存均不存在"},
		},
		{
			name:           "行号越界",
			in:             fmt.Errorf("%w: %s（块内共 %d 行）", ta.ErrNotArchived, ref, 3),
			wantSubstrings: []string{ref, "块内共 3 行"},
		},
		{
			name:           "目标偏移未命中行首",
			in:             fmt.Errorf("%w: %s（偏移 %d 未命中行首）", ta.ErrNotArchived, ref, 4096),
			wantSubstrings: []string{ref, "偏移 4096 未命中行首"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := obshandler.MapTraceBodyError(tt.in)
			require.Equal(t, http.StatusInternalServerError, status, "未归档是存储故障，不是 404")
			require.Equal(t, "not_archived", resp.Error.Code)
			for _, want := range tt.wantSubstrings {
				require.Contains(t, resp.Error.Message, want,
					"底层文本必须原样保留（不得替换成固定文案），否则三种成因不可分辨")
			}
		})
	}
}

// TestMapTraceBodyErrorSuccessCarriesNoPayload 是【承重】断言（E20 的重点）：
// 200 的 body 必须是**正文 JSON 原文**（服务层返回的字节，由任务 5 直接写出），
// 因此映射层对 nil **不得**产出任何诊断体/包壳 —— 一旦包壳，
// 任务 2 新增的 `TraceBodyResult.Ref` / `Source` 就会泄漏进响应。
func TestMapTraceBodyErrorSuccessCarriesNoPayload(t *testing.T) {
	status, resp := obshandler.MapTraceBodyError(nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, obshandler.TraceBodyErrorResponse{}, resp,
		"成功路径必须返回零值诊断体（调用方直接写 result.Body 原字节）")

	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	require.Equal(t, `{"error":{"type":"","code":"","message":"","body_missing":false}}`, string(raw),
		"成功路径的映射结果只能是既有四键错误体，不得出现任何包装键")
}

// TestTraceBodyErrorResponseKeySet 是【承重】断言（E20 的重点）：
// 诊断体的**响应键集合**逐字固定为 `{"error": {type, code, message, body_missing}}`，
// 不得出现 `ref` / `source` / `body` / `trace` 等本计划可能引入的包装键或定位键分量
// （防 `TraceBodyResult.Ref` 经诊断体泄漏）。
func TestTraceBodyErrorResponseKeySet(t *testing.T) {
	_, resp := obshandler.MapTraceBodyError(ta.ErrBodyMissing)
	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &top))
	require.Equal(t, []string{"error"}, sortedKeys(top), "顶层只能有 error 一个键")

	var detail map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(top["error"], &detail))
	require.Equal(t, []string{"body_missing", "code", "message", "type"}, sortedKeys(detail),
		"诊断体只能有四个既有键")

	// 定位键四分量 + 来源标识都不得出现在任何一层
	for _, forbidden := range []string{"ref", "source", "body", "trace", "dt", "inst", "part", "row"} {
		_, inTop := top[forbidden]
		_, inDetail := detail[forbidden]
		require.False(t, inTop || inDetail, "诊断体不得出现 %q 键（定位键/来源泄漏）", forbidden)
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
