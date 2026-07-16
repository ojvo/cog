package env

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseTimeZone parses simple timezone specifiers:
//   - UTC
//   - UTC+8 / UTC-5
//   - local
func ParseTimeZone(timeZone string) (*time.Location, error) {
	timeZone = strings.ToLower(strings.TrimSpace(timeZone))
	if strings.HasPrefix(timeZone, "utc") {
		if timeZone == "utc" {
			return time.UTC, nil
		}
		offsetStr := strings.TrimPrefix(timeZone, "utc")
		if strings.HasPrefix(offsetStr, "+") || strings.HasPrefix(offsetStr, "-") {
			offset, err := strconv.Atoi(offsetStr)
			if err != nil {
				return nil, fmt.Errorf("invalid UTC offset: %s", offsetStr)
			}
			return time.FixedZone(fmt.Sprintf("UTC%+d", offset), offset*3600), nil
		}
		return nil, fmt.Errorf("invalid UTC offset format: %s", offsetStr)
	}
	if timeZone == "local" {
		return time.Local, nil
	}
	return nil, fmt.Errorf("unsupported time zone: %s", timeZone)
}
