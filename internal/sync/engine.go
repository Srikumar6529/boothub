package sync

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/heatmap"
	"github.com/Srikumar6529/boothub/internal/marker"
)

const submissionsPrefix = "submissions/"

type Request struct {
	LessonID      string
	Trigger       string
	When          time.Time
	Note          string
	ActivityCount int
	Backdate      bool
	SkipPush      bool
	Force         bool // rewrite marker even if identical on disk
}

type Result struct {
	Skipped bool
	RelPath string
	Message string
}

type Engine struct {
	Cfg    config.Config
	Client *heatmap.Client
	Now    func() time.Time

	user *gitops.GitHubUser
}

func New(cfg config.Config) *Engine {
	return &Engine{Cfg: cfg, Client: heatmap.NewClient(), Now: time.Now}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) repo() gitops.Repo {
	return gitops.Repo{Path: e.Cfg.RepoPath}
}

// resolveUser resolves (and memoizes) the GitHub identity. Previously this ran
// `gh api user` once per commit, which made large backfills slow and prone to
// API rate limits.
func (e *Engine) resolveUser() (gitops.GitHubUser, error) {
	if e.user != nil {
		return *e.user, nil
	}
	u, err := gitops.CurrentGitHubUser()
	if err != nil {
		return gitops.GitHubUser{}, err
	}
	e.user = &u
	return u, nil
}

// User exposes the resolved GitHub identity (also used by setup/repair).
func (e *Engine) User() (gitops.GitHubUser, error) { return e.resolveUser() }

// SetIdentity injects a GitHub identity, bypassing `gh`. Useful for tests.
func (e *Engine) SetIdentity(u gitops.GitHubUser) { e.user = &u }

func (e *Engine) Sync(req Request) (Result, error) {
	if !e.Cfg.Enabled {
		return Result{Skipped: true, Message: "sync disabled in config"}, nil
	}
	when := req.When
	if when.IsZero() {
		when = e.now()
	}

	lessonID := req.LessonID
	if strings.TrimSpace(lessonID) == "" {
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
	dayKey := marker.DayKey(when, loc)
	rel := marker.RelPath(dayKey, lessonID)

	m := marker.New(lessonID, req.Trigger, when)
	m.Note = req.Note
	m.ActivityCount = req.ActivityCount
	if req.Force {
		// A unique note guarantees the file content changes so git always
		// creates a new commit. Without this, a second --force run would try
		// to commit an identical file and fail with "nothing to commit".
		m.Note = forceNote(m.Note, e.now())
	}

	repo := e.repo()
	if !repo.IsGitRepo() {
		return Result{}, fmt.Errorf("not a git repo: %s", e.Cfg.RepoPath)
	}

	identical, err := marker.Identical(e.Cfg.RepoPath, rel, m)
	if err != nil {
		return Result{}, err
	}

	// Skip only when content matches AND it is already committed. Orphan
	// on-disk markers (failed prior commit) must still be committed.
	if identical && !req.Force && repo.FileInHEAD(rel) {
		msg := "marker already committed"
		if !req.SkipPush && repo.AheadOfRemote(e.Cfg.Remote, e.Cfg.Branch) {
			if perr := repo.Push(e.Cfg.Remote, e.Cfg.Branch); perr != nil {
				return Result{}, fmt.Errorf("push failed: %w", perr)
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

	if bad, err := repo.StagedOutside(submissionsPrefix); err != nil {
		return Result{}, err
	} else if len(bad) > 0 {
		return Result{}, fmt.Errorf("refusing to commit: unrelated staged files (%s); commit or unstage them first", strings.Join(bad, ", "))
	}

	if err := repo.Add(rel); err != nil {
		return Result{}, err
	}

	user, err := e.resolveUser()
	if err != nil {
		return Result{}, fmt.Errorf("cannot determine GitHub identity: %w", err)
	}

	msg := fmt.Sprintf("%s%s", marker.MessagePrefix, lessonID)
	// Always pin the commit to the configured timezone so GitHub's day
	// bucketing matches the heatmap even when the machine timezone differs.
	local := when.In(loc)
	if err := repo.CommitAs(&user, msg, &local, false); err != nil {
		if isNothingToCommit(err) {
			return Result{Skipped: true, RelPath: rel, Message: "nothing to commit"}, nil
		}
		return Result{}, err
	}

	if !req.SkipPush {
		if err := repo.Push(e.Cfg.Remote, e.Cfg.Branch); err != nil {
			return Result{}, fmt.Errorf("push failed: %w", err)
		}
	}

	return Result{RelPath: rel, Message: "synced"}, nil
}

func (e *Engine) Push() error {
	repo := e.repo()
	return repo.Push(e.Cfg.Remote, e.Cfg.Branch)
}

// NoonLocal returns noon on the calendar day of t in loc (stable backfill
// timestamp base).
func NoonLocal(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
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

// EnsureRepoPath is kept for callers that want to create the repo directory.
func EnsureRepoPath(path string) error {
	return os.MkdirAll(path, 0o755)
}
