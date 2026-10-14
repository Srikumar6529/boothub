package sync

import (
	"testing"
	"time"
)

func TestCalendarDayDoesNotShiftWest(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	// Heatmap encodes calendar day as midnight UTC.
	d := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	dayKey, noon := calendarDay(d, loc)
	if dayKey != "2026-10-01" {
		t.Fatalf("dayKey=%s want 2026-10-01 (must not shift to Sep 30)", dayKey)
	}
	if noon.Location().String() != loc.String() {
		t.Fatalf("noon loc=%v", noon.Location())
	}
	if noon.Hour() != 12 || noon.Day() != 1 {
		t.Fatalf("noon=%v", noon)
	}
}
