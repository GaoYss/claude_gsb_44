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

// TeamFilter 是仓储层使用的班组查询条件。
type TeamFilter struct {
	Keyword string
	Status  string
}

// DispatchFilter 是仓储层使用的派工记录查询条件。
type DispatchFilter struct {
	Keyword       string
	TeamID        uint
	FaultID       uint
	Status        string
	OverdueBefore *time.Time // 仅统计该时间之前派工且仍在办的记录(超时未完工)
}

// Repository 负责班组与派工记录的数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造派工模块仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) session(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// ---------- 班组 ----------

// CreateTeam 新增班组。
func (r *Repository) CreateTeam(ctx context.Context, entity *Team) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("新增班组失败: %w", err)
	}
	return nil
}

// UpdateTeam 保存班组全部字段。
func (r *Repository) UpdateTeam(ctx context.Context, entity *Team) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新班组失败: %w", err)
	}
	return nil
}

// DeleteTeam 按主键删除班组。
func (r *Repository) DeleteTeam(ctx context.Context, id uint) error {
	if err := r.session(ctx).Delete(&Team{}, id).Error; err != nil {
		return fmt.Errorf("删除班组失败: %w", err)
	}
	return nil
}

// GetTeamByID 按主键查询班组, 不存在时返回 404 业务错误。
func (r *Repository) GetTeamByID(ctx context.Context, id uint) (*Team, error) {
	var entity Team
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("班组不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询班组失败: %w", err)
	}
	return &entity, nil
}

// ExistsTeamByName 判断班组名称是否已被占用, excludeID 用于更新场景排除自身。
func (r *Repository) ExistsTeamByName(ctx context.Context, name string, excludeID uint) (bool, error) {
	query := r.session(ctx).Model(&Team{}).Where("name = ?", strings.TrimSpace(name))
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, fmt.Errorf("校验班组名称失败: %w", err)
	}
	return count > 0, nil
}

// ListTeams 分页查询班组。
func (r *Repository) ListTeams(ctx context.Context, filter TeamFilter, page pagination.Query) ([]Team, int64, error) {
	base := func() *gorm.DB {
		statement := r.session(ctx).Model(&Team{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			statement = statement.Where("name LIKE ? OR contact LIKE ?", like, like)
		}
		if value := strings.TrimSpace(filter.Status); value != "" {
			statement = statement.Where("status = ?", value)
		}
		return statement
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计班组总数失败: %w", err)
	}

	entities := make([]Team, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询班组列表失败: %w", err)
	}
	return entities, total, nil
}

// ListAllTeams 查询全部班组(含停用), 按名称排序, 供概览聚合使用。
func (r *Repository) ListAllTeams(ctx context.Context) ([]Team, error) {
	entities := make([]Team, 0)
	if err := r.session(ctx).Order("name ASC, id ASC").Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("查询班组列表失败: %w", err)
	}
	return entities, nil
}

// ListEnabledTeams 查询全部启用状态的班组, 供派工匹配选择。
func (r *Repository) ListEnabledTeams(ctx context.Context) ([]Team, error) {
	entities := make([]Team, 0)
	if err := r.session(ctx).Where("status = ?", TeamStatusEnabled).Order("name ASC, id ASC").Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("查询启用班组失败: %w", err)
	}
	return entities, nil
}

// CountTeams 统计班组总数。
func (r *Repository) CountTeams(ctx context.Context) (int64, error) {
	var total int64
	if err := r.session(ctx).Model(&Team{}).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("统计班组总数失败: %w", err)
	}
	return total, nil
}

// RenameTeamRecords 班组更名后同步更新其派工记录上的班组名称快照。
func (r *Repository) RenameTeamRecords(ctx context.Context, teamID uint, name string) error {
	err := r.session(ctx).Model(&DispatchRecord{}).Where("team_id = ?", teamID).Update("team_name", name).Error
	if err != nil {
		return fmt.Errorf("同步派工记录班组名称失败: %w", err)
	}
	return nil
}

// ---------- 派工记录 ----------

// CreateRecord 新增派工记录。
func (r *Repository) CreateRecord(ctx context.Context, entity *DispatchRecord) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("新增派工记录失败: %w", err)
	}
	return nil
}

// CreateRecordWithUniqueNo 生成唯一派工单号并落库, 冲突时自动重试。
func (r *Repository) CreateRecordWithUniqueNo(ctx context.Context, entity *DispatchRecord, prefix string) error {
	for attempt := 0; attempt < 5; attempt++ {
		sequence, err := r.nextSequence(ctx, prefix)
		if err != nil {
			return err
		}
		entity.DispatchNo = fmt.Sprintf("%s%04d", prefix, sequence+attempt)
		err = r.CreateRecord(ctx, entity)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
	}
	return apperr.Conflict("派工单号生成冲突, 请稍后重试")
}

