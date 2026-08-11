package sched

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// --- Range / Field / Bits / All tests (from tmp/cron parser_test.go) ---

func TestRange(t *testing.T) {
	zero := uint64(0)
	ranges := []struct {
		expr     string
		min, max uint
		expected uint64
		err      string
	}{
		{"5", 0, 7, 1 << 5, ""},
		{"0", 0, 7, 1 << 0, ""},
		{"7", 0, 7, 1 << 7, ""},

		{"5-5", 0, 7, 1 << 5, ""},
		{"5-6", 0, 7, 1<<5 | 1<<6, ""},
		{"5-7", 0, 7, 1<<5 | 1<<6 | 1<<7, ""},

		{"5-6/2", 0, 7, 1 << 5, ""},
		{"5-7/2", 0, 7, 1<<5 | 1<<7, ""},
		{"5-7/1", 0, 7, 1<<5 | 1<<6 | 1<<7, ""},

		{"*", 1, 3, 1<<1 | 1<<2 | 1<<3 | starBit, ""},
		{"*/2", 1, 3, 1<<1 | 1<<3 | starBit, ""},

		{"5--5", 0, 0, zero, "Too many hyphens"},
		{"jan-x", 0, 0, zero, "Failed to parse int from"},
		{"2-x", 1, 5, zero, "Failed to parse int from"},
		{"*/-12", 0, 0, zero, "Negative number"},
		{"*//2", 0, 0, zero, "Too many slashes"},
		{"1", 3, 5, zero, "below minimum"},
		{"6", 3, 5, zero, "above maximum"},
		{"5-3", 3, 5, zero, "beyond end of range"},
		{"*/0", 0, 0, zero, "should be a positive number"},
	}

	for _, c := range ranges {
		actual, err := getRange(c.expr, bounds{c.min, c.max, nil})
		if len(c.err) != 0 && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.err, err)
		}
		if len(c.err) == 0 && err != nil {
			t.Errorf("%s => unexpected error %v", c.expr, err)
		}
		if actual != c.expected {
			t.Errorf("%s => expected %d, got %d", c.expr, c.expected, actual)
		}
	}
}

func TestField(t *testing.T) {
	fields := []struct {
		expr     string
		min, max uint
		expected uint64
	}{
		{"5", 1, 7, 1 << 5},
		{"5,6", 1, 7, 1<<5 | 1<<6},
		{"5,6,7", 1, 7, 1<<5 | 1<<6 | 1<<7},
		{"1,5-7/2,3", 1, 7, 1<<1 | 1<<5 | 1<<7 | 1<<3},
	}

	for _, c := range fields {
		actual, _ := getField(c.expr, bounds{c.min, c.max, nil})
		if actual != c.expected {
			t.Errorf("%s => expected %d, got %d", c.expr, c.expected, actual)
		}
	}
}

func TestAll(t *testing.T) {
	allBits := []struct {
		r        bounds
		expected uint64
	}{
		{minutes, 0xfffffffffffffff}, // 0-59: 60 ones
		{hours, 0xffffff},            // 0-23: 24 ones
		{dom, 0xfffffffe},            // 1-31: 31 ones, 1 zero
		{months, 0x1ffe},             // 1-12: 12 ones, 1 zero
		{dow, 0x7f},                  // 0-6: 7 ones
	}

	for _, c := range allBits {
		actual := all(c.r)
		if c.expected|starBit != actual {
			t.Errorf("%d-%d/%d => expected %b, got %b",
				c.r.min, c.r.max, 1, c.expected|starBit, actual)
		}
	}
}

func TestBits(t *testing.T) {
	bits := []struct {
		min, max, step uint
		expected       uint64
	}{
		{0, 0, 1, 0x1},
		{1, 1, 1, 0x2},
		{1, 5, 2, 0x2a}, // 101010
		{1, 4, 2, 0xa},  // 1010
	}

	for _, c := range bits {
		actual := getBits(c.min, c.max, c.step)
		if c.expected != actual {
			t.Errorf("%d-%d/%d => expected %b, got %b",
				c.min, c.max, c.step, c.expected, actual)
		}
	}
}

