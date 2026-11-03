package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A12：catalog（LiteLLM）`mode` → `models.model_type` 归一的防回归测试。
//
// 缺陷背景：`CreateModelFromCatalog` 曾把 `cat.Mode` 原样写入 `models.model_type`，
// 而两者不是同一套词表，且 `models` 表没有 CHECK 约束 → `image_generation` 之类的脏值能落库。
// 后果链（静默不可用）：
//
//	ModelGroupSelector.vue → listAllModels({model_type}) → model_repo.go 的 `model_type = ?`
//	精确等值匹配 → 组类型 image 时筛不到 image_generation 的模型；
//	group_service.go 的 assertSameModelType 只比成员之间、不比 group.ModelType → 不报错。
//
// 另一症状：脏值不在 binding 白名单内，用户编辑该模型回传时被 `omitempty,oneof=...` 拒绝。

// catalogModeSeedCounts 是仓库自带种子 catalog
// （internal/controlplane/seed/litellm_catalog.json，2725 条）的 mode 实测分布，
// 注释里的数字仅作对照，测试断言的是分类结果。
var catalogModeSeedCounts = []struct {
	mode  string
	count int
}{
	{"chat", 2086},
	{"image_generation", 197},
	{"embedding", 117},
	{"responses", 80},
	{"audio_transcription", 57},
	{"completion", 35},
	{"image_edit", 31},
	{"rerank", 30},
	{"audio_speech", 27},
	{"video_generation", 25},
	{"search", 14},
	{"ocr", 9},
	{"moderation", 5},
	{"realtime", 2},
	{"vector_store", 1},
}

// catalogModeMappingWant 是「有枚举落点」的 mode → model_type 期望映射。
var catalogModeMappingWant = map[string]string{
	"chat":             "chat",
	"completion":       "chat",
	"responses":        "chat",
	"embedding":        "embedding",
	"rerank":           "rerank",
	"image_generation": "image",
	"image_edit":       "image",
	"video_generation": "video",
}

// catalogModeRejected 是**显式拒绝**的 mode 清单（策略：不写脏值、也不伪装成 chat）。
//
// 之所以显式列出：这 7 类在 model_type 枚举里没有诚实的落点
// （枚举只有 chat/image/embedding/embedding_vl/rerank/rerank_vl/video，
// 组类型也是同一套白名单），保留原值 = 建出「选不中且编辑被拒」的僵尸行；
// 兜底 chat = 让音频/OCR/审核模型出现在 chat 组选择器里，请求最终打到不支持的上游。
// 因此选择拒绝，并提示管理员改用「手动创建模型」显式指定 model_type。
var catalogModeRejected = []string{
	"audio_transcription",
	"audio_speech",
	"search",
	"ocr",
	"moderation",
	"realtime",
	"vector_store",
}

// catalogModeKnownDirtySeed 是种子 catalog 里**已知的脏值**：LiteLLM 把报错文案当成了 mode 写进目录。
//
// 单独成列而不是塞进 catalogModeRejected，是为了让读者一眼看出
// 「这一条是数据源脏」（应当拒绝并告警），而不是「我们没有枚举落点」。
// 归一函数对它的处理与其它未知值一致：拒绝，绝不写库。
var catalogModeKnownDirtySeed = []string{
	"one of: chat, embedding, completion, image_generation, audio_transcription, audio_speech, image_generation, moderation, rerank, search",
}

