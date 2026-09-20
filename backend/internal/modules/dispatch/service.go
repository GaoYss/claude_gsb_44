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
		"created_at":   "created_at",
	},
	Default: "id",
}

// dispatchSortSpec 定义派工单列表允许的排序字段白名单。
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

// Service 承载班组与派工的业务规则。
type Service struct {
	teams      *TeamRepository
	dispatches *DispatchRepository
	faults     FaultPort
}

// NewService 构造班组派工服务。
func NewService(teams *TeamRepository, dispatches *DispatchRepository, faults FaultPort) *Service {
	return &Service{teams: teams, dispatches: dispatches, faults: faults}
}

// ---------- 班组 ----------

// GetTeam 查询班组详情。
func (s *Service) GetTeam(ctx context.Context, id uint) (*Team, error) {
	return s.teams.GetByID(ctx, id)
}

// ListTeams 分页查询班组。
func (s *Service) ListTeams(ctx context.Context, query TeamListQuery) ([]Team, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, teamSortSpec)
	filter := TeamFilter{Keyword: strings.TrimSpace(query.Keyword), Enabled: query.Enabled}
	items, total, err := s.teams.List(ctx, filter, page)
	if err != nil {
		return nil, 0, page, err
	}
	return items, total, page, nil
}

// TeamOptions 返回启用中的班组下拉选项。
func (s *Service) TeamOptions(ctx context.Context) ([]TeamOption, error) {
	teams, err := s.teams.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]TeamOption, 0, len(teams))
	for _, team := range teams {
		options = append(options, TeamOption{
			ID:          team.ID,
			Name:        team.Name,
			Leader:      team.Leader,
			MemberCount: team.MemberCount,
			FaultTypes:  team.FaultTypeList,
			Roads:       team.RoadList,
		})
	}
	return options, nil
}

// CreateTeam 新建班组, 校验名称唯一与故障类型合法。
func (s *Service) CreateTeam(ctx context.Context, req TeamCreateRequest) (*Team, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperr.BadRequest("班组名称不能为空")
	}
	exists, err := s.teams.ExistsByName(ctx, name, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperr.Conflict("班组名称已存在: %s", name)
	}
	if err := validateFaultTypes(req.FaultTypes); err != nil {
		return nil, err
	}

	entity := &Team{
		Name:        name,
		Leader:      strings.TrimSpace(req.Leader),
		Phone:       strings.TrimSpace(req.Phone),
		MemberCount: req.MemberCount,
		Enabled:     true,
		Remark:      strings.TrimSpace(req.Remark),
	}
	if req.Enabled != nil {
		entity.Enabled = *req.Enabled
	}
	entity.SetTypes(req.FaultTypes)
	entity.SetRoads(req.Roads)

	if err := s.teams.Create(ctx, entity); err != nil {
		return nil, err
	}
	entity.FillLists()
	return entity, nil
}

// UpdateTeam 修改班组, 仅覆盖显式提交的字段。
func (s *Service) UpdateTeam(ctx context.Context, id uint, req TeamUpdateRequest) (*Team, error) {
	entity, err := s.teams.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, apperr.BadRequest("班组名称不能为空")
		}
		if name != entity.Name {
			exists, err := s.teams.ExistsByName(ctx, name, id)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, apperr.Conflict("班组名称已存在: %s", name)
			}
			entity.Name = name
		}
	}
	if req.Leader != nil {
		entity.Leader = strings.TrimSpace(*req.Leader)
	}
	if req.Phone != nil {
		entity.Phone = strings.TrimSpace(*req.Phone)
	}
	if req.MemberCount != nil {
		entity.MemberCount = *req.MemberCount
	}
	if req.FaultTypes != nil {
		if err := validateFaultTypes(*req.FaultTypes); err != nil {
			return nil, err
		}
		entity.SetTypes(*req.FaultTypes)
	}
	if req.Roads != nil {
		entity.SetRoads(*req.Roads)
	}
	if req.Enabled != nil {
		entity.Enabled = *req.Enabled
	}
	if req.Remark != nil {
		entity.Remark = strings.TrimSpace(*req.Remark)
	}

	if err := s.teams.Update(ctx, entity); err != nil {
		return nil, err
	}
	entity.FillLists()
	return entity, nil
}

