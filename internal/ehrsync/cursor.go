package ehrsync

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// CursorManager 管理增量同步游标（每个实体 + orgId 一个 Redis key）
//
// Key 命名：ehr:cursor:{entity}:{orgId}
// Value:   lastModifyDate 字符串（大群 EHR 的时区字符串）
type CursorManager struct {
	rdb *redis.Client
}

// NewCursorManager 使用 redis 客户端创建
func NewCursorManager(rdb *redis.Client) *CursorManager {
	return &CursorManager{rdb: rdb}
}

// Key 生成 Redis key
func CursorKey(entity, orgID string) string {
	return "ehr:cursor:" + entity + ":" + orgID
}

// Get 读取当前 cursor，不存在时返回空字符串
func (m *CursorManager) Get(ctx context.Context, entity, orgID string) (string, error) {
	v, err := m.rdb.Get(ctx, CursorKey(entity, orgID)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return v, err
}

// Set 写入 cursor（原子）
func (m *CursorManager) Set(ctx context.Context, entity, orgID, value string) error {
	return m.rdb.Set(ctx, CursorKey(entity, orgID), value, 0).Err()
}

// Reset 删除 cursor（强制下次执行全量）
func (m *CursorManager) Reset(ctx context.Context, entity, orgID string) error {
	return m.rdb.Del(ctx, CursorKey(entity, orgID)).Err()
}

// UpdateIfGreater 仅在 newValue 比当前值更大时才更新（按字典序比较）
// 用于 lastModifyDate 这种单调时间字符串
// 使用 Lua 脚本实现原子操作（避免 GET+SET 的并发窗口）
func (m *CursorManager) UpdateIfGreater(ctx context.Context, entity, orgID, newValue string) error {
	if newValue == "" {
		return nil
	}
	const script = `
		local cur = redis.call('GET', KEYS[1])
		if cur == false or ARGV[1] > cur then
			redis.call('SET', KEYS[1], ARGV[1])
			return 1
		end
		return 0
	`
	_, err := m.rdb.Eval(ctx, script, []string{CursorKey(entity, orgID)}, newValue).Result()
	return err
}
