package handler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// usageReportAnomalyGoSource 定位并读取同包 usage_report_anomaly_handler.go 源码（测试运行于同目录）。
func usageReportAnomalyGoSource() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "usage_report_anomaly_handler.go"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// anomalyStatusEnumWant 5 态合法值（与 t14/report 常量对齐）。
var anomalyStatusEnumWant = []string{"待确认", "已确认正常", "修复中", "已恢复", "误报"}

// TestAnomalyStatusEnum 锁定 5 态枚举合法值（非法状态不通过）。
func TestAnomalyStatusEnum(t *testing.T) {
	if len(anomalyStatusEnum) != len(anomalyStatusEnumWant) {
		t.Fatalf("anomalyStatusEnum 应有 %d 个状态, got %d", len(anomalyStatusEnumWant), len(anomalyStatusEnum))
	}
	for _, s := range anomalyStatusEnumWant {
		if !anomalyStatusEnum[s] {
			t.Errorf("状态 %q 缺失", s)
		}
	}
	if anomalyStatusEnum["illegal"] {
		t.Errorf("非法状态不应通过校验")
	}
}

// TestAnomalyWindowStartStr_MinutePrecision 锁定回读窗口串为 +08:00 RFC3339 / 分钟精度。
func TestAnomalyWindowStartStr_MinutePrecision(t *testing.T) {
	ts := time.Date(2026, 8, 28, 8, 58, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	if got := anomalyWindowStartStr(ts); got != "2026-08-28T08:58:00+08:00" {
		t.Fatalf("window_start 串不符: got %q", got)
	}
}

// TestAnomalyWindowStartStr_TruncateSeconds 锁定秒/毫秒截断（分钟精度）。
func TestAnomalyWindowStartStr_TruncateSeconds(t *testing.T) {
	ts := time.Date(2026, 8, 28, 8, 58, 30, 999000000, time.FixedZone("Asia/Shanghai", 8*3600))
	if got := anomalyWindowStartStr(ts); got != "2026-08-28T08:58:00+08:00" {
		t.Fatalf("应截断到分钟: got %q", got)
	}
}

// TestAnomalyWindowStartStr_ZeroTime 锁定零时间哨兵（非按窗规则）回显空串。
func TestAnomalyWindowStartStr_ZeroTime(t *testing.T) {
	if got := anomalyWindowStartStr(time.Time{}); got != "" {
		t.Fatalf("零时间应回显空串, got %q", got)
	}
}

// TestAnomalyWindowStartParse 锁定 +08:00 RFC3339 解析（分钟精度可逆）+ 空串/非法串处理。
func TestAnomalyWindowStartParse(t *testing.T) {
	ts, ok := anomalyWindowStartParse("2026-08-28T08:58:00+08:00")
	if !ok {
		t.Fatalf("应解析成功")
	}
	if got := anomalyWindowStartStr(ts); got != "2026-08-28T08:58:00+08:00" {
		t.Fatalf("解析-格式化应可逆: got %q", got)
	}
	if _, ok := anomalyWindowStartParse("not-a-time"); ok {
		t.Fatalf("非法串不应解析成功")
	}
	if _, ok := anomalyWindowStartParse(""); ok {
		t.Fatalf("空串应返回 ok=false（非按窗规则）")
	}
}

// TestReportAnomalyStatus_UsesAnomalyRecordModel 锁定读回接口操作 model.AnomalyRecord（契约稳定）。
func TestReportAnomalyStatus_UsesAnomalyRecordModel(t *testing.T) {
	src, err := usageReportAnomalyGoSource()
	if err != nil {
		t.Fatalf("读取源码: %v", err)
	}
	if !strings.Contains(src, "model.AnomalyRecord{") {
		t.Fatalf("读回接口必须使用 model.AnomalyRecord")
	}
}

// TestReportAnomalyFeedback_EnumGuard 锁定反馈接口做 5 态枚举校验（非法状态 400）。
func TestReportAnomalyFeedback_EnumGuard(t *testing.T) {
	src, err := usageReportAnomalyGoSource()
	if err != nil {
		t.Fatalf("读取源码: %v", err)
	}
	if !strings.Contains(src, "anomalyStatusEnum[req.Status]") {
		t.Fatalf("反馈接口必须校验状态枚举")
	}
}

// TestReportAnomalyUpsert_UniqKey 锁定 upsert 唯一键五元组 + 不覆盖用户反馈态语义。
func TestReportAnomalyUpsert_UniqKey(t *testing.T) {
	src, err := usageReportAnomalyGoSource()
	if err != nil {
		t.Fatalf("读取源码: %v", err)
	}
	for _, want := range []string{"scope_level", "scope_id", "rule", "window_start", "member_id"} {
		if !strings.Contains(src, want) {
			t.Errorf("upsert 唯一键缺少字段 %q", want)
		}
	}
	if !strings.Contains(src, "不覆盖用户反馈态") {
		t.Errorf("缺少「已存在不覆盖用户反馈态」语义注释")
	}
}
