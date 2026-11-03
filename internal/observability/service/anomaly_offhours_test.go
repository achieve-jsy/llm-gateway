package service

import (
	"testing"
	"time"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
)

// 测试固定用上海时区的具体时间点，避免依赖 time.Local。
var offhoursLoc = mustLoadOffhoursTestLocation()

func mustLoadOffhoursTestLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return loc
}

// mkCallRow 构造一条测试用 call_logs 投影行。ts 用上海时间语义构造，避免时区歧义。
func mkCallRow(userID string, y, m, d, h, min int, cost float64, model, keyID string) OffhoursCallRow {
	return OffhoursCallRow{
		UserID:   userID,
		UserName: userID + "-name",
		DeptID:   "dept-1",
		TS:       time.Date(y, time.Month(m), d, h, min, 0, 0, offhoursLoc),
		Model:    model,
		KeyID:    keyID,
		KeyName:  keyID + "-name",
		CostUSD:  cost,
	}
}

// ---------------------------------------------------------------------------
// 命中场景
// ---------------------------------------------------------------------------

// TestOffhours_Hit 验证：凌晨消耗占比 > 20% 时命中，产出 unrelated_cost/unrelated_ratio/
// top_hours/top_model/top_key，且 status 恒为 pending_review（红线）。
// 2025-01-06 是周一（工作日），用凌晨时段构造命中场景，避免与周末规则混淆。
func TestOffhours_Hit(t *testing.T) {
	rows := []OffhoursCallRow{
		// 凌晨消耗：01:00, 02:00 各花 30，共 60（模型 m-night，key k-night）。
		mkCallRow("u1", 2025, 1, 6, 1, 0, 30, "m-night", "k-night"),
		mkCallRow("u1", 2025, 1, 6, 2, 0, 30, "m-night", "k-night"),
		// 工作时段消耗：140（模型 m-day）。
		mkCallRow("u1", 2025, 1, 6, 10, 0, 140, "m-day", "k-day"),
	}
	// 总成本 200，非工作时段 60 => ratio = 0.30 > 0.20 命中。
	got := ComputeOffhoursJudgments(rows, nil, DefaultOffhoursJudgmentParams())
	if len(got) != 1 {
		t.Fatalf("want 1 hit judgment, got %d: %+v", len(got), got)
	}
	j := got[0]
	if !j.Hit {
		t.Errorf("want Hit=true")
	}
	if j.UserID != "u1" {
		t.Errorf("UserID = %q, want u1", j.UserID)
	}
	if j.TotalCostUSD != 200 {
		t.Errorf("TotalCostUSD = %v, want 200", j.TotalCostUSD)
	}
	if j.UnrelatedCostUSD != 60 {
		t.Errorf("UnrelatedCostUSD = %v, want 60", j.UnrelatedCostUSD)
	}
	wantRatio := 0.30
	if diff := j.UnrelatedRatio - wantRatio; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("UnrelatedRatio = %v, want %v", j.UnrelatedRatio, wantRatio)
	}
	if j.TopModel != "m-night" {
		t.Errorf("TopModel = %q, want m-night", j.TopModel)
	}
	if j.TopKey != "k-night-name" {
		t.Errorf("TopKey = %q, want k-night-name", j.TopKey)
	}
	if len(j.TopHours) != 1 || j.TopHours[0] != "01:00-03:00" {
		t.Errorf("TopHours = %v, want [01:00-03:00]", j.TopHours)
	}
	// 红线：status 恒为 pending_review，不能是其他任何值。
	if j.Status != AnomalyOffhoursStatus {
		t.Errorf("Status = %q, want %q (硬性红线：绝不认定违纪，恒待核查)", j.Status, AnomalyOffhoursStatus)
	}
	if j.Status != "pending_review" {
		t.Errorf("Status literal must be pending_review, got %q", j.Status)
	}
}

