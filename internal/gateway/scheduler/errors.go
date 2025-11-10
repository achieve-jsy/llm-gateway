package scheduler

import "errors"

var (
	ErrGroupNotFound     = errors.New("模型组不存在")
	ErrNoAvailableModel  = errors.New("该模型组当前无可用模型")
	ErrNoOtherModel      = errors.New("该模型组无其他可用模型")
	ErrConfigFetchFailed = errors.New("配置同步失败")
	ErrInvalidConfig     = errors.New("配置数据不合法")
)
