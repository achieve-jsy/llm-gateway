package budget

import (
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
)

// ExceededMessage 构造 402 响应中面向用户的中文提示。
func ExceededMessage(c Constraint) string {
	period := periodLabel(c.PeriodType)
	if c.Dimension == DimensionTier {
		subject := "该模型等级"
		if name := strings.TrimSpace(c.TierName); name != "" {
			subject = name
		}
		if c.StaticZero {
			return subject + period + "预算为 0，无法调用，请联系负责人开通"
		}
		return subject + period + "预算已用尽，请联系负责人扩容"
	}
	if c.StaticZero {
		return "总预算" + period + "限额为 0，无法调用，请联系负责人开通"
	}
	return "总预算" + period + "限额已用尽，请联系负责人扩容"
}

func periodLabel(periodType budgetdomain.PeriodType) string {
	switch periodType {
	case budgetdomain.PeriodDaily:
		return "日"
	case budgetdomain.PeriodWeekly:
		return "周"
	case budgetdomain.PeriodMonthly:
		return "月"
	default:
		return "周期"
	}
}
