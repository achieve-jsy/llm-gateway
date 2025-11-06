package service

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubCatalogLookup 给 ModelService 做 catalog 查询的 mock。
type stubCatalogLookup struct {
	getByIDFn   func(id uuid.UUID) (*model.ModelCatalog, error)
	getByIDsFn  func(ids []uuid.UUID) (map[uuid.UUID]*model.ModelCatalog, error)
}

func (s *stubCatalogLookup) GetByID(id uuid.UUID) (*model.ModelCatalog, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}

func (s *stubCatalogLookup) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.ModelCatalog, error) {
	if s.getByIDsFn != nil {
		return s.getByIDsFn(ids)
	}
	result := map[uuid.UUID]*model.ModelCatalog{}
	for _, id := range ids {
		cat, err := s.GetByID(id)
		if err != nil {
			return nil, err
		}
		if cat != nil {
			result[id] = cat
		}
	}
	return result, nil
}

// TestCreateModelFromCatalog_CopiesSnapshot 验证从 catalog 创建 model 时复制全部业务字段。
func TestCreateModelFromCatalog_CopiesSnapshot(t *testing.T) {
	pid := uuid.New()
	catalogID := uuid.New()
	cat := &model.ModelCatalog{
		ID:                    catalogID,
		Source:                "litellm",
		SourceModelID:         "gpt-4o-2024-11-20",
		Provider:              "openai",
		Mode:                  "chat",
		ContextLength:         128000,
		MaxOutputTokens:       16384,
		InputPricePerMillion:  2.5,
		OutputPricePerMillion: 10.0,
	}

	var captured *model.Model
	modelRepo := &MockModelRepo{
		existsByNameFn: func(string) (bool, error) { return false, nil },
		createFn: func(m *model.Model) error {
			m.ID = uuid.New()
			captured = m
			return nil
		},
	}
	providerRepo := &MockProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: pid, Name: "p", Enabled: true}, nil
	}}
	lookup := &stubCatalogLookup{getByIDFn: func(id uuid.UUID) (*model.ModelCatalog, error) {
		assert.Equal(t, catalogID, id)
		return cat, nil
	}}

	svc := NewModelService(modelRepo, providerRepo, nil)
	svc.SetCatalogLookup(lookup)

	got, err := svc.CreateModelFromCatalog(catalogID, pid, "gpt-4o-imported")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, captured)
	assert.Equal(t, "gpt-4o-imported", captured.Name)
	assert.Equal(t, &catalogID, captured.CatalogID)
	assert.Equal(t, 128000, captured.ContextLength)
	assert.Equal(t, 16384, captured.MaxOutputTokens)
	assert.InDelta(t, 2.5, captured.InputPrice, 1e-6)
	assert.InDelta(t, 10.0, captured.OutputPrice, 1e-6)
	assert.Equal(t, "gpt-4o-2024-11-20", captured.UpstreamModelID)
	assert.False(t, captured.CtxOverridden, "新建模型不应预设 override")
	assert.False(t, captured.MaxOutputOverridden)
}

// TestCreateModelFromCatalog_CatalogNotFound catalog 不存在应返回错误。
func TestCreateModelFromCatalog_CatalogNotFound(t *testing.T) {
	svc := NewModelService(&MockModelRepo{}, &MockProviderRepo{getByIDFn: func(uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: uuid.New(), Enabled: true}, nil
	}}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(uuid.UUID) (*model.ModelCatalog, error) { return nil, nil }})

	_, err := svc.CreateModelFromCatalog(uuid.New(), uuid.New(), "x")
	require.Error(t, err)
}

// TestUpdateModel_AutoMarksCtxOverridden 当 admin 改 context_length 且 catalog_id != nil 时
// 自动标 ctx_overridden=true。
func TestUpdateModel_AutoMarksCtxOverridden(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	enabled := true
	original := &model.Model{
		ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat", Enabled: &enabled,
		ContextLength: 8000, MaxOutputTokens: 1024,
		CatalogID: &cid,
	}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { c := *original; return &c, nil },
		updateFn:  func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	newCtx := 32000
	_, err := svc.UpdateModel(mid, UpdateModelInput{ContextLength: &newCtx})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.True(t, captured.CtxOverridden, "改 context 应自动锁 ctx_overridden")
	assert.False(t, captured.MaxOutputOverridden, "max_output 没动则不锁 max_output_overridden")
	assert.Equal(t, 32000, captured.ContextLength)
}

