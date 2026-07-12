package sched

import (
	"fmt"
	"strconv"
	"time"
	"strings"
)

const (
	baseTimeFmt   = "2006-01-02 15:04:05"
	baseTimeMsFmt = "2006-01-02 15:04:05.000"
	baseTimeTzFmt = "2006-01-02T15:04:05Z"
	ymdTimeFmt    = "2006-01-02"
	ymdSimpleTimeFmt    = "20060102"
)

// 返回unix时间戳
func Ms() int64 {
	return int64(time.Now().UTC().UnixNano() / 1e6)
}

func CurrentYmd() string {
	return time.Now().Format(ymdSimpleTimeFmt)
}

func MsRound() int64 {
	return int64(time.Now().UTC().Unix()) * 1e3
}

func Ms2Time(ms int64) time.Time {
	sec := ms / 1e3
	nsec := (ms % 1e3) * 1e6
	return time.Unix(sec, nsec).UTC()
}

func Ms2String(ms int64) string {
	return Ms2Time(ms).Format("2006-01-02 15:04:00")
}

func Time2Ms(t time.Time) int64 {
	return int64(t.UnixNano() / 1000000)
}

func Date2Ms(dateStr string) int64 {
	t, err := time.Parse("2006-01-02 15:04:05", dateStr)
	if err != nil {
		return 0
	}

	return Time2Ms(t)
}

func Ns2DateS(nsStr string) string {
	ts, _ := strconv.ParseInt(nsStr, 10, 64)
	return Ms2String(ts / 1000)
}


func Ms2YmdHms(ms int64) string {
	return Ms2Time(ms).Format(baseTimeFmt)
}

//+8?
func Ms2Ymd(ms int64) string {
	return Ms2Time(ms).Format(ymdTimeFmt)
}

// Ms2Ymd8 formats ms as yyyy-MM-dd in UTC+8 (CST).
func Ms2Ymd8(ms int64) string {
	cst := time.FixedZone("CST", 8*60*60)
	return Ms2Time(ms).In(cst).Format(ymdTimeFmt)
}

func YmdHmsMs2Ms(ts string) int64 {
	curTime, _ := time.Parse(baseTimeMsFmt, ts)
	return curTime.UnixNano() / 1e6
}

func YmdHms2Ms(ts string) int64 {
	curTime, _ := time.Parse(baseTimeFmt, ts)
	return curTime.UnixNano() / 1e6
}

func YmdTHmsZ2Ms(ts string) int64 {
	curTime, _ := time.Parse(baseTimeTzFmt, ts)
	return curTime.UnixNano() / 1e6
}

//yyyy-mm-ddThh:mm:ssZ
func Ms2YmdTHmsZ(ms int64) string {
	return Ms2Time(ms).Format(baseTimeTzFmt)
}

func parseYmdI32(t int32) (int32, int32, int32) {
	y := t / 1e4
	t = t % 1e4
	m := t / 1e2
	d := t % 1e2
	return y, m, d
}

func Ymd2Str(t int64) string {
	y := t / 1e4
	t = t % 1e4
	m := t / 1e2
	d := t % 1e2
	return fmt.Sprintf("%.4d-%.2d-%.2d 00:00:00", y, m, d)
}

func Ymd2I32(ymd string) int32 {
	tmp64, _ := strconv.ParseInt(ymd, 10, 32)
	return int32(tmp64)
}

func Ymd2Stamp(ymd string, off int) int64 {
	ts := fmt.Sprintf("%s-%s-%s 00:00:00", ymd[0:4], ymd[4:6], ymd[6:8])
	curTime, _ := time.Parse(baseTimeFmt, ts)
	curTime = curTime.Add(-time.Duration(off * 60 * 60 * 1e9))
	return curTime.UnixNano() / 1e6
}

func YmdSub(t0, t1 int64) float32 {
	t0Time, _ := time.Parse(baseTimeFmt, Ymd2Str(t0))
	t1Time, _ := time.Parse(baseTimeFmt, Ymd2Str(t1))
	//fmt.Printf("time %+v, %+v\n", Ymd2Str(t0), t1Time)
	return float32(t0Time.Sub(t1Time).Hours()) / (365 * 24)
}

