package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

// 本文件实现严重异常 3.6「与工作无关占比过高」的层1时间窗判定，对齐
// docs/token-usage-anomaly-work-unrelated-design.md §2。
//
// ============================================================================
// 硬性红线（§0，比其他五类判定都硬，任何改动都不得违反）：
//
//   1. 不认定违纪：无论时间窗还是后续大模型语义判定，结论天花板永远是「待核查」，
//      本层判定绝不、也不能输出"违规/滥用/私用"之类的定论。
//   2. 排除优先：宁可漏报（未接备案表时结论仍只到待核查），不可误伤——命中前先扣除
//      已备案（批处理/跨时区/发布值班）部分。
//
// 因此本文件导出的 AnomalyOffhoursStatus 是唯一合法状态值，ComputeOffhoursJudgments
// 的每一条命中结果都硬编码这个常量，不接受、也不提供任何入参把它改成别的状态
// （negotiation/负责人终裁只发生在 anomaly_review 状态机里，不在本层）。
// ============================================================================

// AnomalyOffhoursStatus 是层1时间窗判定命中后唯一合法的初始状态：待核查。
// 红线：本文件中任何函数都不得、也不会产出除此之外的状态值。
const AnomalyOffhoursStatus = "pending_review"

// offhoursDefaultTZ 是层1时间窗判定的默认时区（凌晨/周末的判定基准），对齐设计文档 §2.1。
const offhoursDefaultTZ = "Asia/Shanghai"

// offhoursDefaultRatioThreshold 是命中阈值：非工作时段（未备案）消耗占个人总成本 > 20%。
const offhoursDefaultRatioThreshold = 0.20

// OffhoursCallRow 是层1时间窗判定的最小输入行（从 call_logs 投影而来）。
type OffhoursCallRow struct {
	RequestID string    `gorm:"column:request_id"`
	UserID    string    `gorm:"column:user_id"`
	UserName  string    `gorm:"column:user_name"`
	DeptID    string    `gorm:"column:dept_id"`
	TS        time.Time `gorm:"column:ts"`
	Model     string    `gorm:"column:model"`
	KeyID     string    `gorm:"column:key_id"`
	KeyName   string    `gorm:"column:key_name"`
	CostUSD   float64   `gorm:"column:cost_usd"`
}

// OffhoursFilingWindow 是已备案窗口（来自 offhours_filing 表），命中判定前先用它排除
// 「批处理/跨时区/发布值班」等已知的非工作时段消耗（§2.2，宁可漏报不可误伤）。
type OffhoursFilingWindow struct {
	SubjectType string // user | dept | key，见 cpmodel.OffhoursFilingSubject*
	SubjectID   string
	StartAt     time.Time
	EndAt       time.Time
	FilingType  string // 批处理 | 跨时区 | 发布值班，见 cpmodel.OffhoursFilingType*
}

// OffhoursJudgmentParams 层1时间窗判定参数。
type OffhoursJudgmentParams struct {
	// TZ 判定凌晨(00:00-07:00)/周末所用的时区，默认 Asia/Shanghai。
	TZ string
	// RatioThreshold 命中阈值，默认 0.20（>20% 命中，严格大于）。
	RatioThreshold float64
}

// DefaultOffhoursJudgmentParams 返回设计文档 §2 给出的默认参数。
func DefaultOffhoursJudgmentParams() OffhoursJudgmentParams {
	return OffhoursJudgmentParams{
		TZ:             offhoursDefaultTZ,
		RatioThreshold: offhoursDefaultRatioThreshold,
	}
}

