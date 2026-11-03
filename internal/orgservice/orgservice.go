// Package orgservice 提供组织架构领域服务：
// - 四级组织实体管理（公司→工作室→部门→员工）
// - 部门↔工作室映射维护
// - Bootstrap 初始管理员
// - RBAC 四角色派发与鉴权
//
// 设计原则：内嵌 control-plane 进程，共享 DB/Redis 连接，通过 control-plane router 对外暴露 API。
package orgservice

import (
	"context"
	"os"

	"github.com/example/llm-gateway/internal/orgservice/bootstrap"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/example/llm-gateway/internal/orgservice/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Container 组织服务容器（统一依赖注入入口）
type Container struct {
	OrgSvc     *service.OrgService
	StudioSvc  *service.StudioService
	MappingSvc *service.MappingService
	RoleSvc    *service.RoleService
	Bootstrap  *bootstrap.Checker
	RBAC       *rbac.Checker // 共享权限检查器，所有 handler 通过此实例调用
}

// New 创建并初始化 orgservice 容器
// bootstrapAdmins: BOOTSTRAP_ADMIN 环境变量值（逗号分隔）
func New(db *gorm.DB, rdb *redis.Client, bootstrapAdmins string) *Container {
	// repositories
	studioRepo := repository.NewStudioRepo(db)
	deptRepo := repository.NewDeptRepo(db)
	mappingRepo := repository.NewMappingRepo(db)
	roleRepo := repository.NewRoleRepo(db)
	userOrgRepo := repository.NewUserOrgRepo(db)

	// bootstrap
	bs := bootstrap.NewChecker(bootstrapAdmins, roleRepo)
	bs.Init(context.Background())

	// services
	orgSvc := service.NewOrgService(db, studioRepo, deptRepo, mappingRepo, userOrgRepo, roleRepo, bs)
	studioSvc := service.NewStudioService(studioRepo)
	mappingSvc := service.NewMappingService(mappingRepo)
	roleSvc := service.NewRoleService(roleRepo, rdb)

	rbacChecker := rbac.NewChecker(roleRepo, userOrgRepo)

	return &Container{
		OrgSvc:     orgSvc,
		StudioSvc:  studioSvc,
		MappingSvc: mappingSvc,
		RoleSvc:    roleSvc,
		Bootstrap:  bs,
		RBAC:       rbacChecker,
	}
}

// BootstrapAdmins 从环境变量读取 BOOTSTRAP_ADMIN
func BootstrapAdmins() string {
	return os.Getenv("BOOTSTRAP_ADMIN")
}
