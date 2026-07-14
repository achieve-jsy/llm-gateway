package derive

// topchanged_test.go —— TopChangedMember 真实姓名 Name 装配（② 成员真实姓名数据）白盒单测。
//
// 背景（第4轮）：重点变化表由「员工N(匿名)」改为显示真实姓名。derive 层 buildTopChanged
// 需从 MemberAgg.UserName（花名册/成员聚合带出）回填 TopChangedMember.Name，供渲染层不匿名显示。
// 本测试锁定：Name 正确携带、UserID 仅内部使用（json:"-" 不序列化）、按 |WoWCostPct| 降序、仅两期都有的成员计入。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestBuildTopChanged_CarriesRealName(t *testing.T) {
	cur := []report.MemberAgg{
		{UserID: "u1", UserName: "张三", JobCategory: "后端开发", ReqCount: 100, CostUSD: 100, TotalTokens: 1000},
		{UserID: "u2", UserName: "李四", JobCategory: "算法", ReqCount: 50, CostUSD: 10, TotalTokens: 500},
		{UserID: "u3", UserName: "王五", JobCategory: "测试", ReqCount: 10, CostUSD: 1, TotalTokens: 100}, // 仅本期，无上期 → 不计入
	}
	prev := []report.MemberAgg{
		{UserID: "u1", JobCategory: "后端开发", ReqCount: 40, CostUSD: 50, TotalTokens: 400},
		{UserID: "u2", JobCategory: "算法", ReqCount: 100, CostUSD: 20, TotalTokens: 1000},
	}

	got := buildTopChanged(cur, prev, nil)

	if len(got) != 2 {
		t.Fatalf("len=%d, want 2（仅两期都有的成员计入）", len(got))
	}

	// 按 |WoWCostPct| 降序：u1 +100% 在前，u2 -50% 在后。
	if got[0].UserID != "u1" || got[0].Name != "张三" {
		t.Errorf("top1 = %+v, want u1/张三", got[0])
	}
	if got[1].UserID != "u2" || got[1].Name != "李四" {
		t.Errorf("top2 = %+v, want u2/李四", got[1])
	}
	if got[0].JobCategory != "后端开发" {
		t.Errorf("top1.JobCategory=%q, want 后端开发", got[0].JobCategory)
	}
	if got[1].JobCategory != "算法" {
		t.Errorf("top2.JobCategory=%q, want 算法", got[1].JobCategory)
	}
}

// 上期成本为 0 时 WoWCostPct 应保持 0（而非 ±Inf/NaN），Name 仍携带。
func TestBuildTopChanged_ZeroPrevCostKeepsName(t *testing.T) {
	cur := []report.MemberAgg{
		{UserID: "u1", UserName: "赵六", JobCategory: "服务端开发", ReqCount: 10, CostUSD: 5, TotalTokens: 100},
	}
	prev := []report.MemberAgg{
		{UserID: "u1", JobCategory: "服务端开发", ReqCount: 5, CostUSD: 0, TotalTokens: 50},
	}

	got := buildTopChanged(cur, prev, nil)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].Name != "赵六" {
		t.Errorf("Name=%q, want 赵六", got[0].Name)
	}
	if got[0].WoWCostPct != 0 {
		t.Errorf("WoWCostPct=%v, want 0（上期成本为 0）", got[0].WoWCostPct)
	}
}

// 序列化契约：TopChangedMember.UserID 为 json:"-"（不输出 UUID，维持内部用途），Name 序列化为 name。
func TestTopChangedMemberJSON_KeepsAnonUUIDButHasName(t *testing.T) {
	tc := report.TopChangedMember{Idx: 1, Name: "王五", JobCategory: "测试", UserID: "secret-uuid"}
	j, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(j)
	if !strings.Contains(s, `"name":"王五"`) {
		t.Errorf("marshal 缺 name 字段: %s", s)
	}
	if strings.Contains(s, "secret-uuid") || strings.Contains(s, "user_id") || strings.Contains(s, "UserID") {
		t.Errorf("marshal 不应泄漏 user_id/UUID: %s", s)
	}
}
