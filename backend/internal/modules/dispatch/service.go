package dispatch

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/fault"
	"streetlight/pkg/pagination"
)

// teamSortSpec 定义班组列表允许的排序字段白名单。
var teamSortSpec = pagination.SortSpec{
	Allowed: map[string]string{
		"name":         "name",
		"member_count": "member_count",
		"status":       "status",
		"created_at":   "created_at",
	},
	Default: "id",
}

// dispatchSortSpec 定义派工记录列表允许的排序字段白名单。
var dispatchSortSpec = pagination.SortSpec{
	Allowed: map[string]string{
		"dispatch_no":   "dispatch_no",
		"dispatched_at": "dispatched_at",
		"finished_at":   "finished_at",
		"status":        "status",
		"team_name":     "team_name",
		"created_at":    "created_at",
	},
	Default: "dispatched_at",
}

// FaultPort 由故障登记模块实现, 派工模块通过它读取故障信息。
type FaultPort interface {
	GetByID(ctx context.Context, id uint) (*fault.Fault, error)
}

// LampRoadPort 由路灯台账模块实现, 派工模块通过它读取负责区域候选(道路清单)。
type LampRoadPort interface {
	DistinctValues(ctx context.Context, column string) ([]string, error)
}

// Service 承载班组维护与派工/改派的业务规则。
type Service struct {
	repo   *Repository
	faults FaultPort
	lamps  LampRoadPort
}

// NewService 构造派工服务。
func NewService(repo *Repository, faults FaultPort, lamps LampRoadPort) *Service {
	return &Service{repo: repo, faults: faults, lamps: lamps}
}

// ---------- 班组维护 ----------

// GetTeam 查询班组详情。
func (s *Service) GetTeam(ctx context.Context, id uint) (*Team, error) {
	return s.repo.GetTeamByID(ctx, id)
}

// ListTeams 分页查询班组。
func (s *Service) ListTeams(ctx context.Context, query TeamListQuery) ([]Team, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, teamSortSpec)
	filter := TeamFilter{
		Keyword: strings.TrimSpace(query.Keyword),
		Status:  strings.TrimSpace(query.Status),
	}
	if filter.Status != "" && !IsValidTeamStatus(filter.Status) {
		return nil, 0, page, apperr.BadRequest("非法的班组状态: %s", filter.Status)
	}
	items, total, err := s.repo.ListTeams(ctx, filter, page)
	if err != nil {
		return nil, 0, page, err
	}
	return items, total, page, nil
}

// TeamOptions 返回启用班组的下拉选项, 附带当前在办数量便于均衡派工。
func (s *Service) TeamOptions(ctx context.Context) ([]TeamOption, error) {
	teams, err := s.repo.ListEnabledTeams(ctx)
	if err != nil {
		return nil, err
	}
	ongoing, err := s.repo.CountStatusByTeam(ctx, StatusOngoing)
	if err != nil {
		return nil, err
	}
	options := make([]TeamOption, 0, len(teams))
	for _, item := range teams {
		options = append(options, TeamOption{
			ID:           item.ID,
			Name:         item.Name,
			MemberCount:  item.MemberCount,
			OngoingCount: ongoing[item.ID],
		})
	}
	return options, nil
}

// CreateTeam 新增班组, 校验名称唯一、故障类型与负责区域取值合法。
func (s *Service) CreateTeam(ctx context.Context, req TeamSaveRequest) (*Team, error) {
	entity, err := s.buildTeam(ctx, req, 0)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateTeam(ctx, entity); err != nil {
		return nil, err
	}
	return entity, nil
}

// UpdateTeam 修改班组, 名称变更时同步更新派工记录上的班组名称快照。
func (s *Service) UpdateTeam(ctx context.Context, id uint, req TeamSaveRequest) (*Team, error) {
	entity, err := s.buildTeam(ctx, req, id)
	if err != nil {
		return nil, err
	}
	entity.ID = id
	previous, err := s.repo.GetTeamByID(ctx, id)
	if err != nil {
		return nil, err
	}
	entity.CreatedAt = previous.CreatedAt

	if err := s.repo.UpdateTeam(ctx, entity); err != nil {
		return nil, err
	}
	if previous.Name != entity.Name {
		if err := s.repo.RenameTeamRecords(ctx, id, entity.Name); err != nil {
			return nil, err
		}
	}
	return entity, nil
}

