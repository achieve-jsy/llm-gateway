package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// 本文件承载 A12：catalog（LiteLLM）`mode` → `models.model_type` 的归一。
//
// 背景：`CreateModelFromCatalog` 曾把 `cat.Mode` 原样写入 `models.model_type`，
// 而两者**不是同一套词表**：
//
//	catalog mode 实测分布（internal/controlplane/seed/litellm_catalog.json，2725 条）：
//	  chat 2086 / image_generation 197 / embedding 117 / responses 80 / audio_transcription 57 /
//	  completion 35 / image_edit 31 / rerank 30 / audio_speech 27 / video_generation 25 /
//	  search 14 / ocr 9 / (mode 缺失) 8 / moderation 5 / realtime 2 / vector_store 1 /
//	  1 条把 LiteLLM 的报错文案当成了 mode
//
//	models.model_type 合法枚举（A5 后）：chat image embedding embedding_vl rerank rerank_vl video
//
// 而 `models` 表**没有** CHECK 约束（既有设计，本任务不加约束、不做数据迁移），
// 于是脏值会被静默写库：`ModelGroupSelector.vue` 按 `model_type` 精确等值筛选
// （`model_repo.go` 的 `model_type = ?`）→ 组类型为 image 时筛不出 `image_generation` 的模型；
// 而 `group_service.go` 的 `assertSameModelType` 只比成员之间、不比 `group.ModelType` → 不报错，
// 表现为**静默不可用**。此外白名单外的值会让用户在编辑该模型时被 `binding:"omitempty,oneof=..."` 拒绝。
//
// 归一在这里做（而不是在 `catalog_parser.go`）：`model_catalog.mode` 在别处是**权威语义字段**——
// `pricing/resolver.go` 优先用 `catalog_mode` 判别媒体计价（`image_generation`/`image_edit` → 图像价），
// `provider_snapshot_service.go` 与 `model_info_service.go` 也透传原始 mode。
// 因此**只收敛写入 `models.model_type` 的这一处**，catalog 侧保持原值。

// CatalogModeUnsupportedError 归一失败时返回的哨兵错误。
//
// 语义：该 catalog mode 在当前 model_type 枚举里**没有诚实的落点**，
// 出于「不写脏值 + 不把非对话模型伪装成 chat（伪装会让它出现在 chat 组选择器里，
// 请求最终打到不支持的上游）」的考虑，选择**显式拒绝**而不是保留原值或兜底 chat。
//
// 无对应枚举的 mode（策略：拒绝）：
//
//	audio_transcription / audio_speech / search / ocr / moderation / realtime / vector_store
//	以及任何未知取值（含 LiteLLM 偶发把报错文案写进 mode 的脏数据）
//
// 这类模型目前**不是**本网关可编排的对象：枚举里没有任何组类型能承载它们，
// 硬写白名单外的值只会得到「建出来但选不中、且编辑被拒」的僵尸行。
// 需要登记时请走「手动创建模型」并显式指定 model_type。
var ErrCatalogModeUnsupported = errors.New("catalog mode 无法归一为受支持的 model_type")

// DefaultCatalogModeModelType 是 catalog mode 缺失时的兜底值。
//
// 保留历史行为（`CreateModelFromCatalog` 原本对空 mode 兜底 chat）：
// 种子 catalog 中有 8 条无 mode 的条目，且生产库既有 chat 行不因此变化。
const DefaultCatalogModeModelType = "chat"

