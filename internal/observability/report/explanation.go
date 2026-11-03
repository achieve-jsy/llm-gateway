package report

import (
	"fmt"
	"strings"
)

// IssueNarrative 是面向员工和负责人的可读解释。Evidence 保留机器复核数据，
// Narrative 只回答“发现什么、为什么、影响什么、建议做什么”。
type IssueNarrative struct {
	Finding        string `json:"finding"`
	Reason         string `json:"reason"`
	Impact         string `json:"impact"`
	Advice         string `json:"advice"`
	SemanticReview string `json:"semantic_review,omitempty"`
}

// SafetyNarrative 把严重异常的时间规则和内容复核结果翻译成管理者可读说明。
// 它只解释为何进入待核查，不把时间异常直接认定为非工作使用。
type SafetyNarrative struct {
	Trigger        string `json:"trigger"`
	ContentReview  string `json:"content_review"`
	Interpretation string `json:"interpretation"`
	Action         string `json:"action"`
}

// BuildSafetyNarrative 生成 3.6 严重异常的稳定说明。
func BuildSafetyNarrative(s SafetyAnomaly) SafetyNarrative {
	timeDetail := ""
	if len(s.TopHours) > 0 {
		timeDetail = "，主要集中在 " + strings.Join(s.TopHours, "、")
	}
	trigger := "该记录已进入待核查，但当前快照缺少完整的时段费用比例，请回到原始调用记录确认触发依据。"
	if s.UnrelatedCostRatio > 0.20 {
		trigger = fmt.Sprintf("统计周期内，未匹配备案的非工作时段调用费用占个人总成本 %.1f%%，超过 20%% 关注线%s。", s.UnrelatedCostRatio*100, timeDetail)
	} else if s.UnrelatedCostRatio > 0 {
		trigger = fmt.Sprintf("统计周期内，未匹配备案的非工作时段调用费用占个人总成本 %.1f%%%s；该记录已进入待核查。", s.UnrelatedCostRatio*100, timeDetail)
	}
	n := SafetyNarrative{
		Trigger: trigger,
		Action:  "请核实备案情况，确认对应时段是否属于加班、发布值班、跨时区或批处理；如属正常工作，请补充或关联备案。",
	}

	signal := strings.ToLower(strings.TrimSpace(s.ContentSignal))
	confidence := ""
	if s.ContentConfidence > 0 {
		confidence = fmt.Sprintf("（把握度 %.0f%%）", s.ContentConfidence*100)
	}
	switch {
	case signal == "work_related" || signal == "work" || s.ReviewTag == "疑似加班未备案":
		n.ContentReview = "OpenCode 抽查相关请求内容后，判断这些调用更像工作任务" + confidence + "。"
		n.Interpretation = "因此更可能是正常加班、值班或备案缺失，不代表与工作无关。"
	case signal == "not_work_related" || signal == "personal" || signal == "private":
		n.ContentReview = "OpenCode 抽查相关请求内容后，判断这些调用更像私人事务" + confidence + "。"
		n.Interpretation = "建议优先人工核查，但模型结果只提供线索，不能直接定责。"
	case signal == "uncertain":
		n.ContentReview = "OpenCode 已抽查相关请求内容，但现有信息不足以判断用途" + confidence + "。"
		n.Interpretation = "目前只能确认调用发生在未备案的非工作时段，不能据此判断是否与工作无关。"
	default:
		n.ContentReview = "本次内容复核未形成可靠结论，目前只有调用时段和费用占比信号。"
		n.Interpretation = "仅凭凌晨或周末使用不能判断用途，也不能直接定责。"
	}
	return n
}

// BuildIssueNarrative 把稳定的规则结果翻译为人类可读说明，不暴露内部字段名或会话 ID。
func BuildIssueNarrative(issue UsageIssue) IssueNarrative {
	hitRequests := issue.RequestCount
	hitSessions := issue.SessionCount
	if n := int(evidenceNumber(issue.Evidence, "hit_requests")); n > hitRequests {
		hitRequests = n
	}
	if n := int(evidenceNumber(issue.Evidence, "hit_sessions")); n > hitSessions {
		hitSessions = n
	}

	n := IssueNarrative{}
	switch issue.Type {
	case IssueTypePrefixPollution:
		n.Finding = fmt.Sprintf("发现 %d 个请求处于连续超长、缓存命中偏低的上下文段。", hitRequests)
		n.Reason = "这些请求反复传输了大量没有被缓存复用的历史内容。"
		n.Impact = "额外成本主要落在非缓存输入和缓存处理上。"
		n.Advice = "固定并复用稳定的上下文前缀，减少每轮重复拼接或改写。"
	case IssueTypeToothpaste:
		n.Finding = fmt.Sprintf("系统初步发现 %d 个长会话中存在较多短输入轮次。", hitSessions)
		n.Reason = "这可能表示需求被拆成多次补充，但当前数据还不足以确认这些轮次是否始终属于同一件事。"
		n.Impact = "如果判断成立，额外轮次会反复携带历史上下文，增加输入和缓存成本。"
		n.Advice = "首次提问时尽量一次说明目标、约束和期望结果，减少连续补充。"
	case IssueTypeMultiTaskSession:
		n.Finding = fmt.Sprintf("系统初步发现 %d 个长会话可能在中途切换了任务。", hitSessions)
		n.Reason = "这些会话的上下文持续增长且缓存仍有命中，输入量又出现明显跳升，因此像是新任务继续沿用了旧历史。"
		n.Impact = "如果判断成立，新任务会持续为无关历史付出输入和缓存费用。"
		n.Advice = "开始独立任务时新建会话；同一任务的连续工具操作可以继续复用原会话。"
	case IssueTypeOutputRunaway:
		n.Finding = fmt.Sprintf("发现 %d 个请求的输出量明显高于输入规模或同模型常见范围。", hitRequests)
		n.Reason = "这些请求达到长输出量级，并出现高输出输入比或超过同模型常见输出范围的情况。"
		n.Impact = "超出常见范围的输出费用计入本项，正常的一次性长代码或长文档应排除。"
		n.Advice = "在请求中明确输出范围、格式和长度，只要求返回当前需要的内容。"
	case IssueTypeOverModel:
		n.Finding = fmt.Sprintf("发现 %d 个规模较小的请求可能使用了高阶模型。", hitRequests)
		n.Reason = "这些请求的输入和输出都低于同岗位常见水平，并且看起来可以由同能力组的轻量模型完成。"
		n.Impact = "归因费用只计算相对轻量模型的估算溢价，不把整笔调用成本都视为问题。"
		n.Advice = "简单查询和常规改写优先尝试轻量模型，复杂分析再使用高阶模型。"
	default:
		n.Finding = "本期记录到一项需要关注的使用模式。"
		n.Reason = "系统根据可用的调用数据进行了规则判断。"
		n.Impact = "相关费用已按实际可归因部分统计。"
		n.Advice = "结合具体任务复核后再决定是否调整使用方式。"
	}
	n.SemanticReview = semanticNarrative(issue)

	switch issue.Verdict {
	case VerdictNotHit:
		n.Finding = "本期未发现" + issueDisplayNameForNarrative(issue.Type) + "。"
		n.Reason = "现有调用没有达到该项规则的触发条件。"
		n.Impact = "本项不计入问题消耗。"
	case VerdictNotComputed:
		n.Finding = "本期暂时缺少判断" + issueDisplayNameForNarrative(issue.Type) + "所需的数据。"
		n.Reason = notComputedReason(issue)
		n.Impact = "数据不足不代表使用正常，也不计入问题消耗。"
	case VerdictSuspected:
		if n.SemanticReview == "" {
			n.Reason += " 当前证据不足以确认，结果仅作为观察线索。"
		}
	}

	return n
}