// DeleteTeam 删除班组, 存在在办派工单时拒绝删除。
func (s *Service) DeleteTeam(ctx context.Context, id uint) error {
	entity, err := s.teams.GetByID(ctx, id)
	if err != nil {
		return err
	}
	ongoing, err := s.dispatches.CountOngoingForTeam(ctx, id)
	if err != nil {
		return err
	}
	if ongoing > 0 {
		return apperr.Conflict("班组「%s」还有 %d 单在办, 请先改派或办结后再删除", entity.Name, ongoing)
	}
	return s.teams.Delete(ctx, id)
}

// ---------- 派工 ----------

// Get 查询派工单详情。
func (s *Service) Get(ctx context.Context, id uint) (*Dispatch, error) {
	return s.dispatches.GetByID(ctx, id)
}

// List 分页查询派工单。
func (s *Service) List(ctx context.Context, query ListQuery) ([]Dispatch, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, dispatchSortSpec)
	filter := DispatchFilter{
		Keyword: strings.TrimSpace(query.Keyword),
		TeamID:  query.TeamID,
		FaultID: query.FaultID,
		Status:  strings.TrimSpace(query.Status),
	}
	if filter.Status != "" && !IsValidStatus(filter.Status) {
		return nil, 0, page, apperr.BadRequest("非法的派工状态: %s", filter.Status)
	}
	items, total, err := s.dispatches.List(ctx, filter, page)
	if err != nil {
		return nil, 0, page, err
	}
	return items, total, page, nil
}

// ListByFault 查询某条故障的派工链条(含改派历史)。
func (s *Service) ListByFault(ctx context.Context, faultID uint) ([]Dispatch, error) {
	if _, err := s.faults.GetByID(ctx, faultID); err != nil {
		return nil, err
	}
	return s.dispatches.ListByFault(ctx, faultID)
}

// Create 派工: 校验故障未闭环且无在办派工单, 校验班组与故障类型/区域匹配。
// 未指定班组时, 在匹配班组中自动选择在办数量最少的一家, 保证同一时段各班组在办数量尽量均衡。
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Dispatch, error) {
	target, err := s.faults.GetByID(ctx, req.FaultID)
	if err != nil {
		return nil, err
	}
	if !fault.IsOpen(target.Status) {
		return nil, apperr.Conflict("故障 %s 当前状态为 %s, 不需要派工", target.FaultNo, fault.StatusLabel(target.Status))
	}

	existing, err := s.dispatches.GetOngoingByFault(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperr.Conflict("故障 %s 已派工至班组「%s」(派工单 %s), 如需调整请使用改派", target.FaultNo, existing.TeamName, existing.DispatchNo)
	}

	var team *Team
	if req.TeamID > 0 {
		team, err = s.teams.GetByID(ctx, req.TeamID)
		if err != nil {
			return nil, err
		}
		if err := ensureTeamMatch(team, target); err != nil {
			return nil, err
		}
	} else {
		team, err = s.pickBalancedTeam(ctx, target, 0)
		if err != nil {
			return nil, err
		}
	}

	entity := &Dispatch{
		FaultID:      target.ID,
		FaultNo:      target.FaultNo,
		LampID:       target.LampID,
		LampCode:     target.LampCode,
		RoadName:     target.RoadName,
		FaultType:    target.FaultType,
		TeamID:       team.ID,
		TeamName:     team.Name,
		Status:       StatusOngoing,
		Operator:     strings.TrimSpace(req.Operator),
		DispatchedAt: time.Now(),
		Remark:       strings.TrimSpace(req.Remark),
	}
	if err := s.dispatches.CreateWithUniqueNo(ctx, entity, "PG"+entity.DispatchedAt.Format("20060102")); err != nil {
		return nil, err
	}
	return entity, nil
}

