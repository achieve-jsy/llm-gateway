package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestUpdateModelInput_AllowsEmbeddingVLModelTypeBinding(t *testing.T) {
	input := service.UpdateModelInput{ModelType: "embedding_vl"}

	require.NoError(t, binding.Validator.ValidateStruct(input))
}
