package admin

import (
	"testing"
	"time"
)

func TestParseAdminTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, got time.Time)
	}{
		{
			name:    "empty string returns error",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace-only returns error",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "unsupported format returns error",
			input:   "not-a-date",
			wantErr: true,
		},
		{
			name:  "RFC3339Nano format",
			input: "2024-01-15T10:30:00.123456789Z",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 1 || got.Day() != 15 {
					t.Errorf("unexpected date: %v", got)
				}
				if got.Hour() != 10 || got.Minute() != 30 || got.Second() != 0 {
					t.Errorf("unexpected time: %v", got)
				}
			},
		},
		{
			name:  "RFC3339 format",
			input: "2024-06-01T12:00:00Z",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 6 || got.Day() != 1 {
					t.Errorf("unexpected date: %v", got)
				}
			},
		},
		{
			name:  "SQLite datetime format without timezone",
			input: "2024-03-20 08:45:30",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 3 || got.Day() != 20 {
					t.Errorf("unexpected date: %v", got)
				}
				if got.Hour() != 8 || got.Minute() != 45 || got.Second() != 30 {
					t.Errorf("unexpected time: %v", got)
				}
			},
		},
		{
			name:  "format with nanoseconds no timezone",
			input: "2024-03-20 08:45:30.123456789",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 3 || got.Day() != 20 {
					t.Errorf("unexpected date: %v", got)
				}
				if got.Nanosecond() != 123456789 {
					t.Errorf("unexpected nanoseconds: %v", got.Nanosecond())
				}
			},
		},
		{
			name:  "format with timezone offset",
			input: "2024-03-20 08:45:30.000 +0000",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 {
					t.Errorf("unexpected year: %v", got.Year())
				}
			},
		},
		{
			name:  "format with timezone offset and name",
			input: "2024-03-20 08:45:30.000000000 +0000 UTC",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 3 || got.Day() != 20 {
					t.Errorf("unexpected date: %v", got)
				}
			},
		},
		{
			name:  "leading and trailing whitespace is trimmed",
			input: "  2024-01-15T10:30:00Z  ",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 {
					t.Errorf("unexpected year: %v", got.Year())
				}
			},
		},
		{
			name:  "monotonic clock suffix is stripped",
			input: "2024-03-20 08:45:30.123456789 +0000 UTC m=+12345.678",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 3 || got.Day() != 20 {
					t.Errorf("unexpected date after stripping monotonic: %v", got)
				}
			},
		},
		{
			name:  "Go time.Time.String() output with monotonic",
			input: time.Date(2024, 7, 4, 12, 0, 0, 0, time.UTC).String(),
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 7 || got.Day() != 4 {
					t.Errorf("unexpected date: %v", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAdminTimestamp(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error but got nil, result: %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestParseNullableAdminTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNil   bool
		checkTime func(t *testing.T, got *time.Time)
	}{
		{
			name:    "empty string returns nil",
			input:   "",
			wantNil: true,
		},
		{
			name:    "invalid format returns nil",
			input:   "not-a-date",
			wantNil: true,
		},
		{
			name:    "whitespace returns nil",
			input:   "   ",
			wantNil: true,
		},
		{
			name:  "valid RFC3339 returns non-nil pointer",
			input: "2024-09-01T00:00:00Z",
			checkTime: func(t *testing.T, got *time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 9 || got.Day() != 1 {
					t.Errorf("unexpected date: %v", got)
				}
			},
		},
		{
			name:  "valid SQLite format returns non-nil pointer",
			input: "2024-12-31 23:59:59",
			checkTime: func(t *testing.T, got *time.Time) {
				t.Helper()
				if got.Year() != 2024 || got.Month() != 12 || got.Day() != 31 {
					t.Errorf("unexpected date: %v", got)
				}
				if got.Hour() != 23 || got.Minute() != 59 || got.Second() != 59 {
					t.Errorf("unexpected time: %v", got)
				}
			},
		},
		{
			name:  "valid timestamp with nanoseconds returns non-nil",
			input: "2024-03-20 08:45:30.999999999",
			checkTime: func(t *testing.T, got *time.Time) {
				t.Helper()
				if got == nil {
					t.Fatal("expected non-nil pointer")
				}
				if got.Nanosecond() != 999999999 {
					t.Errorf("unexpected nanoseconds: %v", got.Nanosecond())
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseNullableAdminTimestamp(tc.input)
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil but got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil pointer but got nil")
			}
			if tc.checkTime != nil {
				tc.checkTime(t, got)
			}
		})
	}
}

func TestParseAdminTimestampAllLayouts(t *testing.T) {
	// Verify each layout in adminTimestampLayouts is actually tried
	cases := []struct {
		layout string
		sample string
	}{
		{"2006-01-02 15:04:05.999999999 -0700 MST", "2024-01-02 15:04:05.123456789 +0000 UTC"},
		{"2006-01-02 15:04:05.999999999 -0700 -07", "2024-01-02 15:04:05.123456789 +0000 +00"},
		{"2006-01-02 15:04:05.999999999 -0700", "2024-01-02 15:04:05.123456789 +0000"},
		{"2006-01-02 15:04:05.999999999", "2024-01-02 15:04:05.123456789"},
		{"2006-01-02 15:04:05", "2024-01-02 15:04:05"},
		{time.RFC3339Nano, "2024-01-02T15:04:05.123456789Z"},
		{time.RFC3339, "2024-01-02T15:04:05Z"},
	}

	for _, tc := range cases {
		t.Run(tc.layout, func(t *testing.T) {
			got, err := parseAdminTimestamp(tc.sample)
			if err != nil {
				t.Fatalf("failed to parse %q with layout %q: %v", tc.sample, tc.layout, err)
			}
			if got.IsZero() {
				t.Errorf("parsed time is zero for layout %q", tc.layout)
			}
		})
	}
}
