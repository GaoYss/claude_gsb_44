package dispatch

import "time"

// 班组状态。
const (
	TeamStatusEnabled  = "enabled"  // 启用
	TeamStatusDisabled = "disabled" // 停用
)

// 派工单状态。
const (
	StatusOngoing    = "ongoing"    // 在办
	StatusReassigned = "reassigned" // 已改出(改派后原班组留存的历史记录)
	StatusFinished   = "finished"   // 已办结
)

// OverdueHours 是判定"超时未完工"的时长阈值(小时), 与全局逾期口径保持一致。
const OverdueHours = 24.0

// TeamStatuses 返回全部班组状态取值。
func TeamStatuses() []string {
	return []string{TeamStatusEnabled, TeamStatusDisabled}
}

// DispatchStatuses 返回全部派工单状态取值。
func DispatchStatuses() []string {
	return []string{StatusOngoing, StatusReassigned, StatusFinished}
}

// IsValidTeamStatus 校验班组状态取值。
func IsValidTeamStatus(status string) bool {
	for _, item := range TeamStatuses() {
		if item == status {
			return true
		}
	}
	return false
}

// IsValidDispatchStatus 校验派工单状态取值。
func IsValidDispatchStatus(status string) bool {
	for _, item := range DispatchStatuses() {
		if item == status {
			return true
		}
	}
	return false
}

// Team 维修班组, 维护班组可承接的故障类型与负责区域, 作为派工/改派匹配校验的依据。
type Team struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:64;uniqueIndex;not null" json:"name"`
	MemberCount int       `gorm:"not null;default:1" json:"member_count"`
	FaultTypes  []string  `gorm:"size:512;serializer:json" json:"fault_types"`
	Areas       []string  `gorm:"size:512;serializer:json" json:"areas"`
	Contact     string    `gorm:"size:64" json:"contact"`
	Phone       string    `gorm:"size:32" json:"phone"`
	Status      string    `gorm:"size:32;index;not null;default:enabled" json:"status"`
	Remark      string    `gorm:"size:255" json:"remark"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (Team) TableName() string { return "team" }

// CanHandle 判断班组是否可承接指定故障类型。
func (t *Team) CanHandle(faultType string) bool {
	for _, item := range t.FaultTypes {
		if item == faultType {
			return true
		}
	}
	return false
}

// Covers 判断班组的负责区域是否覆盖指定道路。
func (t *Team) Covers(roadName string) bool {
	for _, item := range t.Areas {
		if item == roadName {
			return true
		}
	}
	return false
}

// DispatchRecord 派工记录, 一次派工或改派生成一条记录。
// 改派时原班组记录置为"已改出"保留, 接收班组生成新的在办记录,
// 因此同一工单在原班组与接收班组各留一条记录, 可完整追溯流转链路。
type DispatchRecord struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	DispatchNo     string     `gorm:"size:64;uniqueIndex;not null" json:"dispatch_no"`
	FaultID        uint       `gorm:"index;not null" json:"fault_id"`
	FaultNo        string     `gorm:"size:64;index" json:"fault_no"`
	LampID         uint       `gorm:"index" json:"lamp_id"`
	LampCode       string     `gorm:"size:64;index" json:"lamp_code"`
	RoadName       string     `gorm:"size:128;index" json:"road_name"`
	FaultType      string     `gorm:"size:32;index" json:"fault_type"`
	FaultLevel     string     `gorm:"size:32;index" json:"fault_level"`
	TeamID         uint       `gorm:"index;not null" json:"team_id"`
	TeamName       string     `gorm:"size:64;index" json:"team_name"`
	Status         string     `gorm:"size:32;index;not null;default:ongoing" json:"status"`
	DispatchedAt   time.Time  `gorm:"index;not null" json:"dispatched_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	PrevRecordID   *uint      `json:"prev_record_id"`
	FromTeamID     *uint      `json:"from_team_id"`
	FromTeamName   string     `gorm:"size:64" json:"from_team_name"`
	ReassignReason string     `gorm:"size:255" json:"reassign_reason"`
	Operator       string     `gorm:"size:64" json:"operator"`
	Remark         string     `gorm:"size:255" json:"remark"`

	// Overdue 仅用于响应展示是否超时未完工, 不落库。
	Overdue *bool `gorm:"-" json:"overdue,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (DispatchRecord) TableName() string { return "dispatch_record" }

// FillOverdue 依据派工时间与办结状态计算是否超时未完工。
func (r *DispatchRecord) FillOverdue(now time.Time) {
	overdue := r.Status == StatusOngoing && now.Sub(r.DispatchedAt).Hours() > OverdueHours
	r.Overdue = &overdue
}
