package service_test

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGroupInfoRepo 模拟 GroupInfoRepo 用于 ModelInfoService 单测
type fakeGroupInfoRepo struct {
	listFunc func() ([]model.Group, error)
}

func (f *fakeGroupInfoRepo) ListEnabledGroupsWithModels() ([]model.Group, error) {
	if f.listFunc != nil {
		return f.listFunc()
	}
	return nil, nil
}

// fakeModelInfoRepo 模拟 ModelInfoRepo 用于 ModelInfoService 单测
type fakeModelInfoRepo struct {
	getByIDsFunc func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error)
}

func (f *fakeModelInfoRepo) GetByIDsWithCatalog(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	if f.getByIDsFunc != nil {
		return f.getByIDsFunc(ids)
	}
	return nil, nil
}

// uuidPtr 返回 uuid.UUID 指针，方便构造 Model.CatalogID
func uuidPtr(id uuid.UUID) *uuid.UUID {
	return &id
}

// boolPtr 返回 bool 指针，方便构造 Model.Enabled
func boolPtr(v bool) *bool {
	return &v
}

// 预置的 uuid 值，在测试中作为 ModelID 和 CatalogID 使用
var (
	modelID1 = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	modelID2 = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	modelID3 = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	catID1   = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	catID2   = uuid.MustParse("10000000-0000-0000-0000-000000000002")
	catID3   = uuid.MustParse("10000000-0000-0000-0000-000000000003")
)

