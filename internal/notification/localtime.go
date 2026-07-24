package notification

import "time"

// displayTimezone 是面向用户的通知/邮件统一使用的业务时区。
// 收件人不应该拿到 UTC 或服务器本地时间后再自己换算。
const displayTimezone = "Asia/Shanghai"

var displayLocation = func() *time.Location {
	location, err := time.LoadLocation(displayTimezone)
	if err != nil {
		// 容器里没有 tzdata 时退化为固定 +08:00，保证展示不退化成 UTC。
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}()

// FormatLocalTime 以业务时区、分钟精度渲染时间，供通知正文与邮件正文统一使用。
func FormatLocalTime(t time.Time) string {
	return t.In(displayLocation).Format("2006-01-02 15:04")
}