func TestParse(t *testing.T) {
	entries := []struct {
		expr     string
		expected TimeRunner
		err      string
	}{
		{
			expr: "* 5 * * * *",
			expected: &SpecSchedule{
				Second: all(seconds),
				Minute: 1 << 5,
				Hour:   all(hours),
				Dom:    all(dom),
				Month:  all(months),
				Dow:    all(dow),
			},
		},
		{
			expr: "* 5 j * * *",
			err:  "Failed to parse int from",
		},
		{
			expr:     "@every 5m",
			expected: ConstantDelaySchedule{Delay: time.Duration(5) * time.Minute},
		},
		{
			expr: "@every Xm",
			err:  "Failed to parse duration",
		},
		{
			expr: "@yearly",
			expected: &SpecSchedule{
				Second: 1 << seconds.min,
				Minute: 1 << minutes.min,
				Hour:   1 << hours.min,
				Dom:    1 << dom.min,
				Month:  1 << months.min,
				Dow:    all(dow),
			},
		},
		{
			expr: "@annually",
			expected: &SpecSchedule{
				Second: 1 << seconds.min,
				Minute: 1 << minutes.min,
				Hour:   1 << hours.min,
				Dom:    1 << dom.min,
				Month:  1 << months.min,
				Dow:    all(dow),
			},
		},
		{
			expr: "@unrecognized",
			err:  "Unrecognized descriptor",
		},
		{
			expr: "* * * *",
			err:  "Expected 5 to 6 fields",
		},
		{
			expr: "",
			err:  "空的cron",
		},
	}

	for _, c := range entries {
		actual, err := ParseCron(c.expr)
		if len(c.err) != 0 && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.err, err)
		}
		if len(c.err) == 0 && err != nil {
			t.Errorf("%s => unexpected error %v", c.expr, err)
		}
		if !reflect.DeepEqual(actual, c.expected) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.expected, actual)
		}
	}
}

func TestStandardSpecSchedule(t *testing.T) {
	entries := []struct {
		expr     string
		expected TimeRunner
		err      string
	}{
		{
			expr:     "5 * * * *",
			expected: &SpecSchedule{1 << seconds.min, 1 << 5, all(hours), all(dom), all(months), all(dow)},
		},
		{
			expr:     "@every 5m",
			expected: ConstantDelaySchedule{time.Duration(5) * time.Minute},
		},
		{
			expr: "5 j * * *",
			err:  "Failed to parse int from",
		},
		{
			expr: "* * * *",
			err:  "Expected exactly 5 fields",
		},
	}

	for _, c := range entries {
		actual, err := ParseStandard(c.expr)
		if len(c.err) != 0 && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.err, err)
		}
		if len(c.err) == 0 && err != nil {
			t.Errorf("%s => unexpected error %v", c.expr, err)
		}
		if !reflect.DeepEqual(actual, c.expected) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.expected, actual)
		}
	}
}

// --- Activation tests (from tmp/cron spec_test.go) ---

func TestActivation(t *testing.T) {
	tests := []struct {
		time, spec string
		expected   bool
	}{
		// Every fifteen minutes.
		{"Mon Jul 9 15:00 2012", "0 0/15 * * *", true},
		{"Mon Jul 9 15:45 2012", "0 0/15 * * *", true},
		{"Mon Jul 9 15:40 2012", "0 0/15 * * *", false},

		// Every fifteen minutes, starting at 5 minutes.
		{"Mon Jul 9 15:05 2012", "0 5/15 * * *", true},
		{"Mon Jul 9 15:20 2012", "0 5/15 * * *", true},
		{"Mon Jul 9 15:50 2012", "0 5/15 * * *", true},

		// Named months
		{"Sun Jul 15 15:00 2012", "0 0/15 * * Jul", true},
		{"Sun Jul 15 15:00 2012", "0 0/15 * * Jun", false},

		// Everything set.
		{"Sun Jul 15 08:30 2012", "0 30 08 ? Jul Sun", true},
		{"Sun Jul 15 08:30 2012", "0 30 08 15 Jul ?", true},
		{"Mon Jul 16 08:30 2012", "0 30 08 ? Jul Sun", false},
		{"Mon Jul 16 08:30 2012", "0 30 08 15 Jul ?", false},

		// Predefined schedules
		{"Mon Jul 9 15:00 2012", "@hourly", true},
		{"Mon Jul 9 15:04 2012", "@hourly", false},
		{"Mon Jul 9 15:00 2012", "@daily", false},
		{"Mon Jul 9 00:00 2012", "@daily", true},
		{"Mon Jul 9 00:00 2012", "@weekly", false},
		{"Sun Jul 8 00:00 2012", "@weekly", true},
		{"Sun Jul 8 01:00 2012", "@weekly", false},
		{"Sun Jul 8 00:00 2012", "@monthly", false},
		{"Sun Jul 1 00:00 2012", "@monthly", true},

		// Test interaction of DOW and DOM.
		{"Sun Jul 15 00:00 2012", "0 * * 1,15 * Sun", true},
		{"Fri Jun 15 00:00 2012", "0 * * 1,15 * Sun", true},
		{"Wed Aug 1 00:00 2012", "0 * * 1,15 * Sun", true},

		// However, if one has a star, then both need to match.
		{"Sun Jul 15 00:00 2012", "0 * * * * Mon", false},
		{"Mon Jul 9 00:00 2012", "0 * * 1,15 * *", false},
		{"Sun Jul 15 00:00 2012", "0 * * 1,15 * *", true},
		{"Sun Jul 15 00:00 2012", "0 * * */2 * Sun", true},
	}

	for _, test := range tests {
		sched, err := ParseCron(test.spec)
		if err != nil {
			t.Error(err)
			continue
		}
		actual := sched.Next(getTimeCron(test.time).Add(-1 * time.Second))
		expected := getTimeCron(test.time)
		if test.expected && expected != actual || !test.expected && expected == actual {
			t.Errorf("Fail evaluating %s on %s: (expected) %v != %v (actual)",
				test.spec, test.time, expected, actual)
		}
	}
}