func issueDisplayNameForNarrative(issueType string) string {
	switch issueType {
	case IssueTypePrefixPollution:
		return "前缀污染"
	case IssueTypeToothpaste:
		return "多轮零散补充"
	case IssueTypeMultiTaskSession:
		return "多任务共用会话"
	case IssueTypeOutputRunaway:
		return "输出过长"
	case IssueTypeOverModel:
		return "简单任务使用高阶模型"
	default:
		return "该使用问题"
	}
}

func notComputedReason(issue UsageIssue) string {
	switch issue.Type {
	case IssueTypeToothpaste:
		return "缺少可靠的本轮输入或任务连续性信息。"
	case IssueTypeMultiTaskSession:
		return "缺少可靠的会话重置或任务切换信息。"
	case IssueTypeOutputRunaway:
		return "缺少可用的同模型输出基线或输出结束原因。"
	case IssueTypeOverModel:
		return "缺少岗位基线、模型档位或可比较的轻量模型价格。"
	default:
		return "当前数据覆盖不足。"
	}
}

func semanticNarrative(issue UsageIssue) string {
	raw, ok := issue.Evidence["semantic"].(map[string]any)
	if !ok || raw == nil {
		return ""
	}
	affirmed := int(evidenceNumber(raw, "affirmed"))
	refuted := int(evidenceNumber(raw, "refuted"))
	uncertain := int(evidenceNumber(raw, "uncertain"))

	if affirmed > 0 {
		switch issue.Type {
		case IssueTypeToothpaste:
			return fmt.Sprintf("模型复核确认其中 %d 个候选会话持续围绕同一任务，支持本项判断。", affirmed)
		case IssueTypeMultiTaskSession:
			return fmt.Sprintf("模型复核确认其中 %d 个候选会话发生了任务切换，支持本项判断。", affirmed)
		case IssueTypeOverModel:
			return fmt.Sprintf("模型复核确认其中 %d 个候选请求属于简单任务，支持使用轻量模型的建议。", affirmed)
		default:
			return fmt.Sprintf("模型复核确认其中 %d 个候选成立。", affirmed)
		}
	}
	if refuted > 0 && uncertain == 0 {
		switch issue.Type {
		case IssueTypeToothpaste:
			return fmt.Sprintf("模型复核认为 %d 个候选会话包含不同任务，不支持“同一件事被拆成多轮”的判断；相应费用已排除。", refuted)
		case IssueTypeMultiTaskSession:
			return fmt.Sprintf("模型复核认为 %d 个候选会话仍围绕同一任务，不支持“中途切换任务”的判断；相应费用已排除。", refuted)
		case IssueTypeOverModel:
			return fmt.Sprintf("模型复核认为 %d 个候选请求属于复杂任务，不支持改用轻量模型；相应费用已排除。", refuted)
		default:
			return fmt.Sprintf("模型复核不支持 %d 个候选，相应费用已排除。", refuted)
		}
	}
	if uncertain > 0 && refuted > 0 {
		return fmt.Sprintf("模型复核排除了 %d 个候选，另有 %d 个证据不足；仅保留证据不足部分作为疑似。", refuted, uncertain)
	}
	if uncertain > 0 {
		return fmt.Sprintf("模型复核了 %d 个候选，但现有内容不足以稳定判断，因此没有改变“疑似”状态。", uncertain)
	}
	return ""
}

func evidenceNumber(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch n := m[key].(type) {
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	case float64:
		return n
	default:
		return 0
	}
}

func attachIssueNarratives(issues []UsageIssue) []UsageIssue {
	for i := range issues {
		narrative := BuildIssueNarrative(issues[i])
		issues[i].Narrative = &narrative
	}
	return issues
}
