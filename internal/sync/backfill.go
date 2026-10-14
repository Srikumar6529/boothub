package sync

import (
	"fmt"
	"time"

	"github.com/Srikumar6529/boothub/internal/heatmap"
	"github.com/Srikumar6529/boothub/internal/marker"
)

type BackfillOptions struct {
	Handle    string
	Timezone  string
	MaxPerDay int // 0 = unlimited
}

type BackfillStats struct {
	Days        int
	Created     int
	Skipped     int
	CappedDays  int
	TotalPlanned int
}

func (e *Engine) Backfill(opts BackfillOptions) (BackfillStats, error) {
	handle := opts.Handle
	if handle == "" {
		handle = e.Cfg.BootdevHandle
	}
	tz := opts.Timezone
	if tz == "" {
		tz = e.Cfg.Timezone
	}

	days, err := heatmap.FetchActivityHeatmap(handle, tz)
	if err != nil {
		return BackfillStats{}, err
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return BackfillStats{}, err
	}

	stats := BackfillStats{Days: len(days)}
	for _, day := range days {
		count := day.Count
		if opts.MaxPerDay > 0 && count > opts.MaxPerDay {
			count = opts.MaxPerDay
			stats.CappedDays++
		}
		stats.TotalPlanned += count

		base := NoonLocal(day.Date, loc)
		dayKey := marker.DayKey(base, loc)

		for n := 1; n <= count; n++ {
			lessonID := fmt.Sprintf("backfill-%d", n)
			rel := marker.RelPath(dayKey, lessonID)
			if marker.Exists(e.Cfg.RepoPath, rel) {
				stats.Skipped++
				continue
			}

			// Stagger by n seconds so commits are distinct on the same calendar day.
			when := base.Add(time.Duration(n) * time.Second)
			_, err := e.Sync(Request{
				LessonID:      lessonID,
				Trigger:       marker.TriggerBackfill,
				When:          when,
				ActivityCount: day.Count,
				Backdate:      true,
				SkipPush:      true,
			})
			if err != nil {
				return stats, fmt.Errorf("day %s commit %d: %w", dayKey, n, err)
			}
			stats.Created++
		}
	}

	if stats.Created > 0 {
		if err := e.Push(); err != nil {
			return stats, fmt.Errorf("created %d commits locally but push failed: %w", stats.Created, err)
		}
	}
	return stats, nil
}
