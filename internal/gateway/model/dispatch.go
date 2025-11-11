package model

import "github.com/gin-gonic/gin"

const ctxKeyDispatch = "dispatch"

type DispatchContext struct {
	GroupName         string
	RoutedModelID     string
	FailoverAttempted bool
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
