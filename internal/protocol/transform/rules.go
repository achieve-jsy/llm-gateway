package transform

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Action 表示单条参数转换规则的动作。
type Action string

const (
	ActionDrop                     Action = "drop"
	ActionRename                   Action = "rename"
	ActionForceLiteral             Action = "force_literal"
	ActionNormalizeReasoningEffort Action = "normalize_reasoning_effort"
	ActionDropReasoningSummary     Action = "drop_reasoning_summary"
)

// ParamTransformRule 描述对 JSON 顶层参数的一条有限转换规则。
// 第一阶段刻意保持为可审计的白名单动作，而不是开放脚本 DSL。
type ParamTransformRule struct {
	Action Action
	From   string
	To     string
	Value  any
}

// Drop 删除一个顶层字段。
func Drop(field string) ParamTransformRule {
	return ParamTransformRule{Action: ActionDrop, From: field}
}

// Rename 将顶层字段重命名；当目标字段已存在时默认保留目标字段并删除来源字段。
func Rename(from, to string) ParamTransformRule {
	return ParamTransformRule{Action: ActionRename, From: from, To: to}
}

// ForceLiteral 强制写入一个可 JSON 序列化的字面量。
func ForceLiteral(field string, value any) ParamTransformRule {
	return ParamTransformRule{Action: ActionForceLiteral, To: field, Value: value}
}

// NormalizeReasoningEffort 归一化 reasoning effort 别名：
// reasoningEffort / reasoning:{effort} / reasoning_effort:{effort} -> reasoning_effort。
func NormalizeReasoningEffort() ParamTransformRule {
	return ParamTransformRule{Action: ActionNormalizeReasoningEffort}
}

// DropReasoningSummary 删除 Chat Completions 不支持的 summary 别名。
func DropReasoningSummary() ParamTransformRule {
	return ParamTransformRule{Action: ActionDropReasoningSummary}
}

// ApplyRules 将 body 编码为 JSON object 后顺序应用转换规则，返回新的 raw map。
func ApplyRules(body any, rules ...ParamTransformRule) (map[string]json.RawMessage, error) {
	raw, err := BodyToRawMap(body)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if err := applyRule(raw, rule); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// BodyToRawMap 将任意请求体转成可修改的 JSON object map。
func BodyToRawMap(body any) (map[string]json.RawMessage, error) {
	if body == nil {
		return map[string]json.RawMessage{}, nil
	}

	switch b := body.(type) {
	case map[string]json.RawMessage:
		return CloneRawMap(b), nil
	case map[string]any:
		raw := make(map[string]json.RawMessage, len(b))
		for key, value := range b {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("marshal field %q: %w", key, err)
			}
			raw[key] = cloneRaw(encoded)
		}
		return raw, nil
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, fmt.Errorf("decode body as JSON object: %w", err)
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	return raw, nil
}

// CloneRawMap 深拷贝 raw map，避免修改调用方持有的数据。
func CloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for key, value := range in {
		out[key] = cloneRaw(value)
	}
	return out
}

func applyRule(raw map[string]json.RawMessage, rule ParamTransformRule) error {
	switch rule.Action {
	case ActionDrop:
		delete(raw, rule.From)
		return nil
	case ActionRename:
		if rule.From == "" || rule.To == "" {
			return fmt.Errorf("rename rule requires from and to")
		}
		if value, ok := raw[rule.From]; ok {
			if _, exists := raw[rule.To]; !exists {
				raw[rule.To] = cloneRaw(value)
			}
			delete(raw, rule.From)
		}
		return nil
	case ActionForceLiteral:
		if rule.To == "" {
			return fmt.Errorf("force literal rule requires target field")
		}
		encoded, err := json.Marshal(rule.Value)
		if err != nil {
			return fmt.Errorf("marshal literal for %q: %w", rule.To, err)
		}
		raw[rule.To] = cloneRaw(encoded)
		return nil
	case ActionNormalizeReasoningEffort:
		NormalizeReasoningEffortRaw(raw)
		return nil
	case ActionDropReasoningSummary:
		DropReasoningSummaryRaw(raw)
		return nil
	default:
		return fmt.Errorf("unknown transform action %q", rule.Action)
	}
}

// DropReasoningSummaryRaw 删除 OpenCode / SDK 常见的 reasoning summary 别名。
func DropReasoningSummaryRaw(raw map[string]json.RawMessage) {
	delete(raw, "reasoningSummary")
	delete(raw, "reasoning_summary")
}

// NormalizeReasoningEffortRaw 将 reasoning effort 的常见别名归一到 Chat Completions 合法字段。
func NormalizeReasoningEffortRaw(raw map[string]json.RawMessage) {
	if raw == nil {
		return
	}
	if _, has := raw["reasoning_effort"]; !has {
		if effort, ok := raw["reasoningEffort"]; ok {
			raw["reasoning_effort"] = cloneRaw(effort)
		} else if nested, ok := raw["reasoning"]; ok {
			if effort := nestedEffort(nested); effort != nil {
				raw["reasoning_effort"] = effort
			}
		}
	}
	delete(raw, "reasoningEffort")
	delete(raw, "reasoning")

	if effortRaw, ok := raw["reasoning_effort"]; ok {
		if normalized := NormalizeReasoningEffortJSON(effortRaw); normalized != nil {
			raw["reasoning_effort"] = normalized
		}
	}
}

// NormalizeReasoningEffortJSON 将 {"effort":"high"} 形态压缩为 "high"。
func NormalizeReasoningEffortJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		return cloneRaw(raw)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return cloneRaw(raw)
	}
	if effort, ok := obj["effort"]; ok {
		return cloneRaw(effort)
	}
	return nil
}

func nestedEffort(raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	if effort, ok := obj["effort"]; ok {
		return cloneRaw(effort)
	}
	return nil
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