// TestUpdateModel_AutoMarksMaxOutputOverridden 同理 max_output_tokens。
func TestUpdateModel_AutoMarksMaxOutputOverridden(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	enabled := true
	original := &model.Model{
		ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat", Enabled: &enabled,
		ContextLength: 8000, MaxOutputTokens: 1024,
		CatalogID: &cid,
	}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { c := *original; return &c, nil },
		updateFn:  func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	newMO := 8192
	_, err := svc.UpdateModel(mid, UpdateModelInput{MaxOutputTokens: &newMO})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.False(t, captured.CtxOverridden)
	assert.True(t, captured.MaxOutputOverridden)
	assert.Equal(t, 8192, captured.MaxOutputTokens)
}

// TestUpdateModel_NoCatalog_DoesNotMarkOverridden 没绑定 catalog 时不应自动设 override。
func TestUpdateModel_NoCatalog_DoesNotMarkOverridden(t *testing.T) {
	mid := uuid.New()
	enabled := true
	original := &model.Model{
		ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat", Enabled: &enabled,
		ContextLength: 8000, // 没 CatalogID
	}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { c := *original; return &c, nil },
		updateFn:  func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	newCtx := 32000
	_, err := svc.UpdateModel(mid, UpdateModelInput{ContextLength: &newCtx})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.False(t, captured.CtxOverridden, "无 catalog 关联时不要乱设 override flag")
}

// TestResetOverride_ReturnsToCatalogValue 清掉 override flag 并把字段重置为 catalog 当前值。
func TestResetOverride_ReturnsToCatalogValue(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	enabled := true
	original := &model.Model{
		ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat", Enabled: &enabled,
		ContextLength: 99, MaxOutputTokens: 99,
		CtxOverridden:       true,
		MaxOutputOverridden: true,
		CatalogID:           &cid,
	}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { c := *original; return &c, nil },
		updateFn:  func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(id uuid.UUID) (*model.ModelCatalog, error) {
		assert.Equal(t, cid, id)
		return &model.ModelCatalog{ID: cid, ContextLength: 128000, MaxOutputTokens: 16384}, nil
	}})

	require.NoError(t, svc.ResetOverride(mid, "context_length"))
	require.NotNil(t, captured)
	assert.False(t, captured.CtxOverridden)
	assert.True(t, captured.MaxOutputOverridden, "另一字段 override 不被影响")
	assert.Equal(t, 128000, captured.ContextLength)

	// 再 reset 另一个：先模拟 admin 已经接受过 ctx 重置
	captured = nil
	original.CtxOverridden = false
	original.ContextLength = 128000
	require.NoError(t, svc.ResetOverride(mid, "max_output_tokens"))
	require.NotNil(t, captured)
	assert.False(t, captured.MaxOutputOverridden)
	assert.Equal(t, 16384, captured.MaxOutputTokens)
}

// TestResetOverride_InvalidField 未知字段名应返回错误。
func TestResetOverride_InvalidField(t *testing.T) {
	mid := uuid.New()
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) {
			cid := uuid.New()
			return &model.Model{ID: mid, CatalogID: &cid}, nil
		},
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(uuid.UUID) (*model.ModelCatalog, error) {
		return &model.ModelCatalog{}, nil
	}})

	err := svc.ResetOverride(mid, "weight")
	require.Error(t, err)
}

// TestResetOverride_ModelNotFound model 找不到时返回错误。
func TestResetOverride_ModelNotFound(t *testing.T) {
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return nil, errors.New("nope") },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	require.Error(t, svc.ResetOverride(uuid.New(), "context_length"))
}

// ===================== BindCatalog =====================

