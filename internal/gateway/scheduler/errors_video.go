package scheduler

import "errors"

// ErrNoVideoCapableModel 组内存活的成员都未声明视频输入能力。
// 与 ErrNoAvailableModel 区分，便于上层返回 video_input_not_supported（400）
// 而不是"该模型组当前无可用模型"（503）。
var ErrNoVideoCapableModel = errors.New("模型组内没有声明支持视频输入的成员")