func TestModelInfoService_GetGroupModelInfo(t *testing.T) {
	t.Run("单个模型组_能力正确映射", func(t *testing.T) {
		// 测试目的：单模型组，Catalog 中全部能力位为 true，验证聚合结果全部为 true
		groupID := uint(1)
		g := model.Group{
			ID:      groupID,
			Name:    "gpt-4o",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID:              modelID1,
				Name:            "gpt-4o",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   128000,
				MaxOutputTokens: 4096,
				CatalogID:       uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:                      catID1,
					Mode:                    "chat",
					ContextLength:           128000,
					MaxOutputTokens:         4096,
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "gpt-4o")

		info := result["gpt-4o"]
		assert.Equal(t, "chat", info.Mode, "模式应为 chat")
		assert.Equal(t, 128000, info.MaxInputTokens, "max_input_tokens 应为 128000")
		assert.Equal(t, 4096, info.MaxOutputTokens, "max_output_tokens 应为 4096")
		assert.True(t, info.SupportsVision, "vision 应为 true")
		assert.True(t, info.SupportsFunctionCalling, "function_calling 应为 true")
		assert.True(t, info.SupportsToolChoice, "tool_choice 应为 true")
	})

	t.Run("多模型组_全部能力一致_聚合全部支持", func(t *testing.T) {
		// 测试目的：组内两个模型的 Catalog 能力位全部为 true，聚合后全部能力位仍为 true
		groupID := uint(2)
		g := model.Group{
			ID:      groupID,
			Name:    "claude-team",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID:              modelID1,
				Name:            "claude-sonnet",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   200000,
				MaxOutputTokens: 8192,
				CatalogID:       uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:                      catID1,
					Mode:                    "chat",
					ContextLength:           200000,
					MaxOutputTokens:         8192,
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
			modelID2: {
				ID:              modelID2,
				Name:            "claude-haiku",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   200000,
				MaxOutputTokens: 4096,
				CatalogID:       uuidPtr(catID2),
				Catalog: &model.ModelCatalog{
					ID:                      catID2,
					Mode:                    "chat",
					ContextLength:           200000,
					MaxOutputTokens:         4096,
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "claude-team")

		info := result["claude-team"]
		assert.True(t, info.SupportsVision, "全部支持 vision，聚合应为 true")
		assert.True(t, info.SupportsFunctionCalling, "全部支持 function_calling，聚合应为 true")
		assert.True(t, info.SupportsToolChoice, "全部支持 tool_choice，聚合应为 true")
	})

	t.Run("多模型组_一个模型缺能力_保守聚合不支持", func(t *testing.T) {
		// 测试目的：组内第二个模型 Catalog 的 SupportsVision=false，保守语义下最终 vision=false
		groupID := uint(3)
		g := model.Group{
			ID:      groupID,
			Name:    "mixed-vision",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID:              modelID1,
				Name:            "gpt-4o-vision",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   128000,
				MaxOutputTokens: 4096,
				CatalogID:       uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:                      catID1,
					Mode:                    "chat",
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
			modelID2: {
				ID:              modelID2,
				Name:            "gpt-4o-mini",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   128000,
				MaxOutputTokens: 4096,
				CatalogID:       uuidPtr(catID2),
				Catalog: &model.ModelCatalog{
					ID:                      catID2,
					Mode:                    "chat",
					SupportsVision:          false, // 这个模型不支持 vision
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "mixed-vision")

		info := result["mixed-vision"]
		assert.False(t, info.SupportsVision, "有一个模型不支持 vision，保守语义下应为 false")
		assert.True(t, info.SupportsFunctionCalling, "都支持 function_calling，应为 true")
		assert.True(t, info.SupportsToolChoice, "都支持 tool_choice，应为 true")
	})

	t.Run("多模型组_不同token上限_取最小正值", func(t *testing.T) {
		// 测试目的：组内两个模型 ContextLength 不同（128000 vs 64000），聚合取最小值 64000；
		// MaxOutputTokens 不同（4096 vs 2048），聚合取最小值 2048
		groupID := uint(4)
		g := model.Group{
			ID:      groupID,
			Name:    "token-mixed",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID:              modelID1,
				Name:            "big-context-model",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   128000,
				MaxOutputTokens: 4096,
				Catalog: &model.ModelCatalog{
					ID:                      catID1,
					Mode:                    "chat",
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
			modelID2: {
				ID:              modelID2,
				Name:            "small-context-model",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   64000,
				MaxOutputTokens: 2048,
				Catalog: &model.ModelCatalog{
					ID:                      catID2,
					Mode:                    "chat",
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "token-mixed")

		info := result["token-mixed"]
		assert.Equal(t, 64000, info.MaxInputTokens, "input token 应取最小值 64000")
		assert.Equal(t, 2048, info.MaxOutputTokens, "output token 应取最小值 2048")
	})

	t.Run("空模型组_返回零值能力信息", func(t *testing.T) {
		// 测试目的：组内没有关联任何模型，聚合结果应为 GroupModelInfo 零值
		groupID := uint(5)
		g := model.Group{
			ID:      groupID,
			Name:    "empty-group",
			Enabled: true,
			Models:  nil,
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return nil, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "empty-group")

		info := result["empty-group"]
		assert.Equal(t, "", info.Mode, "空组 mode 应为空")
		assert.Equal(t, 0, info.MaxInputTokens, "空组 max_input_tokens 应为 0")
		assert.Equal(t, 0, info.MaxOutputTokens, "空组 max_output_tokens 应为 0")
		assert.False(t, info.SupportsVision, "空组 vision 应为 false")
		assert.False(t, info.SupportsFunctionCalling, "空组 function_calling 应为 false")
		assert.False(t, info.SupportsToolChoice, "空组 tool_choice 应为 false")
	})

	t.Run("按名称筛选_只返回匹配的模型组", func(t *testing.T) {
		// 测试目的：传入 names=["gpt-4o"]，只返回名为 gpt-4o 的组，不返回 claude-team
		g1 := model.Group{ID: 6, Name: "gpt-4o", Enabled: true, Models: nil}
		g2 := model.Group{ID: 7, Name: "claude-team", Enabled: true, Models: nil}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g1, g2}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return nil, nil }},
		)

		result, err := svc.GetGroupModelInfo([]string{"gpt-4o"})
		require.NoError(t, err)
		assert.Contains(t, result, "gpt-4o", "应包含 gpt-4o")
		assert.NotContains(t, result, "claude-team", "不应包含 claude-team")
		assert.Len(t, result, 1, "应只返回 1 个组")
	})

	t.Run("未知模型组名_返回空结果", func(t *testing.T) {
		// 测试目的：传入的 names 在已启用组中不存在，返回空 map，不报错
		g := model.Group{ID: 8, Name: "gpt-4o", Enabled: true, Models: nil}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return nil, nil }},
		)

		result, err := svc.GetGroupModelInfo([]string{"nonexistent-group"})
		require.NoError(t, err)
		assert.Empty(t, result, "不存在的组名应返回空 map")
	})

	t.Run("GroupRepo报错_传播错误", func(t *testing.T) {
		// 测试目的：groupRepo 返回 error，service 应原样传播
		wantErr := errors.New("database connection failed")
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return nil, wantErr }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return nil, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		assert.ErrorIs(t, err, wantErr, "应返回 GroupRepo 的原始错误")
		assert.Nil(t, result, "出错时 result 应为 nil")
	})

	t.Run("ModelRepo报错_传播错误", func(t *testing.T) {
		// 测试目的：modelRepo 返回 error，service 应原样传播
		wantErr := errors.New("model lookup failed")
		g := model.Group{ID: 9, Name: "gpt-4o", Enabled: true,
			Models: []model.GroupModel{{GroupID: 9, ModelID: modelID1}},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return nil, wantErr }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		assert.ErrorIs(t, err, wantErr, "应返回 ModelRepo 的原始错误")
		assert.Nil(t, result, "出错时 result 应为 nil")
	})

	t.Run("禁用模型_不被纳入聚合", func(t *testing.T) {
		// 测试目的：组内有两个模型，一个启用（支持 vision），一个禁用（不支持 vision），
		// 禁用模型不应参与聚合，因此最终 vision=true
		groupID := uint(10)
		g := model.Group{
			ID:      groupID,
			Name:    "vision-group",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID:              modelID1,
				Name:            "vision-model",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   128000,
				MaxOutputTokens: 4096,
				CatalogID:       uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:                      catID1,
					Mode:                    "chat",
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
			modelID2: {
				ID:              modelID2,
				Name:            "disabled-model",
				ModelType:       "chat",
				Enabled:         boolPtr(false), // 禁用
				ContextLength:   64000,
				MaxOutputTokens: 2048,
				CatalogID:       uuidPtr(catID2),
				Catalog: &model.ModelCatalog{
					ID:                      catID2,
					Mode:                    "chat",
					SupportsVision:          false,
					SupportsFunctionCalling: false,
					SupportsToolChoice:      false,
				},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "vision-group")

		info := result["vision-group"]
		assert.True(t, info.SupportsVision, "禁用模型不参与聚合，vision 应为 true")
		assert.True(t, info.SupportsFunctionCalling, "禁用模型不参与聚合，function_calling 应为 true")
		assert.True(t, info.SupportsToolChoice, "禁用模型不参与聚合，tool_choice 应为 true")
		assert.Equal(t, 128000, info.MaxInputTokens, "禁用模型不参与聚合，max_input_tokens 应为启用模型的 128000")
		assert.Equal(t, 4096, info.MaxOutputTokens, "禁用模型不参与聚合，max_output_tokens 应为启用模型的 4096")
	})

	t.Run("未绑定Catalog的模型_能力视为不支持_拉低聚合能力", func(t *testing.T) {
		// 测试目的：组内一个模型绑定了 Catalog（全能力），另一个没有绑定 Catalog（能力未知），
		// 未绑定 Catalog 的成员能力视为不支持，最终全部能力位为 false
		groupID := uint(11)
		g := model.Group{
			ID:      groupID,
			Name:    "partial-catalog",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID:              modelID1,
				Name:            "with-catalog",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   128000,
				MaxOutputTokens: 4096,
				CatalogID:       uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:                      catID1,
					Mode:                    "chat",
					SupportsVision:          true,
					SupportsFunctionCalling: true,
					SupportsToolChoice:      true,
				},
			},
			modelID2: {
				ID:              modelID2,
				Name:            "no-catalog",
				ModelType:       "chat",
				Enabled:         boolPtr(true),
				ContextLength:   64000,
				MaxOutputTokens: 2048,
				CatalogID:       nil, // 没有绑定 Catalog
				Catalog:         nil, // 没有预加载 Catalog
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "partial-catalog")

		info := result["partial-catalog"]
		assert.False(t, info.SupportsVision, "未绑定 Catalog 的模型能力未知=不支持，vision 应为 false")
		assert.False(t, info.SupportsFunctionCalling, "未绑定 Catalog 的模型能力未知=不支持，function_calling 应为 false")
		assert.False(t, info.SupportsToolChoice, "未绑定 Catalog 的模型能力未知=不支持，tool_choice 应为 false")
		// token 仍取最小正值
		assert.Equal(t, 64000, info.MaxInputTokens, "token 取所有启用模型的最小值 64000")
		assert.Equal(t, 2048, info.MaxOutputTokens, "output token 取最小值 2048")
	})
}

