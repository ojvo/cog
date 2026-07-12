package sched

import (
	"testing"
	"time"
)

func TestCronParser_Parse(t *testing.T) {
	// Test standard parser (Minute | Hour | Dom | Month | Dow)
	parser := NewCronParser(Minute | Hour | Dom | Month | Dow)

	tests := []struct {
		spec     string
		hasError bool
	}{
		{"* * * * *", false},      // Every minute
		{"0 0 1 1 *", false},      // Specific date
		{"*/5 * * * *", false},    // Every 5 minutes
		{"* * * *", true},         // Too few fields
		{"* * * * * *", true},     // Too many fields
		{"invalid * * * *", true}, // Invalid format
	}

	for _, tt := range tests {
		_, err := parser.Parse(tt.spec)
		if tt.hasError {
			if err == nil {
				t.Errorf("Expected error for spec: %s", tt.spec)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for spec: %s, error: %v", tt.spec, err)
			}
		}
	}
}

func TestCronParser_ParseWithSeconds(t *testing.T) {
	// Test parser with seconds (Second | Minute | Hour | Dom | Month | Dow)
	parser := NewCronParser(Second | Minute | Hour | Dom | Month | Dow)

	tests := []struct {
		spec     string
		hasError bool
	}{
		{"* * * * * *", false},    // Every second
		{"0 */5 * * * *", false},  // Every 5 minutes at 0 seconds
		{"* * * * *", true},       // Too few fields
	}

	for _, tt := range tests {
		_, err := parser.Parse(tt.spec)
		if tt.hasError {
			if err == nil {
				t.Errorf("Expected error for spec: %s", tt.spec)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for spec: %s, error: %v", tt.spec, err)
			}
		}
	}
}

func TestCronParser_ParseDescriptors(t *testing.T) {
	parser := NewCronParser(Minute | Hour | Dom | Month | Dow | Descriptor)

	tests := []struct {
		spec     string
		hasError bool
	}{
		{"@daily", false},
		{"@hourly", false},
		{"@weekly", false},
		{"@monthly", false},
		{"@yearly", false},
		{"@annually", false},
		{"@invalid", true},
	}

	for _, tt := range tests {
		_, err := parser.Parse(tt.spec)
		if tt.hasError {
			if err == nil {
				t.Errorf("Expected error for spec: %s", tt.spec)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for spec: %s, error: %v", tt.spec, err)
			}
		}
	}
}

func TestSchedule_Next(t *testing.T) {
	parser := NewCronParser(Minute | Hour | Dom | Month | Dow)
	
	// "0 10 * * *" -> Every day at 10:00
	sched, err := parser.Parse("0 10 * * *")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Case 1: Before 10:00 -> Today 10:00
	now := time.Date(2023, 1, 1, 9, 0, 0, 0, time.UTC)
	next := sched.Next(now)
	expected := time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, next)
	}

	// Case 2: After 10:00 -> Tomorrow 10:00
	now = time.Date(2023, 1, 1, 10, 0, 1, 0, time.UTC)
	next = sched.Next(now)
	expected = time.Date(2023, 1, 2, 10, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, next)
	}
}

func TestEvery(t *testing.T) {
	// Test Every(duration)
	schedule := Every(5 * time.Minute)
	
	now := time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC)
	next := schedule.Next(now)
	
	// Should be 5 minutes later
	expected := now.Add(5 * time.Minute)
	if !next.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, next)
	}
	
	// Test minimum duration (1 second)
	scheduleSmall := Every(1 * time.Millisecond)
	if scheduleSmall.Delay != time.Second {
		t.Errorf("Expected 1s delay for small duration, got %v", scheduleSmall.Delay)
	}
}

func TestConstantDelaySchedule_Next(t *testing.T) {
	schedule := ConstantDelaySchedule{Delay: 1 * time.Hour}
	
	now := time.Now()
	next := schedule.Next(now)
	
	// Should be 1 hour later, minus nanoseconds (as per implementation)
	// implementation: t.Add(schedule.Delay - time.Duration(t.Nanosecond())*time.Nanosecond)
	// This rounds to the second.
	
	expected := now.Add(1 * time.Hour).Truncate(time.Second).Add(time.Duration(now.Nanosecond()) * time.Nanosecond) 
	// Wait, let's look at implementation:
	// return t.Add(schedule.Delay - time.Duration(t.Nanosecond())*time.Nanosecond)
	// = t + Delay - t.Nano
	// = (t - t.Nano) + Delay
	// = t.Truncate(Second) + Delay
	
	expected = now.Truncate(time.Second).Add(1 * time.Hour)
	
	// However, the Next function returns:
	// return t.Add(schedule.Delay - time.Duration(t.Nanosecond())*time.Nanosecond)
	// If t has 500ms (5e8 ns), and Delay is 1h.
	// Result = t + 1h - 500ms = t + 59m 59.5s ? 
	// No, time.Duration(t.Nanosecond())*time.Nanosecond IS the nanosecond part.
	// So it subtracts the nanoseconds.
	
	if !next.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, next)
	}
}
