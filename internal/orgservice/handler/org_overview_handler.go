package handler

import (
	"errors"
	"net/http"

	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// OrgOverviewHandler 工作室工作台头部 + 左树团队卡：/api/v1/admin/org/studios/:id/overview
type OrgOverviewHandler struct {
	repo *repository.OrgOverviewRepo
}

func NewOrgOverviewHandler(repo *repository.OrgOverviewRepo) *OrgOverviewHandler {
	return &OrgOverviewHandler{repo: repo}
}

// Studio GET /api/v1/admin/org/studios/:id/overview
// 响应是扁平的 StudioOverview（前端 store 直接取 res.data，不再套一层 data）。
func (h *OrgOverviewHandler) Studio(c *gin.Context) {
	overview, err := h.repo.StudioOverview(c.Request.Context(), c.Param("id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, errorResp("studio_not_found", "工作室不存在", "studio_not_found"))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, overview)
}