func TestModelInfoService_GetGroupModelInfo_ThreeMembers(t *testing.T) {
	t.Run("3个成员全部支持_聚合能力全部为true", func(t *testing.T) {
		groupID := uint(20)
		g := model.Group{
			ID:      groupID,
			Name:    "triple-all-cap",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
				{GroupID: groupID, ModelID: modelID3},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				ContextLength: 128000, MaxOutputTokens: 4096,
				CatalogID: uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID: catID1, Mode: "chat",
					SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true,
				},
			},
			modelID2: {
				ID: modelID2, Name: "m2", ModelType: "chat", Enabled: boolPtr(true),
				ContextLength: 64000, MaxOutputTokens: 8192,
				CatalogID: uuidPtr(catID2),
				Catalog: &model.ModelCatalog{
					ID: catID2, Mode: "chat",
					SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true,
				},
			},
			modelID3: {
				ID: modelID3, Name: "m3", ModelType: "chat", Enabled: boolPtr(true),
				ContextLength: 200000, MaxOutputTokens: 16384,
				CatalogID: uuidPtr(catID3),
				Catalog: &model.ModelCatalog{
					ID: catID3, Mode: "chat",
					SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true,
				},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "triple-all-cap")

		info := result["triple-all-cap"]
		assert.Equal(t, "chat", info.Mode)
		assert.True(t, info.SupportsVision, "3 个成员全部支持 vision，聚合应为 true")
		assert.True(t, info.SupportsFunctionCalling, "3 个成员全部支持 function_calling，聚合应为 true")
		assert.True(t, info.SupportsToolChoice, "3 个成员全部支持 tool_choice，聚合应为 true")
		assert.Equal(t, 64000, info.MaxInputTokens, "3 个成员取最小 context 64000")
		assert.Equal(t, 4096, info.MaxOutputTokens, "3 个成员取最小 output 4096")
	})

	t.Run("3个成员token_8000_32000_16000_取最小正值8000", func(t *testing.T) {
		groupID := uint(21)
		g := model.Group{
			ID:      groupID,
			Name:    "triple-tokens",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
				{GroupID: groupID, ModelID: modelID3},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "small-ctx", ModelType: "chat", Enabled: boolPtr(true),
				ContextLength: 8000, MaxOutputTokens: 2048,
				Catalog: &model.ModelCatalog{Mode: "chat", SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true},
			},
			modelID2: {
				ID: modelID2, Name: "big-ctx", ModelType: "chat", Enabled: boolPtr(true),
				ContextLength: 32000, MaxOutputTokens: 4096,
				Catalog: &model.ModelCatalog{Mode: "chat", SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true},
			},
			modelID3: {
				ID: modelID3, Name: "mid-ctx", ModelType: "chat", Enabled: boolPtr(true),
				ContextLength: 16000, MaxOutputTokens: 8192,
				Catalog: &model.ModelCatalog{Mode: "chat", SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "triple-tokens")

		info := result["triple-tokens"]
		assert.Equal(t, 8000, info.MaxInputTokens, "3 个成员 8000/32000/16000 → 最小正值 8000")
		assert.Equal(t, 2048, info.MaxOutputTokens, "3 个成员 2048/4096/8192 → 最小正值 2048")
	})

	t.Run("3个成员mode_embeddings_chat_embedding_聚合为chat", func(t *testing.T) {
		groupID := uint(22)
		g := model.Group{
			ID:      groupID,
			Name:    "mode-mix-three",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
				{GroupID: groupID, ModelID: modelID3},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "embed-model", ModelType: "embeddings", Enabled: boolPtr(true),
				CatalogID: uuidPtr(catID1),
				Catalog:   &model.ModelCatalog{ID: catID1, Mode: "embeddings"},
			},
			modelID2: {
				ID: modelID2, Name: "chat-model", ModelType: "chat", Enabled: boolPtr(true),
				CatalogID: uuidPtr(catID2),
				Catalog:   &model.ModelCatalog{ID: catID2, Mode: "chat"},
			},
			modelID3: {
				ID: modelID3, Name: "embed-model-2", ModelType: "embedding", Enabled: boolPtr(true),
				CatalogID: uuidPtr(catID3),
				Catalog:   &model.ModelCatalog{ID: catID3, Mode: "embedding"},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "mode-mix-three")

		info := result["mode-mix-three"]
		assert.Equal(t, "chat", info.Mode, "有 chat 成员时，聚合 mode 应为 chat")
	})
}

