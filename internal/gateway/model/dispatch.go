package model

import "github.com/gin-gonic/gin"

const ctxKeyDispatch = "dispatch"

type DispatchContext struct {
	GroupName      string
	RoutedModelID  string
	// FailoverRounds 记录已发生的 failover 轮次（每次 HandleFailover 成功 +1）。
	FailoverRounds int
	// TriedModelIDs 记录本次请求中已失败并排除的模型 ID 列表（用于多轮 failover 去重）。
	TriedModelIDs  []string
}

// FailoverAttempted 向后兼容：返回是否已经发生过至少一次 failover。
func (dc *DispatchContext) FailoverAttempted() bool {
	return dc.FailoverRounds > 0
}

func SetDispatchContext(c *gin.Context, dc *DispatchContext) {
	c.Set(ctxKeyDispatch, dc)
}

func GetDispatchContext(c *gin.Context) *DispatchContext {
	if v, ok := c.Get(ctxKeyDispatch); ok {
		if dc, ok := v.(*DispatchContext); ok {
			return dc
		}
	}
	return nil
}
