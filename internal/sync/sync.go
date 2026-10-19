package sync

import (
	"fmt"
	"strings"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/marker"
)

// Engine performs the sync work. The GitHub identity is resolved at most once
// per process so large backfills do not hammer the API.
type Engine struct {
	Cfg  config.Config
	user *gitops.User
	Now  func() time.Time
}

func New(cfg config.Config) *Engine {
	return &Engine{Cfg: cfg, Now: time.Now}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) repo() gitops.Repo { return gitops.Repo{Path: e.Cfg.RepoPath} }

// SetIdentity injects a GitHub identity, bypassing `gh`. Useful for tests.
func (e *Engine) SetIdentity(u gitops.User) { e.user = &u }

func (e *Engine) resolveUser() (gitops.User, error) {
	if e.user != nil {
		return *e.user, nil
	}
	u, err := gitops.CurrentUser()
	if err != nil {
		return gitops.User{}, err
	}
	e.user = &u
	return u, nil
}

// User exposes the resolved GitHub identity (also used by setup).
func (e *Engine) User() (gitops.User, error) { return e.resolveUser() }

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

type Request struct {
	LessonID      string
	Trigger       string
	When          time.Time
	Note          string
	ActivityCount int
	Force         bool
	SkipPush      bool
}

type Result struct {
	Skipped bool
	RelPath string
	Message string
}

// Sync writes one marker file and commits+pushes it. It is the hot path for
// `boothub submit`, so it resolves the identity once and uses a single commit
// and a single push.
func (e *Engine) Sync(req Request) (Result, error) {
	if !e.Cfg.Enabled {
		return Result{Skipped: true, Message: "sync disabled in config"}, nil
	}
	when := req.When
	if when.IsZero() {
		when = e.now()
	}

	lessonID := strings.TrimSpace(req.LessonID)
	if lessonID == "" {
		lessonID = fmt.Sprintf("unknown-%d", when.Unix())
	} else {
		clean, err := marker.SanitizeLessonID(lessonID)
		if err != nil {
			return Result{}, err
		}
		lessonID = clean
	}

	loc, err := e.Cfg.Location()
	if err != nil {
		return Result{}, err
	}
	rel := marker.RelPath(marker.DayKey(when, loc), lessonID)

	m := marker.New(lessonID, req.Trigger, when)
	m.Note = req.Note
	m.ActivityCount = req.ActivityCount
	if req.Force {
		// A unique note makes the content differ so git always makes a new
		// commit, even when the marker already exists on disk.
		m.Note = forceNote(m.Note, e.now())
	}

	r := e.repo()
	if !r.IsRepo() {
		return Result{}, fmt.Errorf("not a git repo: %s", e.Cfg.RepoPath)
	}

	identical, err := marker.Identical(e.Cfg.RepoPath, rel, m)
	if err != nil {
		return Result{}, err
	}
	// Skip only when the content matches AND it is already committed; an
	// orphan on-disk marker from a failed commit must still be committed.
	if identical && !req.Force && r.FileInHEAD(rel) {
		msg := "marker already committed"
		if !req.SkipPush && r.AheadOfRemote(e.Cfg.Remote, e.Cfg.Branch) {
			if err := r.Push(e.Cfg.Remote, e.Cfg.Branch); err != nil {
				return Result{}, fmt.Errorf("push failed: %w", err)
			}
			msg = "marker already committed; pushed pending commits"
		}
		return Result{Skipped: true, RelPath: rel, Message: msg}, nil
	}

	if !identical || req.Force || !marker.Exists(e.Cfg.RepoPath, rel) {
		if err := marker.Write(e.Cfg.RepoPath, rel, m); err != nil {
			return Result{}, err
		}
	}

	if bad, err := r.StagedOutside(marker.SubmissionsPrefix); err != nil {
		return Result{}, err
	} else if len(bad) > 0 {
		return Result{}, fmt.Errorf("refusing to commit: unrelated staged files (%s); commit or unstage them first", strings.Join(bad, ", "))
	}

	if err := r.Add(rel); err != nil {
		return Result{}, err
	}

	user, err := e.resolveUser()
	if err != nil {
		return Result{}, fmt.Errorf("cannot determine GitHub identity: %w", err)
	}

	// Pin the commit to the configured timezone so GitHub's day bucketing
	// matches the heatmap even when the machine timezone differs.
	local := when.In(loc)
	if err := r.Commit(&user, marker.Prefix+lessonID, &local, false); err != nil {
		if isNothingToCommit(err) {
			return Result{Skipped: true, RelPath: rel, Message: "nothing to commit"}, nil
		}
		return Result{}, err
	}

	if !req.SkipPush {
		if err := r.Push(e.Cfg.Remote, e.Cfg.Branch); err != nil {
			return Result{}, fmt.Errorf("push failed: %w", err)
		}
	}
	return Result{RelPath: rel, Message: "synced"}, nil
}

// NoonLocal returns noon on the calendar day of t in loc.
func NoonLocal(t time.Time, loc *time.Location) time.Time {
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
