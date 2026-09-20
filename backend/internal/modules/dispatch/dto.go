package dispatch

import (
	"time"

	"streetlight/pkg/pagination"
)

// TeamCreateRequest 新建班组请求。故障类型与负责区域留空表示不限。
type TeamCreateRequest struct {
	Name        string   `json:"name" binding:"required,max=64"`
	Leader      string   `json:"leader" binding:"max=64"`
	Phone       string   `json:"phone" binding:"max=32"`
	MemberCount int      `json:"member_count" binding:"required,min=1"`
	FaultTypes  []string `json:"fault_types" binding:"omitempty,max=16,dive,max=32"`
	Roads       []string `json:"roads" binding:"omitempty,max=32,dive,max=64"`
	Enabled     *bool    `json:"enabled"`
	Remark      string   `json:"remark" binding:"max=255"`
}

// TeamUpdateRequest 修改班组请求, 仅覆盖显式提交的字段。
type TeamUpdateRequest struct {
	Name        *string   `json:"name" binding:"omitempty,max=64"`
	Leader      *string   `json:"leader" binding:"omitempty,max=64"`
	Phone       *string   `json:"phone" binding:"omitempty,max=32"`
	MemberCount *int      `json:"member_count" binding:"omitempty,min=1"`
	FaultTypes  *[]string `json:"fault_types" binding:"omitempty,max=16,dive,max=32"`
	Roads       *[]string `json:"roads" binding:"omitempty,max=32,dive,max=64"`
	Enabled     *bool     `json:"enabled"`
	Remark      *string   `json:"remark" binding:"omitempty,max=255"`
}

// TeamListQuery 班组列表查询条件。
type TeamListQuery struct {
	pagination.Params
	Keyword string `form:"keyword"` // 班组名称 / 负责人
	Enabled *bool  `form:"enabled"`
}

// TeamOption 班组下拉选项。
type TeamOption struct {
	ID          uint     `json:"id"`
	Name        string   `json:"name"`
	Leader      string   `json:"leader"`
	MemberCount int      `json:"member_count"`
	FaultTypes  []string `json:"fault_types"`
	Roads       []string `json:"roads"`
}

// CreateRequest 派工请求。TeamID 为空时系统在匹配班组中自动选择在办最少的一家。
type CreateRequest struct {
	FaultID  uint   `json:"fault_id" binding:"required"`
	TeamID   uint   `json:"team_id"`
	Operator string `json:"operator" binding:"max=64"`
	Remark   string `json:"remark" binding:"max=255"`
}

// ReassignRequest 改派请求, 必须填写改派原因。TeamID 为空时自动选择(排除原班组)。
type ReassignRequest struct {
	TeamID   uint   `json:"team_id"`
	Reason   string `json:"reason" binding:"required,max=255"`
	Operator string `json:"operator" binding:"max=64"`
}

// ListQuery 派工单列表查询条件。
type ListQuery struct {
	pagination.Params
	Keyword string `form:"keyword"` // 派工单号 / 故障单号 / 路灯编号 / 班组名称
	TeamID  uint   `form:"team_id"`
	FaultID uint   `form:"fault_id"`
	Status  string `form:"status"`
}

// CandidateQuery 候选班组查询条件。
type CandidateQuery struct {
	FaultID uint `form:"fault_id" binding:"required"`
}

// Candidate 一个班组对指定故障的匹配情况与当前在办数量。
type Candidate struct {
	TeamID      uint     `json:"team_id"`
	TeamName    string   `json:"team_name"`
	MemberCount int      `json:"member_count"`
	Ongoing     int64    `json:"ongoing"`
	Matched     bool     `json:"matched"`
	Recommended bool     `json:"recommended"`
	Reasons     []string `json:"reasons"`
}

// CandidatesResult 指定故障的候选班组列表, 匹配的在办最少班组标记为推荐。
type CandidatesResult struct {
	FaultID   uint        `json:"fault_id"`
	FaultNo   string      `json:"fault_no"`
	FaultType string      `json:"fault_type"`
	RoadName  string      `json:"road_name"`
	Teams     []Candidate `json:"teams"`
}

// TeamStat 班组维度的派工统计。
type TeamStat struct {
	TeamID        uint    `json:"team_id"`
	TeamName      string  `json:"team_name"`
	MemberCount   int     `json:"member_count"`
	Enabled       bool    `json:"enabled"`
	Ongoing       int64   `json:"ongoing"`
	Done          int64   `json:"done"`
	ReassignedOut int64   `json:"reassigned_out"`
	Received      int64   `json:"received"`
	PerCapitaDone float64 `json:"per_capita_done"`
	Overdue       int64   `json:"overdue"`
}

// Overview 派工概览: 全局汇总 + 各班组人均维修量与超时未完工数量。
type Overview struct {
	TeamTotal       int64      `json:"team_total"`
	DispatchTotal   int64      `json:"dispatch_total"`
	OngoingTotal    int64      `json:"ongoing_total"`
	ReassignedTotal int64      `json:"reassigned_total"`
	DoneTotal       int64      `json:"done_total"`
	OverdueTotal    int64      `json:"overdue_total"`
	OverdueHours    float64    `json:"overdue_threshold_hours"`
	Teams           []TeamStat `json:"teams"`
	GeneratedAt     time.Time  `json:"generated_at"`
}
