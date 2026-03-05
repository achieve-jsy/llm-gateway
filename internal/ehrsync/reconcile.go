package ehrsync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/ehrclient"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ---------- 全量同步 ----------

// RunFullSync 执行全量同步（忽略 cursor，拉取全部数据写入 DB）
// 不生成 diff_report，仅更新 DB 并写 change_log / skip_log / stat_snapshot。
func (o *Orchestrator) RunFullSync(ctx context.Context) (*RunIncrementalResult, error) {
	batchID := uuid.NewString()
	started := time.Now()
	result := &RunIncrementalResult{
		BatchID: batchID,
		ByOrg:   make(map[string]*OrgSyncResult),
	}

	orgs, err := o.client.ListOrganizations(ctx)
	if err != nil {
		return result, err
	}
	result.CallCount++

	for _, org := range orgs.Items {
		orgStart := time.Now()
		orgResult := &OrgSyncResult{
			OrgID:     org.OrgID,
			OrgName:   org.OrgName,
			ByEntity:  make(map[string]*EntitySyncResult),
			StartedAt: orgStart,
		}
		_ = o.upsertOrg(ctx, org)

		for _, step := range []struct {
			name string
			fn   func(context.Context, string) *EntitySyncResult
		}{
			{"company", o.syncCompanies},
			{"department", o.syncDepartments},
			{"employee", o.syncEmployees},
			{"position", o.syncPositions},
		} {
			res := step.fn(ctx, org.OrgID)
			orgResult.ByEntity[step.name] = res
			result.CallCount += res.CountEHRRequests
			result.ErrorCount += res.ErrCount
		}
		orgResult.FinishedAt = time.Now()
		result.ByOrg[org.OrgID] = orgResult
	}

	execLog := &model.EhrSyncExecutionLog{
		ID:             uuid.New(),
		BatchID:        batchID,
		Entity:         "all",
		SyncType:       "full",
		Status:         "completed",
		TriggerType:    "cron",
		StartedAt:      started,
		RecordsFetched: sumOrgFetched(result),
	}
	finished := time.Now()
	execLog.FinishedAt = &finished
	execLog.DurationMs = time.Since(started).Milliseconds()
	_ = o.recorder.RecordExecution(ctx, execLog)
	_ = o.writeGlobalSnapshot(ctx, batchID, execLog, result, started, "full")

	result.DurationMs = time.Since(started).Milliseconds()
	o.log.Info("full sync done", "batchId", batchID, "durationMs", result.DurationMs)
	return result, nil
}

// syncPositions 同步岗位体系（低频，通常全量）
func (o *Orchestrator) syncPositions(ctx context.Context, orgID string) *EntitySyncResult {
	start := time.Now()
	r := &EntitySyncResult{Entity: "position"}

	repl, err := o.client.ListPositions(ctx, orgID)
	if err != nil {
		r.Error = err.Error()
		r.ErrCount++
		r.CountEHRRequests++
		r.Duration = time.Since(start)
		return r
	}
	r.CountEHRRequests++

	// 扁平化 PositionNode 树
	flat := flattenPositions(repl.Items, "")
	if len(flat) == 0 {
		r.Duration = time.Since(start)
		return r
	}

	res, err := o.repo.UpsertPositions(flat)
	if err != nil {
		r.Error = err.Error()
		r.ErrCount++
		r.Duration = time.Since(start)
		return r
	}
	r.Fetched = int64(res.Fetched)
	r.Created = int64(res.Created)
	r.Updated = int64(res.Updated)

	if res.Created+res.Updated > 0 {
		changeType := "modified"
		if res.Created > 0 {
			changeType = "created"
		}
		logs := make([]model.EhrSyncChangeLog, 0, len(flat))
		for _, p := range flat {
			logs = append(logs, model.EhrSyncChangeLog{
				ID:         uuid.New(),
				BatchID:    uuid.NewString(),
				Entity:     "position",
				EntityID:   p.PositionID,
				OrgID:      orgID,
				ChangeType: changeType,
				OldValue:   "{}",
				NewValue:   MustJSON(p),
				Source:     "full",
				OccurredAt: time.Now(),
			})
		}
		_ = o.recorder.RecordChanges(ctx, logs)
	}

	r.Duration = time.Since(start)
	return r
}

