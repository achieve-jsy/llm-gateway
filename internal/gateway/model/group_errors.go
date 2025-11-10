package model

import "errors"

var (
	ErrGroupNotFound        = errors.New("模型组不存在")
	ErrNoAvailableModel     = errors.New("该模型组当前无可用模型")
	ErrNoOtherModelAvailable = errors.New("该模型组无其他可用模型")
)
