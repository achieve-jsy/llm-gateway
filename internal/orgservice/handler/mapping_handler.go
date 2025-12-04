package handler

import (
	"errors"

	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/example/llm-gateway/internal/orgservice/service"
	"github.com/gin-gonic/gin"
)

// MappingHandler 映射维护 HTTP handler
type MappingHandler struct {
	mappingSvc *service.MappingService
	deptRepo   *repository.DeptRepo
}

func NewMappingHandler(mappingSvc *service.MappingService, deptRepo *repository.DeptRepo) *MappingHandler {
	return &MappingHandler{mappingSvc: mappingSvc, deptRepo: deptRepo}
}

// ListMappings GET /api/v1/org/dept-studio-map
func (h *MappingHandler) ListMappings(c *gin.Context) {
	page, pageSize := parsePagination(c)
	mappings, total, err := h.mappingSvc.List(c.Request.Context(), page, pageSize)
	if err != nil {
		c.JSON(500, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(200, gin.H{"mappings": mappings, "total": total, "page": page, "page_size": pageSize})
}

// ListDepartments GET /api/v1/org/departments
func (h *MappingHandler) ListDepartments(c *gin.Context) {
	status := c.Query("status")
	page, pageSize := parsePagination(c)
	depts, total, err := h.deptRepo.List(status, page, pageSize)
	if err != nil {
		c.JSON(500, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(200, gin.H{"departments": depts, "total": total})
}

// ListUnmappedDepts GET /api/v1/org/departments/unmapped
func (h *MappingHandler) ListUnmappedDepts(c *gin.Context) {
	page, pageSize := parsePagination(c)
	depts, total, err := h.deptRepo.ListPending(page, pageSize)
	if err != nil {
		c.JSON(500, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(200, gin.H{"departments": depts, "total": total})
}

// ImportMappings POST /api/v1/org/dept-studio-map/import
func (h *MappingHandler) ImportMappings(c *gin.Context) {
	dryRun := c.Query("dry_run") == "true"
	operatorID, _ := c.Get("user_id")
	updatedBy, _ := operatorID.(string)

	// 检查文件大小（1MB）
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, errorResp("invalid_request", "缺少 file 字段", "bad_request"))
		return
	}
	defer file.Close()
	if header.Size > 1<<20 {
		c.JSON(413, errorResp("org.import.file_too_large", "文件大小超过 1MB 限制", "org.import.file_too_large"))
		return
	}

	// 解析 CSV
	rows, err := h.mappingSvc.ParseCSV(file)
	if err != nil {
		if errors.Is(err, service.ErrImportTooManyRows) {
			c.JSON(422, errorResp("org.import.too_many_rows", err.Error(), "org.import.too_many_rows"))
			return
		}
		c.JSON(422, errorResp("invalid_request", err.Error(), "bad_request"))
		return
	}

	if dryRun {
		diff, err := h.mappingSvc.DryRun(c.Request.Context(), rows, updatedBy)
		if err != nil {
			h.handleImportError(c, err)
			return
		}
		c.JSON(200, gin.H{"dry_run": true, "diff": diff})
		return
	}

	result, err := h.mappingSvc.Import(c.Request.Context(), rows, updatedBy)
	if err != nil {
		h.handleImportError(c, err)
		return
	}
	c.JSON(200, gin.H{"dry_run": false, "updated": result.Updated, "skipped": result.Skipped, "backfilled": result.Backfilled})
}

// SetDeptStudio PUT /api/v1/org/departments/:id/studio
// studio_id 为空字符串时表示"移出工作室"，部门回到 pending_studio 状态。
func (h *MappingHandler) SetDeptStudio(c *gin.Context) {
	deptID := c.Param("id")
	var req struct {
		StudioID string `json:"studio_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, errorResp("invalid_request", err.Error(), "bad_request"))
		return
	}
	operatorID, _ := c.Get("user_id")
	updatedBy, _ := operatorID.(string)

	if err := h.mappingSvc.SetDeptStudio(c.Request.Context(), deptID, req.StudioID, updatedBy); err != nil {
		if errors.Is(err, service.ErrStudioNotFound) {
			c.JSON(404, errorResp("org.studio.not_found", err.Error(), "org.studio.not_found"))
			return
		}
		c.JSON(500, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(200, gin.H{"dept_id": deptID, "studio_id": req.StudioID, "source": "manual"})
}

func (h *MappingHandler) handleImportError(c *gin.Context, err error) {
	var valErr *service.ImportValidationError
	if errors.As(err, &valErr) {
		c.JSON(422, gin.H{
			"code":         "org.import.invalid_studio",
			"message":      err.Error(),
			"invalid_rows": valErr.InvalidRows,
		})
		return
	}
	c.JSON(500, errorResp("internal_error", err.Error(), "internal"))
}
