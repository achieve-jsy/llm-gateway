package core

import (
	"errors"
	"fmt"
)

// ContentPartError 描述单个内容片段解析/序列化失败：携带片段下标与客户端原始 type，
// 让 HTTP 层能返回稳定的 code + param（content[N].type），而不是笼统的 400。
type ContentPartError struct {
	// Index 是片段在 content 数组中的下标；未知时为 -1。
	Index    int
	PartType string // 客户端原始 type，如 "pdf"
	Field    string // 缺失/非法的具体字段，如 "video_url.url"；未知为空
}

func (e *ContentPartError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s is missing or invalid", e.Field)
	}
	return fmt.Sprintf("unsupported content part type %q", e.PartType)
}

// NewContentPartError 构造片段错误（Index 由 wrapContentPartError 或调用方补齐）。
func NewContentPartError(partType, field string) *ContentPartError {
	return &ContentPartError{Index: -1, PartType: partType, Field: field}
}

// wrapContentPartError 补齐片段下标，并保持既有 "part N: ..." 文案不变。
func wrapContentPartError(index int, err error) error {
	var cpe *ContentPartError
	if errors.As(err, &cpe) {
		cpe.Index = index
	}
	return fmt.Errorf("part %d: %w", index, err)
}
