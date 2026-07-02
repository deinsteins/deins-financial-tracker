package scheduler

import (
	"testing"
	"time"
)

func TestNextMonthlyRunTime_BeforeEightAM(t *testing.T) {
	now := time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC)
	got := nextMonthlyRunTime(now)
	want := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextMonthlyRunTime_AfterEightAM(t *testing.T) {
	now := time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)
	got := nextMonthlyRunTime(now)
	want := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextMonthlyRunTime_ExactlyEightAM(t *testing.T) {
	now := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	got := nextMonthlyRunTime(now)
	want := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTranslateMonthID(t *testing.T) {
	cases := map[time.Month]string{
		time.January:   "Januari",
		time.February:  "Februari",
		time.March:     "Maret",
		time.April:     "April",
		time.May:       "Mei",
		time.June:      "Juni",
		time.July:      "Juli",
		time.August:    "Agustus",
		time.September: "September",
		time.October:   "Oktober",
		time.November:  "November",
		time.December:  "Desember",
	}

	for m, want := range cases {
		if got := translateMonthID(m); got != want {
			t.Errorf("translateMonthID(%v) = %q, want %q", m, got, want)
		}
	}
}
