package dispatch_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/dispatch"
	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
)

// harness 使用内存数据库装配真实模块, 用于验证派工/改派的跨模块业务流程。
type harness struct {
	lamps     *lamp.Service
	faults    *fault.Service
	dispatche *dispatch.Service
	db        *gorm.DB
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.AutoMigrate(&lamp.Lamp{}, &fault.Fault{}, &dispatch.Team{}, &dispatch.DispatchRecord{}))

	lampRepository := lamp.NewRepository(db)
	lampService := lamp.NewService(lampRepository)

	faultRepository := fault.NewRepository(db)
	faultService := fault.NewService(faultRepository, lampService)
	lampService.SetOpenFaultCounter(faultRepository)

	dispatchRepository := dispatch.NewRepository(db)
	dispatchService := dispatch.NewService(dispatchRepository, faultService, lampRepository)

	return &harness{lamps: lampService, faults: faultService, dispatche: dispatchService, db: db}
}

func (h *harness) createLamp(t *testing.T, code, road string) *lamp.Lamp {
	t.Helper()
	entity, err := h.lamps.Create(context.Background(), lamp.CreateRequest{
		Code:     code,
		Name:     "测试灯杆",
		RoadName: road,
		LampType: lamp.LampTypeLED,
	})
	require.NoError(t, err)
	return entity
}

func (h *harness) createFault(t *testing.T, lampID uint, faultType string) *fault.Fault {
	t.Helper()
	entity, err := h.faults.Create(context.Background(), fault.CreateRequest{
		LampID:      lampID,
		FaultType:   faultType,
		FaultLevel:  fault.LevelHigh,
		Source:      fault.SourceInspection,
		Description: "测试故障",
		Reporter:    "巡检员",
	})
	require.NoError(t, err)
	return entity
}

func (h *harness) createTeam(t *testing.T, name string, memberCount int, faultTypes, areas []string) *dispatch.Team {
	t.Helper()
	entity, err := h.dispatche.CreateTeam(context.Background(), dispatch.TeamSaveRequest{
		Name:        name,
		MemberCount: memberCount,
		FaultTypes:  faultTypes,
		Areas:       areas,
	})
	require.NoError(t, err)
	return entity
}

// requireStatus 断言错误是指定 HTTP 状态码的业务错误。
func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	businessErr, ok := apperr.As(err)
	require.True(t, ok, "期望业务错误, 实际: %v", err)
	require.Equal(t, status, businessErr.Status, "错误信息: %s", businessErr.Message)
}

func TestDispatchLifecycle(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-D-001", "中山路")
	target := h.createFault(t, device.ID, "灯不亮")
	team := h.createTeam(t, "维修一班", 4, []string{"灯不亮"}, []string{"中山路"})

	// 派工
	record, err := h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{
		FaultID: target.ID, TeamID: team.ID, Operator: "调度员",
	})
	require.NoError(t, err)
	require.Equal(t, dispatch.StatusOngoing, record.Status)
	require.Regexp(t, `^PG\d{8}\d{4}$`, record.DispatchNo)
	require.Equal(t, team.ID, record.TeamID)
	require.Equal(t, team.Name, record.TeamName)
	require.Equal(t, "灯不亮", record.FaultType)
	require.Equal(t, "中山路", record.RoadName)

	// 同一故障不允许重复派工
	_, err = h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: target.ID, TeamID: team.ID})
	requireStatus(t, err, http.StatusConflict)

	// 办结
	finished, err := h.dispatche.Finish(ctx, record.ID, dispatch.FinishRequest{Remark: "现场已恢复"})
	require.NoError(t, err)
	require.Equal(t, dispatch.StatusFinished, finished.Status)
	require.NotNil(t, finished.FinishedAt)

	// 已办结工单不允许再次办结或改派
	_, err = h.dispatche.Finish(ctx, record.ID, dispatch.FinishRequest{})
	requireStatus(t, err, http.StatusConflict)
	_, err = h.dispatche.Reassign(ctx, record.ID, dispatch.ReassignRequest{Reason: "测试"})
	requireStatus(t, err, http.StatusConflict)
}