func TestNext(t *testing.T) {
	runs := []struct {
		time, spec string
		expected   string
	}{
		// Simple cases
		{"Mon Jul 9 14:45 2012", "0 0/15 * * *", "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 14:59 2012", "0 0/15 * * *", "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 14:59:59 2012", "0 0/15 * * *", "Mon Jul 9 15:00 2012"},

		// Wrap around hours
		{"Mon Jul 9 15:45 2012", "0 20-35/15 * * *", "Mon Jul 9 16:20 2012"},

		// Wrap around days
		{"Mon Jul 9 23:46 2012", "0 */15 * * *", "Tue Jul 10 00:00 2012"},
		{"Mon Jul 9 23:45 2012", "0 20-35/15 * * *", "Tue Jul 10 00:20 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 * * *", "Tue Jul 10 00:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 1/2 * *", "Tue Jul 10 01:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 10-12 * *", "Tue Jul 10 10:20:15 2012"},

		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 1/2 */2 * *", "Thu Jul 11 01:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 * 9-20 * *", "Wed Jul 10 00:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 * 9-20 Jul *", "Wed Jul 10 00:20:15 2012"},

		// Wrap around months
		{"Mon Jul 9 23:35 2012", "0 0 0 9 Apr-Oct ?", "Thu Aug 9 00:00 2012"},
		{"Mon Jul 9 23:35 2012", "0 0 0 */5 Apr,Aug,Oct Mon", "Mon Aug 6 00:00 2012"},
		{"Mon Jul 9 23:35 2012", "0 0 0 */5 Oct Mon", "Mon Oct 1 00:00 2012"},

		// Wrap around years
		{"Mon Jul 9 23:35 2012", "0 0 0 * Feb Mon", "Mon Feb 4 00:00 2013"},
		{"Mon Jul 9 23:35 2012", "0 0 0 * Feb Mon/2", "Fri Feb 1 00:00 2013"},

		// Wrap around minute, hour, day, month, and year
		{"Mon Dec 31 23:59:45 2012", "0 * * * * *", "Tue Jan 1 00:00:00 2013"},

		// Leap year
		{"Mon Jul 9 23:35 2012", "0 0 0 29 Feb ?", "Mon Feb 29 00:00 2016"},

		// Daylight savings time 2am EST (-5) -> 3am EDT (-4)
		{"2012-03-11T00:00:00-0500", "0 30 2 11 Mar ?", "2013-03-11T02:30:00-0400"},

		// hourly job
		{"2012-03-11T00:00:00-0500", "0 0 * * * ?", "2012-03-11T01:00:00-0500"},
		{"2012-03-11T01:00:00-0500", "0 0 * * * ?", "2012-03-11T03:00:00-0400"},
		{"2012-03-11T03:00:00-0400", "0 0 * * * ?", "2012-03-11T04:00:00-0400"},
		{"2012-03-11T04:00:00-0400", "0 0 * * * ?", "2012-03-11T05:00:00-0400"},

		// 1am nightly job
		{"2012-03-11T00:00:00-0500", "0 0 1 * * ?", "2012-03-11T01:00:00-0500"},
		{"2012-03-11T01:00:00-0500", "0 0 1 * * ?", "2012-03-12T01:00:00-0400"},

		// 2am nightly job (skipped)
		{"2012-03-11T00:00:00-0500", "0 0 2 * * ?", "2012-03-12T02:00:00-0400"},

		// Daylight savings time 2am EDT (-4) => 1am EST (-5)
		{"2012-11-04T00:00:00-0400", "0 30 2 04 Nov ?", "2012-11-04T02:30:00-0500"},
		{"2012-11-04T01:45:00-0400", "0 30 1 04 Nov ?", "2012-11-04T01:30:00-0500"},

		// hourly job
		{"2012-11-04T00:00:00-0400", "0 0 * * * ?", "2012-11-04T01:00:00-0400"},
		{"2012-11-04T01:00:00-0400", "0 0 * * * ?", "2012-11-04T01:00:00-0500"},
		{"2012-11-04T01:00:00-0500", "0 0 * * * ?", "2012-11-04T02:00:00-0500"},

		// 1am nightly job (runs twice)
		{"2012-11-04T00:00:00-0400", "0 0 1 * * ?", "2012-11-04T01:00:00-0400"},
		{"2012-11-04T01:00:00-0400", "0 0 1 * * ?", "2012-11-04T01:00:00-0500"},
		{"2012-11-04T01:00:00-0500", "0 0 1 * * ?", "2012-11-05T01:00:00-0500"},

		// 2am nightly job
		{"2012-11-04T00:00:00-0400", "0 0 2 * * ?", "2012-11-04T02:00:00-0500"},
		{"2012-11-04T02:00:00-0500", "0 0 2 * * ?", "2012-11-05T02:00:00-0500"},

		// 3am nightly job
		{"2012-11-04T00:00:00-0400", "0 0 3 * * ?", "2012-11-04T03:00:00-0500"},
		{"2012-11-04T03:00:00-0500", "0 0 3 * * ?", "2012-11-05T03:00:00-0500"},

		// Unsatisfiable
		{"Mon Jul 9 23:35 2012", "0 0 0 30 Feb ?", ""},
		{"Mon Jul 9 23:35 2012", "0 0 0 31 Apr ?", ""},
	}

	for _, c := range runs {
		sched, err := ParseCron(c.spec)
		if err != nil {
			t.Error(err)
			continue
		}
		actual := sched.Next(getTimeCron(c.time))
		expected := getTimeCron(c.expected)
		if !actual.Equal(expected) {
			t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.spec, expected, actual)
		}
	}
}