func TestModelInfoService_GetGroupModelInfo_EmptyNames(t *testing.T) {
	// 测试目的：names 为空切片（非 nil），行为应等同于 nil——返回所有启用组
	g := model.Group{ID: 12, Name: "default-group", Enabled: true, Models: nil}

	svc := service.NewModelInfoService(
		&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
		&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return nil, nil }},
	)

	result, err := svc.GetGroupModelInfo([]string{})
	require.NoError(t, err)
	assert.Contains(t, result, "default-group", "空 names 应返回所有启用组")
}

func TestModelInfoService_GetGroupModelInfo_ModeAggregation(t *testing.T) {
	t.Run("任一成员为chat_mode即为chat", func(t *testing.T) {
		// 测试目的：组内两个模型，一个 mode=completion，一个 mode=chat，聚合 mode 应为 chat
		groupID := uint(13)
		g := model.Group{
			ID:      groupID,
			Name:    "mixed-mode",
			Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID:        modelID1,
				Name:      "completion-model",
				ModelType: "completion",
				Enabled:   boolPtr(true),
				CatalogID: uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:   catID1,
					Mode: "completion",
				},
			},
			modelID2: {
				ID:        modelID2,
				Name:      "chat-model",
				ModelType: "chat",
				Enabled:   boolPtr(true),
				CatalogID: uuidPtr(catID2),
				Catalog: &model.ModelCatalog{
					ID:   catID2,
					Mode: "chat",
				},
			},
		}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "mixed-mode")
		assert.Equal(t, "chat", result["mixed-mode"].Mode, "有 chat 模式成员，聚合 mode 应为 chat")
	})

	t.Run("无成员_返回空mode", func(t *testing.T) {
		// 测试目的：仅有一个组但组内无模型，聚合 Mode 应为空字符串
		g := model.Group{ID: 14, Name: "no-members", Enabled: true, Models: nil}

		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return nil, nil }},
		)

		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "no-members")
		assert.Equal(t, "", result["no-members"].Mode, "空组 mode 应为空")
	})
}

