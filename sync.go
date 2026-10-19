package main

import (
	"fmt"
	"strings"
	"time"
)

// app carries the loaded config and a memoized GitHub identity. The identity is
// resolved at most once per process so large backfills do not hammer the API.
type app struct {
	cfg  config
	user *ghUser
}

func newApp() (app, error) {
	cfg, err := loadConfig()
	if err != nil {
		return app{}, err
	}
	return app{cfg: cfg}, nil
}

func (a *app) repo() repo { return repo{path: a.cfg.RepoPath} }

func (a *app) identity() (ghUser, error) {
	if a.user != nil {
		return *a.user, nil
	}
	u, err := currentGHUser()
	if err != nil {
		return ghUser{}, err
	}
	a.user = &u
	return u, nil
}

func (a *app) names(handle, tz string) (string, string) {
	if handle == "" {
		handle = a.cfg.BootdevHandle
	}
	if tz == "" {
		tz = a.cfg.Timezone
	}
	if tz == "" {
		tz = "UTC"
	}
	return handle, tz
}

// --- single sync ------------------------------------------------------------

type syncRequest struct {
	LessonID      string
	Trigger       string
	When          time.Time
	Note          string
	ActivityCount int
	Force         bool
	SkipPush      bool
}

type syncResult struct {
	Skipped bool
	RelPath string
	Message string
}

// syncOne writes one marker file and commits+pushes it. It is the hot path for
// `boothub submit`, so it resolves the identity once and uses a single commit
// and a single push.
func (a *app) syncOne(req syncRequest) (syncResult, error) {
	if !a.cfg.Enabled {
		return syncResult{Skipped: true, Message: "sync disabled in config"}, nil
	}
	when := req.When
	if when.IsZero() {
		when = time.Now()
	}

	lessonID := strings.TrimSpace(req.LessonID)
	if lessonID == "" {
		lessonID = fmt.Sprintf("unknown-%d", when.Unix())
	} else {
		clean, err := sanitizeLessonID(lessonID)
		if err != nil {
			return syncResult{}, err
		}
		lessonID = clean
	}

	loc, err := a.cfg.location()
	if err != nil {
		return syncResult{}, err
	}
	rel := markerRelPath(dayKey(when, loc), lessonID)

	m := newMarker(lessonID, req.Trigger, when)
	m.Note = req.Note
	m.ActivityCount = req.ActivityCount
	if req.Force {
		// A unique note makes the content differ so git always makes a new
		// commit, even when the marker already exists on disk.
		m.Note = forceNote(m.Note, time.Now())
	}

	r := a.repo()
	if !r.isRepo() {
		return syncResult{}, fmt.Errorf("not a git repo: %s", a.cfg.RepoPath)
	}

	identical, err := markerIdentical(a.cfg.RepoPath, rel, m)
	if err != nil {
		return syncResult{}, err
	}
	// Skip only when the content matches AND it is already committed; an
	// orphan on-disk marker from a failed commit must still be committed.
	if identical && !req.Force && r.fileInHEAD(rel) {
		msg := "marker already committed"
		if !req.SkipPush && r.aheadOfRemote(a.cfg.Remote, a.cfg.Branch) {
			if err := r.push(a.cfg.Remote, a.cfg.Branch); err != nil {
				return syncResult{}, fmt.Errorf("push failed: %w", err)
			}
			msg = "marker already committed; pushed pending commits"
		}
		return syncResult{Skipped: true, RelPath: rel, Message: msg}, nil
	}

	if !identical || req.Force || !markerExists(a.cfg.RepoPath, rel) {
		if err := writeMarker(a.cfg.RepoPath, rel, m); err != nil {
			return syncResult{}, err
		}
	}

	if bad, err := r.stagedOutside(submissionsPrefix); err != nil {
		return syncResult{}, err
	} else if len(bad) > 0 {
		return syncResult{}, fmt.Errorf("refusing to commit: unrelated staged files (%s); commit or unstage them first", strings.Join(bad, ", "))
	}

	if err := r.add(rel); err != nil {
		return syncResult{}, err
	}

	user, err := a.identity()
	if err != nil {
		return syncResult{}, fmt.Errorf("cannot determine GitHub identity: %w", err)
	}

	// Pin the commit to the configured timezone so GitHub's day bucketing
	// matches the heatmap even when the machine timezone differs.
	local := when.In(loc)
	if err := r.commit(&user, markerPrefix+lessonID, &local, false); err != nil {
		if isNothingToCommit(err) {
			return syncResult{Skipped: true, RelPath: rel, Message: "nothing to commit"}, nil
		}
		return syncResult{}, err
	}

	if !req.SkipPush {
		if err := r.push(a.cfg.Remote, a.cfg.Branch); err != nil {
			return syncResult{}, fmt.Errorf("push failed: %w", err)
		}
	}
	return syncResult{RelPath: rel, Message: "synced"}, nil
}

// --- shared helpers ---------------------------------------------------------

// noonLocal returns noon on the calendar day of t in loc.
func noonLocal(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
}

func dayNoon(day string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day %q: %w", day, err)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, loc), nil
}

func forceNote(existing string, when time.Time) string {
	nonce := fmt.Sprintf("force-rebuild-%d", when.UnixNano())
	if existing == "" {
		return nonce
	}
	return existing + " | " + nonce
}

func isNothingToCommit(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "nothing to commit") || strings.Contains(msg, "no changes added to commit")
}
