package env

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFastTime(t *testing.T) {
	f := NewFastTime().Start(context.Background(), 10*time.Millisecond)
	defer f.Stop()
	now := time.Now()
	ft := f.Now()
	if diff := now.Sub(ft); diff > 150*time.Millisecond || diff < -150*time.Millisecond {
		t.Fatalf("FastTime.Now too far off: %v vs %v", ft, now)
	}
	if f.UnixNow() == 0 || f.UnixNanoNow() == 0 {
		t.Fatal("unix timestamps should be non-zero")
	}
}

func TestFastTimeFormat(t *testing.T) {
	f := NewFastTime().SetFormat(time.RFC3339Nano).Start(context.Background(), 10*time.Millisecond)
	defer f.Stop()
	formatted := string(f.FormattedNow())
	if !strings.Contains(formatted, "T") {
		t.Fatalf("formatted time looks wrong: %q", formatted)
	}
}

func TestPackageFastTimeHelpers(t *testing.T) {
	if Now().IsZero() {
		t.Fatal("Now() should not be zero")
	}
	if UnixNow() == 0 || UnixNanoNow() == 0 {
		t.Fatal("package-level fast time timestamps should be non-zero")
	}
}

func TestParseTimeZone(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"utc", false},
		{"UTC+8", false},
		{"utc-5", false},
		{"local", false},
		{"gmt+8", true},
	}
	for _, tc := range cases {
		loc, err := ParseTimeZone(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseTimeZone(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil || loc == nil {
			t.Fatalf("ParseTimeZone(%q) = (%v, %v)", tc.in, loc, err)
		}
	}
}

func TestEnvPaths(t *testing.T) {
	if exe := GetExecutablePath(); exe == "" {
		t.Fatal("GetExecutablePath empty")
	}
	if dir, err := GetExecutableDir(); err != nil || dir == "" {
		t.Fatalf("GetExecutableDir = (%q, %v)", dir, err)
	}
	if wd, err := WorkingDir(); err != nil || wd == "" {
		t.Fatalf("WorkingDir = (%q, %v)", wd, err)
	}
	if home, err := UserHomeDir(); err != nil || home == "" {
		t.Fatalf("UserHomeDir = (%q, %v)", home, err)
	}
	if TempDir() == "" {
		t.Fatal("TempDir empty")
	}
}
