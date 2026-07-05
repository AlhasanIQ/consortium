package admin

import (
	"fmt"
	"strings"
	"time"
)

var adminTimestampLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700 -07",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
}

func parseAdminTimestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	// Go's time.Time.String() may include monotonic suffix, which is not parseable.
	if idx := strings.Index(value, " m="); idx != -1 {
		value = value[:idx]
	}
	for _, layout := range adminTimestampLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format: %q", raw)
}

func parseNullableAdminTimestamp(raw string) *time.Time {
	parsed, err := parseAdminTimestamp(raw)
	if err != nil {
		return nil
	}
	return &parsed
}
