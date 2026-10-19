package sync

import (
	"bytes"
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

// RepairOptions controls a full rebuild of the submissions history so that the
// number of commits on each day exactly matches the Boot.dev heatmap.
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
	// Push is false only in tests; the CLI always pushes.
	Push bool
}

type RepairStats struct {
	Days       int
	Commits    int
	Preserved  int
	Dropped    int
	Backup     string
	Pushed     bool
	DryRun     bool
	FromCache  bool
	Refreshed  bool
	OldCommits int
	OldMarkers int
}

const rebuildBranch = "boothub-rebuild"

type preservedMarker struct {
	id      string
	rel     string
	when    time.Time
	content []byte
}

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

	repo := e.repo()
	if !repo.IsGitRepo() {
		return RepairStats{}, fmt.Errorf("not a git repo: %s", e.Cfg.RepoPath)
	}
	if !repo.IsClean() {
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
	// Markers on days the heatmap does not show will be dropped by the rebuild.
	for day, list := range preserved {
		if _, ok := target[day]; !ok {
			droppedPre += len(list)
		}
	}

	plan, stats, err := buildRepairPlan(target, preserved, droppedPre, loc)
	if err != nil {
		return RepairStats{}, err
	}
	stats.DryRun = opts.DryRun
	stats.FromCache = fromCache
	stats.Refreshed = refreshed

	if old, oldErr := repo.MarkerCommitsPerDay(loc, marker.MessagePrefix); oldErr == nil {
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
	if head, err := repo.Head(); err == nil && head != "" {
		backup := fmt.Sprintf("boothub-backup-%d", e.now().Unix())
		if err := repo.CreateBranch(backup, "HEAD"); err == nil {
			stats.Backup = backup
			if opts.Push {
				if err := repo.PushBranch(e.Cfg.Remote, backup); err != nil {
					fmt.Printf("warning: could not push backup branch %s: %v\n", backup, err)
				}
			}
		}
	}

	stream := buildFastImportStream(plan, user)
	if err := repo.FastImport(stream); err != nil {
		return stats, err
	}
	if err := repo.ResetHard(rebuildBranch); err != nil {
		return stats, err
	}
	_ = repo.DeleteBranch(rebuildBranch)

	if opts.Push {
		if err := repo.PushForce(e.Cfg.Remote, e.Cfg.Branch); err != nil {
			return stats, fmt.Errorf("rebuilt history locally but force-push failed: %w", err)
		}
		stats.Pushed = true
	}
	return stats, nil
}

type repairEntry struct {
	id      string
	rel     string
	when    time.Time
	content []byte
}

type repairDay struct {
	day     string
	entries []repairEntry
}

type repairPlan struct {
	days   []repairDay
	readme []byte
	total  int
}

func buildRepairPlan(target map[string]int, preserved map[string][]preservedMarker, droppedPre int, loc *time.Location) (repairPlan, RepairStats, error) {
	stats := RepairStats{Dropped: droppedPre}
	plan := repairPlan{
		readme: []byte("# bootdev-submissions\n\nSynced automatically by [BootHub](https://github.com/Srikumar6529/boothub).\n"),
	}

	days := make([]string, 0, len(target))
	for day := range target {
		days = append(days, day)
	}
	sort.Strings(days)

	for _, day := range days {
		want := target[day]
		base, err := dayNoon(day, loc)
		if err != nil {
			return plan, stats, err
		}

		keep := preserved[day]
		sort.Slice(keep, func(i, j int) bool { return keep[i].when.Before(keep[j].when) })
		if len(keep) > want {
			stats.Dropped += len(keep) - want
			keep = keep[:want]
		}

		entries := make([]repairEntry, 0, want)
		// Normalize every commit to noon on the target day in the configured
		// timezone. GitHub buckets commits by the offset stored in the commit,
		// so a preserved UTC timestamp would otherwise land on the wrong day.
		for i, p := range keep {
			when := base.Add(time.Duration(i) * time.Second)
			entries = append(entries, repairEntry{id: p.id, rel: p.rel, when: when, content: p.content})
			stats.Preserved++
		}

		for n := len(keep) + 1; n <= want; n++ {
			id := fmt.Sprintf("backfill-%d", n)
			when := base.Add(time.Duration(n) * time.Second)
			m := marker.New(id, marker.TriggerBackfill, when)
			m.ActivityCount = want
			content, err := marker.Encode(m)
			if err != nil {
				return plan, stats, err
			}
			content = append(content, '\n')
			entries = append(entries, repairEntry{id: id, rel: marker.RelPath(day, id), when: when, content: content})
		}

		if len(entries) == 0 {
			continue
		}
		stats.Days++
		stats.Commits += len(entries)
		plan.days = append(plan.days, repairDay{day: day, entries: entries})
	}
	plan.total = stats.Commits
	return plan, stats, nil
}

// buildFastImportStream renders the whole history as one git fast-import
// stream, so thousands of commits are created in a single subprocess.
func buildFastImportStream(plan repairPlan, user gitops.GitHubUser) []byte {
	var b bytes.Buffer
	blobMark := 0

	nextBlob := func(content []byte) int {
		blobMark++
		fmt.Fprintf(&b, "blob\nmark :%d\ndata %d\n", blobMark, len(content))
		b.Write(content)
		b.WriteByte('\n')
		return blobMark
	}
	commitCount := 0
	first := true

	readmeMark := nextBlob(plan.readme)

	for _, d := range plan.days {
		for _, entry := range d.entries {
			content := entry.content
			if content == nil {
				m := marker.New(entry.id, marker.TriggerManual, entry.when)
				content, _ = marker.Encode(m)
				content = append(content, '\n')
			}
			mark := nextBlob(content)

			msg := marker.MessagePrefix + entry.id
			when := entry.when
			fmt.Fprintf(&b, "commit refs/heads/%s\n", rebuildBranch)
			fmt.Fprintf(&b, "author %s <%s> %d %s\n", user.Name, user.Email, when.Unix(), when.Format("-0700"))
			fmt.Fprintf(&b, "committer %s <%s> %d %s\n", user.Name, user.Email, when.Unix(), when.Format("-0700"))
			fmt.Fprintf(&b, "data %d\n%s\n", len(msg), msg)
			fmt.Fprintf(&b, "M 100644 :%d %s\n", mark, entry.rel)
			if first {
				fmt.Fprintf(&b, "M 100644 :%d README.md\n", readmeMark)
				first = false
			}
			commitCount++
		}
	}

	// Never produce an empty stream: fast-import needs at least one commit.
	if commitCount == 0 {
		fmt.Fprintf(&b, "commit refs/heads/%s\n", rebuildBranch)
		fmt.Fprintf(&b, "author %s <%s> %d +0000\n", user.Name, user.Email, time.Now().Unix())
		fmt.Fprintf(&b, "committer %s <%s> %d +0000\n", user.Name, user.Email, time.Now().Unix())
		msg := "Initial commit"
		fmt.Fprintf(&b, "data %d\n%s\n", len(msg), msg)
		fmt.Fprintf(&b, "M 100644 :%d README.md\n", readmeMark)
	}
	return b.Bytes()
}

type markerHeader struct {
	LessonID    string    `json:"lesson_id"`
	Trigger     string    `json:"trigger"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// readPreservedMarkers collects every non-backfill marker currently on disk so
// a repair keeps real submissions instead of replacing them with noise.
func readPreservedMarkers(repoPath string) (map[string][]preservedMarker, int, error) {
	root := filepath.Join(repoPath, "submissions")
	out := map[string][]preservedMarker{}
	dropped := 0

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		var h markerHeader
		if err := json.Unmarshal(content, &h); err != nil {
			return nil
		}
		if h.Trigger == "" || h.Trigger == marker.TriggerBackfill {
			return nil
		}
		id := h.LessonID
		if id == "" {
			id = strings.TrimSuffix(parts[2], ".json")
		}
		out[parts[1]] = append(out[parts[1]], preservedMarker{
			id:      id,
			rel:     rel,
			when:    h.SubmittedAt,
			content: content,
		})
		return nil
	})
	if walkErr != nil {
		return out, dropped, walkErr
	}
	return out, dropped, nil
}