// Reassign 改派: 校验匹配情况后, 原班组的记录置为"已改派"并记录改派原因,
// 接收班组生成一条新的在办记录, 两条记录通过 ID 互相串联。
func (s *Service) Reassign(ctx context.Context, id uint, req ReassignRequest) (*Dispatch, error) {
	current, err := s.dispatches.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status != StatusOngoing {
		return nil, apperr.Conflict("派工单 %s 当前状态为 %s, 不允许改派", current.DispatchNo, StatusLabel(current.Status))
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, apperr.BadRequest("改派原因不能为空")
	}

	target, err := s.faults.GetByID(ctx, current.FaultID)
	if err != nil {
		return nil, err
	}
	if !fault.IsOpen(target.Status) {
		return nil, apperr.Conflict("故障 %s 当前状态为 %s, 不允许改派", target.FaultNo, fault.StatusLabel(target.Status))
	}

	var team *Team
	if req.TeamID > 0 {
		if req.TeamID == current.TeamID {
			return nil, apperr.BadRequest("改派目标班组不能与原班组「%s」相同", current.TeamName)
		}
		team, err = s.teams.GetByID(ctx, req.TeamID)
		if err != nil {
			return nil, err
		}
		if err := ensureTeamMatch(team, target); err != nil {
			return nil, err
		}
	} else {
		team, err = s.pickBalancedTeam(ctx, target, current.TeamID)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now()
	next := &Dispatch{
		FaultID:        current.FaultID,
		FaultNo:        current.FaultNo,
		LampID:         current.LampID,
		LampCode:       current.LampCode,
		RoadName:       current.RoadName,
		FaultType:      current.FaultType,
		TeamID:         team.ID,
		TeamName:       team.Name,
		Status:         StatusOngoing,
		Operator:       strings.TrimSpace(req.Operator),
		PrevDispatchID: &current.ID,
		DispatchedAt:   now,
		Remark:         "由派工单 " + current.DispatchNo + " 改派",
	}
	if err := s.dispatches.CreateWithUniqueNo(ctx, next, "PG"+now.Format("20060102")); err != nil {
		return nil, err
	}

	current.Status = StatusReassigned
	current.ReassignReason = reason
	current.ReplacedByID = &next.ID
	current.FinishedAt = &now
	if err := s.dispatches.Update(ctx, current); err != nil {
		return nil, err
	}
	return next, nil
}

// OnFaultResolved 故障修复或关闭时由故障模块回调, 把该故障的在办派工单批量办结。
func (s *Service) OnFaultResolved(ctx context.Context, faultID uint) error {
	_, err := s.dispatches.FinishOngoingByFault(ctx, faultID, time.Now())
	return err
}

// Candidates 返回指定故障的候选班组: 匹配情况、当前在办数量与系统推荐(在办最少的匹配班组)。
func (s *Service) Candidates(ctx context.Context, faultID uint) (*CandidatesResult, error) {
	target, err := s.faults.GetByID(ctx, faultID)
	if err != nil {
		return nil, err
	}
	teams, err := s.teams.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	ongoingByTeam, err := s.dispatches.CountOngoingByTeam(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]Candidate, 0, len(teams))
	for _, team := range teams {
		reasons := matchReasons(&team, target.FaultType, target.RoadName)
		candidates = append(candidates, Candidate{
			TeamID:      team.ID,
			TeamName:    team.Name,
			MemberCount: team.MemberCount,
			Ongoing:     ongoingByTeam[team.ID],
			Matched:     len(reasons) == 0,
			Reasons:     reasons,
		})
	}

	// 匹配的在前, 同组内按在办数量升序, 保证"尽量均衡"的推荐稳定可预期。
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Matched != candidates[j].Matched {
			return candidates[i].Matched
		}
		if candidates[i].Ongoing != candidates[j].Ongoing {
			return candidates[i].Ongoing < candidates[j].Ongoing
		}
		return candidates[i].TeamID < candidates[j].TeamID
	})
	for index := range candidates {
		if candidates[index].Matched {
			candidates[index].Recommended = true
			break
		}
	}

	return &CandidatesResult{
		FaultID:   target.ID,
		FaultNo:   target.FaultNo,
		FaultType: target.FaultType,
		RoadName:  target.RoadName,
		Teams:     candidates,
	}, nil
}

