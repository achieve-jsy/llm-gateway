package ehrclient

import (
	"context"
	"sync"
)

// TokenManager 管理长期 API Key，支持运行时热加载
type TokenManager struct {
	mu            sync.RWMutex
	token         string
	reloadHook    func() string // 配置变更后回调用以刷新 token
	onRotatedHook func(newToken string)
}

// NewTokenManager 初始化，初始从 env / 配置读取
func NewTokenManager(initial string) *TokenManager {
	return &TokenManager{token: initial}
}

// SetReloadHook 设置配置刷新时的回调：外部配置变更时调用此回调获取新值
func (m *TokenManager) SetReloadHook(fn func() string) {
	m.mu.Lock()
	m.reloadHook = fn
	m.mu.Unlock()
}

// SetOnRotatedHook 当 token 真正发生轮换时通知上层（如记录日志）
func (m *TokenManager) SetOnRotatedHook(fn func(newToken string)) {
	m.mu.Lock()
	m.onRotatedHook = fn
	m.mu.Unlock()
}

// Acquire 返回当前 token（读锁）
func (m *TokenManager) Acquire(ctx context.Context) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.token == "" {
		return "", &ErrConfig{}
	}
	return m.token, nil
}

// ReloadFromConfig 触发热加载：从 reloadHook 获取最新 token 并替换
func (m *TokenManager) ReloadFromConfig() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reloadHook == nil {
		return
	}
	newToken := m.reloadHook()
	if newToken != "" && newToken != m.token {
		m.token = newToken
		if m.onRotatedHook != nil {
			m.onRotatedHook(newToken)
		}
	}
}

// Replace 直接替换 token（测试 / 手动刷新用）
func (m *TokenManager) Replace(token string) {
	m.mu.Lock()
	m.token = token
	m.mu.Unlock()
}
