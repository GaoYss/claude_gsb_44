package dispatch

import "streetlight/pkg/pagination"

// TeamSaveRequest 班组新增/修改请求, 维护可承接故障类型与负责区域。
type TeamSaveRequest struct {
	Name        string   `json:"name" binding:"required,max=64"`
	MemberCount int      `json:"member_count" binding:"required,min=1,max=999"`
	FaultTypes  []string `json:"fault_types" binding:"required,min=1,max=32"`
	Areas       []string `json:"areas" binding:"required,min=1,max=64"`
	Contact     string   `json:"contact" binding:"max=64"`
	Phone       string   `json:"phone" binding:"max=32"`
	Status      string   `json:"status" binding:"omitempty,oneof=enabled disabled"`
	Remark      string   `json:"remark" binding:"max=255"`
}

// TeamListQuery 班组列表查询条件。
type TeamListQuery struct {
	pagination.Params
	Keyword string `form:"keyword"` // 班组名称 / 联系人
	Status  string `form:"status"`
}

// DispatchCreateRequest 派工请求, 班组为空时按在办数量自动均衡选择。
type DispatchCreateRequest struct {
	FaultID   uint   `json:"fault_id" binding:"required"`
	TeamID    uint   `json:"team_id"`
	Operator  string `json:"operator" binding:"max=64"`
	Remark    string `json:"remark" binding:"max=255"`
}

// ReassignRequest 改派请求, 必须填写改派原因; 班组为空时按在办数量自动均衡选择。
type ReassignRequest struct {
	TeamID   uint   `json:"team_id"`
	Reason   string `json:"reason" binding:"required,max=255"`
	Operator string `json:"operator" binding:"max=64"`
}

// FinishRequest 办结请求。
type FinishRequest struct {
	Remark string `json:"remark" binding:"max=255"`
}

// DispatchListQuery 派工记录查询条件。
type DispatchListQuery struct {
	pagination.Params
	Keyword string `form:"keyword"` // 派工单号 / 故障单号 / 路灯编号
	TeamID  uint   `form:"team_id"`
	FaultID uint   `form:"fault_id"`
	Status  string `form:"status"`
	Overdue bool   `form:"overdue"` // 仅看超时未完工
}

// TeamOption 班组下拉选项, 附带当前在办数量便于均衡派工。
type TeamOption struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	MemberCount  int    `json:"member_count"`
	OngoingCount int64  `json:"ongoing_count"`
}

// TeamSuggestion 派工推荐结果: 某个班组对指定故障的匹配情况与当前在办数量。
type TeamSuggestion struct {
	TeamID       uint   `json:"team_id"`
	TeamName     string `json:"team_name"`
	MemberCount  int    `json:"member_count"`
	OngoingCount int64  `json:"ongoing_count"`
	TypeMatched  bool   `json:"type_matched"`
	AreaMatched  bool   `json:"area_matched"`
	Matched      bool   `json:"matched"`
	Recommended  bool   `json:"recommended"`
}

// TeamOverviewRow 概览中单个班组的统计行。
type TeamOverviewRow struct {
	TeamID          uint    `json:"team_id"`
	TeamName        string  `json:"team_name"`
	MemberCount     int     `json:"member_count"`
	Status          string  `json:"status"`
	OngoingCount    int64   `json:"ongoing_count"`
	FinishedCount   int64   `json:"finished_count"`
	ReassignedCount int64   `json:"reassigned_count"`
	OverdueCount    int64   `json:"overdue_count"`
	PerCapitaRepair float64 `json:"per_capita_repair"`
}

// Overview 派工概览: 全局汇总 + 各班组在办均衡、人均维修量与超时未完工数量。
type Overview struct {
	TeamTotal      int64             `json:"team_total"`
	DispatchTotal  int64             `json:"dispatch_total"`
	OngoingTotal   int64             `json:"ongoing_total"`
	FinishedTotal  int64             `json:"finished_total"`
	OverdueTotal   int64             `json:"overdue_total"`
	PerCapitaAvg   float64           `json:"per_capita_avg"`
	OverdueHours   float64           `json:"overdue_hours"`
	Teams          []TeamOverviewRow `json:"teams"`
}

// Meta 派工模块字典, 供前端渲染下拉框与表单选项。
type Meta struct {
	DispatchStatuses []string `json:"dispatch_statuses"`
	TeamStatuses     []string `json:"team_statuses"`
	FaultTypes       []string `json:"fault_types"`
	Roads            []string `json:"roads"`
	OverdueHours     float64  `json:"overdue_hours"`
}
