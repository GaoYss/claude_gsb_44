package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/apperr"
	"streetlight/pkg/pagination"
)

// TeamFilter 是班组仓储层使用的查询条件。
type TeamFilter struct {
	Keyword string
	Enabled *bool
}

// TeamRepository 负责班组的数据访问。
type TeamRepository struct {
	db *gorm.DB
}

// DispatchFilter 是派工单仓储层使用的查询条件。
type DispatchFilter struct {
	Keyword string
	TeamID  uint
	FaultID uint
	Status  string
}

// DispatchRepository 负责派工单的数据访问。
type DispatchRepository struct {
	db *gorm.DB
}

// NewTeamRepository 构造班组仓储。
func NewTeamRepository(db *gorm.DB) *TeamRepository {
	return &TeamRepository{db: db}
}

// NewDispatchRepository 构造派工单仓储。
func NewDispatchRepository(db *gorm.DB) *DispatchRepository {
	return &DispatchRepository{db: db}
}

func (r *TeamRepository) session(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

func (r *DispatchRepository) session(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// ---------- 班组 ----------

// Create 新增班组。
func (r *TeamRepository) Create(ctx context.Context, entity *Team) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("新建班组失败: %w", err)
	}
	return nil
}

// Update 保存班组全部字段。
func (r *TeamRepository) Update(ctx context.Context, entity *Team) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新班组失败: %w", err)
	}
	return nil
}

// Delete 按主键删除班组。
func (r *TeamRepository) Delete(ctx context.Context, id uint) error {
	if err := r.session(ctx).Delete(&Team{}, id).Error; err != nil {
		return fmt.Errorf("删除班组失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询班组。
func (r *TeamRepository) GetByID(ctx context.Context, id uint) (*Team, error) {
	var entity Team
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("班组不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询班组失败: %w", err)
	}
	entity.FillLists()
	return &entity, nil
}

// List 分页查询班组。
func (r *TeamRepository) List(ctx context.Context, filter TeamFilter, page pagination.Query) ([]Team, int64, error) {
	base := func() *gorm.DB {
		statement := r.session(ctx).Model(&Team{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			statement = statement.Where("name LIKE ? OR leader LIKE ?", like, like)
		}
		if filter.Enabled != nil {
			statement = statement.Where("enabled = ?", *filter.Enabled)
		}
		return statement
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计班组失败: %w", err)
	}

	entities := make([]Team, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询班组失败: %w", err)
	}
	for index := range entities {
		entities[index].FillLists()
	}
	return entities, total, nil
}

// ListAll 返回全部班组, 按主键正序。
func (r *TeamRepository) ListAll(ctx context.Context) ([]Team, error) {
	entities := make([]Team, 0)
	if err := r.session(ctx).Order("id ASC").Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("查询班组失败: %w", err)
	}
	for index := range entities {
		entities[index].FillLists()
	}
	return entities, nil
}

// ListEnabled 返回全部启用中的班组, 按主键正序。
func (r *TeamRepository) ListEnabled(ctx context.Context) ([]Team, error) {
	entities := make([]Team, 0)
	if err := r.session(ctx).Where("enabled = ?", true).Order("id ASC").Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("查询可用班组失败: %w", err)
	}
	for index := range entities {
		entities[index].FillLists()
	}
	return entities, nil
}

// ExistsByName 判断班组名称是否已存在, excludeID 用于更新时排除自身。
func (r *TeamRepository) ExistsByName(ctx context.Context, name string, excludeID uint) (bool, error) {
	var count int64
	err := r.session(ctx).Model(&Team{}).
		Where("name = ? AND id <> ?", name, excludeID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("校验班组名称失败: %w", err)
	}
	return count > 0, nil
}

// Count 统计班组总数。
func (r *TeamRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := r.session(ctx).Model(&Team{}).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("统计班组总数失败: %w", err)
	}
	return total, nil
}

// ---------- 派工单 ----------

// Create 新增派工单。
func (r *DispatchRepository) Create(ctx context.Context, entity *Dispatch) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("创建派工单失败: %w", err)
	}
	return nil
}

// CreateWithUniqueNo 生成唯一派工单号并落库, 冲突时自动重试。
func (r *DispatchRepository) CreateWithUniqueNo(ctx context.Context, entity *Dispatch, prefix string) error {
	for attempt := 0; attempt < 5; attempt++ {
		sequence, err := r.NextSequence(ctx, prefix)
		if err != nil {
			return err
		}
		entity.DispatchNo = fmt.Sprintf("%s%04d", prefix, sequence+attempt)
		err = r.Create(ctx, entity)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
	}
	return apperr.Conflict("派工单号生成冲突, 请稍后重试")
}

// NextSequence 返回指定前缀下可用的下一个流水号。
func (r *DispatchRepository) NextSequence(ctx context.Context, prefix string) (int, error) {
	var latest string
	err := r.session(ctx).Model(&Dispatch{}).
		Where("dispatch_no LIKE ?", prefix+"%").
		Order("dispatch_no DESC").
		Limit(1).
		Pluck("dispatch_no", &latest).Error
	if err != nil {
		return 0, fmt.Errorf("生成派工单号失败: %w", err)
	}
	if latest == "" {
		return 1, nil
	}
	value, convErr := strconv.Atoi(strings.TrimPrefix(latest, prefix))
	if convErr != nil {
		return 1, nil
	}
	return value + 1, nil
}

// Update 保存派工单全部字段。
func (r *DispatchRepository) Update(ctx context.Context, entity *Dispatch) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新派工单失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询派工单。
func (r *DispatchRepository) GetByID(ctx context.Context, id uint) (*Dispatch, error) {
	var entity Dispatch
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("派工单不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询派工单失败: %w", err)
	}
	return &entity, nil
}