// flattenPositions 递归展开 PositionNode 树（含 NamePath）
func flattenPositions(nodes []ehrclient.PositionNode, parentPath string) []model.EhrPosition {
	out := make([]model.EhrPosition, 0, len(nodes))
	for _, n := range nodes {
		curPath := parentPath
		if curPath != "" {
			curPath += "/"
		}
		curPath += n.Name
		out = append(out, model.EhrPosition{
			PositionID: n.ID,
			Name:       n.Name,
			Type:       n.Type,
			NamePath:   curPath,
			ParentID:   parentIDFromPath(parentPath),
			SyncedAt:   time.Now(),
		})
		if len(n.Items) > 0 {
			out = append(out, flattenPositions(n.Items, curPath)...)
		}
	}
	return out
}

// parentIDFromPath 从路径取最后一段作为父节点 ID（简单实现：用 name 匹配，业务上可能不准；实际应使用 tree ID）
// 注意：EHR 接口不直接返回 parent ID，此处仅占位。如有 tree node ID 可改用。
func parentIDFromPath(path string) string {
	return ""
}

// ---------- 全量对账 ----------

// ReconcileEntity 对账某个 orgId 的某个实体，生成 diff_report + soft_delete 缺失项
// 不执行 create / update（假设增量同步已完成 DB 写入）
func ReconcileEntity(
	ctx context.Context,
	db *gorm.DB,
	batchID, orgID, entity string,
	ehrAllCount, localAllCount int64,
	missingIDs []string,
	logger *slog.Logger,
) error {
	// 1. 软删缺失实体
	if len(missingIDs) > 0 {
		switch entity {
		case "department":
			err := db.WithContext(ctx).
				Model(&model.EhrDepartment{}).
				Where("org_id = ? AND department_id IN ?", orgID, missingIDs).
				Updates(map[string]any{
					"ehr_modify_date": "",
					"synced_at":       time.Now(),
				}).Error
			if err != nil {
				return err
			}
		case "employee":
			err := db.WithContext(ctx).
				Model(&model.EhrEmployee{}).
				Where("org_id = ? AND employee_id IN ?", orgID, missingIDs).
				Update("synced_at", time.Now()).Error
			if err != nil {
				return err
			}
		case "company":
			err := db.WithContext(ctx).
				Model(&model.EhrCompany{}).
				Where("org_id = ? AND company_id IN ?", orgID, missingIDs).
				Delete(&model.EhrCompany{}).Error // 公司用硬删除（EHR 无公司即不存在）
			if err != nil {
				return err
			}
		}
	}

	// 2. 写 diff report
	report := &model.EhrSyncDiffReport{
		ID:            uuid.New(),
		BatchID:       batchID,
		OrgID:         orgID,
		Entity:        entity,
		TotalInEHR:    ehrAllCount,
		TotalInLocal:  localAllCount,
		MissingCount:  int64(len(missingIDs)),
		Status:        "success",
		Message:       fmt.Sprintf("reconcile done, %d missing", len(missingIDs)),
		CreatedAt:     time.Now(),
	}
	return db.WithContext(ctx).Create(report).Error
}

func ReconcileEntityV2(db *gorm.DB) {}

// ---------- 手动优先（override）管理 ----------

// SetOverride 在 Department / Employee 上打 override 标记（手动优先生效）
func SetOverride[T interface {
	*model.EhrDepartment | *model.EhrEmployee
}](db *gorm.DB, orgID, entityID, overrideBy, reason string) error {
	var zero T
	return db.Model(zero).
		Where("org_id = ? AND "+idColumn(zero)+" = ?", orgID, entityID).
		Updates(map[string]any{
			"manual_override_at":     time.Now(),
			"manual_override_by":     overrideBy,
			"manual_override_reason": reason,
		}).Error
}

// ClearOverride 释放 override 标记，回归 EHR 驱动
func ClearOverride[T interface {
	*model.EhrDepartment | *model.EhrEmployee
}](db *gorm.DB, orgID, entityID string) error {
	var zero T
	return db.Model(zero).
		Where("org_id = ? AND "+idColumn(zero)+" = ?", orgID, entityID).
		Updates(map[string]any{
			"manual_override_at":     nil,
			"manual_override_by":     "",
			"manual_override_reason": "",
		}).Error
}

func idColumn(m any) string {
	switch m.(type) {
	case *model.EhrDepartment:
		return "department_id"
	case *model.EhrEmployee:
		return "employee_id"
	}
	return "id"
}

// CountOverride 统计数据表中被 override 锁定的记录总数
func CountOverride(db *gorm.DB) (int64, error) {
	var deptCount, empCount int64
	if err := db.Model(&model.EhrDepartment{}).Where("manual_override_at IS NOT NULL").Count(&deptCount).Error; err != nil {
		return 0, err
	}
	if err := db.Model(&model.EhrEmployee{}).Where("manual_override_at IS NOT NULL").Count(&empCount).Error; err != nil {
		return 0, err
	}
	return deptCount + empCount, nil
}