func TestModelInfoService_GetGroupModelInfo_MultipleGroups(t *testing.T) {
	// 测试目的：多个组同时返回，各自正确聚合，互不干扰
	g1 := model.Group{
		ID: 15, Name: "vision-pro", Enabled: true,
		Models: []model.GroupModel{
			{GroupID: 15, ModelID: modelID1},
			{GroupID: 15, ModelID: modelID2},
		},
	}
	g2 := model.Group{
		ID: 16, Name: "text-lite", Enabled: true,
		Models: []model.GroupModel{
			{GroupID: 16, ModelID: modelID3},
		},
	}
	models := map[uuid.UUID]*model.Model{
		modelID1: {
			ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
			ContextLength: 128000, MaxOutputTokens: 4096,
			CatalogID: uuidPtr(catID1),
			Catalog:   &model.ModelCatalog{ID: catID1, Mode: "chat", SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true},
		},
		modelID2: {
			ID: modelID2, Name: "m2", ModelType: "chat", Enabled: boolPtr(true),
			ContextLength: 64000, MaxOutputTokens: 2048,
			CatalogID: uuidPtr(catID2),
			Catalog:   &model.ModelCatalog{ID: catID2, Mode: "chat", SupportsVision: true, SupportsFunctionCalling: true, SupportsToolChoice: true},
		},
		modelID3: {
			ID: modelID3, Name: "m3", ModelType: "chat", Enabled: boolPtr(true),
			ContextLength: 32000, MaxOutputTokens: 1024,
			CatalogID: uuidPtr(catID3),
			Catalog:   &model.ModelCatalog{ID: catID3, Mode: "chat", SupportsVision: false, SupportsFunctionCalling: false, SupportsToolChoice: false},
		},
	}

	svc := service.NewModelInfoService(
		&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g1, g2}, nil }},
		&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
	)

	result, err := svc.GetGroupModelInfo(nil)
	require.NoError(t, err)
	require.Len(t, result, 2)

	// vision-pro: 两个模型都支持 vision，聚合 true；token 取最小值
	vp := result["vision-pro"]
	assert.True(t, vp.SupportsVision, "vision-pro 全部成员支持 vision")
	assert.Equal(t, 64000, vp.MaxInputTokens, "vision-pro input token 取最小值 64000")
	assert.Equal(t, 2048, vp.MaxOutputTokens, "vision-pro output token 取最小值 2048")

	// text-lite: 一个模型，不支持 vision
	tl := result["text-lite"]
	assert.False(t, tl.SupportsVision, "text-lite 模型不支持 vision")
	assert.Equal(t, 32000, tl.MaxInputTokens, "text-lite input token 32000")
	assert.Equal(t, 1024, tl.MaxOutputTokens, "text-lite output token 1024")
}

