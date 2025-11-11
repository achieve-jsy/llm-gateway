package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGroupNameCache_IsGroupName(t *testing.T) {
	gc := NewGroupNameCache("")

	assert.False(t, gc.IsGroupName("gpt-4"))
	assert.False(t, gc.IsGroupName("claude-group"))

	gc.mu.Lock()
	gc.names["gpt-4"] = struct{}{}
	gc.names["claude-group"] = struct{}{}
	gc.mu.Unlock()

	assert.True(t, gc.IsGroupName("gpt-4"))
	assert.True(t, gc.IsGroupName("claude-group"))
	assert.False(t, gc.IsGroupName("unknown"))
}

func TestGroupNameCache_Empty(t *testing.T) {
	gc := NewGroupNameCache("")
	assert.False(t, gc.IsGroupName(""))
	assert.False(t, gc.IsGroupName("any"))
}