func TestOffhours_KeyLabelFallbackMasksID(t *testing.T) {
	const keyID = "a015704a-55e2-4a66-a8e3-63f7ff594a6c"
	if got := reportKeyLabel("", keyID); got != "Key ••••4a6c" {
		t.Fatalf("reportKeyLabel() = %q, want masked key", got)
	}
	if got := reportKeyLabel("分析专用 Key", keyID); got != "分析专用 Key" {
		t.Fatalf("reportKeyLabel() = %q, want key name", got)
	}
}

// TestOffhours_WeekendHit 验证周末消耗同样计入非工作时段（§2.1 判据的另一半）。
// 2025-01-04 是周六。
func TestOffhours_WeekendHit(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("u2", 2025, 1, 4, 15, 0, 50, "m1", "k1"),  // 周六下午，非凌晨但是周末
		mkCallRow("u2", 2025, 1, 6, 10, 0, 100, "m2", "k2"), // 周一工作时段
	}
	// ratio = 50/150 = 0.333 > 0.2 命中
	got := ComputeOffhoursJudgments(rows, nil, DefaultOffhoursJudgmentParams())
	if len(got) != 1 {
		t.Fatalf("want 1 hit (weekend counts as offhours), got %d: %+v", len(got), got)
	}
	if got[0].UnrelatedCostUSD != 50 {
		t.Errorf("UnrelatedCostUSD = %v, want 50", got[0].UnrelatedCostUSD)
	}
}

// ---------------------------------------------------------------------------
// 未命中场景
// ---------------------------------------------------------------------------

// TestOffhours_NotHit_BelowThreshold 验证：非工作时段占比 <= 20% 不命中，不产出记录。
func TestOffhours_NotHit_BelowThreshold(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("u3", 2025, 1, 6, 2, 0, 10, "m1", "k1"),  // 凌晨 10
		mkCallRow("u3", 2025, 1, 6, 10, 0, 90, "m2", "k2"), // 工作时段 90
	}
	// ratio = 10/100 = 0.10 <= 0.20 不命中
	got := ComputeOffhoursJudgments(rows, nil, DefaultOffhoursJudgmentParams())
	if len(got) != 0 {
		t.Fatalf("want 0 hits (ratio<=threshold), got %d: %+v", len(got), got)
	}
}

// TestOffhours_NotHit_NoOffhoursCost 验证：全部消耗都在工作时段，不命中。
func TestOffhours_NotHit_NoOffhoursCost(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("u4", 2025, 1, 6, 9, 0, 100, "m1", "k1"),
		mkCallRow("u4", 2025, 1, 7, 14, 0, 200, "m2", "k2"),
	}
	got := ComputeOffhoursJudgments(rows, nil, DefaultOffhoursJudgmentParams())
	if len(got) != 0 {
		t.Fatalf("want 0 hits (all in working hours), got %d: %+v", len(got), got)
	}
}

