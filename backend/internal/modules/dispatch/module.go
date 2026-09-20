package dispatch

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 班组派工模块, 负责班组维护、派工/改派流转与派工概览。
type Module struct {
	repository *Repository
	service    *Service
	handler    *Handler
}

// New 构造班组派工模块, faults 为故障模块端口, lamps 为路灯模块端口(提供道路清单)。
func New(db *gorm.DB, faults FaultPort, lamps LampRoadPort) *Module {
	repository := NewRepository(db)
	service := NewService(repository, faults, lamps)
	return &Module{
		repository: repository,
		service:    service,
		handler:    NewHandler(service),
	}
}

// Repository 暴露仓储, 供其它模块装配。
func (m *Module) Repository() *Repository { return m.repository }

// Name 实现 module.Module 接口。
func (m *Module) Name() string { return "班组派工" }

// Models 实现 module.Module 接口。
func (m *Module) Models() []any { return []any{&Team{}, &DispatchRecord{}} }

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

	dispatches := api.Group("/dispatches")
	{
		dispatches.GET("", m.handler.ListRecords)
		dispatches.POST("", m.handler.Dispatch)
		dispatches.GET("/meta", m.handler.Metadata)
		dispatches.GET("/overview", m.handler.Overview)
		dispatches.GET("/suggest/:faultId", m.handler.Suggest)
		dispatches.GET("/:id", m.handler.GetRecord)
		dispatches.POST("/:id/reassign", m.handler.Reassign)
		dispatches.POST("/:id/finish", m.handler.Finish)
	}
}
