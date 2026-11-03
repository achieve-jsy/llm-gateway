package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 投影结果。同步服务用这三个值累计差量统计，别处不要拿来当业务状态。
const (
	ProjectionInserted  = "inserted"
	ProjectionUpdated   = "updated"
	ProjectionUnchanged = "unchanged"
)

const (
	studioSourceEHR     = "ehr"
	studioSourceManual  = "manual"
	defaultProjectionBy = "ehr-sync"
)

// EHRStudioInput 一个 EHR 事业部（type=2）要投影到 studios 的字段。
type EHRStudioInput struct {
	EHRDepartmentID  string
	Name             string
	LeaderName       string
	LeaderPassport   string
	LeaderEmployeeID string
	// Visible 只在首次建档时生效：人工隐藏名单（元气/公共关系）要在 INSERT 时就写成 false。
	// 更新时永远不碰 visible —— 单向语义，见文件头注释。
	Visible bool
}

// EHRDepartmentInput 一个 EHR 部门节点要投影到 departments 的字段。
type EHRDepartmentInput struct {
	DeptID   string
	DeptName string
	StudioID *uuid.UUID
	Status   string
}

// EHRProjectionRepo 把 EHR 同步结果投影到组织老表（studios / departments / dept_studio_map）。
//
// 铁律：
//   - 只 upsert，永不 delete + insert —— studios.id / departments.id 终身不变，
//     api_keys / user_roles / call_logs / usage_daily 里的组织快照全靠它。
//   - visible 单向：同步只会置 false，永不自动置回 true。
//   - 只写真正变化的列，差量统计按返回值累计。
type EHRProjectionRepo struct {
	db *gorm.DB
}

func NewEHRProjectionRepo(db *gorm.DB) *EHRProjectionRepo {
	return &EHRProjectionRepo{db: db}
}

// UpsertStudio 按 EHR 事业部 id 就地更新；source='manual' 的行同步完全忽略。
func (r *EHRProjectionRepo) UpsertStudio(ctx context.Context, in EHRStudioInput) (string, error) {
	var cur model.Studio
	err := r.db.WithContext(ctx).Where("ehr_department_id = ?", in.EHRDepartmentID).Take(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ehrID := in.EHRDepartmentID
		row := &model.Studio{
			ID:               uuid.NewString(),
			Name:             in.Name,
			EHRDepartmentID:  &ehrID,
			Visible:          in.Visible,
			Source:           studioSourceEHR,
			LeaderName:       in.LeaderName,
			LeaderPassport:   in.LeaderPassport,
			LeaderEmployeeID: in.LeaderEmployeeID,
		}
		// name 上有唯一约束：同名的 manual 行还在说明回填没做完，这里直接报错，
		// 而不是悄悄换个名字（那会让页面出现两个几乎同名的工作室）。
		if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
			return "", err
		}
		// GORM 对带 default 标签的零值字段会在 INSERT 时跳过 —— visible=false 被当成
		// "未设置"，落到列默认 true，隐藏名单（元气/公共关系）就白隐藏了。显式补一刀。
		if !in.Visible {
			if err := r.db.WithContext(ctx).Model(&model.Studio{}).Where("id = ?", row.ID).Update("visible", false).Error; err != nil {
				return "", err
			}
		}
		return ProjectionInserted, nil
	}
	if err != nil {
		return "", err
	}
	if cur.Source == studioSourceManual {
		return ProjectionUnchanged, nil
	}

	updates := map[string]any{}
	if cur.Name != in.Name {
		updates["name"] = in.Name
	}
	if cur.LeaderName != in.LeaderName {
		updates["leader_name"] = in.LeaderName
	}
	if cur.LeaderPassport != in.LeaderPassport {
		updates["leader_passport"] = in.LeaderPassport
	}
	if cur.LeaderEmployeeID != in.LeaderEmployeeID {
		updates["leader_employee_id"] = in.LeaderEmployeeID
	}
	if len(updates) == 0 {
		return ProjectionUnchanged, nil
	}
	if err := r.db.WithContext(ctx).Model(&model.Studio{}).Where("id = ?", cur.ID).Updates(updates).Error; err != nil {
		return "", err
	}
	return ProjectionUpdated, nil
}

