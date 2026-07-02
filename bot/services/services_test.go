package services

import (
	"testing"
	"time"
)

func TestGetBudgetCycleRange(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jakarta")

	tests := []struct {
		name          string
		now           time.Time
		cycleStartDay int
		expectedStart string
		expectedEnd   string
	}{
		{
			name:          "Cycle start 25, today is July 2nd (before 25th)",
			now:           time.Date(2026, 7, 2, 10, 30, 0, 0, loc),
			cycleStartDay: 25,
			expectedStart: "2026-06-25 00:00:00",
			expectedEnd:   "2026-07-24 23:59:59.999999999",
		},
		{
			name:          "Cycle start 25, today is June 26th (after 25th)",
			now:           time.Date(2026, 6, 26, 15, 45, 0, 0, loc),
			cycleStartDay: 25,
			expectedStart: "2026-06-25 00:00:00",
			expectedEnd:   "2026-07-24 23:59:59.999999999",
		},
		{
			name:          "Cycle start 25, today is June 25th (exact start day)",
			now:           time.Date(2026, 6, 25, 0, 0, 0, 0, loc),
			cycleStartDay: 25,
			expectedStart: "2026-06-25 00:00:00",
			expectedEnd:   "2026-07-24 23:59:59.999999999",
		},
		{
			name:          "Cycle start 1, today is July 2nd",
			now:           time.Date(2026, 7, 2, 10, 30, 0, 0, loc),
			cycleStartDay: 1,
			expectedStart: "2026-07-01 00:00:00",
			expectedEnd:   "2026-07-31 23:59:59.999999999",
		},
		{
			name:          "Cycle start 31, today is February 15th (cap to last day of Feb)",
			now:           time.Date(2026, 2, 15, 12, 0, 0, 0, loc),
			cycleStartDay: 31,
			expectedStart: "2026-01-31 00:00:00",
			expectedEnd:   "2026-02-27 23:59:59.999999999", // 2026 is non-leap year (28 days)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := getBudgetCycleRange(tt.now, tt.cycleStartDay)

			startStr := start.Format("2006-01-02 15:04:05")
			endStr := end.Format("2006-01-02 15:04:05.999999999")

			if startStr != tt.expectedStart {
				t.Errorf("Expected start %q, got %q", tt.expectedStart, startStr)
			}
			if endStr != tt.expectedEnd {
				t.Errorf("Expected end %q, got %q", tt.expectedEnd, endStr)
			}
		})
	}
}
