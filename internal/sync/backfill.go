package sync

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Srikumar6529/boothub/internal/cache"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/heatmap"
	"github.com/Srikumar6529/boothub/internal/marker"
)

type BackfillOptions struct {
	Handle    string
	Timezone  string
	MaxPerDay int // 0 = unlimited
	DryRun    bool
	// Refresh forces a heatmap API call even if the cache is fresh.
	Refresh bool
	// NoRefresh never calls the API; fails if there is no cached heatmap.
	NoRefresh bool
	Since     string // YYYY-MM-DD (inclusive, optional)
	Until     string // YYYY-MM-DD (inclusive, optional)
	// IncludeGitHubCommits adds Boot.dev's view of your real GitHub commits to
	// the target. Off by default: those commits already exist on GitHub.
	IncludeGitHubCommits bool
	// SkipPush leaves the new commits local (used by tests).
	SkipPush bool
}

type BackfillStats struct {
	Days        int // days that needed new commits
	Target      int // total planned units for the range
	Existing    int // marker commits already present for the range
	Created     int // commits created (or that would be created on a dry run)
	Skipped     int
	CappedDays  int
	OverDays    int // days with more commits than Boot.dev shows
	OverCommits int // total surplus commits on those days
	Pushed      bool
	DryRun      bool
	FromCache   bool
	Refreshed   bool
	Since       string
	Until       string
}

func (e *Engine) client() *heatmap.Client {
	if e.Client != nil {
		return e.Client
	}
	return heatmap.NewClient()
}

// resolveTarget returns the per-day activity counts to match, using the local
// cache for immutable past days and only hitting the API when the cache is
// missing or stale for the current day.
func (e *Engine) resolveTarget(opts BackfillOptions, handle, tz string, loc *time.Location) (map[string]int, int, bool, bool, error) {
	now := e.now()

	cached, ok, _ := cache.Load(handle, tz)
	needFetch := opts.Refresh || !ok || !cached.FreshFor(now, loc)
	if opts.NoRefresh && ok {
		needFetch = false
	}
	if !ok && opts.NoRefresh {
		return nil, 0, false, false, fmt.Errorf("no cached heatmap for @%s (%s); run without --no-refresh once", handle, tz)
	}

	refreshed := false
	if needFetch {
		hm, err := e.client().Fetch(handle, tz)
		if err != nil {
			if !ok {
				return nil, 0, false, false, err
			}
			// Keep working offline with a stale cache.
			fmt.Printf("warning: heatmap refresh failed (%v); using cached data from %s\n",
				err, cached.FetchedAt.Format("2006-01-02 15:04"))
		} else {
			cached.Handle = handle
			cached.Timezone = tz
			cached.Merge(hm, now)
			if err := cache.Save(cached); err != nil {
				fmt.Printf("warning: could not save heatmap cache: %v\n", err)
			}
			refreshed = true
		}
	}

	raw := map[string]int{}
	for day, count := range cached.Days {
		raw[day] = count
	}
	if opts.IncludeGitHubCommits {
		for day, count := range cached.GitHubCommits {
			raw[day] += count
		}
	}

	days, capped := filterDays(raw, opts.Since, opts.Until, opts.MaxPerDay)
	return days, capped, refreshed, !refreshed, nil
}

func filterDays(raw map[string]int, since, until string, maxPerDay int) (map[string]int, int) {
	out := map[string]int{}
	capped := 0
	for day, count := range raw {
		if since != "" && day < since {
			continue
		}
		if until != "" && day > until {
			continue
		}
		if maxPerDay > 0 && count > maxPerDay {
			count = maxPerDay
			capped++
		}
		if count > 0 {
			out[day] = count
		}
	}
	return out, capped
}

