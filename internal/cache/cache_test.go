package cache

import (
	"testing"
	"time"

	"github.com/Srikumar6529/boothub/internal/heatmap"
)

func TestSaveLoadMergeAndFreshness(t *testing.T) {
	DirOverride = t.TempDir()
	t.Cleanup(func() { DirOverride = "" })

	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}

	h := Heatmap{Handle: "davinci2003", Timezone: "America/Chicago"}
	h.Merge(heatmap.Heatmap{Calendar: []heatmap.Day{
		{Date: "2026-10-17", Count: 3},
		{Date: "2026-10-18", Count: 0},
	}}, now)

	if err := Save(h); err != nil {
		t.Fatal(err)
	}

	loaded, ok, err := Load("davinci2003", "America/Chicago")
	if err != nil || !ok {
		t.Fatalf("load ok=%v err=%v", ok, err)
	}
	if loaded.Days["2026-10-17"] != 3 {
		t.Fatalf("days=%v", loaded.Days)
	}
	if _, zeroKept := loaded.Days["2026-10-18"]; zeroKept {
		t.Fatal("zero-count days should not be stored")
	}
	if !loaded.FreshFor(now, loc) {
		t.Fatal("cache saved today should be fresh")
	}
	if loaded.FreshFor(now.Add(24*time.Hour), loc) {
		t.Fatal("cache should be stale the next day")
	}
}

func TestLoadMissingIsNotAnError(t *testing.T) {
	DirOverride = t.TempDir()
	t.Cleanup(func() { DirOverride = "" })

	if _, ok, err := Load("nobody", "UTC"); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
