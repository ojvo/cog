package sched

import (
	"errors"
	"time"
)

// BeginningOfMinute 返回当前分钟的开始时间。
func (d Date) BeginningOfMinute() time.Time {
	y, m, day := d.Date()
	return time.Date(y, m, day, d.Hour(), d.Minute(), 0, 0, d.Location())
}

// BeginningOfHour 返回当前小时的开始时间。
func (d Date) BeginningOfHour() time.Time {
	y, m, day := d.Date()
	return time.Date(y, m, day, d.Hour(), 0, 0, 0, d.Location())
}

// BeginningOfDay 返回当天的 00:00:00。
func (d Date) BeginningOfDay() time.Time {
	y, m, day := d.Date()
	return time.Date(y, m, day, 0, 0, 0, 0, d.Location())
}

// BeginningOfWeek 返回本周的第一天（周日或自定义 WeekStartDay）。
func (d Date) BeginningOfWeek() time.Time {
	t := d.BeginningOfDay()
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t.AddDate(0, 0, -(wd - 1))
}

// BeginningOfMonth 返回本月第一天 00:00:00。
func (d Date) BeginningOfMonth() time.Time {
	y, m, _ := d.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, d.Location())
}

// BeginningOfQuarter 返回本季度第一天 00:00:00。
func (d Date) BeginningOfQuarter() time.Time {
	month := d.BeginningOfMonth()
	offset := (int(month.Month()) - 1) % 3
	return month.AddDate(0, -offset, 0)
}

// BeginningOfYear 返回本年第一天 00:00:00。
func (d Date) BeginningOfYear() time.Time {
	y, _, _ := d.Date()
	return time.Date(y, 1, 1, 0, 0, 0, 0, d.Location())
}

// EndOfMinute 返回当前分钟的最后一纳秒。
func (d Date) EndOfMinute() time.Time {
	return d.BeginningOfMinute().Add(time.Minute - time.Nanosecond)
}

// EndOfHour 返回当前小时的最后一纳秒。
func (d Date) EndOfHour() time.Time {
	return d.BeginningOfHour().Add(time.Hour - time.Nanosecond)
}

// EndOfDay 返回当天的 23:59:59.999999999。
func (d Date) EndOfDay() time.Time {
	y, m, day := d.Date()
	return time.Date(y, m, day, 23, 59, 59, int(time.Second-time.Nanosecond), d.Location())
}

// EndOfWeek 返回本周最后一纳秒（周六 23:59:59.999999999）。
func (d Date) EndOfWeek() time.Time {
	return d.BeginningOfWeek().AddDate(0, 0, 7).Add(-time.Nanosecond)
}

// EndOfMonth 返回本月最后一纳秒。
func (d Date) EndOfMonth() time.Time {
	return d.BeginningOfMonth().AddDate(0, 1, 0).Add(-time.Nanosecond)
}

// EndOfQuarter 返回本季度最后一纳秒。
func (d Date) EndOfQuarter() time.Time {
	return d.BeginningOfQuarter().AddDate(0, 3, 0).Add(-time.Nanosecond)
}

// EndOfYear 返回本年最后一纳秒。
func (d Date) EndOfYear() time.Time {
	return d.BeginningOfYear().AddDate(1, 0, 0).Add(-time.Nanosecond)
}

// Monday 返回本周一 00:00:00。
func (d Date) Monday() time.Time {
	return d.BeginningOfWeek()
}

// Sunday 返回本周日 00:00:00。
func (d Date) Sunday() time.Time {
	t := d.BeginningOfDay()
	wd := int(t.Weekday())
	return t.AddDate(0, 0, (7-wd)%7)
}

// EndOfSunday 返回本周日 23:59:59.999999999。
func (d Date) EndOfSunday() time.Time {
	return WithTime(d.Sunday()).EndOfDay()
}

// defaultTimeFormats 是 Parse 尝试的时间格式列表，涵盖常用 ISO/RFC/数字格式。
var defaultTimeFormats = []string{
	"2006", "2006-1", "2006-1-2", "2006-1-2 15", "2006-1-2 15:4", "2006-1-2 15:4:5",
	"1-2", "15:4:5", "15:4", "15",
	"2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02T15:04:05-07:00",
	"2006.1.2", "2006.1.2 15:04:05", "2006.01.02", "2006.01.02 15:04:05",
	"1/2/2006", "1/2/2006 15:4:5", "2006/01/02", "2006/01/02 15:04:05",
	time.ANSIC, time.UnixDate, time.RubyDate, time.RFC822, time.RFC822Z,
	time.RFC850, time.RFC1123, time.RFC1123Z, time.RFC3339, time.RFC3339Nano,
	time.Kitchen, time.Stamp, time.StampMilli, time.StampMicro, time.StampNano,
}

// Parse 尝试用多种常见格式将 strs 中的字符串解析为时间。
// 支持仅时间字符串（如 "15:04:05"）——缺失日期部分填当前日期。
func (d Date) Parse(strs ...string) (time.Time, error) {
	if len(strs) == 0 {
		return time.Time{}, errors.New("no strings to parse")
	}

	var t time.Time
	for _, str := range strs {
		parsed := false
		for _, layout := range defaultTimeFormats {
			var err error
			t, err = time.ParseInLocation(layout, str, d.Location())
			if err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			return time.Time{}, errors.New("can't parse string as time: " + str)
		}
	}
	return t, nil
}

// MustParse 与 Parse 相同，但解析失败时 panic。
func (d Date) MustParse(strs ...string) time.Time {
	t, err := d.Parse(strs...)
	if err != nil {
		panic(err)
	}
	return t
}

// Between 检查当前时间是否在 time1 和 time2 之间（含边界）。
// time1/time2 可以是类似 "15:04"（仅时间）或 "2006-01-02 15:04:05" 的格式。
func (d Date) Between(time1, time2 string) bool {
	t1, err := d.Parse(time1)
	if err != nil {
		return false
	}
	t2, err := d.Parse(time2)
	if err != nil {
		return false
	}
	now := d.Time
	return (now.Equal(t1) || now.After(t1)) && (now.Equal(t2) || now.Before(t2))
}
