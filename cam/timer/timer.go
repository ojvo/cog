package timer

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"time"
)

// CallerPerHour calls F at the start, and then every top of the hour.
// It supports context cancellation and panic recovery.
func CallerPerHour(ctx context.Context, F func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "CallerPerHour panic recovered: %v\n", r)
			}
		}()

		// Call immediately
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "CallerPerHour F panic: %v\n", r)
				}
			}()
			F()
		}()

		for {
			now := time.Now()
			// Calculate next local top-of-the-hour.
			// time.Truncate returns a time based on the UTC epoch, so in
			// non-whole-hour timezones (e.g. UTC+8) it does not yield the
			// local whole hour. Construct the next local hour explicitly.
			next := time.Date(
				now.Year(), now.Month(), now.Day(),
				now.Hour()+1, 0, 0, 0, now.Location(),
			)

			timer := time.NewTimer(next.Sub(now))
			
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							fmt.Fprintf(os.Stderr, "CallerPerHour F panic: %v\n", r)
						}
					}()
					F()
				}()
			}
		}
	}()
}

// SleepSecond sleeps for a random duration between [st, et) seconds.
// If st >= et, it sleeps for st seconds.
func SleepSecond(st int, et int) {
	if st < 0 {
		st = 0
	}
	if et <= st {
		time.Sleep(time.Duration(st) * time.Second)
		return
	}
	ss := st + rand.IntN(et-st)
	time.Sleep(time.Duration(ss) * time.Second)
}

// SleepMs sleeps for a random duration between [st, et) milliseconds.
// If st >= et, it sleeps for st milliseconds.
func SleepMs(st int, et int) {
	if st < 0 {
		st = 0
	}
	if et <= st {
		time.Sleep(time.Duration(st) * time.Millisecond)
		return
	}
	ss := st + rand.IntN(et-st)
	time.Sleep(time.Duration(ss) * time.Millisecond)
}
