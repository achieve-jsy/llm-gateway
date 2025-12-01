package rbac

import "github.com/gin-gonic/gin"

const actorKey = "_rbac_actor"

// GetActor 从 gin.Context 取出当前请求者的 Actor（由 Guard middleware 注入）。
// 若 Guard 未注入（测试路由/内部路由），返回零值 Actor（无任何权限）。
func GetActor(c *gin.Context) Actor {
	if v, ok := c.Get(actorKey); ok {
		if a, ok := v.(Actor); ok {
			return a
		}
	}
	return Actor{}
}

// SetActor 由 Guard middleware 调用，将 Actor 注入 gin.Context。
func SetActor(c *gin.Context, a Actor) {
	c.Set(actorKey, a)
}