// TestCatalogModeNormalize_AllSeedModes 表驱动覆盖种子 catalog 的 15 种真实 mode：
// 有落点的断言具体映射结果，无落点的断言显式拒绝；
// **任何成功归一的结果都必须落在 model_type 合法枚举内**（核心不变量）。
func TestCatalogModeNormalize_AllSeedModes(t *testing.T) {
	t.Logf("catalog mode → model_type（合法枚举：%v）", expectedModelTypes)

	for _, tc := range catalogModeSeedCounts {
		t.Run(tc.mode, func(t *testing.T) {
			got, err := service.NormalizeCatalogMode(tc.mode)

			if want, ok := catalogModeMappingWant[tc.mode]; ok {
				require.NoError(t, err, "mode=%q 应可归一", tc.mode)
				assert.Equal(t, want, got, "mode=%q 的归一结果", tc.mode)
				assert.Contains(t, expectedModelTypes, got,
					"mode=%q 的归一结果 %q 必须落在合法枚举内", tc.mode, got)
				t.Logf("%-20s (种子 %4d 条) → %-12s ✓ 合法枚举内", tc.mode, tc.count, got)
				return
			}

			require.ErrorIs(t, err, service.ErrCatalogModeUnsupported,
				"无枚举落点的 mode=%q 必须显式拒绝", tc.mode)
			assert.Empty(t, got, "拒绝时不得返回任何 model_type")
			t.Logf("%-20s (种子 %4d 条) → 拒绝（无枚举落点，%v）", tc.mode, tc.count,
				service.ErrCatalogModeUnsupported)
		})
	}
}

// TestCatalogModeNormalize_ExceptionListIsExhaustive 显式锁定「白名单外的例外清单」：
// 种子里的 15 种 mode 必须恰好等于「8 种可归一 + 7 种拒绝」，不多不少。
// 目的：避免以后有人误以为所有 mode 都归一了，也避免新 mode 悄悄漏网。
func TestCatalogModeNormalize_ExceptionListIsExhaustive(t *testing.T) {
	seedModes := make([]string, 0, len(catalogModeSeedCounts))
	for _, tc := range catalogModeSeedCounts {
		seedModes = append(seedModes, tc.mode)
	}
	sort.Strings(seedModes)

	classified := make([]string, 0, len(catalogModeMappingWant)+len(catalogModeRejected))
	for mode := range catalogModeMappingWant {
		classified = append(classified, mode)
	}
	classified = append(classified, catalogModeRejected...)
	sort.Strings(classified)

	assert.Equal(t, seedModes, classified,
		"种子 mode 清单必须恰好等于「可归一 ∪ 显式拒绝」；新增 mode 时请归类并更新本测试")
	assert.Len(t, catalogModeRejected, 7, "拒绝清单变化时必须同步本文件与 model_type_normalize.go 的注释")

	// 已知脏值桶与「可归一」「显式拒绝」两桶不得重叠：
	_, mappable := catalogModeMappingWant[catalogModeKnownDirtySeed[0]]
	assert.False(t, mappable, "LiteLLM 报错文案不得被登记为可归一 mode")
	assert.NotContains(t, catalogModeRejected, catalogModeKnownDirtySeed[0])
}

// TestCatalogModeNormalize_MappingTableIsWithinWhitelist 遍历映射表本体：
// 每个值都必须是合法 model_type，且键集合与期望完全一致（防止悄悄加一条映射）。
func TestCatalogModeNormalize_MappingTableIsWithinWhitelist(t *testing.T) {
	mapping := service.CatalogModeModelTypeMapping()
	require.NotEmpty(t, mapping)

	keys := make([]string, 0, len(mapping))
	for mode, modelType := range mapping {
		keys = append(keys, mode)
		assert.Contains(t, expectedModelTypes, modelType,
			"映射 %q → %q 的结果不在合法枚举内", mode, modelType)
		assert.True(t, service.IsSupportedModelType(modelType),
			"IsSupportedModelType 应认可映射结果 %q", modelType)
	}
	sort.Strings(keys)

	wantKeys := make([]string, 0, len(catalogModeMappingWant))
	for mode := range catalogModeMappingWant {
		wantKeys = append(wantKeys, mode)
	}
	sort.Strings(wantKeys)

	assert.Equal(t, wantKeys, keys, "映射表的键集合必须与已评审清单一致")
}

