package service_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// model_type 的合法值集合（契约）：binding 白名单与 DB CHECK 约束必须一致。
// 新增类型时，本列表、6 处 binding 白名单与最新一个
// atlas-migrations/*_add_model_type*.sql 的 CHECK 数组要同步更新。
var expectedModelTypes = []string{"chat", "image", "embedding", "embedding_vl", "rerank", "rerank_vl", "video"}

// 明确非法的值：不在白名单内（含大小写不符、旧命名、空串等边界）。
var invalidModelTypes = []string{"IMAGE", "Image", "images", "image_generation", "audio", "completion", "foo", " chat"}

// TestModelTypeImage_BindingAcceptsImage 覆盖 6 处 binding 白名单：
// 每处都必须接受 image，且原有 6 个值不得回归。
func TestModelTypeImage_BindingAcceptsImage(t *testing.T) {
	providerID := uuid.NewString()

	builders := map[string]func(mt string) any{
		"CreateModelInput": func(mt string) any {
			return service.CreateModelInput{Name: "gpt-image-1", ProviderID: providerID, ModelType: mt}
		},
		"UpdateModelInput(omitempty)": func(mt string) any {
			return service.UpdateModelInput{ModelType: mt}
		},
		"BatchCreateModelsInput": func(mt string) any {
			return service.BatchCreateModelsInput{Name: "gpt-image-1", ProviderIDs: []string{providerID}, ModelType: mt}
		},
		"CreateGroupRequest": func(mt string) any {
			return service.CreateGroupRequest{Name: "image-group", ModelType: mt, Strategy: "round_robin"}
		},
		"UpdateGroupRequest(omitempty)": func(mt string) any {
			return service.UpdateGroupRequest{ModelType: &mt}
		},
		"QuickOnboardModelInput(omitempty)": func(mt string) any {
			return service.QuickOnboardModelInput{Name: "gpt-image-1", ModelType: mt}
		},
	}

	// map 遍历顺序随机，先固定顺序便于失败定位。
	names := make([]string, 0, len(builders))
	for name := range builders {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		build := builders[name]
		for _, mt := range expectedModelTypes {
			t.Run(name+"/"+mt, func(t *testing.T) {
				require.NoError(t, binding.Validator.ValidateStruct(build(mt)),
					"%s 应接受 model_type=%q", name, mt)
			})
		}
	}
}

// TestModelTypeImage_BindingRejectsUnknown 非法值仍被拒（image 的加入不得放宽校验）。
func TestModelTypeImage_BindingRejectsUnknown(t *testing.T) {
	providerID := uuid.NewString()

	builders := map[string]func(mt string) any{
		"CreateModelInput": func(mt string) any {
			return service.CreateModelInput{Name: "gpt-image-1", ProviderID: providerID, ModelType: mt}
		},
		"UpdateModelInput": func(mt string) any {
			return service.UpdateModelInput{ModelType: mt}
		},
		"BatchCreateModelsInput": func(mt string) any {
			return service.BatchCreateModelsInput{Name: "gpt-image-1", ProviderIDs: []string{providerID}, ModelType: mt}
		},
		"CreateGroupRequest": func(mt string) any {
			return service.CreateGroupRequest{Name: "image-group", ModelType: mt, Strategy: "round_robin"}
		},
		"UpdateGroupRequest": func(mt string) any {
			return service.UpdateGroupRequest{ModelType: &mt}
		},
		"QuickOnboardModelInput": func(mt string) any {
			return service.QuickOnboardModelInput{Name: "gpt-image-1", ModelType: mt}
		},
	}

	names := make([]string, 0, len(builders))
	for name := range builders {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		build := builders[name]
		for _, mt := range invalidModelTypes {
			t.Run(name+"/"+mt, func(t *testing.T) {
				err := binding.Validator.ValidateStruct(build(mt))
				require.Error(t, err, "%s 应拒绝 model_type=%q", name, mt)
			})
		}
	}
}

// TestModelTypeImage_UpdatePathOmitEmpty 显式覆盖 update 路径的 omitempty 语义：
// 不传 model_type 合法（部分更新），传 image 合法，传非法值被拒。
func TestModelTypeImage_UpdatePathOmitEmpty(t *testing.T) {
	t.Run("未传 model_type 时校验通过", func(t *testing.T) {
		require.NoError(t, binding.Validator.ValidateStruct(service.UpdateModelInput{Name: "gpt-image-1"}))
	})

	t.Run("传 image 时校验通过", func(t *testing.T) {
		mt := "image"
		require.NoError(t, binding.Validator.ValidateStruct(service.UpdateModelInput{ModelType: mt}))
	})

	t.Run("传非法值时校验失败", func(t *testing.T) {
		require.Error(t, binding.Validator.ValidateStruct(service.UpdateModelInput{ModelType: "image_generation"}))
	})

	t.Run("组更新路径：传 image 时校验通过", func(t *testing.T) {
		mt := "image"
		require.NoError(t, binding.Validator.ValidateStruct(service.UpdateGroupRequest{ModelType: &mt}))
	})
}

// TestModelTypeImage_MigrationConstraintMatchesBinding 防漂移：
// 最新一个定义 chk_model_groups_model_type 的迁移，其 CHECK 数组必须与白名单契约完全一致。
// （models.model_type 无 CHECK 约束，见 atlas-migrations/20260609082623_baseline.sql:391。）
func TestModelTypeImage_MigrationConstraintMatchesBinding(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "atlas-migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("跳过：无法读取迁移目录（%s）：%v", dir, err)
	}

	const constraint = `ADD CONSTRAINT "chk_model_groups_model_type"`
	var defining []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		if strings.Contains(string(raw), constraint) {
			defining = append(defining, e.Name())
		}
	}

	require.NotEmpty(t, defining, "应至少有一个迁移定义 %s", "chk_model_groups_model_type")
	sort.Strings(defining) // Atlas 按文件名时间戳线性应用，最后一个即当前生效定义
	latest := defining[len(defining)-1]
	raw, err := os.ReadFile(filepath.Join(dir, latest))
	require.NoError(t, err)

	re := regexp.MustCompile(`'([a-z_]+)'::character varying`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	got := make([]string, 0, len(matches))
	for _, m := range matches {
		got = append(got, m[1])
	}
	sort.Strings(got)

	want := append([]string(nil), expectedModelTypes...)
	sort.Strings(want)

	assert.Equal(t, want, got,
		"迁移 %s 的 CHECK 值集合必须与 binding 白名单一致（新增类型时两者一起改）", latest)
	assert.Contains(t, got, "image", "迁移 %s 必须允许 image", latest)
}
