package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type backfillOptions struct {
	Handle               string
	Timezone             string
	MaxPerDay            int
	DryRun               bool
	Refresh              bool
	NoRefresh            bool
	Since                string
	Until                string
	IncludeGitHubCommits bool
	SkipPush             bool
}

type backfillStats struct {
	Days        int
	Target      int
	Existing    int
	Created     int
	Skipped     int
	CappedDays  int
	OverDays    int
	OverCommits int
	Pushed      bool
	DryRun      bool
	FromCache   bool
	Refreshed   bool
	Since       string
	Until       string
}

// resolveTarget returns the per-day activity counts to match. It uses the local
// cache for immutable past days and only calls the API when the cache is
// missing or stale for the current local day.
func (a *app) resolveTarget(opts backfillOptions, handle, tz string, loc *time.Location) (map[string]int, int, bool, bool, error) {
	now := time.Now()

	cached, ok := loadHeatmapCache(handle, tz)
	needFetch := opts.Refresh || !ok || !cached.freshFor(now, loc)
	if opts.NoRefresh && ok {
		needFetch = false
	}
	if !ok && opts.NoRefresh {
		return nil, 0, false, false, fmt.Errorf("no cached heatmap for @%s (%s); run once without --no-refresh", handle, tz)
	}

	refreshed := false
	if needFetch {
		hm, err := fetchHeatmap(handle, tz)
		if err != nil {
			if !ok {
				return nil, 0, false, false, err
			}
			fmt.Printf("warning: heatmap refresh failed (%v); using cached data from %s\n",
				err, cached.FetchedAt.Format("2006-01-02 15:04"))
		} else {
			cached.Handle = handle
			cached.Timezone = tz
			cached.merge(hm, now)
			if err := saveHeatmapCache(cached); err != nil {
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
		for day, count := range cached.GitHub {
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

// backfill tops up the history by exactly (target - existing) commits per day,
// creating every commit in a single `git fast-import` pass.
func (a *app) backfill(opts backfillOptions) (backfillStats, error) {
	handle, tz := a.names(opts.Handle, opts.Timezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return backfillStats{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}

	target, capped, refreshed, fromCache, err := a.resolveTarget(opts, handle, tz, loc)
	if err != nil {
		return backfillStats{}, err
	}

	r := a.repo()
	if !r.isRepo() {
		return backfillStats{}, fmt.Errorf("not a git repo: %s", a.cfg.RepoPath)
	}

	user, err := a.identity()
	if err != nil {
		return backfillStats{}, fmt.Errorf("cannot determine GitHub identity: %w", err)
	}
	if !opts.DryRun {
		if err := r.ensureIdentity(user); err != nil {
			return backfillStats{}, fmt.Errorf("git identity: %w", err)
		}
	}

	existing := r.markerCounts(loc)
	nextIndex, err := nextBackfillIndexes(r, target)
	if err != nil {
		return backfillStats{}, err
	}

	stats := backfillStats{
		CappedDays: capped,
		DryRun:     opts.DryRun,
		FromCache:  fromCache,
		Refreshed:  refreshed,
		Since:      opts.Since,
		Until:      opts.Until,
	}

	var entries []importEntry
	for _, day := range unionDays(target, existing) {
		want, have := target[day], existing[day]
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
		start := nextIndex[day]
		for i := 0; i < need; i++ {
			id := fmt.Sprintf("backfill-%d", start+1+i)
			when := base.Add(time.Duration(i) * time.Second)
			m := newMarker(id, triggerBackfill, when)
			m.ActivityCount = want
			content, err := encodeMarker(m)
			if err != nil {
				return stats, err
			}
			entries = append(entries, importEntry{
				rel:     markerRelPath(day, id),
				when:    when,
				content: content,
				message: markerPrefix + id,
			})
		}
	}

	if !opts.DryRun && len(entries) > 0 {
		parent, _ := r.head()
		if err := r.fastImport(a.cfg.Branch, parent, entries, user, nil); err != nil {
			return stats, err
		}
		if err := r.resetHard(""); err != nil {
			return stats, err
		}
	}

	if !opts.DryRun && !opts.SkipPush && (stats.Created > 0 || r.aheadOfRemote(a.cfg.Remote, a.cfg.Branch)) {
		if err := r.push(a.cfg.Remote, a.cfg.Branch); err != nil {
			return stats, fmt.Errorf("commits are ready but push failed: %w\nFix auth/remote, then re-run: boothub backfill", err)
		}
		stats.Pushed = true
	}

	return stats, nil
}

func unionDays(target, existing map[string]int) []string {
	seen := map[string]bool{}
	for day := range target {
		seen[day] = true
	}
	for day, have := range existing {
		if have > 0 {
			seen[day] = true
		}
	}
	ordered := make([]string, 0, len(seen))
	for day := range seen {
		ordered = append(ordered, day)
	}
	sort.Strings(ordered)
	return ordered
}

// nextBackfillIndexes returns, for each target day, the highest existing
// backfill-N index so new markers never collide with old ones.
func nextBackfillIndexes(r repo, target map[string]int) (map[string]int, error) {
	files, err := r.filesInHEAD(submissionsPrefix)
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