// TestSupportedModelTypes_MatchesBindingContract 交叉校验：归一文件里的枚举副本
// 必须与 A5 建立的白名单契约（model_type_image_test.go 的 expectedModelTypes，
// 且该列表已被 A5 的迁移 CHECK 测试锁定）完全一致，防止两处漂移。
func TestSupportedModelTypes_MatchesBindingContract(t *testing.T) {
	got := service.SupportedModelTypes()
	want := append([]string(nil), expectedModelTypes...)
	sort.Strings(want)

	assert.Equal(t, want, got, "SupportedModelTypes() 必须等于 binding/迁移的契约白名单")
	for _, mt := range expectedModelTypes {
		assert.True(t, service.IsSupportedModelType(mt), "%q 应被认定为合法", mt)
	}
	assert.False(t, service.IsSupportedModelType("image_generation"))
	assert.False(t, service.IsSupportedModelType(""))
	assert.False(t, service.IsSupportedModelType(" chat"), "IsSupportedModelType 须精确匹配（与 binding oneof 一致）")
	assert.False(t, service.IsSupportedModelType("IMAGE"))
}

// TestCatalogModeNormalize_EmptyAndUnknown 空值兜底与未知值拒绝的边界。
func TestCatalogModeNormalize_EmptyAndUnknown(t *testing.T) {
	t.Run("空 mode 保留历史兜底 chat", func(t *testing.T) {
		for _, mode := range []string{"", "   ", "\t"} {
			got, err := service.NormalizeCatalogMode(mode)
			require.NoError(t, err, "mode=%q", mode)
			assert.Equal(t, service.DefaultCatalogModeModelType, got)
			assert.Equal(t, "chat", got, "空 mode 的历史行为是 chat")
		}
	})

	t.Run("大小写与空白不敏感", func(t *testing.T) {
		got, err := service.NormalizeCatalogMode("  Image_Generation ")
		require.NoError(t, err)
		assert.Equal(t, "image", got)
	})

	t.Run("未知取值（含 LiteLLM 报错文案脏数据）被拒", func(t *testing.T) {
		dirty := []string{
			"one of: chat, embedding, completion, image_generation, audio_transcription, audio_speech, image_generation, moderation, rerank, search",
			"image_generations",
			"ImageGeneration",
			"unknown_mode",
		}
		for _, mode := range dirty {
			got, err := service.NormalizeCatalogMode(mode)
			require.ErrorIs(t, err, service.ErrCatalogModeUnsupported, "mode=%q", mode)
			assert.Empty(t, got)
		}
	})
}

// TestCreateModelFromCatalog_ModelTypeNeverOutsideWhitelist 端到端（service + fake repo）：
// 用种子里的 15 种 mode 各建一次模型，断言写库的 model_type 只可能是合法枚举值；
// 没有落点的 mode 必须**写库前**就失败（repo.Create 零调用 → 不留脏行）。
func TestCreateModelFromCatalog_ModelTypeNeverOutsideWhitelist(t *testing.T) {
	t.Logf("经 CreateModelFromCatalog 落库的 model_type（%d 种 mode）", len(catalogModeSeedCounts))

	for _, tc := range catalogModeSeedCounts {
		t.Run(tc.mode, func(t *testing.T) {
			catID, providerID := uuid.New(), uuid.New()
			modelRepo := &batchFakeModelRepo{}
			svc := service.NewModelService(modelRepo, &batchFakeProviderRepo{
				providers: map[uuid.UUID]*model.Provider{providerID: {ID: providerID, Name: "p", Enabled: true}},
			}, nil)
			svc.SetCatalogLookup(&batchFakeCatalogLookup{byID: map[uuid.UUID]*model.ModelCatalog{
				catID: {ID: catID, SourceModelID: "upstream-" + tc.mode, DisplayName: tc.mode, Mode: tc.mode},
			}})

			m, err := svc.CreateModelFromCatalog(catID, providerID, "model-"+tc.mode)

			if want, ok := catalogModeMappingWant[tc.mode]; ok {
				require.NoError(t, err, "mode=%q 应可建模型", tc.mode)
				require.NotNil(t, m)
				assert.Equal(t, want, m.ModelType)
				assert.Contains(t, expectedModelTypes, m.ModelType,
					"落库的 model_type %q 必须落在合法枚举内", m.ModelType)
				require.Len(t, modelRepo.created, 1)
				t.Logf("%-20s → models.model_type = %-12s ✓", tc.mode, m.ModelType)
				return
			}

			require.ErrorIs(t, err, service.ErrCatalogModeUnsupported, "mode=%q 应被拒绝", tc.mode)
			assert.Nil(t, m, "拒绝时不得返回模型")
			assert.Empty(t, modelRepo.created, "拒绝时必须零写入，不得留下白名单外的脏行")
			t.Logf("%-20s → 拒绝，repo.Create 零调用（无脏行）", tc.mode)
		})
	}
}