func TestDispatchMatchValidation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-D-002", "中山路")
	h.createLamp(t, "LD-D-002B", "滨江路")
	target := h.createFault(t, device.ID, "灯不亮")

	// 故障类型不匹配
	otherType := h.createTeam(t, "线路班", 3, []string{"线路故障"}, []string{"中山路"})
	_, err := h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: target.ID, TeamID: otherType.ID})
	requireStatus(t, err, http.StatusBadRequest)

	// 负责区域不匹配
	otherArea := h.createTeam(t, "西区班", 3, []string{"灯不亮"}, []string{"滨江路"})
	_, err = h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: target.ID, TeamID: otherArea.ID})
	requireStatus(t, err, http.StatusBadRequest)

	// 停用班组不能派工
	disabled, err := h.dispatche.CreateTeam(ctx, dispatch.TeamSaveRequest{
		Name: "停用班", MemberCount: 2, FaultTypes: []string{"灯不亮"}, Areas: []string{"中山路"},
		Status: dispatch.TeamStatusDisabled,
	})
	require.NoError(t, err)
	_, err = h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: target.ID, TeamID: disabled.ID})
	requireStatus(t, err, http.StatusConflict)

	// 没有任何匹配班组时自动派工失败
	_, err = h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: target.ID})
	requireStatus(t, err, http.StatusBadRequest)
}

func TestDispatchAutoBalance(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	deviceA := h.createLamp(t, "LD-D-003", "中山路")
	deviceB := h.createLamp(t, "LD-D-004", "中山路")
	deviceC := h.createLamp(t, "LD-D-005", "中山路")

	teamA := h.createTeam(t, "A班", 3, []string{"灯不亮"}, []string{"中山路"})
	teamB := h.createTeam(t, "B班", 3, []string{"灯不亮"}, []string{"中山路"})

	// 第一张工单: 两个班组在办都是 0, 按名称选 A班
	first := h.createFault(t, deviceA.ID, "灯不亮")
	record, err := h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: first.ID})
	require.NoError(t, err)
	require.Equal(t, teamA.ID, record.TeamID)

	// 第二张工单: A班在办 1, B班在办 0, 自动均衡到 B班
	second := h.createFault(t, deviceB.ID, "灯不亮")
	record, err = h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: second.ID})
	require.NoError(t, err)
	require.Equal(t, teamB.ID, record.TeamID)

	// 推荐接口: 两个班组都匹配, 在办少的排前面
	third := h.createFault(t, deviceC.ID, "灯不亮")
	suggestions, err := h.dispatche.Suggest(ctx, third.ID)
	require.NoError(t, err)
	require.Len(t, suggestions, 2)
	require.True(t, suggestions[0].Matched)
	require.True(t, suggestions[0].Recommended)
	require.Equal(t, int64(1), suggestions[0].OngoingCount)
	require.Equal(t, int64(1), suggestions[1].OngoingCount)
}

func TestReassignKeepsRecordInBothTeams(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-D-006", "中山路")
	target := h.createFault(t, device.ID, "灯不亮")
	teamA := h.createTeam(t, "一班", 3, []string{"灯不亮"}, []string{"中山路"})
	teamB := h.createTeam(t, "二班", 3, []string{"灯不亮"}, []string{"中山路"})

	record, err := h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: target.ID, TeamID: teamA.ID})
	require.NoError(t, err)

	// 改派原因必填
	_, err = h.dispatche.Reassign(ctx, record.ID, dispatch.ReassignRequest{TeamID: teamB.ID})
	requireStatus(t, err, http.StatusBadRequest)

	// 不能改派给当前班组
	_, err = h.dispatche.Reassign(ctx, record.ID, dispatch.ReassignRequest{TeamID: teamA.ID, Reason: "测试"})
	requireStatus(t, err, http.StatusBadRequest)

	// 正常改派
	fresh, err := h.dispatche.Reassign(ctx, record.ID, dispatch.ReassignRequest{
		TeamID: teamB.ID, Reason: "一班满负荷, 调整至二班", Operator: "调度员",
	})
	require.NoError(t, err)
	require.Equal(t, dispatch.StatusOngoing, fresh.Status)
	require.Equal(t, teamB.ID, fresh.TeamID)
	require.Equal(t, teamA.Name, fresh.FromTeamName)
	require.Equal(t, "一班满负荷, 调整至二班", fresh.ReassignReason)
	require.NotNil(t, fresh.PrevRecordID)
	require.Equal(t, record.ID, *fresh.PrevRecordID)

	// 原班组留存"已改出"记录
	old, err := h.dispatche.GetRecord(ctx, record.ID)
	require.NoError(t, err)
	require.Equal(t, dispatch.StatusReassigned, old.Status)
	require.Equal(t, teamA.ID, old.TeamID)

	// 原班组与接收班组各留一条记录
	_, totalA, _, err := h.dispatche.ListRecords(ctx, dispatch.DispatchListQuery{TeamID: teamA.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), totalA)
	_, totalB, _, err := h.dispatche.ListRecords(ctx, dispatch.DispatchListQuery{TeamID: teamB.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), totalB)

	// 故障维度能看到完整流转链路
	items, total, _, err := h.dispatche.ListRecords(ctx, dispatch.DispatchListQuery{FaultID: target.ID})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, items, 2)
}

