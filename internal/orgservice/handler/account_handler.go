package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
)

var errCreatedRange = errors.New("created_from/created_to 需为 YYYY-MM-DD 格式")

// AccountHandler 账号页（一人一行）：/api/v1/admin/org/accounts
type AccountHandler struct {
	repo *repository.AccountRepo
}

func NewAccountHandler(repo *repository.AccountRepo) *AccountHandler {
	return &AccountHandler{repo: repo}
}

// List GET /api/v1/admin/org/accounts?keyword=&role=&studio_id=&dept_id=&created_from=&created_to=&page=&page_size=
// 一人一行 + 主角色 + 全部授权明细 + 四种角色人数统计（按人算，不是按授权条数算）。
func (h *AccountHandler) List(c *gin.Context) {
	page, pageSize := parsePagination(c)

	role := strings.TrimSpace(c.Query("role"))
	if role != "" && !validAccountRole(role) {
		c.JSON(http.StatusBadRequest, errorResp("invalid_filter", "role 只支持 admin/studio_admin/dept_admin/user", "invalid_filter"))
		return
	}

	createdFrom, createdTo, err := parseCreatedRange(c.Query("created_from"), c.Query("created_to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_filter", err.Error(), "invalid_filter"))
		return
	}

	filter := repository.AccountListFilter{
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		Role:        role,
		StudioID:    strings.TrimSpace(c.Query("studio_id")),
		DeptID:      strings.TrimSpace(c.Query("dept_id")),
		CreatedFrom: createdFrom,
		CreatedTo:   createdTo,
		Page:        page,
		PageSize:    pageSize,
	}
	// 非平台管理员只看自己管辖范围（与 /org/all-roles 的数据范围语义一致，
	// 这里是全公司人员清单含邮箱，不能给所有管理角色全量放开）。
	if actor := rbac.GetActor(c); !actor.IsGlobal {
		filter.Restricted = true
		filter.ScopeStudioIDs = actor.StudioIDs
		filter.ScopeDeptIDs = actor.DeptIDs
	}

	accounts, total, counts, err := h.repo.ListAccounts(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", err.Error(), "internal"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"accounts":    accounts,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"role_counts": counts,
	})
}

func validAccountRole(role string) bool {
	switch role {
	case repository.RoleKeyAdmin, repository.RoleKeyStudioAdmin, repository.RoleKeyDeptAdmin, repository.RoleKeyUser:
		return true
	}
	return false
}

// parseCreatedRange 解析创建时间区间（YYYY-MM-DD，两端都含当日）。
// 返回左闭右开区间：to = 解析日期 + 1 天，避免同一天创建的人被排除。
func parseCreatedRange(from, to string) (*time.Time, *time.Time, error) {
	var fromAt, toAt *time.Time
	if s := strings.TrimSpace(from); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			return nil, nil, errCreatedRange
		}
		fromAt = &t
	}
	if s := strings.TrimSpace(to); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			return nil, nil, errCreatedRange
		}
		next := t.AddDate(0, 0, 1)
		toAt = &next
	}
	return fromAt, toAt, nil
}
