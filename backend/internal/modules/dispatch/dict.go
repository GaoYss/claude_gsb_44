package dispatch

var teamStatusLabels = map[string]string{
	TeamStatusEnabled:  "启用",
	TeamStatusDisabled: "停用",
}

var dispatchStatusLabels = map[string]string{
	StatusOngoing:    "在办",
	StatusReassigned: "已改出",
	StatusFinished:   "已办结",
}

// TeamStatusLabel 返回班组状态的中文名称。
func TeamStatusLabel(status string) string {
	if label, ok := teamStatusLabels[status]; ok {
		return label
	}
	return status
}

// DispatchStatusLabel 返回派工单状态的中文名称。
func DispatchStatusLabel(status string) string {
	if label, ok := dispatchStatusLabels[status]; ok {
		return label
	}
	return status
}
