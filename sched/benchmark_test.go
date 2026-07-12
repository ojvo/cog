package sched

import (
	"testing"
	"time"
)

func BenchmarkParseCron(b *testing.B) {
	specs := []string{
		"*/5 * * * *",
		"0 2 * * 1-5",
		"30 3 1,15 * *",
		"0 0 */3 * *",
		"@daily",
		"@every 5m",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseCron(specs[i%len(specs)])
	}
}

func BenchmarkCronParser_Next(b *testing.B) {
	parser, err := ParseCron("*/5 * * * *")
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser.Next(now)
	}
}

func BenchmarkGetNextDue(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		getNextDue("*/5 * * * *")
	}
}