// Overview 派工概览: 全局汇总与各班组的人均维修量、超时未完工数量。
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	now := time.Now()
	overdueBefore := now.Add(-OverdueThreshold)

	teams, err := s.teams.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	dispatchTotal, err := s.dispatches.Count(ctx)
	if err != nil {
		return nil, err
	}
	byStatus, err := s.dispatches.CountByColumn(ctx, "status")
	if err != nil {
		return nil, err
	}
	ongoingByTeam, err := s.dispatches.CountOngoingByTeam(ctx)
	if err != nil {
		return nil, err
	}
	groupedByTeam, err := s.dispatches.CountByTeamGrouped(ctx)
	if err != nil {
		return nil, err
	}
	overdueByTeam, err := s.dispatches.CountOverdueByTeam(ctx, overdueBefore)
	if err != nil {
		return nil, err
	}

	stats := make([]TeamStat, 0, len(teams))
	for _, team := range teams {
		grouped := groupedByTeam[team.ID]
		done := grouped[StatusDone]
		reassigned := grouped[StatusReassigned]
		received := done + reassigned + grouped[StatusOngoing]
		stat := TeamStat{
			TeamID:        team.ID,
			TeamName:      team.Name,
			MemberCount:   team.MemberCount,
			Enabled:       team.Enabled,
			Ongoing:       ongoingByTeam[team.ID],
			Done:          done,
			ReassignedOut: reassigned,
			Received:      received,
			Overdue:       overdueByTeam[team.ID],
		}
		if team.MemberCount > 0 {
			stat.PerCapitaDone = math.Round(float64(done)/float64(team.MemberCount)*100) / 100
		}
		stats = append(stats, stat)
	}

	return &Overview{
		TeamTotal:       int64(len(teams)),
		DispatchTotal:   dispatchTotal,
		OngoingTotal:    byStatus[StatusOngoing],
		ReassignedTotal: byStatus[StatusReassigned],
		DoneTotal:       byStatus[StatusDone],
		OverdueTotal:    sumCounts(overdueByTeam),
		OverdueHours:    OverdueThreshold.Hours(),
		Teams:           stats,
		GeneratedAt:     now,
	}, nil
}

// pickBalancedTeam 在启用且匹配的班组中选择在办数量最少的一家, excludeTeamID 用于改派时排除原班组。
func (s *Service) pickBalancedTeam(ctx context.Context, target *fault.Fault, excludeTeamID uint) (*Team, error) {
	teams, err := s.teams.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	ongoingByTeam, err := s.dispatches.CountOngoingByTeam(ctx)
	if err != nil {
		return nil, err
	}

	var best *Team
	bestLoad := int64(0)
	for index := range teams {
		team := teams[index]
		if team.ID == excludeTeamID {
			continue
		}
		if len(matchReasons(&team, target.FaultType, target.RoadName)) > 0 {
			continue
		}
		load := ongoingByTeam[team.ID]
		if best == nil || load < bestLoad {
			best = &teams[index]
			bestLoad = load
		}
	}
	if best == nil {
		return nil, apperr.BadRequest("没有可承接故障类型「%s」且负责区域「%s」的可用班组, 请先维护班组", target.FaultType, target.RoadName)
	}
	return best, nil
}

// ensureTeamMatch 校验班组启用状态与故障类型/区域匹配情况, 派工与改派共用。
func ensureTeamMatch(team *Team, target *fault.Fault) error {
	if !team.Enabled {
		return apperr.BadRequest("班组「%s」已停用, 不能派工", team.Name)
	}
	if reasons := matchReasons(team, target.FaultType, target.RoadName); len(reasons) > 0 {
		return apperr.BadRequest("班组「%s」与故障 %s 不匹配: %s", team.Name, target.FaultNo, strings.Join(reasons, "; "))
	}
	return nil
}

// matchReasons 返回班组与故障不匹配的原因列表, 空切片表示完全匹配。
func matchReasons(team *Team, faultType, roadName string) []string {
	reasons := make([]string, 0, 2)
	if !team.CoversType(faultType) {
		reasons = append(reasons, "不可承接故障类型「"+faultType+"」")
	}
	if !team.CoversRoad(roadName) {
		reasons = append(reasons, "不负责区域「"+roadName+"」")
	}
	return reasons
}

// validateFaultTypes 校验班组配置的可承接故障类型均在字典范围内。
func validateFaultTypes(types []string) error {
	for _, item := range types {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		valid := false
		for _, allowed := range fault.FaultTypes() {
			if item == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return apperr.BadRequest("非法的故障类型: %s", item)
		}
	}
	return nil
}

// sumCounts 汇总分班组统计值。
func sumCounts(counts map[uint]int64) int64 {
	var total int64
	for _, count := range counts {
		total += count
	}
	return total
}