// nextSequence 返回指定前缀下可用的下一个流水号。
func (r *Repository) nextSequence(ctx context.Context, prefix string) (int, error) {
	var latest string
	err := r.session(ctx).Model(&DispatchRecord{}).
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

// UpdateRecord 保存派工记录全部字段。
func (r *Repository) UpdateRecord(ctx context.Context, entity *DispatchRecord) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新派工记录失败: %w", err)
	}
	return nil
}

// ReassignInTx 在事务内完成改派: 原班组记录置为已改出留存, 接收班组生成新的在办记录。
func (r *Repository) ReassignInTx(ctx context.Context, old *DispatchRecord, fresh *DispatchRecord, prefix string) error {
	return r.session(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(old).Error; err != nil {
			return fmt.Errorf("更新原派工记录失败: %w", err)
		}
		txRepo := &Repository{db: tx}
		return txRepo.CreateRecordWithUniqueNo(ctx, fresh, prefix)
	})
}

// GetRecordByID 按主键查询派工记录, 不存在时返回 404 业务错误。
func (r *Repository) GetRecordByID(ctx context.Context, id uint) (*DispatchRecord, error) {
	var entity DispatchRecord
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("派工记录不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询派工记录失败: %w", err)
	}
	return &entity, nil
}

// GetOngoingByFault 查询某条故障当前在办的派工记录, 不存在时返回 nil。
func (r *Repository) GetOngoingByFault(ctx context.Context, faultID uint) (*DispatchRecord, error) {
	var entity DispatchRecord
	err := r.session(ctx).
		Where("fault_id = ? AND status = ?", faultID, StatusOngoing).
		Order("dispatched_at DESC, id DESC").
		First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询在办派工记录失败: %w", err)
	}
	return &entity, nil
}

// ListRecords 分页查询派工记录。
func (r *Repository) ListRecords(ctx context.Context, filter DispatchFilter, page pagination.Query) ([]DispatchRecord, int64, error) {
	base := func() *gorm.DB {
		return applyRecordFilter(r.session(ctx).Model(&DispatchRecord{}), filter)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计派工记录失败: %w", err)
	}

	entities := make([]DispatchRecord, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询派工记录失败: %w", err)
	}
	return entities, total, nil
}

// CountRecords 统计派工记录总数。
func (r *Repository) CountRecords(ctx context.Context) (int64, error) {
	var total int64
	if err := r.session(ctx).Model(&DispatchRecord{}).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("统计派工记录总数失败: %w", err)
	}
	return total, nil
}

// CountRecordsByStatus 按状态统计派工记录数量。
func (r *Repository) CountRecordsByStatus(ctx context.Context, status string) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&DispatchRecord{}).Where("status = ?", status).Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计派工记录数量失败: %w", err)
	}
	return total, nil
}

// CountOngoingByTeam 统计单个班组当前在办数量, 用于删除校验。
func (r *Repository) CountOngoingByTeam(ctx context.Context, teamID uint) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&DispatchRecord{}).
		Where("team_id = ? AND status = ?", teamID, StatusOngoing).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计班组在办数量失败: %w", err)
	}
	return total, nil
}

// CountStatusByTeam 按班组分组统计指定状态的派工记录数量。
func (r *Repository) CountStatusByTeam(ctx context.Context, status string) (map[uint]int64, error) {
	return r.countByTeam(ctx, status, nil)
}

// CountOverdueByTeam 按班组分组统计超时未完工数量(在办且派工时间早于阈值)。
func (r *Repository) CountOverdueByTeam(ctx context.Context, before time.Time) (map[uint]int64, error) {
	return r.countByTeam(ctx, StatusOngoing, &before)
}

// countByTeam 是按班组分组统计的通用实现, before 非空时附加派工时间上限。
func (r *Repository) countByTeam(ctx context.Context, status string, before *time.Time) (map[uint]int64, error) {
	type row struct {
		TeamID uint
		Total  int64
	}
	rows := make([]row, 0)
	statement := r.session(ctx).Model(&DispatchRecord{}).
		Select("team_id, COUNT(*) AS total").
		Where("status = ?", status)
	if before != nil {
		statement = statement.Where("dispatched_at < ?", *before)
	}
	if err := statement.Group("team_id").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("按班组统计派工记录失败: %w", err)
	}
	result := make(map[uint]int64, len(rows))
	for _, item := range rows {
		result[item.TeamID] = item.Total
	}
	return result, nil
}

// CountOverdue 统计全局超时未完工数量。
func (r *Repository) CountOverdue(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&DispatchRecord{}).
		Where("status = ? AND dispatched_at < ?", StatusOngoing, before).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计超时未完工数量失败: %w", err)
	}
	return total, nil
}

// applyRecordFilter 统一拼装派工记录查询条件。
func applyRecordFilter(statement *gorm.DB, filter DispatchFilter) *gorm.DB {
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
	if value := strings.TrimSpace(filter.Status); value != "" {
		statement = statement.Where("status = ?", value)
	}
	if filter.OverdueBefore != nil {
		statement = statement.Where("status = ? AND dispatched_at < ?", StatusOngoing, *filter.OverdueBefore)
	}
	return statement
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
