package configsync

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/redis/go-redis/v9"
)

// 组名随 provider snapshot 一起落 Redis，供网关冷启动预热组名缓存，
// 因此 JSON 字段名必须是 group_names 且往返保真。
func TestProviderSnapshotGroupNamesJSONRoundTrip(t *testing.T) {
	snapshot := ProviderSnapshot{
		Version:    42,
		UpdatedAt:  time.Date(2026, 10, 16, 14, 33, 4, 0, time.UTC),
		Entries:    []gatewaymodel.ProviderEntry{{ID: "p1", Name: "provider-1", Enabled: true}},
		GroupNames: []string{"gpt-5.6-luna", "gpt-5.6-terra"},
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(payload), `"group_names"`) {
		t.Fatalf("期望 JSON 中出现 group_names 字段，实际 payload: %s", payload)
	}

	var decoded ProviderSnapshot
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if len(decoded.GroupNames) != len(snapshot.GroupNames) {
		t.Fatalf("组名往返后数量不一致: 期望 %d，实际 %d (%#v)",
			len(snapshot.GroupNames), len(decoded.GroupNames), decoded.GroupNames)
	}
	for i := range snapshot.GroupNames {
		if decoded.GroupNames[i] != snapshot.GroupNames[i] {
			t.Fatalf("组名往返后第 %d 项不一致: 期望 %q，实际 %q",
				i, snapshot.GroupNames[i], decoded.GroupNames[i])
		}
	}
}

// 旧控制面写入的 payload 没有 group_names 字段（灰度/回滚共存场景），
// 反序列化必须成功，且 GroupNames 保持 nil（不能被填成空字符串等脏值）。
func TestProviderSnapshotLegacyPayloadWithoutGroupNames(t *testing.T) {
	legacy := []byte(`{"version":1,"updated_at":"2026-10-16T14:32:33Z","entries":[{"id":"p1","name":"provider-1","enabled":true}]}`)

	var snapshot ProviderSnapshot
	if err := json.Unmarshal(legacy, &snapshot); err != nil {
		t.Fatalf("旧 payload 反序列化失败: %v", err)
	}
	if snapshot.GroupNames != nil {
		t.Fatalf("旧 payload 的 GroupNames 期望为 nil，实际 %#v", snapshot.GroupNames)
	}
	if snapshot.Version != 1 || len(snapshot.Entries) != 1 {
		t.Fatalf("旧 payload 其余字段解析异常: version=%d entries=%d", snapshot.Version, len(snapshot.Entries))
	}
}

// Redis 读写路径往返：写入的组名能被原样读回；旧快照（无 group_names）读回后为 nil。
func TestProviderSnapshotGroupNamesRedisRoundTrip(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()
	ctx := context.Background()

	names := []string{"gpt-5.6-luna", "gpt-5.6-terra"}
	if err := SaveProviderSnapshot(ctx, rdb, &ProviderSnapshot{
		Version:    7,
		UpdatedAt:  time.Date(2026, 10, 16, 14, 33, 4, 0, time.UTC),
		GroupNames: names,
	}); err != nil {
		t.Fatalf("写入快照失败: %v", err)
	}

	loaded, err := LoadProviderSnapshot(ctx, rdb)
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if len(loaded.GroupNames) != len(names) {
		t.Fatalf("读回组名数量不一致: 期望 %d，实际 %d (%#v)", len(names), len(loaded.GroupNames), loaded.GroupNames)
	}
	for i := range names {
		if loaded.GroupNames[i] != names[i] {
			t.Fatalf("读回组名第 %d 项不一致: 期望 %q，实际 %q", i, names[i], loaded.GroupNames[i])
		}
	}

	// 覆盖为旧控制面风格的 payload：必须能读，且 GroupNames 为 nil。
	legacy := `{"version":8,"updated_at":"2026-10-16T14:32:33Z","entries":[]}`
	if err := rdb.Set(ctx, ProviderSnapshotKey, legacy, 0).Err(); err != nil {
		t.Fatalf("写入旧 payload 失败: %v", err)
	}
	legacySnapshot, err := LoadProviderSnapshot(ctx, rdb)
	if err != nil {
		t.Fatalf("读取旧 payload 失败: %v", err)
	}
	if legacySnapshot.GroupNames != nil {
		t.Fatalf("旧 payload 的 GroupNames 期望为 nil，实际 %#v", legacySnapshot.GroupNames)
	}
}
