package handler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// reportServiceKeysGoSource 定位并读取同包 usage_handler.go 源码（测试运行于同目录）。
func reportServiceKeysGoSource() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	src := filepath.Join(filepath.Dir(thisFile), "usage_handler.go")
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// TestReportServiceKeysOwnerCoalesceNullIf 锁定 ReportServiceKeys 中 Owner 回填的 SQL 表达式：
// 必须是 COALESCE(NULLIF(MAX(call_logs.user_name),”), MAX(ak.user_name), ”)。
// 原因：采集侧把 service key 的 call_logs.user_name 置成【空字符串 ”】而非 NULL，
// 若写成 COALESCE(MAX(call_logs.user_name), MAX(ak.user_name), ”)，” 会被当作非空短路，
// 导致 LEFT JOIN api_keys 的 ak.user_name 永远取不到，Owner 恒为空（生产实测为空正是此 bug）。
//
// 该单元测试用读取同包源码的方式来断言 SQL 表达式，避免与实现漂移；
// 失败即说明有人把 NULLIF 归一回退掉，Owner 会再次恒为空。
func TestReportServiceKeysOwnerCoalesceNullIf(t *testing.T) {
	const wantOwnerExpr = `NULLIF(MAX(call_logs.user_name), '')`
	const badOwnerExpr = `COALESCE(MAX(call_logs.user_name), MAX(ak.user_name), '')`

	src, err := reportServiceKeysGoSource()
	if err != nil {
		t.Fatalf("读取 ReportServiceKeys 源码: %v", err)
	}
	if !strings.Contains(src, wantOwnerExpr) {
		t.Fatalf("ReportServiceKeys owner_name 未用 NULLIF 归一空串，Owner 会恒为空。期望包含 %q", wantOwnerExpr)
	}
	if strings.Contains(src, badOwnerExpr) {
		t.Fatalf("发现会短路的 owner_name SQL（无 NULLIF 归一空串）：%q", badOwnerExpr)
	}
}
