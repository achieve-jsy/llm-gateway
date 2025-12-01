package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/service"
	"github.com/gin-gonic/gin"
)

// EmployeeImportHandler 员工花名册批量导入
type EmployeeImportHandler struct {
	svc *service.EmployeeImportService
}

func NewEmployeeImportHandler(svc *service.EmployeeImportService) *EmployeeImportHandler {
	return &EmployeeImportHandler{svc: svc}
}

// Import POST /api/v1/org/employees/import?dry_run=true
// Content-Type: multipart/form-data
//   file       : CSV 或 XLSX 文件
//   overrides  : JSON 字符串 {"<row_index>": "<user_id>"} （仅正式导入时有效）
func (h *EmployeeImportHandler) Import(c *gin.Context) {
	dryRun := c.Query("dry_run") == "true"
	operatorID, _ := c.Get("user_id")
	opID, _ := operatorID.(string)

	// 限制 10 MB
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_request", "文件过大或格式错误", "bad_request"))
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_request", "缺少 file 字段", "bad_request"))
		return
	}
	defer file.Close()

	rows, err := h.svc.ParseFile(file, header.Filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResp("parse_error", err.Error(), "bad_request"))
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusBadRequest, errorResp("empty_file", "文件中没有有效数据行", "bad_request"))
		return
	}

	// ── dry_run：仅预览，不写库 ───────────────────────────────────────────────
	if dryRun {
		preview, err := h.svc.Preview(c.Request.Context(), rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, errorResp("preview_error", err.Error(), "internal_error"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"rows":  preview,
			"total": len(preview),
		}})
		return
	}

	// ── 正式导入 ──────────────────────────────────────────────────────────────
	// 解析手动覆盖映射
	overrides := map[int]string{}
	if raw := c.PostForm("overrides"); raw != "" {
		rawMap := map[string]string{}
		if jsonErr := json.Unmarshal([]byte(raw), &rawMap); jsonErr == nil {
			for k, v := range rawMap {
				var idx int
				if _, scanErr := parseIntStr(k, &idx); scanErr == nil {
					overrides[idx] = v
				}
			}
		}
	}

	// 同时解析 JSON body 中的 overrides（兼容两种提交方式）
	if bodyRaw := c.PostForm("overrides_json"); bodyRaw != "" {
		var req model.EmployeeImportRequest
		if jsonErr := json.Unmarshal([]byte(bodyRaw), &req); jsonErr == nil {
			for k, v := range req.Overrides {
				overrides[k] = v
			}
		}
	}

	summary, err := h.svc.Import(c.Request.Context(), rows, opID, overrides)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("import_error", err.Error(), "internal_error"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": summary})
}

func parseIntStr(s string, out *int) (string, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return s, err
	}
	*out = n
	return s, nil
}