// OffhoursJudgment 是层1时间窗判定的单个用户产出（§2.3）。
//
// 红线：Status 恒为 AnomalyOffhoursStatus（"pending_review"）；本类型的任何实例都不应、
// 也不会被本文件的函数赋成其他状态值——由负责人在 anomaly_review 状态机（§5）里终裁，
// 不在这一层。
type OffhoursJudgment struct {
	UserID   string
	UserName string
	DeptID   string

	// TotalCostUSD 个人总成本（不限时段）。
	TotalCostUSD float64
	// UnrelatedCostUSD 未备案的非工作时段消耗金额（已扣除已备案部分）。
	UnrelatedCostUSD float64
	// UnrelatedRatio = UnrelatedCostUSD / TotalCostUSD（> RatioThreshold 才命中）。
	UnrelatedRatio float64

	// Hit 恒为 true：本结构体只由 ComputeOffhoursJudgments 在命中时构造，
	// 未命中用户不产出记录（对齐设计文档 §2.1 SQL 的 WHERE 过滤语义）。
	Hit bool

	// TopHours 非工作时段消耗最集中的时段（格式 "HH:00-HH:00"，取消耗最高的连续时段）。
	TopHours []string
	// TopModel 非工作时段消耗最高的模型。
	TopModel string
	// TopKey 非工作时段消耗最高的 Key 展示名；优先 KeyName，缺失时使用脱敏 ID。
	TopKey string

	// ExcludedFilingTypes 本次判定排除掉的已备案类型（去重、排序），供报告呈现
	// 「已排除：跨时区、发布值班、已备案批处理」（§6.1）。
	ExcludedFilingTypes []string

	// Status 恒为 AnomalyOffhoursStatus，见类型注释红线。
	Status string

	// HitRows 该用户在非工作时段且未备案的所有原始调用行（用于层2正文语义抽取）。
	HitRows []OffhoursCallRow
}

// ComputeOffhoursJudgments 是层1时间窗判定的纯函数实现（不依赖 DB，可独立单测）。
//
// 算法（§2.1/§2.2）：
//  1. 按 user_id 聚合：TotalCostUSD = Σ CostUSD（不限时段）。
//  2. 一行落在「凌晨(00:00-07:00 TZ) 或 周末」才算非工作时段候选。
//  3. 候选行若命中任一 offhours_filing 窗口（按 user/dept/key 三种主体类型匹配，
//     时间落在 [StartAt, EndAt] 闭区间——对齐 §2.2 SQL 的 BETWEEN 语义）→ 排除，
//     不计入 UnrelatedCostUSD（宁可漏报不可误伤）。
//  4. 未被排除的候选行累加进 UnrelatedCostUSD，同时按小时/模型/Key 分桶供 Top 字段用。
//  5. UnrelatedRatio = UnrelatedCostUSD / TotalCostUSD；> params.RatioThreshold 才命中，
//     命中的用户才产出一条 OffhoursJudgment（Status 恒 pending_review，红线见上）。
//  6. TotalCostUSD <= 0 的用户跳过（避免除零，也无意义判定）。
//
// 返回结果按 UserID 升序排列，保证确定性。
func ComputeOffhoursJudgments(rows []OffhoursCallRow, filings []OffhoursFilingWindow, params OffhoursJudgmentParams) []OffhoursJudgment {
	loc := resolveOffhoursLocation(params.TZ)
	threshold := params.RatioThreshold
	if threshold <= 0 {
		threshold = offhoursDefaultRatioThreshold
	}
	idx := buildOffhoursFilingIndex(filings)

	type userAgg struct {
		userID, userName, deptID string
		totalCost                float64
		unrelatedCost            float64
		hourCost                 [24]float64
		modelCost                map[string]float64
		keyCost                  map[string]float64
		excludedTypes            map[string]bool
		hitRows                  []OffhoursCallRow
	}

	aggs := make(map[string]*userAgg)
	for _, r := range rows {
		a, ok := aggs[r.UserID]
		if !ok {
			a = &userAgg{
				userID:        r.UserID,
				userName:      r.UserName,
				deptID:        r.DeptID,
				modelCost:     make(map[string]float64),
				keyCost:       make(map[string]float64),
				excludedTypes: make(map[string]bool),
			}
			aggs[r.UserID] = a
		}
		if a.userName == "" && r.UserName != "" {
			a.userName = r.UserName
		}
		if a.deptID == "" && r.DeptID != "" {
			a.deptID = r.DeptID
		}
		a.totalCost += r.CostUSD

		if !isOffhoursTimestamp(r.TS, loc) {
			continue
		}
		if matched := idx.matchedFilingTypes(r); len(matched) > 0 {
			for _, t := range matched {
				a.excludedTypes[t] = true
			}
			continue // 已备案，排除，不计入 unrelated_cost（§2.2）
		}
		a.unrelatedCost += r.CostUSD
		a.hitRows = append(a.hitRows, r)
		hour := r.TS.In(loc).Hour()
		a.hourCost[hour] += r.CostUSD
		if r.Model != "" {
			a.modelCost[r.Model] += r.CostUSD
		}
		if keyLabel := reportKeyLabel(r.KeyName, r.KeyID); keyLabel != "" {
			a.keyCost[keyLabel] += r.CostUSD
		}
	}

	userIDs := make([]string, 0, len(aggs))
	for id := range aggs {
		userIDs = append(userIDs, id)
	}
	sort.Strings(userIDs)

	out := make([]OffhoursJudgment, 0)
	for _, id := range userIDs {
		a := aggs[id]
		if a.totalCost <= 0 {
			continue // 避免除零，也无意义判定
		}
		ratio := a.unrelatedCost / a.totalCost
		if ratio <= threshold {
			continue // 未命中：语义单独/时间窗单独都不进 3.6（§4）
		}

		excluded := make([]string, 0, len(a.excludedTypes))
		for t := range a.excludedTypes {
			excluded = append(excluded, t)
		}
		sort.Strings(excluded)

		out = append(out, OffhoursJudgment{
			UserID:              a.userID,
			UserName:            a.userName,
			DeptID:              a.deptID,
			TotalCostUSD:        a.totalCost,
			UnrelatedCostUSD:    a.unrelatedCost,
			UnrelatedRatio:      ratio,
			Hit:                 true,
			TopHours:            topOffhoursRange(a.hourCost),
			TopModel:            topByCost(a.modelCost),
			TopKey:              topByCost(a.keyCost),
			ExcludedFilingTypes: excluded,
			// 红线：唯一合法状态，不接受外部覆盖。
			Status:  AnomalyOffhoursStatus,
			HitRows: a.hitRows,
		})
	}
	return out
}

