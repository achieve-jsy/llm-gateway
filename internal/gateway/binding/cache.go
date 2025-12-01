package binding

import (
	"sync"
	"time"
)

const keyGroupCacheTTL = 60 * time.Second

type keyGroupEntry struct {
	groups []BoundGroup
	expiry time.Time
}

// KeyGroupCache 进程内 Key-Group 绑定结果缓存。
// TTL 60s，Key 被禁用/删除时通过 Invalidate 主动清除。
// 使用 sync.Map 保证并发安全，零锁读路径。
type KeyGroupCache struct {
	m sync.Map // keyID(string) → *keyGroupEntry
}

// Get 返回 keyID 对应的绑定组列表。未命中或已过期返回 nil, false。
func (c *KeyGroupCache) Get(keyID string) ([]BoundGroup, bool) {
	v, ok := c.m.Load(keyID)
	if !ok {
		return nil, false
	}
	e := v.(*keyGroupEntry)
	if time.Now().After(e.expiry) {
		c.m.Delete(keyID)
		return nil, false
	}
	return e.groups, true
}

// Set 写入 keyID 对应的绑定组列表，TTL 60s。
func (c *KeyGroupCache) Set(keyID string, groups []BoundGroup) {
	c.m.Store(keyID, &keyGroupEntry{
		groups: groups,
		expiry: time.Now().Add(keyGroupCacheTTL),
	})
}

// Invalidate 主动删除 keyID 对应的缓存条目（Key 禁用/删除事件触发）。
func (c *KeyGroupCache) Invalidate(keyID string) {
	c.m.Delete(keyID)
}
