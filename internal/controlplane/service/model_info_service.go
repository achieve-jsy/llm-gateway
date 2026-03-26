package service

import (
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

// GroupModelInfo 聚合后的「模型组」能力信息，按 LiteLLM model_info 的字段语义命名，
// 供 gateway-api 的 /v1/model/info 端点转译为 LiteLLM 格式后供 opencode 插件富化。
//
// 网关对外暴露的「模型」是模型组（group），一个组可能包含多个后端模型。这里把成员
// 模型的能力聚合到组级：
//   - 能力位（vision / function_calling / tool_choice）：任一成员支持即视为支持，
//     以便组内的多模态模型能力被 opencode 感知（这正是用户的诉求）；
//   - 上下文/输出上限：取成员中的最小正值，避免超过最弱成员的实际能力；
//   - mode：任一成员为 chat 即为 chat，否则取首个非空 mode。
type GroupModelInfo struct {
	Mode                    string  `json:"mode"`
	MaxInputTokens          int     `json:"max_input_tokens"`
	MaxOutputTokens         int     `json:"max_output_tokens"`
	SupportsVision          bool    `json:"supports_vision"`
	SupportsFunctionCalling bool    `json:"supports_function_calling"`
	SupportsToolChoice      bool    `json:"supports_tool_choice"`
	// 单价：USD per 1,000,000 tokens。组内成员价格不一致时取最大值（保守上界），
	// 单模型组即为精确值；0 表示缺价（成员未绑定 catalog 或未配置 manual 价）。
	InputPricePerMillion  float64 `json:"input_price_per_million"`
	OutputPricePerMillion float64 `json:"output_price_per_million"`
	IsCapabilityOverride  bool    `json:"is_capability_override"`

	// ── 视频模型专用字段（seedance） ──────────────────────────────────────
	// 当 mode == "video" 时由 seedance 适配器填充；非视频模型保持零值。
	VideoSupportedActions    []string `json:"video_supported_actions,omitempty"`    // 文生视频 / 图生视频 / 多模态参考
	VideoSupportedResolutions []string `json:"video_supported_resolutions,omitempty"` // 720p / 1080p
	VideoMaxDurationSeconds   int      `json:"video_max_duration_seconds,omitempty"`
	VideoAPIEndpoint          string   `json:"video_api_endpoint,omitempty"` // POST /v1/contents/generations/tasks
}

// GroupInfoRepo 抽象按需查询启用模型组（含成员）的能力。
// 实现者：repository.GroupRepo
type GroupInfoRepo interface {
	ListEnabledGroupsWithModels() ([]model.Group, error)
}

// ModelInfoRepo 抽象按 ID 批量查询模型并预加载 Catalog。
// 实现者：repository.ModelRepo
type ModelInfoRepo interface {
	GetByIDsWithCatalog(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error)
}

// ModelInfoService 计算模型组的聚合能力信息。
type ModelInfoService struct {
	groupRepo GroupInfoRepo
	modelRepo ModelInfoRepo
}

// NewModelInfoService 创建 ModelInfoService 实例。
func NewModelInfoService(groupRepo GroupInfoRepo, modelRepo ModelInfoRepo) *ModelInfoService {
	return &ModelInfoService{groupRepo: groupRepo, modelRepo: modelRepo}
}

// GetGroupModelInfo 返回指定模型组名 → 聚合能力信息。
// names 为空时返回所有启用的模型组。
func (s *ModelInfoService) GetGroupModelInfo(names []string) (map[string]GroupModelInfo, error) {
	wanted := make(map[string]struct{}, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			wanted[n] = struct{}{}
		}
	}

	groups, err := s.groupRepo.ListEnabledGroupsWithModels()
	if err != nil {
		return nil, err
	}

	// 收集需要的组及其成员模型 ID，单次批量查询模型+catalog。
	type groupRef struct {
		name     string
		modelIDs []uuid.UUID
	}
	refs := make([]groupRef, 0, len(groups))
	idSet := make(map[uuid.UUID]struct{})
	groupsByName := make(map[string]*model.Group, len(groups))
	for i := range groups {
		g := &groups[i]
		if g.Name == "" {
			continue
		}
		groupsByName[g.Name] = g
		if len(wanted) > 0 {
			if _, ok := wanted[g.Name]; !ok {
				continue
			}
		}
		ref := groupRef{name: g.Name}
		for _, gm := range g.Models {
			ref.modelIDs = append(ref.modelIDs, gm.ModelID)
			idSet[gm.ModelID] = struct{}{}
		}
		refs = append(refs, ref)
	}

	allIDs := make([]uuid.UUID, 0, len(idSet))
	for id := range idSet {
		allIDs = append(allIDs, id)
	}
	modelsByID, err := s.modelRepo.GetByIDsWithCatalog(allIDs)
	if err != nil {
		return nil, err
	}

	result := make(map[string]GroupModelInfo, len(refs))
	for _, ref := range refs {
		members := make([]*model.Model, 0, len(ref.modelIDs))
		for _, id := range ref.modelIDs {
			if m, ok := modelsByID[id]; ok && m != nil && (m.Enabled == nil || *m.Enabled) {
				members = append(members, m)
			}
		}
		info := aggregateGroupModelInfo(members)
		if g, ok := groupsByName[ref.name]; ok && g.IsCapabilityOverride {
			info = applyGroupOverride(info, g)
		}
		result[ref.name] = info
	}
	return result, nil
}