//周几
func Ms2WeekDay(ms int64) int {
	t := Ms2Time(ms)
	return int(t.Weekday())
}

func Ms2yearWeek(ms int64) int {
	t := Ms2Time(ms)
	yearDay := t.YearDay()
	yearFirstDay := t.AddDate(0, 0, -yearDay+1)
	firstDayInWeek := int(yearFirstDay.Weekday())

	firstWeekDays := 1
	if firstDayInWeek != 0 {
		firstWeekDays = 7 - firstDayInWeek + 1
	}
	var week int
	if yearDay <= firstWeekDays {
		week = 1
	} else {
		week = (yearDay-firstWeekDays)/7 + 2
	}
	return week
}

func PassDays(now, base int64) int {
	tn := Ms2Time(now)
	tb := Ms2Time(base)
	tn = tn.UTC().Truncate(24 * time.Hour)
	tb = tb.UTC().Truncate(24 * time.Hour)
	return int(tn.Sub(tb).Hours() / 24)
}

func PassWeeks(now, base int64) int {
	tn := Ms2Time(now)
	tb := Ms2Time(base)
	tn = tn.UTC().Truncate(7 * 24 * time.Hour)
	tb = tb.UTC().Truncate(7 * 24 * time.Hour)
	return int(tn.Sub(tb).Hours() / (7 * 24))
}

func NowToYmd(offset string) string {
	now := time.Now()

	if offset != "" {
		off, _ := time.ParseDuration(offset)
		now = now.Add(off)
		//fmt.Println("offset", offset, "off", off, "now", now)
	} else {
		//fmt.Println("offset", offset, "now", now)
	}
	ymd := fmt.Sprintf("%04d%02d%02d", now.Year(), now.Month(), now.Day())
	return ymd
}
func NowToHms() string {
	now := time.Now()
	hms := fmt.Sprintf("%02d%02d%02d", now.Hour(), now.Minute(), now.Second())
	return hms
}
func NowToHms3() string {
	now := time.Now()
	hms := fmt.Sprintf("%02d%02d%02d%03d", now.Hour(), now.Minute(), now.Second(), now.Nanosecond()/1000000)
	return hms
}

func NowToYmdHms3() string {
	now := time.Now()
	ymdhms3 := fmt.Sprintf("%04d%02d%02d%02d%02d%02d%03d", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond()/1000000)
	return ymdhms3
}

func YmdHms3ToTime(ts string) string {
	if ts == "" {
		return "0000-00-00 00:00:00"
	}
	timeStr := fmt.Sprintf("%04s-%02s-%02s %02s:%02s:%02s", ts[0:4], ts[4:6], ts[6:8], ts[8:10], ts[10:12], ts[12:14])
	return timeStr
}

func Ymd2Date(ts string) string {
	timeStr := fmt.Sprintf("%04s-%02s-%02s", ts[0:4], ts[4:6], ts[6:8])
	return timeStr
}


// 预定义数字字符
var digits = [10]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}

func formatTime2Builder(t time.Time, b *strings.Builder) {
	year, month, day := t.Date()
	hour, min, sec := t.Clock()
	msec := t.Nanosecond() / 1000000
	
	// 年份（两位数）
	y := year % 100
	b.WriteByte(digits[y/10])
	b.WriteByte(digits[y%10])
	b.WriteByte('-')
	
	// 月份
	b.WriteByte(digits[month/10])
	b.WriteByte(digits[month%10])
	b.WriteByte('-')
	
	// 日期
	b.WriteByte(digits[day/10])
	b.WriteByte(digits[day%10])
	b.WriteByte(' ')
	
	// 小时
	b.WriteByte(digits[hour/10])
	b.WriteByte(digits[hour%10])
	b.WriteByte(':')
	
	// 分钟
	b.WriteByte(digits[min/10])
	b.WriteByte(digits[min%10])
	b.WriteByte(':')
	
	// 秒
	b.WriteByte(digits[sec/10])
	b.WriteByte(digits[sec%10])
	b.WriteByte('.')
	
	// 毫秒
	b.WriteByte(digits[msec/100])
	b.WriteByte(digits[(msec/10)%10])
	b.WriteByte(digits[msec%10])
}



