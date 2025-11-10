package model

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSetDispatchContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	dc := &DispatchContext{
		GroupName:     "gpt-group",
		RoutedModelID: "model-001",
	}
	SetDispatchContext(c, dc)

	got := GetDispatchContext(c)
	assert.NotNil(t, got)
	assert.Equal(t, "gpt-group", got.GroupName)
	assert.Equal(t, "model-001", got.RoutedModelID)
	assert.False(t, got.FailoverAttempted)
}

func TestGetDispatchContext_NotSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	got := GetDispatchContext(c)
	assert.Nil(t, got)
}

func TestDispatchContext_FailoverAttempted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	dc := &DispatchContext{
		GroupName:         "claude-group",
		RoutedModelID:     "model-002",
		FailoverAttempted: true,
	}
	SetDispatchContext(c, dc)

	got := GetDispatchContext(c)
	assert.True(t, got.FailoverAttempted)
}