// aggregateGroupModelInfo 把组内成员模型的能力聚合为组级能力。
//
// 聚合语义（保守 / "all"）：
//   - 能力位（vision / function_calling / tool_choice）：**全部成员都支持**才算支持。
//     只要有一个成员不支持（或未绑定 catalog 无法确认），组级就为 false，避免对外
//     宣称能力却被路由到不支持的成员而失败。
//   - 上下文/输出上限：取成员中的最小正值，不超过最弱成员的实际能力。
//   - mode：任一成员为 chat 即为 chat，否则取首个非空 mode。
func aggregateGroupModelInfo(members []*model.Model) GroupModelInfo {
	info := GroupModelInfo{}
	if len(members) == 0 {
		return info
	}

	// 能力位用 "全部支持才算支持"：先假定支持，遇到任一不支持即置 false。
	vision, functionCalling, toolChoice := true, true, true
	firstMode := ""
	for _, m := range members {
		mode := memberMode(m)
		if firstMode == "" && mode != "" {
			firstMode = mode
		}
		if mode == "chat" {
			info.Mode = "chat"
		}

		if ctx := memberContext(m); ctx > 0 {
			if info.MaxInputTokens == 0 || ctx < info.MaxInputTokens {
				info.MaxInputTokens = ctx
			}
		}
		if out := memberOutput(m); out > 0 {
			if info.MaxOutputTokens == 0 || out < info.MaxOutputTokens {
				info.MaxOutputTokens = out
			}
		}

		// 单价取组内最大值，作为对外展示的保守上界。
		if in := memberInputPrice(m); in > info.InputPricePerMillion {
			info.InputPricePerMillion = in
		}
		if out := memberOutputPrice(m); out > info.OutputPricePerMillion {
			info.OutputPricePerMillion = out
		}

		// 未绑定 catalog 的成员视为「能力未知 = 不支持」，从而拉低组级能力。
		var memberVision, memberFC, memberTC bool
		if m.Catalog != nil {
			memberVision = m.Catalog.SupportsVision
			memberFC = m.Catalog.SupportsFunctionCalling
			memberTC = m.Catalog.SupportsToolChoice
		}
		vision = vision && memberVision
		functionCalling = functionCalling && memberFC
		toolChoice = toolChoice && memberTC
	}

	info.SupportsVision = vision
	info.SupportsFunctionCalling = functionCalling
	info.SupportsToolChoice = toolChoice
	if info.Mode == "" {
		info.Mode = firstMode
	}
	return info
}

func memberMode(m *model.Model) string {
	if m.Catalog != nil && strings.TrimSpace(m.Catalog.Mode) != "" {
		return strings.TrimSpace(m.Catalog.Mode)
	}
	return strings.TrimSpace(m.ModelType)
}

func memberContext(m *model.Model) int {
	if m.ContextLength > 0 {
		return m.ContextLength
	}
	if m.Catalog != nil && m.Catalog.ContextLength > 0 {
		return int(m.Catalog.ContextLength)
	}
	return 0
}

func memberOutput(m *model.Model) int {
	if m.MaxOutputTokens > 0 {
		return m.MaxOutputTokens
	}
	if m.Catalog != nil && m.Catalog.MaxOutputTokens > 0 {
		return int(m.Catalog.MaxOutputTokens)
	}
	return 0
}

// memberInputPrice / memberOutputPrice 解析单个成员模型的有效单价：
//   - price_mode=manual：取模型自身的 InputPrice/OutputPrice；
//   - 其他（catalog/默认）：取关联 catalog 的 *PricePerMillion，未绑定则为 0。
func memberInputPrice(m *model.Model) float64 {
	if m.PriceMode == "manual" {
		return m.InputPrice
	}
	if m.Catalog != nil {
		return m.Catalog.InputPricePerMillion
	}
	return 0
}

func memberOutputPrice(m *model.Model) float64 {
	if m.PriceMode == "manual" {
		return m.OutputPrice
	}
	if m.Catalog != nil {
		return m.Catalog.OutputPricePerMillion
	}
	return 0
}

func applyGroupOverride(info GroupModelInfo, g *model.Group) GroupModelInfo {
	info.SupportsVision = g.OverrideSupportsVision
	info.SupportsFunctionCalling = g.OverrideSupportsFC
	info.SupportsToolChoice = g.OverrideSupportsTC
	info.IsCapabilityOverride = true
	return info
}
