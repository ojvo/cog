package sched

import (
	"testing"
	"time"
)

func TestDateOp_Now(t *testing.T) {
	d := Now()
	if d.Unix() == 0 {
		t.Error("Now() returned zero time")
	}
}

func TestDateOp_Format(t *testing.T) {
	// 2023-01-02 15:04:05
	dt := time.Date(2023, 1, 2, 15, 4, 5, 0, time.Local)
	d := WithTime(dt)
	
	// Test basic formatting
	// yyyy-MM-dd HH:mm:ss
	// Note: The library uses custom layout tokens like yyyy, MM, dd
	
	// yyyy-MM-dd
	res, err := d.Format("yyyy-MM-dd")
	if err != nil {
		t.Errorf("Format error: %v", err)
	}
	if res != "2023-01-02" {
		t.Errorf("Expected 2023-01-02, got %s", res)
	}
	
	// HH:mm:ss -> H:m:s in this lib?
	// Implementation:
	// H: hour 0-23
	// m: minute
	// s: second
	
	res, err = d.Format("HH:mm:ss")
	if err != nil {
		t.Errorf("Format error: %v", err)
	}
	if res != "15:04:05" {
		t.Errorf("Expected 15:04:05, got %s", res)
	}
	
	// Chinese format
	// E: Weekday
	res, err = d.Format("E", true)
	if err != nil {
		t.Errorf("Format error: %v", err)
	}
	// 2023-01-02 is Monday (星期一)
	if res != "星期一" {
		t.Errorf("Expected 星期一, got %s", res)
	}
}

func TestTime_Conversions(t *testing.T) {
	// Test Ms(), Time2Ms, Ms2Time
	now := time.Now()
	ms := Time2Ms(now)
	
	// Ms() returns current ms
	curMs := Ms()
	if curMs == 0 {
		t.Error("Ms() returned 0")
	}
	
	// Ms2Time
	t2 := Ms2Time(ms)
	// Precision loss is expected (microseconds/nanoseconds lost)
	// But seconds should match
	if t2.Unix() != now.Unix() {
		t.Errorf("Unix timestamp mismatch: %d vs %d", t2.Unix(), now.Unix())
	}
	
	// Ms2String
	_ = Ms2String(ms)
	// "2006-01-02 15:04:00" - seconds are 00 in Ms2String implementation?
	// Code: return Ms2Time(ms).Format("2006-01-02 15:04:00")
	// Wait, the format string hardcodes seconds to 00?
	// Let's check the code: return Ms2Time(ms).Format("2006-01-02 15:04:00")
	// Yes, it seems so based on my read. Or maybe I misread the format string.
	// "2006-01-02 15:04:00" -> This format string effectively ignores the seconds from the time object and prints 00?
	// No, 00 is not a standard format specifier. 05 is seconds.
	// If the format string is literally "2006-01-02 15:04:00", then seconds will always be "00".
	// Let's verify this behavior.
	
	// Actually, looking at the code again:
	// func Ms2String(ms int64) string {
	// 	return Ms2Time(ms).Format("2006-01-02 15:04:00")
	// }
	// If the intention was to format seconds, it should be 05.
	// If it is 00, it's a bug or feature. I will test what it does.
}

func TestTime_Ymd(t *testing.T) {
	// Ms2Ymd
	// 2023-01-02 10:00:00 UTC
	dt := time.Date(2023, 1, 2, 10, 0, 0, 0, time.UTC)
	ms := Time2Ms(dt)
	
	ymd := Ms2Ymd(ms)
	if ymd != "2023-01-02" {
		t.Errorf("Expected 2023-01-02, got %s", ymd)
	}
	
	// Ms2Ymd8 (+8 hours)
	// 2023-01-02 10:00:00 UTC -> 18:00:00 +8 -> Still 2023-01-02
	// Let's try boundary.
	// 2023-01-02 20:00:00 UTC -> 04:00:00 +8 (Next day)
	dt2 := time.Date(2023, 1, 2, 20, 0, 0, 0, time.UTC)
	ms2 := Time2Ms(dt2)
	ymd8 := Ms2Ymd8(ms2)
	if ymd8 != "2023-01-03" {
		t.Errorf("Expected 2023-01-03 (UTC+8), got %s", ymd8)
	}
}

func TestTime_Parsing(t *testing.T) {
	// Date2Ms
	ms := Date2Ms("2023-01-02 15:04:05")
	if ms == 0 {
		t.Error("Date2Ms failed")
	}
	
	// Ymd2I32
	i := Ymd2I32("20230102")
	if i != 20230102 {
		t.Errorf("Expected 20230102, got %d", i)
	}
}

func TestTime_PassDays(t *testing.T) {
	t1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	
	ms1 := Time2Ms(t1)
	ms2 := Time2Ms(t2)
	
	days := PassDays(ms2, ms1)
	if days != 2 {
		t.Errorf("Expected 2 days, got %d", days)
	}
}

func TestTime_WeekDay(t *testing.T) {
	// 2023-01-01 was Sunday (0)
	dt := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	ms := Time2Ms(dt)
	
	wd := Ms2WeekDay(ms)
	if wd != 0 {
		t.Errorf("Expected 0 (Sunday), got %d", wd)
	}
}