// StudioIDByEHRDept 按 EHR 事业部 id 取老表 studios.id。
//
// 部门投影必须写这个 id：departments.studio_id 上有 FK 指向 studios.id，
// 拿 ai_app_studios.id 去写会直接违反外键（首轮同步就这么炸的，见 ledger）。
func (r *EHRProjectionRepo) StudioIDByEHRDept(ctx context.Context, ehrDeptID string) (uuid.UUID, error) {
	var row model.Studio
	if err := r.db.WithContext(ctx).Select("id").Where("ehr_department_id = ?", ehrDeptID).Take(&row).Error; err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(row.ID)
}

// UpsertDepartment 按 EHR 部门 id 就地更新名称/归属/状态。
// status=deleted 的部门被 EHR 重新看到时会改回 active（EHR 才是权威）。
func (r *EHRProjectionRepo) UpsertDepartment(ctx context.Context, in EHRDepartmentInput) (string, uuid.UUID, error) {
	var cur model.OrgDepartment
	err := r.db.WithContext(ctx).Where("dept_id = ?", in.DeptID).Take(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		id := uuid.New()
		row := &model.OrgDepartment{
			ID:       id.String(),
			DeptID:   in.DeptID,
			DeptName: in.DeptName,
			StudioID: uuidPtrToString(in.StudioID),
			Status:   model.DeptStatus(in.Status),
		}
		if row.Status == "" {
			row.Status = model.DeptStatusPendingStudio
		}
		if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
			return "", uuid.Nil, err
		}
		return ProjectionInserted, id, nil
	}
	if err != nil {
		return "", uuid.Nil, err
	}
	curID, perr := uuid.Parse(cur.ID)
	if perr != nil {
		return "", uuid.Nil, perr
	}

	updates := map[string]any{}
	if cur.DeptName != in.DeptName {
		updates["dept_name"] = in.DeptName
	}
	if !sameStudioID(cur.StudioID, in.StudioID) {
		updates["studio_id"] = in.StudioID
	}
	if string(cur.Status) != in.Status {
		updates["status"] = in.Status
	}
	if len(updates) == 0 {
		return ProjectionUnchanged, curID, nil
	}
	if err := r.db.WithContext(ctx).Model(&model.OrgDepartment{}).Where("id = ?", cur.ID).Updates(updates).Error; err != nil {
		return "", uuid.Nil, err
	}
	return ProjectionUpdated, curID, nil
}

// OrgLeaderInput 负责人 upsert 入参。身份 = employee_id > passport > name（与唯一索引同表达式）。
type OrgLeaderInput struct {
	NodeType   string
	NodeID     uuid.UUID
	Name       string
	Passport   string
	EmployeeID string
	Source     string
}

func leaderIdentity(name, passport, employeeID string) string {
	switch {
	case employeeID != "":
		return employeeID
	case passport != "":
		return passport
	default:
		return name
	}
}

// UpsertLeader 增量覆盖与保留：行存在只更新姓名/证件属性（**source 保持原值**，manual 不被 ehr 洗掉），
// 不存在才插入；任何路径都不删除 —— 旧 EHR 负责人与手动加的人因此天然保留。
//
// ponytail: 身份表达式取"当前输入"的优先级链；若旧行只有 passport、新输入带来 employee_id，
// 会被当成两个人插第二行。EHR 与花名册对同一人两个字段都稳定带全，实际碰不到；真碰到再补合并。
func (r *EHRProjectionRepo) UpsertLeader(ctx context.Context, in OrgLeaderInput) (string, error) {
	identity := leaderIdentity(in.Name, in.Passport, in.EmployeeID)
	var cur model.OrgNodeLeader
	err := r.db.WithContext(ctx).
		Where("node_type = ? AND node_id = ? AND (CASE WHEN employee_id <> '' THEN employee_id WHEN passport <> '' THEN passport ELSE name END) = ?",
			in.NodeType, in.NodeID, identity).
		Take(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row := &model.OrgNodeLeader{
			ID: uuid.NewString(), NodeType: in.NodeType, NodeID: in.NodeID.String(),
			Name: in.Name, Passport: in.Passport, EmployeeID: in.EmployeeID, Source: in.Source,
		}
		if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
			return "", err
		}
		return ProjectionInserted, nil
	}
	if err != nil {
		return "", err
	}
	updates := map[string]any{}
	if cur.Name != in.Name {
		updates["name"] = in.Name
	}
	if cur.Passport != in.Passport {
		updates["passport"] = in.Passport
	}
	if cur.EmployeeID != in.EmployeeID {
		updates["employee_id"] = in.EmployeeID
	}
	if len(updates) == 0 {
		return ProjectionUnchanged, nil
	}
	if err := r.db.WithContext(ctx).Model(&model.OrgNodeLeader{}).Where("id = ?", cur.ID).Updates(updates).Error; err != nil {
		return "", err
	}
	return ProjectionUpdated, nil
}

