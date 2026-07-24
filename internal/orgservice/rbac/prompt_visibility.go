package rbac

import "github.com/gin-gonic/gin"

// ActorUserID 返回当前请求的登录用户 ID：
// 优先用 Guard 注入的 Actor（已按服务端 user_roles 校验），
// 回退到 JWT 中间件写入的 user_id（/my/* 等未挂 Guard 的路由）。
func ActorUserID(c *gin.Context) string {
	if a := GetActor(c); a.UserID != "" {
		return a.UserID
	}
	return c.GetString("user_id")
}

// CanViewPrompt 判定当前请求者能否查看归属 ownerUserID 的提示词正文。
//
// 产品口径：
//   - 平台 admin（IsGlobal）例外，可见全部
//   - 程序（服务）Key 发起的调用（isProgramKey=true）提示词一律不下发，
//     即便是该 Key 的持有者本人也看不到
//   - 其余情况只能看自己的记录（viewer == owner）
//
// 归属未知（历史数据 user_id 为空）或请求者身份缺失时一律不可见（fail closed）。
//
// 注意：管辖范围（studio/dept）只决定「看得到这条记录」，
// 不决定「能否看到记录里的提示词正文」。
func CanViewPrompt(c *gin.Context, ownerUserID string, isProgramKey bool) bool {
	if GetActor(c).IsGlobal {
		return true
	}
	if isProgramKey {
		return false
	}
	viewer := ActorUserID(c)
	if viewer == "" || ownerUserID == "" {
		return false
	}
	return viewer == ownerUserID
}
