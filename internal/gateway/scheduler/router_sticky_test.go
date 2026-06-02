package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stickyEntry(modelName, providerName string) *ModelRoutingEntry {
	return &ModelRoutingEntry{
		ModelName:    modelName,
		ProviderName: providerName,
	}
}

func TestTrySticky_SameNameMultiChannel(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	r := &Router{stickyStore: NewStickyStore(client)}
	ctx := context.Background()

	require.NoError(t, r.stickyStore.Set(ctx, "deepseek-v4-flash", "conv-1",
		encodeStickyBinding("deepseek-v4-flash", "deepseek"), 300e9))

	healthy := []*ModelRoutingEntry{
		stickyEntry("deepseek-v4-flash", "deepseek"),
		stickyEntry("deepseek-v4-flash", "阿里百炼"),
	}

	entry := r.trySticky("deepseek-v4-flash", "conv-1", healthy)
	require.NotNil(t, entry)
	assert.Equal(t, "deepseek", entry.ProviderName)
}

func TestTrySticky_PinnedChannelDown(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	r := &Router{stickyStore: NewStickyStore(client)}
	ctx := context.Background()

	// pin 到不健康的渠道(不在 healthyModels 里)
	require.NoError(t, r.stickyStore.Set(ctx, "deepseek-v4-flash", "conv-1",
		encodeStickyBinding("deepseek-v4-flash", "尊享版-席位1"), 300e9))

	healthy := []*ModelRoutingEntry{
		stickyEntry("deepseek-v4-flash", "deepseek"),
		stickyEntry("deepseek-v4-flash", "阿里百炼"),
	}

	assert.Nil(t, r.trySticky("deepseek-v4-flash", "conv-1", healthy))
}

func TestTrySticky_LegacyValueBackwardCompatible(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	r := &Router{stickyStore: NewStickyStore(client)}
	ctx := context.Background()

	// 旧格式:纯模型名,无渠道
	require.NoError(t, r.stickyStore.Set(ctx, "deepseek-v4-flash", "conv-1",
		"deepseek-v4-flash", 300e9))

	healthy := []*ModelRoutingEntry{
		stickyEntry("deepseek-v4-flash", "阿里百炼"),
		stickyEntry("deepseek-v4-flash", "deepseek"),
	}

	entry := r.trySticky("deepseek-v4-flash", "conv-1", healthy)
	require.NotNil(t, entry)
	assert.Equal(t, "阿里百炼", entry.ProviderName) // 匹配第一个同名健康渠道
}

func TestTrySticky_NoPin(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	r := &Router{stickyStore: NewStickyStore(client)}

	healthy := []*ModelRoutingEntry{stickyEntry("deepseek-v4-flash", "deepseek")}
	assert.Nil(t, r.trySticky("deepseek-v4-flash", "unknown-session", healthy))
}

func TestStickyBinding_EncodeParse(t *testing.T) {
	// 带渠道:model|provider roundtrip
	m, p := parseStickyBinding(encodeStickyBinding("deepseek-v4-flash", "阿里百炼"))
	assert.Equal(t, "deepseek-v4-flash", m)
	assert.Equal(t, "阿里百炼", p)

	// 单渠道组:provider 空,退化为纯模型名
	assert.Equal(t, "deepseek-v4-flash", encodeStickyBinding("deepseek-v4-flash", ""))
	m, p = parseStickyBinding("deepseek-v4-flash")
	assert.Equal(t, "deepseek-v4-flash", m)
	assert.Equal(t, "", p)
}
