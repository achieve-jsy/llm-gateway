package service

import (
	"os"
	"time"
)

// 归档/结算统一时区（docs/trace-archive-final-design.md §10，env ARCHIVE_TZ，默认 Asia/Shanghai）。
const defaultArchiveTZ = "Asia/Shanghai"

// archiveTZ 返回结算/归档统一时区；优先读 ARCHIVE_TZ 环境变量，缺省 Asia/Shanghai。
func archiveTZ() string {
	if tz := os.Getenv("ARCHIVE_TZ"); tz != "" {
		return tz
	}
	return defaultArchiveTZ
}

// BusinessLocation 返回业务时区（Asia/Shanghai / ARCHIVE_TZ），无法加载时回退 UTC。
//
// call_logs.ts 以 UTC 存储，而"某一天"在业务语义上是本地日界线。若天边界用
// UTC 推算（time.Date(..., day.Location()) 在进程为 UTC 时、或原生 date(ts)），
// 北京时间 0–8 点的请求会被算进前一天，usage_daily.stat_date 也随之偏移。
func BusinessLocation() *time.Location {
	if loc, err := time.LoadLocation(archiveTZ()); err == nil {
		return loc
	}
	return time.UTC
}

// localMidnight 返回业务时区下"该时刻所在本地日期"的零点。
func localMidnight(t time.Time) time.Time {
	loc := BusinessLocation()
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// TSWholeDayRange 返回覆盖"本地日期 day 整天"的半开区间 [start, end)。
// day 只取日历部分，忽略其时刻与 Location。
func TSWholeDayRange(day time.Time) (time.Time, time.Time) {
	l := localMidnight(day)
	loc := BusinessLocation()
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc),
		time.Date(l.Year(), l.Month(), l.Day()+1, 0, 0, 0, 0, loc)
}

// TSWholeDays 返回覆盖"本地日期 from..to（含两端）"的半开区间 [start, end)。
func TSWholeDays(from, to time.Time) (time.Time, time.Time) {
	fs := localMidnight(from)
	te := localMidnight(to)
	loc := BusinessLocation()
	return time.Date(fs.Year(), fs.Month(), fs.Day(), 0, 0, 0, 0, loc),
		time.Date(te.Year(), te.Month(), te.Day()+1, 0, 0, 0, 0, loc)
}

// ParseDayWindow 把前端传入的 YYYY-MM-DD 区间 [start, end] 翻译成半开时间戳区间。
// 与历史 ts::date BETWEEN start AND end 等价（两端按整天），但边界按业务时区取。
func ParseDayWindow(startStr, endStr string) (time.Time, time.Time) {
	loc := BusinessLocation()
	start, errS := time.ParseInLocation("2006-01-02", startStr, loc)
	end, errE := time.ParseInLocation("2006-01-02", endStr, loc)
	if errS != nil || errE != nil {
		start, _ = time.Parse("2006-01-02", startStr)
		end, _ = time.Parse("2006-01-02", endStr)
	}
	return TSWholeDays(start, end)
}

// TSKeySQL 返回"按业务时区切天"的 SQL 表达式（timestamptz，本地零点对应的绝对时刻）。
func TSKeySQL(table string) string {
	col := "ts"
	if table != "" {
		col = table + ".ts"
	}
	tz := BusinessLocation().String()
	return "((date_trunc('day', " + col + " AT TIME ZONE '" + tz + "')) AT TIME ZONE '" + tz + "')"
}
