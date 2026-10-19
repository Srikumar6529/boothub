package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/marker"
)

type RepairOptions struct {
	Handle               string
	Timezone             string
	Yes                  bool
	DryRun               bool
	Refresh              bool
	NoRefresh            bool
	IncludeGitHubCommits bool
	Since                string
	Until                string
	MaxPerDay            int
	Push                 bool
}

type RepairStats struct {
	Days       int
	Commits    int
	Preserved  int
	Dropped    int
	OldCommits int
	OldMarkers int
	Backup     string
	Pushed     bool
	DryRun     bool
	FromCache  bool
	Refreshed  bool
}

const rebuildBranch = "boothub-rebuild"

type preservedMarker struct {
	id      string
	rel     string
	when    time.Time
	content []byte
}

// Repair rebuilds the whole history so each day has exactly as many commits as
// the heatmap shows, keeping real submission markers where possible.
func (e *Engine) Repair(opts RepairOptions) (RepairStats, error) {
	handle, tz := e.names(opts.Handle, opts.Timezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return RepairStats{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}

	target, _, refreshed, fromCache, err := e.resolveTarget(BackfillOptions{
		Handle:               handle,
		Timezone:             tz,
		Refresh:              opts.Refresh,
		NoRefresh:            opts.NoRefresh,
		Since:                opts.Since,
		Until:                opts.Until,
		MaxPerDay:            opts.MaxPerDay,
		IncludeGitHubCommits: opts.IncludeGitHubCommits,
	}, handle, tz, loc)
	if err != nil {
		return RepairStats{}, err
	}

	r := e.repo()
	if !r.IsRepo() {
		return RepairStats{}, fmt.Errorf("not a git repo: %s", e.Cfg.RepoPath)
	}
	if !r.IsClean() {
		return RepairStats{}, fmt.Errorf("working tree at %s has uncommitted changes; commit or discard them before repairing", e.Cfg.RepoPath)
	}

	user, err := e.resolveUser()
	if err != nil {
		return RepairStats{}, fmt.Errorf("cannot determine GitHub identity: %w", err)
	}

	preserved, droppedPre, err := readPreservedMarkers(e.Cfg.RepoPath)
	if err != nil {
		return RepairStats{}, err
	}
	for day, list := range preserved {
		if _, ok := target[day]; !ok {
			droppedPre += len(list)
		}
	}

	entries, stats, err := buildRepairPlan(target, preserved, droppedPre, loc)
	if err != nil {
		return RepairStats{}, err
	}
	stats.DryRun = opts.DryRun
	stats.FromCache = fromCache
	stats.Refreshed = refreshed

	if old := r.MarkerCounts(loc, marker.Prefix); len(old) > 0 {
		for _, c := range old {
			stats.OldCommits += c
		}
		stats.OldMarkers = len(preserved)
	}

	if opts.DryRun {
		return stats, nil
	}
	if !opts.Yes {
		return stats, fmt.Errorf("repair rewrites history and force-pushes; re-run with --yes to confirm")
	}

	// Back up the old history locally and (best effort) on the remote.
	if head, err := r.Head(); err == nil && head != "" {
		backup := fmt.Sprintf("boothub-backup-%d", e.now().Unix())
		if err := r.CreateBranch(backup, "HEAD"); err == nil {
			stats.Backup = backup
			if opts.Push {
				if err := r.Push(e.Cfg.Remote, backup); err != nil {
					fmt.Printf("warning: could not push backup branch %s: %v\n", backup, err)
				}
			}
		}
	}

	if err := r.FastImport(rebuildBranch, "", entries, user, []byte(gitops.DefaultReadme)); err != nil {
		return stats, err
	}
	if err := r.ResetHard(rebuildBranch); err != nil {
		return stats, err
	}
	_ = r.DeleteBranch(rebuildBranch)

	if opts.Push {
		if err := r.PushForce(e.Cfg.Remote, e.Cfg.Branch); err != nil {
			return stats, fmt.Errorf("rebuilt history locally but force-push failed: %w", err)
		}
		stats.Pushed = true
	}
	return stats, nil
}

func buildRepairPlan(target map[string]int, preserved map[string][]preservedMarker, droppedPre int, loc *time.Location) ([]gitops.ImportEntry, RepairStats, error) {
	stats := RepairStats{Dropped: droppedPre}

	days := make([]string, 0, len(target))
	for day := range target {
		days = append(days, day)
	}
	sort.Strings(days)

	var entries []gitops.ImportEntry
	for _, day := range days {
		want := target[day]
		base, err := dayNoon(day, loc)
		if err != nil {
			return nil, stats, err
		}

		keep := preserved[day]
		sort.Slice(keep, func(i, j int) bool { return keep[i].when.Before(keep[j].when) })
		if len(keep) > want {
			stats.Dropped += len(keep) - want
			keep = keep[:want]
		}

		dayEntries := make([]gitops.ImportEntry, 0, want)
		// Normalize every commit to noon on the target day in the configured
		// timezone: GitHub buckets by the offset stored in the commit, so a
		// preserved UTC timestamp would otherwise land on the wrong day.
		for i, p := range keep {
			when := base.Add(time.Duration(i) * time.Second)
			dayEntries = append(dayEntries, gitops.ImportEntry{Rel: p.rel, When: when, Content: p.content, Message: marker.Prefix + p.id})
			stats.Preserved++
		}
		for n := len(keep) + 1; n <= want; n++ {
			id := fmt.Sprintf("backfill-%d", n)
			when := base.Add(time.Duration(n) * time.Second)
			m := marker.New(id, marker.TriggerBackfill, when)
			m.ActivityCount = want
			content, err := marker.Encode(m)
			if err != nil {
				return nil, stats, err
			}
			dayEntries = append(dayEntries, gitops.ImportEntry{Rel: marker.RelPath(day, id), When: when, Content: content, Message: marker.Prefix + id})
		}

		if len(dayEntries) == 0 {
			continue
		}
		stats.Days++
		stats.Commits += len(dayEntries)
		entries = append(entries, dayEntries...)
	}
	return entries, stats, nil
}

// readPreservedMarkers collects every non-backfill marker currently on disk so a
// repair keeps real submissions instead of replacing them with noise.
func readPreservedMarkers(repoPath string) (map[string][]preservedMarker, int, error) {
	root := filepath.Join(repoPath, "submissions")
	out := map[string][]preservedMarker{}
	dropped := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if len(parts) != 3 || parts[0] != "submissions" || !marker.ValidDayKey(parts[1]) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var m marker.Marker
		if err := json.Unmarshal(content, &m); err != nil {
			return nil
		}
		if m.Trigger == "" || m.Trigger == marker.TriggerBackfill {
			return nil
		}
		id := m.LessonID
		if id == "" {
			id = strings.TrimSuffix(parts[2], ".json")
		}
		out[parts[1]] = append(out[parts[1]], preservedMarker{id: id, rel: rel, when: m.SubmittedAt, content: content})
		return nil
	})
	if err != nil {
		return out, dropped, err
	}
	return out, dropped, nil
}