// TestOffhours_ZeroTotalCost_Skipped 验证：总成本为 0（或负）的用户跳过，不除零 panic。
func TestOffhours_ZeroTotalCost_Skipped(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("u5", 2025, 1, 6, 2, 0, 0, "m1", "k1"),
	}
	got := ComputeOffhoursJudgments(rows, nil, DefaultOffhoursJudgmentParams())
	if len(got) != 0 {
		t.Fatalf("want 0 hits for zero-cost user, got %d: %+v", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// 备案排除后降到阈值下不命中
// ---------------------------------------------------------------------------

// TestOffhours_FilingExclusion_DropsBelowThreshold 验证：备案排除是关键场景——
// 未排除时命中(>20%)，排除已备案窗口后 unrelated_cost 降到阈值以下，不再命中。
// 对齐 §2.2「命中后先扣除已备案部分」+ §0 红线「排除优先，宁可漏报不可误伤」。
func TestOffhours_FilingExclusion_DropsBelowThreshold(t *testing.T) {
	rows := []OffhoursCallRow{
		// 凌晨批处理消耗 60（应被备案排除）。
		mkCallRow("u6", 2025, 1, 6, 3, 0, 60, "m-batch", "k-batch"),
		// 凌晨另一笔真实非工作消耗 10（未备案，仍计入）。
		mkCallRow("u6", 2025, 1, 6, 4, 0, 10, "m-night", "k-night"),
		// 工作时段消耗 130。
		mkCallRow("u6", 2025, 1, 6, 10, 0, 130, "m-day", "k-day"),
	}
	// 无备案：unrelated=70, total=200, ratio=0.35 > 0.20 命中。
	withoutFiling := ComputeOffhoursJudgments(rows, nil, DefaultOffhoursJudgmentParams())
	if len(withoutFiling) != 1 {
		t.Fatalf("precondition failed: want 1 hit without filing, got %d: %+v", len(withoutFiling), withoutFiling)
	}
	if withoutFiling[0].UnrelatedCostUSD != 70 {
		t.Fatalf("precondition failed: UnrelatedCostUSD = %v, want 70", withoutFiling[0].UnrelatedCostUSD)
	}

	// 备案覆盖凌晨 03:00-03:30 的批处理窗口。
	filings := []OffhoursFilingWindow{
		{
			SubjectType: cpmodel.OffhoursFilingSubjectUser,
			SubjectID:   "u6",
			StartAt:     time.Date(2025, 1, 6, 3, 0, 0, 0, offhoursLoc),
			EndAt:       time.Date(2025, 1, 6, 3, 30, 0, 0, offhoursLoc),
			FilingType:  cpmodel.OffhoursFilingTypeBatchJob,
		},
	}
	// 排除批处理 60 后：unrelated=10, total=200, ratio=0.05 <= 0.20 不命中。
	withFiling := ComputeOffhoursJudgments(rows, filings, DefaultOffhoursJudgmentParams())
	if len(withFiling) != 0 {
		t.Fatalf("want 0 hits after filing exclusion drops ratio below threshold, got %d: %+v", len(withFiling), withFiling)
	}
}

// TestOffhours_FilingExclusion_PartialStillHits 验证：备案只排除部分消耗，
// 排除后占比仍 > 20% 时依然命中，且 ExcludedFilingTypes 正确记录被排除的类型
// （供报告呈现"已排除：跨时区、发布值班、已备案批处理"）。
func TestOffhours_FilingExclusion_PartialStillHits(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("u7", 2025, 1, 6, 1, 0, 30, "m-batch", "k-batch"), // 应被备案排除
		mkCallRow("u7", 2025, 1, 6, 2, 0, 40, "m-night", "k-night"), // 未备案，仍计入
		mkCallRow("u7", 2025, 1, 6, 10, 0, 30, "m-day", "k-day"),
	}
	filings := []OffhoursFilingWindow{
		{
			SubjectType: cpmodel.OffhoursFilingSubjectUser,
			SubjectID:   "u7",
			StartAt:     time.Date(2025, 1, 6, 1, 0, 0, 0, offhoursLoc),
			EndAt:       time.Date(2025, 1, 6, 1, 30, 0, 0, offhoursLoc),
			FilingType:  cpmodel.OffhoursFilingTypeTimezone,
		},
	}
	// total=100, unrelated(排除后)=40, ratio=0.40 > 0.20 仍命中。
	got := ComputeOffhoursJudgments(rows, filings, DefaultOffhoursJudgmentParams())
	if len(got) != 1 {
		t.Fatalf("want 1 hit (partial exclusion, still above threshold), got %d: %+v", len(got), got)
	}
	j := got[0]
	if j.UnrelatedCostUSD != 40 {
		t.Errorf("UnrelatedCostUSD = %v, want 40 (30 batch excluded)", j.UnrelatedCostUSD)
	}
	if len(j.ExcludedFilingTypes) != 1 || j.ExcludedFilingTypes[0] != cpmodel.OffhoursFilingTypeTimezone {
		t.Errorf("ExcludedFilingTypes = %v, want [%s]", j.ExcludedFilingTypes, cpmodel.OffhoursFilingTypeTimezone)
	}
	if j.Status != AnomalyOffhoursStatus {
		t.Errorf("Status = %q, want pending_review", j.Status)
	}
}

// TestOffhours_FilingByDeptAndKey 验证备案表按 dept/key 主体类型也能匹配排除
// （§2.2 offhours_filing 的 subject_type ∈ {user, dept, key}）。
func TestOffhours_FilingByDeptAndKey(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("u8", 2025, 1, 6, 2, 0, 60, "m1", "k-dutykey"),
		mkCallRow("u8", 2025, 1, 6, 10, 0, 140, "m2", "k-day"),
	}
	filings := []OffhoursFilingWindow{
		{
			SubjectType: cpmodel.OffhoursFilingSubjectKey,
			SubjectID:   "k-dutykey",
			StartAt:     time.Date(2025, 1, 6, 1, 0, 0, 0, offhoursLoc),
			EndAt:       time.Date(2025, 1, 6, 3, 0, 0, 0, offhoursLoc),
			FilingType:  cpmodel.OffhoursFilingTypeReleaseDuty,
		},
	}
	got := ComputeOffhoursJudgments(rows, filings, DefaultOffhoursJudgmentParams())
	if len(got) != 0 {
		t.Fatalf("want 0 hits (key-scoped filing excludes all offhours cost), got %d: %+v", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// 边界 / 确定性
// ---------------------------------------------------------------------------

// TestOffhours_MultiUser_Independent 验证多用户独立判定，互不干扰。
func TestOffhours_MultiUser_Independent(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("hit-user", 2025, 1, 6, 2, 0, 50, "m1", "k1"),
		mkCallRow("hit-user", 2025, 1, 6, 10, 0, 50, "m2", "k2"),
		mkCallRow("safe-user", 2025, 1, 6, 9, 0, 100, "m1", "k1"),
	}
	got := ComputeOffhoursJudgments(rows, nil, DefaultOffhoursJudgmentParams())
	if len(got) != 1 {
		t.Fatalf("want exactly 1 hit among 2 users, got %d: %+v", len(got), got)
	}
	if got[0].UserID != "hit-user" {
		t.Errorf("hit UserID = %q, want hit-user", got[0].UserID)
	}
}

// TestOffhours_Empty 验证空输入不 panic，返回空结果。
func TestOffhours_Empty(t *testing.T) {
	got := ComputeOffhoursJudgments(nil, nil, DefaultOffhoursJudgmentParams())
	if len(got) != 0 {
		t.Errorf("want 0 results for empty input, got %d", len(got))
	}
}

// TestOffhours_DefaultParams_UsedWhenThresholdZero 验证 RatioThreshold<=0 时回退默认阈值 0.20，
// 不会被误用为 ">0" 的宽松口径（避免参数误配置导致大量误命中）。
func TestOffhours_DefaultParams_UsedWhenThresholdZero(t *testing.T) {
	rows := []OffhoursCallRow{
		mkCallRow("u9", 2025, 1, 6, 2, 0, 15, "m1", "k1"),
		mkCallRow("u9", 2025, 1, 6, 10, 0, 85, "m2", "k2"),
	}
	// ratio = 0.15，用零值阈值应回退到默认 0.20，从而不命中。
	got := ComputeOffhoursJudgments(rows, nil, OffhoursJudgmentParams{TZ: "Asia/Shanghai", RatioThreshold: 0})
	if len(got) != 0 {
		t.Fatalf("want 0 hits when zero threshold falls back to default 0.20, got %d: %+v", len(got), got)
	}
}

func TestDefaultOffhoursJudgmentParams(t *testing.T) {
	p := DefaultOffhoursJudgmentParams()
	if p.TZ != "Asia/Shanghai" {
		t.Errorf("TZ = %q, want Asia/Shanghai", p.TZ)
	}
	if p.RatioThreshold != 0.20 {
		t.Errorf("RatioThreshold = %v, want 0.20", p.RatioThreshold)
	}
}