func TestNextWithTz(t *testing.T) {
	runs := []struct {
		time, spec string
		expected   string
	}{
		{"2016-01-03T13:09:03+0530", "0 14 14 * * *", "2016-01-03T14:14:00+0530"},
		{"2016-01-03T04:09:03+0530", "0 14 14 * * ?", "2016-01-03T14:14:00+0530"},
		{"2016-01-03T14:09:03+0530", "0 14 14 * * *", "2016-01-03T14:14:00+0530"},
		{"2016-01-03T14:00:00+0530", "0 14 14 * * ?", "2016-01-03T14:14:00+0530"},
	}
	for _, c := range runs {
		sched, err := ParseCron(c.spec)
		if err != nil {
			t.Error(err)
			continue
		}
		actual := sched.Next(getTimeCronTZ(c.time))
		expected := getTimeCronTZ(c.expected)
		if !actual.Equal(expected) {
			t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.spec, expected, actual)
		}
	}
}

func TestErrors(t *testing.T) {
	invalidSpecs := []string{
		"xyz",
		"60 0 * * *",
		"0 60 * * *",
		"0 0 * * XYZ",
	}
	for _, spec := range invalidSpecs {
		_, err := ParseCron(spec)
		if err == nil {
			t.Error("expected an error parsing: ", spec)
		}
	}
}

// getTime parses a time string in cron test format.
func getTimeCron(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse("Mon Jan 2 15:04 2006", value)
	if err != nil {
		t, err = time.Parse("Mon Jan 2 15:04:05 2006", value)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05-0700", value)
			if err != nil {
				panic(err)
			}
			if ny, err := time.LoadLocation("America/New_York"); err == nil {
				t = t.In(ny)
			}
		}
	}
	return t
}

// getTimeCronTZ parses a time string keeping the parsed timezone offset.
func getTimeCronTZ(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse("Mon Jan 2 15:04 2006", value)
	if err != nil {
		t, err = time.Parse("Mon Jan 2 15:04:05 2006", value)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05-0700", value)
			if err != nil {
				panic(err)
			}
		}
	}
	return t
}
