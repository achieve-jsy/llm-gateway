package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
)

// ModelTierLookupService 提供「模型档位/价格」只读查询，供杀鸡用牛刀(3.5)判定用：
//   - 判断某模型是否走了高阶档位(GetModelTier)；
//   - 计算「同能力组最低档模型 + 其单价」(GetLightestAlternative)，用于溢价重算。
//
// ⚠️ 真实 schema 校正（对齐 docs/token-usage-over-model-analysis-design.md §2，但按本项目 schema 落地）：
//   - model_tiers 表并没有 high/mid/low 等级字段（只有 Name/Color/SortOrder/Enabled/ArchivedAt），
//     因此「档位」从 group_models.complexity_tier (SIMPLE/MEDIUM/COMPLEX/REASONING) 近似：
//     COMPLEX/REASONING → high；SIMPLE → low；MEDIUM → mid；缺失 → unknown。
//   - 同一能力组 = 同一个 group_id 下的所有 group_models 成员；每个成员经 models 关联可读价格。
//   - 价格口径统一复用项目语义：price_mode=manual → model 自身价；否则 → 关联 model_catalog 价。
//
// 本服务只读，不写任何表；与 ModelTierService(controlplane) 的职责区分：那个是管理面 CRUD+预算绑定，
// 这里是观测侧只读查询，供判定引擎复用。
type ModelTierLookupService struct {
	db *gorm.DB
}

// NewModelTierLookupService 创建 ModelTierLookupService。
func NewModelTierLookupService(db *gorm.DB) *ModelTierLookupService {
	return &ModelTierLookupService{db: db}
}

// TierLevel 归一化的模型档位等级。
type TierLevel string

const (
	TierHigh    TierLevel = "high"
	TierMid     TierLevel = "mid"
	TierLow     TierLevel = "low"
	TierUnknown TierLevel = "unknown"
)

// ModelTierInfo 单个模型的档位 + 有效单价 + 归属能力组。
type ModelTierInfo struct {
	ModelName    string
	ProviderName string
	GroupName    string
	GroupID      uint
	Complexity   string // group_models.complexity_tier 原值
	Tier         TierLevel
	InputPrice   float64 // USD / 1M tokens
	OutputPrice  float64 // USD / 1M tokens
	CacheReadP   *float64
	CacheWriteP  *float64
}

// resolveTier 把 group_models.complexity_tier 映射为归一档位。
func resolveTier(complexity string) TierLevel {
	switch strings.ToUpper(strings.TrimSpace(complexity)) {
	case "COMPLEX", "REASONING":
		return TierHigh
	case "SIMPLE":
		return TierLow
	case "MEDIUM":
		return TierMid
	default:
		return TierUnknown
	}
}

// resolveInputPrice / resolveOutputPrice 按项目统一价格口径解析：
// price_mode=manual → 取 model 自身价；否则 → 取关联 catalog 价，未绑定为 0。
func resolveInputPrice(m *cpmodel.Model) float64 {
	if m == nil {
		return 0
	}
	if m.PriceMode == "manual" {
		return m.InputPrice
	}
	if m.Catalog != nil {
		return m.Catalog.InputPricePerMillion
	}
	return 0
}

func resolveOutputPrice(m *cpmodel.Model) float64 {
	if m == nil {
		return 0
	}
	if m.PriceMode == "manual" {
		return m.OutputPrice
	}
	if m.Catalog != nil {
		return m.Catalog.OutputPricePerMillion
	}
	return 0
}

func resolveCacheReadPrice(m *cpmodel.Model) *float64 {
	if m == nil {
		return nil
	}
	if m.PriceMode == "manual" {
		return m.CacheReadPrice
	}
	if m.Catalog != nil {
		return m.Catalog.CacheReadPrice
	}
	return nil
}

func resolveCacheWritePrice(m *cpmodel.Model) *float64 {
	if m == nil {
		return nil
	}
	if m.PriceMode == "manual" {
		return m.CacheWritePrice
	}
	if m.Catalog != nil {
		return m.Catalog.CacheWritePrice
	}
	return nil
}