// TestCreateModelFromCatalog_EmptyModeFallsBackToChat 保留历史兜底行为：
// catalog 无 mode（种子中有 8 条）时落 chat，而不是报错或写空串。
func TestCreateModelFromCatalog_EmptyModeFallsBackToChat(t *testing.T) {
	catID, providerID := uuid.New(), uuid.New()
	modelRepo := &batchFakeModelRepo{}
	svc := service.NewModelService(modelRepo, &batchFakeProviderRepo{
		providers: map[uuid.UUID]*model.Provider{providerID: {ID: providerID, Enabled: true}},
	}, nil)
	svc.SetCatalogLookup(&batchFakeCatalogLookup{byID: map[uuid.UUID]*model.ModelCatalog{
		catID: {ID: catID, SourceModelID: "no-mode", Mode: ""},
	}})

	m, err := svc.CreateModelFromCatalog(catID, providerID, "no-mode-model")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "chat", m.ModelType)
	require.Len(t, modelRepo.created, 1)
}

// TestCreateModelFromCatalog_RealSeedModesAreClassified 直接读仓库自带的种子 catalog，
// 对**文件里真实出现的每一种 mode** 断言：
//   - 能归一 → 结果必落在合法枚举内；
//   - 不能归一 → 必须被显式拒绝（不得写脏值）。
//
// 并且：若种子出现了本文件未归类的新 mode，测试失败，强制人工归类
// （新 mode 静默落到「拒绝」虽然安全，但会让新能力悄悄不可用，必须有人看一眼）。
func TestCreateModelFromCatalog_RealSeedModesAreClassified(t *testing.T) {
	path := filepath.Join("..", "..", "..", "internal", "controlplane", "seed", "litellm_catalog.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("跳过：无法读取种子 catalog（%s）：%v", path, err)
	}

	var entries map[string]struct {
		Mode string `json:"mode"`
	}
	require.NoError(t, json.Unmarshal(raw, &entries))
	require.NotEmpty(t, entries)

	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Mode]++
	}
	modes := make([]string, 0, len(counts))
	for mode := range counts {
		if mode != "" { // 空 mode 单独在 TestCatalogModeNormalize_EmptyAndUnknown 覆盖
			modes = append(modes, mode)
		}
	}
	sort.Strings(modes)

	// 真实出现的 mode 必须全部已归类（可归一 ∪ 显式拒绝 ∪ 已知脏值）。
	classified := map[string]bool{}
	for mode := range catalogModeMappingWant {
		classified[mode] = true
	}
	for _, mode := range catalogModeRejected {
		classified[mode] = true
	}
	for _, mode := range catalogModeKnownDirtySeed {
		classified[mode] = true
	}
	for _, mode := range modes {
		require.True(t, classified[mode],
			"种子 catalog 出现未归类的 mode=%q（%d 条）：请在 catalogModeModelTypeMapping 中登记映射，"+
				"或确认拒绝并加入 catalogModeRejected，同时更新本测试的清单", mode, counts[mode])
	}

	// 不变量：可归一的必落在合法枚举内；不可归一的必被拒绝。
	for _, mode := range modes {
		got, err := service.NormalizeCatalogMode(mode)
		if err == nil {
			assert.Contains(t, expectedModelTypes, got,
				"mode=%q 归一结果 %q 必须落在合法枚举内", mode, got)
			continue
		}
		require.ErrorIs(t, err, service.ErrCatalogModeUnsupported, "mode=%q", mode)
		assert.Empty(t, got)
	}
	t.Logf("种子 catalog %d 条、%d 种 mode：全部已归类且满足「合法枚举否则拒绝」不变量",
		len(entries), len(modes))
}
