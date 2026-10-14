package sync

import (
	"fmt"
	"time"

	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/heatmap"
	"github.com/Srikumar6529/boothub/internal/marker"
)

type BackfillOptions struct {
	Handle    string
	Timezone  string
	MaxPerDay int // 0 = unlimited
	Force     bool
}

type BackfillStats struct {
	Days         int
	Created      int
	Skipped      int
	CappedDays   int
	TotalPlanned int
	Pushed       bool
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

	repo := gitops.Repo{Path: e.Cfg.RepoPath}
	if !repo.IsGitRepo() {
		return BackfillStats{}, fmt.Errorf("not a git repo: %s", e.Cfg.RepoPath)
	}
	if err := repo.EnsureIdentity(); err != nil {
		return BackfillStats{}, fmt.Errorf("git identity: %w", err)
	}

	stats := BackfillStats{Days: len(days)}
	for _, day := range days {
		count := day.Count
		if opts.MaxPerDay > 0 && count > opts.MaxPerDay {
			count = opts.MaxPerDay
			stats.CappedDays++
		}
		stats.TotalPlanned += count

		// Heatmap dates are calendar days encoded as midnight UTC for the
		// requested timezone — do NOT shift them with .In(loc).
		dayKey, base := calendarDay(day.Date, loc)

		for n := 1; n <= count; n++ {
			lessonID := fmt.Sprintf("backfill-%d", n)
			rel := marker.RelPath(dayKey, lessonID)

			// Skip only when already committed in HEAD (not merely on disk).
			// Orphan files from a failed first run must still get committed.
			if !opts.Force && repo.FileInHEAD(rel) {
				stats.Skipped++
				continue
			}

			when := base.Add(time.Duration(n) * time.Second)
			res, err := e.Sync(Request{
				LessonID:      lessonID,
				Trigger:       marker.TriggerBackfill,
				When:          when,
				ActivityCount: day.Count,
				Backdate:      true,
				SkipPush:      true,
				Force:         opts.Force,
			})
			if err != nil {
				return stats, fmt.Errorf("day %s commit %d: %w", dayKey, n, err)
			}
			if res.Skipped {
				stats.Skipped++
				continue
			}
			stats.Created++
		}
	}

	// Always push when we created commits OR when local is ahead of origin
	// (covers: first run committed but push failed → second run would skip all).
	needsPush := stats.Created > 0 || repo.AheadOfRemote(e.Cfg.Remote, e.Cfg.Branch)
	if needsPush {
		if err := e.Push(); err != nil {
			return stats, fmt.Errorf("local commits ready but push failed: %w\nFix auth/remote, then re-run: boothub backfill", err)
		}
		stats.Pushed = true
	}
	return stats, nil
}

// calendarDay treats heatmap timestamps as date-only labels (midnight UTC).
func calendarDay(d time.Time, loc *time.Location) (dayKey string, noonLocal time.Time) {
	u := d.UTC()
	dayKey = u.Format("2006-01-02")
	noonLocal = time.Date(u.Year(), u.Month(), u.Day(), 12, 0, 0, 0, loc)
	return dayKey, noonLocal
}
