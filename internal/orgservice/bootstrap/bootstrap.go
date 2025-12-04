package bootstrap

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
)

// State bootstrap 通道状态
type State int

const (
	StateUnknown State = iota
	StateStandby // 无 admin，等待白名单登录
	StateClosed  // 已有 admin，bootstrap 通道关闭
)

// Checker Bootstrap 初始管理员检查器
type Checker struct {
	whitelist map[string]bool // email → true
	roleRepo  *repository.RoleRepo
	mu        sync.Mutex
	state     State
}

// NewChecker 创建 Bootstrap Checker
// admins 为逗号分隔的邮箱/工号白名单
func NewChecker(admins string, roleRepo *repository.RoleRepo) *Checker {
	wl := make(map[string]bool)
	for _, a := range strings.Split(admins, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			wl[strings.ToLower(a)] = true
		}
	}
	return &Checker{
		whitelist: wl,
		roleRepo:  roleRepo,
		state:     StateUnknown,
	}
}

// Init 服务启动时调用，检测系统是否已有 admin
func (c *Checker) Init(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hasAdmin, err := c.roleRepo.HasAdmins()
	if err != nil {
		slog.Error("[bootstrap] failed to check admin count", "error", err)
		c.state = StateUnknown
		return
	}
	if hasAdmin {
		c.state = StateClosed
		slog.Info("[bootstrap] system already has admin, bootstrap channel closed")
	} else {
		c.state = StateStandby
		if len(c.whitelist) == 0 {
			slog.Warn("[bootstrap] system has no admin and BOOTSTRAP_ADMIN is not configured")
		} else {
			slog.Info("[bootstrap] bootstrap channel standby", "whitelist_size", len(c.whitelist))
		}
	}
}

// CheckAndGrant 登录后调用：若满足条件则授予 admin
// 返回 true 表示本次授予了 admin
func (c *Checker) CheckAndGrant(ctx context.Context, email, userID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 通道已关闭
	if c.state == StateClosed {
		return false
	}

	// 未在白名单
	if !c.whitelist[strings.ToLower(email)] {
		return false
	}

	// 再次检查是否已有 admin（并发安全）
	hasAdmin, err := c.roleRepo.HasAdmins()
	if err != nil {
		slog.Warn("[bootstrap] failed to check admin before grant", "error", err)
		return false
	}
	if hasAdmin {
		c.state = StateClosed
		return false
	}

	// 授予 admin
	role := &model.UserRole{
		UserID:    userID,
		Role:      model.RoleAdmin,
		ScopeType: model.ScopeCompany,
		ScopeID:   "",
		GrantedBy: "system:bootstrap",
		ActorID:   "system:bootstrap",
		CreatedAt: time.Now(),
	}
	if err := c.roleRepo.Grant(role); err != nil {
		slog.Error("[bootstrap] failed to grant admin", "user_id", userID, "error", err)
		return false
	}

	c.state = StateClosed
	slog.Info("[bootstrap] admin granted via bootstrap", "user_id", userID, "email", email)
	return true
}

// IsStandby 是否处于待命态
func (c *Checker) IsStandby() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == StateStandby
}
