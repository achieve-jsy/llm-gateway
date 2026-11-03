package handler

import (
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
