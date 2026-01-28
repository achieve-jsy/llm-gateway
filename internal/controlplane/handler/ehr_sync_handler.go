package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/ehrsync"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// EhrSyncRepoInterface 抽象数据层
type EhrSyncRepoInterface interface {
	ListAllOrganizations(ctx context.Context) ([]model.EhrOrganization, error)
	ListAllCompanies(ctx context.Context) ([]model.EhrCompany, error)
	ListAllDepartments(ctx context.Context) ([]model.EhrDepartment, error)
	ListAllEmployees(ctx context.Context) ([]model.EhrEmployee, error)
}

// EhrSyncSchedulerInterface 抽象 scheduler
type EhrSyncSchedulerInterface interface {
	TriggerManual(ctx context.Context, syncType string) (string, error)
	IsRunning() bool
}

// EhrSyncHandler EHR 同步 Handler
type EhrSyncHandler struct {
	db        *gorm.DB
	repo      EhrSyncRepoInterface
	scheduler EhrSyncSchedulerInterface
}

// NewEhrSyncHandler 创建
func NewEhrSyncHandler(db *gorm.DB, repo EhrSyncRepoInterface, scheduler EhrSyncSchedulerInterface) *EhrSyncHandler {
	return &EhrSyncHandler{db: db, repo: repo, scheduler: scheduler}
}

// ----------------------- 查询 -----------------------

