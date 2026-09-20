package dispatch

import (
	"encoding/json"
	"strings"
	"time"
)

// 派工单状态。
const (
	StatusOngoing    = "ongoing"    // 在办
	StatusReassigned = "reassigned" // 已改派(原班组留存的转出记录)
	StatusDone       = "done"       // 已完工
)

// OverdueThreshold 是判定"超时未完工"的派工时长阈值。
const OverdueThreshold = 24 * time.Hour

var statusLabels = map[string]string{
	StatusOngoing:    "在办",
	StatusReassigned: "已改派",
	StatusDone:       "已完工",
}

// Statuses 返回全部派工单状态取值。
func Statuses() []string {
	return []string{StatusOngoing, StatusReassigned, StatusDone}
}

// IsValidStatus 校验派工单状态取值。
func IsValidStatus(status string) bool {
	_, ok := statusLabels[status]
	return ok
}

// StatusLabel 返回派工单状态的中文名称, 未知取值原样返回。
func StatusLabel(status string) string {
	if label, ok := statusLabels[status]; ok {
		return label
	}
	return status
}

// Team 维修班组, 维护班组可承接的故障类型与负责区域, 是派工与改派的校验依据。
// FaultTypes / Roads 以 JSON 数组字符串落库, 空串表示"不限"。
type Team struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:64;uniqueIndex;not null" json:"name"`
	Leader      string    `gorm:"size:64" json:"leader"`
	Phone       string    `gorm:"size:32" json:"phone"`
	MemberCount int       `gorm:"not null;default:1" json:"member_count"`
	FaultTypes  string    `gorm:"size:512" json:"-"`
	Roads       string    `gorm:"size:1024" json:"-"`
	Enabled     bool      `gorm:"not null;default:true" json:"enabled"`
	Remark      string    `gorm:"size:255" json:"remark"`

	// 以下字段仅用于响应展示, 不落库。
	FaultTypeList []string `gorm:"-" json:"fault_types"`
	RoadList      []string `gorm:"-" json:"roads"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (Team) TableName() string { return "team" }

// FillLists 把落库的 JSON 字符串解析为列表字段, 供响应展示。
func (t *Team) FillLists() {
	t.FaultTypeList = decodeList(t.FaultTypes)
	t.RoadList = decodeList(t.Roads)
}

// SetTypes 写入可承接故障类型, 自动去重去空, 空列表表示不限类型。
func (t *Team) SetTypes(types []string) { t.FaultTypes = encodeList(types) }

// SetRoads 写入负责区域(道路), 自动去重去空, 空列表表示不限区域。
func (t *Team) SetRoads(roads []string) { t.Roads = encodeList(roads) }

// CoversType 判断班组是否可承接指定故障类型, 未配置类型时视为全部可承接。
func (t *Team) CoversType(faultType string) bool {
	types := decodeList(t.FaultTypes)
	if len(types) == 0 {
		return true
	}
	for _, item := range types {
		if item == faultType {
			return true
		}
	}
	return false
}

// CoversRoad 判断班组是否负责指定道路, 未配置区域时视为全区负责。
func (t *Team) CoversRoad(roadName string) bool {
	roads := decodeList(t.Roads)
	if len(roads) == 0 {
		return true
	}
	for _, item := range roads {
		if item == roadName {
			return true
		}
	}
	return false
}

// Dispatch 派工单, 记录一次故障到班组的派工。
// 改派时原班组的记录置为"已改派"并写明改派原因, 接收班组生成一条新的在办记录,
// 通过 PrevDispatchID / ReplacedByID 串联完整的改派链条。
type Dispatch struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	DispatchNo     string     `gorm:"size:64;uniqueIndex;not null" json:"dispatch_no"`
	FaultID        uint       `gorm:"index;not null" json:"fault_id"`
	FaultNo        string     `gorm:"size:64;index" json:"fault_no"`
	LampID         uint       `gorm:"index" json:"lamp_id"`
	LampCode       string     `gorm:"size:64;index" json:"lamp_code"`
	RoadName       string     `gorm:"size:128;index" json:"road_name"`
	FaultType      string     `gorm:"size:32;index" json:"fault_type"`
	TeamID         uint       `gorm:"index;not null" json:"team_id"`
	TeamName       string     `gorm:"size:64;index" json:"team_name"`
	Status         string     `gorm:"size:32;index;not null;default:ongoing" json:"status"`
	Operator       string     `gorm:"size:64" json:"operator"`
	ReassignReason string     `gorm:"size:255" json:"reassign_reason"`
	PrevDispatchID *uint      `json:"prev_dispatch_id"`
	ReplacedByID   *uint      `json:"replaced_by_id"`
	DispatchedAt   time.Time  `gorm:"index;not null" json:"dispatched_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	Remark         string     `gorm:"size:255" json:"remark"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TableName 指定表名。
func (Dispatch) TableName() string { return "dispatch" }

// decodeList 解析 JSON 数组字符串, 空串或解析失败时返回 nil。
func decodeList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return items
}

// encodeList 把字符串列表编码为 JSON 数组字符串, 去重去空, 空列表返回空串。
func encodeList(items []string) string {
	cleaned := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		cleaned = append(cleaned, item)
	}
	if len(cleaned) == 0 {
		return ""
	}
	data, err := json.Marshal(cleaned)
	if err != nil {
		return ""
	}
	return string(data)
}