func reportKeyLabel(keyName, keyID string) string {
	if name := strings.TrimSpace(keyName); name != "" {
		return name
	}
	id := strings.TrimSpace(keyID)
	if id == "" {
		return ""
	}
	if len(id) <= 4 {
		return "Key ••••" + id
	}
	return "Key ••••" + id[len(id)-4:]
}

// isOffhoursTimestamp 判定时间戳是否落在「凌晨(00:00-07:00) 或 周末」（对齐 §2.1 SQL）。
func isOffhoursTimestamp(ts time.Time, loc *time.Location) bool {
	local := ts.In(loc)
	if local.Hour() < 7 {
		return true
	}
	switch local.Weekday() {
	case time.Saturday, time.Sunday:
		return true
	default:
		return false
	}
}

// resolveOffhoursLocation 解析时区，失败回退 Asia/Shanghai，仍失败回退 UTC（不 panic）。
func resolveOffhoursLocation(tz string) *time.Location {
	if tz == "" {
		tz = offhoursDefaultTZ
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc, err = time.LoadLocation(offhoursDefaultTZ)
	}
	if err != nil || loc == nil {
		return time.UTC
	}
	return loc
}

// offhoursFilingIndex 是 offhours_filing 窗口按主体类型分桶的查找索引，避免 O(n*m) 全量扫描。
type offhoursFilingIndex struct {
	byUser map[string][]OffhoursFilingWindow
	byDept map[string][]OffhoursFilingWindow
	byKey  map[string][]OffhoursFilingWindow
}

func buildOffhoursFilingIndex(filings []OffhoursFilingWindow) offhoursFilingIndex {
	idx := offhoursFilingIndex{
		byUser: make(map[string][]OffhoursFilingWindow),
		byDept: make(map[string][]OffhoursFilingWindow),
		byKey:  make(map[string][]OffhoursFilingWindow),
	}
	for _, f := range filings {
		switch f.SubjectType {
		case cpmodel.OffhoursFilingSubjectUser:
			idx.byUser[f.SubjectID] = append(idx.byUser[f.SubjectID], f)
		case cpmodel.OffhoursFilingSubjectDept:
			idx.byDept[f.SubjectID] = append(idx.byDept[f.SubjectID], f)
		case cpmodel.OffhoursFilingSubjectKey:
			idx.byKey[f.SubjectID] = append(idx.byKey[f.SubjectID], f)
		}
	}
	return idx
}