// ListLeaders 全量返回（量级百行），前端按节点分组。
func (r *EHRProjectionRepo) ListLeaders(ctx context.Context) ([]model.OrgNodeLeader, error) {
	var rows []model.OrgNodeLeader
	err := r.db.WithContext(ctx).Order("node_type, node_id, created_at").Find(&rows).Error
	return rows, err
}

// NodeExists 手动加负责人前校验节点真实存在，避免孤儿行。
func (r *EHRProjectionRepo) NodeExists(ctx context.Context, nodeType string, nodeID uuid.UUID) (bool, error) {
	var count int64
	var err error
	switch nodeType {
	case model.LeaderNodeStudio:
		err = r.db.WithContext(ctx).Model(&model.Studio{}).Where("id = ?", nodeID.String()).Count(&count).Error
	case model.LeaderNodeDept:
		err = r.db.WithContext(ctx).Model(&model.OrgDepartment{}).Where("id = ?", nodeID.String()).Count(&count).Error
	default:
		return false, fmt.Errorf("unknown node_type %q", nodeType)
	}
	return count > 0, err
}

// UpsertMapping 部门→工作室映射以 EHR 为准：任何来源（import/manual）都会被覆盖。
func (r *EHRProjectionRepo) UpsertMapping(ctx context.Context, deptID string, studioID uuid.UUID, updatedBy string) (string, error) {
	if updatedBy == "" {
		updatedBy = defaultProjectionBy
	}
	studio := studioID.String()
	var cur model.DeptStudioMap
	err := r.db.WithContext(ctx).Where("dept_id = ?", deptID).Take(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row := &model.DeptStudioMap{DeptID: deptID, StudioID: studio, Source: studioSourceEHR, UpdatedBy: updatedBy}
		if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
			return "", err
		}
		return ProjectionInserted, nil
	}
	if err != nil {
		return "", err
	}
	if cur.StudioID == studio && cur.Source == studioSourceEHR {
		return ProjectionUnchanged, nil
	}
	updates := map[string]any{"studio_id": studio, "source": studioSourceEHR, "updated_by": updatedBy}
	if err := r.db.WithContext(ctx).Model(&model.DeptStudioMap{}).Where("dept_id = ?", deptID).Updates(updates).Error; err != nil {
		return "", err
	}
	return ProjectionUpdated, nil
}

// HideStudiosNotIn 把 EHR 里消失的工作室置为不可见（不删行：历史快照还要 join）。
// keep 为空时直接返回 0 —— EHR 异常返回空列表不能把整库隐藏。
func (r *EHRProjectionRepo) HideStudiosNotIn(ctx context.Context, keepEHRDeptIDs []string) (int, error) {
	if len(keepEHRDeptIDs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.Studio{}).
		Where("source = ?", studioSourceEHR).
		Where("visible = ?", true).
		Where("ehr_department_id NOT IN ?", keepEHRDeptIDs).
		Update("visible", false)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// ArchiveDepartmentsNotIn 把 EHR 里消失的部门软删除（status=deleted，行保留）。
func (r *EHRProjectionRepo) ArchiveDepartmentsNotIn(ctx context.Context, keepDeptIDs []string) (int, error) {
	if len(keepDeptIDs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.OrgDepartment{}).
		Where("status <> ?", string(model.DeptStatusDeleted)).
		Where("dept_id NOT IN ?", keepDeptIDs).
		Update("status", string(model.DeptStatusDeleted))
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// uuidPtrToString departments.studio_id 是 varchar 型 uuid（OrgDepartment 用 *string 承接）。
func uuidPtrToString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func sameStudioID(cur *string, next *uuid.UUID) bool {
	if cur == nil || next == nil {
		return cur == nil && next == nil
	}
	return *cur == next.String()
}