// GetModelTier 查询某个模型名（含 "{provider}/" 前缀或裸名）的档位等级。
// 返回 TierInfo；模型不存在 / 未关联任何能力组时返回零值 TierInfo + nil error，
// 由调用方判定 Tier == TierUnknown（视为「未知档位」，不应回退为 high）。
func (s *ModelTierLookupService) GetModelTier(ctx context.Context, modelName string) (ModelTierInfo, error) {
	if s == nil || s.db == nil {
		return ModelTierInfo{}, nil
	}
	name := strings.TrimSpace(modelName)
	if name == "" {
		return ModelTierInfo{}, nil
	}

	var rows []ModelTierInfo
	err := s.db.WithContext(ctx).Raw(`
		SELECT
			m.name                                   AS model_name,
			p.name                                   AS provider_name,
			g.name                                   AS group_name,
			gm.group_id                              AS group_id,
			gm.complexity_tier                       AS complexity,
			COALESCE(CASE WHEN m.price_mode = 'manual'
			              THEN m.input_price
			              ELSE mc.input_price_per_million END, 0)   AS input_price,
			COALESCE(CASE WHEN m.price_mode = 'manual'
			              THEN m.output_price
			              ELSE mc.output_price_per_million END, 0)  AS output_price,
			(CASE WHEN m.price_mode = 'manual' THEN m.cache_read_price
			      ELSE mc.cache_read_price END)                     AS cache_read_p,
			(CASE WHEN m.price_mode = 'manual' THEN m.cache_write_price
			      ELSE mc.cache_write_price END)                    AS cache_write_p
		FROM models m
		JOIN providers p          ON p.id = m.provider_id
		JOIN group_models gm      ON gm.model_id = m.id
		JOIN groups g             ON g.id = gm.group_id
		LEFT JOIN model_catalog mc ON mc.id = m.catalog_id
		WHERE m.name = ?
		  AND m.deleted_at IS NULL
		  AND g.deleted_at IS NULL
		ORDER BY gm.group_id
	`, name).Scan(&rows).Error
	if err != nil {
		return ModelTierInfo{}, err
	}
	if len(rows) == 0 {
		return ModelTierInfo{ModelName: name, Tier: TierUnknown}, nil
	}
	// 一个模型可属多个组；取档位最高的一组作为代表（保守：不低估高阶）。
	best := rows[0]
	for _, r := range rows[1:] {
		if tierRank(r.Tier) > tierRank(best.Tier) {
			best = r
		}
	}
	return best, nil
}

// tierRank 用于在多项中取「档位最高」代表：unknown 视为最低，避免把未知当 high。
func tierRank(t TierLevel) int {
	switch t {
	case TierHigh:
		return 3
	case TierMid:
		return 2
	case TierLow:
		return 1
	default:
		return 0
	}
}

// LightestAlternative 同能力组内「最低档位模型」及其有效单价，用于重算溢价。
type LightestAlternative struct {
	ModelName    string
	ProviderName string
	GroupName    string
	GroupID      uint
	Tier         TierLevel
	InputPrice   float64
	OutputPrice  float64
}

// GetLightestAlternative 返回指定能力组内最低档位(Tier 最低)的模型及单价。
//   - 能力组内找不到更低档位的模型（组内只有当前档位）→ 返回 nil + nil（不可算溢价）。
//   - 组内全部为 unknown 档位 → 返回 nil + nil（无法判定哪个更低）。
//   - 组不存在时返回 nil + nil。
//
// excludeModelName 可传当前已用模型名以排除自身（如组内只有自己时排除后无更低档 → nil）。
func (s *ModelTierLookupService) GetLightestAlternative(ctx context.Context, groupID uint, excludeModelName string) (*LightestAlternative, error) {
	if s == nil || s.db == nil || groupID == 0 {
		return nil, nil
	}

	var rows []ModelTierInfo
	err := s.db.WithContext(ctx).Raw(`
		SELECT
			m.name                                   AS model_name,
			p.name                                   AS provider_name,
			g.name                                   AS group_name,
			gm.group_id                              AS group_id,
			gm.complexity_tier                       AS complexity,
			COALESCE(CASE WHEN m.price_mode = 'manual'
			              THEN m.input_price
			              ELSE mc.input_price_per_million END, 0)   AS input_price,
			COALESCE(CASE WHEN m.price_mode = 'manual'
			              THEN m.output_price
			              ELSE mc.output_price_per_million END, 0)  AS output_price,
			(CASE WHEN m.price_mode = 'manual' THEN m.cache_read_price
			      ELSE mc.cache_read_price END)                     AS cache_read_p,
			(CASE WHEN m.price_mode = 'manual' THEN m.cache_write_price
			      ELSE mc.cache_write_price END)                    AS cache_write_p
		FROM group_models gm
		JOIN models m             ON m.id = gm.model_id
		JOIN providers p          ON p.id = m.provider_id
		JOIN groups g             ON g.id = gm.group_id
		LEFT JOIN model_catalog mc ON mc.id = m.catalog_id
		WHERE gm.group_id = ?
		  AND m.deleted_at IS NULL
		  AND g.deleted_at IS NULL
	`, groupID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// 排除自身后，取 Tier 最低者；若最低者 tier 为 unknown，则整体不可判定。
	var best *ModelTierInfo
	for i := range rows {
		r := rows[i]
		if excludeModelName != "" && r.ModelName == excludeModelName {
			continue
		}
		if best == nil || tierRank(r.Tier) < tierRank(best.Tier) {
			best = &r
		}
	}
	if best == nil || best.Tier == TierUnknown {
		return nil, nil
	}
	if tierRank(best.Tier) <= 0 {
		return nil, nil
	}
	return &LightestAlternative{
		ModelName:    best.ModelName,
		ProviderName: best.ProviderName,
		GroupName:    best.GroupName,
		GroupID:      best.GroupID,
		Tier:         best.Tier,
		InputPrice:   best.InputPrice,
		OutputPrice:  best.OutputPrice,
	}, nil
}

var (
	// ErrModelTierLookup 标注查询异常（不用于「模型不存在」的预期返回）。
	ErrModelTierLookup = errors.New("model tier lookup failed")
)