func (e *Engine) Backfill(opts BackfillOptions) (BackfillStats, error) {
	handle, tz := e.names(opts.Handle, opts.Timezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return BackfillStats{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}

	target, capped, refreshed, fromCache, err := e.resolveTarget(opts, handle, tz, loc)
	if err != nil {
		return BackfillStats{}, err
	}

	repo := e.repo()
	if !repo.IsGitRepo() {
		return BackfillStats{}, fmt.Errorf("not a git repo: %s", e.Cfg.RepoPath)
	}

	user, err := e.resolveUser()
	if err != nil {
		return BackfillStats{}, fmt.Errorf("cannot determine GitHub identity: %w", err)
	}
	if !opts.DryRun {
		if err := repo.EnsureIdentity(user); err != nil {
			return BackfillStats{}, fmt.Errorf("git identity: %w", err)
		}
	}

	existing, err := repo.MarkerCommitsPerDay(loc, marker.MessagePrefix)
	if err != nil {
		return BackfillStats{}, err
	}
	nextIndex, err := nextBackfillIndexes(repo, target)
	if err != nil {
		return BackfillStats{}, err
	}

	stats := BackfillStats{
		CappedDays: capped,
		DryRun:     opts.DryRun,
		FromCache:  fromCache,
		Refreshed:  refreshed,
		Since:      opts.Since,
		Until:      opts.Until,
	}

	days := map[string]bool{}
	for day := range target {
		days[day] = true
	}
	for day, have := range existing {
		if have > 0 {
			days[day] = true
		}
	}
	ordered := make([]string, 0, len(days))
	for day := range days {
		ordered = append(ordered, day)
	}
	sort.Strings(ordered)

	for _, day := range ordered {
		want := target[day]
		have := existing[day]
		stats.Target += want
		stats.Existing += have

		if have > want {
			stats.OverDays++
			stats.OverCommits += have - want
			stats.Skipped++
			continue
		}
		need := want - have
		if need == 0 {
			if want > 0 {
				stats.Skipped++
			}
			continue
		}
		stats.Days++
		stats.Created += need
		if opts.DryRun {
			continue
		}

		base, err := dayNoon(day, loc)
		if err != nil {
			return stats, fmt.Errorf("day %s: %w", day, err)
		}
		if err := e.addDayCommits(repo, user, day, base, need, nextIndex[day], want); err != nil {
			return stats, fmt.Errorf("day %s: %w", day, err)
		}
	}

	if !opts.DryRun && !opts.SkipPush && (stats.Created > 0 || repo.AheadOfRemote(e.Cfg.Remote, e.Cfg.Branch)) {
		if err := repo.Push(e.Cfg.Remote, e.Cfg.Branch); err != nil {
			return stats, fmt.Errorf("commits are ready but push failed: %w\nFix auth/remote, then re-run: boothub backfill", err)
		}
		stats.Pushed = true
	}

	return stats, nil
}

// addDayCommits writes `need` new backfill markers for day and creates `need`
// commits dated to that day. Staging happens once per day; the first commit
// carries the files and the rest are intentionally empty so the commit count
// rises by exactly `need`.
func (e *Engine) addDayCommits(repo gitops.Repo, user gitops.GitHubUser, day string, base time.Time, need, startIndex, activityCount int) error {
	for i := 0; i < need; i++ {
		id := fmt.Sprintf("backfill-%d", startIndex+1+i)
		when := base.Add(time.Duration(i) * time.Second)
		m := marker.New(id, marker.TriggerBackfill, when)
		m.ActivityCount = activityCount
		if err := marker.Write(e.Cfg.RepoPath, marker.RelPath(day, id), m); err != nil {
			return err
		}
	}
	if err := repo.Add(submissionsPrefix + day); err != nil {
		return err
	}
	for i := 0; i < need; i++ {
		id := fmt.Sprintf("backfill-%d", startIndex+1+i)
		when := base.Add(time.Duration(i) * time.Second)
		if err := repo.CommitAs(&user, marker.MessagePrefix+id, &when, i > 0); err != nil {
			return err
		}
	}
	return nil
}

// nextBackfillIndexes returns, for each target day, the highest existing
// backfill-N index so new markers never collide with old ones.
func nextBackfillIndexes(repo gitops.Repo, target map[string]int) (map[string]int, error) {
	files, err := repo.FilesInHEAD(submissionsPrefix)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, f := range files {
		parts := strings.Split(f, "/")
		if len(parts) != 3 || parts[0] != "submissions" {
			continue
		}
		day := parts[1]
		if _, wanted := target[day]; !wanted {
			continue
		}
		name := strings.TrimSuffix(parts[2], ".json")
		var n int
		if _, err := fmt.Sscanf(name, "backfill-%d", &n); err != nil {
			continue
		}
		if n > out[day] {
			out[day] = n
		}
	}
	return out, nil
}

func (e *Engine) names(handle, tz string) (string, string) {
	if handle == "" {
		handle = e.Cfg.BootdevHandle
	}
	if tz == "" {
		tz = e.Cfg.Timezone
	}
	if tz == "" {
		tz = "UTC"
	}
	return handle, tz
}

func dayNoon(day string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day %q: %w", day, err)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, loc), nil
}