// TestBindCatalog_NoReset 仅切 catalog_id，本地价格/容量字段保留。
func TestBindCatalog_NoReset(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	cat := &model.ModelCatalog{
		ID: cid, SourceModelID: "gpt-4o", InputPricePerMillion: 2.5, OutputPricePerMillion: 10,
		ContextLength: 128000, MaxOutputTokens: 16384,
	}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(id uuid.UUID) (*model.Model, error) {
			return &model.Model{
				ID: mid, Name: "my-model", InputPrice: 1, OutputPrice: 2,
				ContextLength: 32000, MaxOutputTokens: 4096, UpstreamModelID: "old-id",
				CtxOverridden: true,
			}, nil
		},
		updateFn: func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(uuid.UUID) (*model.ModelCatalog, error) {
		return cat, nil
	}})

	got, err := svc.BindCatalog(mid, cid, false)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, captured.CatalogID)
	assert.Equal(t, cid, *captured.CatalogID)
	assert.False(t, captured.CtxOverridden, "标志位必须重置为 false（admin 显式选择新关联）")
	assert.False(t, captured.MaxOutputOverridden)
	// 其他字段保留
	assert.InDelta(t, 1.0, captured.InputPrice, 1e-6)
	assert.InDelta(t, 2.0, captured.OutputPrice, 1e-6)
	assert.Equal(t, 32000, captured.ContextLength)
	assert.Equal(t, "old-id", captured.UpstreamModelID)
}

// TestBindCatalog_WithReset reset_fields=true 时把价格/容量/upstream 全部回写为新 catalog 当前值。
func TestBindCatalog_WithReset(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	cat := &model.ModelCatalog{
		ID: cid, SourceModelID: "gpt-4o", InputPricePerMillion: 2.5, OutputPricePerMillion: 10,
		ContextLength: 128000, MaxOutputTokens: 16384,
	}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(id uuid.UUID) (*model.Model, error) {
			return &model.Model{
				ID: mid, Name: "my-model", InputPrice: 1, OutputPrice: 2,
				ContextLength: 32000, MaxOutputTokens: 4096, UpstreamModelID: "old-id",
			}, nil
		},
		updateFn: func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(uuid.UUID) (*model.ModelCatalog, error) {
		return cat, nil
	}})

	_, err := svc.BindCatalog(mid, cid, true)
	require.NoError(t, err)
	assert.Equal(t, cid, *captured.CatalogID)
	assert.InDelta(t, 2.5, captured.InputPrice, 1e-6)
	assert.InDelta(t, 10.0, captured.OutputPrice, 1e-6)
	assert.Equal(t, 128000, captured.ContextLength)
	assert.Equal(t, 16384, captured.MaxOutputTokens)
	assert.Equal(t, "gpt-4o", captured.UpstreamModelID)
}

// TestBindCatalog_AlreadySameRejectsWhenNoReset 已绑同 catalog 且不 reset 时报幂等错误。
func TestBindCatalog_AlreadySameRejectsWhenNoReset(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) {
			return &model.Model{ID: mid, CatalogID: &cid}, nil
		},
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(uuid.UUID) (*model.ModelCatalog, error) {
		return &model.ModelCatalog{ID: cid, SourceModelID: "x"}, nil
	}})

	_, err := svc.BindCatalog(mid, cid, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCatalogAlreadyBoundToSame)
}

// TestBindCatalog_AlreadySameAllowsWhenReset 已绑同 catalog 但 reset_fields=true 时允许（场景：admin 想"按目录现价回写"）
func TestBindCatalog_AlreadySameAllowsWhenReset(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	cat := &model.ModelCatalog{ID: cid, SourceModelID: "gpt-4o", InputPricePerMillion: 9, OutputPricePerMillion: 18}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) {
			return &model.Model{ID: mid, CatalogID: &cid, InputPrice: 1, OutputPrice: 2}, nil
		},
		updateFn: func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(uuid.UUID) (*model.ModelCatalog, error) {
		return cat, nil
	}})

	_, err := svc.BindCatalog(mid, cid, true)
	require.NoError(t, err)
	assert.InDelta(t, 9.0, captured.InputPrice, 1e-6)
	assert.InDelta(t, 18.0, captured.OutputPrice, 1e-6)
}