// List 分页查询派工单。
func (r *DispatchRepository) List(ctx context.Context, filter DispatchFilter, page pagination.Query) ([]Dispatch, int64, error) {
	base := func() *gorm.DB {
		statement := r.session(ctx).Model(&Dispatch{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			statement = statement.Where(
				"dispatch_no LIKE ? OR fault_no LIKE ? OR lamp_code LIKE ? OR team_name LIKE ?",
				like, like, like, like,
			)
		}
		if filter.TeamID > 0 {
			statement = statement.Where("team_id = ?", filter.TeamID)
		}
		if filter.FaultID > 0 {
			statement = statement.Where("fault_id = ?", filter.FaultID)
		}
		if filter.Status != "" {
			statement = statement.Where("status = ?", filter.Status)
		}
		return statement
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计派工单失败: %w", err)
	}

	entities := make([]Dispatch, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询派工单失败: %w", err)
	}
	return entities, total, nil
}

// ListByFault 查询某条故障的全部派工记录, 按派工时间正序, 用于还原改派链条。
func (r *DispatchRepository) ListByFault(ctx context.Context, faultID uint) ([]Dispatch, error) {
	entities := make([]Dispatch, 0)
	err := r.session(ctx).Where("fault_id = ?", faultID).Order("dispatched_at ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询故障派工记录失败: %w", err)
	}
	return entities, nil
}

// GetOngoingByFault 查询某条故障当前在办的派工单, 不存在时返回 nil。
func (r *DispatchRepository) GetOngoingByFault(ctx context.Context, faultID uint) (*Dispatch, error) {
	var entity Dispatch
	err := r.session(ctx).
		Where("fault_id = ? AND status = ?", faultID, StatusOngoing).
		Order("dispatched_at DESC, id DESC").
		First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询在办派工单失败: %w", err)
	}
	return &entity, nil
}

// FinishOngoingByFault 把某条故障的在办派工单批量置为已完工, 返回办结数量。
func (r *DispatchRepository) FinishOngoingByFault(ctx context.Context, faultID uint, finishedAt time.Time) (int64, error) {
	result := r.session(ctx).Model(&Dispatch{}).
		Where("fault_id = ? AND status = ?", faultID, StatusOngoing).
		Updates(map[string]any{
			"status":      StatusDone,
			"finished_at": finishedAt,
			"updated_at":  finishedAt,
		})
	if result.Error != nil {
		return 0, fmt.Errorf("办结故障派工单失败: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// Count 统计派工单总数。
func (r *DispatchRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := r.session(ctx).Model(&Dispatch{}).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("统计派工单总数失败: %w", err)
	}
	return total, nil
}

// CountByColumn 按列分组统计派工单。
func (r *DispatchRepository) CountByColumn(ctx context.Context, column string) (map[string]int64, error) {
	type row struct {
		Label string
		Total int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Dispatch{}).
		Select(column + " AS label, COUNT(*) AS total").
		Group(column).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("分组统计 %s 失败: %w", column, err)
	}
	result := make(map[string]int64, len(rows))
	for _, item := range rows {
		result[item.Label] = item.Total
	}
	return result, nil
}

// CountOngoingByTeam 统计各班组当前在办数量。
func (r *DispatchRepository) CountOngoingByTeam(ctx context.Context) (map[uint]int64, error) {
	type row struct {
		TeamID uint
		Total  int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Dispatch{}).
		Select("team_id, COUNT(*) AS total").
		Where("status = ?", StatusOngoing).
		Group("team_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("统计班组在办数量失败: %w", err)
	}
	result := make(map[uint]int64, len(rows))
	for _, item := range rows {
		result[item.TeamID] = item.Total
	}
	return result, nil
}

// CountByTeamGrouped 按班组与状态分组统计, 用于概览的班组维度汇总。
func (r *DispatchRepository) CountByTeamGrouped(ctx context.Context) (map[uint]map[string]int64, error) {
	type row struct {
		TeamID uint
		Status string
		Total  int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Dispatch{}).
		Select("team_id, status, COUNT(*) AS total").
		Group("team_id, status").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("按班组统计派工单失败: %w", err)
	}
	result := make(map[uint]map[string]int64)
	for _, item := range rows {
		if result[item.TeamID] == nil {
			result[item.TeamID] = make(map[string]int64)
		}
		result[item.TeamID][item.Status] = item.Total
	}
	return result, nil
}

// CountOverdue 统计派工时间早于 before 且仍在办的派工单数量。
func (r *DispatchRepository) CountOverdue(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&Dispatch{}).
		Where("status = ? AND dispatched_at < ?", StatusOngoing, before).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计超时未完工派工单失败: %w", err)
	}
	return total, nil
}

// CountOverdueByTeam 按班组统计超时未完工数量。
func (r *DispatchRepository) CountOverdueByTeam(ctx context.Context, before time.Time) (map[uint]int64, error) {
	type row struct {
		TeamID uint
		Total  int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Dispatch{}).
		Select("team_id, COUNT(*) AS total").
		Where("status = ? AND dispatched_at < ?", StatusOngoing, before).
		Group("team_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("按班组统计超时未完工失败: %w", err)
	}
	result := make(map[uint]int64, len(rows))
	for _, item := range rows {
		result[item.TeamID] = item.Total
	}
	return result, nil
}

// CountOngoingForTeam 统计单个班组的在办数量, 删除班组前校验使用。
func (r *DispatchRepository) CountOngoingForTeam(ctx context.Context, teamID uint) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&Dispatch{}).
		Where("team_id = ? AND status = ?", teamID, StatusOngoing).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计班组在办数量失败: %w", err)
	}
	return total, nil
}

// isUniqueViolation 兼容 sqlite 与 postgres 的唯一约束冲突判断。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "unique violation")
}
