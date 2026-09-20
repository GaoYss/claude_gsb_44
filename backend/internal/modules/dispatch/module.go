package dispatch

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 班组派工模块, 维护班组承接能力并负责故障派工与改派。
type Module struct {
	service *Service
	handler *Handler
}

// New 构造班组派工模块, faults 为故障模块提供的端口实现。
func New(db *gorm.DB, faults FaultPort) *Module {
	teamRepository := NewTeamRepository(db)
	dispatchRepository := NewDispatchRepository(db)
	service := NewService(teamRepository, dispatchRepository, faults)
	return &Module{
		service: service,
		handler: NewHandler(service),
	}
}

// Service 暴露业务服务, 供故障模块装配闭环监听端口。
func (m *Module) Service() *Service { return m.service }

// Name 实现 module.Module 接口。
func (m *Module) Name() string { return "班组派工" }

// Models 实现 module.Module 接口。
func (m *Module) Models() []any { return []any{&Team{}, &Dispatch{}} }

// RegisterRoutes 实现 module.Module 接口。
func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	teams := api.Group("/teams")
	{
		teams.GET("", m.handler.ListTeams)
		teams.POST("", m.handler.CreateTeam)
		teams.GET("/options", m.handler.TeamOptions)
		teams.GET("/:id", m.handler.GetTeam)
		teams.PUT("/:id", m.handler.UpdateTeam)
		teams.DELETE("/:id", m.handler.DeleteTeam)
	}

	group := api.Group("/dispatches")
	{
		group.GET("", m.handler.List)
		group.POST("", m.handler.Create)
		group.GET("/candidates", m.handler.Candidates)
		group.GET("/overview", m.handler.Overview)
		group.GET("/fault/:faultId", m.handler.ListByFault)
		group.GET("/:id", m.handler.Get)
		group.POST("/:id/reassign", m.handler.Reassign)
	}
}
