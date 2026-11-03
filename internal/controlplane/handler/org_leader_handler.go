package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OrgLeaderHandler 组织负责人：列表读 + 手动新增。
// 手动新增是阶段 A 拆除手动维护后唯一重开的手动入口；语义是增量覆盖与保留
// （只增不删，见 specs/2026-11-03-org-leaders-incremental-design.md）。
type OrgLeaderHandler struct {
	leaders *orgrepo.EHRProjectionRepo
	roster  *repository.AIAppHubRepo
}

func NewOrgLeaderHandler(leaders *orgrepo.EHRProjectionRepo, roster *repository.AIAppHubRepo) *OrgLeaderHandler {
	return &OrgLeaderHandler{leaders: leaders, roster: roster}
}

// List GET /api/v1/admin/org/leaders —— 全量返回（百行量级），前端按节点分组。
func (h *OrgLeaderHandler) List(c *gin.Context) {
	rows, err := h.leaders.ListLeaders(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// Add POST /api/v1/admin/org/leaders —— keyword 按花名册解析（工号精确 / 姓名唯一），
// 负责人必须是真实存在的人，杜绝自由文本脏数据。
func (h *OrgLeaderHandler) Add(c *gin.Context) {
	var req struct {
		NodeType string `json:"node_type"`
		NodeID   string `json:"node_id"`
		Keyword  string `json:"keyword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if req.NodeType != orgmodel.LeaderNodeStudio && req.NodeType != orgmodel.LeaderNodeDept {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_node_type"})
		return
	}
	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_node_id"})
		return
	}
	ctx := c.Request.Context()
	ok, err := h.leaders.NodeExists(ctx, req.NodeType, nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "node_not_found"})
		return
	}
	emp, err := h.roster.FindEmployeeByKeyword(ctx, strings.TrimSpace(req.Keyword))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "employee_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	outcome, err := h.leaders.UpsertLeader(ctx, orgrepo.OrgLeaderInput{
		NodeType: req.NodeType, NodeID: nodeID,
		Name: emp.PersonName, Passport: emp.Passport, EmployeeID: emp.EmployeeID,
		Source: orgmodel.LeaderSourceManual,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"outcome": outcome}})
}