func TestModelInfoService_GetGroupModelInfo_CachePrices(t *testing.T) {
	fp := func(v float64) *float64 { return &v }

	t.Run("单成员_catalog缓存价_原样返回", func(t *testing.T) {
		groupID := uint(30)
		g := model.Group{
			ID: groupID, Name: "cache-single", Enabled: true,
			Models: []model.GroupModel{{GroupID: groupID, ModelID: modelID1}},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				PriceMode: "catalog",
				CatalogID: uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID: catID1, Mode: "chat",
					InputPricePerMillion:  3,
					OutputPricePerMillion: 15,
					CacheReadPrice:        fp(0.3),
					CacheWritePrice:       fp(3.75),
				},
			},
		}
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)
		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "cache-single")
		info := result["cache-single"]
		assert.NotNil(t, info.CacheReadPricePerMillion, "catalog 有缓存读价，应返回")
		assert.Equal(t, 0.3, *info.CacheReadPricePerMillion)
		assert.NotNil(t, info.CacheWritePricePerMillion, "catalog 有缓存写价，应返回")
		assert.Equal(t, 3.75, *info.CacheWritePricePerMillion)
	})

	t.Run("多成员_catalog缓存价_取最大值", func(t *testing.T) {
		groupID := uint(31)
		g := model.Group{
			ID: groupID, Name: "cache-multi", Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				CatalogID: uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID: catID1, Mode: "chat",
					CacheReadPrice:  fp(0.1),
					CacheWritePrice: fp(1.0),
				},
			},
			modelID2: {
				ID: modelID2, Name: "m2", ModelType: "chat", Enabled: boolPtr(true),
				CatalogID: uuidPtr(catID2),
				Catalog: &model.ModelCatalog{
					ID: catID2, Mode: "chat",
					CacheReadPrice:  fp(0.3),
					CacheWritePrice: fp(0.5),
				},
			},
		}
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)
		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "cache-multi")
		info := result["cache-multi"]
		assert.NotNil(t, info.CacheReadPricePerMillion, "应返回缓存读价")
		assert.Equal(t, 0.3, *info.CacheReadPricePerMillion, "缓存读取组内最大值 0.3")
		assert.NotNil(t, info.CacheWritePricePerMillion, "应返回缓存写价")
		assert.Equal(t, 1.0, *info.CacheWritePricePerMillion, "缓存写取组内最大值 1.0")
	})

	t.Run("无缓存价_输入价兜底十分之一", func(t *testing.T) {
		groupID := uint(32)
		g := model.Group{
			ID: groupID, Name: "cache-none", Enabled: true,
			Models: []model.GroupModel{{GroupID: groupID, ModelID: modelID1}},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				PriceMode: "catalog",
				CatalogID: uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:                     catID1,
					Mode:                   "chat",
					InputPricePerMillion:  2,
					OutputPricePerMillion: 8,
					CacheReadPrice:        nil,
					CacheWritePrice:       nil,
				},
			},
		}
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)
		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "cache-none")
		info := result["cache-none"]
		assert.NotNil(t, info.CacheReadPricePerMillion, "无缓存价但有输入价，应按输入价 1/10 兜底")
		assert.Equal(t, 0.2, *info.CacheReadPricePerMillion, "输入价 2 的 1/10 应为 0.2")
		assert.NotNil(t, info.CacheWritePricePerMillion, "无缓存写价但有输入价，应按输入价 1/10 兜底")
		assert.Equal(t, 0.2, *info.CacheWritePricePerMillion, "输入价 2 的 1/10 应为 0.2")
		assert.Equal(t, 2.0, info.InputPricePerMillion, "常规输入价不受影响")
		assert.Equal(t, 8.0, info.OutputPricePerMillion, "常规输出价不受影响")
	})

	t.Run("输入价也为0_完全缺价_保持nil", func(t *testing.T) {
		// 模型既没有缓存价也没有输入价（完全缺价），兜底无从谈起，缓存价保持 nil
		groupID := uint(35)
		g := model.Group{
			ID: groupID, Name: "cache-fully-missing", Enabled: true,
			Models: []model.GroupModel{{GroupID: groupID, ModelID: modelID1}},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				PriceMode: "catalog",
				CatalogID: uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID: catID1, Mode: "chat",
					InputPricePerMillion:  0,
					OutputPricePerMillion: 0,
					CacheReadPrice:        nil,
					CacheWritePrice:       nil,
				},
			},
		}
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)
		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "cache-fully-missing")
		info := result["cache-fully-missing"]
		assert.Nil(t, info.CacheReadPricePerMillion, "输入价 0 无兜底，应为 nil")
		assert.Nil(t, info.CacheWritePricePerMillion, "输入价 0 无兜底，应为 nil")
	})

	t.Run("多成员_显式缓存价与兜底混合_取最大值", func(t *testing.T) {
		// m1 无缓存价但输入价 100 → 兜底 10；m2 显式缓存读 0.5/写 0.3。
		// 保守上界：读取 10，写入取 10（兜底写 10 > 显式写 0.3）
		groupID := uint(36)
		g := model.Group{
			ID: groupID, Name: "cache-mixed", Enabled: true,
			Models: []model.GroupModel{
				{GroupID: groupID, ModelID: modelID1},
				{GroupID: groupID, ModelID: modelID2},
			},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				CatalogID: uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:                      catID1,
					Mode:                    "chat",
					InputPricePerMillion:   100,
					OutputPricePerMillion:  200,
					CacheReadPrice:         nil,
					CacheWritePrice:        nil,
				},
			},
			modelID2: {
				ID: modelID2, Name: "m2", ModelType: "chat", Enabled: boolPtr(true),
				CatalogID: uuidPtr(catID2),
				Catalog: &model.ModelCatalog{
					ID:                      catID2,
					Mode:                    "chat",
					InputPricePerMillion:   3,
					OutputPricePerMillion:  12,
					CacheReadPrice:         fp(0.5),
					CacheWritePrice:        fp(0.3),
				},
			},
		}
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)
		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "cache-mixed")
		info := result["cache-mixed"]
		assert.NotNil(t, info.CacheReadPricePerMillion)
		assert.Equal(t, 10.0, *info.CacheReadPricePerMillion, "兜底读 10 > 显式读 0.5，取 10")
		assert.NotNil(t, info.CacheWritePricePerMillion)
		assert.Equal(t, 10.0, *info.CacheWritePricePerMillion, "兜底写 10 > 显式写 0.3，取 10")
	})

	t.Run("manual模式_取模型自身缓存价", func(t *testing.T) {
		groupID := uint(33)
		g := model.Group{
			ID: groupID, Name: "cache-manual", Enabled: true,
			Models: []model.GroupModel{{GroupID: groupID, ModelID: modelID1}},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				PriceMode:       "manual",
				InputPrice:      4,
				OutputPrice:     20,
				CacheReadPrice:  fp(0.4),
				CacheWritePrice: fp(5),
			},
		}
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)
		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "cache-manual")
		info := result["cache-manual"]
		assert.NotNil(t, info.CacheReadPricePerMillion, "manual 缓存读价应返回")
		assert.Equal(t, 0.4, *info.CacheReadPricePerMillion)
		assert.NotNil(t, info.CacheWritePricePerMillion, "manual 缓存写价应返回")
		assert.Equal(t, 5.0, *info.CacheWritePricePerMillion)
	})

	t.Run("catalog模式_模型自身缓存价被忽略", func(t *testing.T) {
		// price_mode=catalog 时，模型自身的 CacheReadPrice/CacheWritePrice 不生效，
		// 应取 catalog 上的缓存价。
		groupID := uint(34)
		g := model.Group{
			ID: groupID, Name: "cache-catalog-ignores-self", Enabled: true,
			Models: []model.GroupModel{{GroupID: groupID, ModelID: modelID1}},
		}
		models := map[uuid.UUID]*model.Model{
			modelID1: {
				ID: modelID1, Name: "m1", ModelType: "chat", Enabled: boolPtr(true),
				PriceMode:      "catalog",
				CacheReadPrice: fp(9.9), // catalog 模式下应被忽略
				CatalogID:      uuidPtr(catID1),
				Catalog: &model.ModelCatalog{
					ID:              catID1,
					Mode:            "chat",
					CacheReadPrice:  fp(0.2),
					CacheWritePrice: fp(2),
				},
			},
		}
		svc := service.NewModelInfoService(
			&fakeGroupInfoRepo{listFunc: func() ([]model.Group, error) { return []model.Group{g}, nil }},
			&fakeModelInfoRepo{getByIDsFunc: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) { return models, nil }},
		)
		result, err := svc.GetGroupModelInfo(nil)
		require.NoError(t, err)
		require.Contains(t, result, "cache-catalog-ignores-self")
		info := result["cache-catalog-ignores-self"]
		assert.NotNil(t, info.CacheReadPricePerMillion)
		assert.Equal(t, 0.2, *info.CacheReadPricePerMillion, "catalog 模式应取 catalog 缓存读价，忽略模型自身 9.9")
		assert.NotNil(t, info.CacheWritePricePerMillion)
		assert.Equal(t, 2.0, *info.CacheWritePricePerMillion)
	})
}