package marker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDayKeyAndPaths(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 13, 23, 30, 0, 0, time.UTC)
	day := DayKey(when, loc)
	if day != "2026-10-13" {
		t.Fatalf("got %s", day)
	}
	rel := BackfillRelPath(day, 3)
	want := filepath.Join("submissions", "2026-10-13", "backfill-3.json")
	if rel != want {
		t.Fatalf("got %s want %s", rel, want)
	}
}

func TestWriteIdentical(t *testing.T) {
	dir := t.TempDir()
	m := New("lesson-1", TriggerManual, time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC))
	rel := RelPath("2026-01-02", "lesson-1")
	if err := Write(dir, rel, m); err != nil {
		t.Fatal(err)
	}
	ok, err := Identical(dir, rel, m)
	if err != nil || !ok {
		t.Fatalf("identical=%v err=%v", ok, err)
	}
	if !Exists(dir, rel) {
		t.Fatal("expected exists")
	}
	data, _ := os.ReadFile(AbsPath(dir, rel))
	if len(data) == 0 {
		t.Fatal("empty file")
	}
}
