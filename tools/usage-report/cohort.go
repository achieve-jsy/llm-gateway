package main

// cohort.go —— 同岗分位对标池：/org/* 花名册(job_category/studio) + 每用户效能值。
// 唯一规范见 docs/org-roster.md（backend-eng，t5）：枚举 active 部门 → 逐部门拉成员 → 按 user_id 去重
// → studio 解析(departments.studio_id → dept_studio_map → "") → job_category 取值链(job_category→job_family→job_title→'')。
// 原则：匿名分位、不指名、不跨岗位裸排名；样本不足逐级回退，仍不足 → NotComputed（无依据不下结论）。
// 安全：admin JWT 仅内存/env，不写 roster.json/report/日志；只取组织元数据，不取请求正文/PII。

import (
	"context"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/tools/usage-report/client"
)

type rosterEntry struct {
	UserID      string
	DeptID      string
	DeptName    string
	StudioID    string
	JobKey      string // 取值链归一后的同岗键：job_category→job_family→job_title→""
	JobCategory string
	JobFamily   string
	JobTitle    string
	HasConflict bool
	LoginStatus string
}

// buildCohort 构建同岗分位对标池（全量 rosters 之最小必要子集，仅供同岗分位）。
func buildCohort(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, calls []report.CallEvent) (report.CohortDef, []report.NotComputed) {
	var ncs []report.NotComputed
	cohort := report.CohortDef{Enabled: true, Option: "sameRole", Metric: "cost_per_1k_tokens", Direction: "cost"}

	// 每用户效能（cost_per_1k_tokens）与总 token
	perUser := map[string]float64{}
	validUser := map[string]bool{}
	agg := map[string]*struct{ cost, tokens float64 }{}
	for _, c := range calls {
		if c.KeyType == "service" {
			continue // 同岗分位排除 service
		}
		u := c.UserID
		if u == "" {
			continue
		}
		a := agg[u]
		if a == nil {
			a = &struct{ cost, tokens float64 }{}
			agg[u] = a
		}
		a.cost += c.EstCostUSD
		a.tokens += float64(c.PromptTokens + c.CompletionTokens + c.ReasoningTokens)
	}
	for u, a := range agg {
		if a.tokens > 0 {
			perUser[u] = a.cost / a.tokens * 1000
			validUser[u] = true
		}
	}

	// 花名册：active 部门 + dept-studio-map + 逐部门成员
	depts, err := cli.FetchDepartments(ctx, "active")
	if err != nil {
		ncs = append(ncs, report.NotComputed{Metric: "同岗分位", Reason: "拉取部门失败：" + err.Error()})
		cohort.Enabled = false
		return cohort, ncs
	}
	sm, _ := cli.FetchDeptStudioMap(ctx)
	studioOfDept := map[string]string{}
	for _, m := range sm {
		studioOfDept[m.DeptID] = m.StudioID
	}
	roster := map[string]rosterEntry{}
	skipped := []string{}
	for _, d := range depts {
		// studio 解析：① departments.studio_id(非空) → ② dept_studio_map → ③ ""
		st := d.StudioID
		if st == "" {
			st = studioOfDept[d.DeptID]
		}
		r, err := cli.FetchMembers(ctx, d.DeptID)
		if err != nil {
			skipped = append(skipped, d.DeptID)
			continue // 403 越权 / 404 / 429 限流 → 记录跳过，不把整次运行当失败
		}
		for _, mb := range r {
			// 去重：以 user_id 为键；理论每用户单部门，保留。
			roster[mb.ID] = rosterEntry{
				UserID: mb.ID, DeptID: d.DeptID, DeptName: d.DeptName, StudioID: st,
				JobKey:      jobSlot(mb.JobCategory, mb.JobFamily, mb.JobTitle),
				JobCategory: mb.JobCategory, JobFamily: mb.JobFamily, JobTitle: mb.JobTitle,
				HasConflict: mb.HasConflict, LoginStatus: mb.LoginStatus,
			}
		}
	}

	target := roster[cfg.Scope]
	if target.UserID == "" {
		ncs = append(ncs, report.NotComputed{Metric: "同岗分位", Reason: "目标用户在 active 部门花名册中未匹配到岗位（可能 users.dept_id IS NULL 或非 active 部门）。"})
		cohort.Enabled = false
		return cohort, ncs
	}

	members := poolFor(target, roster, validUser, perUser)
	if len(members) == 0 {
		ncs = append(ncs, report.NotComputed{Metric: "同岗分位", Reason: "同岗位(同 category/studio)可比样本为 0，无法产出匿名分位。"})
		cohort.Enabled = false
		return cohort, ncs
	}
	cohort.PoolType = "studio+job_category"
	cohort.Members = members
	if len(skipped) > 0 {
		cohort.Enabled = true
		ncs = append(ncs, report.NotComputed{Metric: "同岗分位-覆盖", Reason: "有 " + itoa(len(skipped)) + " 个部门成员拉取失败(越权/限流)，roster 不完整；peer 分位仅基于可取到样本。"})
	}
	return cohort, ncs
}

// jobSlot 取值链：job_category → job_family → job_title → ""（org-roster.md §3-C）。
func jobSlot(c, f, t string) string {
	if c != "" {
		return c
	}
	if f != "" {
		return f
	}
	return t
}

// poolFor 依「同岗位 + 同 studio」优先，样本不足时逐级回退（org-roster §4.2）：
// 同studio+同category → 全公司同category → 同studio(任意) → 全公司(任意)；
// 仍 <MIN_SAMPLE(5) 则交由 derive 判定 supported=false。
func poolFor(t rosterEntry, roster map[string]rosterEntry, validUser map[string]bool, perUser map[string]float64) []report.CohortMember {
	got := collectSame(t, roster, validUser, perUser, func(e rosterEntry) bool {
		return e.StudioID == t.StudioID && e.JobKey == t.JobKey && e.UserID != t.UserID
	})
	if len(got) >= 5 {
		return got
	}
	// 回退：公司同岗位
	got = collectSame(t, roster, validUser, perUser, func(e rosterEntry) bool {
		return e.JobKey == t.JobKey && e.UserID != t.UserID
	})
	if len(got) >= 5 {
		return got
	}
	// 回退：同 studio（任意岗位）
	got = collectSame(t, roster, validUser, perUser, func(e rosterEntry) bool {
		return e.StudioID == t.StudioID && e.UserID != t.UserID
	})
	if len(got) >= 5 {
		return got
	}
	// 回退：全公司（任意岗位）
	return collectSame(t, roster, validUser, perUser, func(e rosterEntry) bool {
		return e.UserID != t.UserID
	})
}

func collectSame(t rosterEntry, roster map[string]rosterEntry, validUser map[string]bool, perUser map[string]float64, match func(rosterEntry) bool) []report.CohortMember {
	var out []report.CohortMember
	for _, e := range roster {
		if !match(e) {
			continue
		}
		if !validUser[e.UserID] {
			continue
		}
		out = append(out, report.CohortMember{UserID: e.UserID, StudioID: e.StudioID, DeptID: e.DeptID, JobCategory: e.JobKey, JobFamily: e.JobFamily, JobTitle: e.JobTitle, MetricValue: perUser[e.UserID], MetricValid: true})
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