// matchedFilingTypes 返回该行命中的所有已备案类型（按 user/dept/key 三种主体匹配，去重排序）。
// 时间窗匹配用闭区间 [StartAt, EndAt]，对齐 §2.2 SQL 的 `cl.ts BETWEEN f.start_at AND f.end_at`。
func (idx offhoursFilingIndex) matchedFilingTypes(row OffhoursCallRow) []string {
	seen := make(map[string]bool)
	check := func(windows []OffhoursFilingWindow) {
		for _, w := range windows {
			if !row.TS.Before(w.StartAt) && !row.TS.After(w.EndAt) {
				seen[w.FilingType] = true
			}
		}
	}
	check(idx.byUser[row.UserID])
	check(idx.byDept[row.DeptID])
	check(idx.byKey[row.KeyID])

	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// topOffhoursRange 从 24 小时消耗分桶里找出消耗最集中的连续时段，格式化为 "HH:00-HH:00"。
// 无消耗（数组全零）返回 nil。多个不连续时段时取总消耗最高的一段（并列按起始小时升序）。
func topOffhoursRange(hourCost [24]float64) []string {
	type run struct {
		start, end int // [start, end) 小时区间
		cost       float64
	}
	var runs []run
	i := 0
	for i < 24 {
		if hourCost[i] <= 0 {
			i++
			continue
		}
		j := i
		sum := 0.0
		for j < 24 && hourCost[j] > 0 {
			sum += hourCost[j]
			j++
		}
		runs = append(runs, run{start: i, end: j, cost: sum})
		i = j
	}
	if len(runs) == 0 {
		return nil
	}
	sort.SliceStable(runs, func(a, b int) bool {
		if runs[a].cost != runs[b].cost {
			return runs[a].cost > runs[b].cost
		}
		return runs[a].start < runs[b].start
	})
	top := runs[0]
	endHour := top.end % 24
	return []string{fmt.Sprintf("%02d:00-%02d:00", top.start, endHour)}
}

// topByCost 返回 costMap 中消耗最高的 key；并列时按 key 字典序取最小（确定性）。空 map 返回 ""。
func topByCost(costMap map[string]float64) string {
	if len(costMap) == 0 {
		return ""
	}
	keys := make([]string, 0, len(costMap))
	for k := range costMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	best := keys[0]
	bestCost := costMap[best]
	for _, k := range keys[1:] {
		if c := costMap[k]; c > bestCost {
			bestCost = c
			best = k
		}
	}
	return best
}

// ============================================================================
// DB 取数辅助（供上层 report 装配调用；本身不做判定，只投影 call_logs / offhours_filing）。
// ============================================================================

// QueryOffhoursCallRows 按设计文档 §2.1 从 call_logs 投影层1时间窗判定所需的最小行集合，
// 排除 Service Key 调用（个人口径）。scopeWhere/scopeArgs 用于叠加部门/工作室等范围过滤，
// 传空字符串表示不叠加范围过滤（全量个人口径）。
func QueryOffhoursCallRows(ctx context.Context, db *gorm.DB, from, to time.Time, scopeWhere string, scopeArgs ...any) ([]OffhoursCallRow, error) {
	var rows []OffhoursCallRow
	q := db.WithContext(ctx).
		Table("call_logs").
		Select("request_id, user_id, COALESCE(user_name,'') AS user_name, COALESCE(dept_id,'') AS dept_id, "+
			"ts, model, key_id, COALESCE(key_name,'') AS key_name, est_cost_usd AS cost_usd").
		Where("ts >= ? AND ts < ?", from, to).
		Where(obsmodel.WhereExcludeServiceKeyLogs)
	if scopeWhere != "" {
		q = q.Where(scopeWhere, scopeArgs...)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("anomaly_offhours: query call_logs: %w", err)
	}
	return rows, nil
}

// QueryOffhoursFilingWindows 从 offhours_filing 读取与 [from,to) 有交集的备案窗口
// （§2.2；P0 无备案表数据时返回空切片，判定层按「未接备案表」的口径继续，不报错）。
func QueryOffhoursFilingWindows(ctx context.Context, db *gorm.DB, from, to time.Time) ([]OffhoursFilingWindow, error) {
	var filings []cpmodel.OffhoursFiling
	if err := db.WithContext(ctx).
		Where("start_at < ? AND end_at >= ?", to, from).
		Find(&filings).Error; err != nil {
		return nil, fmt.Errorf("anomaly_offhours: query offhours_filing: %w", err)
	}
	windows := make([]OffhoursFilingWindow, 0, len(filings))
	for _, f := range filings {
		windows = append(windows, OffhoursFilingWindow{
			SubjectType: f.SubjectType,
			SubjectID:   f.SubjectID,
			StartAt:     f.StartAt,
			EndAt:       f.EndAt,
			FilingType:  f.FilingType,
		})
	}
	return windows, nil
}
