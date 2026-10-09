package schedule

import (
	"testing"
	"time"
)

func must(t *testing.T, spec, tz string) *Schedule {
	t.Helper()
	s, err := Parse(spec, tz)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIntervalsAndLimits(t *testing.T) {
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	if n := must(t, "every 2h", "UTC").Next(now); !n.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("next %v", n)
	}
	for _, bad := range []string{"every 1m", "* * * * *", "*/2 * * * *", "every soon", "0 25 * * *", "0 9 * *"} {
		if _, err := Parse(bad, "UTC"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := Parse("*/5 * * * *", "UTC"); err != nil {
		t.Error(err)
	}
	if _, err := Parse("0 9 * * *", "Mars/Olympus"); err == nil {
		t.Error("bad timezone accepted")
	}
}

func TestCronInZone(t *testing.T) {
	s := must(t, "0 9 * * 1-5", "America/New_York")
	ny, _ := time.LoadLocation("America/New_York")
	fri := time.Date(2026, 10, 9, 10, 0, 0, 0, ny) // a Friday, after 9
	n := s.Next(fri)
	if n.Weekday() != time.Monday || n.Hour() != 9 || n.Location().String() != "America/New_York" {
		t.Fatalf("next %v", n)
	}
	if got := s.Text(); got != "Weekdays at 9:00" {
		t.Fatalf("text %q", got)
	}
	monthly := must(t, "30 8 1 * *", "UTC")
	if got := monthly.Text(); got != "On the 1st of every month at 8:30" {
		t.Fatalf("text %q", got)
	}
	if n := monthly.Next(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)); !n.Equal(time.Date(2026, 11, 1, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("monthly next %v", n)
	}
	if got := must(t, "every 90m", "").Text(); got != "Every 90 minutes" {
		t.Fatalf("text %q", got)
	}
}

func TestDaylightSavingSkip(t *testing.T) {
	// 2:30 does not exist on 2026-03-08 in New York; that day does not fire.
	s := must(t, "30 2 * * *", "America/New_York")
	ny, _ := time.LoadLocation("America/New_York")
	n := s.Next(time.Date(2026, 3, 7, 12, 0, 0, 0, ny))
	if n.Day() != 9 || n.Hour() != 2 || n.Minute() != 30 {
		t.Fatalf("next %v", n)
	}
}

func TestDayOfMonthOrWeek(t *testing.T) {
	s := must(t, "0 12 13 * 5", "UTC") // the 13th, or any Friday
	n := s.Next(time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC))
	if n.Day() != 13 { // Tuesday the 13th before Friday the 16th
		t.Fatalf("next %v", n)
	}
}