// CatalogModeModelTypeMapping 是 catalog mode → model_type 的**唯一**映射表。
//
// 只登记「语义等价、有明确枚举落点」的 mode；未登记的一律拒绝（见 ErrCatalogModeUnsupported）。
// 取值理由：
//   - chat：同名直通。
//   - completion：LiteLLM 的 legacy `/v1/completions` 文本模型，在本网关与 chat 同属对话/文本补全族。
//   - responses：OpenAI Responses API 模型，同样是文本对话族（组选择器只有 chat 这一类）。
//   - embedding / rerank：同名直通（VL 变体 embedding_vl/rerank_vl 无对应 catalog mode，
//     由 admin 建好后手工改型）。
//   - image_generation / image_edit：本网关的图像组类型只有 image（A5 扩的枚举），
//     两者都是图像产出能力，归一到 image 才能被图像组选择器筛到。
//   - video_generation：视频组类型只有 video。
var catalogModeModelTypeMapping = map[string]string{
	"chat":             "chat",
	"completion":       "chat",
	"responses":        "chat",
	"embedding":        "embedding",
	"rerank":           "rerank",
	"image_generation": "image",
	"image_edit":       "image",
	"video_generation": "video",
}

// supportedModelTypes 是 model_type 合法枚举的**运行时**视图。
//
// 契约本体在 6 处 binding 白名单（`oneof=chat image embedding embedding_vl rerank rerank_vl video`）
// 与 `model_groups.model_type` 的 CHECK 迁移里；这里的副本只用于
// 「归一结果必须落在枚举内」这一不变量的运行时防御与错误提示。
// 单测会把它与 binding/迁移的契约列表做交叉断言，防止悄悄漂移。
var supportedModelTypes = map[string]struct{}{
	"chat":         {},
	"image":        {},
	"embedding":    {},
	"embedding_vl": {},
	"rerank":       {},
	"rerank_vl":    {},
	"video":        {},
}

// SupportedModelTypes 返回排序后的 model_type 合法枚举副本（供提示信息与单测交叉校验）。
func SupportedModelTypes() []string {
	out := make([]string, 0, len(supportedModelTypes))
	for name := range supportedModelTypes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// IsSupportedModelType 判断 model_type 是否在合法枚举内。
//
// **严格精确匹配**（不 trim、不转小写）：契约的 binding `oneof=` 与
// `model_groups.model_type` 的 CHECK 都是精确匹配，`" chat"` / `"IMAGE"` 都不合法。
// 归一入口 NormalizeCatalogMode 会先做 trim/lower 再查表，产出的一定是规范值。
func IsSupportedModelType(modelType string) bool {
	_, ok := supportedModelTypes[modelType]
	return ok
}

// CatalogModeModelTypeMapping 返回 catalog mode → model_type 映射表的副本（供单测遍历校验）。
func CatalogModeModelTypeMapping() map[string]string {
	out := make(map[string]string, len(catalogModeModelTypeMapping))
	for mode, modelType := range catalogModeModelTypeMapping {
		out[mode] = modelType
	}
	return out
}

// NormalizeCatalogMode 把 catalog（LiteLLM）的 mode 归一为 models.model_type 合法枚举。
//
// 行为：
//   - 空 mode（含仅有空白）→ DefaultCatalogModeModelType（"chat"），保留历史兜底；
//   - 已登记的 mode → 对应枚举值（大小写与首尾空白不敏感）；
//   - 未登记的 mode → ErrCatalogModeUnsupported（调用方应拒绝建模型，不得写库）。
//
// 返回值保证：err == nil 时结果一定在 supportedModelTypes 内。
func NormalizeCatalogMode(mode string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(mode))
	if key == "" {
		return DefaultCatalogModeModelType, nil
	}
	mapped, ok := catalogModeModelTypeMapping[key]
	if !ok {
		return "", fmt.Errorf("%w：%q（请改用「手动创建模型」并显式指定 model_type，合法取值：%s）",
			ErrCatalogModeUnsupported, mode, strings.Join(SupportedModelTypes(), "/"))
	}
	if !IsSupportedModelType(mapped) {
		// 防御：映射表被改成枚举外的值时，宁可拒绝也不写脏值（属编程错误，单测会先暴露）。
		return "", fmt.Errorf("%w：内部映射 %q → %q 不在合法枚举内", ErrCatalogModeUnsupported, mode, mapped)
	}
	return mapped, nil
}
