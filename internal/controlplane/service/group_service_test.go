package service

import (
	"errors"
	"testing"
)

func TestAssertSameModelType(t *testing.T) {
	tests := []struct {
		name    string
		types   []string
		wantErr bool
	}{
		{
			name:    "allows identical model types",
			types:   []string{"embedding", "embedding"},
			wantErr: false,
		},
		{
			name:    "rejects mixed model types",
			types:   []string{"embedding", "rerank"},
			wantErr: true,
		},
		{
			name:    "rejects empty model type",
			types:   []string{"embedding", ""},
			wantErr: true,
		},
		{
			name:    "allows no model types",
			types:   nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := assertSameModelType(tt.types)
			if tt.wantErr && !errors.Is(err, ErrGroupModelTypeMismatch) {
				t.Fatalf("expected ErrGroupModelTypeMismatch, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}