// DeleteTeam 删除班组, 存在在办工单时拦截。
func (s *Service) DeleteTeam(ctx context.Context, id uint) error {
	entity, err := s.repo.GetTeamByID(ctx, id)
	if err != nil {
		return err
	}
	ongoing, err := s.repo.CountOngoingByTeam(ctx, id)
	if err != nil {
		return err
	}
	if ongoing > 0 {
		return apperr.Conflict("班组 %s 还有 %d 条在办工单, 请先办结或改派后再删除", entity.Name, ongoing)
	}
	return s.repo.DeleteTeam(ctx, id)
}

// buildTeam 校验并组装班组实体, excludeID 用于更新场景排除自身。
func (s *Service) buildTeam(ctx context.Context, req TeamSaveRequest, excludeID uint) (*Team, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperr.BadRequest("班组名称不能为空")
	}
	exists, err := s.repo.ExistsTeamByName(ctx, name, excludeID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperr.Conflict("班组名称 %s 已存在", name)
	}
	if req.MemberCount < 1 {
		return nil, apperr.BadRequest("班组人数不能少于 1 人")
	}

	faultTypes, err := normalizeFaultTypes(req.FaultTypes)
	if err != nil {
		return nil, err
	}
	areas, err := s.normalizeAreas(ctx, req.Areas)
	if err != nil {
		return nil, err
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = TeamStatusEnabled
	}
	if !IsValidTeamStatus(status) {
		return nil, apperr.BadRequest("非法的班组状态: %s", status)
	}

	return &Team{
		Name:        name,
		MemberCount: req.MemberCount,
		FaultTypes:  faultTypes,
		Areas:       areas,
		Contact:     strings.TrimSpace(req.Contact),
		Phone:       strings.TrimSpace(req.Phone),
		Status:      status,
		Remark:      strings.TrimSpace(req.Remark),
	}, nil
}

// normalizeFaultTypes 校验故障类型取值并去重, 保持入参顺序。
func normalizeFaultTypes(values []string) ([]string, error) {
	allowed := make(map[string]bool, len(fault.FaultTypes()))
	for _, item := range fault.FaultTypes() {
		allowed[item] = true
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if !allowed[value] {
			return nil, apperr.BadRequest("非法的故障类型: %s", value)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, apperr.BadRequest("可承接故障类型至少选择一项")
	}
	return result, nil
}

// normalizeAreas 校验负责区域必须来自路灯台账的道路清单并去重。
func (s *Service) normalizeAreas(ctx context.Context, values []string) ([]string, error) {
	roads, err := s.lamps.DistinctValues(ctx, "road_name")
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(roads))
	for _, item := range roads {
		allowed[item] = true
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if !allowed[value] {
			return nil, apperr.BadRequest("负责区域 %s 不在路灯台账的道路清单中", value)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, apperr.BadRequest("负责区域至少选择一项")
	}
	return result, nil
}

// ---------- 派工 / 改派 / 办结 ----------

// GetRecord 查询派工记录详情。
func (s *Service) GetRecord(ctx context.Context, id uint) (*DispatchRecord, error) {
	entity, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		return nil, err
	}
	entity.FillOverdue(time.Now())
	return entity, nil
}

// ListRecords 分页查询派工记录。
func (s *Service) ListRecords(ctx context.Context, query DispatchListQuery) ([]DispatchRecord, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, dispatchSortSpec)
	filter := DispatchFilter{
		Keyword: strings.TrimSpace(query.Keyword),
		TeamID:  query.TeamID,
		FaultID: query.FaultID,
		Status:  strings.TrimSpace(query.Status),
	}
	if filter.Status != "" && !IsValidDispatchStatus(filter.Status) {
		return nil, 0, page, apperr.BadRequest("非法的派工单状态: %s", filter.Status)
	}
	if query.Overdue {
		before := time.Now().Add(-time.Duration(OverdueHours * float64(time.Hour)))
		filter.OverdueBefore = &before
	}
	items, total, err := s.repo.ListRecords(ctx, filter, page)
	if err != nil {
		return nil, 0, page, err
	}
	now := time.Now()
	for index := range items {
		items[index].FillOverdue(now)
	}
	return items, total, page, nil
}