func TestTeamMaintenanceValidation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.createLamp(t, "LD-D-007", "中山路")

	// 非法故障类型
	_, err := h.dispatche.CreateTeam(ctx, dispatch.TeamSaveRequest{
		Name: "测试班", MemberCount: 2, FaultTypes: []string{"不存在的类型"}, Areas: []string{"中山路"},
	})
	requireStatus(t, err, http.StatusBadRequest)

	// 负责区域不在台账道路清单中
	_, err = h.dispatche.CreateTeam(ctx, dispatch.TeamSaveRequest{
		Name: "测试班", MemberCount: 2, FaultTypes: []string{"灯不亮"}, Areas: []string{"不存在的路"},
	})
	requireStatus(t, err, http.StatusBadRequest)

	// 班组名称唯一
	h.createTeam(t, "唯一班", 2, []string{"灯不亮"}, []string{"中山路"})
	_, err = h.dispatche.CreateTeam(ctx, dispatch.TeamSaveRequest{
		Name: "唯一班", MemberCount: 2, FaultTypes: []string{"灯不亮"}, Areas: []string{"中山路"},
	})
	requireStatus(t, err, http.StatusConflict)

	// 存在在办工单的班组不允许删除
	device := h.createLamp(t, "LD-D-008", "中山路")
	target := h.createFault(t, device.ID, "灯不亮")
	team := h.createTeam(t, "在办班", 2, []string{"灯不亮"}, []string{"中山路"})
	_, err = h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: target.ID, TeamID: team.ID})
	require.NoError(t, err)
	requireStatus(t, h.dispatche.DeleteTeam(ctx, team.ID), http.StatusConflict)
}

func TestOverviewPerCapitaAndOverdue(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	deviceA := h.createLamp(t, "LD-D-009", "中山路")
	deviceB := h.createLamp(t, "LD-D-010", "中山路")
	deviceC := h.createLamp(t, "LD-D-011", "中山路")
	deviceD := h.createLamp(t, "LD-D-012", "中山路")
	team := h.createTeam(t, "统计班", 4, []string{"灯不亮"}, []string{"中山路"})

	// 两条已办结 + 一条在办 + 一条超时在办
	faultA := h.createFault(t, deviceA.ID, "灯不亮")
	recordA, err := h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: faultA.ID, TeamID: team.ID})
	require.NoError(t, err)
	_, err = h.dispatche.Finish(ctx, recordA.ID, dispatch.FinishRequest{})
	require.NoError(t, err)

	faultB := h.createFault(t, deviceB.ID, "灯不亮")
	recordB, err := h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: faultB.ID, TeamID: team.ID})
	require.NoError(t, err)
	_, err = h.dispatche.Finish(ctx, recordB.ID, dispatch.FinishRequest{})
	require.NoError(t, err)

	faultC := h.createFault(t, deviceC.ID, "灯不亮")
	_, err = h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: faultC.ID, TeamID: team.ID})
	require.NoError(t, err)

	faultD := h.createFault(t, deviceD.ID, "灯不亮")
	recordD, err := h.dispatche.Dispatch(ctx, dispatch.DispatchCreateRequest{FaultID: faultD.ID, TeamID: team.ID})
	require.NoError(t, err)
	// 将派工时间改到 48 小时前, 使其成为超时未完工
	overdueAt := time.Now().Add(-48 * time.Hour)
	require.NoError(t, h.db.Model(&dispatch.DispatchRecord{}).
		Where("id = ?", recordD.ID).
		Update("dispatched_at", overdueAt).Error)

	overview, err := h.dispatche.Overview(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), overview.TeamTotal)
	require.Equal(t, int64(4), overview.DispatchTotal)
	require.Equal(t, int64(2), overview.OngoingTotal)
	require.Equal(t, int64(2), overview.FinishedTotal)
	require.Equal(t, int64(1), overview.OverdueTotal)
	require.Equal(t, dispatch.OverdueHours, overview.OverdueHours)

	require.Len(t, overview.Teams, 1)
	row := overview.Teams[0]
	require.Equal(t, team.ID, row.TeamID)
	require.Equal(t, int64(2), row.OngoingCount)
	require.Equal(t, int64(2), row.FinishedCount)
	require.Equal(t, int64(1), row.OverdueCount)
	require.InDelta(t, 0.5, row.PerCapitaRepair, 0.001, "人均维修量 = 办结 2 / 人数 4")

	// 超时筛选只返回超时未完工工单
	items, total, _, err := h.dispatche.ListRecords(ctx, dispatch.DispatchListQuery{Overdue: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, recordD.ID, items[0].ID)
	require.NotNil(t, items[0].Overdue)
	require.True(t, *items[0].Overdue)
}