// TestBindCatalog_ModelNotFound model 不存在时返回 ErrModelNotFound。
func TestBindCatalog_ModelNotFound(t *testing.T) {
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return nil, errors.New("nope") },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{})

	_, err := svc.BindCatalog(uuid.New(), uuid.New(), false)
	assert.ErrorIs(t, err, ErrModelNotFound)
}

// TestBindCatalog_CatalogNotFound catalog lookup 返回 nil 时报错。
func TestBindCatalog_CatalogNotFound(t *testing.T) {
	mid := uuid.New()
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) {
			return &model.Model{ID: mid}, nil
		},
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDFn: func(uuid.UUID) (*model.ModelCatalog, error) {
		return nil, nil
	}})

	_, err := svc.BindCatalog(mid, uuid.New(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ===================== UnbindCatalog =====================

func TestUnbindCatalog_ClearsLink(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) {
			return &model.Model{
				ID: mid, CatalogID: &cid, CtxOverridden: true, MaxOutputOverridden: true,
				InputPrice: 7, OutputPrice: 14, ContextLength: 32000,
			}, nil
		},
		updateFn: func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	got, err := svc.UnbindCatalog(mid)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, captured.CatalogID)
	assert.False(t, captured.CtxOverridden)
	assert.False(t, captured.MaxOutputOverridden)
	// 价格/容量保留 snapshot
	assert.InDelta(t, 7.0, captured.InputPrice, 1e-6)
	assert.Equal(t, 32000, captured.ContextLength)
}

func TestUnbindCatalog_NotBound(t *testing.T) {
	mid := uuid.New()
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) {
			return &model.Model{ID: mid, CatalogID: nil}, nil
		},
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	_, err := svc.UnbindCatalog(mid)
	assert.ErrorIs(t, err, ErrCatalogNotBound)
}

func TestUnbindCatalog_ModelNotFound(t *testing.T) {
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return nil, errors.New("nope") },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	_, err := svc.UnbindCatalog(uuid.New())
	assert.ErrorIs(t, err, ErrModelNotFound)
}

// ===================== BatchBindCatalog =====================

// TestBatchBindCatalog_PartialFailure 一条失败不影响其他成功项。
func TestBatchBindCatalog_PartialFailure(t *testing.T) {
	cid := uuid.New()
	cat := &model.ModelCatalog{ID: cid, SourceModelID: "x", InputPricePerMillion: 1, OutputPricePerMillion: 2}
	mid1 := uuid.New()
	mid2 := uuid.New()
	mid3 := uuid.New()

	modelRepo := &MockModelRepo{
		getByIDsFn: func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
			m := map[uuid.UUID]*model.Model{}
			for _, id := range ids {
				if id == mid2 {
					continue
				}
				m[id] = &model.Model{ID: id}
			}
			return m, nil
		},
		updateFn: func(*model.Model) error { return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	svc.SetCatalogLookup(&stubCatalogLookup{getByIDsFn: func(ids []uuid.UUID) (map[uuid.UUID]*model.ModelCatalog, error) {
		m := map[uuid.UUID]*model.ModelCatalog{}
		for _, id := range ids {
			m[id] = cat
		}
		return m, nil
	}})

	res, err := svc.BatchBindCatalog([]BatchBindInput{
		{ModelID: mid1, CatalogID: cid, ResetFields: false},
		{ModelID: mid2, CatalogID: cid, ResetFields: false},
		{ModelID: mid3, CatalogID: cid, ResetFields: true},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Applied)
	require.Len(t, res.FailedIDs, 1)
	assert.Contains(t, res.FailedIDs, mid2.String())
}

// TestBatchBindCatalog_Empty 空列表应返回错误。
func TestBatchBindCatalog_Empty(t *testing.T) {
	svc := NewModelService(&MockModelRepo{}, &MockProviderRepo{}, nil)
	_, err := svc.BatchBindCatalog(nil)
	require.Error(t, err)
}