// Dispatch 派工: 校验故障状态与班组匹配情况后生成在办工单, 班组缺省时按在办数量自动均衡。
func (s *Service) Dispatch(ctx context.Context, req DispatchCreateRequest) (*DispatchRecord, error) {
	target, err := s.faults.GetByID(ctx, req.FaultID)
	if err != nil {
		return nil, err
	}
	if !fault.IsOpen(target.Status) {
		return nil, apperr.Conflict("故障 %s 当前状态为 %s, 不允许派工", target.FaultNo, fault.StatusLabel(target.Status))
	}

	ongoing, err := s.repo.GetOngoingByFault(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	if ongoing != nil {
		return nil, apperr.Conflict("故障 %s 已派工至 %s(单号 %s), 如需调整请使用改派", target.FaultNo, ongoing.TeamName, ongoing.DispatchNo)
	}

	team, err := s.resolveTeam(ctx, req.TeamID, target.FaultType, target.RoadName, 0)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	entity := &DispatchRecord{
		FaultID:      target.ID,
		FaultNo:      target.FaultNo,
		LampID:       target.LampID,
		LampCode:     target.LampCode,
		RoadName:     target.RoadName,
		FaultType:    target.FaultType,
		FaultLevel:   target.FaultLevel,
		TeamID:       team.ID,
		TeamName:     team.Name,
		Status:       StatusOngoing,
		DispatchedAt: now,
		Operator:     strings.TrimSpace(req.Operator),
		Remark:       strings.TrimSpace(req.Remark),
	}
	if err := s.repo.CreateRecordWithUniqueNo(ctx, entity, dispatchNoPrefix(now)); err != nil {
		return nil, err
	}
	entity.FillOverdue(now)
	return entity, nil
}

// Reassign 改派: 原班组记录置为已改出留存, 接收班组生成新的在办记录, 改派原因必填。
func (s *Service) Reassign(ctx context.Context, id uint, req ReassignRequest) (*DispatchRecord, error) {
	record, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status != StatusOngoing {
		return nil, apperr.Conflict("派工单 %s 当前状态为 %s, 不允许改派", record.DispatchNo, DispatchStatusLabel(record.Status))
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, apperr.BadRequest("改派原因不能为空")
	}
	if req.TeamID > 0 && req.TeamID == record.TeamID {
		return nil, apperr.BadRequest("工单已在班组 %s, 无需改派", record.TeamName)
	}

	// 以工单上固化的故障类型与区域做匹配校验, 改派不改变工单内容
	team, err := s.resolveTeam(ctx, req.TeamID, record.FaultType, record.RoadName, record.TeamID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	record.Status = StatusReassigned
	record.FinishedAt = &now

	fresh := &DispatchRecord{
		FaultID:        record.FaultID,
		FaultNo:        record.FaultNo,
		LampID:         record.LampID,
		LampCode:       record.LampCode,
		RoadName:       record.RoadName,
		FaultType:      record.FaultType,
		FaultLevel:     record.FaultLevel,
		TeamID:         team.ID,
		TeamName:       team.Name,
		Status:         StatusOngoing,
		DispatchedAt:   now,
		PrevRecordID:   &record.ID,
		FromTeamID:     &record.TeamID,
		FromTeamName:   record.TeamName,
		ReassignReason: reason,
		Operator:       strings.TrimSpace(req.Operator),
	}
	if err := s.repo.ReassignInTx(ctx, record, fresh, dispatchNoPrefix(now)); err != nil {
		return nil, err
	}
	fresh.FillOverdue(now)
	return fresh, nil
}

// Finish 办结在办工单。
func (s *Service) Finish(ctx context.Context, id uint, req FinishRequest) (*DispatchRecord, error) {
	record, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status != StatusOngoing {
		return nil, apperr.Conflict("派工单 %s 当前状态为 %s, 不允许办结", record.DispatchNo, DispatchStatusLabel(record.Status))
	}

	now := time.Now()
	record.Status = StatusFinished
	record.FinishedAt = &now
	if remark := strings.TrimSpace(req.Remark); remark != "" {
		record.Remark = remark
	}
	if err := s.repo.UpdateRecord(ctx, record); err != nil {
		return nil, err
	}
	record.FillOverdue(now)
	return record, nil
}

// Suggest 返回各启用班组对指定故障的匹配情况与在办数量, 并给出均衡推荐。
func (s *Service) Suggest(ctx context.Context, faultID uint) ([]TeamSuggestion, error) {
	target, err := s.faults.GetByID(ctx, faultID)
	if err != nil {
		return nil, err
	}
	if !fault.IsOpen(target.Status) {
		return nil, apperr.Conflict("故障 %s 当前状态为 %s, 不允许派工", target.FaultNo, fault.StatusLabel(target.Status))
	}

	suggestions, err := s.buildSuggestions(ctx, target.FaultType, target.RoadName, 0)
	if err != nil {
		return nil, err
	}
	return suggestions, nil
}

// resolveTeam 解析目标班组: 指定班组时校验匹配情况, 未指定时按在办数量自动均衡选择。
func (s *Service) resolveTeam(ctx context.Context, teamID uint, faultType, roadName string, excludeTeamID uint) (*Team, error) {
	if teamID > 0 {
		team, err := s.repo.GetTeamByID(ctx, teamID)
		if err != nil {
			return nil, err
		}
		if team.Status != TeamStatusEnabled {
			return nil, apperr.Conflict("班组 %s 已停用, 不能承接新工单", team.Name)
		}
		if err := checkTeamMatch(team, faultType, roadName); err != nil {
			return nil, err
		}
		return team, nil
	}

	suggestions, err := s.buildSuggestions(ctx, faultType, roadName, excludeTeamID)
	if err != nil {
		return nil, err
	}
	for _, item := range suggestions {
		if item.Matched {
			return s.repo.GetTeamByID(ctx, item.TeamID)
		}
	}
	return nil, apperr.BadRequest("没有可承接该故障的启用班组(故障类型「%s」/ 区域「%s」), 请检查班组维护", faultType, roadName)
}

// buildSuggestions 计算各启用班组的匹配情况并按"匹配优先、在办少优先"排序, 首个匹配项为推荐班组。
func (s *Service) buildSuggestions(ctx context.Context, faultType, roadName string, excludeTeamID uint) ([]TeamSuggestion, error) {
	teams, err := s.repo.ListEnabledTeams(ctx)
	if err != nil {
		return nil, err
	}
	ongoing, err := s.repo.CountStatusByTeam(ctx, StatusOngoing)
	if err != nil {
		return nil, err
	}

	suggestions := make([]TeamSuggestion, 0, len(teams))
	for _, team := range teams {
		if excludeTeamID > 0 && team.ID == excludeTeamID {
			continue
		}
		typeMatched := team.CanHandle(faultType)
		areaMatched := team.Covers(roadName)
		suggestions = append(suggestions, TeamSuggestion{
			TeamID:       team.ID,
			TeamName:     team.Name,
			MemberCount:  team.MemberCount,
			OngoingCount: ongoing[team.ID],
			TypeMatched:  typeMatched,
			AreaMatched:  areaMatched,
			Matched:      typeMatched && areaMatched,
		})
	}

	// 均衡策略: 匹配的班组排在前面, 同组内在办数量少的优先, 再按名称稳定排序
	sort.SliceStable(suggestions, func(i, j int) bool {
		left, right := suggestions[i], suggestions[j]
		if left.Matched != right.Matched {
			return left.Matched
		}
		if left.OngoingCount != right.OngoingCount {
			return left.OngoingCount < right.OngoingCount
		}
		return left.TeamName < right.TeamName
	})
	for index := range suggestions {
		if suggestions[index].Matched {
			suggestions[index].Recommended = true
			break
		}
	}
	return suggestions, nil
}

// checkTeamMatch 校验班组是否可承接指定故障类型与区域。
func checkTeamMatch(team *Team, faultType, roadName string) error {
	if !team.CanHandle(faultType) {
		return apperr.BadRequest("班组 %s 不可承接故障类型「%s」", team.Name, faultType)
	}
	if !team.Covers(roadName) {
		return apperr.BadRequest("班组 %s 的负责区域不包含「%s」", team.Name, roadName)
	}
	return nil
}

// ---------- 概览与字典 ----------

// Overview 汇总各班组在办均衡、人均维修量与超时未完工数量。
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	now := time.Now()
	overdueBefore := now.Add(-time.Duration(OverdueHours * float64(time.Hour)))

	teams, err := s.repo.ListAllTeams(ctx)
	if err != nil {
		return nil, err
	}
	dispatchTotal, err := s.repo.CountRecords(ctx)
	if err != nil {
		return nil, err
	}
	ongoingByTeam, err := s.repo.CountStatusByTeam(ctx, StatusOngoing)
	if err != nil {
		return nil, err
	}
	finishedByTeam, err := s.repo.CountStatusByTeam(ctx, StatusFinished)
	if err != nil {
		return nil, err
	}
	reassignedByTeam, err := s.repo.CountStatusByTeam(ctx, StatusReassigned)
	if err != nil {
		return nil, err
	}
	overdueByTeam, err := s.repo.CountOverdueByTeam(ctx, overdueBefore)
	if err != nil {
		return nil, err
	}

	rows := make([]TeamOverviewRow, 0, len(teams))
	var ongoingTotal, finishedTotal, overdueTotal, memberTotal int64
	for _, team := range teams {
		ongoing := ongoingByTeam[team.ID]
		finished := finishedByTeam[team.ID]
		overdue := overdueByTeam[team.ID]
		members := int64(team.MemberCount)
		if members < 1 {
			members = 1
		}
		rows = append(rows, TeamOverviewRow{
			TeamID:          team.ID,
			TeamName:        team.Name,
			MemberCount:     team.MemberCount,
			Status:          team.Status,
			OngoingCount:    ongoing,
			FinishedCount:   finished,
			ReassignedCount: reassignedByTeam[team.ID],
			OverdueCount:    overdue,
			PerCapitaRepair: round2(float64(finished) / float64(members)),
		})
		ongoingTotal += ongoing
		finishedTotal += finished
		overdueTotal += overdue
		memberTotal += members
	}

	perCapitaAvg := 0.0
	if memberTotal > 0 {
		perCapitaAvg = round2(float64(finishedTotal) / float64(memberTotal))
	}
	return &Overview{
		TeamTotal:     int64(len(teams)),
		DispatchTotal: dispatchTotal,
		OngoingTotal:  ongoingTotal,
		FinishedTotal: finishedTotal,
		OverdueTotal:  overdueTotal,
		PerCapitaAvg:  perCapitaAvg,
		OverdueHours:  OverdueHours,
		Teams:         rows,
	}, nil
}

// Metadata 返回派工模块字典。
func (s *Service) Metadata(ctx context.Context) (*Meta, error) {
	roads, err := s.lamps.DistinctValues(ctx, "road_name")
	if err != nil {
		return nil, err
	}
	return &Meta{
		DispatchStatuses: DispatchStatuses(),
		TeamStatuses:     TeamStatuses(),
		FaultTypes:       fault.FaultTypes(),
		Roads:            roads,
		OverdueHours:     OverdueHours,
	}, nil
}

// dispatchNoPrefix 生成派工单号前缀, 例如 PG20260919。
func dispatchNoPrefix(dispatchedAt time.Time) string {
	return "PG" + dispatchedAt.Format("20060102")
}

// round2 保留两位小数。
func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