// ListOrganizations GET /api/v1/ehr/organizations
func (h *EhrSyncHandler) ListOrganizations(c *gin.Context) {
	items, err := h.repo.ListAllOrganizations(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": len(items)})
}

// ListDepartments GET /api/v1/ehr/departments?org_id=xxx&name=xxx
func (h *EhrSyncHandler) ListDepartments(c *gin.Context) {
	orgID := c.Query("org_id")
	name := c.Query("name")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	q := h.db.WithContext(c.Request.Context()).Model(&model.EhrDepartment{})
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if name != "" {
		q = q.Where("department_name ILIKE ?", "%"+name+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}

	var items []model.EhrDepartment
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("department_name ASC").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// ListEmployees GET /api/v1/ehr/employees?org_id=xxx&dept_id=xxx&name=xxx&person_name=xxx
func (h *EhrSyncHandler) ListEmployees(c *gin.Context) {
	orgID := c.Query("org_id")
	deptID := c.Query("dept_id")
	name := c.Query("name")
	personName := c.Query("person_name")
	statusStr := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	q := h.db.WithContext(c.Request.Context()).Model(&model.EhrEmployee{})
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if deptID != "" {
		q = q.Where("department_id = ?", deptID)
	}
	if personName != "" {
		q = q.Where("person_name ILIKE ?", "%"+personName+"%")
	} else if name != "" {
		q = q.Where("person_name ILIKE ? OR appoint_name ILIKE ?", "%"+name+"%", "%"+name+"%")
	}
	if statusStr != "" {
		if v, err := strconv.Atoi(statusStr); err == nil {
			q = q.Where("person_status = ?", v)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}

	var items []model.EhrEmployee
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("person_name ASC").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// ----------------------- 同步 -----------------------

// TriggerSync POST /api/v1/ehr/sync  body: {"sync_type":"incremental|full"}
func (h *EhrSyncHandler) TriggerSync(c *gin.Context) {
	if h.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"type": "service_unavailable",
			"message": "ehr sync scheduler not initialized",
			"code":  "service_unavailable",
		}})
		return
	}
	var req struct {
		SyncType string `json:"sync_type"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.SyncType == "" {
		req.SyncType = "incremental"
	}
	if req.SyncType != "incremental" && req.SyncType != "full" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type":    "invalid_request",
			"message": "sync_type must be incremental or full",
			"code":    CodeBadRequest,
		}})
		return
	}

	batchID, err := h.scheduler.TriggerManual(c.Request.Context(), req.SyncType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"data": gin.H{"batch_id": batchID, "sync_type": req.SyncType, "status": "running"},
	})
}

// GetStatus GET /api/v1/ehr/status
func (h *EhrSyncHandler) GetStatus(c *gin.Context) {
	// 最近一次 stat_snapshot
	var snap model.EhrSyncStatSnapshot
	_ = h.db.WithContext(c.Request.Context()).
		Order("synced_at DESC").Limit(1).Find(&snap).Error

	// 最近一次 execution_log
	var exec model.EhrSyncExecutionLog
	_ = h.db.WithContext(c.Request.Context()).
		Order("created_at DESC").Limit(1).Find(&exec).Error

	overrideCount, _ := ehrsync.CountOverride(h.db)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"scheduler_running":  h.scheduler != nil && h.scheduler.IsRunning(),
			"latest_snapshot":    snap,
			"latest_execution":   exec,
			"override_count":     overrideCount,
		},
	})
}

// ----------------------- Override 管理 -----------------------

type OverrideReq struct {
	Entity      string `json:"entity"`      // "department" | "employee"
	EntityID    string `json:"entity_id"`
	OrgID       string `json:"org_id"`
	Reason      string `json:"reason"`
}

// SetOverride POST /api/v1/ehr/override
func (h *EhrSyncHandler) SetOverride(c *gin.Context) {
	var req OverrideReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest,
		}})
		return
	}
	if req.EntityID == "" || req.OrgID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "invalid_request", "message": "entity_id and org_id are required", "code": CodeBadRequest,
		}})
		return
	}

	// 从上下文取当前用户（若有）
	operator := "unknown"
	if uid, exists := c.Get("user_id"); exists {
		operator, _ = uid.(string)
	}

	var err error
	switch req.Entity {
	case "department":
		err = ehrsync.SetOverride[*model.EhrDepartment](h.db, req.OrgID, req.EntityID, operator, req.Reason)
	case "employee":
		err = ehrsync.SetOverride[*model.EhrEmployee](h.db, req.OrgID, req.EntityID, operator, req.Reason)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "invalid_request",
			"message": "entity must be department or employee",
			"code":  CodeBadRequest,
		}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"entity":        req.Entity,
		"entity_id":     req.EntityID,
		"org_id":        req.OrgID,
		"override_at":   time.Now(),
		"override_by":   operator,
		"reason":        req.Reason,
	}})
}

// ReleaseOverride DELETE /api/v1/ehr/override/:entity/:org_id/:entity_id
func (h *EhrSyncHandler) ReleaseOverride(c *gin.Context) {
	entity := c.Param("entity")
	orgID := c.Param("org_id")
	entityID := c.Param("entity_id")
	if entity == "" || orgID == "" || entityID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "invalid_request", "message": "missing path params", "code": CodeBadRequest,
		}})
		return
	}

	var err error
	switch entity {
	case "department":
		err = ehrsync.ClearOverride[*model.EhrDepartment](h.db, orgID, entityID)
	case "employee":
		err = ehrsync.ClearOverride[*model.EhrEmployee](h.db, orgID, entityID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "invalid_request", "message": "entity must be department or employee", "code": CodeBadRequest,
		}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"type": "internal_error", "message": err.Error(), "code": CodeInternalError,
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"entity":    entity,
		"entity_id": entityID,
		"org_id":    orgID,
		"released":  true,
	}})
}

// ListOverrides GET /api/v1/ehr/overrides
func (h *EhrSyncHandler) ListOverrides(c *gin.Context) {
	var depts []model.EhrDepartment
	var empls []model.EhrEmployee
	_ = h.db.WithContext(c.Request.Context()).
		Where("manual_override_at IS NOT NULL").
		Order("manual_override_at DESC").Find(&depts).Error
	_ = h.db.WithContext(c.Request.Context()).
		Where("manual_override_at IS NOT NULL").
		Order("manual_override_at DESC").Find(&empls).Error
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"departments": depts,
		"employees":   empls,
		"total":       len(depts) + len(empls),
	}})
}

// ----------------------- 日志查询 -----------------------

// ListExecutionLogs GET /api/v1/ehr/logs/executions
func (h *EhrSyncHandler) ListExecutionLogs(c *gin.Context) {
	entity := c.Query("entity")
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	q := h.db.WithContext(c.Request.Context()).Model(&model.EhrSyncExecutionLog{})
	if entity != "" {
		q = q.Where("entity = ?", entity)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	_ = q.Count(&total).Error
	var items []model.EhrSyncExecutionLog
	_ = q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("created_at DESC").Find(&items).Error
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// ListChangeLogs GET /api/v1/ehr/logs/changes
func (h *EhrSyncHandler) ListChangeLogs(c *gin.Context) {
	entity := c.Query("entity")
	entityID := c.Query("entity_id")
	changeType := c.Query("change_type")
	batchID := c.Query("batch_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	q := h.db.WithContext(c.Request.Context()).Model(&model.EhrSyncChangeLog{})
	if entity != "" {
		q = q.Where("entity = ?", entity)
	}
	if entityID != "" {
		q = q.Where("entity_id = ?", entityID)
	}
	if changeType != "" {
		q = q.Where("change_type = ?", changeType)
	}
	if batchID != "" {
		q = q.Where("batch_id = ?", batchID)
	}
	var total int64
	_ = q.Count(&total).Error
	var items []model.EhrSyncChangeLog
	_ = q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("occurred_at DESC").Find(&items).Error
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// ListErrorLogs GET /api/v1/ehr/logs/errors
func (h *EhrSyncHandler) ListErrorLogs(c *gin.Context) {
	entity := c.Query("entity")
	batchID := c.Query("batch_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	q := h.db.WithContext(c.Request.Context()).Model(&model.EhrSyncErrorLog{})
	if entity != "" {
		q = q.Where("entity = ?", entity)
	}
	if batchID != "" {
		q = q.Where("batch_id = ?", batchID)
	}
	var total int64
	_ = q.Count(&total).Error
	var items []model.EhrSyncErrorLog
	_ = q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("occurred_at DESC").Find(&items).Error
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// ListDiffReports GET /api/v1/ehr/logs/diffs
func (h *EhrSyncHandler) ListDiffReports(c *gin.Context) {
	entity := c.Query("entity")
	orgID := c.Query("org_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	q := h.db.WithContext(c.Request.Context()).Model(&model.EhrSyncDiffReport{})
	if entity != "" {
		q = q.Where("entity = ?", entity)
	}
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	var total int64
	_ = q.Count(&total).Error
	var items []model.EhrSyncDiffReport
	_ = q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("created_at DESC").Find(&items).Error
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// ListSkipLogs GET /api/v1/ehr/logs/skips
func (h *EhrSyncHandler) ListSkipLogs(c *gin.Context) {
	entity := c.Query("entity")
	entityID := c.Query("entity_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	q := h.db.WithContext(c.Request.Context()).Model(&model.EhrSyncSkipLog{})
	if entity != "" {
		q = q.Where("entity = ?", entity)
	}
	if entityID != "" {
		q = q.Where("entity_id = ?", entityID)
	}
	var total int64
	_ = q.Count(&total).Error
	var items []model.EhrSyncSkipLog
	_ = q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("created_at DESC").Find(&items).Error
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}
