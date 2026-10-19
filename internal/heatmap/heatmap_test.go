package heatmap

import (
	"testing"
	"time"
)

func TestParseHeatmapDay(t *testing.T) {
	cases := map[string]string{
		`"2026-05-01"`:                "2026-05-01",
		`"2026-05-01T00:00:00Z"`:      "2026-05-01",
		`"2026-05-01T05:00:00-05:00"`: "2026-05-01",
	}
	for in, want := range cases {
		got, err := parseHeatmapDay([]byte(in))
		if err != nil {
			t.Fatalf("parse(%s): %v", in, err)
		}
		if got != want {
			t.Fatalf("parse(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestCacheRoundTripAndFreshness(t *testing.T) {
	DirOverride = t.TempDir()
	t.Cleanup(func() { DirOverride = "" })

	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	if err := SaveCache(Cache{
		Handle:    "someone",
		Timezone:  "America/Chicago",
		FetchedAt: now,
		Days:      map[string]int{"2026-05-01": 3},
	}); err != nil {
		t.Fatal(err)
	}

	got, ok := LoadCache("someone", "America/Chicago")
	if !ok {
		t.Fatal("cache not found after save")
	}
	if got.Days["2026-05-01"] != 3 {
		t.Fatalf("days = %v", got.Days)
	}
	if !got.FreshFor(now, loc) {
		t.Fatal("cache should be fresh for the same day")
	}
	if got.FreshFor(now.Add(48*time.Hour), loc) {
		t.Fatal("cache should be stale two days later")
	}
}

func TestMergeKeepsOlderDays(t *testing.T) {
	c := Cache{Days: map[string]int{"2026-01-01": 1}}
	c.Merge(Data{Calendar: []Day{{Date: "2026-05-01", Count: 2}}}, time.Now())
	if c.Days["2026-01-01"] != 1 || c.Days["2026-05-01"] != 2 {
		t.Fatalf("merge lost data: %v", c.Days)
	}
}
