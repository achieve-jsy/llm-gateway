package service

// NotificationTypeMetadata 通知类型元数据
type NotificationTypeMetadata struct {
	Name             string   `json:"name"`
	DisplayName      string   `json:"displayName"`
	Description      string   `json:"description"`
	DefaultVariables []string `json:"defaultVariables,omitempty"`
	Channels         []string `json:"channels,omitempty"`
}

// NotificationChannelMetadata 通知渠道元数据
type NotificationChannelMetadata struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// NotificationMetadataResponse 元数据响应
type NotificationMetadataResponse struct {
	Types    []NotificationTypeMetadata    `json:"types"`
	Channels []NotificationChannelMetadata `json:"channels"`
}

// GetNotificationMetadata 返回所有通知类型和渠道的元数据
func GetNotificationMetadata() NotificationMetadataResponse {
	return NotificationMetadataResponse{
		Types:    getNotificationTypes(),
		Channels: getNotificationChannels(),
	}
}

func getNotificationTypes() []NotificationTypeMetadata {
	regs := GetNotificationTypeRegistry()
	out := make([]NotificationTypeMetadata, 0, len(regs))
	for _, t := range regs {
		out = append(out, NotificationTypeMetadata{
			Name:             t.Name,
			DisplayName:      t.DisplayName,
			Description:      t.Description,
			DefaultVariables: t.DefaultVariables,
			Channels:         t.Channels,
		})
	}
	return out
}

func getNotificationChannels() []NotificationChannelMetadata {
	out := make([]NotificationChannelMetadata, 0, 3)
	for _, ch := range allRegistryChannels() {
		out = append(out, NotificationChannelMetadata{
			Name:        ch,
			DisplayName: channelDisplayName(ch),
		})
	}
	return out
}

func channelDisplayName(ch string) string {
	switch ch {
	case ChannelInapp:
		return "站内信"
	case ChannelEmail:
		return "邮件"
	case ChannelWebhook:
		return "Webhook"
	}
	return ch
}
